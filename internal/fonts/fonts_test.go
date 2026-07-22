package fonts

import "testing"

func TestLoad(t *testing.T) {
	tests := []struct {
		name     string
		fontname string
		wantNil  bool
	}{
		{"registered font", "lato-regular", false},
		{"unregistered font", "no-such-font", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Load(tt.fontname)
			if (got == nil) != tt.wantNil {
				t.Errorf("Load(%q) = %v, wantNil %v", tt.fontname, got, tt.wantNil)
			}
		})
	}
}

// TestLoadReturnsCachedFont guards the performance half of this fix: fonts
// are parsed once at init into a package-level map, not re-decoded and
// re-parsed (~105us of base64+TrueType work) on every call. If Load reverts
// to parsing per call, this test fails because each call would return a
// distinct *truetype.Font value.
func TestLoadReturnsCachedFont(t *testing.T) {
	first := Load("lato-regular")
	second := Load("lato-regular")
	if first != second {
		t.Errorf("Load(\"lato-regular\") returned different pointers across calls: %p != %p; font should be parsed once and cached", first, second)
	}
}
