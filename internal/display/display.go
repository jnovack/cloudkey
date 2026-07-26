package display

import (
	"fmt"
	"image"
	"image/draw"
	"math/rand"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/jnovack/cloudkey/internal/buildversion"
	"github.com/jnovack/cloudkey/internal/images"
	"github.com/jnovack/cloudkey/internal/state"
	"github.com/jnovack/cloudkey/pkg/framebuffer"
	"github.com/jnovack/cloudkey/pkg/leds"
)

// fbDevice is the panel device openFramebuffer opens. It is a var rather than
// a const only so a test can point it at a nonexistent path and exercise New's
// open-failure path; nothing in production ever reassigns it.
var fbDevice = "/dev/fb0"

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

// hub is the shared state store the screen goroutines publish their raw
// readings into, so the same timer that redraws a screen also updates any
// connected web dashboard. It is set once by New and may be nil (the web
// server disabled, or a buildX unit test that never calls New) — the publish
// helpers below are all no-ops in that case, so screen goroutines can call
// them unconditionally. This deliberately avoids threading a hub parameter
// through every build func, which would break the func(CmdLineOpts) draw.Image
// contract registerScreen depends on.
var hub *state.Hub

// hubEnabled reports whether a hub is attached, so a screen goroutine can skip
// the extra work of gathering web-only data (a latency ping, a systemd
// connected-time lookup) when nothing consumes it.
func hubEnabled() bool { return hub != nil }

func publishHost(v state.Host)             { publishTo(func() { hub.PublishHost(v) }) }
func publishNet(v state.Net)               { publishTo(func() { hub.PublishNet(v) }) }
func publishCPU(v state.CPU)               { publishTo(func() { hub.PublishCPU(v) }) }
func publishMem(v state.Mem)               { publishTo(func() { hub.PublishMem(v) }) }
func publishDisk(key string, v state.Disk) { publishTo(func() { hub.PublishDisk(key, v) }) }
func publishTunnel(v state.Tunnel)         { publishTo(func() { hub.PublishTunnel(v) }) }

// publishTo runs fn only when a hub is attached, centralizing the nil check the
// six typed helpers above share.
func publishTo(fn func()) {
	if hub != nil {
		fn()
	}
}

// CmdLineOpts structure for the command line options
type CmdLineOpts struct {
	Delay                 float64
	BlankDelay            float64
	Reset                 bool
	Demo                  bool
	Version               bool
	Pidfile               string
	StealthMode           bool
	AutoSSHTunnel1Name    string
	AutoSSHTunnel1Service string
	AutoSSHTunnel2Name    string
	AutoSSHTunnel2Service string
	WireGuardName         string
	WireGuardIface        string
	WireGuardCmd          string
	Tailscale             bool
	TailscaleName         string
	TailscaleCmd          string
	HTTPPort              int
	WebRoot               string
	Apps                  string
}

// screenBuilder describes one screen in the carousel: a name for logging, a
// predicate deciding whether it's included for the current CLI options, and
// the function that allocates its image and starts whatever goroutines keep
// it updated.
type screenBuilder struct {
	name    string
	enabled func(CmdLineOpts) bool
	build   func(CmdLineOpts) draw.Image
}

// registry lists every known screen, in rotation order. A screen adds itself
// via registerScreen (see the init() in screens.go) — New() below never
// needs to change as screens are added or removed.
var registry []screenBuilder

func registerScreen(name string, enabled func(CmdLineOpts) bool, build func(CmdLineOpts) draw.Image) {
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

	ledsRunning()
}

// New opens the framebuffer and initializes the screens. Call it after any
// startup logging in main() — opening the framebuffer prints the panel
// resolution, and callers generally want their own banner to appear first.
//
// h is the shared state hub the screen goroutines publish into; pass nil to run
// display-only (no web dashboard). It is stored before any screen is built so a
// screen's first-frame publish reaches it.
//
// It returns an error only when the framebuffer device can't be opened — the
// expected failure on non-target hardware (no /dev/fb0, or no privileges).
// That is returned rather than panicked or log.Fatal'd here so the entrypoint
// reports it in the same structured form as every other startup failure and
// still runs its cleanup (clearing the pidfile); panicking printed a raw Go
// stack trace and skipped that cleanup entirely.
func New(opts CmdLineOpts, h *state.Hub) error {
	hub = h

	// Set before the first LED write or draw below, so a stealth boot never
	// lights the panel at all — not even briefly for the logo.
	stealthOn.Store(opts.StealthMode)

	if err := openFramebuffer(); err != nil {
		return fmt.Errorf("open framebuffer: %w", err)
	}

	if opts.Reset {
		clearScreen()
		return nil
	}

	myLeds = leds.LEDS{}
	// A stealth boot must actively darken the LEDs rather than merely skip
	// lighting them: they hold whatever state the previous run (or Shutdown)
	// left behind, so "don't touch them" leaves the panel lit.
	if stealthOn.Load() {
		ledsDark()
	} else {
		ledsBooting()
	}

	// Always clear, stealth or not — the panel keeps its last contents across a
	// restart, so skipping this would leave a stale frame lit in stealth.
	clearScreen()

	// The logo, version, and loader bar are front-panel theatre; a stealth boot
	// skips them outright rather than drawing them and hoping nobody looks.
	withPanel(func() {
		draw.Draw(fb, image.Rect(64, 4, 64+32, 4+32), images.Load("logo"), image.Point{}, draw.Src)
		center(fb, buildversion.Version, 40, 8, "lato-regular", false)
		animateBootLoader()
	})

	// Build every screen enabled for these opts, in registry order. Adding a
	// screen means writing its build func and calling registerScreen (see
	// screens.go's init()) — nothing here needs to change.
	//
	// Screens are built even in stealth mode: their collector goroutines are
	// what feed the state hub, and stealth darkens the panel without taking the
	// web dashboard down with it.
	screens = make([]draw.Image, 0, len(registry))
	for _, sb := range registry {
		if !sb.enabled(opts) {
			continue
		}
		log.Info().Str("screen", sb.name).Msg("building screen")
		screens = append(screens, sb.build(opts))
	}

	// Start the carousel!
	startFadeCarousel(opts.Delay, opts.BlankDelay)
	return nil
}

// Shutdown turns off the "running" blue LED and leaves white lit, so the
// panel still shows the box is powered even though the cloudkey service
// itself has stopped — a fully dark panel is indistinguishable from the
// device being off.
//
// In stealth mode that reasoning inverts: a dark panel is precisely what was
// asked for, and lighting white on the way out would undo it at the one moment
// nobody is watching to press the button again. Hence the gate.
func Shutdown() {
	withPanel(ledsBooting)
}
