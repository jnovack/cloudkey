package display

import (
	"path/filepath"
	"testing"
)

// TestNewReturnsErrorWhenFramebufferMissing guards the decision that a
// missing/unopenable /dev/fb0 — the expected failure on non-target hardware —
// is a returned error, not a panic. Reverting New to panic(err) makes this
// test fail: the entrypoint would print a raw Go stack trace instead of a
// structured zerolog line, and would skip its pidfile cleanup.
func TestNewReturnsErrorWhenFramebufferMissing(t *testing.T) {
	orig := fbDevice
	t.Cleanup(func() { fbDevice = orig })
	fbDevice = filepath.Join(t.TempDir(), "no-such-fb0")

	var (
		err       error
		panicked  bool
		panicVal  any
		callInner = func() {
			defer func() {
				if r := recover(); r != nil {
					panicked = true
					panicVal = r
				}
			}()
			// hub is nil: display-only, no web dashboard. New must fail before
			// touching LEDs or building any screen.
			err = New(CmdLineOpts{}, nil)
		}
	)
	callInner()

	if panicked {
		t.Fatalf("New panicked on framebuffer open failure (%v); it must return an error instead", panicVal)
	}
	if err == nil {
		t.Fatal("New returned nil error when the framebuffer device does not exist")
	}
}
