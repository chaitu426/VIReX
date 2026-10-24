// Package ocr is the Go client for text recognition in the ML service.
package ocr

import (
	"context"
	"fmt"
	"time"

	"virex/semantic/mlclient"

	mlv1 "virex/proto/ml/v1"
)

// Line is one piece of text read from one frame, with its box.
type Line struct {
	Text       string
	Confidence float32
	Box        mlclient.Box
}

// Options tune one call. Zero values mean "server default".
type Options struct {
	MinConfidence float32
	Languages     []string
}

// Result is the answer for one frame.
type Result struct {
	Lines []Line
	Model mlclient.ModelInfo
	Stats mlclient.Stats
}

// Reader reads text from a frame.
type Reader interface {
	Read(ctx context.Context, img mlclient.Image, opt Options) (Result, error)
	// Info reports the model and whether the service has finished loading it.
	Info(ctx context.Context) (mlclient.ModelInfo, bool, error)
}

// GRPC calls the TextReader service.
type GRPC struct {
	conn *mlclient.Conn
	stub mlv1.TextReaderClient
}

// New wraps a connection to a TextReader service.
func New(conn *mlclient.Conn) *GRPC {
	return &GRPC{conn: conn, stub: mlv1.NewTextReaderClient(conn.ClientConn())}
}

var _ Reader = (*GRPC)(nil)

// Read sends one frame.
func (g *GRPC) Read(ctx context.Context, img mlclient.Image, opt Options) (Result, error) {
	pb, err := g.conn.PBImage(img)
	if err != nil {
		return Result{}, err
	}
	req := &mlv1.ReadRequest{Image: pb, MinConfidence: opt.MinConfidence, Languages: opt.Languages}
	var resp *mlv1.ReadResponse
	start := time.Now()
	attempts, err := g.conn.Do(ctx, func(c context.Context) error {
		start = time.Now()
		var e error
		resp, e = g.stub.Read(c, req)
		return e
	})
	if err != nil {
		return Result{}, fmt.Errorf("ocr: %w", err)
	}
	res := Result{
		Model: modelInfo(resp.GetModel()),
		Stats: mlclient.Stats{
			Attempts: attempts, RTT: time.Since(start), InferenceMS: float64(resp.GetInferenceMs()),
			ServerMS: float64(resp.GetTotalMs()), BytesSent: len(pb.Data),
		},
	}
	for _, l := range resp.GetLines() {
		b := l.GetBox()
		res.Lines = append(res.Lines, Line{
			Text: l.GetText(), Confidence: l.GetConfidence(),
			Box: mlclient.Box{X: b.GetX(), Y: b.GetY(), W: b.GetW(), H: b.GetH()},
		})
	}
	return res, nil
}

// Info asks the service which model it runs and whether it is ready.
func (g *GRPC) Info(ctx context.Context) (mlclient.ModelInfo, bool, error) {
	var resp *mlv1.InfoResponse
	_, err := g.conn.Do(ctx, func(c context.Context) error {
		var e error
		resp, e = g.stub.Info(c, &mlv1.InfoRequest{})
		return e
	})
	if err != nil {
		return mlclient.ModelInfo{}, false, fmt.Errorf("ocr info: %w", err)
	}
	return modelInfo(resp.GetModel()), resp.GetReady(), nil
}

func modelInfo(m *mlv1.ModelInfo) mlclient.ModelInfo {
	return mlclient.ModelInfo{Name: m.GetName(), Version: m.GetVersion(), Backend: m.GetBackend(), Device: m.GetDevice(), Classes: m.GetClasses()}
}
