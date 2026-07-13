package images

import (
	"encoding/base64"
	"image"
	"image/png"
	"log"
	"strings"
)

var assets map[string]string

// Load decodes the named PNG from the embedded asset registry.
// It calls log.Fatal (terminating the process) if the name is not registered or
// the embedded data cannot be decoded, since every registered image is a
// required startup asset.
func Load(name string) image.Image {
	reader := base64.NewDecoder(base64.StdEncoding, strings.NewReader(assets[name]))
	img, err := png.Decode(reader)
	if err != nil {
		log.Fatal(err)
	}
	return img
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
}
