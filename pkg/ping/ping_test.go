package ping

import (
	"strings"
	"testing"
	"time"
)

func TestParseRTT(t *testing.T) {
	const iputils = "PING 1.1.1.1 (1.1.1.1) 56(84) bytes of data.\n" +
		"64 bytes from 1.1.1.1: icmp_seq=1 ttl=59 time=12.3 ms\n\n" +
		"--- 1.1.1.1 ping statistics ---\n" +
		"1 packets transmitted, 1 received, 0% packet loss, time 0ms\n"

	const busybox = "PING 10.0.0.1 (10.0.0.1): 56 data bytes\n" +
		"64 bytes from 10.0.0.1: seq=0 ttl=64 time=0.456 ms\n"

	const subMillisecond = "64 bytes from 10.0.0.1: icmp_seq=1 ttl=64 time<1 ms\n"

	const loss = "PING 10.0.0.9 (10.0.0.9) 56(84) bytes of data.\n\n" +
		"--- 10.0.0.9 ping statistics ---\n" +
		"1 packets transmitted, 0 received, 100% packet loss, time 0ms\n"

	cases := []struct {
		name    string
		out     string
		want    time.Duration
		wantErr bool
	}{
		{"iputils reply", iputils, time.Duration(12.3 * float64(time.Millisecond)), false},
		{"busybox reply", busybox, time.Duration(0.456 * float64(time.Millisecond)), false},
		{"sub-millisecond time<", subMillisecond, time.Duration(1 * float64(time.Millisecond)), false},
		{"100 percent loss", loss, 0, true},
		{"empty", "", 0, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parseRTT([]byte(c.out))
			if (err != nil) != c.wantErr {
				t.Fatalf("parseRTT err = %v, wantErr %v", err, c.wantErr)
			}
			if !c.wantErr && got != c.want {
				t.Errorf("parseRTT = %v, want %v", got, c.want)
			}
		})
	}
}

// TestValidHostRejectsPingFlagInjection is the regression guard for
// #PING-INJ-05: ping(8) reads a leading-dash positional argument as a flag
// (e.g. "-f" floods, "-s 65500" oversizes the packet), so any host string
// that could be misread as one or more flags must be rejected before it ever
// reaches exec.Command's argv. This exercises the actual function RTT calls,
// not a copy of its logic.
func TestValidHostRejectsPingFlagInjection(t *testing.T) {
	cases := []struct {
		name string
		host string
		want bool
	}{
		{"flood flag", "-f", false},
		{"size flag with argument", "-s 65500", false},
		{"empty", "", false},
		{"bare dash", "-", false},
		{"label with leading/trailing hyphen segment", "a-.b", false},
		{"300-char name", strings.Repeat("a", 300), false},
		{"ipv4 loopback", "127.0.0.1", true},
		{"ipv6 loopback", "::1", true},
		{"tailscale-style ipv4", "100.64.0.1", true},
		{"dns name", "peer.example.com", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := validHost(c.host); got != c.want {
				t.Errorf("validHost(%q) = %v, want %v", c.host, got, c.want)
			}
		})
	}
}
