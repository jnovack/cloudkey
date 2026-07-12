package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	build "github.com/jnovack/go-version"

	"github.com/tabalt/pidfile"

	"github.com/coreos/pkg/flagutil"
	"github.com/jnovack/cloudkey/internal/display"
	_ "github.com/jnovack/cloudkey/internal/fonts"
)

var opts display.CmdLineOpts

func main() {
	// Parse CLI flags last so they override the environment values applied in init().
	flag.Parse()

	if opts.Version {
		fmt.Printf("cloudkey %s\n", build.Version)
		os.Exit(0)
	}

	pid, _ := pidfile.Create(opts.Pidfile)

	// Setup Service
	// https://fabianlee.org/2017/05/21/golang-running-a-go-binary-as-a-systemd-service-on-ubuntu-16-04/
	fmt.Println("Starting cloudkey service")

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

func init() {
	flag.Float64Var(&opts.Delay, "delay", 4000, "delay in milliseconds each screen stays lit")
	flag.Float64Var(&opts.BlankDelay, "blank-delay", 3000, "delay in milliseconds screens stay blanked between screens")
	flag.BoolVar(&opts.Reset, "reset", false, "reset/clear the screen")
	flag.BoolVar(&opts.Demo, "demo", false, "use fake data for display only")
	flag.BoolVar(&opts.SpeedTest, "speedtest", false, "enable and display the speedtest screen")
	flag.StringVar(&opts.Pidfile, "pidfile", "/var/run/zeromon.pid", "pidfile")
	flag.BoolVar(&opts.Version, "version", false, "print version and exit")
	flagutil.SetFlagsFromEnv(flag.CommandLine, "CLOUDKEY")
}
