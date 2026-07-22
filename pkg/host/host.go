// Package host reports basic identity and uptime for the machine, so the
// display and web layers can show them without shelling out. Reads are cheap
// local lookups (/proc/uptime, /etc/os-release, runtime constants).
package host

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Uptime reports how long the system has been up, from /proc/uptime, whose
// first field is seconds-since-boot as a float.
func Uptime() (time.Duration, error) {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0, fmt.Errorf("read /proc/uptime: %w", err)
	}
	return parseUptime(string(data))
}

// parseUptime extracts the seconds-since-boot (first field) from /proc/uptime
// content and returns it as a Duration.
func parseUptime(content string) (time.Duration, error) {
	fields := strings.Fields(content)
	if len(fields) == 0 {
		return 0, fmt.Errorf("parse /proc/uptime: empty")
	}
	secs, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, fmt.Errorf("parse /proc/uptime: %w", err)
	}
	return time.Duration(secs * float64(time.Second)), nil
}

// Arch reports the CPU architecture this binary was built for (e.g. "arm64",
// "amd64"). It is the build-time GOARCH, which on these single-arch device
// images matches the running hardware.
func Arch() string {
	return runtime.GOARCH
}

// OSName reports a human OS label. It prefers /etc/os-release's PRETTY_NAME
// (e.g. "Debian GNU/Linux 12 (bookworm)") and falls back to the build-time
// GOOS when the file is absent or has no PRETTY_NAME, so it never returns "".
func OSName() string {
	f, err := os.Open("/etc/os-release")
	if err != nil {
		return runtime.GOOS
	}
	defer f.Close()
	if name := parseOSRelease(f); name != "" {
		return name
	}
	return runtime.GOOS
}

// parseOSRelease returns the PRETTY_NAME value from /etc/os-release-format
// input, with surrounding quotes stripped, or "" if not present.
func parseOSRelease(r io.Reader) string {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "PRETTY_NAME=") {
			continue
		}
		val := strings.TrimPrefix(line, "PRETTY_NAME=")
		return strings.Trim(val, `"'`)
	}
	return ""
}
