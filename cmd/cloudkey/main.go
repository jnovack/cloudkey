package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"sync/atomic"
	"syscall"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/tabalt/pidfile"

	"github.com/coreos/pkg/flagutil"
	"github.com/jnovack/cloudkey/internal/buildversion"
	"github.com/jnovack/cloudkey/internal/display"
	_ "github.com/jnovack/cloudkey/internal/fonts"
	"github.com/jnovack/cloudkey/pkg/resetbutton"
)

var (
	opts      display.CmdLineOpts
	configErr error
)

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

	if opts.ResetButtonCmd != "" {
		go func() {
			err := resetbutton.Watch(func() {
				display.BlinkResetAck()
				runResetButtonCmd(opts.ResetButtonCmd)
			})
			log.Error().Err(err).Msg("reset button watcher exited")
		}()
	}

	// Setup Service
	// https://fabianlee.org/2017/05/21/golang-running-a-go-binary-as-a-systemd-service-on-ubuntu-16-04/

	// Catch SIGINT/SIGTERM for a clean shutdown; leave other signals to default handling.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	go func() {
		s := <-sigs
		display.Shutdown()
		fmt.Printf("Received signal '%s', shutting down\n", s)
		fmt.Println("Stopping cloudkey service")
		if pid != nil {
			_ = pid.Clear()
		}
		os.Exit(0)
	}()

	display.New(opts)
}

// resetButtonBusy is 1 while the configured reset-button command is
// running, 0 otherwise.
var resetButtonBusy int32

// runResetButtonCmd runs the configured reset-button command in the
// background. A press that arrives while a previous run is still in flight
// is dropped rather than queued or overlapped - a slow or stuck command
// (a network call, say) should not pile up concurrent runs just because
// someone tapped the button more than once.
func runResetButtonCmd(cmdStr string) {
	if !atomic.CompareAndSwapInt32(&resetButtonBusy, 0, 1) {
		log.Warn().Msg("reset button pressed again while previous command is still running, ignoring")
		return
	}
	log.Warn().Str("cmd", cmdStr).Msg("reset button pressed")
	go func() {
		defer atomic.StoreInt32(&resetButtonBusy, 0)
		out, err := exec.Command("/bin/sh", "-c", cmdStr).CombinedOutput()
		if err != nil {
			log.Error().Err(err).Str("output", string(out)).Msg("reset button command failed")
			return
		}
		log.Info().Str("output", string(out)).Msg("reset button command finished")
	}()
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
	fs.BoolVar(&opts.SpeedTest, "speedtest", false, "enable and display the speedtest screen")
	fs.StringVar(&opts.Pidfile, "pidfile", "/var/run/cloudkey.pid", "pidfile")
	fs.BoolVar(&opts.Version, "version", false, "print version and exit")
	fs.StringVar(&opts.ResetButtonCmd, "reset-button-cmd", "", "shell command to run on a single physical reset-button press (empty disables)")
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
	return flagutil.SetFlagsFromEnv(fs, "CLOUDKEY")
}
