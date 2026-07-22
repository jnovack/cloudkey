// Package fonts parses the display's compiled-in TrueType fonts once at
// init and serves them by name to internal/display's text rendering.
package fonts

import (
	"encoding/base64"
	"io"
	"strings"

	"github.com/golang/freetype"
	"github.com/golang/freetype/truetype"
)

var assets map[string]string

// parsed holds every registered font, decoded and parsed once at init. Fonts are
// immutable embedded assets, so re-parsing per call bought nothing and cost a full
// base64+TrueType parse of a ~78KB payload on every text draw (~105us on a
// developer machine, materially more on the target ARM device).
var parsed map[string]*truetype.Font

// Load returns the named font from the embedded asset registry, or nil if no such
// name is registered. It never returns nil for a registered name: every embedded
// font is parsed at init, and an unparseable one panics there rather than handing
// back a nil that would nil-dereference deep inside a redraw goroutine (freetype's
// truetype.NewFace panics on a nil font).
func Load(name string) *truetype.Font {
	return parsed[name]
}

func init() {
	assets = make(map[string]string)

	// Font data is compiled in as Go constant strings (see lato-regular.go).
	// Register each font name here so Load can find it by name.
	assets["lato-regular"] = latoRegular

	parsed = make(map[string]*truetype.Font, len(assets))
	for name, data := range assets {
		file, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, strings.NewReader(data)))
		if err != nil {
			panic("fonts: decode " + name + ": " + err.Error())
		}
		font, err := freetype.ParseFont(file)
		if err != nil {
			panic("fonts: parse " + name + ": " + err.Error())
		}
		parsed[name] = font
	}
}
