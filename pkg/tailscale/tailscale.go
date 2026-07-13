// Package tailscale reports whether this node is authenticated and
// connected to its tailnet by checking tailscaled's backend state via
// `tailscale status --json`, so the display package can show connection
// status without relying on tailscaled's systemd unit state, which reports
// "active" as long as the daemon process is running regardless of whether
// it has ever logged in or reached the coordination server.
package tailscale

import (
	"encoding/json"
	"fmt"
	"os/exec"
)

// runningState is the BackendState value tailscaled reports once it is
// authenticated and connected to the tailnet. Other values (e.g.
// "NeedsLogin", "Stopped", "Starting") all mean not connected.
const runningState = "Running"

// status is the subset of `tailscale status --json`'s output this package
// reads.
type status struct {
	BackendState string
}

// Connected reports whether tailscaled is authenticated and connected to
// the tailnet. cmd is the tailscale binary to run — a bare name resolves
// via PATH, or the caller may pass an absolute path.
func Connected(cmd string) (bool, error) {
	out, err := exec.Command(cmd, "status", "--json").Output()
	if err != nil {
		return false, fmt.Errorf("%s status: %w", cmd, err)
	}
	return parseBackendState(out) == runningState, nil
}

// parseBackendState extracts BackendState from `tailscale status --json`
// output. It returns "" (never equal to runningState) on malformed JSON
// rather than erroring, since Connected only cares about a yes/no match.
func parseBackendState(out []byte) string {
	var s status
	if err := json.Unmarshal(out, &s); err != nil {
		return ""
	}
	return s.BackendState
}
