package main

import (
	"flag"
	"io"
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
		})
	}
}
