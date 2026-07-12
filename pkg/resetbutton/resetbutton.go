// Package resetbutton reads the physical front-panel reset button as a
// plain evdev key while the OS is running. It is completely independent of
// the board's hold-10-seconds-at-boot factory-reset path, which lives in
// the bootloader and runs before Linux — and this process — ever starts;
// nothing here can see or interfere with it.
package resetbutton

import (
	"encoding/binary"
	"io"
	"math/bits"
	"os"
)

// device is this board's gpio-keys reset button. Confirmed via
// /sys/firmware/devicetree/base/gpio_keys/reset (label "reset", GPIO 93,
// active-low, linux,code 0x100) and /proc/bus/input/devices (gpio-keys ->
// event1); dmesg shows it as the only gpio-keys node. No other input
// device is expected on this hardware, so the event number is hardcoded
// the same way fbDevice and the LED names are elsewhere in this project.
const device = "/dev/input/event1"

const (
	evKey   = 1     // struct input_event.type for a key/button event
	btnCode = 0x100 // BTN_0 - this board's device-tree linux,code for "reset"
	keyUp   = 0     // struct input_event.value on release (1 = press, 2 = autorepeat)
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

// isRelease reports whether e is the reset button's key-up transition - the
// only transition Watch acts on. A press (key-down) or autorepeat event on
// the same button, or any event from another key, is not a "press" in the
// sense this package cares about.
func isRelease(e rawEvent) bool {
	return e.Type == evKey && e.Code == btnCode && e.Value == keyUp
}

// Watch opens the reset button's input device and calls onPress once for
// every complete press-then-release while it's running - a single
// momentary press, nothing more. It blocks until reading the device fails
// (e.g. the device node disappears), and returns that error.
func Watch(onPress func()) error {
	f, err := os.Open(device)
	if err != nil {
		return err
	}
	defer f.Close()

	for {
		e, err := readEvent(f)
		if err != nil {
			return err
		}
		if isRelease(e) {
			onPress()
		}
	}
}
