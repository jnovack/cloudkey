package memory

import (
	"strings"
	"testing"
)

// meminfo builds a minimal /proc/meminfo body from the given kB values.
func meminfo(totalKB, availKB string, lineEnd string) string {
	var b strings.Builder
	if totalKB != "" {
		b.WriteString("MemTotal:       " + totalKB + " kB" + lineEnd)
	}
	b.WriteString("MemFree:         100000 kB" + lineEnd)
	if availKB != "" {
		b.WriteString("MemAvailable:    " + availKB + " kB" + lineEnd)
	}
	return b.String()
}

func TestParseMemInfo(t *testing.T) {
	cases := []struct {
		name          string
		content       string
		wantTotal     uint64
		wantAvailable uint64
		wantErr       bool
	}{
		{"typical", meminfo("4096000", "1024000", "\n"), 4096000 * 1024, 1024000 * 1024, false},
		{"crlf", meminfo("4096000", "1024000", "\r\n"), 4096000 * 1024, 1024000 * 1024, false},
		{"missing available treated as zero", meminfo("4096000", "", "\n"), 4096000 * 1024, 0, false},
		{"missing total is error", meminfo("", "1024000", "\n"), 0, 0, true},
		{"empty is error", "", 0, 0, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			total, avail, err := parseMemInfo(strings.NewReader(c.content))
			if (err != nil) != c.wantErr {
				t.Fatalf("parseMemInfo err = %v, wantErr %v", err, c.wantErr)
			}
			if c.wantErr {
				return
			}
			if total != c.wantTotal || avail != c.wantAvailable {
				t.Errorf("parseMemInfo = (%d, %d), want (%d, %d)", total, avail, c.wantTotal, c.wantAvailable)
			}
		})
	}
}

func TestComputeUsed(t *testing.T) {
	cases := []struct {
		name      string
		total     uint64
		available uint64
		want      uint64
	}{
		{"normal", 4096000 * 1024, 1024000 * 1024, (4096000 - 1024000) * 1024},
		{"none available", 100, 0, 100},
		{"available exceeds total clamps to zero", 100, 200, 0},
		{"available equals total", 100, 100, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := computeUsed(c.total, c.available); got != c.want {
				t.Errorf("computeUsed(%d, %d) = %d, want %d", c.total, c.available, got, c.want)
			}
		})
	}
}
