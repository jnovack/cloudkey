package resetbutton

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"
)

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

// TestReadEvent guards against a repeat of the EINVAL bug described in
// eventSize's doc comment: an event decodes correctly only when the reader
// is fed exactly eventSize bytes laid out as GOARCH's struct input_event
// (two timeval fields, then type/code/value in the trailing 8 bytes).
func TestReadEvent(t *testing.T) {
	t.Run("well-formed event decodes correctly", func(t *testing.T) {
		buf := make([]byte, eventSize)
		tail := buf[eventSize-8:]
		binary.LittleEndian.PutUint16(tail[0:2], evKey)
		binary.LittleEndian.PutUint16(tail[2:4], btnCode)
		binary.LittleEndian.PutUint32(tail[4:8], uint32(keyUp))

		got, err := readEvent(bytes.NewReader(buf))
		if err != nil {
			t.Fatalf("readEvent() unexpected error: %v", err)
		}
		want := rawEvent{Type: evKey, Code: btnCode, Value: keyUp}
		if got != want {
			t.Errorf("readEvent() = %+v, want %+v", got, want)
		}
	})

	t.Run("truncated buffer yields an error", func(t *testing.T) {
		buf := make([]byte, eventSize-1)
		_, err := readEvent(bytes.NewReader(buf))
		if err == nil {
			t.Fatal("readEvent() with a short buffer, want error")
		}
		if !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Errorf("readEvent() err = %v, want io.ErrUnexpectedEOF", err)
		}
	})
}
