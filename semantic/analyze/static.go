package analyze

import (
	"math"

	"virex/semantic/mlclient"
)

// A lecture slide stays on screen for a minute while the sampler keeps picking frames.
// Running OCR on each copy is the most expensive thing the pipeline could do, so frames
// that look identical to the last OCR'd frame reuse its text.
//
// "Identical" must not hide a new bullet point, so the test is on the worst cell, not
// the average: the frame is cut into a 48x27 grid, each cell is reduced to its mean
// luma, and the frames match only if EVERY cell moved less than a few levels. Codec
// noise averages out inside a cell (well under 2 levels); a line of text changes the
// cells under it by tens of levels.
const (
	gridW = 48
	gridH = 27
	// sampleStride reads one pixel in 9 inside each cell.
	sampleStride = 3
)

type signature [gridW * gridH]float32

func computeSignature(im mlclient.Image) signature {
	var sig signature
	w, h := im.Width, im.Height
	for cy := 0; cy < gridH; cy++ {
		y0, y1 := cy*h/gridH, (cy+1)*h/gridH
		for cx := 0; cx < gridW; cx++ {
			x0, x1 := cx*w/gridW, (cx+1)*w/gridW
			var sum, n float64
			for y := y0; y < y1; y += sampleStride {
				row := y * w * 3
				for x := x0; x < x1; x += sampleStride {
					i := row + x*3
					sum += float64(299*int(im.RGB[i])+587*int(im.RGB[i+1])+114*int(im.RGB[i+2])) / 1000
					n++
				}
			}
			if n > 0 {
				sig[cy*gridW+cx] = float32(sum / n)
			}
		}
	}
	return sig
}

// similar reports whether every cell of a is within levels of the same cell of b.
func (a *signature) similar(b *signature, levels float64) bool {
	for i := range a {
		if math.Abs(float64(a[i]-b[i])) >= levels {
			return false
		}
	}
	return true
}
