package main

import (
	"image"
	"image/draw"
	"os"

	"github.com/jnovack/cloudkey/images"
	"github.com/jnovack/cloudkey/src/network"
)

func buildNetwork(i int) {
	hostname := "cloudkey-gen2.local"
	lan := "192.168.10.111"
	wan := "203.0.113.32"
	if !*demo {
		hostname, _ = os.Hostname()
		lan, _ = network.LANIP()
		wan, _ = network.WANIP()
	}

	screen := screens[i]

	draw.Draw(screen, screen.Bounds(), image.Black, image.ZP, draw.Src)
	draw.Draw(screen, image.Rect(2, 2, 2+16, 2+16), images.Load("host"), image.ZP, draw.Src)
	draw.Draw(screen, image.Rect(2, 22, 2+16, 22+16), images.Load("network"), image.ZP, draw.Src)
	draw.Draw(screen, image.Rect(2, 42, 2+16, 42+16), images.Load("internet"), image.ZP, draw.Src)

	write(screen, hostname, 22, 1, 12, "lato-regular")
	write(screen, lan, 22, 21, 12, "lato-regular")
	write(screen, wan, 22, 41, 12, "lato-regular")

}

func buildSpeedTest(i int) {
	download := "calculating..."
	upload := "calculating..."
	timesince := "in progress"
	if *demo {
		download = "86.1 Mb/s"
		upload = "43.9 Mb/s"
		timesince = "25 minutes ago"
	}

	screen := screens[i]

	draw.Draw(screen, screen.Bounds(), image.Black, image.ZP, draw.Src)
	draw.Draw(screen, image.Rect(2, 2, 2+16, 2+16), images.Load("download"), image.ZP, draw.Src)
	draw.Draw(screen, image.Rect(2, 22, 2+16, 22+16), images.Load("upload"), image.ZP, draw.Src)
	draw.Draw(screen, image.Rect(2, 42, 2+16, 42+16), images.Load("clock"), image.ZP, draw.Src)

	write(screen, download, 22, 1, 12, "lato-regular")
	write(screen, upload, 22, 21, 12, "lato-regular")
	write(screen, timesince, 22, 41, 12, "lato-regular")
}
