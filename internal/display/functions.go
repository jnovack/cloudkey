package display

import (
	"image"
	"image/color"
	"image/draw"
	"log"
	"math"
	"time"

	"github.com/golang/freetype"
	"github.com/golang/freetype/truetype"
	"github.com/jnovack/cloudkey/internal/fonts"
)

// Colors from Black to White
var colors = []color.Gray{
	color.Gray{0x00},
	color.Gray{0x11},
	color.Gray{0x22},
	color.Gray{0x33},
	color.Gray{0x44},
	color.Gray{0x55},
	color.Gray{0x66},
	color.Gray{0x77},
	color.Gray{0x88},
	color.Gray{0x99},
	color.Gray{0xaa},
	color.Gray{0xbb},
	color.Gray{0xcc},
	color.Gray{0xdd},
	color.Gray{0xee},
	color.Gray{0xff},
}

// Increasing alpha fades out, decreasing alpha fades in.
// No need to fade EVERY step.
var fades = []color.Alpha{
	color.Alpha{0xff},
	color.Alpha{0xcc},
	color.Alpha{0x99},
	color.Alpha{0x66},
	color.Alpha{0x33},
	color.Alpha{0x00},
}

// clearScreen clears... the... screen
func clearScreen() {
	draw.Draw(fb, fb.Bounds(), image.NewUniform(color.Gray{0}), image.ZP, draw.Src)
}

// startFadeCarousel Fast and smooth (default)
func startFadeCarousel(delay float64) {
	for {
		for s := range screens {
			capture := image.NewGray(fb.Bounds())
			draw.Draw(capture, capture.Bounds(), fb, image.ZP, draw.Src)
			// Fade Old Screen Out
			for x := range fades {
				bg := image.NewGray(fb.Bounds())
				draw.Draw(bg, bg.Bounds(), image.NewUniform(color.Gray{0}), image.ZP, draw.Src)
				draw.DrawMask(bg, bg.Bounds(), capture, image.ZP, image.NewUniform(fades[x]), image.ZP, draw.Over)
				draw.Draw(fb, fb.Bounds(), bg, image.ZP, draw.Over)
				time.Sleep(8 * time.Millisecond)
			}

			// Fade New Screen In
			for x := len(fades) - 1; x >= 0; x-- {
				bg := image.NewGray(fb.Bounds())
				draw.Draw(bg, bg.Bounds(), image.NewUniform(color.Gray{0}), image.ZP, draw.Src)
				draw.DrawMask(bg, bg.Bounds(), screens[s], image.ZP, image.NewUniform(fades[x]), image.ZP, draw.Over)
				draw.Draw(fb, fb.Bounds(), bg, image.ZP, draw.Over)
				time.Sleep(8 * time.Millisecond)
			}
			time.Sleep(time.Duration(delay) * time.Millisecond)
		}
	}
}

// Write draws text to a x,y coordinate on the image
func write(screen draw.Image, text string, x, y int, size float64, fontname string) {
	font := fonts.Load(fontname)
	// Setup new context
	c := freetype.NewContext()
	c.SetFont(font)            // Set the font
	c.SetFontSize(size)        // Set font size
	c.SetDPI(72)               // Fixed DPI
	c.SetClip(screen.Bounds()) // Clip the text?
	c.SetDst(screen)           // Send it where?
	c.SetSrc(image.White)      // Color of Foreground

	_, err := c.DrawString(text, freetype.Pt(x, y+int(c.PointToFixed(math.Round(float64(size)+1))>>6))) // y is center of line, shift to top of line
	if err != nil {
		log.Println(err)
		return
	}
}

// center horizontally centers text on the given screen at vertical position y.
func center(screen draw.Image, text string, x, y int, size float64, fontname string) {
	font := fonts.Load(fontname)

	// Measure the rendered width of the text so it can be horizontally centered.
	opts := truetype.Options{}
	opts.DPI = 72
	opts.Size = size + 1
	face := truetype.NewFace(font, &opts)

	var widths int
	for _, t := range text {
		awidth, ok := face.GlyphAdvance(rune(t))
		if !ok {
			return
		}
		widths += int(float64(awidth) / 64)
	}
	write(screen, text, screen.Bounds().Max.X/2-widths/2, y, size, fontname)
}
