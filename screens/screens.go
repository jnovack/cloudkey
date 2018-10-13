package screens

import (
	"fmt"
	"image"
	"image/draw"
	"os"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/jnovack/cloudkey/images"
	"github.com/jnovack/cloudkey/src/network"
	"github.com/jnovack/speedtest"
)

func buildNetwork(i int) {
	screen := screens[i]
	hostname := "cloudkey-gen2.local"
	lan := "192.168.10.111"
	wan := "203.0.113.32"

	draw.Draw(screen, screen.Bounds(), image.Black, image.ZP, draw.Src)
	draw.Draw(screen, image.Rect(2, 2, 2+16, 2+16), images.Load("host"), image.ZP, draw.Src)
	draw.Draw(screen, image.Rect(2, 22, 2+16, 22+16), images.Load("network"), image.ZP, draw.Src)
	draw.Draw(screen, image.Rect(2, 42, 2+16, 42+16), images.Load("internet"), image.ZP, draw.Src)

	// Loop Every Hour
	go func() {
		for {
			if !*demo {
				hostname, _ = os.Hostname()
				lan, _ = network.LANIP()
				wan, _ = network.WANIP()
			}

			write(screen, hostname, 22, 1, 12, "lato-regular")
			write(screen, lan, 22, 21, 12, "lato-regular")
			write(screen, wan, 22, 41, 12, "lato-regular")
			time.Sleep(59 * time.Minute)
		}
	}()
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

		// Loop every 10 Seconds
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

		// Loop Every Hour
		go func() {
			for {
				myLeds.LED("blue").Blink(128, 500, 500)

				server := client.SelectServer(&speedtest.Opts{})

				j(fmt.Sprintf("Hosted by %s (%s) [%.2f km]: %d ms\n",
					server.Sponsor,
					server.Name,
					server.Distance,
					server.Latency/time.Millisecond))

				go func() { download <- server.DownloadSpeed() }()

			Download:
				for {
					select {
					case dlspeed := <-download:
						dmsg = fmt.Sprintf("%.2f Mb", float64(dlspeed)/(1<<17))
						break Download
					}
				}

				go func() { upload <- server.UploadSpeed() }()

			Upload:
				for {
					select {
					case ulspeed := <-upload:
						umsg = fmt.Sprintf("%.2f Mb", float64(ulspeed)/(1<<17))
						break Upload
					}
				}

				lastcheck = time.Now()
				j(fmt.Sprintf("Download: %s / Upload: %s", dmsg, umsg))
				myLeds.LED("blue").On()
				time.Sleep(59 * time.Minute)
			}
		}()
	}
}
