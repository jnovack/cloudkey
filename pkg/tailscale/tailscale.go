// Package tailscale reports tailnet connection status and transfer counters by
// parsing `tailscale status --json`, so the display and web layers can show
// status without relying on tailscaled's systemd unit state, which reports
// "active" as long as the daemon process is running regardless of whether it
// has ever logged in or reached the coordination server.
package tailscale

import (
	"encoding/json"
	"fmt"
	"net"
	"os/exec"
)

// runningState is the BackendState value tailscaled reports once it is
// authenticated and connected to the tailnet. Other values (e.g.
// "NeedsLogin", "Stopped", "Starting") all mean not connected.
const runningState = "Running"

// Status is a parsed `tailscale status --json` result. Rx/Tx are cumulative
// bytes summed across all peers; Up is true when the backend is Running;
// PingAddr is a tailnet IPv4 of an online peer to use as a latency-probe
// target, or "" when no online peer is known.
type Status struct {
	Up       bool
	Rx       uint64
	Tx       uint64
	PingAddr string
}

// peer is the subset of a `tailscale status --json` Peer entry this package
// reads.
type peer struct {
	TailscaleIPs []string `json:"TailscaleIPs"`
	RxBytes      uint64   `json:"RxBytes"`
	TxBytes      uint64   `json:"TxBytes"`
	Online       bool     `json:"Online"`
}

// statusJSON is the subset of `tailscale status --json`'s output this package
// reads.
type statusJSON struct {
	BackendState string
	Peer         map[string]peer
}

// Stat runs `tailscale status --json` and parses it. cmd is the tailscale
// binary to run — a bare name resolves via PATH, or the caller may pass an
// absolute path.
func Stat(cmd string) (Status, error) {
	out, err := exec.Command(cmd, "status", "--json").Output()
	if err != nil {
		return Status{}, fmt.Errorf("%s status: %w", cmd, err)
	}
	return parseStatus(out), nil
}

// parseStatus extracts backend state and per-peer transfer/reachability from
// `tailscale status --json` output. It returns a zero Status (down, no bytes)
// on malformed JSON rather than erroring, since a redraw loop has nowhere to
// surface a parse error and "down" is the safe display.
func parseStatus(out []byte) Status {
	var s statusJSON
	if err := json.Unmarshal(out, &s); err != nil {
		return Status{}
	}
	st := Status{Up: s.BackendState == runningState}
	for _, p := range s.Peer {
		st.Rx += p.RxBytes
		st.Tx += p.TxBytes
		if st.PingAddr == "" && p.Online {
			st.PingAddr = firstIPv4(p.TailscaleIPs)
		}
	}
	return st
}

// firstIPv4 returns the first IPv4 address among ips, or "" if none is IPv4.
// Tailnet peers carry both a 100.x IPv4 and an fd7a:: IPv6; ICMP probing is
// simplest against the IPv4.
func firstIPv4(ips []string) string {
	for _, s := range ips {
		if ip := net.ParseIP(s); ip != nil && ip.To4() != nil {
			return s
		}
	}
	return ""
}
