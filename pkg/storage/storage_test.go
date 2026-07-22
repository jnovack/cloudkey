package storage

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestIsMountedDistinguishesEmptyDirFromMount is the guard for this package's
// reason to exist: statfs on an unmounted directory succeeds and reports the
// PARENT filesystem's space, so isMounted must not trust statfs alone.
func TestIsMountedDistinguishesEmptyDirFromMount(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sdcard")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if mounted, err := isMounted(sub); err != nil || mounted {
		t.Errorf("isMounted(empty subdir) = (%v, %v), want (false, nil)", mounted, err)
	}
	if mounted, err := isMounted("/"); err != nil || !mounted {
		t.Errorf(`isMounted("/") = (%v, %v), want (true, nil)`, mounted, err)
	}
}

func TestStatErrors(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sdcard")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if _, err := Stat(sub); !errors.Is(err, ErrNotMounted) {
		t.Errorf("Stat(unmounted dir) error = %v, want ErrNotMounted", err)
	}
	// A path that does not exist is a different failure from "not mounted":
	// diskFor renders both as not-mounted, but only this one must NOT be
	// ErrNotMounted, so a genuine config typo stays distinguishable in logs.
	missing := filepath.Join(dir, "does-not-exist")
	if _, err := Stat(missing); err == nil || errors.Is(err, ErrNotMounted) {
		t.Errorf("Stat(missing path) error = %v, want a non-ErrNotMounted error", err)
	}
}

// TestStatRootReportsUsage covers the success path against the one filesystem
// every test host is guaranteed to have mounted.
func TestStatRootReportsUsage(t *testing.T) {
	u, err := Stat("/")
	if err != nil {
		t.Fatalf("Stat(/): %v", err)
	}
	if u.TotalBytes == 0 {
		t.Error("TotalBytes = 0, want a real figure for /")
	}
	if u.UsedBytes > u.TotalBytes {
		t.Errorf("UsedBytes %d > TotalBytes %d", u.UsedBytes, u.TotalBytes)
	}
	if u.Percent < 0 || u.Percent > 100 {
		t.Errorf("Percent = %v, want 0-100", u.Percent)
	}
}
