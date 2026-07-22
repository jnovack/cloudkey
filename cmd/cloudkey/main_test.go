package main

import (
	"flag"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jnovack/cloudkey/internal/display"
)

func TestConfigureFlags(t *testing.T) {
	tests := []struct {
		name      string
		delay     string
		wantDelay float64
		wantErr   bool
	}{
		{name: "uses default", wantDelay: 5000},
		{name: "uses valid environment value", delay: "1250", wantDelay: 1250},
		{name: "rejects invalid environment value", delay: "fast", wantDelay: 5000, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("CLOUDKEY_DELAY", tt.delay)

			fs := flag.NewFlagSet("cloudkey", flag.ContinueOnError)
			fs.SetOutput(io.Discard)
			var opts display.CmdLineOpts
			err := configureFlags(fs, &opts)

			if (err != nil) != tt.wantErr {
				t.Fatalf("configureFlags() error = %v, want error %t", err, tt.wantErr)
			}
			if !tt.wantErr && opts.Delay != tt.wantDelay {
				t.Errorf("delay = %v, want %v", opts.Delay, tt.wantDelay)
			}
			if opts.Pidfile != "/var/run/cloudkey.pid" {
				t.Errorf("pidfile = %q, want %q", opts.Pidfile, "/var/run/cloudkey.pid")
			}
			if opts.WireGuardName != "WireGuard" {
				t.Errorf("wireguard name = %q, want %q", opts.WireGuardName, "WireGuard")
			}
			if opts.WireGuardCmd != "wg" {
				t.Errorf("wireguard cmd = %q, want %q", opts.WireGuardCmd, "wg")
			}
			if opts.TailscaleName != "TailScale" {
				t.Errorf("tailscale name = %q, want %q", opts.TailscaleName, "TailScale")
			}
			if opts.TailscaleCmd != "tailscale" {
				t.Errorf("tailscale cmd = %q, want %q", opts.TailscaleCmd, "tailscale")
			}
		})
	}
}

// TestAwaitServerShutdown_WaitsForDoneBeforeGivingUp guards the fix for
// MAIN-LIFE-02: the signal handler must observe api.Serve's graceful drain
// (via the done channel) rather than racing straight to os.Exit. A regression
// that drops the wait — e.g. returning true unconditionally, or reordering
// the select so timeout always wins — would fail one of these two cases.
func TestAwaitServerShutdown_WaitsForDoneBeforeGivingUp(t *testing.T) {
	tests := []struct {
		name    string
		makeCh  func() chan struct{}
		timeout time.Duration
		want    bool
	}{
		{
			name: "done already closed returns true immediately",
			makeCh: func() chan struct{} {
				ch := make(chan struct{})
				close(ch)
				return ch
			},
			timeout: 200 * time.Millisecond,
			want:    true,
		},
		{
			name: "done never closes returns false after timeout",
			makeCh: func() chan struct{} {
				return make(chan struct{})
			},
			timeout: 20 * time.Millisecond,
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := awaitServerShutdown(tt.makeCh(), tt.timeout)
			if got != tt.want {
				t.Errorf("awaitServerShutdown() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestRunResetButtonCmd_DropsOverlappingPress guards runResetButtonCmd's
// documented invariant: a press arriving while a previous command is still
// running is dropped via the CompareAndSwap guard, not queued. Pre-setting
// resetButtonBusy simulates "previous command still in flight" without
// needing a slow real command to race against.
func TestRunResetButtonCmd_DropsOverlappingPress(t *testing.T) {
	if !atomic.CompareAndSwapInt32(&resetButtonBusy, 0, 1) {
		t.Fatal("resetButtonBusy already held at test start; another test left it dirty")
	}
	t.Cleanup(func() { atomic.StoreInt32(&resetButtonBusy, 0) })

	sentinel := filepath.Join(t.TempDir(), "dropped")
	runResetButtonCmd("touch " + sentinel)

	// The guard must drop the command synchronously, before ever forking
	// /bin/sh, so there is no background completion to race against here.
	if _, err := os.Stat(sentinel); err == nil {
		t.Fatal("sentinel file exists; overlapping press was not dropped")
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat sentinel: %v", err)
	}
}

// TestRunResetButtonCmd_RunsWhenIdle is the counterpart to the drop test:
// with resetButtonBusy clear, the command must actually run (in its
// background goroutine) rather than always being dropped.
func TestRunResetButtonCmd_RunsWhenIdle(t *testing.T) {
	atomic.StoreInt32(&resetButtonBusy, 0)
	t.Cleanup(func() { atomic.StoreInt32(&resetButtonBusy, 0) })

	sentinel := filepath.Join(t.TempDir(), "created")
	runResetButtonCmd("touch " + sentinel)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(sentinel); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("sentinel file %q was not created within deadline; command did not run", sentinel)
}

func TestValidateExternalCommands(t *testing.T) {
	dir := t.TempDir()
	realCmd := filepath.Join(dir, "fake-cmd")
	if err := os.WriteFile(realCmd, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("failed to write fake command: %v", err)
	}
	missingCmd := filepath.Join(dir, "does-not-exist")

	tests := []struct {
		name    string
		opts    display.CmdLineOpts
		wantErr bool
	}{
		{"nothing enabled", display.CmdLineOpts{}, false},
		{"wireguard enabled, command found", display.CmdLineOpts{WireGuardIface: "wg0", WireGuardCmd: realCmd}, false},
		{"wireguard enabled, command missing", display.CmdLineOpts{WireGuardIface: "wg0", WireGuardCmd: missingCmd}, true},
		{"tailscale enabled, command found", display.CmdLineOpts{Tailscale: true, TailscaleCmd: realCmd}, false},
		{"tailscale enabled, command missing", display.CmdLineOpts{Tailscale: true, TailscaleCmd: missingCmd}, true},
		{"wireguard command missing but demo mode exempts it", display.CmdLineOpts{Demo: true, WireGuardIface: "wg0", WireGuardCmd: missingCmd}, false},
		{"tailscale command missing but demo mode exempts it", display.CmdLineOpts{Demo: true, Tailscale: true, TailscaleCmd: missingCmd}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateExternalCommands(tt.opts)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateExternalCommands() error = %v, want error %t", err, tt.wantErr)
			}
		})
	}
}
