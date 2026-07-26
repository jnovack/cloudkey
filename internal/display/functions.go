package display

import (
	"image"
	"image/color"
	"image/draw"
	"math"
	"time"

	"github.com/golang/freetype"
	"github.com/golang/freetype/truetype"
	"github.com/rs/zerolog/log"

	"github.com/jnovack/cloudkey/internal/fonts"
)

// Colors from Black to White
var colors = []color.Gray{
	color.Gray{0x00},
	color.Gray{0x11},
	color.Gray{0x22},
	color.Gray{0x33},
	color.Gray{0x44},
	color.Gray{0x55},
	color.Gray{0x66},
	color.Gray{0x77},
	color.Gray{0x88},
	color.Gray{0x99},
	color.Gray{0xaa},
	color.Gray{0xbb},
	color.Gray{0xcc},
	color.Gray{0xdd},
	color.Gray{0xee},
	color.Gray{0xff},
}

// Increasing alpha fades out, decreasing alpha fades in.
// No need to fade EVERY step.
var fades = []color.Alpha{
	color.Alpha{0xff},
	color.Alpha{0xcc},
	color.Alpha{0x99},
	color.Alpha{0x66},
	color.Alpha{0x33},
	color.Alpha{0x00},
}

// clearScreen clears... the... screen
func clearScreen() {
	draw.Draw(fb, fb.Bounds(), image.NewUniform(color.Gray{0}), image.Point{}, draw.Src)
}

// fadeStep renders target over the framebuffer at the given alpha; shared by
// fadeOut and fadeIn. target is one of the shared images in screens, which a
// build* goroutine may be redrawing concurrently (see screens.go), so the
// pixel read is held under screenMu — the same lock those goroutines take
// before calling their drawX function.
func fadeStep(target draw.Image, alpha color.Alpha) {
	bg := image.NewGray(fb.Bounds())
	draw.Draw(bg, bg.Bounds(), image.NewUniform(color.Gray{0}), image.Point{}, draw.Src)

	screenMu.Lock()
	draw.DrawMask(bg, bg.Bounds(), target, image.Point{}, image.NewUniform(alpha), image.Point{}, draw.Over)
	screenMu.Unlock()

	draw.Draw(fb, fb.Bounds(), bg, image.Point{}, draw.Over)
}

// fadeOut crossfades the framebuffer's current contents down to black.
func fadeOut() {
	capture := image.NewGray(fb.Bounds())
	draw.Draw(capture, capture.Bounds(), fb, image.Point{}, draw.Src)
	for x := range fades {
		fadeStep(capture, fades[x])
		time.Sleep(8 * time.Millisecond)
	}
}

// fadeIn crossfades the framebuffer from black up to target.
func fadeIn(target draw.Image) {
	for x := len(fades) - 1; x >= 0; x-- {
		fadeStep(target, fades[x])
		time.Sleep(8 * time.Millisecond)
	}
}

// holdOrToggle sleeps for d, returning true if a stealth toggle arrived first.
//
// The carousel's holds are seconds long, so a press has to cut the current one
// short rather than wait it out: otherwise engaging stealth lags by up to
// -delay + -blank-delay milliseconds after the button, which reads as a dead
// button and invites a second press. The fades themselves stay uninterruptible
// — they are tens of milliseconds, and aborting mid-crossfade would leave the
// panel at a partial alpha.
func holdOrToggle(d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()

	select {
	case <-stealthToggle:
		return true
	case <-t.C:
		return false
	}
}

// startFadeCarousel cycles the screens with a real duty cycle: each screen
// fades in and holds lit for onDelay, then fades to a genuinely black,
// held-blank state for offDelay before the next screen. The off-hold (not
// just the brief fade transition) is what actually gives the OLED panel
// rest time between screens.
//
// It also owns every stealth-mode transition. That is not incidental placement:
// this is the only goroutine that writes fb (see the screens/screenMu note in
// display.go), so the button watcher can signal but never draw, and the panel
// work of engaging or leaving stealth has to happen here.
//
// The rotation index is tracked explicitly rather than by a range loop so that
// leaving stealth resumes where the carousel was interrupted, instead of
// visibly restarting the rotation from the first screen.
func startFadeCarousel(onDelay, offDelay float64) {
	// screens is built once in New and every registry entry but the optional
	// VPN/tunnel ones is always enabled, so this is unreachable in production.
	// Guarding anyway: the modulo below would panic on an empty slice, and the
	// loop this replaced spun a core flat out instead.
	if len(screens) == 0 {
		log.Warn().Msg("no screens enabled, carousel not started")
		return
	}

	on := time.Duration(onDelay) * time.Millisecond
	off := time.Duration(offDelay) * time.Millisecond

	idx := 0
	for {
		if stealthOn.Load() {
			// Park indefinitely. Only a button press wakes the panel, and there
			// is nothing to redraw meanwhile — the screens' own goroutines keep
			// updating their images and feeding the hub regardless.
			<-stealthToggle
			exitStealth()

			// Go straight to a lit screen, skipping the blank hold below. The
			// panel is already dark, and holding it dark for another
			// -blank-delay after the press would read as a press that did not
			// take — which invites a second press, turning stealth back on.
			if showScreen(screens[idx], on) {
				enterStealth()
			}
			idx = (idx + 1) % len(screens)
			continue
		}

		fadeOut()
		if holdOrToggle(off) {
			enterStealth()
			continue
		}

		if showScreen(screens[idx], on) {
			enterStealth()
		}
		idx = (idx + 1) % len(screens)
	}
}

// showScreen fades s in and holds it lit, reporting whether a stealth toggle
// cut the hold short.
func showScreen(s draw.Image, on time.Duration) bool {
	fadeIn(s)
	return holdOrToggle(on)
}

// textColor caps drawn text below full white. OLED wear tracks drive
// current, and dimmer text is still legible at these font sizes.
var textColor = image.NewUniform(colors[12]) // 0xcc, ~80% white

// shiftOffsets is a slow 8-position drift pattern. Centered text moves
// through it a couple pixels at a time so repeated redraws don't keep the
// same panel pixels lit at the same spot indefinitely (OLED burn-in).
var shiftOffsets = []image.Point{
	{0, 0}, {2, 0}, {2, 2}, {0, 2},
	{-2, 2}, {-2, 0}, {-2, -2}, {0, -2},
}

const shiftPeriod = 4 * time.Hour

// pixelShift returns the current drift offset for t, advancing one step
// through shiftOffsets every shiftPeriod (full cycle ~= 32 hours).
func pixelShift(t time.Time) image.Point {
	idx := int(t.Unix()/int64(shiftPeriod.Seconds())) % len(shiftOffsets)
	return shiftOffsets[idx]
}

// write draws text to a x,y coordinate on the image. No bold typeface is
// embedded, so bold is faked by drawing the glyphs twice with a 1px
// horizontal offset, which thickens the strokes enough to read as bold on
// the small OLED screens.
func write(screen draw.Image, text string, x, y int, size float64, fontname string, bold bool) {
	font := fonts.Load(fontname)
	if font == nil {
		log.Warn().Str("font", fontname).Msg("write: unregistered font")
		return
	}
	// Setup new context
	c := freetype.NewContext()
	c.SetFont(font)            // Set the font
	c.SetFontSize(size)        // Set font size
	c.SetDPI(72)               // Fixed DPI
	c.SetClip(screen.Bounds()) // Clip the text?
	c.SetDst(screen)           // Send it where?
	c.SetSrc(textColor)        // Color of Foreground

	baseY := y + int(c.PointToFixed(math.Round(float64(size)+1))>>6) // y is center of line, shift to top of line
	if bold {
		if _, err := c.DrawString(text, freetype.Pt(x+1, baseY)); err != nil {
			log.Warn().Err(err).Str("text", text).Msg("draw string")
			return
		}
	}
	if _, err := c.DrawString(text, freetype.Pt(x, baseY)); err != nil {
		log.Warn().Err(err).Str("text", text).Msg("draw string")
		return
	}
}

// textWidth measures the rendered pixel width of text at size in fontname,
// so callers can position it (e.g. horizontally centering it, or aligning a
// block of multiple lines to a shared left edge). ok is false if any rune in
// text has no glyph in the font, matching truetype.Face.GlyphAdvance.
func textWidth(text string, size float64, fontname string) (width int, ok bool) {
	font := fonts.Load(fontname)
	if font == nil {
		// truetype.NewFace nil-dereferences on a nil font, which would panic a
		// redraw goroutine and take the whole daemon with it.
		return 0, false
	}
	opts := truetype.Options{}
	opts.DPI = 72
	opts.Size = size + 1
	face := truetype.NewFace(font, &opts)

	for _, t := range text {
		awidth, ok := face.GlyphAdvance(rune(t))
		if !ok {
			return 0, false
		}
		width += int(float64(awidth) / 64)
	}
	return width, true
}

// center horizontally centers text on the given screen at vertical position y.
func center(screen draw.Image, text string, y int, size float64, fontname string, bold bool) {
	width, ok := textWidth(text, size, fontname)
	if !ok {
		return
	}
	shift := pixelShift(time.Now())
	write(screen, text, screen.Bounds().Max.X/2-width/2+shift.X, y+shift.Y, size, fontname, bold)
}
