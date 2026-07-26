package display

import (
	"image"
	"image/color"
	"testing"
	"time"
)

// resetStealthState restores the package-level stealth state after a test.
// These tests share process-wide state (stealthOn, stealthToggle, fb), so every
// one of them has to hand the package back exactly as it found it.
func resetStealthState(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		stealthOn.Store(false)
		drainStealthToggle()
	})
}

func drainStealthToggle() {
	for {
		select {
		case <-stealthToggle:
		default:
			return
		}
	}
}

// TestToggleStealthNeverBlocksWhenCarouselIsNotListening guards the default:
// case in ToggleStealth's send. This runs on the reset-button watcher's
// goroutine, which is also the goroutine that reads the next button event — a
// blocking send while the carousel is mid-fade would wedge the device's only
// physical control, and stealth mode is the one state you cannot escape any
// other way.
func TestToggleStealthNeverBlocksWhenCarouselIsNotListening(t *testing.T) {
	resetStealthState(t)
	drainStealthToggle()

	done := make(chan struct{})
	go func() {
		defer close(done)
		ToggleStealth()
		ToggleStealth()
		ToggleStealth()
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ToggleStealth blocked with no carousel draining the channel")
	}

	// Exactly one press should be pending: repeated taps coalesce rather than
	// queueing a second flip that would darken the panel again seconds later.
	select {
	case <-stealthToggle:
	default:
		t.Fatal("no toggle queued; the press was dropped entirely")
	}
	select {
	case <-stealthToggle:
		t.Fatal("more than one toggle queued; repeated presses must coalesce")
	default:
	}
}

// TestWithPanelSkipsFrontPanelWorkWhenStealthEngaged guards the single gate all
// LED writes and boot-time drawing pass through. Stealth means nothing lights
// up at all, so the gate either holds for every caller or the guarantee is
// worthless.
func TestWithPanelSkipsFrontPanelWorkWhenStealthEngaged(t *testing.T) {
	resetStealthState(t)

	cases := []struct {
		name    string
		stealth bool
		wantRun bool
	}{
		{"panel active runs the work", false, true},
		{"stealth engaged skips the work", true, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			stealthOn.Store(c.stealth)

			ran := false
			withPanel(func() { ran = true })

			if ran != c.wantRun {
				t.Errorf("withPanel ran = %v, want %v", ran, c.wantRun)
			}
		})
	}
}

// TestHoldOrToggleReturnsTrueOnPressAndFalseOnTimeout covers the carousel's
// interruptible hold. Without the press case the carousel would finish its
// current screen before noticing the button, adding up to -delay +
// -blank-delay milliseconds of lag to something the operator expects to be
// instant.
func TestHoldOrToggleReturnsTrueOnPressAndFalseOnTimeout(t *testing.T) {
	resetStealthState(t)

	cases := []struct {
		name    string
		pending bool
		hold    time.Duration
		want    bool
	}{
		{"pending press cuts the hold short", true, time.Minute, true},
		{"no press means the hold runs out", false, 10 * time.Millisecond, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			drainStealthToggle()
			if c.pending {
				ToggleStealth()
			}

			if got := holdOrToggle(c.hold); got != c.want {
				t.Errorf("holdOrToggle() = %v, want %v", got, c.want)
			}
		})
	}
}

// TestEnterStealthLeavesPanelBlackAndStealthEngaged is the behavioural guard
// for the whole feature: after the acknowledgment banner, every pixel must be
// genuinely off. fadeOut alone lands on the lowest alpha step rather than exact
// black, and "nearly off" is still clearly visible on an OLED in a dark room —
// dropping the clearScreen that follows it would pass a casual look and fail
// the actual requirement.
func TestEnterStealthLeavesPanelBlackAndStealthEngaged(t *testing.T) {
	resetStealthState(t)
	drainStealthToggle()

	prevFB := fb
	t.Cleanup(func() { fb = prevFB })
	fb = image.NewGray(image.Rect(0, 0, 160, 64))

	// Seed a lit panel so an implementation that never clears is caught.
	for y := 0; y < 64; y++ {
		for x := 0; x < 160; x++ {
			fb.Set(x, y, color.Gray{Y: 0xff})
		}
	}

	prevHold := stealthBannerHold
	t.Cleanup(func() { stealthBannerHold = prevHold })
	stealthBannerHold = time.Millisecond

	enterStealth()

	if !stealthOn.Load() {
		t.Error("stealthOn = false after enterStealth, want true")
	}
	for y := 0; y < 64; y++ {
		for x := 0; x < 160; x++ {
			if r, g, b, _ := fb.At(x, y).RGBA(); r|g|b != 0 {
				t.Fatalf("pixel (%d,%d) = (%d,%d,%d), want black", x, y, r, g, b)
			}
		}
	}
}

// TestExitStealthClearsStealthState is the counterpart: leaving stealth must
// publish the state change, or the carousel's next iteration parks again and
// the panel never comes back.
func TestExitStealthClearsStealthState(t *testing.T) {
	resetStealthState(t)

	stealthOn.Store(true)
	exitStealth()

	if stealthOn.Load() {
		t.Error("stealthOn = true after exitStealth, want false")
	}
}
