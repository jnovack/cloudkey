// Package wireguard reports WireGuard tunnel liveness by checking each
// peer's most recent handshake via `wg show`, so the display package can
// show connection status without relying on wg-quick's systemd unit state.
// wg-quick@<iface> is typically Type=oneshot with RemainAfterExit=yes, so
// systemd reports it "active" forever once `wg-quick up` succeeds, even
// after the peer goes unreachable and the tunnel goes stale.
package wireguard

import (
	"bufio"
	"bytes"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// handshakeStaleAfter is how long since a peer's last handshake before the
// tunnel is considered down. WireGuard rekeys at least every 120s while
// traffic is flowing, so a live tunnel always has a handshake newer than
// this.
const handshakeStaleAfter = 180 * time.Second

// Connected reports whether iface has at least one peer with a handshake
// newer than handshakeStaleAfter. cmd is the wg binary to run — a bare name
// resolves via PATH, or the caller may pass an absolute path.
func Connected(cmd, iface string) (bool, error) {
	out, err := exec.Command(cmd, "show", iface, "latest-handshakes").Output()
	if err != nil {
		return false, fmt.Errorf("%s show %s: %w", cmd, iface, err)
	}
	return anyRecentHandshake(out, time.Now()), nil
}

// anyRecentHandshake parses `wg show <iface> latest-handshakes` output —
// one "<pubkey>\t<unix-seconds>" line per peer (0 for a peer that has never
// handshaked) — and reports whether any peer's handshake falls within
// handshakeStaleAfter of now.
func anyRecentHandshake(out []byte, now time.Time) bool {
	cutoff := now.Add(-handshakeStaleAfter).Unix()
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 2 {
			continue
		}
		ts, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			continue
		}
		if ts >= cutoff {
			return true
		}
	}
	return false
}
