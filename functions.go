package main

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"log"
	"math"
	"math/rand"
	"time"

	"github.com/golang/freetype"
	"github.com/jnovack/cloudkey/fonts"
	"github.com/jnovack/cloudkey/images"
)

// Colors from Black to White
var colors = []color.RGBA{
	color.RGBA{0x00, 0x00, 0x00, 0xff},
	color.RGBA{0x11, 0x11, 0x11, 0xff},
	color.RGBA{0x22, 0x22, 0x22, 0xff},
	color.RGBA{0x33, 0x33, 0x33, 0xff},
	color.RGBA{0x44, 0x44, 0x44, 0xff},
	color.RGBA{0x55, 0x55, 0x55, 0xff},
	color.RGBA{0x66, 0x66, 0x66, 0xff},
	color.RGBA{0x77, 0x77, 0x77, 0xff},
	color.RGBA{0x88, 0x88, 0x88, 0xff},
	color.RGBA{0x99, 0x99, 0x99, 0xff},
	color.RGBA{0xaa, 0xaa, 0xaa, 0xff},
	color.RGBA{0xbb, 0xbb, 0xbb, 0xff},
	color.RGBA{0xcc, 0xcc, 0xcc, 0xff},
	color.RGBA{0xdd, 0xdd, 0xdd, 0xff},
	color.RGBA{0xee, 0xee, 0xee, 0xff},
	color.RGBA{0xff, 0xff, 0xff, 0xff},
}

// Increase Fades Out, Decrease Faces In
// No need to fade EVERY step
var fades = []color.RGBA{
	color.RGBA{0xff, 0xff, 0xff, 0xff},
	// color.RGBA{0xff, 0xff, 0xff, 0xee},
	// color.RGBA{0xff, 0xff, 0xff, 0xdd},
	color.RGBA{0xff, 0xff, 0xff, 0xcc},
	// color.RGBA{0xff, 0xff, 0xff, 0xbb},
	// color.RGBA{0xff, 0xff, 0xff, 0xaa},
	// color.RGBA{0xff, 0xff, 0xff, 0x99},
	color.RGBA{0xff, 0xff, 0xff, 0x88},
	// color.RGBA{0xff, 0xff, 0xff, 0x77},
	// color.RGBA{0xff, 0xff, 0xff, 0x66},
	// color.RGBA{0xff, 0xff, 0xff, 0x55},
	color.RGBA{0xff, 0xff, 0xff, 0x44},
	// color.RGBA{0xff, 0xff, 0xff, 0x33},
	// color.RGBA{0xff, 0xff, 0xff, 0x22},
	// color.RGBA{0xff, 0xff, 0xff, 0x11},
	color.RGBA{0xff, 0xff, 0xff, 0x00},
}

// Fade the current screen, in or out (default out)
func fade(inverse bool) {
	capture := image.NewRGBA(fb.Bounds())
	draw.Draw(capture, capture.Bounds(), fb, image.ZP, draw.Src)

	for x := range colors {
		y := x
		if inverse {
			y = len(fades) - 1 - x
		}

		fmt.Printf("%d\r", y)

		bg := image.NewRGBA(fb.Bounds())
		draw.Draw(bg, bg.Bounds(), image.Black, image.ZP, draw.Src)
		draw.DrawMask(bg, bg.Bounds(), capture, image.ZP, image.NewUniform(fades[y]), image.ZP, draw.Over)

		// Put it on the RITZ!
		draw.Draw(fb, fb.Bounds(), bg, image.ZP, draw.Over)
		time.Sleep(8 * time.Millisecond)
	}
}

func colorTest() {
	for x := range colors {
		fmt.Printf("%d\r", x)
		draw.Draw(fb, fb.Bounds(), image.NewUniform(colors[x]), image.ZP, draw.Src)
		time.Sleep(32 * time.Millisecond)
	}
	for x := range colors {
		fmt.Printf("%d\r", x)
		draw.Draw(fb, fb.Bounds(), image.NewUniform(colors[len(colors)-1-x]), image.ZP, draw.Src)
		time.Sleep(32 * time.Millisecond)
	}
}

// Display sends the screen/image immediately to the framebuffer
func display(i int) {
	screen := screens[i]
	draw.Draw(fb, fb.Bounds(), screen, image.ZP, draw.Over)
}

// Fast and smooth (default)
func startFadeCarousel() {
	for {
		for s := range screens {
			capture := image.NewRGBA(fb.Bounds())
			draw.Draw(capture, capture.Bounds(), fb, image.ZP, draw.Src)
			// Fade Old Screen Out
			for x := range fades {
				bg := image.NewRGBA(fb.Bounds())
				draw.Draw(bg, bg.Bounds(), image.Black, image.ZP, draw.Src)
				draw.DrawMask(bg, bg.Bounds(), capture, image.ZP, image.NewUniform(fades[x]), image.ZP, draw.Over)
				draw.Draw(fb, fb.Bounds(), bg, image.ZP, draw.Over)
				time.Sleep(8 * time.Millisecond)
			}
			// Fade New Screen In
			for x := len(fades) - 1; x >= 0; x-- {
				bg := image.NewRGBA(fb.Bounds())
				draw.Draw(bg, bg.Bounds(), image.Black, image.ZP, draw.Src)
				draw.DrawMask(bg, bg.Bounds(), screens[s], image.ZP, image.NewUniform(fades[x]), image.ZP, draw.Over)
				draw.Draw(fb, fb.Bounds(), bg, image.ZP, draw.Over)
				time.Sleep(8 * time.Millisecond)
			}
			time.Sleep(time.Duration(*delay) * time.Millisecond)
		}
	}
}

// Very slow and CPU intensive on arm
func startXCarousel() {
	capture := image.NewRGBA(fb.Bounds())
	for i := 0; i < 2; i++ {
		for s := range screens {
			for x := fb.Bounds().Max.X; x > -1; x-- {
				// Offset current framebuffer 1 pixel to the left (slide out)
				draw.Draw(capture, image.Rect(-1, 0, -1+screens[s].Bounds().Max.X, screens[s].Bounds().Max.Y), fb, image.ZP, draw.Src)

				// Print new screen directly on the capture as it slides out
				draw.Draw(capture, image.Rect(x, 0, x+screens[s].Bounds().Max.X, screens[s].Bounds().Max.Y), screens[s], image.ZP, draw.Src)

				// Send it all to the framebuffer
				draw.Draw(fb, fb.Bounds(), capture, image.ZP, draw.Over)
			}
			time.Sleep(time.Duration(*delay) * time.Millisecond)
		}
	}
}

// slow and cpu intensive in bursts on arm
func startYCarousel() {
	capture := image.NewRGBA(fb.Bounds())
	for i := 0; i < 2; i++ {
		for s := range screens {
			for y := fb.Bounds().Max.Y; y > -1; y-- {
				// Offset current framebuffer 1 pixel to the left (slide out)
				draw.Draw(capture, image.Rect(0, -1, screens[s].Bounds().Max.X, -1+screens[s].Bounds().Max.Y), fb, image.ZP, draw.Src)

				// Print new screen directly on the capture as it slides out
				draw.Draw(capture, image.Rect(0, y, screens[s].Bounds().Max.X, y+screens[s].Bounds().Max.Y), screens[s], image.ZP, draw.Src)

				// Send it all to the framebuffer
				draw.Draw(fb, fb.Bounds(), capture, image.ZP, draw.Over)
			}
			time.Sleep(time.Duration(*delay) * time.Millisecond)
		}
	}
}

// Clear clears the screen
func clear() {
	draw.Draw(fb, fb.Bounds(), image.Black, image.ZP, draw.Src)
}

// Loader gives times for everything time to populate (loaders in 2018?)
func load() {
	clear()

	draw.Draw(fb, image.Rect(64, 8, 64+32, 8+32), images.Load("logo"), image.ZP, draw.Src)

	// Outline the loader line
	for i := 0; i < 100; i++ {
		fb.Set(30+i, 52, colors[3])
	}

	// Fill the loader line
	// This is just a delay right now, do your checks here!
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	for i := 0; i < 100; i++ {
		fb.Set(30+i, 52, colors[15])
		time.Sleep(time.Duration(r.Intn(32)) * time.Millisecond)
		// mathmatically, the average sleep time is about half of the seed number
	}
	// fade(false)
}

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
