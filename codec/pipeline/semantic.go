package pipeline

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"virex/codec/encoder"
	"virex/codec/pixel"
	"virex/codec/sampler"
	"virex/codec/scheduler"
	"virex/cost"
	"virex/semantic/analyze"
	"virex/semantic/mlclient"
	"virex/semantic/objects"
	"virex/semantic/ocr"
	"virex/temporal/engine"
	"virex/temporal/tracker"
)

// SemanticML is the Config.Semantic value that runs the real ML pipeline.
const SemanticML = "ml"

// Default addresses of the two containers started by docker-compose.yml.
const (
	DefaultDetectAddr = "127.0.0.1:50051"
	DefaultOCRAddr    = "127.0.0.1:50052"
)

// MLConfig says where the ML containers are and how frames are sent to them. The
// embedded mlclient.Config supplies detect_addr, ocr_addr, timeout_seconds, retries,
// backoff_ms, max_message_mb, format and jpeg_quality.
type MLConfig struct {
	mlclient.Config

	// Workers is how many frames are analysed at once (default 4).
	Workers int `json:"workers"`
	// NoDetect and NoOCR switch a service off (for example to skip the slow OCR on footage
	// without text).
	NoDetect bool `json:"no_detect"`
	NoOCR    bool `json:"no_ocr"`

	// Detector options; zero means the container's own default.
	DetectMinConfidence float32  `json:"detect_min_confidence"`
	DetectMax           int      `json:"detect_max"`
	DetectClasses       []string `json:"detect_classes"`
	DetectSize          int      `json:"detect_size"`
	// OCRMinConfidence is the OCR confidence floor; zero means the container's default.
	OCRMinConfidence float32 `json:"ocr_min_confidence"`

	// StaticLevels: frames unchanged since the last OCR'd frame reuse its text (see
	// analyze.Config.StaticLevels). Default 6; negative turns reuse off.
	StaticLevels float64 `json:"static_levels"`
	// MaxFrameErrors is how many frames may fail before the encode aborts (default 0).
	MaxFrameErrors int `json:"max_frame_errors"`

	// ConnectTimeoutSeconds is how long to wait for a container to be reachable before
	// giving up (default 20); ReadyTimeoutSeconds is how long to wait for one that is up
	// but still loading its model (default 300).
	ConnectTimeoutSeconds float64 `json:"connect_timeout_seconds"`
	ReadyTimeoutSeconds   float64 `json:"ready_timeout_seconds"`
}

func (c MLConfig) withDefaults() MLConfig {
	if c.DetectAddr == "" && !c.NoDetect {
		c.DetectAddr = DefaultDetectAddr
	}
	if c.OCRAddr == "" && !c.NoOCR {
		c.OCRAddr = DefaultOCRAddr
	}
	if c.ConnectTimeoutSeconds <= 0 {
		c.ConnectTimeoutSeconds = 20
	}
	if c.ReadyTimeoutSeconds <= 0 {
		c.ReadyTimeoutSeconds = 300
	}
	return c
}

// ModelRecord is one model that produced results, as stored in the file's metadata.
type ModelRecord struct {
	Role    string `json:"role"` // detector or ocr
	Name    string `json:"name"`
	Version string `json:"version"`
	Backend string `json:"backend"`
	Device  string `json:"device"`
}

// SemanticMeta is what the semantic pass did, stored in the file's metadata.
type SemanticMeta struct {
	Frames       int      `json:"frames"`
	DetectCalls  int      `json:"detect_calls"`
	OCRCalls     int      `json:"ocr_calls"`
	OCRReused    int      `json:"ocr_reused"`
	FailedFrames int      `json:"failed_frames"`
	InferenceMS  float64  `json:"inference_ms"`
	Warnings     []string `json:"warnings,omitempty"`
}

// mlRuntime holds the open connections to the containers.
type mlRuntime struct {
	det    objects.Detector
	rdr    ocr.Reader
	conns  []*mlclient.Conn
	models []ModelRecord
}

func (r *mlRuntime) Close() {
	for _, c := range r.conns {
		c.Close()
	}
}

// openML connects to the containers and waits until their models are loaded, so a wrong
// address or a stopped container fails in seconds, before any video work is done.
func openML(ctx context.Context, cfg MLConfig) (*mlRuntime, error) {
	cfg = cfg.withDefaults()
	if cfg.NoDetect && cfg.NoOCR {
		return nil, errors.New("pipeline: ml.no_detect and ml.no_ocr are both set: nothing to run")
	}
	if err := cfg.Config.Validate(); err != nil {
		return nil, err
	}
	rt := &mlRuntime{}
	ok := false
	defer func() {
		if !ok {
			rt.Close()
		}
	}()

	dial := func(addr string) (*mlclient.Conn, error) {
		for _, c := range rt.conns { // role "all": one container serves both
			if c.Addr() == addr {
				return c, nil
			}
		}
		c, err := mlclient.Dial(addr, cfg.Config)
		if err != nil {
			return nil, err
		}
		rt.conns = append(rt.conns, c)
		return c, nil
	}
	type infoer interface {
		Info(context.Context) (mlclient.ModelInfo, bool, error)
	}
	wait := func(role, addr string, svc infoer) error {
		start := time.Now()
		connected := false
		var last error
		for {
			info, ready, err := svc.Info(ctx)
			switch {
			case err == nil && ready:
				rt.models = append(rt.models, ModelRecord{role, info.Name, info.Version, info.Backend, info.Device})
				return nil
			case err == nil:
				connected, last = true, fmt.Errorf("%s at %s is up but still loading its model", role, addr)
			default:
				if ctx.Err() != nil {
					return ctx.Err()
				}
				last = err
			}
			limit := cfg.ConnectTimeoutSeconds
			if connected {
				limit = cfg.ReadyTimeoutSeconds
			}
			if time.Since(start) > time.Duration(limit*float64(time.Second)) {
				return fmt.Errorf("pipeline: %s not ready after %.0fs: %w", role, limit, last)
			}
			select {
			case <-time.After(time.Second):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}

	if !cfg.NoDetect {
		c, err := dial(cfg.DetectAddr)
		if err != nil {
			return nil, err
		}
		d := objects.New(c)
		if err := wait("detector", cfg.DetectAddr, d); err != nil {
			return nil, err
		}
		rt.det = d
	}
	if !cfg.NoOCR {
		c, err := dial(cfg.OCRAddr)
		if err != nil {
			return nil, err
		}
		r := ocr.New(c)
		if err := wait("ocr", cfg.OCRAddr, r); err != nil {
			return nil, err
		}
		rt.rdr = r
	}
	ok = true
	return rt, nil
}

// maxSamplingGap is the longest the sampler can go without picking a frame, in seconds
// (0 = unbounded), for the warning about tracks that may fragment.
func maxSamplingGap(c sampler.Config, fpsNum, fpsDen int) float64 {
	switch c.Mode {
	case sampler.EverySeconds:
		if c.Seconds == 0 {
			return 1
		}
		return c.Seconds
	case sampler.EveryN:
		n := c.N
		if n == 0 {
			n = 30
		}
		if fpsNum > 0 && fpsDen > 0 {
			return float64(n) * float64(fpsDen) / float64(fpsNum)
		}
		return 0
	}
	return c.MaxGapSeconds
}

// samplingWarnings tells the user when the sampler is too sparse for the trackers.
func samplingWarnings(cfg Config, vi encoder.VideoInfo) []string {
	tcfg := cfg.Engine.Tracker
	gap := tcfg.MaxGapSeconds
	if gap <= 0 {
		gap = 3
	}
	g := maxSamplingGap(cfg.Sampler, vi.FPSNum, vi.FPSDen)
	if g == 0 || g > gap {
		which := "unbounded"
		if g > 0 {
			which = fmt.Sprintf("up to %.1fs", g)
		}
		return []string{fmt.Sprintf(
			"sampling gaps are %s but the tracker bridges only %.1fs: people and objects will split into several entities. "+
				"Set sampler.max_gap_seconds to 1 (see configs/phase2.json) or raise engine.tracker.max_gap_seconds", which, gap)}
	}
	return nil
}

// runSemantic is the semantic pass: decode, sample, send sampled frames to the ML
// containers (in parallel, results back in order) and feed the temporal engine. It also
// produces the sample list for the metadata, so the video is decoded only once.
func runSemantic(ctx context.Context, src string, vi encoder.VideoInfo, pkts []encoder.Packet, cfg Config, rt *mlRuntime, timescale uint32, log *cost.Logger) (*engine.Engine, []SampleRecord, analyze.Stats, error) {
	sp := log.Stage("semantic_pass")
	strat, err := sampler.New(cfg.Sampler, vi.Width, vi.Height)
	if err != nil {
		return nil, nil, analyze.Stats{}, err
	}
	ml := cfg.ML.withDefaults()
	an, err := analyze.New(rt.det, rt.rdr, analyze.Config{
		Workers: ml.Workers,
		Detect: objects.Options{
			MinConfidence: ml.DetectMinConfidence, MaxDetections: ml.DetectMax,
			Classes: ml.DetectClasses, InferenceSize: ml.DetectSize,
		},
		OCR:            ocr.Options{MinConfidence: ml.OCRMinConfidence},
		StaticLevels:   ml.StaticLevels,
		MaxFrameErrors: ml.MaxFrameErrors,
	}, log)
	if err != nil {
		return nil, nil, analyze.Stats{}, err
	}

	eng := engine.New(cfg.Engine, int64(timescale))
	toTicks := ptsScale(vi, timescale)
	var samples []SampleRecord

	run := an.Start(ctx, func(r analyze.Result) error { return eng.Add(toEngineFrame(r)) })
	_, schedErr := scheduler.Run(ctx, func(c context.Context) (<-chan encoder.Frame, func() error) {
		return encoder.Frames(c, src, vi, pkts, cfg.FrameBuffer)
	}, strat, scheduler.Options{Buffer: cfg.QueueBuffer}, func(s scheduler.Sampled) error {
		ticks := toTicks(s.Frame.PTS)
		samples = append(samples, SampleRecord{FrameID: s.Frame.ID, PTS: ticks, Reason: s.Reason})
		return run.Submit(analyze.Frame{
			ID: s.Frame.ID, Ticks: ticks, Time: s.Frame.Time, Reason: s.Reason,
			Image: mlclient.Image{Width: vi.Width, Height: vi.Height, RGB: s.Frame.Image},
		})
	})
	stats, closeErr := run.Close() // always: it stops the workers even when the scheduler failed
	sp.Note(fmt.Sprintf("%d frames analysed (%d detect, %d ocr, %d ocr reused, %d failed)",
		stats.Frames, stats.DetectCalls, stats.OCRCalls, stats.OCRReused, stats.FailedFrames))
	sp.End()
	if schedErr != nil {
		return nil, nil, stats, schedErr
	}
	if closeErr != nil {
		return nil, nil, stats, closeErr
	}
	return eng, samples, stats, nil
}

func toEngineFrame(r analyze.Result) engine.Frame {
	f := engine.Frame{ID: r.Meta.ID, Ticks: r.Meta.Ticks, Time: r.Meta.Time, Reason: r.Meta.Reason}
	for _, d := range r.Detections {
		f.Detections = append(f.Detections, tracker.Det{
			Class: d.Class, Conf: float64(d.Confidence),
			Box: tracker.Box{X: float64(d.Box.X), Y: float64(d.Box.Y), W: float64(d.Box.W), H: float64(d.Box.H)},
		})
	}
	for _, l := range r.Lines {
		f.Lines = append(f.Lines, tracker.TextLine{
			Text: l.Text, Conf: float64(l.Confidence),
			Box: tracker.Box{X: float64(l.Box.X), Y: float64(l.Box.Y), W: float64(l.Box.W), H: float64(l.Box.H)},
		})
	}
	return f
}

// encodeBoth runs the pixel pipeline and the semantic pass concurrently. Both only read
// the source file, so neither waits for the other; the first failure cancels the other
// and its error is the one returned.
func encodeBoth(ctx context.Context, src string, vi encoder.VideoInfo, pkts []encoder.Packet, work string, cfg Config, rt *mlRuntime, log *cost.Logger) (*pixel.Result, *engine.Engine, []SampleRecord, analyze.Stats, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		eng     *engine.Engine
		samples []SampleRecord
		stats   analyze.Stats
		semErr  error
		wg      sync.WaitGroup
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		eng, samples, stats, semErr = runSemantic(ctx, src, vi, pkts, cfg, rt, pixel.ChooseTimescale(vi), log)
		if semErr != nil {
			cancel()
		}
	}()
	px, pxErr := pixel.Encode(ctx, src, vi, pkts, work, cfg.Pixel, log)
	if pxErr != nil {
		cancel()
	}
	wg.Wait()

	// A cancellation is usually the echo of the other side's failure: report the cause.
	for _, err := range []error{pxErr, semErr} {
		if err != nil && !errors.Is(err, context.Canceled) {
			return nil, nil, nil, stats, err
		}
	}
	for _, err := range []error{pxErr, semErr} {
		if err != nil {
			return nil, nil, nil, stats, err
		}
	}
	return px, eng, samples, stats, nil
}
