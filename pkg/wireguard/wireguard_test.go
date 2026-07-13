package wireguard

import (
	"strconv"
	"testing"
	"time"
)

func TestAnyRecentHandshake(t *testing.T) {
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name string
		out  string
		want bool
	}{
		{"no peers", "", false},
		{"single peer never handshaked", "PUBKEY1\t0\n", false},
		{
			"single peer recent handshake",
			"PUBKEY1\t" + fmtUnix(now.Add(-30*time.Second)) + "\n",
			true,
		},
		{
			"single peer stale handshake",
			"PUBKEY1\t" + fmtUnix(now.Add(-10*time.Minute)) + "\n",
			false,
		},
		{
			"one stale one recent peer",
			"PUBKEY1\t" + fmtUnix(now.Add(-10*time.Minute)) + "\n" +
				"PUBKEY2\t" + fmtUnix(now.Add(-5*time.Second)) + "\n",
			true,
		},
		{"malformed line ignored", "not-tab-separated\n", false},
		{
			"handshake exactly at cutoff counts as recent",
			"PUBKEY1\t" + fmtUnix(now.Add(-handshakeStaleAfter)) + "\n",
			true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := anyRecentHandshake([]byte(c.out), now); got != c.want {
				t.Errorf("anyRecentHandshake(%q) = %v, want %v", c.out, got, c.want)
			}
		})
	}
}

func fmtUnix(t time.Time) string {
	return strconv.FormatInt(t.Unix(), 10)
}
