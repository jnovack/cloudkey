package display

import (
	"fmt"
	"image"
	"image/draw"
	"math/rand"
	"testing"
	"time"

	build "github.com/jnovack/go-version"

	"github.com/jnovack/cloudkey/internal/images"
	"github.com/jnovack/cloudkey/pkg/framebuffer"
	"github.com/jnovack/cloudkey/pkg/leds"
)

var screens []draw.Image
var myLeds leds.LEDS
var fb draw.Image
var width, height int

// CmdLineOpts structure for the command line options
type CmdLineOpts struct {
	Delay      float64
	BlankDelay float64
	Reset      bool
	Demo       bool
	SpeedTest  bool
	Version    bool
	Pidfile    string
}

func init() {
	// Hardware bring-up (framebuffer device, boot animation, status LEDs)
	// only makes sense outside test binaries — unit tests exercise drawing
	// functions directly against in-memory images, never the real panel.
	if testing.Testing() {
		return
	}

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

	draw.Draw(fb, image.Rect(64, 4, 64+32, 4+32), images.Load("logo"), image.ZP, draw.Src)

	center(fb, build.Version, 40, 8, "lato-regular", false)

	// Outline the loader line
	for i := 0; i < 100; i++ {
		fb.Set(30+i, 56, colors[3])
	}

	// Fill the loader line — add startup checks here before marking ready.
	// Currently this is only a randomized delay for visual effect.
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
	if opts.Reset {
		clearScreen()
		return
	}

	// Allocate the screens here so their count lives next to the builders below.
	numScreens := 2
	if opts.SpeedTest {
		numScreens = 3
	}
	screens = make([]draw.Image, 0, numScreens)
	screens = append(screens, image.NewRGBA(fb.Bounds())) // index 0: local network
	screens = append(screens, image.NewRGBA(fb.Bounds())) // index 1: internet/time

	// Build the screens in the background
	buildLocal(0, opts.Demo)
	buildRemote(1, opts.Demo)

	if opts.SpeedTest {
		screens = append(screens, image.NewRGBA(fb.Bounds())) // index 2: speed test
		buildSpeedTest(2, opts.Demo)
	}

	// Start the carousel!
	startFadeCarousel(opts.Delay, opts.BlankDelay)
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
