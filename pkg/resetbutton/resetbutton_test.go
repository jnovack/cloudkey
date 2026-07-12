package resetbutton

import "testing"

func TestIsRelease(t *testing.T) {
	cases := []struct {
		name string
		e    rawEvent
		want bool
	}{
		{"reset button release", rawEvent{Type: evKey, Code: btnCode, Value: keyUp}, true},
		{"reset button press (key-down) ignored", rawEvent{Type: evKey, Code: btnCode, Value: 1}, false},
		{"reset button autorepeat ignored", rawEvent{Type: evKey, Code: btnCode, Value: 2}, false},
		{"different key code released ignored", rawEvent{Type: evKey, Code: 0x101, Value: keyUp}, false},
		{"non-key event type ignored", rawEvent{Type: 0, Code: btnCode, Value: keyUp}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isRelease(c.e); got != c.want {
				t.Errorf("isRelease(%+v) = %v, want %v", c.e, got, c.want)
			}
		})
	}
}
