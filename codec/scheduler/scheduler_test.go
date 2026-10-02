package scheduler

import (
	"context"
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"virex/codec/encoder"
	"virex/codec/sampler"
)

// fakeSource sends n tiny frames through a channel of capacity srcBuf and counts how
// many it managed to send. It stops when ctx is cancelled, like encoder.Frames.
func fakeSource(n, srcBuf int, sent *atomic.Int64) Source {
	return func(ctx context.Context) (<-chan encoder.Frame, func() error) {
		ch := make(chan encoder.Frame, srcBuf)
		done := make(chan struct{})
		go func() {
			defer close(done)
			defer close(ch)
			for i := 0; i < n; i++ {
				select {
				case ch <- encoder.Frame{ID: i, Time: float64(i) / 30}:
					sent.Add(1)
				case <-ctx.Done():
					return
				}
			}
		}()
		return ch, func() error { <-done; return nil }
	}
}

func everyFrame(t *testing.T) sampler.Strategy {
	s, err := sampler.New(sampler.Config{Mode: sampler.EveryN, N: 1}, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestRunDeliversInOrder(t *testing.T) {
	var sent atomic.Int64
	var got []int
	st, err := Run(context.Background(), fakeSource(100, 2, &sent), everyFrame(t), Options{Buffer: 3},
		func(s Sampled) error { got = append(got, s.Frame.ID); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if st.Frames != 100 || st.Selected != 100 || len(got) != 100 {
		t.Fatalf("stats %+v, got %d", st, len(got))
	}
	for i, id := range got {
		if id != i {
			t.Fatalf("out of order at %d: %d", i, id)
		}
	}
}

// A blocked sink must stop the source after a bounded number of frames, not let it
// run on and fill memory.
func TestBackPressure(t *testing.T) {
	const srcBuf, qBuf = 2, 3
	var sent atomic.Int64
	gate := make(chan struct{})
	entered := make(chan struct{}, 1)
	errc := make(chan error, 1)
	go func() {
		_, err := Run(context.Background(), fakeSource(100000, srcBuf, &sent), everyFrame(t), Options{Buffer: qBuf},
			func(Sampled) error {
				select {
				case entered <- struct{}{}:
				default:
				}
				<-gate
				return nil
			})
		errc <- err
	}()
	<-entered
	time.Sleep(200 * time.Millisecond) // give the source every chance to run ahead
	// In flight at most: 1 in the sink + qBuf queued + 1 held by the sampler + srcBuf in the source channel + 1 being sent.
	if n, max := sent.Load(), int64(1+qBuf+1+srcBuf+1); n > max {
		t.Fatalf("source ran ahead: sent %d frames while the sink was blocked (max %d)", n, max)
	}
	close(gate)
	if err := <-errc; err != nil {
		t.Fatal(err)
	}
	if sent.Load() != 100000 {
		t.Fatalf("sent %d", sent.Load())
	}
}

func TestSinkErrorStopsEverything(t *testing.T) {
	var sent atomic.Int64
	boom := errors.New("boom")
	n := 0
	done := make(chan struct{})
	var err error
	go func() {
		defer close(done)
		_, err = Run(context.Background(), fakeSource(1_000_000, 2, &sent), everyFrame(t), Options{},
			func(Sampled) error {
				n++
				if n == 5 {
					return boom
				}
				return nil
			})
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not stop after a sink error")
	}
	if !errors.Is(err, boom) {
		t.Fatalf("got %v", err)
	}
	if sent.Load() > 1000 {
		t.Fatalf("source kept going: %d frames", sent.Load())
	}
}

func TestContextCancel(t *testing.T) {
	var sent atomic.Int64
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := Run(ctx, fakeSource(1_000_000, 2, &sent), everyFrame(t), Options{},
			func(Sampled) error { time.Sleep(time.Millisecond); return nil })
		done <- err
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("got %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not stop after cancel")
	}
}

// End to end on a real clip: ffmpeg -> scheduler -> scene sampler.
func TestRealClip(t *testing.T) {
	path := "../../tests/testdata/clips/c04_cuts_360p.mp4"
	if _, err := os.Stat(path); err != nil {
		t.Skip("clip missing; run scripts/make-testclips.ps1")
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
	strat, _ := sampler.New(sampler.Config{Mode: sampler.SceneChange}, vi.Width, vi.Height)
	var got []int
	var last float64
	st, err := Run(ctx, func(c context.Context) (<-chan encoder.Frame, func() error) {
		return encoder.Frames(c, path, vi, pk, 4)
	}, strat, Options{Buffer: 2}, func(s Sampled) error {
		got = append(got, s.Frame.ID)
		last = s.Frame.Time
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if st.Frames != 300 || len(got) != 4 || got[1] != 75 || got[2] != 150 || got[3] != 225 {
		t.Fatalf("stats %+v selected %v", st, got)
	}
	if st.MaxQueued > 2 {
		t.Fatalf("queue exceeded its bound: %d", st.MaxQueued)
	}
	if last < 7.4 || last > 7.6 {
		t.Fatalf("last sampled time %v, want about 7.5 s", last)
	}
}
