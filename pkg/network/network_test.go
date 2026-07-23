package network

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestIsLANInterface(t *testing.T) {
	tests := []struct {
		name  string
		flags net.Flags
		want  bool
	}{
		{"up broadcast lan adapter", net.FlagUp | net.FlagBroadcast | net.FlagMulticast, true},
		{"down interface", net.FlagBroadcast, false},
		{"loopback", net.FlagUp | net.FlagLoopback, false},
		{"point-to-point wireguard/tailscale tunnel", net.FlagUp | net.FlagPointToPoint, false},
		{"up with no other flags", net.FlagUp, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			iface := net.Interface{Flags: tt.flags}
			if got := isLANInterface(iface); got != tt.want {
				t.Errorf("isLANInterface(%v) = %v, want %v", tt.flags, got, tt.want)
			}
		})
	}
}

func TestFirstIPv4(t *testing.T) {
	tests := []struct {
		name  string
		addrs []net.Addr
		want  string
	}{
		{
			name:  "ipnet ipv4",
			addrs: []net.Addr{&net.IPNet{IP: net.ParseIP("192.168.1.50")}},
			want:  "192.168.1.50",
		},
		{
			name:  "ipaddr ipv4",
			addrs: []net.Addr{&net.IPAddr{IP: net.ParseIP("10.0.0.5")}},
			want:  "10.0.0.5",
		},
		{
			name:  "skips loopback then returns ipv4",
			addrs: []net.Addr{&net.IPNet{IP: net.ParseIP("127.0.0.1")}, &net.IPNet{IP: net.ParseIP("192.168.1.50")}},
			want:  "192.168.1.50",
		},
		{
			name:  "skips ipv6 then returns ipv4",
			addrs: []net.Addr{&net.IPNet{IP: net.ParseIP("fe80::1")}, &net.IPNet{IP: net.ParseIP("192.168.1.50")}},
			want:  "192.168.1.50",
		},
		{
			name:  "no ipv4 present",
			addrs: []net.Addr{&net.IPNet{IP: net.ParseIP("fe80::1")}},
			want:  "",
		},
		{
			name:  "empty addrs",
			addrs: nil,
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := firstIPv4(tt.addrs); got != tt.want {
				t.Errorf("firstIPv4() = %q, want %q", got, tt.want)
			}
		})
	}
}

// withWANIPEndpoint points WANIP at srv for the duration of the test,
// restoring the real ipify endpoint on cleanup.
func withWANIPEndpoint(t *testing.T, srv *httptest.Server) {
	t.Helper()
	prev := wanIPEndpoint
	wanIPEndpoint = srv.URL
	t.Cleanup(func() { wanIPEndpoint = prev })
}

func TestWANIP(t *testing.T) {
	t.Run("returns the body when it is a valid IP", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("203.0.113.32"))
		}))
		defer srv.Close()
		withWANIPEndpoint(t, srv)

		got, err := WANIP(t.Context())
		if err != nil {
			t.Fatalf("WANIP() error = %v", err)
		}
		if got != "203.0.113.32" {
			t.Errorf("WANIP() = %q, want %q", got, "203.0.113.32")
		}
	})

	t.Run("errors on non-200 status", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		defer srv.Close()
		withWANIPEndpoint(t, srv)

		if _, err := WANIP(t.Context()); err == nil {
			t.Error("WANIP() error = nil, want error for 503 status")
		}
	})

	t.Run("errors when the body is not an IP", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("<html>rate limited</html>"))
		}))
		defer srv.Close()
		withWANIPEndpoint(t, srv)

		if _, err := WANIP(t.Context()); err == nil {
			t.Error("WANIP() error = nil, want error for non-IP body")
		}
	})

	// Regression test for #3: WANIP previously called go-ipify, which had no
	// way to bound or cancel its request — a dead WAN link left it blocked
	// indefinitely. A slow server plus a short context timeout now must make
	// WANIP return within the timeout instead of waiting for the server.
	t.Run("respects context timeout instead of blocking on a slow server", func(t *testing.T) {
		unblock := make(chan struct{})
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			<-unblock
			w.Write([]byte("203.0.113.32"))
		}))
		// srv.Close() waits for the in-flight handler to return, so unblock
		// must close first — deferred after srv.Close(), it runs first (LIFO).
		defer srv.Close()
		defer close(unblock)
		withWANIPEndpoint(t, srv)

		ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
		defer cancel()

		start := time.Now()
		_, err := WANIP(ctx)
		elapsed := time.Since(start)

		if err == nil {
			t.Fatal("WANIP() error = nil, want context deadline exceeded")
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("WANIP() error = %v, want wrapping context.DeadlineExceeded", err)
		}
		if elapsed > 2*time.Second {
			t.Errorf("WANIP() took %v, want it bounded by the context timeout", elapsed)
		}
	})
}
