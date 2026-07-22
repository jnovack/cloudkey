package host

import (
	"strings"
	"testing"
	"time"
)

func TestParseUptime(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    time.Duration
		wantErr bool
	}{
		{"typical", "12345.67 98765.43\n", time.Duration(12345.67 * float64(time.Second)), false},
		{"zero", "0.00 0.00\n", 0, false},
		{"crlf", "3600.00 100.0\r\n", time.Hour, false},
		{"empty", "", 0, true},
		{"non-numeric", "up 100\n", 0, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parseUptime(c.content)
			if (err != nil) != c.wantErr {
				t.Fatalf("parseUptime(%q) err = %v, wantErr %v", c.content, err, c.wantErr)
			}
			if !c.wantErr && got != c.want {
				t.Errorf("parseUptime(%q) = %v, want %v", c.content, got, c.want)
			}
		})
	}
}

func TestParseOSRelease(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    string
	}{
		{
			"pretty name quoted",
			"NAME=\"Debian GNU/Linux\"\nPRETTY_NAME=\"Debian GNU/Linux 12 (bookworm)\"\nID=debian\n",
			"Debian GNU/Linux 12 (bookworm)",
		},
		{"pretty name unquoted", "PRETTY_NAME=Alpine Linux\n", "Alpine Linux"},
		{"crlf", "PRETTY_NAME=\"Ubuntu 22.04\"\r\n", "Ubuntu 22.04"},
		{"absent", "NAME=\"Foo\"\nID=foo\n", ""},
		{"empty", "", ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := parseOSRelease(strings.NewReader(c.content)); got != c.want {
				t.Errorf("parseOSRelease(%q) = %q, want %q", c.content, got, c.want)
			}
		})
	}
}

func TestArchNonEmpty(t *testing.T) {
	if Arch() == "" {
		t.Error("Arch() = empty, want a GOARCH value")
	}
}
