// Package resetbutton reads the physical front-panel reset button as a plain
// evdev key while the OS is running, classifying each completed press into a
// Band the caller acts on.
//
// What is known, measured on a live UCK-G2: at runtime the kernel exposes this
// button only through evdev (gpio-keys, Handlers=event1, no EV_REP), cloudkey is
// the sole holder of that device node, and GPIO 93 is claimed by the gpio-keys
// driver — so userspace cannot reach the line by any other route. Nothing else
// on the running system can observe the button.
//
// What is assumed: those observations only cover Linux. A hold path implemented
// below it — in the bootloader, a PMIC, or a separate microcontroller — would be
// invisible to all of them and cannot be ruled out from userspace. Both a
// documented hold-at-boot factory reset and an operator report of a destructive
// long hold at runtime fit that shape, so this package treats such a path as
// real: no band extends past six seconds. See classify in band.go for why that
// ceiling is load-bearing rather than arbitrary.
package resetbutton

import (
	"encoding/binary"
	"io"
	"math/bits"
	"os"
	"time"
)

// now is the clock this package measures press durations against. It is a var
// only so tests can drive a scripted clock instead of sleeping for real band
// durations; nothing in production reassigns it.
var now = time.Now

// device is this board's gpio-keys reset button. Confirmed via
// /sys/firmware/devicetree/base/gpio_keys/reset (label "reset", GPIO 93,
// active-low, linux,code 0x100) and /proc/bus/input/devices (gpio-keys ->
// event1); dmesg shows it as the only gpio-keys node. No other input
// device is expected on this hardware, so the event number is hardcoded
// the same way fbDevice and the LED names are elsewhere in this project.
const device = "/dev/input/event1"

const (
	evKey     = 1     // struct input_event.type for a key/button event
	btnCode   = 0x100 // BTN_0 - this board's device-tree linux,code for "reset"
	keyUp     = 0     // struct input_event.value on release
	keyDown   = 1     // struct input_event.value on press
	keyRepeat = 2     // struct input_event.value on autorepeat
)

// rawEvent holds the fields of struct input_event that Watch actually cares
// about. The kernel struct also leads with a struct timeval (tv_sec,
// tv_usec), but Watch never reads it — only its width matters, since it
// shifts where type/code/value start on the wire (see eventSize).
type rawEvent struct {
	Type, Code uint16
	Value      int32
}

// timevalFieldSize is the width of each of struct timeval's two fields
// (tv_sec, tv_usec) as the kernel lays out struct input_event: a plain
// `long`, which is 4 bytes on 32-bit architectures (this board's arm target)
// and 8 bytes on 64-bit ones. bits.UintSize tracks the same divide for the
// current GOARCH, so this stays correct if the build target ever changes.
const timevalFieldSize = bits.UintSize / 8

// eventSize is the on-the-wire size of struct input_event for the current
// GOARCH: two timeval fields, then type (2 bytes), code (2 bytes), and value
// (4 bytes) with no padding in between on either width. The kernel's evdev
// read() requires the read buffer size to match this exactly - a size
// mismatch (e.g. this project's earlier hardcoded 64-bit assumption running
// against a 32-bit kernel) fails the read with EINVAL rather than
// misparsing.
const eventSize = timevalFieldSize*2 + 8

// readEvent reads one struct input_event from r and extracts its
// type/code/value tail, skipping over the timeval fields at the front (see
// eventSize) since Watch never needs them.
func readEvent(r io.Reader) (rawEvent, error) {
	buf := make([]byte, eventSize)
	if _, err := io.ReadFull(r, buf); err != nil {
		return rawEvent{}, err
	}
	tail := buf[eventSize-8:]
	return rawEvent{
		Type:  binary.LittleEndian.Uint16(tail[0:2]),
		Code:  binary.LittleEndian.Uint16(tail[2:4]),
		Value: int32(binary.LittleEndian.Uint32(tail[4:8])),
	}, nil
}

// isPress reports whether e is the reset button's key-down transition, which
// starts the duration this package measures. Autorepeat (keyRepeat) is
// deliberately not a press: restarting the clock on every repeat would cap
// every hold at one repeat interval and collapse the bands into each other.
// This board reports no EV_REP so autorepeat should never arrive, but the guard
// costs nothing and the failure it prevents is silent.
func isPress(e rawEvent) bool {
	return e.Type == evKey && e.Code == btnCode && e.Value == keyDown
}

// isRelease reports whether e is the reset button's key-up transition - the
// transition that ends a press and triggers classification. An event from
// another key is not a release in the sense this package cares about.
func isRelease(e rawEvent) bool {
	return e.Type == evKey && e.Code == btnCode && e.Value == keyUp
}

// Watch opens the reset button's input device and calls onBand once for every
// completed press that lands in a real band. It blocks until reading the device
// fails (e.g. the device node disappears), and returns that error.
//
// onBand runs on Watch's goroutine, so a slow callback delays the next press.
// Callers that do anything more than signal should hand off to their own
// goroutine.
func Watch(onBand func(Band)) error {
	f, err := os.Open(device)
	if err != nil {
		return err
	}
	defer f.Close()

	return watchReader(f, onBand)
}

// watchReader is Watch's event loop, split out from the device open so it can
// be tested against a synthetic event stream without the hardware.
//
// The duration is measured between the key-down and key-up events rather than
// with timers running while the button is held, because nothing yet gives
// feedback mid-hold - the band is only needed once the press is over. Adding
// live feedback (LEDs that show which band you are currently in) is what forces
// the read into its own goroutine with band-boundary timers; until then that
// machinery would have no consumer.
//
// A release with no matching press is ignored rather than treated as a
// zero-length press: the button may already be held when the device is opened,
// and a phantom sub-debounce press is a confusing thing to log.
func watchReader(r io.Reader, onBand func(Band)) error {
	var down time.Time
	var held bool

	for {
		e, err := readEvent(r)
		if err != nil {
			return err
		}
		switch {
		case isPress(e):
			down, held = now(), true
		case isRelease(e) && held:
			held = false
			if b := classify(now().Sub(down)); b != BandNone {
				onBand(b)
			}
		}
	}
}
