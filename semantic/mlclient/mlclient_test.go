package mlclient_test

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	mlv1 "virex/proto/ml/v1"
	"virex/semantic/mlclient"
	"virex/semantic/mltest"
	"virex/semantic/objects"
	"virex/semantic/ocr"
)

func rgb(w, h int, fill func(x, y int) (r, g, b byte)) mlclient.Image {
	buf := make([]byte, w*h*3)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, b := fill(x, y)
			i := (y*w + x) * 3
			buf[i], buf[i+1], buf[i+2] = r, g, b
		}
	}
	return mlclient.Image{Width: w, Height: h, RGB: buf}
}

// scene has a red box (the fake detector calls it a person) and a green box (fake text).
func scene() mlclient.Image {
	return rgb(320, 180, func(x, y int) (byte, byte, byte) {
		switch {
		case x >= 32 && x < 96 && y >= 18 && y < 90:
			return 230, 20, 20
		case x >= 150 && x < 300 && y >= 20 && y < 50:
			return 20, 230, 20
		}
		return 255, 255, 255
	})
}

func dial(t *testing.T, addr string, cfg mlclient.Config) *mlclient.Conn {
	t.Helper()
	c, err := mlclient.Dial(addr, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func TestDetectAndReadOverRealGRPC(t *testing.T) {
	srv := mltest.Start(t)
	conn := dial(t, srv.Addr, mlclient.Config{})
	det, rdr := objects.New(conn), ocr.New(conn)

	res, err := det.Detect(context.Background(), scene(), objects.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Detections) != 1 || res.Detections[0].Class != "person" {
		t.Fatalf("detections: %+v", res.Detections)
	}
	b := res.Detections[0].Box
	if b.X < 0.07 || b.X > 0.13 || b.W < 0.17 || b.W > 0.23 {
		t.Fatalf("box %+v not near x=0.1 w=0.2", b)
	}
	if res.Model.Name != "fake-colour-detector" || res.Stats.Attempts != 1 || res.Stats.BytesSent != 320*180*3 {
		t.Fatalf("model/stats: %+v %+v", res.Model, res.Stats)
	}

	lines, err := rdr.Read(context.Background(), scene(), ocr.Options{})
	if err != nil || len(lines.Lines) != 1 || lines.Lines[0].Text != "FAKE TEXT" {
		t.Fatalf("ocr: %+v %v", lines, err)
	}

	m, ready, err := det.Info(context.Background())
	if err != nil || !ready || m.Name != "fake-detector" {
		t.Fatalf("info: %+v %v %v", m, ready, err)
	}
}

func TestJPEGFormatSendsSmallerPayload(t *testing.T) {
	srv := mltest.Start(t)
	var gotFormat atomic.Int32
	var gotLen atomic.Int64
	srv.SetDetect(func(r *mlv1.DetectRequest) (*mlv1.DetectResponse, error) {
		gotFormat.Store(int32(r.Image.Format))
		gotLen.Store(int64(len(r.Image.Data)))
		return &mlv1.DetectResponse{}, nil
	})
	conn := dial(t, srv.Addr, mlclient.Config{Format: mlclient.JPEG})
	if _, err := objects.New(conn).Detect(context.Background(), scene(), objects.Options{}); err != nil {
		t.Fatal(err)
	}
	if mlv1.Image_Format(gotFormat.Load()) != mlv1.Image_JPEG || gotLen.Load() >= 320*180*3/4 {
		t.Fatalf("format %v len %d", gotFormat.Load(), gotLen.Load())
	}
}

func TestRetriesTransientFailureThenSucceeds(t *testing.T) {
	srv := mltest.Start(t)
	var n atomic.Int32
	srv.SetDetect(func(r *mlv1.DetectRequest) (*mlv1.DetectResponse, error) {
		if n.Add(1) < 3 {
			return nil, mltest.Unavailable()
		}
		return &mlv1.DetectResponse{}, nil
	})
	conn := dial(t, srv.Addr, mlclient.Config{Retries: 2, BackoffMS: 5})
	res, err := objects.New(conn).Detect(context.Background(), scene(), objects.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Stats.Attempts != 3 || srv.DetectCalls.Load() != 3 {
		t.Fatalf("attempts %d calls %d", res.Stats.Attempts, srv.DetectCalls.Load())
	}
}

func TestGivesUpAfterRetries(t *testing.T) {
	srv := mltest.Start(t)
	srv.SetDetect(func(*mlv1.DetectRequest) (*mlv1.DetectResponse, error) { return nil, mltest.Unavailable() })
	conn := dial(t, srv.Addr, mlclient.Config{Retries: 1, BackoffMS: 5})
	_, err := objects.New(conn).Detect(context.Background(), scene(), objects.Options{})
	if err == nil || srv.DetectCalls.Load() != 2 {
		t.Fatalf("err %v calls %d", err, srv.DetectCalls.Load())
	}
}

func TestBadInputIsNotRetried(t *testing.T) {
	srv := mltest.Start(t)
	srv.SetDetect(func(*mlv1.DetectRequest) (*mlv1.DetectResponse, error) { return nil, mltest.BadInput() })
	conn := dial(t, srv.Addr, mlclient.Config{Retries: 3, BackoffMS: 5})
	if _, err := objects.New(conn).Detect(context.Background(), scene(), objects.Options{}); err == nil {
		t.Fatal("want error")
	}
	if srv.DetectCalls.Load() != 1 {
		t.Fatalf("retried a bad request: %d calls", srv.DetectCalls.Load())
	}
}

func TestPerAttemptTimeout(t *testing.T) {
	srv := mltest.Start(t)
	srv.SetDetect(func(*mlv1.DetectRequest) (*mlv1.DetectResponse, error) {
		time.Sleep(300 * time.Millisecond)
		return &mlv1.DetectResponse{}, nil
	})
	conn := dial(t, srv.Addr, mlclient.Config{TimeoutSeconds: 0.05, Retries: -1})
	start := time.Now()
	_, err := objects.New(conn).Detect(context.Background(), scene(), objects.Options{})
	if err == nil || time.Since(start) > 250*time.Millisecond {
		t.Fatalf("want a quick timeout, got %v after %v", err, time.Since(start))
	}
}

func TestContextCancelStopsRetrying(t *testing.T) {
	srv := mltest.Start(t)
	srv.SetDetect(func(*mlv1.DetectRequest) (*mlv1.DetectResponse, error) { return nil, mltest.Unavailable() })
	conn := dial(t, srv.Addr, mlclient.Config{Retries: 50, BackoffMS: 200})
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := objects.New(conn).Detect(ctx, scene(), objects.Options{})
	if err == nil || time.Since(start) > time.Second {
		t.Fatalf("did not stop: %v after %v", err, time.Since(start))
	}
}

func TestUnreachableServerGivesHelpfulError(t *testing.T) {
	conn := dial(t, "127.0.0.1:1", mlclient.Config{Retries: -1, TimeoutSeconds: 1})
	_, _, err := objects.New(conn).Info(context.Background())
	if err == nil {
		t.Fatal("want error")
	}
	if s := err.Error(); !strings.Contains(s, "unreachable") || !strings.Contains(s, "docker compose") {
		t.Fatalf("unhelpful: %v", err)
	}
}

func TestWrongServiceIsExplained(t *testing.T) {
	srv := mltest.StartOnly(t, true) // detector only
	conn := dial(t, srv.Addr, mlclient.Config{Retries: -1})
	_, _, err := ocr.New(conn).Info(context.Background())
	if err == nil || !strings.Contains(err.Error(), "VIREX_ROLE") {
		t.Fatalf("want role hint, got %v", err)
	}
}

func TestImageValidation(t *testing.T) {
	conn := dial(t, "127.0.0.1:1", mlclient.Config{})
	if _, err := objects.New(conn).Detect(context.Background(), mlclient.Image{Width: 10, Height: 10, RGB: make([]byte, 5)}, objects.Options{}); err == nil {
		t.Fatal("want size error")
	}
}

func TestConfigValidate(t *testing.T) {
	if (mlclient.Config{}).Validate() == nil {
		t.Fatal("no addresses must fail")
	}
	if (mlclient.Config{DetectAddr: "x:1", Format: "bmp"}).Validate() == nil {
		t.Fatal("bad format must fail")
	}
	if err := (mlclient.Config{DetectAddr: "x:1"}).Validate(); err != nil {
		t.Fatal(err)
	}
}
