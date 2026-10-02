package sampler

import (
	"fmt"
	"math"

	"virex/codec/encoder"
)

// Reasons a frame was selected.
const (
	ReasonFirst    = "first"
	ReasonInterval = "interval"
	ReasonScene    = "scene"
	ReasonMaxGap   = "max_gap"
)

// Strategy names.
const (
	EveryN       = "every_n"       // every N-th frame
	EverySeconds = "every_seconds" // one frame per T seconds of video time
	SceneChange  = "scene_change"  // when the picture changes a lot
)

// Config selects and tunes a sampling strategy. Zero fields get defaults.
type Config struct {
	Mode    string  `json:"mode"`
	N       int     `json:"n"`       // every_n: frames between samples (default 30)
	Seconds float64 `json:"seconds"` // every_seconds: seconds between samples (default 1)

	// scene_change
	HistThreshold float64 `json:"hist_threshold"`  // 0..1 luma histogram distance (default DefaultHistThreshold)
	MADThreshold  float64 `json:"mad_threshold"`   // 0..1 thumbnail difference (default DefaultMADThreshold)
	MinGapSeconds float64 `json:"min_gap_seconds"` // ignore cuts closer than this to the last sample (default 0.25)
	MaxGapSeconds float64 `json:"max_gap_seconds"` // force a sample after this long without one; 0 = off
}

// Defaults for scene_change, calibrated on the test clips (see sampler_test.go).
const (
	DefaultHistThreshold = 0.35
	DefaultMADThreshold  = 0.12
)

// Strategy decides, frame by frame, which frames get semantic analysis. Frames must
// be offered in display order. A Strategy is not safe for concurrent use.
type Strategy interface {
	// Select reports whether f is sampled and why.
	Select(f encoder.Frame) (bool, string)
}

// New builds the strategy named in cfg. width and height are the frame size in pixels.
func New(cfg Config, width, height int) (Strategy, error) {
	switch cfg.Mode {
	case EveryN:
		if cfg.N == 0 {
			cfg.N = 30
		}
		if cfg.N < 1 {
			return nil, fmt.Errorf("sampler: n must be >= 1")
		}
		return &everyN{n: cfg.N}, nil
	case EverySeconds:
		if cfg.Seconds == 0 {
			cfg.Seconds = 1
		}
		if cfg.Seconds <= 0 {
			return nil, fmt.Errorf("sampler: seconds must be > 0")
		}
		return &everySeconds{step: cfg.Seconds, next: 0}, nil
	case SceneChange:
		if width <= 0 || height <= 0 {
			return nil, fmt.Errorf("sampler: scene_change needs the frame size")
		}
		if cfg.HistThreshold == 0 {
			cfg.HistThreshold = DefaultHistThreshold
		}
		if cfg.MADThreshold == 0 {
			cfg.MADThreshold = DefaultMADThreshold
		}
		if cfg.MinGapSeconds == 0 {
			cfg.MinGapSeconds = 0.25
		}
		return &scene{cfg: cfg, w: width, h: height}, nil
	}
	return nil, fmt.Errorf("sampler: unknown mode %q (want %s, %s or %s)", cfg.Mode, EveryN, EverySeconds, SceneChange)
}

type everyN struct {
	n, seen int
}

func (s *everyN) Select(f encoder.Frame) (bool, string) {
	i := s.seen
	s.seen++
	if i == 0 {
		return true, ReasonFirst
	}
	return i%s.n == 0, ReasonInterval
}

// everySeconds uses the real frame time, so it is right for variable frame rate.
// It takes the first frame at or after each multiple of step.
type everySeconds struct {
	step, next float64
	started    bool
}

func (s *everySeconds) Select(f encoder.Frame) (bool, string) {
	const eps = 1e-9
	if !s.started {
		s.started = true
		s.next = (math.Floor(f.Time/s.step+eps) + 1) * s.step
		return true, ReasonFirst
	}
	if f.Time+eps < s.next {
		return false, ""
	}
	s.next = (math.Floor(f.Time/s.step+eps) + 1) * s.step
	return true, ReasonInterval
}

const (
	histBins  = 64
	thumbW    = 16
	thumbH    = 9
	pixelStep = 4 // look at one pixel in 16
)

// signature is a cheap summary of a frame used to compare it with the previous one.
type signature struct {
	hist  [histBins]float64 // luma histogram, sums to 1
	thumb [thumbW * thumbH]float64
}

func computeSignature(img []byte, w, h int) signature {
	var s signature
	var cnt [thumbW * thumbH]float64
	var n float64
	for y := 0; y < h; y += pixelStep {
		row := y * w * 3
		ty := y * thumbH / h
		for x := 0; x < w; x += pixelStep {
			i := row + x*3
			// Integer luma, Rec.601 weights.
			l := (299*int(img[i]) + 587*int(img[i+1]) + 114*int(img[i+2])) / 1000
			s.hist[l>>2]++
			n++
			c := ty*thumbW + x*thumbW/w
			s.thumb[c] += float64(l)
			cnt[c]++
		}
	}
	if n > 0 {
		for i := range s.hist {
			s.hist[i] /= n
		}
	}
	for i := range s.thumb {
		if cnt[i] > 0 {
			s.thumb[i] /= cnt[i]
		}
	}
	return s
}

// Score compares two frames' signatures: hist is half the L1 distance between luma
// histograms (0 = same, 1 = disjoint); mad is the mean absolute difference of the
// 16x9 luma thumbnails divided by 255.
func (a signature) score(b signature) (hist, mad float64) {
	for i := range a.hist {
		hist += math.Abs(a.hist[i] - b.hist[i])
	}
	hist /= 2
	for i := range a.thumb {
		mad += math.Abs(a.thumb[i] - b.thumb[i])
	}
	mad /= float64(len(a.thumb)) * 255
	return hist, mad
}

// Scores returns the (hist, mad) scene-change scores between two RGB24 frames.
// Exported so thresholds can be calibrated and tested.
func Scores(prev, cur []byte, w, h int) (hist, mad float64) {
	return computeSignature(prev, w, h).score(computeSignature(cur, w, h))
}

type scene struct {
	cfg        Config
	w, h       int
	prev       signature
	havePrev   bool
	lastSample float64
	sampled    bool
}

func (s *scene) Select(f encoder.Frame) (bool, string) {
	sig := computeSignature(f.Image, s.w, s.h)
	defer func() { s.prev, s.havePrev = sig, true }()

	if !s.sampled {
		s.sampled, s.lastSample = true, f.Time
		return true, ReasonFirst
	}
	hist, mad := sig.score(s.prev)
	since := f.Time - s.lastSample
	if (hist >= s.cfg.HistThreshold || mad >= s.cfg.MADThreshold) && since >= s.cfg.MinGapSeconds {
		s.lastSample = f.Time
		return true, ReasonScene
	}
	if s.cfg.MaxGapSeconds > 0 && since >= s.cfg.MaxGapSeconds {
		s.lastSample = f.Time
		return true, ReasonMaxGap
	}
	return false, ""
}
