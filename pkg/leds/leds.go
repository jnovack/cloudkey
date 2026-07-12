package leds

import (
	"log"
	"os"
	"strconv"
	"strings"
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
func (r LED) write(where, what string) LED {
	filename := r.filename() + "/" + where
	if err := os.WriteFile(filename, []byte(what), 0666); err != nil {
		log.Printf("led write %s: %v", filename, err)
	}
	return r
}

// On turns on the led to maximum brightness, and clears the current running trigger (if any)
func (r LED) On() LED {
	r.write("trigger", "none")
	max, err := r.read("max_brightness")
	if err != nil {
		log.Printf("led read max_brightness: %v", err)
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
