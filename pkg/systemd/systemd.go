// Package systemd reports whether a systemd unit is active via
// `systemctl is-active`, used as a process-liveness proxy where no more
// precise signal is available.
package systemd

import (
	"fmt"
	"os/exec"
	"strings"
)

// IsActive reports whether `systemctl is-active <unit>` reports "active".
// systemctl exits non-zero for every non-active state (inactive, failed,
// activating, ...), so a run error alone doesn't mean failure — only a run
// that produced no output at all (unit doesn't exist, systemctl itself
// missing) is treated as an error.
func IsActive(unit string) (bool, error) {
	out, err := exec.Command("systemctl", "is-active", unit).Output()
	return parseIsActive(out, err)
}

func parseIsActive(out []byte, runErr error) (bool, error) {
	state := strings.TrimSpace(string(out))
	if state == "" {
		return false, fmt.Errorf("systemctl is-active: %w", runErr)
	}
	return state == "active", nil
}
