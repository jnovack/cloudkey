package main

import (
	"flag"
	"framebuffer"
	"image"
	"image/draw"
	"log"
	"os"
	"os/signal"

	"github.com/jnovack/cloudkey/src/leds"

	_ "github.com/jnovack/cloudkey/fonts"
)

var fb draw.Image
var screens [2]draw.Image
var width, height int
var myLeds leds.LEDS

var (
	delay = flag.Float64("delay", 7500, "delay in milliseconds between screens")
	reset = flag.Bool("reset", false, "reset/clear the screen")
	demo  = flag.Bool("demo", false, "use fake data for display only")
)

func main() {
	clear()
	log.Printf("Resolution: %dx%d pixels\r\n", width, height)

	// Build the screens in the background
	// Slow screens should run first, display last :(
	go buildSpeedTest(1)
	go buildNetwork(0)

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

	// Framebuffer has global scope
	var err error
	fb, err = framebuffer.Open("/dev/fb0")
	if err != nil {
		panic(err)
	}

	width = fb.Bounds().Max.X
	height = fb.Bounds().Max.Y

	// Set up additional screens
	for x := range screens {
		screens[x] = image.NewRGBA(fb.Bounds())
	}

	// Setup Service
	// https://fabianlee.org/2017/05/21/golang-running-a-go-binary-as-a-systemd-service-on-ubuntu-16-04/
	log.Printf("Starting cloudkey service.")
	// setup signal catching
	sigs := make(chan os.Signal, 1)
	// catch all signals since not explicitly listing
	signal.Notify(sigs)
	// method invoked upon seeing signal
	go func() {
		s := <-sigs
		log.Printf("Received signal %s for cloudkey service", s)
		log.Printf("Stopping cloudkey service.")
		os.Exit(1)
	}()

	myLeds = leds.LEDS{}
	myLeds.LED("blue").Off()
	myLeds.LED("white").On()
}
