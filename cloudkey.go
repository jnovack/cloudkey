package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"

	"github.com/tabalt/pidfile"

	"github.com/coreos/pkg/flagutil"
	"github.com/jnovack/cloudkey/display"
	_ "github.com/jnovack/cloudkey/fonts"
)

var tags = map[string]string{
	"SYSLOG_IDENTIFIER": "cloudkey",
}

var opts display.CmdLineOpts

func main() {
	display.New(opts)
}

func init() {
	flag.Float64Var(&opts.Delay, "delay", 7500, "delay in milliseconds between screens")
	flag.BoolVar(&opts.Reset, "reset", false, "reset/clear the screen")
	flag.BoolVar(&opts.Demo, "demo", false, "use fake data for display only")
	flag.StringVar(&opts.Pidfile, "pidfile", "/var/run/zeromon.pid", "pidfile")
	flag.BoolVar(&opts.Version, "version", false, "print version and exit")
	flagutil.SetFlagsFromEnv(flag.CommandLine, "CLOUDKEY")

	if opts.Version {
		// already printed version
		os.Exit(0)
	}

	pid, _ := pidfile.Create(opts.Pidfile)

	// Setup Service
	// https://fabianlee.org/2017/05/21/golang-running-a-go-binary-as-a-systemd-service-on-ubuntu-16-04/
	fmt.Println("Starting cloudkey service")
	// setup signal catching
	sigs := make(chan os.Signal, 1)
	// catch all signals since not explicitly listing
	signal.Notify(sigs)
	// method invoked upon seeing signal
	go func() {
		s := <-sigs
		display.Shutdown()
		fmt.Printf("Received signal '%s', shutting down\n", s)
		fmt.Println("Stopping cloudkey service")
		_ = pid.Clear()
		os.Exit(1)
	}()
}
