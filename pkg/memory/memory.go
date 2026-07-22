// Package memory reports memory utilization by reading /proc/meminfo, so the
// display package can show it without shelling out to a monitoring tool.
package memory

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// parseMemInfo reads MemTotal and MemAvailable from /proc/meminfo-format input
// and returns them in bytes. /proc/meminfo reports these in kibibytes (the
// trailing "kB" unit), so each value is scaled by 1024. MemAvailable is the
// kernel's own estimate of memory available to a new allocation (accounting for
// reclaimable caches), which is a truer "free" than MemFree. A missing MemTotal
// is an error; a missing MemAvailable leaves available zero (100% used).
func parseMemInfo(r io.Reader) (total, available uint64, err error) {
	scanner := bufio.NewScanner(r)
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
			total = v * 1024
		case "MemAvailable":
			available = v * 1024
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, 0, fmt.Errorf("read /proc/meminfo: %w", err)
	}
	if total == 0 {
		return 0, 0, fmt.Errorf("read /proc/meminfo: MemTotal not found")
	}
	return total, available, nil
}

// readMemInfo opens /proc/meminfo and parses it into total and available bytes.
func readMemInfo() (total, available uint64, err error) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0, fmt.Errorf("read /proc/meminfo: %w", err)
	}
	defer f.Close()
	return parseMemInfo(f)
}

// Stats reports used and total memory in bytes, where used is total minus
// MemAvailable — the kernel's own estimate of memory available to a new
// allocation, accounting for reclaimable caches, which is a truer "free"
// than MemFree. The web dashboard needs the raw byte figures (it formats and
// derives its own percentage), which a bare percentage alone can't provide.
func Stats() (used, total uint64, err error) {
	total, available, err := readMemInfo()
	if err != nil {
		return 0, 0, err
	}
	return computeUsed(total, available), total, nil
}

// computeUsed returns total minus available, clamping available to total so a
// transient reading where MemAvailable momentarily exceeds MemTotal can't
// underflow the unsigned subtraction into a huge bogus "used" value.
func computeUsed(total, available uint64) uint64 {
	if available > total {
		return 0
	}
	return total - available
}
