package main

import (
	"fmt"
	"image"
	"image/draw"
	"log"
	"os"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/jnovack/cloudkey/images"
	"github.com/jnovack/cloudkey/src/network"
	"github.com/jnovack/speedtest"
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

	dmsg := "calculating..."
	umsg := "calculating..."
	tmsg := "in progress"

	download := make(chan int)
	upload := make(chan int)
	lastcheck := time.Now()

	screen := screens[i]

	draw.Draw(screen, screen.Bounds(), image.Black, image.ZP, draw.Src)
	draw.Draw(screen, image.Rect(2, 2, 2+16, 2+16), images.Load("download"), image.ZP, draw.Src)
	draw.Draw(screen, image.Rect(2, 22, 2+16, 22+16), images.Load("upload"), image.ZP, draw.Src)
	draw.Draw(screen, image.Rect(2, 42, 2+16, 42+16), images.Load("clock"), image.ZP, draw.Src)

	if *demo {
		dmsg = "86.1 Mb/s"
		umsg = "43.9 Mb/s"
		tmsg = "25 minutes ago"
		write(screen, dmsg, 22, 1, 12, "lato-regular")
		write(screen, umsg, 22, 21, 12, "lato-regular")
		write(screen, tmsg, 22, 41, 12, "lato-regular")
	} else {

		client := speedtest.NewClient(&speedtest.Opts{})
		server := client.SelectServer(&speedtest.Opts{})

		go func() {
			for {
				tmsg = fmt.Sprintf("%s", humanize.Time(lastcheck))
				draw.Draw(screen, image.Rect(20, 0, 160, 60), image.Black, image.ZP, draw.Src)
				write(screen, dmsg, 22, 1, 12, "lato-regular")
				write(screen, umsg, 22, 21, 12, "lato-regular")
				write(screen, tmsg, 22, 41, 12, "lato-regular")
				time.Sleep(10 * time.Second)
			}
		}()

		for { // Loop Every Hour
			myLeds.LED("blue").Blink(128, 500, 500)
			ddone := false
			udone := false
			go func() { download <- server.DownloadSpeed() }()

			for {
				if ddone != true {
					select {
					case dlspeed := <-download:
						dmsg = fmt.Sprintf("%.2f Mb", float64(dlspeed)/(1<<17))
						go func() { upload <- server.UploadSpeed() }()
						ddone = true
					}
				}

				if udone != true {
					select {
					case ulspeed := <-upload:
						umsg = fmt.Sprintf("%.2f Mb", float64(ulspeed)/(1<<17))
						udone = true
					}
				}

				if ddone && udone {
					tmsg = fmt.Sprintf("%s", humanize.Time(time.Now()))
					break
				}

			}

			myLeds.LED("blue").On()
			log.Println("Download: %s / Upload: %s", dmsg, umsg)
			time.Sleep(59 * time.Minute)
		}
	}
}
