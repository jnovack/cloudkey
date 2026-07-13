package systemd

import (
	"errors"
	"testing"
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
			if got != c.want {
				t.Errorf("parseIsActive(%q) = %v, want %v", c.out, got, c.want)
			}
		})
	}
}
