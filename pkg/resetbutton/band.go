package resetbutton

import "time"

// Band is the press-duration class a completed button press falls into,
// measured key-down to key-up. Callers act on the band rather than on raw
// durations so the timing schema lives in one place.
type Band int

const (
	// BandNone is a press that lands in the debounce window, one of the
	// deliberate dead zones between bands, or past the safety ceiling. It is
	// not an error - it means "do nothing".
	BandNone Band = iota
	BandShortPress
	BandShortHold
	BandLongHold
)

func (b Band) String() string {
	switch b {
	case BandShortPress:
		return "shortpress"
	case BandShortHold:
		return "shorthold"
	case BandLongHold:
		return "longhold"
	default:
		return "none"
	}
}

// Band boundaries. Every interval is half-open, [lo, hi), so each millisecond
// belongs to exactly one band and no release is ambiguous.
const (
	debounceMax   = 100 * time.Millisecond
	shortPressMax = 500 * time.Millisecond
	shortHoldMin  = 1 * time.Second
	shortHoldMax  = 2500 * time.Millisecond
	longHoldMin   = 4 * time.Second
	longHoldMax   = 6 * time.Second
)

// classify maps a completed press duration onto its band.
//
// The gaps between bands (500ms-1s, 2.5s-4s) return BandNone on purpose: they
// make an ambiguous release impossible, so an operator who is unsure which band
// they are in can release in a gap and nothing happens. Closing them to "tidy
// up" the table would remove that escape and silently change what a hesitant
// press does.
//
// Nothing fires at or beyond longHoldMax either. A destructive hardware
// long-hold path is assumed to exist below Linux (see the package comment), and
// the 6-second ceiling is the entire safety margin against it - it is not
// padding to be reclaimed for another band later. Releasing is always the abort;
// "keep holding" must never be the way out of a mistake.
func classify(d time.Duration) Band {
	switch {
	case d < debounceMax:
		return BandNone
	case d < shortPressMax:
		return BandShortPress
	case d < shortHoldMin:
		return BandNone
	case d < shortHoldMax:
		return BandShortHold
	case d < longHoldMin:
		return BandNone
	case d < longHoldMax:
		return BandLongHold
	default:
		return BandNone
	}
}
