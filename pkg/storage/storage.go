// Package storage reports used space for a mounted filesystem via statfs, so
// the display package can show it without shelling out to df.
package storage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// ErrNotMounted is returned by Stat when path exists but nothing is mounted
// there — e.g. an empty /sdcard directory with no card inserted. Unlike a
// missing path (which syscall.Statfs itself rejects with ENOENT), an empty
// mount-point directory statfs's successfully and silently reports its
// parent filesystem's space instead, so Stat checks for a real mount
// explicitly rather than trusting statfs alone.
var ErrNotMounted = errors.New("not mounted")

// Usage is a mounted filesystem's space, in bytes, plus the derived percent
// used.
type Usage struct {
	TotalBytes uint64
	UsedBytes  uint64
	Percent    float64
}

// isMounted reports whether path is the root of a distinct mounted
// filesystem, rather than merely a directory that happens to exist on its
// parent's filesystem. It compares path's device ID against its parent
// directory's, the same check "mountpoint(1)" makes. The filesystem root
// ("/") has no parent to compare against and is always considered mounted.
func isMounted(path string) (bool, error) {
	clean := filepath.Clean(path)
	if clean == "/" {
		return true, nil
	}

	info, err := os.Stat(clean)
	if err != nil {
		return false, err
	}
	parentInfo, err := os.Stat(filepath.Dir(clean))
	if err != nil {
		return false, err
	}

	dev, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return false, fmt.Errorf("stat %s: unsupported platform", clean)
	}
	parentDev, ok := parentInfo.Sys().(*syscall.Stat_t)
	if !ok {
		return false, fmt.Errorf("stat %s: unsupported platform", filepath.Dir(clean))
	}

	return dev.Dev != parentDev.Dev, nil
}

// Stat reports the used space and percent-used for the filesystem mounted at
// path. It returns ErrNotMounted if path exists but nothing is mounted
// there, or the underlying stat error if path doesn't exist at all.
func Stat(path string) (Usage, error) {
	mounted, err := isMounted(path)
	if err != nil {
		return Usage{}, fmt.Errorf("stat %s: %w", path, err)
	}
	if !mounted {
		return Usage{}, ErrNotMounted
	}

	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return Usage{}, fmt.Errorf("statfs %s: %w", path, err)
	}

	total := uint64(st.Blocks) * uint64(st.Bsize)
	free := uint64(st.Bfree) * uint64(st.Bsize)
	used := total - free

	var percent float64
	if total > 0 {
		percent = float64(used) / float64(total) * 100
	}

	return Usage{TotalBytes: total, UsedBytes: used, Percent: percent}, nil
}
