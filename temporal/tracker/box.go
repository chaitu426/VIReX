// Package tracker turns per-frame detections into persistent entities with lifetimes:
// an IoU + Hungarian tracker for objects and a fuzzy-matching tracker for OCR text.
package tracker

import "math"

// Box is a rectangle in normalised coordinates (0..1, origin top-left, x/y = top-left
// corner), the same convention as SVIR's BBox.
type Box struct{ X, Y, W, H float64 }

func (b Box) Area() float64          { return math.Max(b.W, 0) * math.Max(b.H, 0) }
func (b Box) Center() (x, y float64) { return b.X + b.W/2, b.Y + b.H/2 }
func (b Box) Right() float64         { return b.X + b.W }
func (b Box) Bottom() float64        { return b.Y + b.H }

// Diag is the length of the box diagonal.
func (b Box) Diag() float64 { return math.Hypot(b.W, b.H) }

// Intersection is the area shared by a and b.
func Intersection(a, b Box) float64 {
	w := math.Min(a.Right(), b.Right()) - math.Max(a.X, b.X)
	h := math.Min(a.Bottom(), b.Bottom()) - math.Max(a.Y, b.Y)
	if w <= 0 || h <= 0 {
		return 0
	}
	return w * h
}

// IoU is intersection over union, 0 when the boxes do not touch or either is empty.
func IoU(a, b Box) float64 {
	in := Intersection(a, b)
	if in == 0 {
		return 0
	}
	return in / (a.Area() + b.Area() - in)
}

// CenterDistance is the distance between the two box centres.
func CenterDistance(a, b Box) float64 {
	ax, ay := a.Center()
	bx, by := b.Center()
	return math.Hypot(ax-bx, ay-by)
}

// Union is the smallest box containing both.
func Union(a, b Box) Box {
	x, y := math.Min(a.X, b.X), math.Min(a.Y, b.Y)
	return Box{x, y, math.Max(a.Right(), b.Right()) - x, math.Max(a.Bottom(), b.Bottom()) - y}
}

// Clamp keeps the box inside the frame.
func (b Box) Clamp() Box {
	x, y := math.Min(math.Max(b.X, 0), 1), math.Min(math.Max(b.Y, 0), 1)
	return Box{x, y, math.Min(math.Max(b.W, 0), 1-x), math.Min(math.Max(b.H, 0), 1-y)}
}
