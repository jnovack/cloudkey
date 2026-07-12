package display

import (
	"image"
	"image/draw"
	"math/rand"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/jnovack/cloudkey/internal/buildversion"
	"github.com/jnovack/cloudkey/internal/images"
	"github.com/jnovack/cloudkey/pkg/framebuffer"
	"github.com/jnovack/cloudkey/pkg/leds"
)

const fbDevice = "/dev/fb0"

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

// openFramebuffer opens the panel device and logs its resolution. Hardware
// bring-up lives here (called from New()) rather than in a package init(),
// which Go always runs before main() starts — that ordering used to print
// the resolution line before main() had a chance to log its own startup
// banner, regardless of statement order inside main().
func openFramebuffer() error {
	var err error
	fb, err = framebuffer.Open(fbDevice)
	if err != nil {
		return err
	}

	width = fb.Bounds().Max.X
	height = fb.Bounds().Max.Y

	log.Info().Str("device", fbDevice).Int("width", width).Int("height", height).Msg("resolution")
	return nil
}

// animateBootLoader draws and fills the boot loader bar before signaling
// ready via the status LEDs. This is purely a visual delay (no real checks
// behind it) so it runs late in New(), after the boot screen is already
// drawn.
func animateBootLoader() {
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

// New opens the framebuffer and initializes the screens. Call it after any
// startup logging in main() — opening the framebuffer prints the panel
// resolution, and callers generally want their own banner to appear first.
func New(opts CmdLineOpts) {
	if err := openFramebuffer(); err != nil {
		panic(err)
	}

	if opts.Reset {
		clearScreen()
		return
	}

	myLeds = leds.LEDS{}
	myLeds.LED("blue").Off()
	myLeds.LED("white").On()

	clearScreen()
	draw.Draw(fb, image.Rect(64, 4, 64+32, 4+32), images.Load("logo"), image.Point{}, draw.Src)
	center(fb, buildversion.Version, 40, 8, "lato-regular", false)

	animateBootLoader()

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
	draw.Draw(fb, fb.Bounds(), screen, image.Point{}, draw.Over)
}
