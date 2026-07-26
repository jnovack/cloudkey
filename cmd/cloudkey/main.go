package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/tabalt/pidfile"

	"github.com/coreos/pkg/flagutil"
	"github.com/jnovack/cloudkey/internal/api"
	"github.com/jnovack/cloudkey/internal/buildversion"
	"github.com/jnovack/cloudkey/internal/display"
	_ "github.com/jnovack/cloudkey/internal/fonts"
	"github.com/jnovack/cloudkey/internal/state"
	"github.com/jnovack/cloudkey/pkg/resetbutton"
)

var (
	opts      display.CmdLineOpts
	configErr error
)

// serverShutdownWait bounds how long the signal handler waits for api.Serve to
// finish draining before exiting anyway. It is deliberately longer than the
// api package's own shutdownGrace so the drain gets its full budget.
const serverShutdownWait = 10 * time.Second

func main() {
	buildversion.Populate()

	log.Logger = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: "15:04:05"}).With().Timestamp().Logger()
	if configErr != nil {
		log.Fatal().Err(configErr).Msg("invalid CLOUDKEY environment configuration")
	}

	// Parse CLI flags last so they override the environment values applied in init().
	flag.Parse()

	log.Info().
		Str("version", buildversion.Current.Version).
		Str("build_rfc3339", buildversion.Current.BuildRFC3339).
		Str("revision", buildversion.Current.Revision).
		Msg("jnovack/cloudkey")

	if opts.Version {
		os.Exit(0)
	}

	if err := validateExternalCommands(opts); err != nil {
		log.Fatal().Err(err).Msg("invalid CLOUDKEY environment configuration")
	}

	pid, err := pidfile.Create(opts.Pidfile)
	if err != nil {
		log.Warn().Err(err).Str("pidfile", opts.Pidfile).Msg("failed to create pidfile")
		pid = nil
	}
	// Clear on every normal return, not just the signal path: --reset makes
	// display.New return early, and without this the process exits leaving a
	// pidfile naming a PID that no longer exists. Every path that calls
	// os.Exit — the signal handler, the dashboard server's log.Fatal, and the
	// log.Fatal below — clears the pidfile itself first, since os.Exit does not
	// run deferred functions; none of them double-clears with this.
	defer func() {
		if pid != nil {
			_ = pid.Clear()
		}
	}()

	// The reset button is the only physical control on the device and the only
	// way to leave stealth mode, so the watcher runs unconditionally — there is
	// deliberately no configuration that turns it off, because the setting to
	// re-enable it would be unreachable from a dark panel.
	//
	// Mapping bands to actions belongs here rather than in pkg/resetbutton,
	// which stays ignorant of LEDs, screens, and commands. Every classified band
	// is logged, not just the one that acts: it is how an operator discovers the
	// other bands exist, and the first thing to look at when a press seems to do
	// nothing (a hold that lands in a dead zone is supposed to do nothing).
	go func() {
		err := resetbutton.Watch(func(b resetbutton.Band) {
			log.Info().Str("band", b.String()).Msg("reset button pressed")
			if b == resetbutton.BandShortPress {
				display.ToggleStealth()
			}
		})
		log.Error().Err(err).Msg("reset button watcher exited")
	}()

	// hub carries live readings from the display's collector goroutines to any
	// connected web dashboard. It is created unconditionally and passed to
	// display.New; the HTTP server that consumes it starts only when a port is
	// configured, so a display-only deployment is unaffected.
	hub := state.NewHub()

	// serverCtx is cancelled by the signal handler to shut the HTTP server down
	// gracefully before the process exits. serverDone is closed once api.Serve
	// has actually returned, so the handler waits for that drain instead of
	// calling os.Exit out from under it — without the wait, cancelServer() is
	// immediately followed by process death and no client is ever closed cleanly.
	serverCtx, cancelServer := context.WithCancel(context.Background())
	serverDone := make(chan struct{})

	if opts.HTTPPort > 0 {
		go func() {
			defer close(serverDone)
			cfg := api.Config{Port: opts.HTTPPort, WebRoot: opts.WebRoot, Apps: opts.Apps}
			if err := api.Serve(serverCtx, hub, cfg); err != nil {
				// log.Fatal calls os.Exit, which skips main's deferred
				// pid.Clear — clear here explicitly, the way every other fatal
				// path in main does.
				if pid != nil {
					_ = pid.Clear()
				}
				log.Fatal().Err(err).Msg("dashboard http server failed")
			}
		}()
	} else {
		// No server to wait for; keep the handler's select from blocking.
		close(serverDone)
	}

	// Setup Service
	// https://fabianlee.org/2017/05/21/golang-running-a-go-binary-as-a-systemd-service-on-ubuntu-16-04/

	// Catch SIGINT/SIGTERM for a clean shutdown; leave other signals to default handling.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	go func() {
		s := <-sigs
		cancelServer()
		if !awaitServerShutdown(serverDone, serverShutdownWait) {
			log.Warn().Msg("dashboard http server did not stop in time")
		}
		display.Shutdown()
		log.Info().Str("signal", s.String()).Msg("received signal, stopping cloudkey service")
		if pid != nil {
			_ = pid.Clear()
		}
		os.Exit(0)
	}()

	if err := display.New(opts, hub); err != nil {
		if pid != nil {
			_ = pid.Clear()
		}
		log.Fatal().Err(err).Msg("display initialization failed")
	}
}

// awaitServerShutdown blocks until done is closed or timeout elapses,
// reporting which happened. It exists as a standalone function so the
// signal handler's wait-with-timeout logic is unit-testable without
// standing up a real HTTP server.
func awaitServerShutdown(done <-chan struct{}, timeout time.Duration) bool {
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

// validateExternalCommands checks that every external binary a requested
// screen depends on is actually runnable, so a missing `wg` or `tailscale`
// fails fast at startup as a configuration error instead of silently
// leaving that screen stuck on "disconnected" forever. It mirrors the
// wireguard/tailscale enabled predicates registered in
// internal/display/screens.go's init() — a screen enabled there needs its
// dependency checked here too.
//
// Demo mode never shells out to wg/tailscale (see buildWireGuard/
// buildTailscale), so it's exempt: someone previewing screens on a dev
// machine shouldn't need either binary installed.
func validateExternalCommands(opts display.CmdLineOpts) error {
	if opts.Demo {
		return nil
	}
	if opts.WireGuardIface != "" {
		if _, err := exec.LookPath(opts.WireGuardCmd); err != nil {
			return fmt.Errorf("wireguard screen enabled (CLOUDKEY_WIREGUARD_IFACE=%s) but %w", opts.WireGuardIface, err)
		}
	}
	if opts.Tailscale {
		if _, err := exec.LookPath(opts.TailscaleCmd); err != nil {
			return fmt.Errorf("tailscale screen enabled but %w", err)
		}
	}
	return nil
}

func init() {
	configErr = configureFlags(flag.CommandLine, &opts)
}

func configureFlags(fs *flag.FlagSet, opts *display.CmdLineOpts) error {
	fs.Float64Var(&opts.Delay, "delay", 5000, "delay in milliseconds each screen stays lit")
	fs.Float64Var(&opts.BlankDelay, "blank-delay", 3000, "delay in milliseconds screens stay blanked between screens")
	fs.BoolVar(&opts.Reset, "reset", false, "reset/clear the screen")
	fs.BoolVar(&opts.Demo, "demo", false, "use fake data for display only")
	fs.StringVar(&opts.Pidfile, "pidfile", "/var/run/cloudkey.pid", "pidfile")
	fs.BoolVar(&opts.Version, "version", false, "print version and exit")
	fs.BoolVar(&opts.StealthMode, "stealth-mode", false, "start with the front panel dark: no LEDs, no screens (a brief reset-button tap toggles it at runtime; not persisted across restarts)")
	fs.StringVar(&opts.AutoSSHTunnel1Name, "autossh-tunnel1-name", "", "label for the first autossh tunnel (empty disables the autossh screen)")
	fs.StringVar(&opts.AutoSSHTunnel1Service, "autossh-tunnel1-service", "", "systemd unit name to check for the first autossh tunnel's liveness (empty always shows down)")
	fs.StringVar(&opts.AutoSSHTunnel2Name, "autossh-tunnel2-name", "", "label for the second autossh tunnel (empty hides the second row)")
	fs.StringVar(&opts.AutoSSHTunnel2Service, "autossh-tunnel2-service", "", "systemd unit name to check for the second autossh tunnel's liveness (empty always shows down)")
	fs.StringVar(&opts.WireGuardName, "wireguard-name", "WireGuard", "display name for the WireGuard screen")
	fs.StringVar(&opts.WireGuardIface, "wireguard-iface", "", "WireGuard interface name to check, e.g. wg0 (empty disables the wireguard screen)")
	fs.StringVar(&opts.WireGuardCmd, "wg-cmd", "wg", "wg binary to run for WireGuard status checks; a bare name resolves via PATH")
	fs.BoolVar(&opts.Tailscale, "tailscale", false, "enable and display the Tailscale screen")
	fs.StringVar(&opts.TailscaleName, "tailscale-name", "TailScale", "display name for the Tailscale screen")
	fs.StringVar(&opts.TailscaleCmd, "tailscale-cmd", "tailscale", "tailscale binary to run for Tailscale status checks; a bare name resolves via PATH")
	fs.IntVar(&opts.HTTPPort, "http-port", 0, "TCP port for the web dashboard + SSE API; 0 disables it (binding 80 needs root or cap_net_bind_service)")
	fs.StringVar(&opts.WebRoot, "web-root", "/usr/share/cloudkey/website", "directory of dashboard static files served at /")
	fs.StringVar(&opts.Apps, "apps", "", "comma-separated local apps to show on the dashboard, each name:port; liveness comes from TCP LISTEN tables, e.g. \"Grafana:3000,Sonarr:8989\"")
	return flagutil.SetFlagsFromEnv(fs, "CLOUDKEY")
}
