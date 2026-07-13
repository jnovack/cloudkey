package main

import (
	"flag"
	"io"
	"os"
	"path/filepath"
	"testing"

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
