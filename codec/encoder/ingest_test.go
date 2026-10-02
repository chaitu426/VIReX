package encoder

import (
	"context"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"testing"
	"time"
)

// makeClip builds a 3 s test clip with ffmpeg. vfr drops frames so timing is irregular.
func makeClip(t *testing.T, vfr bool) string {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not on PATH")
	}
	path := filepath.Join(t.TempDir(), "clip.mp4")
	args := []string{"-v", "error", "-y", "-f", "lavfi", "-i", "testsrc=size=160x120:rate=30", "-t", "3"}
	if vfr {
		args = append(args, "-vf", "select='not(eq(mod(n,5),3))'", "-fps_mode", "vfr")
	}
	args = append(args, "-c:v", "libx264", "-bf", "2", "-g", "25", path)
	if out, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg: %v: %s", err, out)
	}
	return path
}

// frameTimesFromFFprobe lists decoded-frame PTS using a different ffprobe query
// (frame=pts) than the code under test (packet=pts), as an independent check.
func frameTimesFromFFprobe(t *testing.T, path string) []int64 {
	t.Helper()
	out, err := exec.Command("ffprobe", "-v", "error", "-select_streams", "v:0",
		"-show_entries", "frame=pts", "-of", "compact=p=0", path).Output()
	if err != nil {
		t.Fatal(err)
	}
	var v []int64
	for _, r := range parseCompact(string(out)) {
		n, err := strconv.ParseInt(r["pts"], 10, 64)
		if err != nil {
			t.Fatalf("bad pts %q", r["pts"])
		}
		v = append(v, n)
	}
	sort.Slice(v, func(i, j int) bool { return v[i] < v[j] })
	return v
}

func TestIngest(t *testing.T) {
	for _, vfr := range []bool{false, true} {
		name := map[bool]string{false: "cfr", true: "vfr"}[vfr]
		t.Run(name, func(t *testing.T) {
			path := makeClip(t, vfr)
			ctx := context.Background()

			vi, err := Probe(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			if vi.Width != 160 || vi.Height != 120 || vi.Codec != "h264" || vi.TimeBaseDen == 0 {
				t.Fatalf("probe: %+v", vi)
			}
			pkts, err := Packets(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			if vi.NbFrames != 0 && vi.NbFrames != len(pkts) {
				t.Fatalf("nb_frames %d != packets %d", vi.NbFrames, len(pkts))
			}
			if !pkts[0].Key || len(KeyFrames(pkts)) < 2 {
				t.Fatalf("expected key frames, got %d", len(KeyFrames(pkts)))
			}
			if pkts[0].DTS >= 0 {
				t.Logf("note: first DTS %d (B-frame delay not negative)", pkts[0].DTS)
			}

			want := frameTimesFromFFprobe(t, path)
			ch, wait := Frames(ctx, path, vi, pkts, 2)
			var got []Frame
			for f := range ch {
				if len(f.Image) != vi.Width*vi.Height*3 {
					t.Fatalf("frame %d has %d bytes", f.ID, len(f.Image))
				}
				got = append(got, f)
			}
			if err := wait(); err != nil {
				t.Fatal(err)
			}

			if len(got) != len(want) {
				t.Fatalf("frame count %d, ffprobe says %d", len(got), len(want))
			}
			for i, f := range got {
				if f.ID != i || f.PTS != want[i] {
					t.Fatalf("frame %d: id=%d pts=%d, ffprobe pts=%d", i, f.ID, f.PTS, want[i])
				}
				if i > 0 && f.PTS <= got[i-1].PTS {
					t.Fatalf("timestamps not increasing at %d", i)
				}
			}
			last := got[len(got)-1]
			wantLast := float64(want[len(want)-1]) * float64(vi.TimeBaseNum) / float64(vi.TimeBaseDen)
			if last.Time != wantLast {
				t.Fatalf("last time %v, want %v", last.Time, wantLast)
			}

			// Variable frame rate must show uneven gaps; constant must not.
			uneven := false
			for i := 2; i < len(got); i++ {
				if got[i].PTS-got[i-1].PTS != got[1].PTS-got[0].PTS {
					uneven = true
				}
			}
			if uneven != vfr {
				t.Fatalf("uneven frame gaps = %v, want %v", uneven, vfr)
			}
		})
	}
}

func TestFramesCancel(t *testing.T) {
	path := makeClip(t, false)
	ctx, cancel := context.WithCancel(context.Background())
	vi, err := Probe(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	pkts, _ := Packets(ctx, path)
	ch, wait := Frames(ctx, path, vi, pkts, 1)
	<-ch // take one frame, then stop reading
	cancel()
	done := make(chan error, 1)
	go func() {
		for range ch {
		}
		done <- wait()
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected an error after cancel")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Frames did not stop after cancel")
	}
}

func TestProbeMissingFile(t *testing.T) {
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not on PATH")
	}
	if _, err := Probe(context.Background(), filepath.Join(t.TempDir(), "nope.mp4")); err == nil {
		t.Fatal("expected error")
	}
}
