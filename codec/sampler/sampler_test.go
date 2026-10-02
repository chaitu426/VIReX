package sampler

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"virex/codec/encoder"
)

const clipDir = "../../tests/testdata/clips"

// frame builds a synthetic RGB24 frame for strategy tests that need no video file.
func frame(id int, t float64, w, h int, rgb [3]byte) encoder.Frame {
	img := make([]byte, w*h*3)
	for i := 0; i < len(img); i += 3 {
		img[i], img[i+1], img[i+2] = rgb[0], rgb[1], rgb[2]
	}
	return encoder.Frame{ID: id, Time: t, Image: img}
}

func ids(s Strategy, frames []encoder.Frame) []int {
	var out []int
	for _, f := range frames {
		if ok, _ := s.Select(f); ok {
			out = append(out, f.ID)
		}
	}
	return out
}

func equal(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestEveryN(t *testing.T) {
	s, _ := New(Config{Mode: EveryN, N: 3}, 0, 0)
	var fr []encoder.Frame
	for i := 0; i < 10; i++ {
		fr = append(fr, encoder.Frame{ID: i})
	}
	if got := ids(s, fr); !equal(got, []int{0, 3, 6, 9}) {
		t.Fatalf("got %v", got)
	}
}

func TestEverySecondsUsesRealTimeNotFrameCount(t *testing.T) {
	// Irregular times (variable frame rate): 0, .1, .4, .9, 1.0, 1.05, 2.5, 2.6, 3.0
	times := []float64{0, 0.1, 0.4, 0.9, 1.0, 1.05, 2.5, 2.6, 3.0}
	var fr []encoder.Frame
	for i, tm := range times {
		fr = append(fr, encoder.Frame{ID: i, Time: tm})
	}
	s, _ := New(Config{Mode: EverySeconds, Seconds: 1}, 0, 0)
	// first frame, then first frame at/after t=1 (id 4), then t=2 -> 2.5 (id 6), then 3.0 (id 8)
	if got := ids(s, fr); !equal(got, []int{0, 4, 6, 8}) {
		t.Fatalf("got %v", got)
	}
}

func TestSceneChangeSynthetic(t *testing.T) {
	const w, h = 64, 36
	black, white := [3]byte{10, 10, 10}, [3]byte{240, 240, 240}
	var fr []encoder.Frame
	for i := 0; i < 30; i++ {
		c := black
		if i >= 10 {
			c = white
		}
		if i >= 20 {
			c = black
		}
		fr = append(fr, frame(i, float64(i)/30, w, h, c))
	}
	s, _ := New(Config{Mode: SceneChange}, w, h)
	if got := ids(s, fr); !equal(got, []int{0, 10, 20}) {
		t.Fatalf("got %v", got)
	}
}

func TestSceneChangeMinAndMaxGap(t *testing.T) {
	const w, h = 32, 18
	a, b := [3]byte{0, 0, 0}, [3]byte{255, 255, 255}
	// Flicker every frame at 30 fps: min gap 0.25 s keeps it from firing every frame.
	var fr []encoder.Frame
	for i := 0; i < 30; i++ {
		c := a
		if i%2 == 1 {
			c = b
		}
		fr = append(fr, frame(i, float64(i)/30, w, h, c))
	}
	s, _ := New(Config{Mode: SceneChange, MinGapSeconds: 0.25}, w, h)
	got := ids(s, fr)
	for i := 1; i < len(got); i++ {
		if float64(got[i]-got[i-1])/30 < 0.25-1e-9 {
			t.Fatalf("samples closer than the min gap: %v", got)
		}
	}
	// A still picture still gets sampled every 0.5 s with MaxGapSeconds.
	var still []encoder.Frame
	for i := 0; i < 60; i++ {
		still = append(still, frame(i, float64(i)/30, w, h, a))
	}
	s2, _ := New(Config{Mode: SceneChange, MaxGapSeconds: 0.5}, w, h)
	if got := ids(s2, still); !equal(got, []int{0, 15, 30, 45}) {
		t.Fatalf("max gap: got %v", got)
	}
}

func TestConfigErrors(t *testing.T) {
	if _, err := New(Config{Mode: "nope"}, 10, 10); err == nil {
		t.Fatal("unknown mode accepted")
	}
	if _, err := New(Config{Mode: EveryN, N: -1}, 0, 0); err == nil {
		t.Fatal("negative n accepted")
	}
	if _, err := New(Config{Mode: SceneChange}, 0, 0); err == nil {
		t.Fatal("scene_change without size accepted")
	}
}

// sampleClip runs the sampler over a real clip and returns the selected frame ids.
func sampleClip(t *testing.T, name string, cfg Config) []int {
	t.Helper()
	path := filepath.Join(clipDir, name)
	if _, err := os.Stat(path); err != nil {
		t.Skipf("%s missing; run scripts/make-testclips.ps1", name)
	}
	ctx := context.Background()
	vi, err := encoder.Probe(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	pk, err := encoder.Packets(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(cfg, vi.Width, vi.Height)
	if err != nil {
		t.Fatal(err)
	}
	ch, wait := encoder.Frames(ctx, path, vi, pk, 4)
	var out []int
	for f := range ch {
		if ok, _ := s.Select(f); ok {
			out = append(out, f.ID)
		}
	}
	if err := wait(); err != nil {
		t.Fatal(err)
	}
	return out
}

// c04 has hard cuts at 2.5, 5.0 and 7.5 s = frames 75, 150, 225 at 30 fps.
func TestSceneChangeFindsRealCuts(t *testing.T) {
	got := sampleClip(t, "c04_cuts_360p.mp4", Config{Mode: SceneChange})
	want := []int{0, 75, 150, 225}
	if !equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestSceneChangeNoFalseCuts(t *testing.T) {
	for _, c := range []string{
		"c01_mandelbrot_720p.mp4", "c02_testsrc2_360p.mp4", "c03_life_360p.mp4",
		"c05_vfr_360p.mp4", "c07_noise_360p.mp4", "c08_static_360p.mp4",
	} {
		t.Run(c, func(t *testing.T) {
			if got := sampleClip(t, c, Config{Mode: SceneChange}); !equal(got, []int{0}) {
				t.Fatalf("false scene changes: %v", got)
			}
		})
	}
}

func TestEverySecondsOnVFRClip(t *testing.T) {
	got := sampleClip(t, "c05_vfr_360p.mp4", Config{Mode: EverySeconds, Seconds: 2})
	// 10 s clip, one sample per 2 s -> 5 samples (t = 0, 2, 4, 6, 8).
	if len(got) != 5 || got[0] != 0 {
		t.Fatalf("got %v", got)
	}
}
