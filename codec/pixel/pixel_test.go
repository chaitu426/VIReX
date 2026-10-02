package pixel

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"virex/codec/container"
	"virex/codec/encoder"
)

type clip struct {
	name string
	ext  string
	args []string // extra ffmpeg args between input and output
}

var clips = []clip{
	{"cfr", "mp4", nil},
	{"vfr", "mp4", []string{"-vf", "select='not(eq(mod(n,5),3))'", "-fps_mode", "vfr"}},
	{"offset-start", "mp4", []string{"-vf", "setpts=PTS+3/TB", "-t", "7"}},                                            // first frame at 3 s
	{"mkv-ms-timebase", "mkv", []string{"-vf", "select='not(eq(mod(n,7),3))'", "-fps_mode", "vfr", "-f", "matroska"}}, // time base 1/1000
}

func makeClip(t *testing.T, c clip) string {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not on PATH")
	}
	path := filepath.Join(t.TempDir(), "src."+c.ext)
	args := []string{"-v", "error", "-y", "-f", "lavfi", "-i", "testsrc2=size=160x120:rate=30", "-t", "4"}
	args = append(args, c.args...)
	args = append(args, "-c:v", "libx264", "-crf", "14", "-bf", "2", "-g", "40", "-pix_fmt", "yuv420p", path)
	if out, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg: %v: %s", err, out)
	}
	return path
}

func sortedPTS(p []encoder.Packet) []int64 {
	v := make([]int64, len(p))
	for i, x := range p {
		v[i] = x.PTS
	}
	sort.Slice(v, func(i, j int) bool { return v[i] < v[j] })
	return v
}

func recPTS(r []container.FrameRecord) []int64 {
	v := make([]int64, len(r))
	for i, x := range r {
		v[i] = x.PTS
	}
	sort.Slice(v, func(i, j int) bool { return v[i] < v[j] })
	return v
}

var md5re = regexp.MustCompile(`(?m)^\d+,\s*[-\d]+,\s*[-\d]+,\s*[-\d]+,\s*\d+,\s*([0-9a-f]{32})\s*$`)

// frameHashes decodes a file and returns the MD5 of every decoded frame in display order.
func frameHashes(t *testing.T, args ...string) []string {
	t.Helper()
	a := append([]string{"-v", "error", "-nostdin"}, args...)
	a = append(a, "-map", "0:v:0", "-f", "framemd5", "-")
	out, err := exec.Command("ffmpeg", a...).Output()
	if err != nil {
		t.Fatalf("framemd5: %v", err)
	}
	var h []string
	for _, m := range md5re.FindAllStringSubmatch(string(out), -1) {
		h = append(h, m[1])
	}
	if len(h) == 0 {
		t.Fatalf("no frame hashes in %q", out)
	}
	return h
}

func TestEncodeExactTimestampsAndStream(t *testing.T) {
	for _, c := range clips {
		for _, mode := range []Mode{ModeCopy, ModeReencode} {
			t.Run(c.name+"/"+string(mode), func(t *testing.T) {
				src := makeClip(t, c)
				ctx := context.Background()
				vi, err := encoder.Probe(ctx, src)
				if err != nil {
					t.Fatal(err)
				}
				pk, err := encoder.Packets(ctx, src)
				if err != nil {
					t.Fatal(err)
				}
				res, err := Encode(ctx, src, vi, pk, t.TempDir(), Options{Mode: mode, GOPSeconds: 1}, nil)
				if err != nil {
					t.Fatal(err)
				}
				if res.Mode != mode {
					t.Fatalf("mode %s", res.Mode)
				}
				if res.Timescale != ChooseTimescale(vi) {
					t.Fatalf("timescale %d", res.Timescale)
				}

				// 1. Every source frame time survives exactly (same timescale, no rounding).
				want, got := sortedPTS(pk), recPTS(res.Records)
				if len(got) != len(want) {
					t.Fatalf("%d records, %d source frames", len(got), len(want))
				}
				for i := range want {
					if got[i] != want[i] {
						t.Fatalf("frame %d: pts %d, source %d", i, got[i], want[i])
					}
				}

				// 2. DTS never decreases, PTS >= DTS, first frame is a key frame.
				for i, r := range res.Records {
					if r.PTS < r.DTS {
						t.Fatalf("record %d: pts %d < dts %d", i, r.PTS, r.DTS)
					}
					if i > 0 && r.DTS < res.Records[i-1].DTS {
						t.Fatalf("record %d: dts goes backwards", i)
					}
				}
				if !res.Records[0].IsKey() || res.KeyFrames < 2 {
					t.Fatalf("key frames: first=%v total=%d", res.Records[0].IsKey(), res.KeyFrames)
				}

				// 3. Offsets are contiguous and cover the whole Annex B file.
				fi, _ := os.Stat(res.AnnexBPath)
				var next uint64
				for i, r := range res.Records {
					if r.Offset != next {
						t.Fatalf("record %d at %d, expected %d", i, r.Offset, next)
					}
					next += uint64(r.Size)
				}
				if int64(next) != fi.Size() || res.Bytes != fi.Size() {
					t.Fatalf("records cover %d bytes, file has %d", next, fi.Size())
				}

				// 4. Each record's bytes start with an access unit delimiter.
				f, _ := os.Open(res.AnnexBPath)
				defer f.Close()
				for i, r := range res.Records {
					b := make([]byte, 5)
					if _, err := f.ReadAt(b, int64(r.Offset)); err != nil {
						t.Fatal(err)
					}
					if string(b[:4]) != "\x00\x00\x00\x01" || b[4]&0x1F != 9 {
						t.Fatalf("record %d does not start with an AUD: % x", i, b)
					}
				}

				// 5. The stream decodes to as many frames as there are records.
				n := countFrames(t, res.AnnexBPath)
				if n != len(res.Records) {
					t.Fatalf("decoded %d frames, %d records", n, len(res.Records))
				}

				if mode == ModeCopy {
					// 6. Copy mode must not change a single pixel or timestamp.
					for i, r := range res.Records {
						if r.PTS != pk[i].PTS || r.DTS != pk[i].DTS {
							t.Fatalf("record %d: pts/dts %d/%d, source %d/%d", i, r.PTS, r.DTS, pk[i].PTS, pk[i].DTS)
						}
					}
					a := frameHashes(t, "-i", src)
					b := frameHashes(t, "-f", "h264", "-i", res.AnnexBPath)
					if strings.Join(a, ",") != strings.Join(b, ",") {
						t.Fatal("decoded pixels differ from the source in copy mode")
					}
				} else {
					// GOP limit: no more than ~1 s (30 frames) between key frames in decode order.
					last, maxGap := 0, 0
					for i, r := range res.Records {
						if r.IsKey() {
							if i-last > maxGap {
								maxGap = i - last
							}
							last = i
						}
					}
					if maxGap > 31 {
						t.Fatalf("GOP of %d frames with gop_seconds=1 at 30 fps", maxGap)
					}
				}
			})
		}
	}
}

func countFrames(t *testing.T, annexB string) int {
	t.Helper()
	out, err := exec.Command("ffprobe", "-v", "error", "-count_frames", "-select_streams", "v:0",
		"-show_entries", "stream=nb_read_frames", "-of", "csv=p=0", "-f", "h264", annexB).Output()
	if err != nil {
		t.Fatal(err)
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		t.Fatalf("frame count %q", out)
	}
	return n
}

func TestAutoPicksCopyForH264(t *testing.T) {
	src := makeClip(t, clips[0])
	ctx := context.Background()
	vi, _ := encoder.Probe(ctx, src)
	pk, _ := encoder.Packets(ctx, src)
	res, err := Encode(ctx, src, vi, pk, t.TempDir(), Options{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Mode != ModeCopy {
		t.Fatalf("auto chose %s for an H.264 source", res.Mode)
	}
}

func TestCopyRejectsNonH264(t *testing.T) {
	vi := encoder.VideoInfo{Codec: "mpeg4", PixFmt: "yuv420p"}
	if _, err := Encode(context.Background(), "x", vi, nil, t.TempDir(), Options{Mode: ModeCopy}, nil); err == nil {
		t.Fatal("copy of a non-H.264 source accepted")
	}
}

func TestSplitAccessUnitsRejectsRawNALs(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.h264")
	// SPS then slice, but no AUD: cannot be split safely.
	os.WriteFile(p, []byte{0, 0, 0, 1, 0x67, 1, 2, 3, 0, 0, 0, 1, 0x65, 9, 9}, 0o644)
	if _, err := SplitAccessUnits(p); err == nil {
		t.Fatal("expected an error for a stream without AUDs")
	}
}

func TestScale(t *testing.T) {
	// 1/25 s ticks -> 90000/s: 3 ticks = 10800.
	s := scale{num: 1 * 90000, den: 25}
	if got := s.apply(3); got != 10800 {
		t.Fatalf("got %d", got)
	}
	if got := s.apply(-3); got != -10800 {
		t.Fatalf("got %d", got)
	}
	if (scale{5, 5}).apply(7) != 7 {
		t.Fatal("identity scale changed a value")
	}
}
