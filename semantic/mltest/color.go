package mltest

import (
	"fmt"

	mlv1 "virex/proto/ml/v1"
)

const (
	step     = 4 // look at one pixel in 16
	minCells = 4
)

type pred func(r, g, b int) bool

var (
	isRed   pred = func(r, g, b int) bool { return r > 170 && g < 90 && b < 90 }
	isBlue  pred = func(r, g, b int) bool { return b > 170 && r < 90 && g < 90 }
	isGreen pred = func(r, g, b int) bool { return g > 170 && r < 90 && b < 90 }
)

// regions finds 4-connected blobs of pixels matching p and returns their boxes,
// normalised. It works on a grid sampled every `step` pixels.
func regions(img *mlv1.Image, p pred) []*mlv1.BBox {
	w, h := int(img.Width), int(img.Height)
	gw, gh := (w+step-1)/step, (h+step-1)/step
	mask := make([]bool, gw*gh)
	for gy := 0; gy < gh; gy++ {
		for gx := 0; gx < gw; gx++ {
			i := ((gy*step)*w + gx*step) * 3
			mask[gy*gw+gx] = p(int(img.Data[i]), int(img.Data[i+1]), int(img.Data[i+2]))
		}
	}
	seen := make([]bool, len(mask))
	var out []*mlv1.BBox
	for sy := 0; sy < gh; sy++ {
		for sx := 0; sx < gw; sx++ {
			if !mask[sy*gw+sx] || seen[sy*gw+sx] {
				continue
			}
			q := [][2]int{{sx, sy}}
			seen[sy*gw+sx] = true
			x1, x2, y1, y2, n := sx, sx, sy, sy, 0
			for len(q) > 0 {
				c := q[0]
				q = q[1:]
				n++
				x1, x2 = min(x1, c[0]), max(x2, c[0])
				y1, y2 = min(y1, c[1]), max(y2, c[1])
				for _, d := range [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
					nx, ny := c[0]+d[0], c[1]+d[1]
					if nx >= 0 && nx < gw && ny >= 0 && ny < gh && mask[ny*gw+nx] && !seen[ny*gw+nx] {
						seen[ny*gw+nx] = true
						q = append(q, [2]int{nx, ny})
					}
				}
			}
			if n >= minCells {
				out = append(out, &mlv1.BBox{
					X: float32(x1) / float32(gw), Y: float32(y1) / float32(gh),
					W: float32(x2+1-x1) / float32(gw), H: float32(y2+1-y1) / float32(gh),
				})
			}
		}
	}
	return out
}

func rawOnly(img *mlv1.Image) error {
	if img.GetFormat() != mlv1.Image_RGB24 {
		return fmt.Errorf("mltest colour model reads RGB24 only")
	}
	if len(img.Data) != int(img.Width)*int(img.Height)*3 {
		return fmt.Errorf("mltest: bad RGB24 size")
	}
	return nil
}

// ColorDetect is the fake detector: red -> person, blue -> cell phone.
func ColorDetect(r *mlv1.DetectRequest) (*mlv1.DetectResponse, error) {
	if err := rawOnly(r.Image); err != nil {
		return nil, BadInput()
	}
	resp := &mlv1.DetectResponse{
		Model:      &mlv1.ModelInfo{Name: "fake-colour-detector", Version: "test", Backend: "mltest", Device: "cpu"},
		RequestId:  r.RequestId,
		ImageWidth: r.Image.Width, ImageHeight: r.Image.Height, InferenceMs: 1, TotalMs: 2,
	}
	want := map[string]bool{}
	for _, c := range r.Classes {
		want[c] = true
	}
	for _, rule := range []struct {
		name string
		id   uint32
		p    pred
	}{{"person", 0, isRed}, {"cell phone", 67, isBlue}} {
		if len(want) > 0 && !want[rule.name] {
			continue
		}
		for _, b := range regions(r.Image, rule.p) {
			resp.Detections = append(resp.Detections, &mlv1.Detection{ClassName: rule.name, ClassId: rule.id, Confidence: 0.9, Box: b})
		}
	}
	return resp, nil
}

// ColorRead is the fake OCR: every green rectangle reads "FAKE TEXT".
func ColorRead(r *mlv1.ReadRequest) (*mlv1.ReadResponse, error) {
	if err := rawOnly(r.Image); err != nil {
		return nil, BadInput()
	}
	resp := &mlv1.ReadResponse{
		Model:      &mlv1.ModelInfo{Name: "fake-colour-ocr", Version: "test", Backend: "mltest", Device: "cpu"},
		RequestId:  r.RequestId,
		ImageWidth: r.Image.Width, ImageHeight: r.Image.Height, InferenceMs: 1, TotalMs: 2,
	}
	for _, b := range regions(r.Image, isGreen) {
		resp.Lines = append(resp.Lines, &mlv1.TextLine{Text: "FAKE TEXT", Confidence: 0.9, Box: b})
	}
	return resp, nil
}
