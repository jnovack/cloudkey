// Package cpu reports CPU utilization by sampling /proc/stat, so the display
// package can show it without shelling out to a monitoring tool.
package cpu

import (
	"bufio"
	"fmt"
	"os"
	"runtime"
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
	return parseStat(scanner.Text())
}

// parseStat parses one "cpu ..." line from /proc/stat (the aggregate line,
// summed across every core) into idle vs total jiffies. Extracted from
// readSample as a pure function, mirroring parseLoadAvg's split in this same
// file, so the field-parsing logic is testable without a real /proc/stat.
func parseStat(line string) (sample, error) {
	fields := strings.Fields(line)
	if len(fields) < 5 || fields[0] != "cpu" {
		return sample{}, fmt.Errorf("read /proc/stat: unexpected format %q", line)
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

// Cores reports the number of logical CPUs usable by the process. It is a
// cheap runtime lookup (no /proc read), suited to the web dashboard's static
// core count.
func Cores() int {
	return runtime.NumCPU()
}

// LoadAverage reports the 1-minute load average from /proc/loadavg. The file's
// first field is the 1-minute figure; the rest (5/15-minute, running/total
// procs, last PID) are ignored.
func LoadAverage() (float64, error) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, fmt.Errorf("read /proc/loadavg: %w", err)
	}
	return parseLoadAvg(string(data))
}

// parseLoadAvg extracts the 1-minute load average (the first whitespace-
// separated field) from /proc/loadavg content.
func parseLoadAvg(content string) (float64, error) {
	fields := strings.Fields(content)
	if len(fields) == 0 {
		return 0, fmt.Errorf("parse /proc/loadavg: empty")
	}
	v, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, fmt.Errorf("parse /proc/loadavg: %w", err)
	}
	return v, nil
}
