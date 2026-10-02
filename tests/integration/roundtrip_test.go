package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	"virex/codec/container"
	"virex/codec/decoder"
	"virex/codec/pipeline"
	"virex/codec/pixel"
	"virex/codec/sampler"
	"virex/cost"
	"virex/svir/schema"
)

const clipDir = "../testdata/clips"

func clips(t *testing.T) []string {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not on PATH")
	}
	files, _ := filepath.Glob(filepath.Join(clipDir, "*.mp4"))
	if len(files) == 0 {
		t.Skip("no test clips; run scripts/make-testclips.ps1")
	}
	return files
}

func encode(t *testing.T, src string, cfg pipeline.Config) (string, *pipeline.Report, *bytes.Buffer) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "o.virex")
	logBuf := &bytes.Buffer{}
	rep, err := pipeline.Encode(context.Background(), src, out, cfg, cost.New(logBuf, "test"))
	if err != nil {
		t.Fatalf("encode %s: %v", src, err)
	}
	return out, rep, logBuf
}

func decode(t *testing.T, virex string) string {
	t.Helper()
	f, err := os.Open(virex)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	st, _ := f.Stat()
	rd, err := container.NewReader(f, st.Size())
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "o.mp4")
	o, err := os.Create(out)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decoder.Decode(rd, o, false); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if err := o.Close(); err != nil {
		t.Fatal(err)
	}
	return out
}

// packets returns pts,dts,flags of every video packet as text lines, in file order.
func packets(t *testing.T, path string) []string {
	t.Helper()
	out, err := exec.Command("ffprobe", "-v", "error", "-select_streams", "v:0",
		"-show_entries", "packet=pts,dts,flags", "-of", "csv=p=0", path).Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.Fields(strings.TrimSpace(string(out)))
}

func ptsList(lines []string) []int64 {
	var v []int64
	for _, l := range lines {
		n, _ := strconv.ParseInt(strings.Split(l, ",")[0], 10, 64)
		v = append(v, n)
	}
	sort.Slice(v, func(i, j int) bool { return v[i] < v[j] })
	return v
}

var md5re = regexp.MustCompile(`(?m)^\d+,\s*[-\d]+,\s*[-\d]+,\s*[-\d]+,\s*\d+,\s*([0-9a-f]{32})\s*$`)

func frameHashes(t *testing.T, args ...string) string {
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
	return strings.Join(h, ",")
}

var ssimRe = regexp.MustCompile(`SSIM.*All:([\d.]+)`)
var psnrRe = regexp.MustCompile(`PSNR.*average:([\d.]+|inf)`)

func quality(t *testing.T, dist, ref string) (ssim, psnr float64) {
	t.Helper()
	out, _ := exec.Command("ffmpeg", "-hide_banner", "-i", dist, "-i", ref, "-lavfi",
		"[0:v][1:v]ssim;[0:v][1:v]psnr", "-f", "null", "-").CombinedOutput()
	m := ssimRe.FindSubmatch(out)
	p := psnrRe.FindSubmatch(out)
	if m == nil || p == nil {
		t.Fatalf("could not read ssim/psnr: %s", out)
	}
	ssim, _ = strconv.ParseFloat(string(m[1]), 64)
	if string(p[1]) == "inf" {
		psnr = 1000
	} else {
		psnr, _ = strconv.ParseFloat(string(p[1]), 64)
	}
	return
}

// TestRoundTripAllClips: encode every test clip with the default config (stream copy for
// H.264 sources), decode it, and require the original timestamps and pixels back.
func TestRoundTripAllClips(t *testing.T) {
	for _, src := range clips(t) {
		src := src
		t.Run(filepath.Base(src), func(t *testing.T) {
			virex, rep, logs := encode(t, src, pipeline.DefaultConfig())
			if rep.PixelMode != pixel.ModeCopy {
				t.Fatalf("default config used %s on an H.264 clip", rep.PixelMode)
			}
			dec := decode(t, virex)

			a, b := packets(t, src), packets(t, dec)
			if len(a) != len(b) {
				t.Fatalf("source has %d frames, decoded has %d", len(a), len(b))
			}
			for i := range a {
				if a[i] != b[i] {
					t.Fatalf("packet %d: source %q, decoded %q (pts,dts,flags must be identical)", i, a[i], b[i])
				}
			}
			if frameHashes(t, "-i", src) != frameHashes(t, "-i", dec) {
				t.Fatal("decoded pixels differ from the source")
			}
			ssim, _ := quality(t, dec, src)
			if ssim != 1 {
				t.Fatalf("SSIM %v, want exactly 1 for stream copy", ssim)
			}

			// Cost logger: every stage reported, as JSON lines.
			stages := map[string]bool{}
			for _, l := range bytes.Split(bytes.TrimSpace(logs.Bytes()), []byte("\n")) {
				var r cost.Record
				if err := json.Unmarshal(l, &r); err != nil {
					t.Fatalf("bad cost line %q: %v", l, err)
				}
				stages[r.Stage] = true
				if r.WallMS <= 0 && r.Stage != "container_write" {
					t.Errorf("stage %s has wall_ms %v", r.Stage, r.WallMS)
				}
			}
			for _, s := range []string{"ingest_probe", "pixel_annexb", "sample", "container_write"} {
				if !stages[s] {
					t.Errorf("cost log has no %q record", s)
				}
			}
		})
	}
}

// TestReencodeNoQualityDrop: with x264 at the default CRF the decoded video must be
// bit-identical to the x264 stream, and close to the source.
func TestReencodeQuality(t *testing.T) {
	for _, name := range []string{"c01_mandelbrot_720p.mp4", "c02_testsrc2_360p.mp4", "c03_life_360p.mp4", "c05_vfr_360p.mp4"} {
		name := name
		t.Run(name, func(t *testing.T) {
			src := filepath.Join(clipDir, name)
			if _, err := os.Stat(src); err != nil {
				t.Skip("clip missing")
			}
			if testing.Short() && strings.HasPrefix(name, "c01") {
				t.Skip("slow")
			}
			cfg := pipeline.DefaultConfig()
			cfg.Pixel.Mode = pixel.ModeReencode
			cfg.SkipSampling = true
			virex, rep, _ := encode(t, src, cfg)
			dec := decode(t, virex)

			// Exact timestamps survive re-encoding.
			if !equalInts(ptsList(packets(t, src)), ptsList(packets(t, dec))) {
				t.Fatal("presentation timestamps changed")
			}
			ssim, psnr := quality(t, dec, src)
			t.Logf("%s crf %d: SSIM %.4f PSNR %.2f dB, %.2f MB -> %.2f MB", name, cfg.Pixel.CRF, ssim, psnr,
				float64(rep.SourceBytes)/1e6, float64(rep.PixelBytes)/1e6)
			if ssim < 0.985 || psnr < 38 {
				t.Fatalf("quality dropped: SSIM %.4f PSNR %.2f", ssim, psnr)
			}

			// The decoded file must equal the x264 stream inside the .virex frame for frame.
			f, _ := os.Open(virex)
			defer f.Close()
			st, _ := f.Stat()
			rd, _ := container.NewReader(f, st.Size())
			px, _ := rd.FindType(container.SectionPixel)
			annexB := filepath.Join(t.TempDir(), "p.h264")
			data, err := rd.ReadSection(px)
			if err != nil {
				t.Fatal(err)
			}
			os.WriteFile(annexB, data, 0o644)
			if frameHashes(t, "-f", "h264", "-i", annexB) != frameHashes(t, "-i", dec) {
				t.Fatal("decoded video differs from the pixel stream stored in the file")
			}
		})
	}
}

func equalInts(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// countingReaderAt counts bytes read, to prove random access.
type countingReaderAt struct {
	r io.ReaderAt
	n int64
}

func (c *countingReaderAt) ReadAt(p []byte, off int64) (int, error) {
	n, err := c.r.ReadAt(p, off)
	c.n += int64(n)
	return n, err
}

func TestSemanticSectionWithoutReadingPixels(t *testing.T) {
	src := filepath.Join(clipDir, "c02_testsrc2_360p.mp4")
	if _, err := os.Stat(src); err != nil {
		t.Skip("clip missing")
	}
	cfg := pipeline.DefaultConfig()
	cfg.SkipSampling = true
	cfg.Semantic = "fake"
	virex, _, _ := encode(t, src, cfg)

	f, _ := os.Open(virex)
	defer f.Close()
	st, _ := f.Stat()
	c := &countingReaderAt{r: f}
	rd, err := container.NewReader(c, st.Size())
	if err != nil {
		t.Fatal(err)
	}
	e, ok := rd.Find(container.SectionSemantic, container.LayerAll)
	if !ok {
		t.Fatal("no semantic section")
	}
	b, err := rd.ReadSection(e)
	if err != nil {
		t.Fatal(err)
	}
	px, _ := rd.FindType(container.SectionPixel)
	if c.n >= int64(px.Length)/10 {
		t.Fatalf("read %d bytes to reach the semantic data; pixel section is %d", c.n, px.Length)
	}
	doc, err := schema.Unmarshal(b)
	if err != nil {
		t.Fatal(err)
	}
	want := schema.Example(int64(rd.Header.Timescale), float64(rd.Header.Duration)/float64(rd.Header.Timescale))
	if !proto.Equal(doc, want) {
		t.Fatal("semantic data changed on the way through the file")
	}
}

func TestSplitLayersRoundTrip(t *testing.T) {
	src := filepath.Join(clipDir, "c08_static_360p.mp4")
	if _, err := os.Stat(src); err != nil {
		t.Skip("clip missing")
	}
	cfg := pipeline.DefaultConfig()
	cfg.SkipSampling = true
	cfg.Semantic = "fake"
	cfg.SplitLayers = true
	virex, _, _ := encode(t, src, cfg)

	f, _ := os.Open(virex)
	defer f.Close()
	st, _ := f.Stat()
	rd, _ := container.NewReader(f, st.Size())
	var docs []*schema.SVIRDocument
	for _, layer := range []container.Layer{container.LayerEntity, container.LayerEvent, container.LayerText, container.LayerEmbedding} {
		e, ok := rd.Find(container.SectionSemantic, layer)
		if !ok {
			t.Fatalf("no section for layer %d", layer)
		}
		b, err := rd.ReadSection(e)
		if err != nil {
			t.Fatal(err)
		}
		d, err := schema.Unmarshal(b)
		if err != nil {
			t.Fatal(err)
		}
		if int(d.Layer) != int(layer) {
			t.Fatalf("section layer %d holds a document for layer %d", layer, d.Layer)
		}
		docs = append(docs, d)
	}
	want := schema.Example(int64(rd.Header.Timescale), float64(rd.Header.Duration)/float64(rd.Header.Timescale))
	got := schema.Merge(docs...)
	if len(got.Objects) != len(want.Objects) || len(got.Events) != len(want.Events) || len(got.Texts) != len(want.Texts) ||
		len(got.Entities) != len(want.Entities) || len(got.Embeddings) != len(want.Embeddings) {
		t.Fatal("merged layers differ from the original document")
	}
}

// The container layer ids and the SVIR Layer enum must stay in step (C3).
func TestLayerIDsMatchSchema(t *testing.T) {
	pairs := map[container.Layer]schema.Layer{
		container.LayerEntity:    schema.Layer_L0_ENTITY,
		container.LayerEvent:     schema.Layer_L1_EVENT,
		container.LayerText:      schema.Layer_L2_TEXT,
		container.LayerEmbedding: schema.Layer_L3_EMBEDDING,
	}
	for c, s := range pairs {
		if int32(c) != int32(s) {
			t.Fatalf("container layer %d != schema layer %v (%d)", c, s, int32(s))
		}
	}
	if schema.VersionMajor != 0 || schema.VersionMinor != 1 {
		t.Fatal("schema version changed; update the docs and the freeze snapshot deliberately")
	}
}

func TestCorruptionIsDetected(t *testing.T) {
	src := filepath.Join(clipDir, "c02_testsrc2_360p.mp4")
	if _, err := os.Stat(src); err != nil {
		t.Skip("clip missing")
	}
	cfg := pipeline.DefaultConfig()
	cfg.SkipSampling = true
	virex, _, _ := encode(t, src, cfg)
	good, _ := os.ReadFile(virex)

	rd, _ := container.NewReader(bytes.NewReader(good), int64(len(good)))
	px, _ := rd.FindType(container.SectionPixel)
	bad := append([]byte(nil), good...)
	bad[px.Offset+px.Length/2] ^= 0x5A
	badPath := filepath.Join(t.TempDir(), "bad.virex")
	os.WriteFile(badPath, bad, 0o644)

	f, _ := os.Open(badPath)
	defer f.Close()
	st, _ := f.Stat()
	rd2, err := container.NewReader(f, st.Size())
	if err != nil {
		t.Fatalf("header and table are intact and must open: %v", err)
	}
	o, err := os.Create(filepath.Join(t.TempDir(), "x.mp4"))
	if err != nil {
		t.Fatal(err)
	}
	defer o.Close()
	if _, err := decoder.Decode(rd2, o, false); err == nil {
		t.Fatal("decode accepted a damaged pixel section")
	}
}

// Sampling decisions are written into the metadata with exact timestamps.
func TestSamplesRecordedInMetadata(t *testing.T) {
	src := filepath.Join(clipDir, "c04_cuts_360p.mp4")
	if _, err := os.Stat(src); err != nil {
		t.Skip("clip missing")
	}
	cfg := pipeline.DefaultConfig()
	cfg.Sampler = sampler.Config{Mode: sampler.SceneChange}
	virex, rep, _ := encode(t, src, cfg)
	if rep.Sampled != 4 {
		t.Fatalf("sampled %d frames, want 4 (start + 3 cuts)", rep.Sampled)
	}
	f, _ := os.Open(virex)
	defer f.Close()
	st, _ := f.Stat()
	rd, _ := container.NewReader(f, st.Size())
	e, _ := rd.FindType(container.SectionMetadata)
	b, err := rd.ReadSection(e)
	if err != nil {
		t.Fatal(err)
	}
	var meta pipeline.Metadata
	if err := json.Unmarshal(b, &meta); err != nil {
		t.Fatal(err)
	}
	ts := float64(rd.Header.Timescale)
	want := []float64{0, 2.5, 5.0, 7.5}
	if len(meta.Samples) != 4 {
		t.Fatalf("metadata has %d samples", len(meta.Samples))
	}
	for i, s := range meta.Samples {
		if got := float64(s.PTS) / ts; got < want[i]-0.05 || got > want[i]+0.05 {
			t.Fatalf("sample %d at %.3fs, want about %.1fs", i, got, want[i])
		}
	}
	if meta.Frames != 300 || meta.PixelMode != pixel.ModeCopy {
		t.Fatalf("metadata: %+v", meta)
	}
}

// Sources with awkward timing: first frame at 3 s, and Matroska (millisecond time base,
// no DTS stored). Every presentation time must come back exactly, in both pixel modes.
func TestTimestampEdgeCases(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not on PATH")
	}
	cases := []struct {
		name, ext string
		args      []string
	}{
		{"offset-start", "mp4", []string{"-vf", "setpts=PTS+3/TB", "-t", "7"}},
		{"mkv-vfr", "mkv", []string{"-vf", "select='not(eq(mod(n,7),3))'", "-fps_mode", "vfr", "-f", "matroska"}},
	}
	for _, c := range cases {
		for _, mode := range []pixel.Mode{pixel.ModeCopy, pixel.ModeReencode} {
			c, mode := c, mode
			t.Run(c.name+"/"+string(mode), func(t *testing.T) {
				src := filepath.Join(t.TempDir(), "src."+c.ext)
				args := append([]string{"-v", "error", "-y", "-f", "lavfi", "-i", "testsrc2=size=160x120:rate=30", "-t", "4"}, c.args...)
				args = append(args, "-c:v", "libx264", "-crf", "14", "-bf", "2", "-g", "40", "-pix_fmt", "yuv420p", src)
				if out, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
					t.Fatalf("ffmpeg: %v: %s", err, out)
				}
				cfg := pipeline.DefaultConfig()
				cfg.Pixel.Mode = mode
				cfg.SkipSampling = true
				virex, _, _ := encode(t, src, cfg)
				dec := decode(t, virex)
				if !equalInts(ptsList(packets(t, src)), ptsList(packets(t, dec))) {
					t.Fatal("presentation timestamps changed")
				}
				if mode == pixel.ModeCopy && frameHashes(t, "-i", src) != frameHashes(t, "-i", dec) {
					t.Fatal("decoded pixels differ from the source")
				}
			})
		}
	}
}
