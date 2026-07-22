package systemd

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestParseIsActive(t *testing.T) {
	cases := []struct {
		name    string
		out     string
		runErr  error
		want    bool
		wantErr bool
	}{
		{"active unit, nil err", "active\n", nil, true, false},
		{"no output, nil err", "", nil, false, true},
		{"inactive unit reported via nonzero exit", "inactive\n", errors.New("exit status 3"), false, false},
		{"failed unit reported via nonzero exit", "failed\n", errors.New("exit status 3"), false, false},
		{"activating unit", "activating\n", errors.New("exit status 3"), false, false},
		{"no output at all is a real error", "", errors.New("exec: \"systemctl\": executable file not found in $PATH"), false, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parseIsActive([]byte(c.out), c.runErr)
			if (err != nil) != c.wantErr {
				t.Fatalf("parseIsActive(%q) error = %v, wantErr %v", c.out, err, c.wantErr)
			}
			if c.wantErr && c.runErr == nil && strings.Contains(err.Error(), "%!w") {
				t.Fatalf("parseIsActive(%q) error = %v, must not contain nil %%w artifact", c.out, err)
			}
			if got != c.want {
				t.Errorf("parseIsActive(%q) = %v, want %v", c.out, got, c.want)
			}
		})
	}
}

func TestParseActiveEnterMonotonic(t *testing.T) {
	cases := []struct {
		name    string
		out     string
		want    time.Duration
		wantErr bool
	}{
		{"typical", "ActiveEnterTimestampMonotonic=123456789\n", 123456789 * time.Microsecond, false},
		{"not active reports zero", "ActiveEnterTimestampMonotonic=0\n", 0, false},
		{"crlf", "ActiveEnterTimestampMonotonic=5000000\r\n", 5 * time.Second, false},
		{"no equals is error", "garbage\n", 0, true},
		{"non-numeric is error", "ActiveEnterTimestampMonotonic=abc\n", 0, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parseActiveEnterMonotonic([]byte(c.out))
			if (err != nil) != c.wantErr {
				t.Fatalf("parseActiveEnterMonotonic(%q) err = %v, wantErr %v", c.out, err, c.wantErr)
			}
			if !c.wantErr && got != c.want {
				t.Errorf("parseActiveEnterMonotonic(%q) = %v, want %v", c.out, got, c.want)
			}
		})
	}
}
