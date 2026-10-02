package cost

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os/exec"
	"testing"
	"time"
)

func TestJSONLines(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, "run1")
	s := l.Stage("ingest")
	time.Sleep(5 * time.Millisecond)
	s.Bytes(100, 40)
	s.Model(2, 350)
	s.Note("hello")
	r := s.End()
	l.Stage("pixel").End()

	if r.WallMS < 4 {
		t.Fatalf("wall_ms %v", r.WallMS)
	}
	sc := bufio.NewScanner(&buf)
	var got []Record
	for sc.Scan() {
		var x Record
		if err := json.Unmarshal(sc.Bytes(), &x); err != nil {
			t.Fatalf("not JSON: %v: %s", err, sc.Text())
		}
		got = append(got, x)
	}
	if len(got) != 2 || got[0].Stage != "ingest" || got[1].Stage != "pixel" {
		t.Fatalf("records: %+v", got)
	}
	g := got[0]
	if g.Run != "run1" || g.BytesIn != 100 || g.BytesOut != 40 || g.ModelCalls != 2 || g.Tokens != 350 || g.Note != "hello" {
		t.Fatalf("record: %+v", g)
	}
	if _, err := time.Parse(time.RFC3339Nano, g.Start); err != nil {
		t.Fatal(err)
	}
}

func TestNilLoggerIsSafe(t *testing.T) {
	var l *Logger
	s := l.Stage("x")
	s.Bytes(1, 2)
	s.AddCPU(nil)
	s.End()
}

func TestChildCPU(t *testing.T) {
	cmd := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "testsrc=size=320x240:rate=30", "-t", "1", "-f", "null", "-")
	if err := cmd.Run(); err != nil {
		t.Skip("ffmpeg unavailable")
	}
	var buf bytes.Buffer
	s := New(&buf, "r").Stage("enc")
	s.AddCPU(cmd.ProcessState)
	if r := s.End(); r.CPUMS <= 0 {
		t.Fatalf("expected child CPU time, got %v", r.CPUMS)
	}
}
