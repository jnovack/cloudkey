package display

import (
	"image"
	"image/color"
	"image/draw"
	"sync"
	"testing"
)

// TestScreenMuGuardsConcurrentAccess is the regression test for #DISP-RACE-01:
// a build* goroutine redrawing a screen (drawX under screenMu) concurrently
// with the fade carousel reading it (fadeStep under screenMu). Run with
// `go test -race`: dropping either lock reintroduces the race and this test
// catches it.
func TestScreenMuGuardsConcurrentAccess(t *testing.T) {
	fb = image.NewRGBA(image.Rect(0, 0, 160, 64))
	target := image.NewRGBA(fb.Bounds())

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			screenMu.Lock()
			draw.Draw(target, target.Bounds(), image.NewUniform(color.Gray{0x42}), image.Point{}, draw.Src)
			screenMu.Unlock()
		}
	})

	for range 200 {
		fadeStep(target, fades[0])
	}
	close(stop)
	wg.Wait()
}
