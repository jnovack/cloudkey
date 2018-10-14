package display

import (
	"fmt"
	"image"
	"image/draw"
	"math/rand"
	"time"

	build "github.com/jnovack/go-version"

	"github.com/jnovack/cloudkey/images"
	"github.com/jnovack/cloudkey/src/framebuffer"
	"github.com/jnovack/cloudkey/src/leds"
)

var screens [2]draw.Image
var myLeds leds.LEDS
var fb draw.Image
var width, height int

// CmdLineOpts structure for the command line options
type CmdLineOpts struct {
	Delay   float64
	Reset   bool
	Demo    bool
	Version bool
	Pidfile string
}

func init() {
	myLeds = leds.LEDS{}
	myLeds.LED("blue").Off()
	myLeds.LED("white").On()

	// Framebuffer has global scope
	// therefore err must have local scope to prevent redefining
	var err error
	fb, err = framebuffer.Open("/dev/fb0")
	if err != nil {
		panic(err)
	}

	width = fb.Bounds().Max.X
	height = fb.Bounds().Max.Y

	fmt.Printf("Resolution: %dx%d pixels\n", width, height)
	clearScreen()

	// Set up additional screens
	for x := range screens {
		screens[x] = image.NewRGBA(fb.Bounds())
	}

	draw.Draw(fb, image.Rect(64, 4, 64+32, 4+32), images.Load("logo"), image.ZP, draw.Src)

	center(fb, build.Version, 80, 40, 8, "lato-regular")

	// Outline the loader line
	for i := 0; i < 100; i++ {
		fb.Set(30+i, 56, colors[3])
	}

	// Fill the loader line
	// This is just a delay right now, do your checks here!
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	for i := 0; i < 100; i++ {
		fb.Set(30+i, 56, colors[15])
		// mathmatically, the average sleep time is about half of the seed number
		time.Sleep(time.Duration(r.Intn(50)) * time.Millisecond)
	}

	myLeds.LED("blue").On()
	myLeds.LED("white").Off()
}

// New initializes the screens
func New(opts CmdLineOpts) {
	// Build the screens in the background
	buildNetwork(0, opts.Demo)
	buildSpeedTest(1, opts.Demo)

	// Start the carousel!
	startFadeCarousel(opts.Delay)
}

// Shutdown the LEDs
func Shutdown() {
	myLeds.LED("blue").Off()
	myLeds.LED("white").Off()
}

// Output the screen/image immediately to the framebuffer
func Output(i int) {
	screen := screens[i]
	draw.Draw(fb, fb.Bounds(), screen, image.ZP, draw.Over)
}
