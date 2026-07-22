package cpu

import "testing"

func TestParseLoadAvg(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    float64
		wantErr bool
	}{
		{"typical", "0.62 0.55 0.49 1/234 5678\n", 0.62, false},
		{"zero", "0.00 0.01 0.05 1/100 200\n", 0.00, false},
		{"integer field", "2 1 1 3/400 900\n", 2, false},
		{"crlf line ending", "0.62 0.55 0.49 1/234 5678\r\n", 0.62, false},
		{"empty", "", 0, true},
		{"non-numeric first field", "x 0.55 0.49 1/234 5678\n", 0, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parseLoadAvg(c.content)
			if (err != nil) != c.wantErr {
				t.Fatalf("parseLoadAvg(%q) err = %v, wantErr %v", c.content, err, c.wantErr)
			}
			if !c.wantErr && got != c.want {
				t.Errorf("parseLoadAvg(%q) = %v, want %v", c.content, got, c.want)
			}
		})
	}
}

func TestCores(t *testing.T) {
	if got := Cores(); got < 1 {
		t.Errorf("Cores() = %d, want >= 1", got)
	}
}

func TestParseStat(t *testing.T) {
	cases := []struct {
		name       string
		line       string
		want       sample
		wantErr    bool
		wantIdleOf bool // if true, assert want.idle == fields[3]+fields[4]
	}{
		{
			name:       "valid line",
			line:       "cpu 100 20 30 40 50",
			want:       sample{idle: 40 + 50, total: 100 + 20 + 30 + 40 + 50},
			wantIdleOf: true,
		},
		{
			name:    "wrong prefix",
			line:    "intr 1 2 3 4 5",
			wantErr: true,
		},
		{
			name:    "too short",
			line:    "cpu 1 2",
			wantErr: true,
		},
		{
			name:    "non-numeric field",
			line:    "cpu 1 2 3 x 5",
			wantErr: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parseStat(c.line)
			if (err != nil) != c.wantErr {
				t.Fatalf("parseStat(%q) err = %v, wantErr %v", c.line, err, c.wantErr)
			}
			if c.wantErr {
				return
			}
			if got != c.want {
				t.Errorf("parseStat(%q) = %+v, want %+v", c.line, got, c.want)
			}
			if c.wantIdleOf && got.idle != 40+50 {
				t.Errorf("parseStat(%q) idle = %d, want fields[3]+fields[4] = %d", c.line, got.idle, 40+50)
			}
		})
	}
}
