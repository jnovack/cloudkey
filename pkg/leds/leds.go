// Package leds controls the Cloud Key's status LEDs by writing the Linux
// sysfs led-class attributes under /sys/class/leds/<name>, so the display
// package can signal boot/running/reset state without shelling out. Writes
// are best-effort: on non-target hardware the sysfs paths are absent, so
// failures are logged (at debug) and never fatal.
package leds

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// https://scene-si.org/2016/07/19/building-your-own-build-status-indicator-with-golang-and-rpi3/

// LED is an individual led
type LED struct {
	name string
}

// filename returns the /sys path of the led
func (r LED) filename() string {
	return "/sys/class/leds/" + r.name
}

// read returns the contents of a sysfs attribute for the led.
func (r LED) read(where string) ([]byte, error) {
	filename := r.filename() + "/" + where
	return os.ReadFile(filename)
}

// write sets a sysfs attribute for the led. Writes are best-effort: the sysfs
// path may not exist on non-target hardware, so failures are logged, not fatal.
// Logged at debug because off-device this fires on literally every LED call —
// promoting it to warn would drown the log on any dev machine.
func (r LED) write(where, what string) LED {
	filename := r.filename() + "/" + where
	if err := os.WriteFile(filename, []byte(what), 0666); err != nil {
		log.Debug().Err(err).Str("file", filename).Msg("led write")
	}
	return r
}

// On turns on the led to maximum brightness, and clears the current running trigger (if any)
func (r LED) On() LED {
	r.write("trigger", "none")
	max, err := r.read("max_brightness")
	if err != nil {
		log.Debug().Err(err).Str("led", r.name).Msg("led read max_brightness")
		return r
	}
	return r.write("brightness", strings.TrimSuffix(string(max), "\n"))
}

// Off turns off the led, sets to zero brightness, and clears the current running trigger (if any)
func (r LED) Off() LED {
	r.write("trigger", "none")
	return r.write("brightness", "0")
}

// Brightness sets the brightness directly, and clears the current running trigger (if any)
func (r LED) Brightness(i int) LED {
	r.write("trigger", "none")
	return r.write("brightness", strconv.Itoa(i))
}

// parseBrightness parses a sysfs brightness attribute's raw contents (a
// decimal integer, typically newline-terminated) into an int. Shared by
// currentBrightness and FadeIn, the two call sites that read a brightness
// value out of sysfs, so the trim-then-parse behavior only needs testing
// once.
func parseBrightness(b []byte) (int, error) {
	return strconv.Atoi(strings.TrimSpace(string(b)))
}

// currentBrightness reads the led's live brightness value from sysfs, the
// starting point for a fade.
func (r LED) currentBrightness() int {
	b, err := r.read("brightness")
	if err != nil {
		log.Debug().Err(err).Str("led", r.name).Msg("led read brightness")
		return 0
	}
	v, err := parseBrightness(b)
	if err != nil {
		log.Debug().Err(err).Str("led", r.name).Msg("led parse brightness")
		return 0
	}
	return v
}

// fade steps brightness linearly from the led's current value to target over
// duration, blocking until done. Like the other setters it clears any
// running trigger (via Brightness).
func (r LED) fade(target int, duration time.Duration) LED {
	const steps = 15
	start := r.currentBrightness()
	interval := duration / steps
	for i := 1; i <= steps; i++ {
		r.Brightness(start + (target-start)*i/steps)
		time.Sleep(interval)
	}
	return r
}

// FadeIn ramps the led linearly from its current brightness up to maximum
// over duration, blocking until done.
func (r LED) FadeIn(duration time.Duration) LED {
	max, err := r.read("max_brightness")
	if err != nil {
		log.Debug().Err(err).Str("led", r.name).Msg("led read max_brightness")
		return r
	}
	target, err := parseBrightness(max)
	if err != nil {
		log.Debug().Err(err).Str("led", r.name).Msg("led parse max_brightness")
		return r
	}
	return r.fade(target, duration)
}

// FadeOut ramps the led linearly from its current brightness down to zero
// over duration, blocking until done.
func (r LED) FadeOut(duration time.Duration) LED {
	return r.fade(0, duration)
}

// Blink creates a blinking trigger action
func (r LED) Blink(i int, onTime int, offTime int) LED {
	r.write("trigger", "none")
	r.Brightness(i)
	r.write("trigger", "timer")
	r.write("delay_on", strconv.Itoa(onTime))
	r.write("delay_off", strconv.Itoa(offTime))
	return r
}

// LEDS is a factory for individual LEDs.
type LEDS struct{}

// LED returns a handle to the named led.
func (r LEDS) LED(name string) LED {
	return LED{name: name}
}
