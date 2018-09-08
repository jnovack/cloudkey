package fonts

import (
	"encoding/base64"
	"io/ioutil"
	"log"
	"strings"

	"github.com/golang/freetype"
	"github.com/golang/freetype/truetype"
)

// Font ...
func Font() (*truetype.Font, error) {
	reader := base64.NewDecoder(base64.StdEncoding, strings.NewReader(latoRegular))
	file, err := ioutil.ReadAll(reader)
	if err != nil {
		log.Println(err)
		return nil, err
	}
	font, err := freetype.ParseFont(file)
	if err != nil {
		log.Println(err)
		return nil, err
	}
	return font, nil
}
