package display

import (
	"fmt"
	"image"
	"image/draw"
	"os"
	"sync"
	"time"

	humanize "github.com/dustin/go-humanize"
	"github.com/rs/zerolog/log"

	"github.com/jnovack/cloudkey/internal/images"
	"github.com/jnovack/cloudkey/pkg/network"
	"github.com/jnovack/speedtest"
)

// speedtest reports transfer rates in bytes/sec. Dividing by 2^17 (131072)
// converts bytes/sec to mebibits/sec (Mb): bytes * 8 bits / 2^20.
const bytesToMebibits = 1 << 17

// drawLocal renders the hostname/LAN-IP layout onto screen, large and
// centered so the screen stays readable as the OLED ages. It redraws from a
// blank background each call: centered text shifts position whenever its
// width changes, so a partial redraw would leave stale glyphs behind.
func drawLocal(screen draw.Image, hostname, lan string) {
	draw.Draw(screen, screen.Bounds(), image.Black, image.ZP, draw.Src)
	center(screen, hostname, 8, 14, "lato-regular", true)
	center(screen, lan, 34, 13, "lato-regular", false)
}

// drawRemote renders the date/time + WAN-IP layout onto screen, large and
// centered.
func drawRemote(screen draw.Image, now time.Time, wan string) {
	draw.Draw(screen, screen.Bounds(), image.Black, image.ZP, draw.Src)
	center(screen, now.Format("2006-01-02 15:04"), 8, 13, "lato-regular", true)
	center(screen, wan, 34, 13, "lato-regular", false)
}

// drawSpeedTest renders the speedtest layout (icons + stats) onto screen.
func drawSpeedTest(screen draw.Image, dmsg, umsg, tmsg string) {
	draw.Draw(screen, screen.Bounds(), image.Black, image.ZP, draw.Src)
	draw.Draw(screen, image.Rect(2, 2, 2+16, 2+16), images.Load("download"), image.ZP, draw.Src)
	draw.Draw(screen, image.Rect(2, 22, 2+16, 22+16), images.Load("upload"), image.ZP, draw.Src)
	draw.Draw(screen, image.Rect(2, 42, 2+16, 42+16), images.Load("clock"), image.ZP, draw.Src)
	write(screen, dmsg, 22, 1, 12, "lato-regular", false)
	write(screen, umsg, 22, 21, 12, "lato-regular", false)
	write(screen, tmsg, 22, 41, 12, "lato-regular", false)
}

// buildLocal starts the goroutine that keeps the hostname/LAN-IP screen
// up to date.
func buildLocal(i int, demo bool) {
	screen := screens[i]
	hostname := "cloudkey-gen2.local"
	lan := "192.168.10.111"

	go func() {
		for {
			if !demo {
				hostname, _ = os.Hostname()
			}
			if !demo {
				lan, _ = network.LANIP()
			}

			drawLocal(screen, hostname, lan)

			time.Sleep(59 * time.Minute)
		}
	}()
}

// buildRemote starts the goroutines that keep the date/time + WAN-IP screen
// up to date. The clock line refreshes frequently; the WAN IP is looked up
// hourly like the rest of the network info.
func buildRemote(i int, demo bool) {
	screen := screens[i]
	// Live deployments show "checking..." until the first WANIP() lookup
	// succeeds, rather than a placeholder that looks like a real address.
	wan := "checking..."
	if demo {
		wan = "203.0.113.32"
	}

	// mu guards wan, which is written by the hourly lookup goroutine and
	// read by the clock-refresh goroutine.
	var mu sync.Mutex

	go func() {
		for {
			if !demo {
				if w, err := network.WANIP(); err == nil {
					log.Info().Str("wan_ip", w).Msg("found external IP address")
					mu.Lock()
					wan = w
					mu.Unlock()
				}
			}
			time.Sleep(59 * time.Minute)
		}
	}()

	go func() {
		for {
			mu.Lock()
			w := wan
			mu.Unlock()

			drawRemote(screen, time.Now(), w)

			time.Sleep(30 * time.Second)
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

	if demo {
		dmsg = "86.1 Mb/s"
		umsg = "43.9 Mb/s"
		tmsg = "25 minutes ago"
		drawSpeedTest(screen, dmsg, umsg, tmsg)
		return
	}

	drawSpeedTest(screen, dmsg, umsg, tmsg)

	client := speedtest.NewClient(&speedtest.Opts{})

	// Loop every 10 Seconds
	go func() {
		for {
			mu.Lock()
			d, u, lc := dmsg, umsg, lastcheck
			mu.Unlock()

			tmsg = humanize.Time(lc)
			drawSpeedTest(screen, d, u, tmsg)
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

			log.Debug().Str("download", d).Str("upload", u).Msg("speedtest complete")
			myLeds.LED("blue").On()
			time.Sleep(59 * time.Minute)
		}
	}()
}
