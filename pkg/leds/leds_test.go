package leds

import "testing"

func TestParseBrightness(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		want    int
		wantErr bool
	}{
		{"trailing newline", "128\n", 128, false},
		{"surrounding whitespace", " 0 ", 0, false},
		{"empty", "", 0, true},
		{"non-numeric", "x", 0, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parseBrightness([]byte(c.input))
			if (err != nil) != c.wantErr {
				t.Fatalf("parseBrightness(%q) err = %v, wantErr %v", c.input, err, c.wantErr)
			}
			if !c.wantErr && got != c.want {
				t.Errorf("parseBrightness(%q) = %v, want %v", c.input, got, c.want)
			}
		})
	}
}
