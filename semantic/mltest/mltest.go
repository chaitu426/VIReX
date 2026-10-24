// Package mltest is an in-process fake of the ML containers, for tests. It speaks the
// real gRPC contract on a real local port, so the production client code runs unchanged.
//
// Besides programmable handlers it ships a colour-rectangle "model" matching the Python
// service's fake backend: red -> person, blue -> cell phone, green -> text.
package mltest

import (
	"context"
	"net"
	"sync"
	"sync/atomic"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	mlv1 "virex/proto/ml/v1"
)

// Server hosts both services on one port.
type Server struct {
	Addr string

	mu sync.Mutex
	// DetectFn and ReadFn answer requests. Replace them before or during a test.
	detectFn func(*mlv1.DetectRequest) (*mlv1.DetectResponse, error)
	readFn   func(*mlv1.ReadRequest) (*mlv1.ReadResponse, error)

	DetectCalls atomic.Int64
	ReadCalls   atomic.Int64
	// Ready is what Info reports; set false to simulate a model that is still loading.
	notReady atomic.Bool

	mlv1.UnimplementedObjectDetectorServer
	mlv1.UnimplementedTextReaderServer
	gs *grpc.Server
}

// Start launches a server on 127.0.0.1:0 that detects coloured rectangles. It stops
// when the test ends.
func Start(tb testing.TB) *Server {
	tb.Helper()
	s := &Server{}
	s.SetDetect(ColorDetect)
	s.SetRead(ColorRead)
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		tb.Fatal(err)
	}
	s.Addr = lis.Addr().String()
	s.gs = grpc.NewServer(grpc.MaxRecvMsgSize(256 << 20))
	mlv1.RegisterObjectDetectorServer(s.gs, detectorSvc{s})
	mlv1.RegisterTextReaderServer(s.gs, readerSvc{s})
	go s.gs.Serve(lis)
	tb.Cleanup(s.gs.Stop)
	return s
}

// SetDetect replaces the detector handler.
func (s *Server) SetDetect(f func(*mlv1.DetectRequest) (*mlv1.DetectResponse, error)) {
	s.mu.Lock()
	s.detectFn = f
	s.mu.Unlock()
}

// SetRead replaces the OCR handler.
func (s *Server) SetRead(f func(*mlv1.ReadRequest) (*mlv1.ReadResponse, error)) {
	s.mu.Lock()
	s.readFn = f
	s.mu.Unlock()
}

// SetReady controls what Info reports.
func (s *Server) SetReady(ok bool) { s.notReady.Store(!ok) }

func (s *Server) Detect(_ context.Context, r *mlv1.DetectRequest) (*mlv1.DetectResponse, error) {
	s.DetectCalls.Add(1)
	s.mu.Lock()
	f := s.detectFn
	s.mu.Unlock()
	return f(r)
}

func (s *Server) Read(_ context.Context, r *mlv1.ReadRequest) (*mlv1.ReadResponse, error) {
	s.ReadCalls.Add(1)
	s.mu.Lock()
	f := s.readFn
	s.mu.Unlock()
	return f(r)
}

func info(ready bool, name string) *mlv1.InfoResponse {
	return &mlv1.InfoResponse{
		Model:          &mlv1.ModelInfo{Name: name, Version: "test", Backend: "mltest", Device: "cpu"},
		ServiceVersion: "test", Ready: ready,
	}
}

// Both services have an Info method, so the fake exposes each through a small adapter.
func (s *Server) detectorInfo() *mlv1.InfoResponse { return info(!s.notReady.Load(), "fake-detector") }
func (s *Server) readerInfo() *mlv1.InfoResponse   { return info(!s.notReady.Load(), "fake-ocr") }

type detectorSvc struct{ *Server }

func (d detectorSvc) Info(context.Context, *mlv1.InfoRequest) (*mlv1.InfoResponse, error) {
	return d.detectorInfo(), nil
}

type readerSvc struct{ *Server }

func (d readerSvc) Info(context.Context, *mlv1.InfoRequest) (*mlv1.InfoResponse, error) {
	return d.readerInfo(), nil
}

// StartOnly registers just one service, to test "wrong container" errors.
func StartOnly(tb testing.TB, detector bool) *Server {
	tb.Helper()
	s := &Server{}
	s.SetDetect(ColorDetect)
	s.SetRead(ColorRead)
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		tb.Fatal(err)
	}
	s.Addr = lis.Addr().String()
	s.gs = grpc.NewServer()
	if detector {
		mlv1.RegisterObjectDetectorServer(s.gs, detectorSvc{s})
	} else {
		mlv1.RegisterTextReaderServer(s.gs, readerSvc{s})
	}
	go s.gs.Serve(lis)
	tb.Cleanup(s.gs.Stop)
	return s
}

// Unavailable is a handler result that makes the client retry.
func Unavailable() error {
	return status.Error(codes.Unavailable, "mltest: pretend the container is down")
}

// BadInput is a handler result the client must not retry.
func BadInput() error { return status.Error(codes.InvalidArgument, "mltest: bad input") }
