package display

import (
	"fmt"
	"image"
	"image/draw"
	"os"
	"sync"
	"time"

	humanize "github.com/dustin/go-humanize"
	"github.com/jnovack/cloudkey/internal/images"
	"github.com/jnovack/cloudkey/pkg/network"
	"github.com/jnovack/speedtest"
)

// speedtest reports transfer rates in bytes/sec. Dividing by 2^17 (131072)
// converts bytes/sec to mebibits/sec (Mb): bytes * 8 bits / 2^20.
const bytesToMebibits = 1 << 17

func buildNetwork(i int, demo bool) {
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
			if !demo {
				hostname, _ = os.Hostname()
			}
			write(screen, hostname, 22, 1, 12, "lato-regular")

			if !demo {
				lan, _ = network.LANIP()
			}
			write(screen, lan, 22, 21, 12, "lato-regular")

			if !demo {
				wan, _ = network.WANIP()
			}
			write(screen, wan, 22, 41, 12, "lato-regular")

			time.Sleep(59 * time.Minute)
		}
	}()
}

func buildSpeedTest(i int, demo bool) {
	dmsg := "calculating..."
	umsg := "calculating..."
	tmsg := "in progress"

	download := make(chan int)
	upload := make(chan int)
	lastcheck := time.Now()

	// mu guards dmsg, umsg, and lastcheck, which are written by the hourly
	// speed-test goroutine and read by the 10-second refresh goroutine.
	var mu sync.Mutex

	screen := screens[i]

	draw.Draw(screen, screen.Bounds(), image.Black, image.ZP, draw.Src)
	draw.Draw(screen, image.Rect(2, 2, 2+16, 2+16), images.Load("download"), image.ZP, draw.Src)
	draw.Draw(screen, image.Rect(2, 22, 2+16, 22+16), images.Load("upload"), image.ZP, draw.Src)
	draw.Draw(screen, image.Rect(2, 42, 2+16, 42+16), images.Load("clock"), image.ZP, draw.Src)

	if demo {
		dmsg = "86.1 Mb/s"
		umsg = "43.9 Mb/s"
		tmsg = "25 minutes ago"
		write(screen, dmsg, 22, 1, 12, "lato-regular")
		write(screen, umsg, 22, 21, 12, "lato-regular")
		write(screen, tmsg, 22, 41, 12, "lato-regular")
		return
	}

	client := speedtest.NewClient(&speedtest.Opts{})

	// Loop every 10 Seconds
	go func() {
		for {
			mu.Lock()
			d, u, lc := dmsg, umsg, lastcheck
			mu.Unlock()

			tmsg = humanize.Time(lc)
			draw.Draw(screen, image.Rect(20, 0, 160, 60), image.Black, image.ZP, draw.Src)
			write(screen, d, 22, 1, 12, "lato-regular")
			write(screen, u, 22, 21, 12, "lato-regular")
			write(screen, tmsg, 22, 41, 12, "lato-regular")
			time.Sleep(10 * time.Second)
		}
	}()

	// Loop Every Hour
	go func() {
		for {
			myLeds.LED("blue").Blink(128, 500, 500)

			server := client.SelectServer(&speedtest.Opts{})

			fmt.Printf("Hosted by %s (%s) [%.2f km]: %d ms\n",
				server.Sponsor,
				server.Name,
				server.Distance,
				server.Latency/time.Millisecond)

			go func() { download <- server.DownloadSpeed() }()
			dlspeed := <-download
			mu.Lock()
			dmsg = fmt.Sprintf("%.2f Mb", float64(dlspeed)/bytesToMebibits)
			mu.Unlock()

			go func() { upload <- server.UploadSpeed() }()
			ulspeed := <-upload
			mu.Lock()
			umsg = fmt.Sprintf("%.2f Mb", float64(ulspeed)/bytesToMebibits)
			mu.Unlock()

			mu.Lock()
			lastcheck = time.Now()
			d, u := dmsg, umsg
			mu.Unlock()

			fmt.Printf("Download: %s / Upload: %s\n", d, u)
			myLeds.LED("blue").On()
			time.Sleep(59 * time.Minute)
		}
	}()
}
