package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"

	"github.com/jnovack/cloudkey/src/leds"

	journal "github.com/coreos/go-systemd/journal"
	_ "github.com/jnovack/cloudkey/fonts"
	_ "github.com/jnovack/cloudkey/screens"
)

var myLeds leds.LEDS

var tags = map[string]string{
	"SYSLOG_IDENTIFIER": "cloudkey",
}

var (
	delay = flag.Float64("delay", 7500, "delay in milliseconds between screens")
	reset = flag.Bool("reset", false, "reset/clear the screen")
	demo  = flag.Bool("demo", false, "use fake data for display only")
)

func j(message string) {
	err := journal.Send(message, journal.PriInfo, tags)
	if err != nil {
		log.Fatalf("%s", err)
	}
}

func main() {
	clear()

	// Build the screens in the background
	go buildNetwork(0)
	go buildSpeedTest(1)

	// Fire up the loader while the screens build
	load()

	// time.Sleep(2000 * time.Millisecond)
	// Start the carousel!
	myLeds.LED("blue").On()
	myLeds.LED("white").Off()
	startFadeCarousel()
	// display(1)
}

func init() {
	flag.Parse()

	// Setup Service
	// https://fabianlee.org/2017/05/21/golang-running-a-go-binary-as-a-systemd-service-on-ubuntu-16-04/
	j(fmt.Sprintf("Starting cloudkey service"))
	// setup signal catching
	sigs := make(chan os.Signal, 1)
	// catch all signals since not explicitly listing
	signal.Notify(sigs)
	// method invoked upon seeing signal
	go func() {
		s := <-sigs
		j(fmt.Sprintf("Received signal '%s', shutting down", s))
		j("Stopping cloudkey service")
		os.Exit(1)
	}()

	myLeds = leds.LEDS{}
	myLeds.LED("blue").Off()
	myLeds.LED("white").On()
}
