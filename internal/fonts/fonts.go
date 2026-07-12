package fonts

import (
	"encoding/base64"
	"io"
	"log"
	"strings"

	"github.com/golang/freetype"
	"github.com/golang/freetype/truetype"
)

var assets map[string]string

// Load decodes and parses the named font from the embedded asset registry.
// It returns nil (and logs the error) if the name is not registered or the
// embedded data cannot be parsed; callers must check for nil before use.
func Load(name string) *truetype.Font {
	reader := base64.NewDecoder(base64.StdEncoding, strings.NewReader(assets[name]))
	file, err := io.ReadAll(reader)
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

	// Font data is compiled in as Go constant strings (see lato-regular.go).
	// Register each font name here so Load can find it by name.
	assets["lato-regular"] = latoRegular
}
