package analyze_test

import (
	"context"
	"math/rand"
	"sync/atomic"
	"testing"
	"time"

	mlv1 "virex/proto/ml/v1"
	"virex/semantic/analyze"
	"virex/semantic/mlclient"
	"virex/semantic/mltest"
	"virex/semantic/objects"
	"virex/semantic/ocr"
)

const W, H = 320, 180

type rect struct {
	x1, y1, x2, y2 int
	r, g, b        byte
}

func frameWith(rects ...rect) mlclient.Image {
	buf := make([]byte, W*H*3)
	for i := range buf {
		buf[i] = 255
	}
	for _, rc := range rects {
		for y := rc.y1; y < rc.y2; y++ {
			for x := rc.x1; x < rc.x2; x++ {
				i := (y*W + x) * 3
				buf[i], buf[i+1], buf[i+2] = rc.r, rc.g, rc.b
			}
		}
	}
	return mlclient.Image{Width: W, Height: H, RGB: buf}
}

var (
	person = rect{32, 18, 96, 90, 230, 20, 20}
	text1  = rect{150, 20, 300, 50, 20, 230, 20}
	text2  = rect{150, 100, 300, 130, 20, 230, 20}
)

func newAnalyzer(t *testing.T, srv *mltest.Server, cfg analyze.Config, withOCR bool) *analyze.Analyzer {
	t.Helper()
	conn, err := mlclient.Dial(srv.Addr, mlclient.Config{BackoffMS: 5})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	var rdr ocr.Reader
	if withOCR {
		rdr = ocr.New(conn)
	}
	a, err := analyze.New(objects.New(conn), rdr, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestResultsArriveInFrameOrderDespiteJitter(t *testing.T) {
	srv := mltest.Start(t)
	srv.SetDetect(func(r *mlv1.DetectRequest) (*mlv1.DetectResponse, error) {
		time.Sleep(time.Duration(rand.Intn(20)) * time.Millisecond)
		return mltest.ColorDetect(r)
	})
	a := newAnalyzer(t, srv, analyze.Config{Workers: 6}, false)
	var got []int
	run := a.Start(context.Background(), func(r analyze.Result) error {
		got = append(got, r.Meta.ID)
		return nil
	})
	for i := 0; i < 60; i++ {
		if err := run.Submit(analyze.Frame{ID: i, Ticks: int64(i * 1000), Time: float64(i), Image: frameWith(person)}); err != nil {
			t.Fatal(err)
		}
	}
	st, err := run.Close()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 60 || st.Frames != 60 || st.DetectCalls != 60 {
		t.Fatalf("got %d results, stats %+v", len(got), st)
	}
	for i, id := range got {
		if id != i {
			t.Fatalf("out of order at %d: %v", i, got[:i+1])
		}
	}
}

func TestInFlightWorkIsBounded(t *testing.T) {
	srv := mltest.Start(t)
	var cur, peak atomic.Int32
	srv.SetDetect(func(r *mlv1.DetectRequest) (*mlv1.DetectResponse, error) {
		n := cur.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(15 * time.Millisecond)
		cur.Add(-1)
		return mltest.ColorDetect(r)
	})
	a := newAnalyzer(t, srv, analyze.Config{Workers: 3}, false)
	run := a.Start(context.Background(), func(analyze.Result) error { return nil })
	for i := 0; i < 30; i++ {
		if err := run.Submit(analyze.Frame{ID: i, Image: frameWith(person)}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := run.Close(); err != nil {
		t.Fatal(err)
	}
	if peak.Load() > 3 {
		t.Fatalf("%d concurrent calls with 3 workers", peak.Load())
	}
	if peak.Load() < 2 {
		t.Fatalf("no parallelism: peak %d", peak.Load())
	}
}

func TestStaticFramesReuseOCRButNewTextIsRead(t *testing.T) {
	srv := mltest.Start(t)
	a := newAnalyzer(t, srv, analyze.Config{Workers: 2}, true)
	var res []analyze.Result
	run := a.Start(context.Background(), func(r analyze.Result) error { res = append(res, r); return nil })
	frames := []mlclient.Image{
		frameWith(text1), frameWith(text1), frameWith(text1), // identical slide
		frameWith(text1, text2), // a new line of text appears: must be OCR'd again
		frameWith(text1, text2),
	}
	for i, im := range frames {
		if err := run.Submit(analyze.Frame{ID: i, Image: im}); err != nil {
			t.Fatal(err)
		}
	}
	st, err := run.Close()
	if err != nil {
		t.Fatal(err)
	}
	wantReuse := []bool{false, true, true, false, true}
	wantLines := []int{1, 1, 1, 2, 2}
	for i, r := range res {
		if r.OCRReused != wantReuse[i] || len(r.Lines) != wantLines[i] {
			t.Errorf("frame %d: reused=%v lines=%d, want reused=%v lines=%d", i, r.OCRReused, len(r.Lines), wantReuse[i], wantLines[i])
		}
	}
	if srv.ReadCalls.Load() != 2 || st.OCRCalls != 2 || st.OCRReused != 3 {
		t.Fatalf("read calls %d, stats %+v", srv.ReadCalls.Load(), st)
	}
}

func TestReuseCanBeDisabled(t *testing.T) {
	srv := mltest.Start(t)
	a := newAnalyzer(t, srv, analyze.Config{Workers: 1, StaticLevels: -1}, true)
	run := a.Start(context.Background(), func(analyze.Result) error { return nil })
	for i := 0; i < 4; i++ {
		_ = run.Submit(analyze.Frame{ID: i, Image: frameWith(text1)})
	}
	if _, err := run.Close(); err != nil {
		t.Fatal(err)
	}
	if srv.ReadCalls.Load() != 4 {
		t.Fatalf("want 4 OCR calls, got %d", srv.ReadCalls.Load())
	}
}

func TestFrameFailureIsFatalByDefault(t *testing.T) {
	srv := mltest.Start(t)
	var n atomic.Int32
	srv.SetDetect(func(r *mlv1.DetectRequest) (*mlv1.DetectResponse, error) {
		if n.Add(1) == 3 {
			return nil, mltest.BadInput()
		}
		return mltest.ColorDetect(r)
	})
	a := newAnalyzer(t, srv, analyze.Config{Workers: 1}, false)
	run := a.Start(context.Background(), func(analyze.Result) error { return nil })
	var subErr error
	for i := 0; i < 10 && subErr == nil; i++ {
		subErr = run.Submit(analyze.Frame{ID: i, Image: frameWith(person)})
	}
	_, err := run.Close()
	if err == nil {
		t.Fatalf("want an error (submit said %v)", subErr)
	}
}

func TestFrameFailureCanBeTolerated(t *testing.T) {
	srv := mltest.Start(t)
	var n atomic.Int32
	srv.SetDetect(func(r *mlv1.DetectRequest) (*mlv1.DetectResponse, error) {
		if n.Add(1) == 3 {
			return nil, mltest.BadInput()
		}
		return mltest.ColorDetect(r)
	})
	a := newAnalyzer(t, srv, analyze.Config{Workers: 1, MaxFrameErrors: 1}, false)
	var ids []int
	run := a.Start(context.Background(), func(r analyze.Result) error { ids = append(ids, r.Meta.ID); return nil })
	for i := 0; i < 6; i++ {
		if err := run.Submit(analyze.Frame{ID: i, Image: frameWith(person)}); err != nil {
			t.Fatal(err)
		}
	}
	st, err := run.Close()
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 5 || st.FailedFrames != 1 || len(st.Warnings) != 1 {
		t.Fatalf("ids %v stats %+v", ids, st)
	}
}

func TestHandlerErrorStopsTheRun(t *testing.T) {
	srv := mltest.Start(t)
	a := newAnalyzer(t, srv, analyze.Config{Workers: 2}, false)
	run := a.Start(context.Background(), func(r analyze.Result) error {
		if r.Meta.ID == 2 {
			return context.DeadlineExceeded
		}
		return nil
	})
	var subErr error
	for i := 0; i < 100 && subErr == nil; i++ {
		subErr = run.Submit(analyze.Frame{ID: i, Image: frameWith(person)})
	}
	if _, err := run.Close(); err == nil {
		t.Fatal("want handler error back from Close")
	}
}

func TestCancelledContextDoesNotHang(t *testing.T) {
	srv := mltest.Start(t)
	srv.SetDetect(func(r *mlv1.DetectRequest) (*mlv1.DetectResponse, error) {
		time.Sleep(50 * time.Millisecond)
		return mltest.ColorDetect(r)
	})
	a := newAnalyzer(t, srv, analyze.Config{Workers: 2}, false)
	ctx, cancel := context.WithCancel(context.Background())
	run := a.Start(ctx, func(analyze.Result) error { return nil })
	go func() { time.Sleep(60 * time.Millisecond); cancel() }()
	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			if run.Submit(analyze.Frame{ID: i, Image: frameWith(person)}) != nil {
				break
			}
		}
		run.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("run did not stop after cancel")
	}
}

func TestNeedsAtLeastOneService(t *testing.T) {
	if _, err := analyze.New(nil, nil, analyze.Config{}, nil); err == nil {
		t.Fatal("want error")
	}
}
