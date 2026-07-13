package display

import (
	"image"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestPreviewScreens renders each screen layout with representative sample
// data and writes it as a PNG, so changes to fonts/sizing/dimming/shift can
// be eyeballed without real OLED hardware. Skipped unless CLOUDKEY_PREVIEW_DIR
// is set, since it writes files outside the test sandbox for manual review:
//
//	CLOUDKEY_PREVIEW_DIR=.local/preview go test ./internal/display -run TestPreviewScreens -v
func TestPreviewScreens(t *testing.T) {
	dir := os.Getenv("CLOUDKEY_PREVIEW_DIR")
	if dir == "" {
		t.Skip("set CLOUDKEY_PREVIEW_DIR to render preview PNGs")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	const w, h = 160, 64 // matches the panel resolution assumed elsewhere (speedtest's hardcoded redraw rect)

	cases := []struct {
		name string
		draw func(draw.Image)
	}{
		{"host", func(s draw.Image) { drawHost(s, "cloudkey-gen2.local", time.Date(2026, 7, 11, 14, 32, 0, 0, time.UTC)) }},
		{"network", func(s draw.Image) { drawNetwork(s, "192.168.10.111", "203.0.113.32") }},
		{"speedtest", func(s draw.Image) { drawSpeedTest(s, "86.10 Mb", "43.90 Mb", "25 minutes ago") }},
		{"storage", func(s draw.Image) {
			drawStorage(s,
				storageDisplay{gb: "23.4GB", percent: "45%"},
				storageDisplay{gb: "897GB", percent: "92%", warn: true},
			)
		}},
		{"storage-not-mounted", func(s draw.Image) {
			drawStorage(s,
				storageDisplay{notMounted: true},
				storageDisplay{notMounted: true},
			)
		}},
		{"system", func(s draw.Image) {
			drawSystem(s, "56%", "34%", storageDisplay{gb: "45GB", percent: "34%"})
		}},
		{"autossh-one-tunnel", func(s draw.Image) {
			drawAutoSSH(s, []tunnelStatus{{name: "primary", up: true}})
		}},
		{"autossh-two-tunnels", func(s draw.Image) {
			drawAutoSSH(s, []tunnelStatus{
				{name: "primary", up: true},
				{name: "backup", up: false},
			})
		}},
		{"wireguard-connected", func(s draw.Image) { drawVPNStatus(s, "WireGuard", true) }},
		{"wireguard-disconnected", func(s draw.Image) { drawVPNStatus(s, "WireGuard", false) }},
		{"tailscale-connected", func(s draw.Image) { drawVPNStatus(s, "TailScale", true) }},
		{"tailscale-disconnected", func(s draw.Image) { drawVPNStatus(s, "TailScale", false) }},
	}
	for _, c := range cases {
		img := image.NewRGBA(image.Rect(0, 0, w, h))
		c.draw(img)

		f, err := os.Create(filepath.Join(dir, "screen-"+c.name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, img); err != nil {
			f.Close()
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
