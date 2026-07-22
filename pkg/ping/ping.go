// Package ping reports round-trip latency to a host by shelling out to the
// system ping(8), matching the rest of this codebase's "run the standard CLI
// and parse it" approach (see the wireguard/tailscale/systemd packages) rather
// than pulling in a raw-socket ICMP dependency that would need extra
// privileges. A single bounded probe keeps it cheap on the low-power device.
package ping

import (
	"fmt"
	"net"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// probeTimeoutSecs bounds a single ping so a dead host costs at most ~1s. It is
// passed as the -W (per-reply wait) argument.
const probeTimeoutSecs = 1

// rttPattern matches the "time=12.3 ms" field of a ping reply line. iputils and
// BusyBox ping both print this form; the unit ("ms") is fixed for a single-hop
// reply, so only the numeric value is captured.
var rttPattern = regexp.MustCompile(`time[=<]([0-9]+(?:\.[0-9]+)?)\s*ms`)

// RTT sends one ICMP echo to host and returns the round-trip time. It runs
// `ping -c 1 -W 1 <host>`, bounding the wait so an unreachable host returns an
// error within ~1s rather than hanging. host may be an IP or a name, and is
// validated before use: ping(8) reads a leading-dash positional as a flag, so
// an unexpected value from a caller's parser (see wireguard.endpointHost,
// which returns its input verbatim when it can't split it) must not reach
// argv. A "--" separator is not used instead because BusyBox ping — likely on
// this appliance — does not accept one.
func RTT(host string) (time.Duration, error) {
	if !validHost(host) {
		return 0, fmt.Errorf("ping: invalid host %q", host)
	}
	out, err := exec.Command("ping", "-c", "1", "-W", strconv.Itoa(probeTimeoutSecs), host).Output()
	if err != nil {
		return 0, fmt.Errorf("ping %s: %w", host, err)
	}
	return parseRTT(out)
}

// validHost reports whether host is a literal IP or a plausible DNS name:
// dot-separated labels of alphanumerics and interior hyphens. Anything else —
// most importantly anything starting with '-' — is rejected so it can't be
// read as a ping(8) flag.
func validHost(host string) bool {
	if host == "" || len(host) > 253 {
		return false
	}
	if net.ParseIP(host) != nil {
		return true
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			switch {
			case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-':
			default:
				return false
			}
		}
	}
	return true
}

// parseRTT extracts the round-trip time from ping(8) output. It returns an
// error when no reply line with a time field is present (e.g. 100% packet
// loss), which the caller treats as "no latency reading" rather than zero.
func parseRTT(out []byte) (time.Duration, error) {
	m := rttPattern.FindSubmatch(out)
	if m == nil {
		return 0, fmt.Errorf("ping: no rtt in output")
	}
	ms, err := strconv.ParseFloat(string(m[1]), 64)
	if err != nil {
		return 0, fmt.Errorf("ping: parse rtt %q: %w", m[1], err)
	}
	return time.Duration(ms * float64(time.Millisecond)), nil
}
