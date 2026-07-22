// Package wireguard reports WireGuard tunnel liveness and transfer counters by
// parsing plain `wg show <iface>` output, so the display and web layers can show
// status without relying on wg-quick's systemd unit state. wg-quick@<iface> is
// typically Type=oneshot with RemainAfterExit=yes, so systemd reports it
// "active" forever once `wg-quick up` succeeds, even after the peer goes
// unreachable and the tunnel goes stale.
//
// It deliberately parses the human-readable `wg show <iface>` rather than
// `wg show <iface> dump`: on the target hardware (userspace/Mullvad WireGuard)
// the dump/latest-handshakes machine forms fail with "Unable to access
// interface: Protocol not supported", while the plain form works and carries
// everything needed — per-peer `latest handshake:` age and `transfer:` counters.
package wireguard

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	humanize "github.com/dustin/go-humanize"
)

// handshakeStaleAfter is how long since a peer's last handshake before the
// tunnel is considered down. WireGuard rekeys at least every 120s while
// traffic is flowing, so a live tunnel always has a handshake newer than
// this.
const handshakeStaleAfter = 180 * time.Second

// Status is a parsed `wg show <iface>` result. Rx/Tx are cumulative bytes
// summed across all peers; Up is true when any peer handshaked within
// handshakeStaleAfter; Endpoint is the first peer's endpoint host (no port),
// used as a latency-probe target. Fields with no data are left zero/empty.
type Status struct {
	Up       bool
	Rx       uint64
	Tx       uint64
	Endpoint string
}

// Stat runs `wg show <iface>` and parses it. cmd is the wg binary to run — a
// bare name resolves via PATH, or the caller may pass an absolute path.
func Stat(cmd, iface string) (Status, error) {
	out, err := exec.Command(cmd, "show", iface).Output()
	if err != nil {
		return Status{}, fmt.Errorf("%s show %s: %w", cmd, iface, err)
	}
	return parseShow(out), nil
}

// parseShow parses `wg show <iface>` output. The format is an "interface:"
// block followed by one "peer:" block per peer, each with indented
// "  key: value" lines; a peer that has never handshaked simply omits its
// "latest handshake:" and "transfer:" lines. Transfer counters are summed
// across peers, and the first endpoint seen becomes the ping target.
func parseShow(out []byte) Status {
	var st Status
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case strings.HasPrefix(line, "endpoint:"):
			if st.Endpoint == "" {
				st.Endpoint = endpointHost(strings.TrimSpace(strings.TrimPrefix(line, "endpoint:")))
			}
		case strings.HasPrefix(line, "latest handshake:"):
			v := strings.TrimSpace(strings.TrimPrefix(line, "latest handshake:"))
			if age, ok := parseHandshakeAge(v); ok && age < handshakeStaleAfter {
				st.Up = true
			}
		case strings.HasPrefix(line, "transfer:"):
			rx, tx := parseTransfer(strings.TrimPrefix(line, "transfer:"))
			st.Rx += rx
			st.Tx += tx
		}
	}
	return st
}

// endpointHost strips the port from a "host:port" endpoint, handling IPv6
// "[::1]:51820" as well as IPv4. If it can't split, the raw value is returned
// so a malformed endpoint still yields a best-effort probe target.
func endpointHost(ep string) string {
	if host, _, err := net.SplitHostPort(ep); err == nil {
		return host
	}
	return ep
}

// parseTransfer parses a `transfer:` value like "15.88 MiB received, 3.00 MiB
// sent" into received/sent bytes. Byte figures use IEC units (MiB/GiB), which
// humanize.ParseBytes handles.
func parseTransfer(s string) (rx, tx uint64) {
	for part := range strings.SplitSeq(s, ",") {
		part = strings.TrimSpace(part)
		switch {
		case strings.HasSuffix(part, "received"):
			if n, err := humanize.ParseBytes(strings.TrimSpace(strings.TrimSuffix(part, "received"))); err == nil {
				rx = n
			}
		case strings.HasSuffix(part, "sent"):
			if n, err := humanize.ParseBytes(strings.TrimSpace(strings.TrimSuffix(part, "sent"))); err == nil {
				tx = n
			}
		}
	}
	return rx, tx
}

// handshakeUnit maps wg's handshake-age unit words to their durations.
var handshakeUnit = map[string]time.Duration{
	"day":    24 * time.Hour,
	"hour":   time.Hour,
	"minute": time.Minute,
	"second": time.Second,
}

// handshakeToken matches one "<n> <unit>[s]" pair of a handshake age.
var handshakeToken = regexp.MustCompile(`(\d+)\s+(day|hour|minute|second)s?`)

// parseHandshakeAge turns wg's human handshake age into a Duration. wg prints
// the age as comma-separated unit parts, e.g. "31 seconds ago",
// "1 minute, 5 seconds ago", or "2 days, 3 hours, 4 minutes, 12 seconds ago";
// a trailing "ago" (present on some builds) is ignored since only the numeric
// parts are matched. ok is false when the value contains no recognizable parts,
// so a peer with an unparseable/absent age is not counted as recently seen.
func parseHandshakeAge(s string) (age time.Duration, ok bool) {
	matches := handshakeToken.FindAllStringSubmatch(s, -1)
	if len(matches) == 0 {
		return 0, false
	}
	for _, m := range matches {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		age += time.Duration(n) * handshakeUnit[m[2]]
	}
	return age, true
}
