// Package mlclient is the transport layer to the Python ML containers: gRPC
// connections, image encoding, per-attempt timeouts and retries with backoff.
//
// It knows nothing about models. The typed clients in semantic/objects and semantic/ocr
// sit on top of it, and semantic/analyze runs them over many frames in parallel.
package mlclient

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"math/rand"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	mlv1 "virex/proto/ml/v1"
)

// Image is one frame: Width*Height*3 bytes of RGB24, row-major.
type Image struct {
	Width, Height int
	RGB           []byte
}

// Validate checks the buffer matches the declared size.
func (im Image) Validate() error {
	if im.Width <= 0 || im.Height <= 0 {
		return fmt.Errorf("mlclient: bad image size %dx%d", im.Width, im.Height)
	}
	if want := im.Width * im.Height * 3; len(im.RGB) != want {
		return fmt.Errorf("mlclient: RGB image %dx%d needs %d bytes, has %d", im.Width, im.Height, want, len(im.RGB))
	}
	return nil
}

// Box is a rectangle in normalised coordinates: 0..1, origin top-left, x/y = top-left
// corner, w/h = size. Same convention as SVIR's BBox.
type Box struct{ X, Y, W, H float32 }

// ModelInfo identifies the model behind a result.
type ModelInfo struct {
	Name, Version, Backend, Device string
	Classes                        []string
}

// String is "name (version)", what goes into logs and the container metadata.
func (m ModelInfo) String() string {
	if m.Version == "" {
		return m.Name
	}
	return m.Name + " (" + m.Version + ")"
}

// Stats describes one call, for the cost log (C1).
type Stats struct {
	Attempts    int           // 1 = succeeded first time
	RTT         time.Duration // wall time of the successful attempt
	InferenceMS float64       // server-reported time inside the model
	ServerMS    float64       // server-reported total handling time
	BytesSent   int           // payload size of the image
}

// Format is how frames travel.
type Format string

const (
	RGB24 Format = "rgb24" // raw pixels: no codec cost, no loss (default)
	JPEG  Format = "jpeg"  // smaller on slow links, lossy
)

// Config describes the ML endpoints and how calls behave.
type Config struct {
	// DetectAddr and OCRAddr are host:port of the two services. Either may be empty
	// when that service is not used. They may be the same address (role "all").
	DetectAddr string `json:"detect_addr"`
	OCRAddr    string `json:"ocr_addr"`

	// TimeoutSeconds bounds one attempt (default 30; CPU OCR of a large frame can take seconds).
	TimeoutSeconds float64 `json:"timeout_seconds"`
	// Retries is how many extra attempts follow a transient failure (default 2;
	// a negative value means no retries).
	Retries int `json:"retries"`
	// BackoffMS is the first retry delay; it doubles each retry, with jitter (default 200).
	BackoffMS int `json:"backoff_ms"`
	// MaxMessageMB caps one gRPC message either way (default 64; a 4K RGB frame is 25 MB).
	MaxMessageMB int `json:"max_message_mb"`

	Format      Format `json:"format"`       // rgb24 (default) or jpeg
	JPEGQuality int    `json:"jpeg_quality"` // 1..100, default 92
}

func (c Config) withDefaults() Config {
	if c.TimeoutSeconds <= 0 {
		c.TimeoutSeconds = 30
	}
	if c.Retries < 0 {
		c.Retries = 0
	} else if c.Retries == 0 {
		c.Retries = 2
	}
	if c.BackoffMS <= 0 {
		c.BackoffMS = 200
	}
	if c.MaxMessageMB <= 0 {
		c.MaxMessageMB = 64
	}
	if c.Format == "" {
		c.Format = RGB24
	}
	if c.JPEGQuality <= 0 || c.JPEGQuality > 100 {
		c.JPEGQuality = 92
	}
	return c
}

// Validate rejects settings that can never work.
func (c Config) Validate() error {
	if c.Format != "" && c.Format != RGB24 && c.Format != JPEG {
		return fmt.Errorf("mlclient: format must be rgb24 or jpeg, got %q", c.Format)
	}
	if c.DetectAddr == "" && c.OCRAddr == "" {
		return errors.New("mlclient: no ML address configured (detect_addr / ocr_addr)")
	}
	return nil
}

// Conn is a connection to one ML service plus the call policy. It is safe for
// concurrent use.
type Conn struct {
	cc   *grpc.ClientConn
	addr string
	cfg  Config
}

// Dial prepares a connection. gRPC connects lazily, so this succeeds even when the
// container is down; use Conn.WaitReady for a clear early failure.
func Dial(addr string, cfg Config) (*Conn, error) {
	cfg = cfg.withDefaults()
	if addr == "" {
		return nil, errors.New("mlclient: empty address")
	}
	limit := cfg.MaxMessageMB << 20
	cc, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(limit), grpc.MaxCallSendMsgSize(limit)),
	)
	if err != nil {
		return nil, fmt.Errorf("mlclient: dial %s: %w", addr, err)
	}
	return &Conn{cc: cc, addr: addr, cfg: cfg}, nil
}

// FromClientConn wraps an existing connection (tests use this with in-memory servers).
func FromClientConn(cc *grpc.ClientConn, addr string, cfg Config) *Conn {
	return &Conn{cc: cc, addr: addr, cfg: cfg.withDefaults()}
}

// Addr is the address this connection was dialled with.
func (c *Conn) Addr() string { return c.addr }

// Config returns the effective settings (defaults filled in).
func (c *Conn) Config() Config { return c.cfg }

// Close releases the connection.
func (c *Conn) Close() error { return c.cc.Close() }

// ClientConn exposes the underlying connection so typed clients can build stubs.
func (c *Conn) ClientConn() *grpc.ClientConn { return c.cc }

// PBImage encodes im for the wire in the configured format.
func (c *Conn) PBImage(im Image) (*mlv1.Image, error) {
	if err := im.Validate(); err != nil {
		return nil, err
	}
	if c.cfg.Format == JPEG {
		rgba := image.NewRGBA(image.Rect(0, 0, im.Width, im.Height))
		for i, j := 0, 0; i < len(im.RGB); i, j = i+3, j+4 {
			rgba.Pix[j], rgba.Pix[j+1], rgba.Pix[j+2], rgba.Pix[j+3] = im.RGB[i], im.RGB[i+1], im.RGB[i+2], 255
		}
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, rgba, &jpeg.Options{Quality: c.cfg.JPEGQuality}); err != nil {
			return nil, fmt.Errorf("mlclient: jpeg encode: %w", err)
		}
		return &mlv1.Image{Format: mlv1.Image_JPEG, Width: uint32(im.Width), Height: uint32(im.Height), Data: buf.Bytes()}, nil
	}
	return &mlv1.Image{Format: mlv1.Image_RGB24, Width: uint32(im.Width), Height: uint32(im.Height), Data: im.RGB}, nil
}

// Retryable reports whether err is worth another attempt: the server was unreachable
// or briefly busy. Bad input and model failures are not retried, since they would
// fail the same way.
func Retryable(err error) bool {
	switch status.Code(err) {
	case codes.Unavailable, codes.DeadlineExceeded, codes.Aborted, codes.ResourceExhausted:
		return true
	}
	return false
}

// Do runs call with a per-attempt timeout, retrying transient failures with
// exponential backoff and jitter. The attempt count is returned for the cost log.
// A cancelled ctx stops immediately.
func (c *Conn) Do(ctx context.Context, call func(ctx context.Context) error) (attempts int, err error) {
	delay := time.Duration(c.cfg.BackoffMS) * time.Millisecond
	for attempt := 1; ; attempt++ {
		actx, cancel := context.WithTimeout(ctx, time.Duration(c.cfg.TimeoutSeconds*float64(time.Second)))
		err = call(actx)
		cancel()
		if err == nil {
			return attempt, nil
		}
		if ctx.Err() != nil {
			return attempt, ctx.Err()
		}
		if attempt > c.cfg.Retries || !Retryable(err) {
			return attempt, c.describe(err)
		}
		wait := delay + time.Duration(rand.Int63n(int64(delay)/2+1))
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return attempt, ctx.Err()
		}
		delay *= 2
	}
}

func (c *Conn) describe(err error) error {
	st, _ := status.FromError(err)
	switch st.Code() {
	case codes.Unavailable:
		return fmt.Errorf("ML service at %s is unreachable (is the container running? try `docker compose ps`): %w", c.addr, err)
	case codes.DeadlineExceeded:
		return fmt.Errorf("ML service at %s did not answer within %.0fs (raise ml.timeout_seconds, or use a smaller model / GPU): %w",
			c.addr, c.cfg.TimeoutSeconds, err)
	case codes.Unimplemented:
		return fmt.Errorf("ML service at %s does not host this service (wrong port, or wrong VIREX_ROLE?): %w", c.addr, err)
	}
	return fmt.Errorf("ML service at %s: %w", c.addr, err)
}
