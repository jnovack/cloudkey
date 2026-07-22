// Package images decodes the display's compiled-in PNG icons once at init
// and serves them by name to internal/display's redraw path.
package images

import (
	"encoding/base64"
	"image"
	"image/png"
	"strings"
)

var assets map[string]string

// decoded holds every registered image, decoded once at init. Images are immutable
// compiled-in constants and Load is called from the redraw path (twelve call sites
// in internal/display, several per screen refresh), so decoding per call repeated a
// base64+PNG decode forever for no benefit.
var decoded map[string]image.Image

// Load returns the named image from the embedded asset registry, or nil if no such
// name is registered. Decoding happens at init, so a corrupt embedded asset panics
// at startup — where it is a genuine programmer error — rather than terminating a
// long-running daemon from inside a redraw goroutine.
func Load(name string) image.Image {
	return decoded[name]
}

func init() {
	assets = make(map[string]string)

	// Image data is compiled in as Go constant strings (see data.go).
	// Register each asset name here so Load can find it by name.
	assets["check"] = check
	assets["clock"] = clock
	assets["dockerOff"] = dockerOff
	assets["dockerOn"] = dockerOn
	assets["cpu"] = cpu
	assets["download"] = download
	assets["downloadIdle"] = downloadIdle
	assets["hardDrive"] = hardDrive
	assets["hdd"] = hdd
	assets["host"] = host
	assets["internet"] = internet
	assets["logo"] = logo
	assets["memory"] = memory
	assets["network"] = network
	assets["noEntry"] = noEntry
	assets["sdcard"] = sdcard
	assets["upload"] = upload
	assets["uploadIdle"] = uploadIdle
	assets["warning"] = warning

	decoded = make(map[string]image.Image, len(assets))
	for name, data := range assets {
		img, err := png.Decode(base64.NewDecoder(base64.StdEncoding, strings.NewReader(data)))
		if err != nil {
			panic("images: decode " + name + ": " + err.Error())
		}
		decoded[name] = img
	}
}
