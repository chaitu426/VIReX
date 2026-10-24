// Package objects is the Go client for object detection in the ML service.
package objects

import (
	"context"
	"fmt"
	"time"

	"virex/semantic/mlclient"

	mlv1 "virex/proto/ml/v1"
)

// Detection is one object found in one frame.
type Detection struct {
	Class      string
	ClassID    int
	Confidence float32
	Box        mlclient.Box
}

// Options tune one call. Zero values mean "server default".
type Options struct {
	MinConfidence float32
	MaxDetections int
	Classes       []string // empty = all classes
	InferenceSize int
}

// Result is the answer for one frame.
type Result struct {
	Detections []Detection
	Model      mlclient.ModelInfo
	Stats      mlclient.Stats
}

// Detector finds objects in a frame. The gRPC client below is the real one; tests and
// offline runs can substitute their own.
type Detector interface {
	Detect(ctx context.Context, img mlclient.Image, opt Options) (Result, error)
	// Info reports the model and whether the service has finished loading it.
	Info(ctx context.Context) (mlclient.ModelInfo, bool, error)
}

// GRPC calls the ObjectDetector service.
type GRPC struct {
	conn *mlclient.Conn
	stub mlv1.ObjectDetectorClient
}

// New wraps a connection to an ObjectDetector service.
func New(conn *mlclient.Conn) *GRPC {
	return &GRPC{conn: conn, stub: mlv1.NewObjectDetectorClient(conn.ClientConn())}
}

var _ Detector = (*GRPC)(nil)

// Detect sends one frame.
func (g *GRPC) Detect(ctx context.Context, img mlclient.Image, opt Options) (Result, error) {
	pb, err := g.conn.PBImage(img)
	if err != nil {
		return Result{}, err
	}
	req := &mlv1.DetectRequest{
		Image:         pb,
		MinConfidence: opt.MinConfidence,
		MaxDetections: uint32(max(opt.MaxDetections, 0)),
		Classes:       opt.Classes,
		InferenceSize: uint32(max(opt.InferenceSize, 0)),
	}
	var resp *mlv1.DetectResponse
	start := time.Now()
	attempts, err := g.conn.Do(ctx, func(c context.Context) error {
		start = time.Now()
		var e error
		resp, e = g.stub.Detect(c, req)
		return e
	})
	if err != nil {
		return Result{}, fmt.Errorf("detect: %w", err)
	}
	res := Result{
		Model: modelInfo(resp.GetModel()),
		Stats: mlclient.Stats{
			Attempts: attempts, RTT: time.Since(start), InferenceMS: float64(resp.GetInferenceMs()),
			ServerMS: float64(resp.GetTotalMs()), BytesSent: len(pb.Data),
		},
	}
	for _, d := range resp.GetDetections() {
		b := d.GetBox()
		res.Detections = append(res.Detections, Detection{
			Class: d.GetClassName(), ClassID: int(d.GetClassId()), Confidence: d.GetConfidence(),
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
		return mlclient.ModelInfo{}, false, fmt.Errorf("detector info: %w", err)
	}
	return modelInfo(resp.GetModel()), resp.GetReady(), nil
}

func modelInfo(m *mlv1.ModelInfo) mlclient.ModelInfo {
	return mlclient.ModelInfo{Name: m.GetName(), Version: m.GetVersion(), Backend: m.GetBackend(), Device: m.GetDevice(), Classes: m.GetClasses()}
}
