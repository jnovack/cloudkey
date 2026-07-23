package display

import (
	"context"
	"fmt"
	"image"
	"image/draw"
	"os"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/jnovack/cloudkey/internal/images"
	"github.com/jnovack/cloudkey/internal/state"
	"github.com/jnovack/cloudkey/pkg/cpu"
	"github.com/jnovack/cloudkey/pkg/host"
	"github.com/jnovack/cloudkey/pkg/memory"
	"github.com/jnovack/cloudkey/pkg/network"
	"github.com/jnovack/cloudkey/pkg/ping"
	"github.com/jnovack/cloudkey/pkg/storage"
	"github.com/jnovack/cloudkey/pkg/systemd"
	"github.com/jnovack/cloudkey/pkg/tailscale"
	"github.com/jnovack/cloudkey/pkg/wireguard"
)

// storageWarnThreshold is the percent-used a mount must exceed before
// drawStorageRow draws its warning icon.
const storageWarnThreshold = 90.0

// wanIPTimeout bounds a single network.WANIP round-trip in buildNetwork's
// hourly refresh, so a dead internet connection fails that iteration within
// seconds rather than leaving the goroutine blocked for the full interval
// between refreshes.
const wanIPTimeout = 10 * time.Second

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

// diskFor stats path once and returns both the OLED display state and the raw
// hub disk slice, so a screen goroutine feeds the panel and the web dashboard
// from a single storage.Stat call. storage.Stat can fail two distinct ways,
// both handled without panicking: path exists but nothing is mounted there
// (storage.ErrNotMounted, e.g. no SD card inserted), or any other error (e.g.
// the mount-point directory itself doesn't exist) — both render "not mounted"
// on the panel and Mounted:false to the web, rather than crashing the screen or
// showing a stale/zeroed reading.
func diskFor(path string) (storageDisplay, state.Disk) {
	u, err := storage.Stat(path)
	if err != nil {
		return storageDisplay{notMounted: true}, state.Disk{Mounted: false}
	}
	d := storageDisplay{
		gb:      formatGB(u.UsedBytes),
		percent: formatPercent(u.Percent),
		warn:    u.Percent > storageWarnThreshold,
	}
	return d, state.Disk{Mounted: true, Used: u.UsedBytes, Total: u.TotalBytes}
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
// the two bare IPs are told apart at a glance. It follows drawStorageRow's
// icon-plus-text row layout rather than the centered text of drawHost.
func drawNetwork(screen draw.Image, lan, wan string) {
	draw.Draw(screen, screen.Bounds(), image.Black, image.Point{}, draw.Src)
	draw.Draw(screen, image.Rect(2, 10, 2+16, 10+16), images.Load("host"), image.Point{}, draw.Src)
	draw.Draw(screen, image.Rect(2, 42, 2+16, 42+16), images.Load("internet"), image.Point{}, draw.Src)
	write(screen, lan, 22, 7, 16, "lato-regular", false)
	write(screen, wan, 22, 39, 16, "lato-regular", true)
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
		publishDisk("sdcard", state.Disk{Mounted: true, Used: 6900000000, Total: 31900000000})
		publishDisk("ssd", state.Disk{Mounted: true, Used: 897000000000, Total: 1000000000000})
		return screen
	}

	// The web dashboard names these mounts "sdcard" (/sdcard) and "ssd"
	// (/volume, the removable SSD/HDD bay), matching the panel's two rows.
	sd, sdRaw := diskFor("/sdcard")
	vol, volRaw := diskFor("/volume")
	drawStorage(screen, sd, vol)
	publishDisk("sdcard", sdRaw)
	publishDisk("ssd", volRaw)

	go func() {
		for {
			time.Sleep(5 * time.Minute)
			sd, sdRaw := diskFor("/sdcard")
			vol, volRaw := diskFor("/volume")
			screenMu.Lock()
			drawStorage(screen, sd, vol)
			screenMu.Unlock()
			publishDisk("sdcard", sdRaw)
			publishDisk("ssd", volRaw)
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

// systemStat is one reading of CPU, memory, and root-filesystem usage: the
// strings the panel draws plus the raw figures the web dashboard needs (bytes,
// not percentages). Gathering both from one sample avoids sampling the CPU
// twice — cpu.Percent blocks for cpuSampleWindow, so a second call would double
// this loop's period.
type systemStat struct {
	cpuText string
	memText string
	disk    storageDisplay

	cpuPct float64
	cpuOK  bool
	mem    state.Mem
	memOK  bool
	rootfs state.Disk
}

// statSystem samples CPU, memory, and root-filesystem usage once and returns
// both display and raw forms. A failed CPU or memory read (e.g. unsupported OS)
// shows dashes on the panel and is omitted from the web slice, rather than
// publishing a stale or zeroed reading.
func statSystem() systemStat {
	s := systemStat{cpuText: "--%", memText: "--%"}
	if pct, err := cpu.Percent(cpuSampleWindow); err == nil {
		s.cpuPct, s.cpuOK = pct, true
		s.cpuText = formatPercent(pct)
	}
	if used, total, err := memory.Stats(); err == nil && total > 0 {
		s.mem, s.memOK = state.Mem{Used: used, Total: total}, true
		s.memText = formatPercent(float64(used) / float64(total) * 100)
	}
	s.disk, s.rootfs = diskFor("/")
	return s
}

// publishSystem sends one systemStat's raw figures to the hub. CPU cores and
// load average are gathered here (cheap reads the panel doesn't need) only when
// a hub is attached, so a display-only run does no extra work.
func publishSystem(s systemStat) {
	if !hubEnabled() {
		return
	}
	c := state.CPU{Cores: cpu.Cores()}
	if s.cpuOK {
		c.Pct = s.cpuPct
	}
	if la, err := cpu.LoadAverage(); err == nil {
		c.LoadAvg = la
	}
	publishCPU(c)
	if s.memOK {
		publishMem(s.mem)
	}
	publishDisk("rootfs", s.rootfs)
}

// buildSystem allocates the CPU/memory/local-disk screen and, unless demo,
// starts the goroutine that keeps it up to date. It satisfies the
// func(CmdLineOpts) draw.Image contract registerScreen expects (see init()
// below).
//
// cpu.Percent normally blocks for cpuSampleWindow to take its measurement, and
// that block is this loop's refresh cadence. It is not a guarantee, though: on
// a read failure cpu.Percent returns before it ever sleeps, so systemLoopTick
// enforces cpuSampleWindow as a floor. Without it, an unreadable /proc/stat
// turns this into an unthrottled spin that pegs a core and saturates every SSE
// subscriber's buffer. One goroutine owns every stat call, so there's no shared
// state and no data lock beyond screenMu, which the fade carousel needs for its
// concurrent reads (functions.go's fadeStep).
func buildSystem(opts CmdLineOpts) draw.Image {
	screen := image.NewRGBA(fb.Bounds())

	if opts.Demo {
		drawSystem(screen, "56%", "34%", storageDisplay{gb: "45GB", percent: "34%"})
		publishCPU(state.CPU{Pct: 56, Cores: cpu.Cores(), LoadAvg: 0.62})
		publishMem(state.Mem{Used: 2630000000, Total: 4090000000})
		publishDisk("rootfs", state.Disk{Mounted: true, Used: 24900000000, Total: 31900000000})
		return screen
	}

	s := statSystem()
	drawSystem(screen, s.cpuText, s.memText, s.disk)
	publishSystem(s)

	go func() {
		for {
			systemLoopTick(statSystem, func(s systemStat) {
				screenMu.Lock()
				drawSystem(screen, s.cpuText, s.memText, s.disk)
				screenMu.Unlock()
				publishSystem(s)
			}, time.Sleep)
		}
	}()

	return screen
}

// systemLoopTick runs one iteration of buildSystem's redraw loop: stat, then
// redraw, then a floor sleep so a stat call that returns before cpuSampleWindow
// elapses (e.g. cpu.Percent returning immediately on a /proc/stat read
// failure, before it ever reaches its own internal sleep) can't spin the loop
// unthrottled. stat, redraw, and sleep are injected so tests can exercise the
// floor with a fake instantaneous stat and a fake sleep, instead of a real
// failing CPU read and a real multi-second wait.
func systemLoopTick(stat func() systemStat, redraw func(systemStat), sleep func(time.Duration)) {
	start := time.Now()
	s := stat()
	redraw(s)
	// Sleep only the remainder of the window: on the healthy path stat()
	// already consumed it and this is a no-op.
	if rest := cpuSampleWindow - time.Since(start); rest > 0 {
		sleep(rest)
	}
}

// resolveHostname runs lookup and returns its result, or prev unchanged on
// failure. os.Hostname's error path returns "" (its zero value), and blindly
// assigning that would blank the panel's title row and publish an empty name
// to the web on every transient lookup failure — so the previous good value is
// kept instead, and the failure is logged since a redraw loop has nowhere else
// to surface it.
func resolveHostname(prev string, lookup func() (string, error)) string {
	h, err := lookup()
	if err != nil {
		log.Warn().Err(err).Msg("hostname lookup failed, keeping previous value")
		return prev
	}
	return h
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

	// OS pretty-name and architecture are static for the life of the process,
	// so they're read once here rather than on every tick.
	osName, arch := host.OSName(), host.Arch()

	// Redraw every 30s so the displayed HH:MM stays current. os.Hostname() is
	// a cheap local syscall, so it's re-read on the same tick rather than run
	// on its own slower loop — one goroutine, nothing shared to guard.
	go func() {
		for {
			if !opts.Demo {
				hostname = resolveHostname(hostname, os.Hostname)
			}
			screenMu.Lock()
			drawHost(screen, hostname, time.Now())
			screenMu.Unlock()

			h := state.Host{Name: hostname, OS: osName, Arch: arch}
			if up, err := host.Uptime(); err == nil {
				h.UptimeSec = int64(up.Seconds())
			}
			publishHost(h)

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
//
// The WAN lookup is bounded by wanIPTimeout so a dead internet connection
// fails that iteration within seconds instead of leaving the round-trip
// outstanding for the full hour between refreshes; on failure the screen
// shows "unreachable" rather than a stale address that would otherwise look
// current.
func buildNetwork(opts CmdLineOpts) draw.Image {
	screen := image.NewRGBA(fb.Bounds())
	lan := "checking..."
	wan := "checking..."

	if opts.Demo {
		lan = "192.168.10.111"
		wan = "203.0.113.32"
		drawNetwork(screen, lan, wan)
		publishNet(state.Net{LAN: lan, WAN: wan})
		return screen
	}

	if l, err := network.LANIP(); err == nil && l != "" {
		lan = l
	}
	drawNetwork(screen, lan, wan)
	publishNet(state.Net{LAN: lan, WAN: wan})

	go func() {
		for {
			if l, err := network.LANIP(); err == nil && l != "" {
				lan = l
			}
			ctx, cancel := context.WithTimeout(context.Background(), wanIPTimeout)
			w, err := network.WANIP(ctx)
			cancel()
			if err == nil {
				log.Info().Str("wan_ip", w).Msg("found external IP address")
				wan = w
			} else {
				log.Warn().Err(err).Msg("failed to resolve external IP address")
				wan = "unreachable"
			}
			screenMu.Lock()
			drawNetwork(screen, lan, wan)
			screenMu.Unlock()
			publishNet(state.Net{LAN: lan, WAN: wan})
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

// tunnelStatus is one row of the autossh screen: a tunnel's label, the systemd
// unit backing it, and whether that unit is currently active. The service is
// carried so the web publisher can derive connected-time without a second
// is-active check.
type tunnelStatus struct {
	name    string
	service string
	up      bool
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
			name:    opts.AutoSSHTunnel1Name,
			service: opts.AutoSSHTunnel1Service,
			up:      autoSSHTunnelActive(opts.AutoSSHTunnel1Service),
		})
	}
	if opts.AutoSSHTunnel2Name != "" {
		tunnels = append(tunnels, tunnelStatus{
			name:    opts.AutoSSHTunnel2Name,
			service: opts.AutoSSHTunnel2Service,
			up:      autoSSHTunnelActive(opts.AutoSSHTunnel2Service),
		})
	}
	return tunnels
}

// publishAutoSSH sends each autossh tunnel's state to the hub, reusing the
// is-active result the panel already computed. For an up tunnel it derives
// connected-time from the unit's ActiveEnterTimestampMonotonic and current
// uptime (same CLOCK_MONOTONIC epoch); rx/tx/ping stay zero because an autossh
// -R forward exposes no local interface to measure (see statAutoSSH). Because
// those three are structurally unavailable, the dashboard gives autossh its own
// one-line section rather than the RX/TX/ping card the VPN tunnels use — name,
// up, and conn are the whole payload.
func publishAutoSSH(tunnels []tunnelStatus) {
	if !hubEnabled() {
		return
	}
	for _, t := range tunnels {
		tun := state.Tunnel{Name: t.name, Type: "AUTOSSH", Up: t.up}
		if t.up && t.service != "" {
			if active, err := systemd.ActiveEnterMonotonic(t.service); err == nil && active > 0 {
				if up, err := host.Uptime(); err == nil && up > active {
					tun.Conn = int64((up - active).Seconds())
				}
			}
		}
		publishTunnel(tun)
	}
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
		drawIconRows(screen, rows, 24, 19, 16, "lato-regular")
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
		demo := []tunnelStatus{
			{name: "tunnel1", up: true},
			{name: "tunnel2", up: false},
		}
		drawAutoSSH(screen, demo)
		publishTunnel(state.Tunnel{Name: "tunnel1", Type: "AUTOSSH", Up: true, Conn: 3*86400 + 6*3600})
		publishTunnel(state.Tunnel{Name: "tunnel2", Type: "AUTOSSH", Up: false, LastSeen: "2m ago"})
		return screen
	}

	tunnels := statAutoSSH(opts)
	drawAutoSSH(screen, tunnels)
	publishAutoSSH(tunnels)

	go func() {
		for {
			time.Sleep(statusPollInterval)
			tunnels := statAutoSSH(opts)
			screenMu.Lock()
			drawAutoSSH(screen, tunnels)
			screenMu.Unlock()
			publishAutoSSH(tunnels)
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

// wgStat runs one WireGuard status check, logging and returning a zero (down)
// Status on error so both the panel and the hub degrade to "disconnected"
// rather than propagating an error a redraw loop can't surface.
func wgStat(opts CmdLineOpts) wireguard.Status {
	s, err := wireguard.Stat(opts.WireGuardCmd, opts.WireGuardIface)
	if err != nil {
		log.Warn().Err(err).Str("iface", opts.WireGuardIface).Msg("wireguard status check failed")
	}
	return s
}

// publishWireGuard sends one WireGuard reading to the hub, pinging the peer
// endpoint for latency only when the tunnel is up and a hub is attached (the
// only case that consumes the value), so a display-only run spawns no ping.
func publishWireGuard(opts CmdLineOpts, s wireguard.Status) {
	if !hubEnabled() {
		return
	}
	t := state.Tunnel{Name: opts.WireGuardName, Type: "WIREGUARD", Up: s.Up, Rx: s.Rx, Tx: s.Tx}
	if s.Up && s.Endpoint != "" {
		if rtt, err := ping.RTT(s.Endpoint); err == nil {
			t.Ping = float64(rtt) / float64(time.Millisecond)
		}
	}
	publishTunnel(t)
}

// buildWireGuard allocates the WireGuard connection-status screen and,
// unless demo, starts the goroutine that keeps it up to date. It satisfies
// the func(CmdLineOpts) draw.Image contract registerScreen expects (see
// init() below).
//
// wireguard.Stat reads WireGuard state via `wg show <iface>` rather than
// blocking on the network, so a short poll is cheap. The same call feeds the
// panel's up/down icon and the web tunnel's rx/tx, so status is gathered once
// per tick. One goroutine owns the check, so there's no shared state and no
// data lock beyond screenMu, which the fade carousel needs for its concurrent
// reads (functions.go's fadeStep).
func buildWireGuard(opts CmdLineOpts) draw.Image {
	screen := image.NewRGBA(fb.Bounds())

	if opts.Demo {
		drawVPNStatus(screen, opts.WireGuardName, true)
		publishTunnel(state.Tunnel{Name: opts.WireGuardName, Type: "WIREGUARD", Up: true, Rx: 18600000000, Tx: 4100000000, Ping: 88})
		return screen
	}

	s := wgStat(opts)
	drawVPNStatus(screen, opts.WireGuardName, s.Up)
	publishWireGuard(opts, s)

	go func() {
		for {
			time.Sleep(statusPollInterval)
			s := wgStat(opts)
			screenMu.Lock()
			drawVPNStatus(screen, opts.WireGuardName, s.Up)
			screenMu.Unlock()
			publishWireGuard(opts, s)
		}
	}()

	return screen
}

// tsStat runs one Tailscale status check, logging and returning a zero (down)
// Status on error so both the panel and the hub degrade to "disconnected".
func tsStat(opts CmdLineOpts) tailscale.Status {
	s, err := tailscale.Stat(opts.TailscaleCmd)
	if err != nil {
		log.Warn().Err(err).Msg("tailscale status check failed")
	}
	return s
}

// publishTailscale sends one Tailscale reading to the hub, pinging an online
// peer's tailnet address for latency only when up and a hub is attached.
func publishTailscale(opts CmdLineOpts, s tailscale.Status) {
	if !hubEnabled() {
		return
	}
	t := state.Tunnel{Name: opts.TailscaleName, Type: "TAILSCALE", Up: s.Up, Rx: s.Rx, Tx: s.Tx}
	if s.Up && s.PingAddr != "" {
		if rtt, err := ping.RTT(s.PingAddr); err == nil {
			t.Ping = float64(rtt) / float64(time.Millisecond)
		}
	}
	publishTunnel(t)
}

// buildTailscale allocates the Tailscale connection-status screen and,
// unless demo, starts the goroutine that keeps it up to date. It satisfies
// the func(CmdLineOpts) draw.Image contract registerScreen expects (see
// init() below).
//
// tailscale.Stat shells out to `tailscale status --json`, a local IPC call to
// tailscaled rather than a network round-trip, so a short poll is cheap. The
// one call feeds both the panel's up/down icon and the web tunnel's rx/tx. One
// goroutine owns the check, so there's no shared state and no data lock beyond
// screenMu, which the fade carousel needs for its concurrent reads
// (functions.go's fadeStep).
func buildTailscale(opts CmdLineOpts) draw.Image {
	screen := image.NewRGBA(fb.Bounds())

	if opts.Demo {
		drawVPNStatus(screen, opts.TailscaleName, true)
		publishTunnel(state.Tunnel{Name: opts.TailscaleName, Type: "TAILSCALE", Up: true, Rx: 620000000, Tx: 210000000, Ping: 12})
		return screen
	}

	s := tsStat(opts)
	drawVPNStatus(screen, opts.TailscaleName, s.Up)
	publishTailscale(opts, s)

	go func() {
		for {
			time.Sleep(statusPollInterval)
			s := tsStat(opts)
			screenMu.Lock()
			drawVPNStatus(screen, opts.TailscaleName, s.Up)
			screenMu.Unlock()
			publishTailscale(opts, s)
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
//     drawHost/drawNetwork/drawStorage above.
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
//     in which case pass a predicate like the wireguard entry does.
//
// New() in display.go never needs to change — it just walks the registry.
func init() {
	registerScreen("host", alwaysEnabled, buildHost)
	registerScreen("network", alwaysEnabled, buildNetwork)
	registerScreen("storage", alwaysEnabled, buildStorage)
	registerScreen("system", alwaysEnabled, buildSystem)
	registerScreen("autossh", func(o CmdLineOpts) bool { return o.AutoSSHTunnel1Name != "" }, buildAutoSSH)
	registerScreen("wireguard", func(o CmdLineOpts) bool { return o.WireGuardIface != "" }, buildWireGuard)
	registerScreen("tailscale", func(o CmdLineOpts) bool { return o.Tailscale }, buildTailscale)
}
