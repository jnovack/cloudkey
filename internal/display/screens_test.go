package display

import (
	"errors"
	"image"
	"testing"
	"time"
)

func TestFormatGB(t *testing.T) {
	gb := float64(1 << 30)
	cases := []struct {
		name  string
		bytes uint64
		want  string
	}{
		{"zero", 0, "0.00GB"},
		{"under 10GB keeps two decimals", uint64(9.87 * gb), "9.87GB"},
		{"exactly 10GB drops to one decimal", uint64(10 * gb), "10.0GB"},
		{"tens keep one decimal", uint64(23.4 * gb), "23.4GB"},
		{"just under 100GB keeps one decimal", uint64(99.9 * gb), "99.9GB"},
		{"exactly 100GB drops decimals", uint64(100 * gb), "100GB"},
		{"hundreds have no decimals", uint64(897 * gb), "897GB"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := formatGB(c.bytes); got != c.want {
				t.Errorf("formatGB(%d) = %q, want %q", c.bytes, got, c.want)
			}
		})
	}
}

func TestFormatPercent(t *testing.T) {
	cases := []struct {
		name    string
		percent float64
		want    string
	}{
		{"zero pads to two digits", 0, "00%"},
		{"single digit zero-padded", 5, "05%"},
		{"two digits unchanged", 45, "45%"},
		{"hundred is three digits", 100, "100%"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := formatPercent(c.percent); got != c.want {
				t.Errorf("formatPercent(%v) = %q, want %q", c.percent, got, c.want)
			}
		})
	}
}

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

// TestResolveHostnameKeepsPreviousValueOnFailure is the regression guard for
// #HOST-NULL-02: os.Hostname's error path must never overwrite the previous
// hostname with the empty-string zero value it returns on failure. It drives
// the actual production helper buildHost's goroutine calls, not a
// reimplementation of its logic.
func TestResolveHostnameKeepsPreviousValueOnFailure(t *testing.T) {
	cases := []struct {
		name   string
		prev   string
		lookup func() (string, error)
		want   string
	}{
		{
			name:   "success returns new value",
			prev:   "cloudkey-gen2.local",
			lookup: func() (string, error) { return "real-host", nil },
			want:   "real-host",
		},
		{
			name:   "failure keeps previous value",
			prev:   "real-host",
			lookup: func() (string, error) { return "", errors.New("lookup failed") },
			want:   "real-host",
		},
		{
			name:   "failure on the first tick keeps the seed value",
			prev:   "cloudkey-gen2.local",
			lookup: func() (string, error) { return "", errors.New("lookup failed") },
			want:   "cloudkey-gen2.local",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := resolveHostname(c.prev, c.lookup); got != c.want {
				t.Errorf("resolveHostname(%q, ...) = %q, want %q", c.prev, got, c.want)
			}
		})
	}
}

// TestSystemLoopHonorsMinimumPeriodWhenCPUReadFails is the regression guard
// for #SYS-SPIN-01: statSystem returning before cpuSampleWindow elapses (e.g.
// cpu.Percent bailing out on a /proc/stat read failure before it ever reaches
// its own sleep) must not let buildSystem's redraw loop spin unthrottled. A
// fast fake stat is fed to systemLoopTick and the injected sleep is checked
// for a positive, window-bounded floor.
func TestSystemLoopHonorsMinimumPeriodWhenCPUReadFails(t *testing.T) {
	fastStat := func() systemStat { return systemStat{cpuText: "--%", memText: "--%"} }

	var slept time.Duration
	fakeSleep := func(d time.Duration) { slept = d }

	systemLoopTick(fastStat, func(systemStat) {}, fakeSleep)

	if slept <= 0 {
		t.Fatalf("systemLoopTick slept %v after an instantaneous stat call, want a positive floor sleep close to cpuSampleWindow (%v)", slept, cpuSampleWindow)
	}
	if slept > cpuSampleWindow {
		t.Errorf("systemLoopTick slept %v, want <= cpuSampleWindow (%v)", slept, cpuSampleWindow)
	}
}
