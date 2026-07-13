package tailscale

import "testing"

func TestParseBackendState(t *testing.T) {
	cases := []struct {
		name string
		out  string
		want string
	}{
		{"running", `{"BackendState":"Running"}`, "Running"},
		{"needs login", `{"BackendState":"NeedsLogin"}`, "NeedsLogin"},
		{"stopped", `{"BackendState":"Stopped"}`, "Stopped"},
		{"missing field", `{}`, ""},
		{"malformed json", `not json`, ""},
		{"empty output", ``, ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := parseBackendState([]byte(c.out)); got != c.want {
				t.Errorf("parseBackendState(%q) = %q, want %q", c.out, got, c.want)
			}
		})
	}
}
