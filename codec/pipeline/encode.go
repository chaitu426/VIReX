// Package pipeline wires ingestion, the pixel pipeline, the sampler and the container
// into one encode: video file in, .virex file out.
package pipeline

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"virex/codec/container"
	"virex/codec/encoder"
	"virex/codec/pixel"
	"virex/codec/sampler"
	"virex/codec/scheduler"
	"virex/cost"
	"virex/svir/schema"
)

// ToolVersion is written into every file's metadata.
const ToolVersion = "virex-encode 0.1"

// Config is everything an encode needs. It is the shape of configs/default.json.
type Config struct {
	Pixel   pixel.Options  `json:"pixel"`
	Sampler sampler.Config `json:"sampler"`

	// SkipSampling skips the sampling pass (it decodes the whole video once more).
	SkipSampling bool `json:"skip_sampling"`
	// Semantic selects the SVIR content: "empty" (Phase 1 default) or "fake" (hand-made
	// data, for testing the container and tools).
	Semantic string `json:"semantic"`
	// SplitLayers stores one SEMANTIC section per layer instead of one for all.
	SplitLayers bool `json:"split_layers"`

	// FrameBuffer is how many decoded frames may wait between ffmpeg and the sampler.
	FrameBuffer int `json:"frame_buffer"`
	// QueueBuffer is how many sampled frames may wait for the sink.
	QueueBuffer int `json:"queue_buffer"`
}

// DefaultConfig returns the Phase 1 defaults.
func DefaultConfig() Config {
	return Config{
		Pixel:       pixel.Options{Mode: pixel.ModeAuto, CRF: pixel.DefaultCRF, Preset: pixel.DefaultPreset, GOPSeconds: pixel.DefaultGOPSeconds},
		Sampler:     sampler.Config{Mode: sampler.SceneChange, MaxGapSeconds: 10},
		Semantic:    "empty",
		FrameBuffer: 8,
		QueueBuffer: 4,
	}
}

// Report summarises one encode.
type Report struct {
	Source        string
	Output        string
	PixelMode     pixel.Mode
	Frames        int
	KeyFrames     int
	Sampled       int
	SourceBytes   int64
	PixelBytes    int64
	OutputBytes   int64
	Width, Height int
	Timescale     uint32
	Warnings      []string
}

// SampleRecord is one sampled frame as stored in the metadata section.
type SampleRecord struct {
	FrameID int    `json:"frame_id"`
	PTS     int64  `json:"pts"`
	Reason  string `json:"reason"`
}

// Metadata is the JSON stored in the METADATA section.
type Metadata struct {
	Tool         string         `json:"tool"`
	SourceName   string         `json:"source_name"`
	SourceCodec  string         `json:"source_codec"`
	SourceBytes  int64          `json:"source_bytes"`
	PixelMode    pixel.Mode     `json:"pixel_mode"`
	Pixel        pixel.Options  `json:"pixel_options"`
	Sampler      sampler.Config `json:"sampler"`
	Samples      []SampleRecord `json:"samples"`
	AudioDropped string         `json:"audio_dropped,omitempty"`
	Frames       int            `json:"frames"`
	KeyFrames    int            `json:"key_frames"`
}

// Encode turns src into a .virex file at out. workDir holds temporary files and is
// removed afterwards. log may be nil.
func Encode(ctx context.Context, src, out string, cfg Config, log *cost.Logger) (*Report, error) {
	rep := &Report{Source: src, Output: out}
	srcStat, err := os.Stat(src)
	if err != nil {
		return nil, err
	}
	rep.SourceBytes = srcStat.Size()

	sp := log.Stage("ingest_probe")
	vi, err := encoder.Probe(ctx, src)
	if err != nil {
		return nil, err
	}
	pkts, err := encoder.Packets(ctx, src)
	if err != nil {
		return nil, err
	}
	if len(pkts) == 0 {
		return nil, fmt.Errorf("pipeline: %s has no video frames", src)
	}
	sp.Bytes(rep.SourceBytes, 0)
	sp.Note(fmt.Sprintf("%dx%d %s %s, %d frames", vi.Width, vi.Height, vi.Codec, vi.PixFmt, len(pkts)))
	sp.End()

	meta := Metadata{Tool: ToolVersion, SourceName: filepath.Base(src), SourceCodec: vi.Codec, SourceBytes: rep.SourceBytes}
	if ac, err := encoder.AudioCodec(ctx, src); err == nil && ac != "" {
		meta.AudioDropped = ac
		rep.Warnings = append(rep.Warnings,
			fmt.Sprintf("source has %s audio; audio is not stored in .virex v0.1, the decoded video will be silent", ac))
	}

	work, err := os.MkdirTemp(filepath.Dir(absOr(out)), ".virex-work-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(work)

	px, err := pixel.Encode(ctx, src, vi, pkts, work, cfg.Pixel, log)
	if err != nil {
		return nil, err
	}
	rep.PixelMode, rep.Frames, rep.KeyFrames, rep.PixelBytes = px.Mode, len(px.Records), px.KeyFrames, px.Bytes
	rep.Width, rep.Height, rep.Timescale = px.Width, px.Height, px.Timescale
	meta.PixelMode, meta.Pixel, meta.Frames, meta.KeyFrames = px.Mode, cfg.Pixel, len(px.Records), px.KeyFrames
	meta.Sampler = cfg.Sampler

	if !cfg.SkipSampling {
		sp := log.Stage("sample")
		samples, err := runSampler(ctx, src, vi, pkts, cfg, ptsScale(vi, px.Timescale))
		if err != nil {
			return nil, err
		}
		meta.Samples = samples
		rep.Sampled = len(samples)
		sp.Bytes(rep.SourceBytes, 0)
		sp.Note(fmt.Sprintf("%d of %d frames sampled (%s)", len(samples), len(pkts), cfg.Sampler.Mode))
		sp.End()
	}

	semantic, err := buildSemantic(cfg, px)
	if err != nil {
		return nil, err
	}

	sp = log.Stage("container_write")
	if err := writeContainer(out, vi, px, semantic, meta); err != nil {
		return nil, err
	}
	st, err := os.Stat(out)
	if err != nil {
		return nil, err
	}
	rep.OutputBytes = st.Size()
	sp.Bytes(px.Bytes, rep.OutputBytes)
	sp.End()
	return rep, nil
}

func absOr(p string) string {
	if a, err := filepath.Abs(p); err == nil {
		return a
	}
	return p
}

// ptsScale converts source ticks to the file's timescale (identity when the source
// time base is 1/timescale).
func ptsScale(vi encoder.VideoInfo, ts uint32) func(int64) int64 {
	num, den := int64(vi.TimeBaseNum)*int64(ts), int64(vi.TimeBaseDen)
	return func(v int64) int64 {
		if num == den {
			return v
		}
		return (v*num + den/2) / den
	}
}

func runSampler(ctx context.Context, src string, vi encoder.VideoInfo, pkts []encoder.Packet, cfg Config, toTicks func(int64) int64) ([]SampleRecord, error) {
	strat, err := sampler.New(cfg.Sampler, vi.Width, vi.Height)
	if err != nil {
		return nil, err
	}
	var out []SampleRecord
	_, err = scheduler.Run(ctx, func(c context.Context) (<-chan encoder.Frame, func() error) {
		return encoder.Frames(c, src, vi, pkts, cfg.FrameBuffer)
	}, strat, scheduler.Options{Buffer: cfg.QueueBuffer}, func(s scheduler.Sampled) error {
		out = append(out, SampleRecord{FrameID: s.Frame.ID, PTS: toTicks(s.Frame.PTS), Reason: s.Reason})
		return nil
	})
	return out, err
}

// section is one SEMANTIC section to write.
type section struct {
	layer container.Layer
	data  []byte
}

func buildSemantic(cfg Config, px *pixel.Result) ([]section, error) {
	var doc *schema.SVIRDocument
	switch cfg.Semantic {
	case "", "empty":
		doc = schema.NewDocument()
		doc.TEnd = int64(px.Duration)
	case "fake":
		doc = schema.Example(int64(px.Timescale), float64(px.Duration)/float64(px.Timescale))
	default:
		return nil, fmt.Errorf("pipeline: unknown semantic mode %q (want empty or fake)", cfg.Semantic)
	}
	schema.Normalize(doc)
	if err := schema.Validate(doc, true); err != nil {
		return nil, err
	}
	if !cfg.SplitLayers {
		b, err := schema.Marshal(doc)
		return []section{{container.LayerAll, b}}, err
	}
	parts := schema.SplitByLayer(doc)
	var layers []int
	for l := range parts {
		layers = append(layers, int(l))
	}
	sort.Ints(layers)
	var out []section
	for _, l := range layers {
		b, err := schema.Marshal(parts[schema.Layer(l)])
		if err != nil {
			return nil, err
		}
		out = append(out, section{container.Layer(l), b})
	}
	if len(out) == 0 { // an empty document still gets its section
		b, _ := schema.Marshal(doc)
		out = append(out, section{container.LayerAll, b})
	}
	return out, nil
}

func writeContainer(out string, vi encoder.VideoInfo, px *pixel.Result, semantic []section, meta Metadata) error {
	metaJSON, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	fpsNum, fpsDen := vi.FPSNum, vi.FPSDen
	if fpsNum <= 0 || fpsDen <= 0 {
		fpsNum, fpsDen = 30, 1
	}
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		f.Close()
		if !ok {
			os.Remove(out) // never leave a half-written file behind
		}
	}()

	h := container.Header{
		Width: uint32(px.Width), Height: uint32(px.Height),
		FPSNum: uint32(fpsNum), FPSDen: uint32(fpsDen),
		Timescale: px.Timescale, Duration: px.Duration,
		PixelCodec:  container.CodecH264AnnexB,
		SchemaMajor: schema.VersionMajor, SchemaMinor: schema.VersionMinor,
	}
	w, err := container.NewWriter(f, h, 3+len(semantic))
	if err != nil {
		return err
	}
	pf, err := os.Open(px.AnnexBPath)
	if err != nil {
		return err
	}
	defer pf.Close()
	if err := w.AddSection(container.SectionPixel, container.LayerNone, 0, pf); err != nil {
		return err
	}
	if err := w.AddSection(container.SectionTemporalIndex, container.LayerNone, 0, bytesReader(container.EncodeFrameIndex(px.Records))); err != nil {
		return err
	}
	for _, s := range semantic {
		if err := w.AddSection(container.SectionSemantic, s.layer, 0, bytesReader(s.data)); err != nil {
			return err
		}
	}
	if err := w.AddSection(container.SectionMetadata, container.LayerNone, 0, bytesReader(metaJSON)); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	ok = true
	return nil
}

func bytesReader(b []byte) *bytes.Reader { return bytes.NewReader(b) }
