package pixel

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"virex/codec/container"
	"virex/codec/encoder"
	"virex/cost"
)

// Mode says how the pixel stream is produced.
type Mode string

const (
	// ModeReencode encodes with libx264 at the given CRF.
	ModeReencode Mode = "reencode"
	// ModeCopy keeps the source's own H.264 packets: no generation loss at all.
	// It needs an 8-bit yuv420p H.264 source.
	ModeCopy Mode = "copy"
	// ModeAuto copies when the source allows it and re-encodes otherwise.
	ModeAuto Mode = "auto"
)

// Options configure the pixel pipeline. Zero fields get the defaults chosen in
// research/aniket/research/2026-10-01-keyframes-and-x264-settings.md.
type Options struct {
	Mode       Mode    `json:"mode"`        // default auto
	CRF        int     `json:"crf"`         // default 18
	Preset     string  `json:"preset"`      // default medium
	GOPSeconds float64 `json:"gop_seconds"` // default 2: maximum distance between key frames
	Threads    int     `json:"threads"`     // 0 = ffmpeg decides
}

// Defaults.
const (
	DefaultCRF        = 18
	DefaultPreset     = "medium"
	DefaultGOPSeconds = 2.0
)

func (o Options) withDefaults() Options {
	if o.Mode == "" {
		o.Mode = ModeAuto
	}
	if o.CRF == 0 {
		o.CRF = DefaultCRF
	}
	if o.Preset == "" {
		o.Preset = DefaultPreset
	}
	if o.GOPSeconds == 0 {
		o.GOPSeconds = DefaultGOPSeconds
	}
	return o
}

// Result is a finished pixel stream plus everything the container needs about it.
type Result struct {
	AnnexBPath string                  // H.264 Annex B elementary stream
	Records    []container.FrameRecord // one per coded frame, decode order, offsets inside AnnexBPath
	Timescale  uint32                  // ticks per second for every timestamp in Records
	Duration   uint64                  // ticks
	Width      int
	Height     int
	Mode       Mode // the mode actually used (never auto)
	Bytes      int64
	KeyFrames  int
}

// ChooseTimescale picks the ticks-per-second used for timestamps: the source's own
// time base when it is 1/N (so source timestamps carry over unchanged), else 90000.
func ChooseTimescale(vi encoder.VideoInfo) uint32 {
	if vi.TimeBaseNum == 1 && vi.TimeBaseDen > 0 {
		return uint32(vi.TimeBaseDen)
	}
	return 90000
}

// CanCopy reports whether the source can be stream-copied into the pixel section.
func CanCopy(vi encoder.VideoInfo) bool {
	return vi.Codec == "h264" && vi.PixFmt == "yuv420p"
}

// Encode produces the pixel stream for src in workDir. srcPkts are the source's packets
// (encoder.Packets), used for copy mode and for checking no frame was lost.
func Encode(ctx context.Context, src string, vi encoder.VideoInfo, srcPkts []encoder.Packet, workDir string, opt Options, log *cost.Logger) (*Result, error) {
	opt = opt.withDefaults()
	mode := opt.Mode
	switch mode {
	case ModeAuto:
		mode = ModeReencode
		if CanCopy(vi) {
			mode = ModeCopy
		}
	case ModeCopy:
		if !CanCopy(vi) {
			return nil, fmt.Errorf("pixel: copy mode needs 8-bit yuv420p H.264, source is %s/%s", vi.Codec, vi.PixFmt)
		}
	case ModeReencode:
	default:
		return nil, fmt.Errorf("pixel: unknown mode %q", opt.Mode)
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return nil, err
	}
	srcInfo, err := os.Stat(src)
	if err != nil {
		return nil, err
	}

	ts := ChooseTimescale(vi)
	var pkts []encoder.Packet // packets of the stream that ends up in the pixel section
	var annexIn string        // file to convert to Annex B
	var pktScale scale        // converts those packets' ticks to ts
	annexOut := filepath.Join(workDir, "video.h264")

	if mode == ModeReencode {
		mp4 := filepath.Join(workDir, "video.mp4")
		gop := int(math.Round(float64(vi.FPSNum) / float64(max(vi.FPSDen, 1)) * opt.GOPSeconds))
		if gop < 1 {
			gop = 1
		}
		args := []string{"-v", "error", "-nostdin", "-y", "-i", src, "-map", "0:v:0", "-an", "-sn", "-dn",
			"-fps_mode", "passthrough", "-c:v", "libx264", "-crf", strconv.Itoa(opt.CRF), "-preset", opt.Preset,
			"-g", strconv.Itoa(gop), "-pix_fmt", "yuv420p", "-video_track_timescale", strconv.Itoa(int(ts))}
		if opt.Threads > 0 {
			args = append(args, "-threads", strconv.Itoa(opt.Threads))
		}
		args = append(args, mp4)
		sp := log.Stage("pixel_x264")
		if err := run(ctx, sp, "ffmpeg", args...); err != nil {
			return nil, err
		}
		st, _ := os.Stat(mp4)
		sp.Bytes(srcInfo.Size(), st.Size())
		sp.Note(fmt.Sprintf("crf=%d preset=%s gop=%d", opt.CRF, opt.Preset, gop))
		sp.End()

		if pkts, err = encoder.Packets(ctx, mp4); err != nil {
			return nil, err
		}
		out, err := encoder.Probe(ctx, mp4)
		if err != nil {
			return nil, err
		}
		pktScale = scaleFor(out, ts)
		if pktScale.num != pktScale.den {
			return nil, fmt.Errorf("pixel: encoded time base %d/%d does not match timescale %d", out.TimeBaseNum, out.TimeBaseDen, ts)
		}
		annexIn = mp4
	} else {
		pkts = srcPkts
		pktScale = scaleFor(vi, ts)
		annexIn = src
	}
	if len(pkts) != len(srcPkts) {
		return nil, fmt.Errorf("pixel: encoded stream has %d frames, source has %d", len(pkts), len(srcPkts))
	}

	sp := log.Stage("pixel_annexb")
	if err := run(ctx, sp, "ffmpeg", "-v", "error", "-nostdin", "-y", "-i", annexIn, "-map", "0:v:0", "-c:v", "copy",
		"-bsf:v", "h264_mp4toannexb,h264_metadata=aud=insert", "-f", "h264", annexOut); err != nil {
		return nil, err
	}
	st, err := os.Stat(annexOut)
	if err != nil {
		return nil, err
	}
	sp.Bytes(srcInfo.Size(), st.Size())
	sp.End()

	aus, err := SplitAccessUnits(annexOut)
	if err != nil {
		return nil, err
	}
	if len(aus) != len(pkts) {
		return nil, fmt.Errorf("pixel: Annex B stream has %d frames but the source packets say %d", len(aus), len(pkts))
	}

	// The mp4 muxer moves a re-encoded stream's timeline to start at zero. Put the
	// source's start offset back, so every timestamp equals the source's exactly.
	var shift int64
	if mode == ModeReencode && len(pkts) > 0 {
		shift = scaleFor(vi, ts).apply(minPTS(srcPkts)) - pktScale.apply(minPTS(pkts))
	}

	res := &Result{AnnexBPath: annexOut, Timescale: ts, Width: vi.Width, Height: vi.Height, Mode: mode, Bytes: st.Size()}
	var endMax int64
	first := true
	var firstPTS int64
	for i, p := range pkts {
		pts, dts, dur := pktScale.apply(p.PTS)+shift, pktScale.apply(p.DTS)+shift, pktScale.apply(p.Dur)
		fl := uint32(0)
		if p.Key {
			fl = container.FrameKey
			res.KeyFrames++
		}
		res.Records = append(res.Records, container.FrameRecord{PTS: pts, DTS: dts, Offset: aus[i].Offset, Size: uint32(aus[i].Size), Flags: fl})
		if first || pts < firstPTS {
			firstPTS, first = pts, false
		}
		if pts+dur > endMax {
			endMax = pts + dur
		}
	}
	if len(res.Records) > 0 && (!res.Records[0].IsKey()) {
		return nil, fmt.Errorf("pixel: first frame is not a key frame")
	}
	if endMax > 0 {
		res.Duration = uint64(endMax)
	}
	return res, nil
}

// scale converts ticks of one time base to another: v * num / den, rounded.
type scale struct{ num, den int64 }

func scaleFor(vi encoder.VideoInfo, ts uint32) scale {
	// seconds per source tick = TBNum/TBDen; new ticks = v * TBNum * ts / TBDen.
	return scale{num: int64(vi.TimeBaseNum) * int64(ts), den: int64(vi.TimeBaseDen)}
}

func (s scale) apply(v int64) int64 {
	if s.num == s.den {
		return v
	}
	n := v * s.num
	if n >= 0 {
		return (n + s.den/2) / s.den
	}
	return -((-n + s.den/2) / s.den)
}

// AccessUnit locates one coded frame inside an Annex B file.
type AccessUnit struct {
	Offset uint64
	Size   int
}

// SplitAccessUnits finds every access unit in an Annex B file. It relies on the access
// unit delimiter (NAL type 9) at the start of every frame. Encode adds them with
// ffmpeg's h264_metadata=aud=insert filter; plain h264_mp4toannexb does not.
func SplitAccessUnits(path string) ([]AccessUnit, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 1<<20)

	var starts []uint64
	var pos uint64      // offset of the next byte to read
	var zeros int       // consecutive zero bytes just before pos
	var startOff uint64 // where the most recent start code began
	expectNAL := false
	for {
		b, err := r.ReadByte()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if expectNAL {
			expectNAL = false
			if b&0x1F == 9 { // access unit delimiter
				starts = append(starts, startOff)
			}
		}
		if b == 1 && zeros >= 2 {
			startOff = pos - 2
			if zeros >= 3 {
				startOff = pos - 3 // four-byte start code
			}
			expectNAL = true
		}
		if b == 0 {
			zeros++
		} else {
			zeros = 0
		}
		pos++
	}
	if len(starts) == 0 || starts[0] != 0 {
		return nil, fmt.Errorf("pixel: %s: stream does not begin with an access unit delimiter", path)
	}
	aus := make([]AccessUnit, len(starts))
	for i, s := range starts {
		end := pos
		if i+1 < len(starts) {
			end = starts[i+1]
		}
		aus[i] = AccessUnit{Offset: s, Size: int(end - s)}
	}
	return aus, nil
}

// run executes a command, adding its CPU time to the cost span.
func run(ctx context.Context, sp *cost.Span, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if cmd.ProcessState != nil {
		sp.AddCPU(cmd.ProcessState)
	}
	if err != nil {
		return fmt.Errorf("pixel: %s failed: %w: %s", name, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func minPTS(p []encoder.Packet) int64 {
	m := p[0].PTS
	for _, x := range p {
		if x.PTS < m {
			m = x.PTS
		}
	}
	return m
}
