package resetbutton

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"
	"time"
)

func TestIsPress(t *testing.T) {
	cases := []struct {
		name string
		e    rawEvent
		want bool
	}{
		{"reset button press", rawEvent{Type: evKey, Code: btnCode, Value: keyDown}, true},
		{"reset button release ignored", rawEvent{Type: evKey, Code: btnCode, Value: keyUp}, false},
		{"reset button autorepeat ignored", rawEvent{Type: evKey, Code: btnCode, Value: keyRepeat}, false},
		{"different key code pressed ignored", rawEvent{Type: evKey, Code: 0x101, Value: keyDown}, false},
		{"non-key event type ignored", rawEvent{Type: 0, Code: btnCode, Value: keyDown}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isPress(c.e); got != c.want {
				t.Errorf("isPress(%+v) = %v, want %v", c.e, got, c.want)
			}
		})
	}
}

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

// encodeEvent lays out e the way the kernel writes struct input_event for the
// current GOARCH, so a synthetic stream feeds readEvent exactly as the device
// would. It mirrors readEvent's own tail arithmetic rather than hardcoding an
// offset, which would silently break on a 64-bit test host.
func encodeEvent(e rawEvent) []byte {
	buf := make([]byte, eventSize)
	tail := buf[eventSize-8:]
	binary.LittleEndian.PutUint16(tail[0:2], e.Type)
	binary.LittleEndian.PutUint16(tail[2:4], e.Code)
	binary.LittleEndian.PutUint32(tail[4:8], uint32(e.Value))
	return buf
}

func pressEvent() []byte   { return encodeEvent(rawEvent{Type: evKey, Code: btnCode, Value: keyDown}) }
func releaseEvent() []byte { return encodeEvent(rawEvent{Type: evKey, Code: btnCode, Value: keyUp}) }
func repeatEvent() []byte {
	return encodeEvent(rawEvent{Type: evKey, Code: btnCode, Value: keyRepeat})
}

// useFakeClock swaps the package clock for one that yields offsets from a fixed
// instant, in order, so a press duration is exact and no test ever sleeps for a
// real band length. Running out of scripted instants fails the test loudly,
// which is itself an assertion: it means the loop consulted the clock more
// often than the press/release pair should require.
func useFakeClock(t *testing.T, offsets ...time.Duration) {
	t.Helper()
	base := time.Unix(1000, 0)
	i := 0
	prev := now
	now = func() time.Time {
		if i >= len(offsets) {
			t.Fatalf("clock consulted %d times, only %d instants scripted", i+1, len(offsets))
		}
		v := base.Add(offsets[i])
		i++
		return v
	}
	t.Cleanup(func() { now = prev })
}

// collectBands runs watchReader over a synthetic stream and returns the bands
// it dispatched. The loop exits when the reader is exhausted, which surfaces as
// io.EOF — the test's stand-in for the device node disappearing.
func collectBands(t *testing.T, stream []byte) []Band {
	t.Helper()
	var got []Band
	err := watchReader(bytes.NewReader(stream), func(b Band) { got = append(got, b) })
	if !errors.Is(err, io.EOF) {
		t.Fatalf("watchReader() err = %v, want io.EOF at end of stream", err)
	}
	return got
}

// TestWatchReaderDispatchesOnlyClassifiedBands guards the whole point of the
// band schema: a press is timed key-down to key-up and only fires when it lands
// in a real band. Before this, Watch fired on every release regardless of
// duration, so a multi-second hold and a quick tap did the same thing.
func TestWatchReaderDispatchesOnlyClassifiedBands(t *testing.T) {
	cases := []struct {
		name string
		hold time.Duration
		want []Band
	}{
		{"a tap fires shortpress", 200 * time.Millisecond, []Band{BandShortPress}},
		{"a bounce fires nothing", 50 * time.Millisecond, nil},
		{"the gap above shortpress fires nothing", 750 * time.Millisecond, nil},
		{"a short hold fires shorthold", 1500 * time.Millisecond, []Band{BandShortHold}},
		{"the gap below longhold fires nothing", 3 * time.Second, nil},
		{"a long hold fires longhold", 5 * time.Second, []Band{BandLongHold}},
		{"holding past the safety ceiling fires nothing", 8 * time.Second, nil},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			useFakeClock(t, 0, c.hold)

			got := collectBands(t, append(pressEvent(), releaseEvent()...))

			if len(got) != len(c.want) {
				t.Fatalf("bands = %v, want %v", got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Errorf("band[%d] = %v, want %v", i, got[i], c.want[i])
				}
			}
		})
	}
}

// TestWatchReaderTimesEachPressIndependently checks that consecutive presses do
// not bleed into one another — a second press must be measured from its own
// key-down, not from the first one.
func TestWatchReaderTimesEachPressIndependently(t *testing.T) {
	// tap at 0→200ms, then a long hold at 10s→15s.
	useFakeClock(t, 0, 200*time.Millisecond, 10*time.Second, 15*time.Second)

	stream := pressEvent()
	stream = append(stream, releaseEvent()...)
	stream = append(stream, pressEvent()...)
	stream = append(stream, releaseEvent()...)

	got := collectBands(t, stream)

	want := []Band{BandShortPress, BandLongHold}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("bands = %v, want %v", got, want)
	}
}

// TestWatchReaderIgnoresReleaseWithoutPress covers the button already being
// held when the device is opened: the first event seen is a release with no
// key-down to measure from. Treating it as a zero-length press would log a
// phantom press the operator never made.
func TestWatchReaderIgnoresReleaseWithoutPress(t *testing.T) {
	useFakeClock(t) // no instants: consulting the clock at all is the failure

	if got := collectBands(t, releaseEvent()); got != nil {
		t.Errorf("bands = %v, want none for a release with no press", got)
	}
}

// TestWatchReaderIgnoresAutorepeat guards isPress's autorepeat exclusion. If a
// repeat event restarted the timer, every hold would measure only the time
// since the last repeat and collapse into the debounce band. The scripted clock
// has exactly two instants, so a third consultation fails the test.
func TestWatchReaderIgnoresAutorepeat(t *testing.T) {
	useFakeClock(t, 0, 1500*time.Millisecond)

	stream := pressEvent()
	stream = append(stream, repeatEvent()...)
	stream = append(stream, repeatEvent()...)
	stream = append(stream, releaseEvent()...)

	got := collectBands(t, stream)

	if len(got) != 1 || got[0] != BandShortHold {
		t.Errorf("bands = %v, want [%v]", got, BandShortHold)
	}
}

// TestWatchReaderReturnsReadError confirms a mid-stream read failure ends the
// loop and propagates, rather than spinning: Watch's caller logs that the
// watcher exited, and a silent spin would burn a core forever.
func TestWatchReaderReturnsReadError(t *testing.T) {
	useFakeClock(t)

	truncated := pressEvent()[:eventSize-1]
	err := watchReader(bytes.NewReader(truncated), func(Band) {
		t.Error("onBand called for a truncated event")
	})
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("watchReader() err = %v, want io.ErrUnexpectedEOF", err)
	}
}
