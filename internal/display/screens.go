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
	"github.com/jnovack/cloudkey/pkg/cpu"
	"github.com/jnovack/cloudkey/pkg/memory"
	"github.com/jnovack/cloudkey/pkg/network"
	"github.com/jnovack/cloudkey/pkg/storage"
	"github.com/jnovack/cloudkey/pkg/systemd"
	"github.com/jnovack/cloudkey/pkg/tailscale"
	"github.com/jnovack/cloudkey/pkg/wireguard"
	"github.com/jnovack/speedtest"
)

// speedtest reports transfer rates in bytes/sec. Dividing by 2^17 (131072)
// converts bytes/sec to mebibits/sec (Mb): bytes * 8 bits / 2^20.
const bytesToMebibits = 1 << 17

// storageWarnThreshold is the percent-used a mount must exceed before
// drawStorageRow draws its warning icon.
const storageWarnThreshold = 90.0

// formatGB renders bytes as a decimal GB value with exactly 3 significant
// digits (e.g. "9.87GB", "23.4GB", "897GB") - enough precision to be useful
// across the range of card and drive sizes this screen shows, without the
// string length jumping around row to row.
func formatGB(bytes uint64) string {
	gb := float64(bytes) / (1 << 30)
	switch {
	case gb >= 100:
		return fmt.Sprintf("%.0fGB", gb)
	case gb >= 10:
		return fmt.Sprintf("%.1fGB", gb)
	default:
		return fmt.Sprintf("%.2fGB", gb)
	}
}

// formatPercent renders a percentage zero-padded to at least 2 digits (e.g.
// "05%", "45%", "100%").
func formatPercent(percent float64) string {
	return fmt.Sprintf("%02.0f%%", percent)
}

// storageDisplay is one row's pre-formatted display state: the text drawn
// for used space and percent used, and whether the row has crossed
// storageWarnThreshold. notMounted takes over the row entirely (see
// drawStorageRow) when nothing is mounted at the stat'd path, so gb/percent/
// warn are left zero-valued in that case.
type storageDisplay struct {
	gb         string
	percent    string
	warn       bool
	notMounted bool
}

// statStorage stats path and formats the result for display. storage.Stat
// can fail two distinct ways, both handled without panicking: path exists
// but nothing is mounted there (storage.ErrNotMounted, e.g. no SD card
// inserted) shows "not mounted"; any other error (e.g. the mount-point
// directory itself doesn't exist) falls back to the same "not mounted"
// display rather than crashing the screen or showing a stale/zeroed reading.
func statStorage(path string) storageDisplay {
	u, err := storage.Stat(path)
	if err != nil {
		return storageDisplay{notMounted: true}
	}
	return storageDisplay{
		gb:      formatGB(u.UsedBytes),
		percent: formatPercent(u.Percent),
		warn:    u.Percent > storageWarnThreshold,
	}
}

// drawHost renders the hostname + date/time layout onto screen, large and
// centered so it stays readable as the OLED ages. It redraws from a blank
// background each call: centered text shifts position whenever its width
// changes, so a partial redraw would leave stale glyphs behind.
func drawHost(screen draw.Image, hostname string, now time.Time) {
	draw.Draw(screen, screen.Bounds(), image.Black, image.Point{}, draw.Src)
	center(screen, hostname, 8, 16, "lato-regular", true)
	center(screen, now.Format("2006-01-02 15:04"), 34, 15, "lato-regular", false)
}

// drawNetwork renders the LAN and WAN addresses onto screen, each tagged with
// an icon — a computer for the LAN address, a globe for the WAN address — so
// the two bare IPs are told apart at a glance. It follows drawSpeedTest's
// icon-plus-text row layout rather than the centered text of drawHost.
func drawNetwork(screen draw.Image, lan, wan string) {
	draw.Draw(screen, screen.Bounds(), image.Black, image.Point{}, draw.Src)
	draw.Draw(screen, image.Rect(2, 10, 2+16, 10+16), images.Load("host"), image.Point{}, draw.Src)
	draw.Draw(screen, image.Rect(2, 42, 2+16, 42+16), images.Load("internet"), image.Point{}, draw.Src)
	write(screen, lan, 22, 7, 16, "lato-regular", false)
	write(screen, wan, 22, 39, 16, "lato-regular", true)
}

// drawSpeedTest renders the speedtest layout (icons + stats) onto screen.
func drawSpeedTest(screen draw.Image, dmsg, umsg, tmsg string) {
	draw.Draw(screen, screen.Bounds(), image.Black, image.Point{}, draw.Src)
	draw.Draw(screen, image.Rect(2, 2, 2+16, 2+16), images.Load("download"), image.Point{}, draw.Src)
	draw.Draw(screen, image.Rect(2, 22, 2+16, 22+16), images.Load("upload"), image.Point{}, draw.Src)
	draw.Draw(screen, image.Rect(2, 42, 2+16, 42+16), images.Load("clock"), image.Point{}, draw.Src)
	write(screen, dmsg, 22, 1, 12, "lato-regular", false)
	write(screen, umsg, 22, 21, 12, "lato-regular", false)
	write(screen, tmsg, 22, 41, 12, "lato-regular", false)
}

// drawStorageRow renders one removable-media row: an icon, its used-space
// text, its percent-used text, and (space always reserved for it, drawn only
// past storageWarnThreshold) a warning icon. The warning icon's column is
// fixed regardless of whether it's drawn, so a row crossing the threshold
// doesn't shift the percent text next to it. When nothing is mounted at the
// row's path, a single "not mounted" message takes over the whole text area
// instead of a used-space/percent pair that wouldn't mean anything.
func drawStorageRow(screen draw.Image, y int, icon string, d storageDisplay) {
	draw.Draw(screen, image.Rect(2, y, 2+16, y+16), images.Load(icon), image.Point{}, draw.Src)
	if d.notMounted {
		write(screen, "not mounted", 22, y-3, 16, "lato-regular", false)
		return
	}
	write(screen, d.gb, 22, y-3, 16, "lato-regular", false)
	write(screen, d.percent, 104, y-3, 16, "lato-regular", false)
	if d.warn {
		draw.Draw(screen, image.Rect(140, y, 140+16, y+16), images.Load("warning"), image.Point{}, draw.Src)
	}
}

// drawStorage renders the sdcard + volume removable-media layout onto
// screen, one drawStorageRow per mount.
func drawStorage(screen draw.Image, sd, vol storageDisplay) {
	draw.Draw(screen, screen.Bounds(), image.Black, image.Point{}, draw.Src)
	drawStorageRow(screen, 10, "sdcard", sd)
	drawStorageRow(screen, 42, "hdd", vol)
}

// buildStorage allocates the removable-media screen and, unless demo, starts
// the goroutine that keeps it up to date. It satisfies the
// func(CmdLineOpts) draw.Image contract registerScreen expects (see init()
// below).
//
// Used space changes slowly, so a 5-minute poll is plenty. One goroutine
// owns both stat calls, so there's no shared state and no data lock beyond
// screenMu, which the fade carousel needs for its concurrent reads
// (functions.go's fadeStep).
func buildStorage(opts CmdLineOpts) draw.Image {
	screen := image.NewRGBA(fb.Bounds())

	if opts.Demo {
		sd := storageDisplay{gb: "23.4GB", percent: "45%"}
		vol := storageDisplay{gb: "897GB", percent: "92%", warn: true}
		drawStorage(screen, sd, vol)
		return screen
	}

	sd := statStorage("/sdcard")
	vol := statStorage("/volume")
	drawStorage(screen, sd, vol)

	go func() {
		for {
			time.Sleep(5 * time.Minute)
			sd := statStorage("/sdcard")
			vol := statStorage("/volume")
			screenMu.Lock()
			drawStorage(screen, sd, vol)
			screenMu.Unlock()
		}
	}()

	return screen
}

// drawSystem renders CPU and memory usage on one row (icon + percent each),
// and local-disk usage on a second row (icon + used space + percent),
// following drawStorageRow's icon-plus-text layout.
func drawSystem(screen draw.Image, cpuPercent, memPercent string, disk storageDisplay) {
	draw.Draw(screen, screen.Bounds(), image.Black, image.Point{}, draw.Src)

	draw.Draw(screen, image.Rect(2, 10, 2+16, 10+16), images.Load("cpu"), image.Point{}, draw.Src)
	write(screen, cpuPercent, 22, 7, 16, "lato-regular", false)

	draw.Draw(screen, image.Rect(90, 10, 90+16, 10+16), images.Load("memory"), image.Point{}, draw.Src)
	write(screen, memPercent, 110, 7, 16, "lato-regular", false)

	draw.Draw(screen, image.Rect(2, 42, 2+16, 42+16), images.Load("hardDrive"), image.Point{}, draw.Src)
	write(screen, disk.gb, 22, 39, 16, "lato-regular", false)
	write(screen, disk.percent, 104, 39, 16, "lato-regular", false)
}

// cpuSampleWindow is how long cpu.Percent averages over. It doubles as
// statSystem's effective refresh cadence, since buildSystem's loop calls it
// with no additional sleep.
const cpuSampleWindow = 3 * time.Second

// statSystem samples CPU, memory, and local-disk usage and formats them for
// display. A failed CPU or memory read (e.g. unsupported OS) shows dashes
// rather than a stale or zeroed reading, matching statStorage.
func statSystem() (cpuText, memText string, disk storageDisplay) {
	cpuText, memText = "--%", "--%"
	if pct, err := cpu.Percent(cpuSampleWindow); err == nil {
		cpuText = formatPercent(pct)
	}
	if pct, err := memory.Percent(); err == nil {
		memText = formatPercent(pct)
	}
	disk = statStorage("/")
	return cpuText, memText, disk
}

// buildSystem allocates the CPU/memory/local-disk screen and, unless demo,
// starts the goroutine that keeps it up to date. It satisfies the
// func(CmdLineOpts) draw.Image contract registerScreen expects (see init()
// below).
//
// cpu.Percent blocks for cpuSampleWindow to take its measurement, which
// doubles as this loop's refresh cadence — no separate sleep needed. One
// goroutine owns every stat call, so there's no shared state and no data
// lock beyond screenMu, which the fade carousel needs for its concurrent
// reads (functions.go's fadeStep).
func buildSystem(opts CmdLineOpts) draw.Image {
	screen := image.NewRGBA(fb.Bounds())

	if opts.Demo {
		drawSystem(screen, "56%", "34%", storageDisplay{gb: "45GB", percent: "34%"})
		return screen
	}

	cpuText, memText, disk := statSystem()
	drawSystem(screen, cpuText, memText, disk)

	go func() {
		for {
			cpuText, memText, disk := statSystem()
			screenMu.Lock()
			drawSystem(screen, cpuText, memText, disk)
			screenMu.Unlock()
		}
	}()

	return screen
}

// buildHost allocates the hostname + date/time screen and starts the single
// goroutine that keeps it up to date. It satisfies the func(CmdLineOpts)
// draw.Image contract registerScreen expects (see init() below).
//
// One goroutine owns everything here, so there is no shared state and no data
// lock. Its drawHost call still races the fade carousel's reads of this image
// (functions.go's fadeStep), so that call is held under screenMu. The initial
// draw runs before the image is reachable from any other goroutine, so it
// needs no lock.
func buildHost(opts CmdLineOpts) draw.Image {
	screen := image.NewRGBA(fb.Bounds())
	hostname := "cloudkey-gen2.local"

	drawHost(screen, hostname, time.Now())

	// Redraw every 30s so the displayed HH:MM stays current. os.Hostname() is
	// a cheap local syscall, so it's re-read on the same tick rather than run
	// on its own slower loop — one goroutine, nothing shared to guard.
	go func() {
		for {
			if !opts.Demo {
				hostname, _ = os.Hostname()
			}
			screenMu.Lock()
			drawHost(screen, hostname, time.Now())
			screenMu.Unlock()
			time.Sleep(30 * time.Second)
		}
	}()

	return screen
}

// buildNetwork allocates the LAN + WAN address screen and, unless demo, starts
// the single goroutine that refreshes both. It satisfies the
// func(CmdLineOpts) draw.Image contract registerScreen expects (see init()
// below).
//
// Both addresses change slowly — LAN rarely, WAN only via a network round-trip
// — so one hourly goroutine refreshes them together. A single owner means no
// shared state and no data lock; only the drawNetwork call needs screenMu,
// since the fade carousel reads this image concurrently (functions.go's
// fadeStep). The LAN address is resolved synchronously for the first frame (a
// cheap local lookup); the WAN address shows "checking..." until the first
// round-trip returns, rather than a placeholder that looks like a real address.
func buildNetwork(opts CmdLineOpts) draw.Image {
	screen := image.NewRGBA(fb.Bounds())
	lan := "checking..."
	wan := "checking..."

	if opts.Demo {
		lan = "192.168.10.111"
		wan = "203.0.113.32"
		drawNetwork(screen, lan, wan)
		return screen
	}

	if l, err := network.LANIP(); err == nil && l != "" {
		lan = l
	}
	drawNetwork(screen, lan, wan)

	go func() {
		for {
			if l, err := network.LANIP(); err == nil && l != "" {
				lan = l
			}
			if w, err := network.WANIP(); err == nil {
				log.Info().Str("wan_ip", w).Msg("found external IP address")
				wan = w
			}
			screenMu.Lock()
			drawNetwork(screen, lan, wan)
			screenMu.Unlock()
			time.Sleep(59 * time.Minute)
		}
	}()

	return screen
}

// buildSpeedTest allocates the speedtest screen and, unless demo, starts the
// goroutines that keep it up to date. It satisfies the func(CmdLineOpts)
// draw.Image contract registerScreen expects (see init() below).
//
// The 10-second loop below redraws the screen on every tick (to keep the
// "N minutes ago" timestamp current even between speed tests), so its call
// to drawSpeedTest must hold screenMu against the fade carousel's concurrent
// reads (functions.go's fadeStep). The hourly loop only ever writes dmsg/umsg
// behind mu — it never touches the screen image directly, so it needs no
// screenMu.
func buildSpeedTest(opts CmdLineOpts) draw.Image {
	dmsg := "calculating..."
	umsg := "calculating..."
	tmsg := "in progress"

	download := make(chan int)
	upload := make(chan int)
	lastcheck := time.Now()

	// mu guards dmsg, umsg, and lastcheck, which are written by the hourly
	// speed-test goroutine and read by the 10-second refresh goroutine.
	var mu sync.Mutex

	screen := image.NewRGBA(fb.Bounds())

	if opts.Demo {
		dmsg = "86.1 Mb/s"
		umsg = "43.9 Mb/s"
		tmsg = "25 minutes ago"
		drawSpeedTest(screen, dmsg, umsg, tmsg)
		return screen
	}

	drawSpeedTest(screen, dmsg, umsg, tmsg)

	client := speedtest.NewClient(&speedtest.Opts{})

	// Redraw every 10s so the "N minutes ago" line stays current between the
	// hourly tests; it only re-renders the last results, it never runs a test.
	go func() {
		for {
			mu.Lock()
			d, u, lc := dmsg, umsg, lastcheck
			mu.Unlock()

			tmsg = humanize.Time(lc)
			screenMu.Lock()
			drawSpeedTest(screen, d, u, tmsg)
			screenMu.Unlock()
			time.Sleep(10 * time.Second)
		}
	}()

	// Run the actual speed test hourly — it saturates the link for several
	// seconds, so it's kept infrequent and off the redraw path above. It
	// blinks the blue LED while running and writes results back under mu.
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

	return screen
}

// statusPollInterval is how often the autossh/wireguard/tailscale screens
// re-check their connection status. All three checks are local shell-outs
// (systemctl, wg, tailscale) rather than network round-trips, so this can
// be much shorter than the network/storage screens' polls without
// meaningful cost.
const statusPollInterval = 15 * time.Second

// iconTextRow is one icon+label row for a block of rows drawn by
// drawIconRows.
type iconTextRow struct {
	icon string
	text string
	bold bool
}

// drawIconRows draws a block of icon+text rows top-to-bottom starting at
// startY, each rowHeight apart. All rows share one left edge, chosen so the
// widest row is horizontally centered on screen — so icons stay aligned to a
// single column regardless of how the row texts differ in length, rather
// than each row re-centering independently and shifting its icon sideways.
func drawIconRows(screen draw.Image, rows []iconTextRow, startY, rowHeight int, size float64, fontname string) {
	const iconSize = 16
	const gap = 4

	maxWidth := 0
	for _, r := range rows {
		w, ok := textWidth(r.text, size, fontname)
		if !ok {
			continue
		}
		if total := iconSize + gap + w; total > maxWidth {
			maxWidth = total
		}
	}
	x := screen.Bounds().Max.X/2 - maxWidth/2

	y := startY
	for _, r := range rows {
		draw.Draw(screen, image.Rect(x, y, x+iconSize, y+iconSize), images.Load(r.icon), image.Point{}, draw.Src)
		write(screen, r.text, x+iconSize+gap, y-3, size, fontname, r.bold)
		y += rowHeight
	}
}

// tunnelStatus is one row of the autossh screen: a tunnel's label and
// whether its systemd unit is currently active.
type tunnelStatus struct {
	name string
	up   bool
}

// statAutoSSH checks every configured tunnel's systemd unit state.
//
// A tunnel is configured when its name is set; an unset service name
// always reports down rather than checking an empty unit. is-active only
// proves the autossh/ssh process supervisor considers itself running, not
// that the tunnel is actually passing traffic — this is a deliberate
// caveat, not an oversight: autossh -R forwards bind no local port to
// probe (the forwarded port lives on the remote end), so without a -M
// monitor port there's no more precise local signal available. Pair this
// with `ServerAliveInterval`/`ServerAliveCountMax` in the tunnel's ssh
// config so a genuinely dead connection makes the process exit (and the
// unit go inactive) rather than hang open indefinitely.
func statAutoSSH(opts CmdLineOpts) []tunnelStatus {
	var tunnels []tunnelStatus
	if opts.AutoSSHTunnel1Name != "" {
		tunnels = append(tunnels, tunnelStatus{
			name: opts.AutoSSHTunnel1Name,
			up:   autoSSHTunnelActive(opts.AutoSSHTunnel1Service),
		})
	}
	if opts.AutoSSHTunnel2Name != "" {
		tunnels = append(tunnels, tunnelStatus{
			name: opts.AutoSSHTunnel2Name,
			up:   autoSSHTunnelActive(opts.AutoSSHTunnel2Service),
		})
	}
	return tunnels
}

// autoSSHTunnelActive reports whether service is an active systemd unit.
// An unconfigured (empty) service name always reports down, and a check
// failure (e.g. the unit doesn't exist) is logged and treated as down
// rather than propagated — a redraw loop has nowhere to surface an error.
func autoSSHTunnelActive(service string) bool {
	if service == "" {
		return false
	}
	up, err := systemd.IsActive(service)
	if err != nil {
		log.Warn().Err(err).Str("service", service).Msg("autossh tunnel status check failed")
	}
	return up
}

// drawAutoSSH renders the autossh layout onto screen: a bold centered
// title, then one aligned icon+label row per tunnel (one or two — see
// statAutoSSH).
func drawAutoSSH(screen draw.Image, tunnels []tunnelStatus) {
	draw.Draw(screen, screen.Bounds(), image.Black, image.Point{}, draw.Src)
	center(screen, "autossh", 4, 16, "lato-regular", true)

	rows := make([]iconTextRow, len(tunnels))
	for i, t := range tunnels {
		icon := "noEntry"
		if t.up {
			icon = "check"
		}
		rows[i] = iconTextRow{icon: icon, text: t.name}
	}

	switch len(rows) {
	case 1:
		drawIconRows(screen, rows, 38, 0, 16, "lato-regular")
	case 2:
		drawIconRows(screen, rows, 26, 20, 16, "lato-regular")
	}
}

// buildAutoSSH allocates the autossh tunnel-status screen and, unless demo,
// starts the goroutine that keeps it up to date. It satisfies the
// func(CmdLineOpts) draw.Image contract registerScreen expects (see init()
// below).
//
// One goroutine owns every check, so there's no shared state and no data
// lock beyond screenMu, which the fade carousel needs for its concurrent
// reads (functions.go's fadeStep).
func buildAutoSSH(opts CmdLineOpts) draw.Image {
	screen := image.NewRGBA(fb.Bounds())

	if opts.Demo {
		drawAutoSSH(screen, []tunnelStatus{
			{name: "tunnel1", up: true},
			{name: "tunnel2", up: false},
		})
		return screen
	}

	tunnels := statAutoSSH(opts)
	drawAutoSSH(screen, tunnels)

	go func() {
		for {
			time.Sleep(statusPollInterval)
			tunnels := statAutoSSH(opts)
			screenMu.Lock()
			drawAutoSSH(screen, tunnels)
			screenMu.Unlock()
		}
	}()

	return screen
}

// vpnTitleSize is the larger title size the wireguard/tailscale screens use
// (the mockup's "[large]" title), bigger than autossh's plain 16px title.
const vpnTitleSize = 20

// drawVPNStatus renders a single connected/disconnected VPN screen: a large
// bold centered title (the VPN's display name) and one icon+status row
// below it. It's shared by wireguard and tailscale, which differ only in
// title text and how up is determined.
func drawVPNStatus(screen draw.Image, name string, up bool) {
	draw.Draw(screen, screen.Bounds(), image.Black, image.Point{}, draw.Src)
	center(screen, name, 4, vpnTitleSize, "lato-regular", true)

	icon, text := "noEntry", "disconnected"
	if up {
		icon, text = "check", "connected"
	}
	drawIconRows(screen, []iconTextRow{{icon: icon, text: text, bold: up}}, 40, 0, 16, "lato-regular")
}

// buildWireGuard allocates the WireGuard connection-status screen and,
// unless demo, starts the goroutine that keeps it up to date. It satisfies
// the func(CmdLineOpts) draw.Image contract registerScreen expects (see
// init() below).
//
// wireguard.Connected reads kernel WireGuard state via `wg show` rather
// than blocking on the network, so a short poll is cheap. One goroutine
// owns the check, so there's no shared state and no data lock beyond
// screenMu, which the fade carousel needs for its concurrent reads
// (functions.go's fadeStep).
func buildWireGuard(opts CmdLineOpts) draw.Image {
	screen := image.NewRGBA(fb.Bounds())

	if opts.Demo {
		drawVPNStatus(screen, opts.WireGuardName, true)
		return screen
	}

	up, err := wireguard.Connected(opts.WireGuardCmd, opts.WireGuardIface)
	if err != nil {
		log.Warn().Err(err).Str("iface", opts.WireGuardIface).Msg("wireguard status check failed")
	}
	drawVPNStatus(screen, opts.WireGuardName, up)

	go func() {
		for {
			time.Sleep(statusPollInterval)
			up, err := wireguard.Connected(opts.WireGuardCmd, opts.WireGuardIface)
			if err != nil {
				log.Warn().Err(err).Str("iface", opts.WireGuardIface).Msg("wireguard status check failed")
			}
			screenMu.Lock()
			drawVPNStatus(screen, opts.WireGuardName, up)
			screenMu.Unlock()
		}
	}()

	return screen
}

// buildTailscale allocates the Tailscale connection-status screen and,
// unless demo, starts the goroutine that keeps it up to date. It satisfies
// the func(CmdLineOpts) draw.Image contract registerScreen expects (see
// init() below).
//
// tailscale.Connected shells out to `tailscale status --json`, a local IPC
// call to tailscaled rather than a network round-trip, so a short poll is
// cheap. One goroutine owns the check, so there's no shared state and no
// data lock beyond screenMu, which the fade carousel needs for its
// concurrent reads (functions.go's fadeStep).
func buildTailscale(opts CmdLineOpts) draw.Image {
	screen := image.NewRGBA(fb.Bounds())

	if opts.Demo {
		drawVPNStatus(screen, opts.TailscaleName, true)
		return screen
	}

	up, err := tailscale.Connected(opts.TailscaleCmd)
	if err != nil {
		log.Warn().Err(err).Msg("tailscale status check failed")
	}
	drawVPNStatus(screen, opts.TailscaleName, up)

	go func() {
		for {
			time.Sleep(statusPollInterval)
			up, err := tailscale.Connected(opts.TailscaleCmd)
			if err != nil {
				log.Warn().Err(err).Msg("tailscale status check failed")
			}
			screenMu.Lock()
			drawVPNStatus(screen, opts.TailscaleName, up)
			screenMu.Unlock()
		}
	}()

	return screen
}

// init registers every screen in this file with the carousel. registerScreen
// (display.go) just appends to the package-level registry; New() builds
// whatever's in it, in this order, when the CLI options say it's enabled.
//
// To add a new screen:
//
//  1. Write drawX(screen draw.Image, ...) — pure rendering, no allocation, no
//     goroutines. It always redraws from a blank background (see drawHost's
//     doc comment for why partial redraws are unsafe here). Model it on
//     drawHost/drawNetwork/drawSpeedTest above.
//  2. Write buildX(opts CmdLineOpts) draw.Image — allocates the screen with
//     image.NewRGBA(fb.Bounds()), draws the first frame synchronously (no
//     lock needed yet — nothing else can see the image), starts whatever
//     goroutine(s) keep redrawing it on a schedule, and returns the image.
//     Every drawX call made from inside one of those goroutines MUST be
//     wrapped in screenMu.Lock()/Unlock(): the fade carousel reads this same
//     image concurrently once it's in rotation (see functions.go's
//     fadeStep). Model it on buildHost above — it's the simplest of the
//     three.
//  3. Call registerScreen("name", enabledFn, buildX) below. Use
//     alwaysEnabled unless the screen should only appear behind a CLI flag,
//     in which case pass a predicate like the speedtest entry does.
//
// New() in display.go never needs to change — it just walks the registry.
func init() {
	registerScreen("host", alwaysEnabled, buildHost)
	registerScreen("network", alwaysEnabled, buildNetwork)
	registerScreen("storage", alwaysEnabled, buildStorage)
	registerScreen("system", alwaysEnabled, buildSystem)
	registerScreen("speedtest", func(o CmdLineOpts) bool { return o.SpeedTest }, buildSpeedTest)
	registerScreen("autossh", func(o CmdLineOpts) bool { return o.AutoSSHTunnel1Name != "" }, buildAutoSSH)
	registerScreen("wireguard", func(o CmdLineOpts) bool { return o.WireGuardIface != "" }, buildWireGuard)
	registerScreen("tailscale", func(o CmdLineOpts) bool { return o.Tailscale }, buildTailscale)
}
