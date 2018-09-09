package leds

import (
	"fmt"
	"io/ioutil"
	"log"
	"strings"
)

// https://scene-si.org/2016/07/19/building-your-own-build-status-indicator-with-golang-and-rpi3/

// LED is an individual led
type LED struct {
	name string
}

// Filename displays the /sys path of the led
func (r LED) filename() string {
	return "/sys/class/leds/" + r.name
}

func (r LED) read(where string) []byte {
	filename := r.filename() + "/" + where
	content, err := ioutil.ReadFile(filename)
	if err != nil {
		log.Fatal(err)
	}
	return content
}

func (r LED) write(where, what string) LED {
	filename := r.filename() + "/" + where
	log.Printf("writing '%s' into '%s'", what, filename)
	ioutil.WriteFile(filename, []byte(what), 0666)
	return r
}

// On turns on the led to maximum brightness, and clears the current running trigger (if any)
func (r LED) On() LED {
	r.write("trigger", "none")
	max := strings.TrimSuffix(string(r.read("max_brightness")), "\n")
	return r.write("brightness", max)
}

// Off turns off the led, sets to zero brightness, and clears the current running trigger (if any)
func (r LED) Off() LED {
	r.write("trigger", "none")
	return r.write("brightness", "0")
}

// Brightness sets the brightness directly, and clears the current running trigger (if any)
func (r LED) Brightness(i int) LED {
	return r
}

// Blink creates a blinking trigger action
func (r LED) Blink(i int, onTime int, offTime int) LED {
	return r
}

type LEDS struct{}

// LED Set an LED
func (r LEDS) LED(name string) LED {
	var err error
	led := LED{name: name}
	if err != nil {
		fmt.Println(err)
	}
	return led
}
