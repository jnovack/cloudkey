// Package api serves the web dashboard: the static single-page app under a
// configurable web root and a Server-Sent Events stream at /events fed by the
// shared state hub. It is optional and off by default — main starts it only
// when a port is configured — so existing display-only deployments are
// unaffected.
package api

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/jnovack/cloudkey/internal/state"
)

// keepaliveInterval is how often an idle SSE connection is sent a comment line.
// It keeps intermediaries (and the browser's EventSource) from treating a quiet
// stream as dead between the dashboard's slow-cadence collector ticks.
const keepaliveInterval = 20 * time.Second

// shutdownGrace bounds how long Serve waits for in-flight requests (chiefly
// long-lived SSE connections) to drain on shutdown before giving up.
const shutdownGrace = 5 * time.Second

// readHeaderTimeout and idleTimeout bound a client's request head and idle
// keep-alive time, so a connection that dribbles headers indefinitely
// (Slowloris) can't pin a goroutine and fd forever — this device is
// memory-constrained and --http-port's help text contemplates binding port
// 80, which faces the whole LAN. They are package-level vars, not consts, so
// tests can shrink them instead of waiting out a real 10s timeout.
//
// ReadTimeout and WriteTimeout are deliberately left unset on the server
// below: /events is a long-lived SSE stream (see keepaliveInterval), and
// either one would sever every dashboard client on a fixed interval
// regardless of health.
var (
	readHeaderTimeout = 10 * time.Second
	idleTimeout       = 120 * time.Second
)

// Config is the api server's runtime configuration, mapped from CLI/env by
// main. It is separate from the display package's options so api depends only
// on the state hub, not on display.
type Config struct {
	Port    int    // TCP port to listen on; caller only starts Serve when > 0
	WebRoot string // directory served at / (dashboard.html and any siblings)
	Apps    string // raw CLOUDKEY_APPS value: "name:port,name:port"
}

// Serve runs the dashboard HTTP server until ctx is cancelled, then shuts it
// down gracefully. It also starts the app-liveness collector for any apps
// parsed from cfg.Apps. It returns nil on a clean shutdown and a non-nil error
// only if the listener could not start.
func Serve(ctx context.Context, hub *state.Hub, cfg Config) error {
	apps := parseApps(cfg.Apps)

	mux := http.NewServeMux()
	mux.HandleFunc("/events", sseHandler(hub))
	mux.Handle("/", staticHandler(cfg.WebRoot))

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           mux,
		ReadHeaderTimeout: readHeaderTimeout,
		IdleTimeout:       idleTimeout,
	}

	ln, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		return fmt.Errorf("dashboard http server: %w", err)
	}

	serveCtx, stopServeWork := context.WithCancel(ctx)
	defer stopServeWork()
	if len(apps) > 0 {
		go collectApps(serveCtx, hub, apps)
	}

	// Shut down when the parent context is cancelled. Shutdown unblocks Serve
	// below with http.ErrServerClosed. serveDone releases this goroutine when
	// Serve returns for any other reason, so a server that stopped doesn't leave
	// a watcher parked for the life of the process.
	serveDone := make(chan struct{})
	defer close(serveDone)
	go func() {
		select {
		case <-serveDone:
			return
		case <-ctx.Done():
		}
		shutCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
		defer cancel()
		if err := srv.Shutdown(shutCtx); err != nil {
			log.Warn().Err(err).Msg("http server shutdown")
		}
	}()

	log.Info().Int("port", cfg.Port).Str("web_root", cfg.WebRoot).Msg("starting dashboard http server")
	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("dashboard http server: %w", err)
	}
	return nil
}

// staticHandler serves the dashboard's files from webRoot, mapping the bare
// "/" to dashboard.html (the whole dashboard: one self-contained file of HTML,
// CSS, and JS) rather than a directory listing. Every other path is served
// verbatim, so sibling assets can be dropped in without touching this code.
func staticHandler(webRoot string) http.Handler {
	fs := http.FileServer(http.Dir(webRoot))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.ServeFile(w, r, filepath.Join(webRoot, "dashboard.html"))
			return
		}
		fs.ServeHTTP(w, r)
	})
}

// sseHandler streams state updates to one browser. It writes the hub's full
// current snapshot as the first event so the client starts from complete state,
// then forwards each per-slice patch the hub broadcasts. A periodic keepalive
// comment holds the connection open through quiet periods, and the handler
// returns (unsubscribing) as soon as the client disconnects.
func sseHandler(hub *state.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}

		h := w.Header()
		h.Set("Content-Type", "text/event-stream")
		h.Set("Cache-Control", "no-cache")
		h.Set("Connection", "keep-alive")
		// Defeat proxy response buffering (e.g. nginx) so events aren't held
		// back; harmless when no such proxy is present.
		h.Set("X-Accel-Buffering", "no")

		initial, ch, cancel := hub.Subscribe()
		defer cancel()

		if _, err := fmt.Fprintf(w, "data: %s\n\n", initial); err != nil {
			return
		}
		flusher.Flush()

		keepalive := time.NewTicker(keepaliveInterval)
		defer keepalive.Stop()

		ctx := r.Context()
		for {
			select {
			case <-ctx.Done():
				return
			case msg := <-ch:
				if _, err := fmt.Fprintf(w, "data: %s\n\n", msg); err != nil {
					return
				}
				flusher.Flush()
			case <-keepalive.C:
				if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
					return
				}
				flusher.Flush()
			}
		}
	}
}
