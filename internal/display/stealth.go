package display

import (
	"image"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"
)

// stealthOn is the live stealth state: true means the front panel produces no
// output at all - no lit pixels, no lit LEDs.
//
// Only the carousel goroutine writes it, because only that goroutine owns the
// transitions (see enterStealth/exitStealth). It is read from other goroutines
// (Shutdown runs on the signal handler's), which is why it is atomic rather
// than a plain bool guarded by convention.
var stealthOn atomic.Bool

// stealthToggle carries a button press from the reset-button watcher goroutine
// to the carousel goroutine, which is the only goroutine allowed to touch the
// framebuffer. The watcher must never draw; it can only ask.
//
// Buffered to 1 and sent to without blocking (see ToggleStealth). Capacity 1
// makes a double-tap collapse into a single flip, which is what an impatient
// operator means by it - queueing the second press would flip the panel back
// again seconds later for no visible reason.
var stealthToggle = make(chan struct{}, 1)

// stealthBannerHold is how long "Stealth Mode / ENGAGED" stays lit before the
// panel goes dark, long enough to read and confirm the press registered. It is
// a var rather than a const only so tests can shrink it instead of sleeping for
// real; nothing in production reassigns it.
var stealthBannerHold = 5 * time.Second

// ToggleStealth asks the panel to flip stealth mode. It is safe to call from
// any goroutine and never blocks.
//
// The send must stay non-blocking: this runs on the reset-button watcher's
// goroutine, and that goroutine is also what reads the next button event. If it
// blocked here - because the carousel is mid-fade and not yet selecting on the
// channel - the device would stop responding to its only physical control until
// the panel happened to catch up.
func ToggleStealth() {
	select {
	case stealthToggle <- struct{}{}:
	default:
		log.Debug().Msg("stealth toggle already pending, coalescing press")
	}
}

// withPanel runs fn only when the front panel is active, and is the single gate
// every piece of front-panel output goes through - LED writes and boot-time
// drawing alike. Mirrors publishTo's shape in display.go.
//
// Keeping it a lone gate is the point: stealth means *nothing* lights up, and
// that guarantee is only checkable if there is one place to check. Re-testing
// stealthOn inline at a call site instead is how a stray LED write survives the
// next refactor and quietly lights the panel in a dark room.
func withPanel(fn func()) {
	if !stealthOn.Load() {
		fn()
	}
}

// The three LED states this device has. The blue/white pair reads as one status
// indicator to anyone looking at the front of the box, so they are always set
// together as a named state - lighting one without settling the other is what
// produces the ambiguous "both lit" and "both dark by accident" combinations.
func ledsDark()    { myLeds.LED("blue").Off(); myLeds.LED("white").Off() }
func ledsBooting() { myLeds.LED("blue").Off(); myLeds.LED("white").On() }
func ledsRunning() { myLeds.LED("blue").On(); myLeds.LED("white").Off() }

// enterStealth acknowledges the press with a banner, then darkens the panel
// completely. It draws, so it must only ever be called from the carousel
// goroutine.
//
// The banner is shown *before* stealth engages rather than after, so the
// acknowledgment is not suppressed by the very mode it is announcing.
func enterStealth() {
	banner := image.NewGray(fb.Bounds())
	drawStealth(banner)

	fadeOut()
	fadeIn(banner)
	time.Sleep(stealthBannerHold)
	fadeOut()

	// fadeOut lands on the last alpha step, not necessarily on exact black, and
	// "nearly off" is still visible on an OLED in a dark room.
	clearScreen()

	stealthOn.Store(true)

	// Deliberately not via withPanel: this call *is* the darkening, and the gate
	// it would pass through is already closed by the line above.
	ledsDark()
}

// exitStealth restores the running state and hands control back to the
// carousel.
//
// There is no "disengaged" banner on purpose. The panel lighting back up is
// itself unambiguous acknowledgment, and a screen announcing that screens are
// working again is noise. The asymmetry with enterStealth is the design, not an
// omission to be tidied up later.
func exitStealth() {
	stealthOn.Store(false)
	ledsRunning()
}
