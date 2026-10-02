package scheduler

import (
	"context"
	"sync"

	"virex/codec/encoder"
	"virex/codec/sampler"
)

// Source starts producing frames. It must stop and close the channel when ctx is
// cancelled, and the returned func must wait for it and report its error.
// encoder.Frames fits this shape.
type Source func(ctx context.Context) (<-chan encoder.Frame, func() error)

// Sampled is a frame chosen for semantic analysis, with the reason.
type Sampled struct {
	Frame  encoder.Frame
	Reason string
}

// Stats summarise one run.
type Stats struct {
	Frames   int // frames read from the source
	Selected int // frames passed to the sink
	// MaxQueued is the most selected frames that were ever waiting for the sink.
	// It never exceeds Options.Buffer, which is the back-pressure guarantee.
	MaxQueued int
}

// Options tune the pipeline.
type Options struct {
	// Buffer is the size of the queue between the sampler and the sink (default 4).
	// Together with the source's own buffer it bounds the frames held in memory.
	Buffer int
}

// Run connects the three stages:
//
//	source (decode) -> sampler -> sink
//
// each in its own goroutine, joined by bounded channels. If the sink is slow, its queue
// fills, the sampler blocks, the source's channel fills, and ffmpeg stops being read:
// memory stays bounded however long the video is. The first error from any stage
// cancels the others. Run returns after every goroutine has finished.
func Run(ctx context.Context, src Source, strat sampler.Strategy, opt Options, sink func(Sampled) error) (Stats, error) {
	if opt.Buffer < 1 {
		opt.Buffer = 4
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	frames, waitSrc := src(ctx)
	queue := make(chan Sampled, opt.Buffer)

	var (
		stats  Stats
		once   sync.Once
		runErr error
		wg     sync.WaitGroup
	)
	fail := func(err error) {
		once.Do(func() { runErr = err; cancel() })
	}

	// Stage 2: sampler.
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(queue)
		for f := range frames {
			stats.Frames++
			ok, why := strat.Select(f)
			if !ok {
				continue
			}
			select {
			case queue <- Sampled{Frame: f, Reason: why}:
				if n := len(queue); n > stats.MaxQueued {
					stats.MaxQueued = n
				}
			case <-ctx.Done():
				// Keep draining so the source can finish; it stops on ctx.
				for range frames {
				}
				return
			}
		}
	}()

	// Stage 3: sink.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for s := range queue {
			if ctx.Err() != nil {
				continue
			}
			if err := sink(s); err != nil {
				fail(err)
				continue
			}
			stats.Selected++
		}
	}()

	wg.Wait()
	if err := waitSrc(); err != nil && runErr == nil && ctx.Err() == nil {
		runErr = err
	}
	if runErr == nil && ctx.Err() != nil {
		runErr = ctx.Err()
	}
	return stats, runErr
}
