package api

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jnovack/cloudkey/internal/state"
)

// nextData reads lines until the next "data: " event and returns its JSON
// payload parsed into a key map, or fails if none arrives promptly.
func nextData(t *testing.T, r *bufio.Reader) map[string]json.RawMessage {
	t.Helper()
	deadline := time.After(2 * time.Second)
	lines := make(chan string, 1)
	go func() {
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				close(lines)
				return
			}
			if strings.HasPrefix(line, "data: ") {
				lines <- strings.TrimPrefix(strings.TrimRight(line, "\n"), "data: ")
				return
			}
		}
	}()
	select {
	case line, ok := <-lines:
		if !ok {
			t.Fatal("stream closed before a data event arrived")
		}
		var m map[string]json.RawMessage
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("unmarshal data %q: %v", line, err)
		}
		return m
	case <-deadline:
		t.Fatal("timed out waiting for a data event")
		return nil
	}
}

func TestSSEInitialFrameThenSlicePatch(t *testing.T) {
	hub := state.NewHub()
	srv := httptest.NewServer(sseHandler(hub))
	defer srv.Close()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}

	r := bufio.NewReader(resp.Body)

	// The first event is the full snapshot: every top-level key present.
	initial := nextData(t, r)
	for _, key := range []string{"host", "net", "cpu", "mem", "ssd", "rootfs", "sdcard", "apps", "tunnels"} {
		if _, ok := initial[key]; !ok {
			t.Errorf("initial frame missing key %q", key)
		}
	}

	// A post-connect publish must arrive as a single-slice patch.
	hub.PublishCPU(state.CPU{Pct: 73, Cores: 4, LoadAvg: 0.9})
	patch := nextData(t, r)
	if len(patch) != 1 {
		t.Fatalf("patch has %d keys, want 1: %v", len(patch), patch)
	}
	if _, ok := patch["cpu"]; !ok {
		t.Fatalf("patch missing cpu key: %v", patch)
	}
}

func TestSSEClientDisconnectReturns(t *testing.T) {
	hub := state.NewHub()
	srv := httptest.NewServer(sseHandler(hub))
	defer srv.Close()

	ctx, cancel := context.WithCancel(t.Context())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}

	r := bufio.NewReader(resp.Body)
	nextData(t, r) // consume the initial frame so the handler is fully engaged

	// Cancelling the request context must let the handler return; reading the
	// body then yields an error rather than hanging.
	cancel()
	done := make(chan struct{})
	go func() {
		_, _ = r.ReadString('\n')
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("body read did not unblock after client disconnect")
	}
	resp.Body.Close()
}

func TestServeEndToEnd(t *testing.T) {
	// Grab a free port, then hand it to Serve. The brief gap between closing
	// the probe listener and Serve binding is a standard, tolerable test race.
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("probe listen: %v", err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	_ = probe.Close()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "dashboard.html"), []byte("<html>ok</html>"), 0o600); err != nil {
		t.Fatalf("write dashboard: %v", err)
	}

	hub := state.NewHub()
	ctx, cancel := context.WithCancel(t.Context())
	errCh := make(chan error, 1)
	go func() { errCh <- Serve(ctx, hub, Config{Port: port, WebRoot: dir}) }()

	base := "http://127.0.0.1:" + strconv.Itoa(port)

	// Poll until the server is accepting connections.
	var rootBody []byte
	for range 50 {
		resp, err := http.Get(base + "/")
		if err == nil {
			rootBody, _ = io.ReadAll(resp.Body)
			resp.Body.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if string(rootBody) != "<html>ok</html>" {
		t.Fatalf("GET / = %q, want dashboard.html contents", rootBody)
	}

	// /events yields the full initial snapshot.
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /events: %v", err)
	}
	frame := nextData(t, bufio.NewReader(resp.Body))
	resp.Body.Close()
	if _, ok := frame["tunnels"]; !ok {
		t.Errorf("initial /events frame missing tunnels key: %v", frame)
	}

	// Cancelling the context must shut Serve down cleanly (nil error).
	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("Serve returned %v, want nil on clean shutdown", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return after context cancel")
	}
}

// TestServeReturnsErrorOnBindFailure is the regression guard for
// #API-LEAK-04 and #API-LEAK-1: when the server fails to bind at all, Serve's
// background goroutines must not remain parked forever waiting on a context
// that may never be cancelled, and the app collector must not start for a
// server that never started.
func TestServeReturnsErrorOnBindFailure(t *testing.T) {
	// Hold the port so the real Serve call fails to bind.
	blocker, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer blocker.Close()
	port := blocker.Addr().(*net.TCPAddr).Port

	hub := state.NewHub()
	_, ch, cancel := hub.Subscribe()
	defer cancel()
	// Context is deliberately never cancelled: if any Serve-owned goroutine
	// only exits via ctx.Done(), this test would leak it forever.
	ctx := t.Context()

	beforeWatchers := serveWatcherGoroutines()
	errCh := make(chan error, 1)
	go func() {
		errCh <- Serve(ctx, hub, Config{
			Port:    port,
			WebRoot: t.TempDir(),
			Apps:    "Grafana:3000",
		})
	}()
	select {
	case err = <-errCh:
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not return after bind failure")
	}
	if err == nil {
		t.Fatal("Serve returned nil error, want a bind failure")
	}

	select {
	case msg := <-ch:
		t.Fatalf("app collector published after bind failure: %q", msg)
	case <-time.After(100 * time.Millisecond):
	}

	// Give the goroutine scheduler a moment to actually unwind the deferred
	// close(serveDone) path before sampling.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if serveWatcherGoroutines() <= beforeWatchers {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Errorf("shutdown watcher goroutine remained after bind failure")
}

func serveWatcherGoroutines() int {
	buf := make([]byte, 1<<20)
	n := runtime.Stack(buf, true)
	return strings.Count(string(buf[:n]), "github.com/jnovack/cloudkey/internal/api.Serve.func1")
}

// TestServeClosesSlowHeaderClient is the regression guard for #API-SEC-03: a
// client that opens a connection and never finishes sending request headers
// (Slowloris) must be dropped by ReadHeaderTimeout rather than pinning a
// goroutine/fd forever. readHeaderTimeout is shrunk for the test so this
// doesn't wait out the real 10s default.
func TestServeClosesSlowHeaderClient(t *testing.T) {
	origHeader := readHeaderTimeout
	readHeaderTimeout = 100 * time.Millisecond
	defer func() { readHeaderTimeout = origHeader }()

	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("probe listen: %v", err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	_ = probe.Close()

	hub := state.NewHub()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() { _ = Serve(ctx, hub, Config{Port: port, WebRoot: t.TempDir()}) }()

	addr := "127.0.0.1:" + strconv.Itoa(port)
	var conn net.Conn
	for range 50 {
		conn, err = net.Dial("tcp", addr)
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Send a partial request line/header and never complete it — the server
	// must not wait forever for the rest.
	if _, err := conn.Write([]byte("GET / HTTP/1.1\r\nHost: localhost\r\n")); err != nil {
		t.Fatalf("write partial request: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		buf := make([]byte, 1)
		_, err := conn.Read(buf)
		done <- err
	}()
	select {
	case <-done:
		// Either a timeout response or a closed connection — both prove the
		// server didn't leave the connection open indefinitely.
	case <-time.After(2 * time.Second):
		t.Fatal("connection was not closed within 2s of a 100ms ReadHeaderTimeout")
	}
}

// TestServeSSEOutlivesReadHeaderTimeout is the regression guard for the
// carve-out in #API-SEC-03: ReadHeaderTimeout and IdleTimeout must not sever
// an established /events stream. Both are shrunk well below the time the
// stream is held open, so a future change that (mistakenly) wires ReadTimeout
// or WriteTimeout in their place — which would apply to the whole
// request/response, not just the idle header wait — would fail this test.
func TestServeSSEOutlivesReadHeaderTimeout(t *testing.T) {
	origHeader, origIdle := readHeaderTimeout, idleTimeout
	readHeaderTimeout = 50 * time.Millisecond
	idleTimeout = 50 * time.Millisecond
	defer func() { readHeaderTimeout, idleTimeout = origHeader, origIdle }()

	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("probe listen: %v", err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	_ = probe.Close()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "dashboard.html"), []byte("<html>ok</html>"), 0o600); err != nil {
		t.Fatalf("write dashboard: %v", err)
	}

	hub := state.NewHub()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() { _ = Serve(ctx, hub, Config{Port: port, WebRoot: dir}) }()

	base := "http://127.0.0.1:" + strconv.Itoa(port)
	var resp *http.Response
	for range 50 {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/events", nil)
		resp, err = http.DefaultClient.Do(req)
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("GET /events: %v", err)
	}
	defer resp.Body.Close()

	r := bufio.NewReader(resp.Body)
	nextData(t, r) // initial frame

	// Outlive both shrunk timeouts while the stream sits idle, then confirm a
	// later publish still arrives — proving the connection wasn't dropped.
	time.Sleep(300 * time.Millisecond)
	hub.PublishCPU(state.CPU{Pct: 42, Cores: 4, LoadAvg: 0.5})
	patch := nextData(t, r)
	if _, ok := patch["cpu"]; !ok {
		t.Fatalf("patch missing cpu key after outliving ReadHeaderTimeout/IdleTimeout: %v", patch)
	}
}

func TestStaticHandlerServesDashboardAtRoot(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "dashboard.html"), []byte("<html>dash</html>"), 0o600); err != nil {
		t.Fatalf("write dashboard: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "favicon.ico"), []byte("icon"), 0o600); err != nil {
		t.Fatalf("write sibling: %v", err)
	}

	srv := httptest.NewServer(staticHandler(dir))
	defer srv.Close()

	cases := []struct {
		name string
		path string
		want string
	}{
		{"root serves dashboard.html", "/", "<html>dash</html>"},
		{"sibling served verbatim", "/favicon.ico", "icon"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resp, err := http.Get(srv.URL + c.path)
			if err != nil {
				t.Fatalf("get %s: %v", c.path, err)
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			if string(body) != c.want {
				t.Errorf("GET %s = %q, want %q", c.path, body, c.want)
			}
		})
	}
}
