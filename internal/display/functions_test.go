package display

import (
	"image"
	"testing"
	"time"
)

// TestTextWidthReturnsNotOkForUnregisteredFont guards against a regression to
// the pre-fix behavior, where fonts.Load("no-such-font") returned nil and
// truetype.NewFace nil-dereferenced inside textWidth, panicking whatever
// redraw goroutine called center or drawIconRows with an unregistered font.
func TestTextWidthReturnsNotOkForUnregisteredFont(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("textWidth panicked for unregistered font: %v", r)
		}
	}()

	width, ok := textWidth("hello", 16, "no-such-font")
	if ok {
		t.Errorf("textWidth(unregistered font) ok = true, want false")
	}
	if width != 0 {
		t.Errorf("textWidth(unregistered font) width = %d, want 0", width)
	}
}

func TestPixelShift(t *testing.T) {
	epoch := time.Unix(0, 0).UTC()

	tests := []struct {
		name string
		t    time.Time
		want image.Point
	}{
		{"epoch", epoch, shiftOffsets[0]},
		{"just before first step", epoch.Add(shiftPeriod - time.Second), shiftOffsets[0]},
		{"first step", epoch.Add(shiftPeriod), shiftOffsets[1]},
		{"wraps after a full cycle", epoch.Add(time.Duration(len(shiftOffsets)) * shiftPeriod), shiftOffsets[0]},
		{"mid-cycle", epoch.Add(time.Duration(len(shiftOffsets)+2) * shiftPeriod), shiftOffsets[2]},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pixelShift(tt.t)
			if got != tt.want {
				t.Errorf("pixelShift(%v) = %v, want %v", tt.t, got, tt.want)
			}
		})
	}
}
