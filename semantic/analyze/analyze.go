// Package analyze runs the ML clients over sampled frames in parallel and hands the
// results on in frame order.
//
// Frames come in one at a time from the scheduler's sink. Submit blocks when every
// worker is busy and the reorder queue is full, so the back-pressure promise of the
// scheduler (bounded memory however long the video) still holds with ML in the loop.
package analyze

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"virex/cost"
	"virex/semantic/mlclient"
	"virex/semantic/objects"
	"virex/semantic/ocr"
)

// Frame is one sampled frame to analyse.
type Frame struct {
	ID     int     // 0-based display-order frame index
	Ticks  int64   // presentation time in container ticks
	Time   float64 // presentation time in seconds
	Reason string  // why the sampler picked it ("scene", "interval", ...)
	Image  mlclient.Image
}

// Meta is a Frame without its pixels, kept in results so memory is freed early.
type Meta struct {
	ID            int
	Ticks         int64
	Time          float64
	Reason        string
	Width, Height int
}

// Result is everything the ML containers said about one frame.
type Result struct {
	Meta       Meta
	Detections []objects.Detection
	Lines      []ocr.Line
	// OCRReused is true when the frame looked identical to the last OCR'd frame, so
	// Lines are that frame's lines and no OCR call was made.
	OCRReused bool
}

// Config tunes an Analyzer.
type Config struct {
	// Workers is how many frames are in flight at once (default 4).
	Workers int `json:"workers"`
	// Detect and OCR are the per-call options sent to the services.
	Detect objects.Options `json:"-"`
	OCR    ocr.Options     `json:"-"`

	// StaticLevels: a frame whose every 1/48 x 1/27 luma cell differs from the last
	// OCR'd frame by less than this many levels (0..255) reuses that frame's text
	// instead of calling OCR again. Default 6; negative turns reuse off.
	StaticLevels float64 `json:"static_levels"`

	// MaxFrameErrors is how many frames may fail (after retries) before the run
	// aborts. Failed frames are skipped; tracking tolerates the gap. Default 0: any
	// failure is fatal, because a silently incomplete SVIR is worse than no SVIR.
	MaxFrameErrors int `json:"max_frame_errors"`
}

func (c Config) withDefaults() Config {
	if c.Workers < 1 {
		c.Workers = 4
	}
	if c.StaticLevels == 0 {
		c.StaticLevels = 6
	}
	return c
}

// Stats summarise a run.
type Stats struct {
	Frames        int
	DetectCalls   int
	OCRCalls      int
	OCRReused     int
	FailedFrames  int
	InferenceMS   float64 // summed server-side model time
	Detector, OCR mlclient.ModelInfo
	Warnings      []string
}

// Analyzer ties a detector and (optionally) a text reader to a worker pool. Either may
// be nil; a nil reader means no OCR at all.
type Analyzer struct {
	det objects.Detector
	rdr ocr.Reader
	cfg Config
	log *cost.Logger
}

// New builds an Analyzer. log may be nil.
func New(det objects.Detector, rdr ocr.Reader, cfg Config, log *cost.Logger) (*Analyzer, error) {
	if det == nil && rdr == nil {
		return nil, errors.New("analyze: need a detector or a reader")
	}
	return &Analyzer{det: det, rdr: rdr, cfg: cfg.withDefaults(), log: log}, nil
}

type job struct {
	seq    int
	frame  Frame
	reuse  bool // OCR is skipped: take the previous OCR'd frame's lines
	res    Result
	err    error // detector and/or OCR failure
	ocrErr error // the OCR part of err, so a good OCR result survives a detector failure
	done   chan struct{}
	detRes objects.Result
	ocrRes ocr.Result
}

// Run is one pass over a stream of frames.
type Run struct {
	a       *Analyzer
	ctx     context.Context
	cancel  context.CancelFunc
	handler func(Result) error

	work  chan *job
	order chan *job
	wg    sync.WaitGroup // workers
	cwg   sync.WaitGroup // collector

	seq     int
	lastSig *signature // of the last frame sent to OCR; touched only by Submit

	mu       sync.Mutex
	firstErr error
	stats    Stats
	failed   atomic.Int64
}

// Start begins a run. handler receives results in frame order, one at a time, from a
// single goroutine. It must not call Submit or Close.
func (a *Analyzer) Start(ctx context.Context, handler func(Result) error) *Run {
	ctx, cancel := context.WithCancel(ctx)
	r := &Run{
		a: a, ctx: ctx, cancel: cancel, handler: handler,
		work:  make(chan *job, a.cfg.Workers),
		order: make(chan *job, a.cfg.Workers*2),
	}
	for i := 0; i < a.cfg.Workers; i++ {
		r.wg.Add(1)
		go r.worker()
	}
	r.cwg.Add(1)
	go r.collect()
	return r
}

func (r *Run) fail(err error) {
	r.mu.Lock()
	if r.firstErr == nil {
		r.firstErr = err
		r.cancel()
	}
	r.mu.Unlock()
}

// Submit queues a frame. It blocks while the pool is saturated and returns an error
// as soon as the run has failed or ctx is cancelled.
func (r *Run) Submit(f Frame) error {
	if err := r.err(); err != nil {
		return err
	}
	j := &job{seq: r.seq, frame: f, done: make(chan struct{})}
	r.seq++
	if r.a.rdr != nil && r.a.cfg.StaticLevels > 0 && f.Image.Width >= gridW && f.Image.Height >= gridH {
		sig := computeSignature(f.Image)
		if r.lastSig != nil && sig.similar(r.lastSig, r.a.cfg.StaticLevels) {
			j.reuse = true
		} else {
			r.lastSig = &sig
		}
	}
	select {
	case r.work <- j:
	case <-r.ctx.Done():
		return r.err()
	}
	select {
	case r.order <- j:
	case <-r.ctx.Done():
		return r.err()
	}
	return nil
}

func (r *Run) err() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.firstErr != nil {
		return r.firstErr
	}
	return r.ctx.Err()
}

// Close waits for every submitted frame to be analysed and delivered, then returns the
// run statistics and the first error, if any.
func (r *Run) Close() (Stats, error) {
	close(r.work)
	r.wg.Wait()
	close(r.order)
	r.cwg.Wait()
	r.cancel()
	r.mu.Lock()
	defer r.mu.Unlock()
	st := r.stats
	st.FailedFrames = int(r.failed.Load())
	return st, r.firstErr
}

func meta(f Frame) Meta {
	return Meta{ID: f.ID, Ticks: f.Ticks, Time: f.Time, Reason: f.Reason, Width: f.Image.Width, Height: f.Image.Height}
}

func (r *Run) worker() {
	defer r.wg.Done()
	for j := range r.work {
		if r.ctx.Err() == nil {
			r.process(j)
		} else {
			j.err = r.ctx.Err()
		}
		close(j.done)
	}
}

func (r *Run) process(j *job) {
	f := j.frame
	j.res.Meta = meta(f)
	a := r.a

	// Detection and OCR of one frame run side by side: they are different containers.
	var wg sync.WaitGroup
	var derr, oerr error
	if a.det != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sp := a.log.Stage("ml_detect")
			res, err := a.det.Detect(r.ctx, f.Image, a.cfg.Detect)
			sp.Bytes(int64(res.Stats.BytesSent), 0)
			if err != nil {
				sp.Note(fmt.Sprintf("frame=%d error=%v", f.ID, err))
				sp.End()
				derr = err
				return
			}
			sp.Model(1, 0)
			sp.ModelName(res.Model.String())
			sp.Note(fmt.Sprintf("frame=%d detections=%d attempts=%d server_ms=%.1f", f.ID, len(res.Detections), res.Stats.Attempts, res.Stats.InferenceMS))
			sp.End()
			j.detRes = res
		}()
	}
	if a.rdr != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sp := a.log.Stage("ml_ocr")
			if j.reuse {
				sp.Note(fmt.Sprintf("frame=%d reused previous OCR result (frame unchanged)", f.ID))
				sp.End()
				return
			}
			res, err := a.rdr.Read(r.ctx, f.Image, a.cfg.OCR)
			sp.Bytes(int64(res.Stats.BytesSent), 0)
			if err != nil {
				sp.Note(fmt.Sprintf("frame=%d error=%v", f.ID, err))
				sp.End()
				oerr = err
				return
			}
			sp.Model(1, 0)
			sp.ModelName(res.Model.String())
			sp.Note(fmt.Sprintf("frame=%d lines=%d attempts=%d server_ms=%.1f", f.ID, len(res.Lines), res.Stats.Attempts, res.Stats.InferenceMS))
			sp.End()
			j.ocrRes = res
		}()
	}
	wg.Wait()
	j.err, j.ocrErr = errors.Join(derr, oerr), oerr
	j.frame.Image = mlclient.Image{} // drop the pixels: only metadata is needed from here on
}

// collect delivers results in submission order.
func (r *Run) collect() {
	defer r.cwg.Done()
	var prevLines []ocr.Line
	var prevOK bool
	for j := range r.order {
		<-j.done
		if r.err() != nil {
			continue // drain
		}
		if j.err != nil {
			// Keep the OCR baseline when OCR itself worked: later unchanged frames copy it.
			if !j.reuse && r.a.rdr != nil {
				prevLines, prevOK = j.ocrRes.Lines, j.ocrErr == nil
			}
			n := int(r.failed.Add(1))
			if n > r.a.cfg.MaxFrameErrors {
				r.fail(fmt.Errorf("analyze: frame %d (t=%.2fs): %w", j.res.Meta.ID, j.res.Meta.Time, j.err))
				continue
			}
			r.mu.Lock()
			r.stats.Warnings = append(r.stats.Warnings, fmt.Sprintf("frame %d skipped: %v", j.res.Meta.ID, j.err))
			r.mu.Unlock()
			continue
		}
		res := j.res
		res.Detections = j.detRes.Detections
		if r.a.rdr != nil {
			switch {
			case j.reuse && prevOK:
				res.Lines, res.OCRReused = prevLines, true
			case j.reuse: // the baseline frame failed: no text known for this frame
			default:
				res.Lines = j.ocrRes.Lines
				prevLines, prevOK = j.ocrRes.Lines, true
			}
		}
		r.mu.Lock()
		s := &r.stats
		s.Frames++
		if r.a.det != nil {
			s.DetectCalls++
			s.InferenceMS += j.detRes.Stats.InferenceMS
			s.Detector = j.detRes.Model
		}
		if r.a.rdr != nil {
			if res.OCRReused {
				s.OCRReused++
			} else if !j.reuse {
				s.OCRCalls++
				s.InferenceMS += j.ocrRes.Stats.InferenceMS
				s.OCR = j.ocrRes.Model
			}
		}
		r.mu.Unlock()
		if err := r.handler(res); err != nil {
			r.fail(err)
		}
	}
}
