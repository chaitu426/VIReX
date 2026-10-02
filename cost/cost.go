package cost

import (
	"encoding/json"
	"io"
	"os"
	"sync"
	"time"
)

// Record is one line of the cost log: what one stage of one run cost.
// CPUMS counts CPU time of child processes (ffmpeg, ffprobe) that the stage waited
// for. CPU time spent inside the Go process itself is not included.
type Record struct {
	Run        string  `json:"run"`
	Stage      string  `json:"stage"`
	Start      string  `json:"start"` // RFC 3339
	WallMS     float64 `json:"wall_ms"`
	CPUMS      float64 `json:"cpu_ms"`
	BytesIn    int64   `json:"bytes_in"`
	BytesOut   int64   `json:"bytes_out"`
	ModelCalls int     `json:"model_calls"` // filled from Phase 2
	Tokens     int64   `json:"tokens"`      // filled from Phase 2
	Note       string  `json:"note,omitempty"`
}

// Logger writes Records as JSON lines. A nil *Logger is valid and does nothing, so
// callers never need to check.
type Logger struct {
	mu  sync.Mutex
	w   io.Writer
	run string
}

// New logs to w. run identifies one encode run in the file.
func New(w io.Writer, run string) *Logger { return &Logger{w: w, run: run} }

// Open appends to a file, creating it if needed.
func Open(path, run string) (*Logger, io.Closer, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, nil, err
	}
	return New(f, run), f, nil
}

// Span times one stage.
type Span struct {
	l     *Logger
	rec   Record
	start time.Time
}

// Stage starts timing a stage.
func (l *Logger) Stage(name string) *Span {
	now := time.Now()
	return &Span{l: l, start: now, rec: Record{Run: runOf(l), Stage: name, Start: now.UTC().Format(time.RFC3339Nano)}}
}

func runOf(l *Logger) string {
	if l == nil {
		return ""
	}
	return l.run
}

// AddCPU adds the CPU time of a finished child process.
func (s *Span) AddCPU(ps interface {
	UserTime() time.Duration
	SystemTime() time.Duration
}) {
	if ps == nil {
		return
	}
	s.rec.CPUMS += float64(ps.UserTime()+ps.SystemTime()) / float64(time.Millisecond)
}

// Bytes sets the data size going in and out of the stage.
func (s *Span) Bytes(in, out int64) { s.rec.BytesIn, s.rec.BytesOut = in, out }

// Model records model calls and tokens (Phase 2 onwards).
func (s *Span) Model(calls int, tokens int64) { s.rec.ModelCalls, s.rec.Tokens = calls, tokens }

// Note attaches a short remark.
func (s *Span) Note(n string) { s.rec.Note = n }

// End stops the clock, writes the record and returns it.
func (s *Span) End() Record {
	s.rec.WallMS = float64(time.Since(s.start)) / float64(time.Millisecond)
	if s.l != nil && s.l.w != nil {
		s.l.mu.Lock()
		defer s.l.mu.Unlock()
		b, _ := json.Marshal(s.rec)
		_, _ = s.l.w.Write(append(b, '\n'))
	}
	return s.rec
}
