package main

// Copyright 2010 The Freetype-Go Authors. All rights reserved.
// Use of this source code is governed by your choice of either the
// FreeType License or the GNU General Public License version 2 (or
// any later version), both of which can be found in the LICENSE file.

import (
	"errors"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"io/ioutil"
	"log"
	"math"
	"net"
	"os"
	"time"

	"framebuffer"

	"github.com/golang/freetype"
	"golang.org/x/image/font"
)

var (
	dpi      = flag.Float64("dpi", 72, "screen resolution in Dots Per Inch")
	fontfile = flag.String("fontfile", "/usr/share/fonts/truetype/Lato2OFL/Lato-Regular.ttf", "filename of the ttf font")
	hinting  = flag.String("hinting", "none", "none | full")
	size     = flag.Float64("size", 12, "font size in points")
	spacing  = flag.Float64("spacing", 1, "line spacing (e.g. 2 means double spaced)")
	inverse  = flag.Bool("inverse", false, "black text on a white background")
	clear    = flag.Bool("clear", false, "clear the screen")
)

var colors = []color.Gray{
	color.Gray{0},
	color.Gray{15},
	color.Gray{31},
	color.Gray{47},
	color.Gray{63},
	color.Gray{79},
	color.Gray{95},
	color.Gray{111},
	color.Gray{127},
	color.Gray{143},
	color.Gray{159},
	color.Gray{175},
	color.Gray{191},
	color.Gray{207},
	color.Gray{223},
	color.Gray{239},
	color.Gray{255},
}

var text = []string{
	"The quick brown fox =",
	"jumps over the lazy dog.",
	"(142) 867-5309 !#*@?+%",
}

func localIP() (string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue // interface down
		}
		if iface.Flags&net.FlagLoopback != 0 {
			continue // loopback interface
		}
		addrs, err := iface.Addrs()
		if err != nil {
			return "", err
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() {
				continue
			}
			ip = ip.To4()
			if ip == nil {
				continue // not an ipv4 address
			}
			return ip.String(), nil
		}
	}
	return "", errors.New("network not found")
}

func drawEveryPixel() {
	// Draw the guidelines.
	// for i := 0; i < width; i++ {
	// for j := 0; j < height; j++ {
	// fb.Set(i, j, colors[x])
	// fmt.Printf("%d: %dx%d\r", x, i, j)
	// }
	// time.Sleep(10 * time.Millisecond)
	// }
	// time.Sleep(1000 * time.Millisecond)
}

func main() {
	flag.Parse()

	fb, err := framebuffer.Open("/dev/fb0")
	if err != nil {
		panic(err)
	}

	width := fb.Bounds().Max.X
	height := fb.Bounds().Max.Y
	fmt.Printf("Resolution: %dx%d pixels\r\n", width, height)

	if *clear {
		for x := range colors {
			fmt.Printf("%d\r", x)
			draw.Draw(fb, fb.Bounds(), image.NewUniform(colors[x]), image.ZP, draw.Src)
			time.Sleep(500 * time.Millisecond)
		}
		draw.Draw(fb, fb.Bounds(), image.NewUniform(colors[0]), image.ZP, draw.Src)
		os.Exit(0)
	}

	hostname, _ := os.Hostname()
	ipaddr, _ := localIP()
	text = append(text, hostname)
	text = append(text, ipaddr)

	// Read the font data.
	fontBytes, err := ioutil.ReadFile(*fontfile)
	if err != nil {
		log.Println(err)
		return
	}
	f, err := freetype.ParseFont(fontBytes)
	if err != nil {
		log.Println(err)
		return
	}

	// Initialize the context.
	fg, bg := image.White, image.Black
	// ruler := color.RGBA{0x22, 0x22, 0x22, 0xff}
	if *inverse {
		fg, bg = image.Black, image.White
		// ruler = color.RGBA{0xdd, 0xdd, 0xdd, 0xff}
	}

	for x := 8; x < 15; x++ {
		fmt.Printf("Font Size: %d\r\n", x)
		// Clear screen
		draw.Draw(fb, fb.Bounds(), bg, image.ZP, draw.Src)

		// Draw the guidelines.
		// for i := 0; i < width; i++ {
		// 	fb.Set(0+i, 0, ruler)
		// 	fb.Set(0+i, height-1, ruler)
		// }
		// for i := 0; i < height; i++ {
		// 	fb.Set(0, 0+i, ruler)
		// 	fb.Set(width-1, 0+i, ruler)
		// }

		// Setup new context
		c := freetype.NewContext()
		c.SetDPI(*dpi)
		c.SetFont(f)
		c.SetFontSize(float64(x)) // was (*size)
		c.SetClip(fb.Bounds())
		c.SetDst(fb)
		c.SetSrc(fg)
		switch *hinting {
		default:
			c.SetHinting(font.HintingNone)
		case "none":
			c.SetHinting(font.HintingNone)
		case "full":
			c.SetHinting(font.HintingFull)
		}

		// Draw the text.
		pt := freetype.Pt(0, 0+int(c.PointToFixed(math.Round(float64(x)+1))>>6))
		// pt := freetype.Pt(10, 10+int(c.PointToFixed(*size)>>6))
		for _, s := range text {
			_, err = c.DrawString(s, pt)
			if err != nil {
				log.Println(err)
				return
			}
			pt.Y += c.PointToFixed(math.RoundToEven(float64(x)**spacing + 1))
			// pt.Y += c.PointToFixed(*size * *spacing)
		}
		time.Sleep(3000 * time.Millisecond)
	}
}
