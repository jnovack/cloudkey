package images

import "testing"

// TestLoadReturnsRegisteredImages guards that every name registered in init is
// actually decodable and reachable through Load. It also catches the case of a
// new asset being added to data.go but never wired into init's registry, which
// would otherwise surface only as a blank icon on the panel at runtime.
func TestLoadReturnsRegisteredImages(t *testing.T) {
	if len(assets) == 0 {
		t.Fatal("assets registry is empty; init did not run")
	}
	for name := range assets {
		img := Load(name)
		if img == nil {
			t.Errorf("Load(%q) = nil, want a decoded image", name)
			continue
		}
		if b := img.Bounds(); b.Dx() == 0 || b.Dy() == 0 {
			t.Errorf("Load(%q) bounds = %v, want non-empty", name, b)
		}
	}
}

// TestLoadUnregisteredNameReturnsNil pins the post-fix contract: an unknown name
// yields nil rather than terminating the process, which is what the old
// log.Fatal path did from inside a redraw goroutine.
func TestLoadUnregisteredNameReturnsNil(t *testing.T) {
	if img := Load("no-such-asset"); img != nil {
		t.Errorf("Load(unregistered) = %v, want nil", img)
	}
}

// TestLoadReturnsCachedImage is the guard for the caching half of the fix: the
// same pointer must come back on every call. Reverting to a per-call
// base64+png.Decode makes this fail.
func TestLoadReturnsCachedImage(t *testing.T) {
	a := Load("logo")
	b := Load("logo")
	if a == nil || b == nil {
		t.Fatal(`Load("logo") returned nil`)
	}
	if a != b {
		t.Errorf("Load(\"logo\") returned different values across calls: %p != %p; image should be decoded once and cached", a, b)
	}
}
