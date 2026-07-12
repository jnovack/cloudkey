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
		{"local", func(s draw.Image) { drawLocal(s, "cloudkey-gen2.local", "192.168.10.111") }},
		{"remote", func(s draw.Image) { drawRemote(s, time.Date(2026, 7, 11, 14, 32, 0, 0, time.UTC), "203.0.113.32") }},
		{"speedtest", func(s draw.Image) { drawSpeedTest(s, "86.10 Mb", "43.90 Mb", "25 minutes ago") }},
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
