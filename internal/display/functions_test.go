package display

import (
	"image"
	"testing"
	"time"
)

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
