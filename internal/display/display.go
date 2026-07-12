package display

import (
	"image"
	"image/draw"
	"math/rand"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/jnovack/cloudkey/internal/buildversion"
	"github.com/jnovack/cloudkey/internal/images"
	"github.com/jnovack/cloudkey/pkg/framebuffer"
	"github.com/jnovack/cloudkey/pkg/leds"
)

const fbDevice = "/dev/fb0"

// screens holds one image per registered screen. Each build* func's
// background goroutine(s) redraw their screen's pixels in place (see
// screens.go), while startFadeCarousel concurrently reads those same pixels
// via fadeIn/fadeStep (functions.go). screenMu is the lock that makes that
// safe: every redraw of an already-returned screen and every fadeStep read
// of one must hold it. (The one-time initial draw inside each build* func
// does not need it — it happens before the image is reachable from any other
// goroutine.)
var screens []draw.Image
var screenMu sync.Mutex
var myLeds leds.LEDS
var fb draw.Image
var width, height int

// CmdLineOpts structure for the command line options
type CmdLineOpts struct {
	Delay          float64
	BlankDelay     float64
	Reset          bool
	Demo           bool
	SpeedTest      bool
	Version        bool
	Pidfile        string
	ResetButtonCmd string
}

// screenBuilder describes one screen in the carousel: a name for logging, a
// predicate deciding whether it's included for the current CLI options, and
// the function that allocates its image and starts whatever goroutines keep
// it updated.
type screenBuilder struct {
	name    string
	enabled func(CmdLineOpts) bool
	build   func(demo bool) draw.Image
}

// registry lists every known screen, in rotation order. A screen adds itself
// via registerScreen (see the init() in screens.go) — New() below never
// needs to change as screens are added or removed.
var registry []screenBuilder

func registerScreen(name string, enabled func(CmdLineOpts) bool, build func(bool) draw.Image) {
	registry = append(registry, screenBuilder{name: name, enabled: enabled, build: build})
}

func alwaysEnabled(CmdLineOpts) bool { return true }

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

	// Build every screen enabled for these opts, in registry order. Adding a
	// screen means writing its build func and calling registerScreen (see
	// screens.go's init()) — nothing here needs to change.
	screens = make([]draw.Image, 0, len(registry))
	for _, sb := range registry {
		if !sb.enabled(opts) {
			continue
		}
		log.Info().Str("screen", sb.name).Msg("building screen")
		screens = append(screens, sb.build(opts.Demo))
	}

	// Start the carousel!
	startFadeCarousel(opts.Delay, opts.BlankDelay)
}

// Shutdown the LEDs
func Shutdown() {
	myLeds.LED("blue").Off()
	myLeds.LED("white").Off()
}

// BlinkResetAck acknowledges a physical reset-button press by swapping the
// status indicator from its steady blue to a single white pulse (300ms
// fade up, 300ms fade down), then back to blue — the steady running state
// animateBootLoader leaves it in once boot completes. To the user the
// blue/white LEDs read as one status indicator, so the blue "ready" light
// must actually go dark while white pulses, not just sit lit underneath it.
func BlinkResetAck() {
	white := myLeds.LED("white")
	myLeds.LED("blue").Off()
	white.FadeIn(300 * time.Millisecond)
	white.FadeOut(300 * time.Millisecond)
	myLeds.LED("blue").On()
}

// Output the screen/image immediately to the framebuffer
func Output(i int) {
	screen := screens[i]
	draw.Draw(fb, fb.Bounds(), screen, image.Point{}, draw.Over)
}
