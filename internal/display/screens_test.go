package display

import (
	"image"
	"testing"
)

// TestDrawAutoSSHTwoTunnelsDescendersFitOnScreen guards against a regression
// where the dual-tunnel autossh layout packed the second row so close to the
// bottom edge that descenders (g, j, p, q, y) on tunnel names were clipped
// by the 160x64 panel bounds. Names are drawn at the real panel resolution
// (matching the panel resolution asserted in preview_test.go) with the
// deepest-descending letters the font has, and the lowest lit row must stay
// off the final row so there's still a margin of unlit pixels below it.
func TestDrawAutoSSHTwoTunnelsDescendersFitOnScreen(t *testing.T) {
	const w, h = 160, 64
	screen := image.NewRGBA(image.Rect(0, 0, w, h))

	drawAutoSSH(screen, []tunnelStatus{
		{name: "gigabyte", up: true}, // g, y
		{name: "jumpgap", up: false}, // j, g, p (deepest descenders in lato-regular)
	})

	lowestLitRow := -1
	for y := range h {
		for x := range w {
			r, g, b, _ := screen.At(x, y).RGBA()
			if r != 0 || g != 0 || b != 0 {
				lowestLitRow = y
			}
		}
	}

	if lowestLitRow < 0 {
		t.Fatal("drawAutoSSH drew nothing")
	}
	if lowestLitRow >= h-1 {
		t.Errorf("lowest lit row = %d, want < %d (margin from bottom edge); descenders are being clipped", lowestLitRow, h-1)
	}
}
