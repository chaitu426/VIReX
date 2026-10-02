package encoder

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

// VideoInfo is what ffprobe reports about the first video stream.
type VideoInfo struct {
	Codec       string
	PixFmt      string // e.g. yuv420p
	Width       int
	Height      int
	FPSNum      int // average frame rate; nominal only, never use it for frame times
	FPSDen      int
	TimeBaseNum int // timestamps are in units of TimeBaseNum/TimeBaseDen seconds
	TimeBaseDen int
	Duration    float64 // seconds, 0 if unknown
	NbFrames    int     // 0 if the container does not say
}

// Packet is one compressed packet as stored in the source file. PTS and DTS are in
// time-base ticks, copied from the source. Size and Pos describe the SOURCE file;
// offsets in a .virex file come from the pixel pipeline, not from here.
type Packet struct {
	PTS  int64
	DTS  int64
	Dur  int64 // packet duration in ticks, 0 if unknown
	Size int
	Pos  int64
	Key  bool
}

// Frame is one decoded RGB24 frame with its real presentation time.
type Frame struct {
	ID    int     // 0-based, display order
	PTS   int64   // ticks of VideoInfo's time base
	Time  float64 // seconds
	Image []byte  // Width*Height*3 bytes, RGB24
}

// Probe runs ffprobe for stream facts.
func Probe(ctx context.Context, path string) (VideoInfo, error) {
	out, err := runProbe(ctx, path,
		"stream=codec_name,pix_fmt,width,height,avg_frame_rate,time_base,duration,nb_frames")
	if err != nil {
		return VideoInfo{}, err
	}
	rows := parseCompact(out)
	if len(rows) != 1 {
		return VideoInfo{}, fmt.Errorf("ingest: %s: expected one video stream, ffprobe gave %d", path, len(rows))
	}
	r := rows[0]
	var vi VideoInfo
	vi.Codec = r["codec_name"]
	vi.PixFmt = r["pix_fmt"]
	vi.Width, _ = strconv.Atoi(r["width"])
	vi.Height, _ = strconv.Atoi(r["height"])
	vi.FPSNum, vi.FPSDen = parseFraction(r["avg_frame_rate"])
	vi.TimeBaseNum, vi.TimeBaseDen = parseFraction(r["time_base"])
	vi.Duration, _ = strconv.ParseFloat(r["duration"], 64)
	vi.NbFrames, _ = strconv.Atoi(r["nb_frames"])
	if vi.Width <= 0 || vi.Height <= 0 || vi.TimeBaseNum <= 0 || vi.TimeBaseDen <= 0 {
		return VideoInfo{}, fmt.Errorf("ingest: %s: unusable stream info %+v", path, vi)
	}
	return vi, nil
}

// Packets returns every video packet in stored (decode) order with its real PTS/DTS.
func Packets(ctx context.Context, path string) ([]Packet, error) {
	out, err := runProbe(ctx, path, "packet=pts,dts,duration,size,pos,flags")
	if err != nil {
		return nil, err
	}
	rows := parseCompact(out)
	pkts := make([]Packet, 0, len(rows))
	noDTS := 0
	for i, r := range rows {
		pts, err1 := strconv.ParseInt(r["pts"], 10, 64)
		dts, err2 := strconv.ParseInt(r["dts"], 10, 64)
		if err1 != nil {
			return nil, fmt.Errorf("ingest: %s: packet %d has no pts (%q)", path, i, r["pts"])
		}
		if err2 != nil { // Matroska, for example, stores no DTS
			noDTS++
		}
		dur, _ := strconv.ParseInt(r["duration"], 10, 64) // "N/A" -> 0
		size, _ := strconv.Atoi(r["size"])
		pos, _ := strconv.ParseInt(r["pos"], 10, 64) // "N/A" -> 0
		pkts = append(pkts, Packet{PTS: pts, DTS: dts, Dur: dur, Size: size, Pos: pos, Key: strings.HasPrefix(r["flags"], "K")})
	}
	if noDTS > 0 {
		// All-or-nothing: a partly known DTS list (some Matroska files) cannot be trusted.
		synthesizeDTS(pkts)
	}
	return pkts, nil
}

// synthesizeDTS fills in decode timestamps for a source that stores only PTS. It
// uses the smallest valid delay: dts[i] = the i-d'th smallest PTS, where d is the
// largest number of frames any frame is displayed before its decode position. That
// keeps dts non-decreasing and dts <= pts, which is all a muxer needs.
func synthesizeDTS(p []Packet) {
	n := len(p)
	idx := make([]int, n)
	for i := range idx {
		idx[i] = i
	}
	sort.Slice(idx, func(a, b int) bool { return p[idx[a]].PTS < p[idx[b]].PTS })
	sorted := make([]int64, n)
	delay := 0
	for rank, i := range idx {
		sorted[rank] = p[i].PTS
		if i-rank > delay {
			delay = i - rank
		}
	}
	gap := int64(1)
	if n > 1 && sorted[1] > sorted[0] {
		gap = sorted[1] - sorted[0]
	}
	for i := range p {
		if i >= delay {
			p[i].DTS = sorted[i-delay]
		} else {
			p[i].DTS = sorted[0] - int64(delay-i)*gap
		}
	}
}

// KeyFrames returns the packets that are random-access points.
func KeyFrames(pkts []Packet) []Packet {
	var k []Packet
	for _, p := range pkts {
		if p.Key {
			k = append(k, p)
		}
	}
	return k
}

// Frames decodes the video through an ffmpeg pipe and sends RGB24 frames in display
// order. The channel is bounded, so a slow consumer pauses ffmpeg instead of filling
// memory. Each frame gets the PTS of the matching packet (packets sorted by PTS), not
// frame_id/fps, so variable frame rate is handled. The channel is closed when done;
// the returned func waits for ffmpeg to exit and reports any error. Cancelling ctx
// stops ffmpeg.
func Frames(ctx context.Context, path string, vi VideoInfo, pkts []Packet, buffer int) (<-chan Frame, func() error) {
	if buffer < 1 {
		buffer = 1
	}
	ptsOrder := make([]int64, len(pkts))
	for i, p := range pkts {
		ptsOrder[i] = p.PTS
	}
	sort.Slice(ptsOrder, func(i, j int) bool { return ptsOrder[i] < ptsOrder[j] })

	ch := make(chan Frame, buffer)
	errc := make(chan error, 1)
	go func() {
		defer close(ch)
		errc <- decode(ctx, path, vi, ptsOrder, ch)
	}()
	return ch, func() error { return <-errc }
}

func decode(ctx context.Context, path string, vi VideoInfo, ptsOrder []int64, ch chan<- Frame) error {
	cmd := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-nostdin",
		"-i", path, "-map", "0:v:0", "-fps_mode", "passthrough",
		"-f", "rawvideo", "-pix_fmt", "rgb24", "pipe:1")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("ingest: start ffmpeg: %w", err)
	}
	br := bufio.NewReaderSize(stdout, 1<<20)
	size := vi.Width * vi.Height * 3
	tb := float64(vi.TimeBaseNum) / float64(vi.TimeBaseDen)

	n := 0
	var readErr error
	for {
		img := make([]byte, size)
		if _, err := io.ReadFull(br, img); err != nil {
			if err != io.EOF {
				readErr = fmt.Errorf("ingest: partial frame %d: %w", n, err)
			}
			break
		}
		if n >= len(ptsOrder) {
			readErr = fmt.Errorf("ingest: ffmpeg produced more frames than the %d packets ffprobe found", len(ptsOrder))
			break
		}
		f := Frame{ID: n, PTS: ptsOrder[n], Time: float64(ptsOrder[n]) * tb, Image: img}
		select {
		case ch <- f:
		case <-ctx.Done():
			readErr = ctx.Err()
		}
		if readErr != nil {
			break
		}
		n++
	}
	if readErr != nil {
		_ = cmd.Process.Kill()
	}
	waitErr := cmd.Wait()
	if readErr != nil {
		return readErr
	}
	if waitErr != nil {
		return fmt.Errorf("ingest: ffmpeg failed: %w: %s", waitErr, strings.TrimSpace(stderr.String()))
	}
	if n != len(ptsOrder) {
		return fmt.Errorf("ingest: decoded %d frames but ffprobe found %d packets", n, len(ptsOrder))
	}
	return nil
}

// runProbe runs ffprobe on the first video stream and returns compact-format output.
func runProbe(ctx context.Context, path, entries string) (string, error) {
	cmd := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-select_streams", "v:0",
		"-show_entries", entries, "-of", "compact=p=0", path)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("ingest: ffprobe %s: %w: %s", path, err, strings.TrimSpace(stderr.String()))
	}
	return string(out), nil
}

// parseCompact parses ffprobe "compact=p=0" lines: key=value|key=value|...
func parseCompact(s string) []map[string]string {
	var rows []map[string]string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		row := map[string]string{}
		for _, kv := range strings.Split(line, "|") {
			if k, v, ok := strings.Cut(kv, "="); ok {
				row[k] = v
			}
		}
		rows = append(rows, row)
	}
	return rows
}

func parseFraction(s string) (num, den int) {
	a, b, ok := strings.Cut(s, "/")
	if !ok {
		return 0, 0
	}
	num, _ = strconv.Atoi(a)
	den, _ = strconv.Atoi(b)
	return num, den
}

// AudioCodec returns the codec of the first audio stream, or "" if there is none.
func AudioCodec(ctx context.Context, path string) (string, error) {
	cmd := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-select_streams", "a:0",
		"-show_entries", "stream=codec_name", "-of", "compact=p=0", path)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("ingest: ffprobe %s: %w", path, err)
	}
	rows := parseCompact(string(out))
	if len(rows) == 0 {
		return "", nil
	}
	return rows[0]["codec_name"], nil
}
