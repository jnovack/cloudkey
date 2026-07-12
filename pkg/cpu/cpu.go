// Package cpu reports CPU utilization by sampling /proc/stat, so the display
// package can show it without shelling out to a monitoring tool.
package cpu

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// sample is one instantaneous read of the aggregate "cpu" line in
// /proc/stat: cumulative jiffies since boot, split into idle and total.
type sample struct {
	idle  uint64
	total uint64
}

// readSample parses /proc/stat's aggregate "cpu" line (summed across every
// core, the first line of the file) into idle vs total jiffies.
func readSample() (sample, error) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return sample{}, fmt.Errorf("read /proc/stat: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		return sample{}, fmt.Errorf("read /proc/stat: empty file")
	}
	fields := strings.Fields(scanner.Text())
	if len(fields) < 5 || fields[0] != "cpu" {
		return sample{}, fmt.Errorf("read /proc/stat: unexpected format %q", scanner.Text())
	}

	var s sample
	for i, field := range fields[1:] {
		v, err := strconv.ParseUint(field, 10, 64)
		if err != nil {
			return sample{}, fmt.Errorf("read /proc/stat: parse field %d: %w", i, err)
		}
		s.total += v
		// Fields are user, nice, system, idle, iowait, ... — idle and iowait
		// (indices 3 and 4 here, since fields[1:] drops the "cpu" label)
		// both count as not-busy.
		if i == 3 || i == 4 {
			s.idle += v
		}
	}
	return s, nil
}

// Percent reports CPU utilization (0-100) averaged over window, by comparing
// two /proc/stat samples window apart. It blocks for window.
func Percent(window time.Duration) (float64, error) {
	before, err := readSample()
	if err != nil {
		return 0, err
	}
	time.Sleep(window)
	after, err := readSample()
	if err != nil {
		return 0, err
	}

	totalDelta := after.total - before.total
	if totalDelta == 0 {
		return 0, nil
	}
	idleDelta := after.idle - before.idle
	return (1 - float64(idleDelta)/float64(totalDelta)) * 100, nil
}
