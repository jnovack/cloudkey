package fonts

import (
	"encoding/base64"
	"io/ioutil"
	"log"
	"strings"

	"github.com/golang/freetype"
	"github.com/golang/freetype/truetype"
)

var assets map[string]string

// Load ...
func Load(name string) *truetype.Font {
	reader := base64.NewDecoder(base64.StdEncoding, strings.NewReader(assets[name]))
	file, err := ioutil.ReadAll(reader)
	if err != nil {
		log.Println(err)
		return nil
	}
	font, err := freetype.ParseFont(file)
	if err != nil {
		log.Println(err)
		return nil
	}
	return font
}

func init() {
	assets = make(map[string]string)

	// Define the permitted fonts
	// Golang has no concept of dynamic variables, because it's a compliled language
	// All variables must be declared, cannot iterate through files and load consts
	assets["lato-regular"] = latoRegular
}
