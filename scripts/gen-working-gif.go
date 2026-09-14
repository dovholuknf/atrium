//go:build ignore

// Generates `internal/api/web/working.gif`, the mark that says a session is
// mid-turn.
//
//	go run scripts/gen-working-gif.go
//
// GENERATED RATHER THAN VENDORED. A binary somebody downloaded is a binary
// nobody can change: the frame count, the speed and the colour are decisions,
// and they should be readable and editable here rather than baked into bytes
// with no source. It is thirty lines of the standard library.
//
// WHY A GIF AT ALL, when CSS can do this. It was CSS, animating `opacity` and
// `transform`, which every account calls the cheap pair because they run on
// the compositor without layout or paint. Measured on a real board with a
// dozen sessions, the tab went from 7-9% of a core to 14-20%. The theory was
// right about the KIND of work and wrong about the amount: each animated mark
// becomes its own compositor layer, and a dozen of those beside a WebGL canvas
// is not free.
//
// A GIF is decoded once and its frames are cached. What it costs after that is
// one small blit per frame, and the frame rate is a number here rather than
// whatever the display refreshes at.
//
// WHAT IT COSTS: the colour is baked in. The CSS version took `currentColor`,
// so a mark wore its own session's theme. This cannot, so it is drawn in a
// neutral light grey that reads on every skin, and the ring around it still
// carries the session's colour.
package main

import (
	"image"
	"image/color"
	"image/gif"
	"log"
	"os"
)

const (
	size   = 24 // pixels, drawn at 2x the 12px it is shown at
	frames = 8
	// Hundredths of a second per frame. Eight frames at 12 is a turn every
	// ~1s, which reads as working without being a strobe. Slower than a CSS
	// animation on purpose: this is peripheral vision, not something anybody
	// watches.
	delay = 12
	// Eight dots around a circle, the classic. A dot is two pixels of radius
	// at this size, which stays a dot rather than a smear when the browser
	// scales it down.
	dots = 8
	dotR = 2.0
	ring = 8.0
)

func main() {
	// Transparent, one grey, and the steps between. Index 0 is transparent, so
	// everything not drawn shows what is behind it.
	pal := color.Palette{color.RGBA{}}
	for i := 1; i <= 8; i++ {
		v := uint8(60 + i*22)
		pal = append(pal, color.RGBA{v, v, v, 255})
	}

	out := &gif.GIF{}
	for f := 0; f < frames; f++ {
		img := image.NewPaletted(image.Rect(0, 0, size, size), pal)
		for d := 0; d < dots; d++ {
			// How far behind the leading dot this one is, so brightness
			// trails around the circle rather than blinking all at once.
			behind := (d - f + dots) % dots
			shade := 8 - behind
			if shade < 1 {
				shade = 1
			}
			cx, cy := dotAt(d)
			fillDisc(img, cx, cy, dotR, uint8(shade))
		}
		out.Image = append(out.Image, img)
		out.Delay = append(out.Delay, delay)
	}

	f, err := os.Create("internal/api/web/working.gif")
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	if err := gif.EncodeAll(f, out); err != nil {
		log.Fatal(err)
	}
	log.Printf("wrote internal/api/web/working.gif, %d frames", frames)
}

// dotAt is where dot `d` sits, clockwise from the top.
func dotAt(d int) (float64, float64) {
	// A quarter turn back, so dot zero is at twelve o'clock rather than three.
	a := float64(d)/float64(dots)*2*3.14159265 - 3.14159265/2
	return size/2 + ring*cos(a), size/2 + ring*sin(a)
}

// fillDisc paints a filled circle. Sampled rather than anti-aliased: the
// palette has eight shades and spending them on edges would leave none for the
// trail, which is the thing that says which way it is going.
func fillDisc(img *image.Paletted, cx, cy, r float64, idx uint8) {
	for y := int(cy - r - 1); y <= int(cy+r+1); y++ {
		for x := int(cx - r - 1); x <= int(cx+r+1); x++ {
			dx, dy := float64(x)-cx, float64(y)-cy
			if dx*dx+dy*dy <= r*r {
				if image.Pt(x, y).In(img.Rect) {
					img.SetColorIndex(x, y, idx)
				}
			}
		}
	}
}

// The two trig functions, so this does not import math for them alone and so
// the series it uses is visible. Accurate enough to place eight dots on a
// circle twenty four pixels across.
func cos(a float64) float64 { return sin(a + 3.14159265/2) }

func sin(a float64) float64 {
	for a > 3.14159265 {
		a -= 2 * 3.14159265
	}
	for a < -3.14159265 {
		a += 2 * 3.14159265
	}
	a2 := a * a
	return a * (1 - a2/6*(1-a2/20*(1-a2/42)))
}
