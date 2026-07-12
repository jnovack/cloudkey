// Package memory reports memory utilization by reading /proc/meminfo, so the
// display package can show it without shelling out to a monitoring tool.
package memory

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Percent reports current memory utilization (0-100) from /proc/meminfo. It
// uses MemAvailable — the kernel's own estimate of memory available to a new
// allocation, accounting for reclaimable caches — rather than MemFree, which
// undercounts memory the kernel would readily hand back.
func Percent() (float64, error) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, fmt.Errorf("read /proc/meminfo: %w", err)
	}
	defer f.Close()

	var total, available uint64
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		v, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		switch strings.TrimSuffix(fields[0], ":") {
		case "MemTotal":
			total = v
		case "MemAvailable":
			available = v
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, fmt.Errorf("read /proc/meminfo: %w", err)
	}
	if total == 0 {
		return 0, fmt.Errorf("read /proc/meminfo: MemTotal not found")
	}

	return (1 - float64(available)/float64(total)) * 100, nil
}
