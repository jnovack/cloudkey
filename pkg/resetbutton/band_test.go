package resetbutton

import (
	"testing"
	"time"
)

// TestClassifyBandBoundaries pins every band edge. The intervals are half-open,
// [lo, hi), so each case below sits one millisecond either side of a boundary:
// a band that silently grew or shrank by a millisecond changes which action a
// press performs, and the 6-second ceiling in particular is a safety limit, not
// a tuning knob.
func TestClassifyBandBoundaries(t *testing.T) {
	cases := []struct {
		name string
		d    time.Duration
		want Band
	}{
		{"zero is debounce", 0, BandNone},
		{"just under debounce ceiling", 99 * time.Millisecond, BandNone},
		{"debounce ceiling starts shortpress", 100 * time.Millisecond, BandShortPress},
		{"just under shortpress ceiling", 499 * time.Millisecond, BandShortPress},
		{"shortpress ceiling is dead", 500 * time.Millisecond, BandNone},
		{"just under shorthold floor is dead", 999 * time.Millisecond, BandNone},
		{"shorthold floor starts shorthold", 1000 * time.Millisecond, BandShortHold},
		{"just under shorthold ceiling", 2499 * time.Millisecond, BandShortHold},
		{"shorthold ceiling is dead", 2500 * time.Millisecond, BandNone},
		{"just under longhold floor is dead", 3999 * time.Millisecond, BandNone},
		{"longhold floor starts longhold", 4000 * time.Millisecond, BandLongHold},
		{"just under longhold ceiling", 5999 * time.Millisecond, BandLongHold},
		{"longhold ceiling begins the danger window", 6000 * time.Millisecond, BandNone},
		{"deep in the danger window fires nothing", 8 * time.Second, BandNone},
		{"firmware-restore territory fires nothing", 10 * time.Second, BandNone},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := classify(c.d); got != c.want {
				t.Errorf("classify(%v) = %v, want %v", c.d, got, c.want)
			}
		})
	}
}

func TestBandString(t *testing.T) {
	cases := []struct {
		b    Band
		want string
	}{
		{BandNone, "none"},
		{BandShortPress, "shortpress"},
		{BandShortHold, "shorthold"},
		{BandLongHold, "longhold"},
		{Band(99), "none"},
	}
	for _, c := range cases {
		if got := c.b.String(); got != c.want {
			t.Errorf("Band(%d).String() = %q, want %q", c.b, got, c.want)
		}
	}
}
