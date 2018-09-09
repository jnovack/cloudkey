package images

import (
	"encoding/base64"
	"image"
	"image/png"
	"log"
	"strings"
)

var assets map[string]string

// Load ...
func Load(name string) image.Image {
	reader := base64.NewDecoder(base64.StdEncoding, strings.NewReader(assets[name]))
	img, err := png.Decode(reader)
	if err != nil {
		log.Fatal(err)
		return nil
	}
	return img
}

func init() {
	assets = make(map[string]string)

	// Define the permitted fonts
	// Golang has no concept of dynamic variables, because it's a compliled language
	// All variables must be declared, cannot iterate through files and load consts
	assets["clock"] = clock
	assets["dockerOff"] = dockerOff
	assets["dockerOn"] = dockerOn
	assets["download"] = download
	assets["downloadIdle"] = downloadIdle
	assets["host"] = host
	assets["internet"] = internet
	assets["logo"] = logo
	assets["network"] = network
	assets["upload"] = upload
	assets["uploadIdle"] = uploadIdle
}
