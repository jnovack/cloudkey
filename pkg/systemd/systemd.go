// Package systemd reports whether a systemd unit is active via
// `systemctl is-active`, used as a process-liveness proxy where no more
// precise signal is available.
package systemd

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
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
		// runErr is nil when systemctl exited 0 but printed nothing; wrapping a
		// nil %w operand renders as "%!w(<nil>)", which names no cause at all.
		if runErr == nil {
			return false, fmt.Errorf("systemctl is-active: no output")
		}
		return false, fmt.Errorf("systemctl is-active: %w", runErr)
	}
	return state == "active", nil
}

// ActiveEnterMonotonic reports how long after boot a unit last became active,
// from `systemctl show <unit> --property=ActiveEnterTimestampMonotonic`. The
// value is CLOCK_MONOTONIC microseconds since boot — the same clock as
// /proc/uptime — so a caller subtracts it from current uptime to get how long
// the unit has been active. A unit that is not (and has never been) active
// reports 0, which this returns as a zero Duration.
//
// The monotonic form is used deliberately over the human ActiveEnterTimestamp
// so there is no locale/timezone-dependent date string to parse.
func ActiveEnterMonotonic(unit string) (time.Duration, error) {
	out, err := exec.Command("systemctl", "show", unit, "--property=ActiveEnterTimestampMonotonic").Output()
	if err != nil {
		return 0, fmt.Errorf("systemctl show %s: %w", unit, err)
	}
	return parseActiveEnterMonotonic(out)
}

// parseActiveEnterMonotonic parses the "ActiveEnterTimestampMonotonic=<micros>"
// property line into a Duration since boot.
func parseActiveEnterMonotonic(out []byte) (time.Duration, error) {
	line := strings.TrimSpace(string(out))
	_, val, found := strings.Cut(line, "=")
	if !found {
		return 0, fmt.Errorf("systemctl show: unexpected output %q", line)
	}
	micros, err := strconv.ParseUint(strings.TrimSpace(val), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("systemctl show: parse monotonic %q: %w", val, err)
	}
	return time.Duration(micros) * time.Microsecond, nil
}
