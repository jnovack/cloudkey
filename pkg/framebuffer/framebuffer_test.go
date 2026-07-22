/*
framebuffer - access Linux framebuffer as draw.Image
Written in 2016 by <Ahmet Inan> <xdsopl@googlemail.com>
Linted in 2018 by <Justin J. Novack> <jnovack@gmail.com>
To the extent possible under law, the author(s) have dedicated all copyright and related and neighboring rights to this software to the public domain worldwide. This software is distributed without any warranty.
You should have received a copy of the CC0 Public Domain Dedication along with this software. If not, see <http://creativecommons.org/publicdomain/zero/1.0/>.
*/

package framebuffer

import (
	"image"
	"image/color"
	"os"
	"path/filepath"
	"testing"
)

// TestOpenReleasesDescriptorOnError guards against the fd/mmap leak in Open:
// every return path used to skip closing the file, so repeated failed calls
// (e.g. a hot-plug retry loop) would exhaust the process's descriptor table.
//
// A regular file fails the FBIOGET_FSCREENINFO ioctl on every platform (it
// isn't a framebuffer device), so Open always takes an error return here —
// exercising exactly the paths that used to leak.
//
// fd numbers are used as the leak signal because POSIX guarantees open()
// returns the lowest-numbered available descriptor: if Open leaks, each
// iteration consumes one, and a probe fd opened after the loop lands higher
// than one opened before it. This avoids relying on /proc/self/fd, which
// doesn't exist on macOS.
func TestOpenReleasesDescriptorOnError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fake-fb")
	if err := os.WriteFile(path, []byte("not a framebuffer"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	before, err := os.Open(path)
	if err != nil {
		t.Fatalf("open probe (before): %v", err)
	}
	fdBefore := before.Fd()
	if err := before.Close(); err != nil {
		t.Fatalf("close probe (before): %v", err)
	}

	const iterations = 200
	for i := 0; i < iterations; i++ {
		if _, err := Open(path); err == nil {
			t.Fatalf("Open(%q) unexpectedly succeeded on iteration %d; test assumes a regular file always fails the framebuffer ioctl", path, i)
		}
	}

	after, err := os.Open(path)
	if err != nil {
		t.Fatalf("open probe (after): %v", err)
	}
	fdAfter := after.Fd()
	if err := after.Close(); err != nil {
		t.Fatalf("close probe (after): %v", err)
	}

	if fdAfter > fdBefore {
		t.Errorf("descriptor number grew from %d to %d after %d failed Open calls; Open is leaking file descriptors", fdBefore, fdAfter, iterations)
	}
}

// pixImage is the subset of draw.Image the four pixel-format types below
// share, letting one table-driven test exercise Set/At round-trips and
// out-of-bounds behavior for all of them.
type pixImage interface {
	Bounds() image.Rectangle
	Set(x, y int, c color.Color)
	At(x, y int) color.Color
}

// TestPixelFormatsRoundTripAndGuardBounds covers the Set/At bit-packing
// logic in BGR565, BGR, BGR32, and NBGRA — previously exercised only via
// Open's ioctl-gated switch, never directly. It round-trips an in-bounds
// Set/At pair and confirms Set outside Rect is a silent no-op (no panic,
// no corruption of neighboring pixels) and At outside Rect returns the
// implementation's documented zero value, color.NRGBA{}.
func TestPixelFormatsRoundTripAndGuardBounds(t *testing.T) {
	rect := image.Rect(0, 0, 2, 2)

	cases := []struct {
		name string
		img  func() pixImage
		// in is the color Set at the in-bounds point; want is what At should
		// return for it. BGR565 loses precision (5/6/5 bits), so its case
		// uses a color that is exactly representable at that precision
		// (R,B multiples of 8; G a multiple of 4) rather than expecting an
		// exact round-trip of an arbitrary 8-bit color.
		in   color.Color
		want color.NRGBA
	}{
		{
			name: "BGR565",
			img:  func() pixImage { return &BGR565{Pix: make([]uint8, 2*2*2), Stride: 2 * 2, Rect: rect} },
			in:   color.NRGBA{R: 200, G: 204, B: 200, A: 255},
			want: color.NRGBA{R: 200, G: 204, B: 200, A: 255},
		},
		{
			name: "BGR",
			img:  func() pixImage { return &BGR{Pix: make([]uint8, 2*2*3), Stride: 2 * 3, Rect: rect} },
			in:   color.NRGBA{R: 10, G: 20, B: 30, A: 255},
			want: color.NRGBA{R: 10, G: 20, B: 30, A: 255},
		},
		{
			name: "BGR32",
			img:  func() pixImage { return &BGR32{Pix: make([]uint8, 2*2*4), Stride: 2 * 4, Rect: rect} },
			in:   color.NRGBA{R: 10, G: 20, B: 30, A: 255},
			want: color.NRGBA{R: 10, G: 20, B: 30, A: 255},
		},
		{
			name: "NBGRA",
			img:  func() pixImage { return &NBGRA{Pix: make([]uint8, 2*2*4), Stride: 2 * 4, Rect: rect} },
			in:   color.NRGBA{R: 10, G: 20, B: 30, A: 128},
			want: color.NRGBA{R: 10, G: 20, B: 30, A: 128},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			img := c.img()

			img.Set(1, 1, c.in)
			if got := img.At(1, 1); got != c.want {
				t.Errorf("At(1,1) after Set = %+v, want %+v", got, c.want)
			}

			// Snapshot Pix before an out-of-bounds Set so we can prove it left
			// every byte untouched, not just that it didn't panic.
			before := imgPix(t, img)

			// Out-of-bounds Set must be a silent no-op.
			img.Set(5, 5, color.NRGBA{R: 1, G: 2, B: 3, A: 4})
			after := imgPix(t, img)
			if string(before) != string(after) {
				t.Errorf("Set outside Rect modified Pix: before %v, after %v", before, after)
			}

			// Out-of-bounds At must return the zero value, not panic or read
			// out of range.
			if got := img.At(5, 5); got != (color.NRGBA{}) {
				t.Errorf("At outside Rect = %+v, want zero value", got)
			}
		})
	}
}

// imgPix extracts the backing Pix slice via a type switch, since pixImage
// deliberately doesn't expose it (Set/At/Bounds is the shared contract).
func imgPix(t *testing.T, img pixImage) []uint8 {
	t.Helper()
	switch v := img.(type) {
	case *BGR565:
		return append([]uint8(nil), v.Pix...)
	case *BGR:
		return append([]uint8(nil), v.Pix...)
	case *BGR32:
		return append([]uint8(nil), v.Pix...)
	case *NBGRA:
		return append([]uint8(nil), v.Pix...)
	default:
		t.Fatalf("imgPix: unsupported type %T", img)
		return nil
	}
}
