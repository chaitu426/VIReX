package container

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

var testHeader = Header{
	Width: 1280, Height: 720, FPSNum: 30000, FPSDen: 1001,
	Timescale: 30000, Duration: 375_000, PixelCodec: CodecH264AnnexB, SchemaMajor: 0, SchemaMinor: 1,
}

type sec struct {
	t     SectionType
	layer Layer
	data  []byte
}

// writeFile writes the sections to a temp file and returns its bytes.
func writeFile(t *testing.T, h Header, secs []sec) []byte {
	t.Helper()
	path := filepath.Join(t.TempDir(), "t.virex")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w, err := NewWriter(f, h, len(secs))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range secs {
		if err := w.AddSection(s.t, s.layer, 0, bytes.NewReader(s.data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func open(t *testing.T, b []byte) (*Reader, error) {
	t.Helper()
	return NewReader(bytes.NewReader(b), int64(len(b)))
}

func standardSections() []sec {
	return []sec{
		{SectionPixel, LayerNone, bytes.Repeat([]byte{0, 0, 0, 1, 0x65}, 5000)},
		{SectionSemantic, LayerAll, []byte("svir-bytes")},
		{SectionMetadata, LayerNone, []byte(`{"source":"a.mp4"}`)},
	}
}

func TestRoundTrip(t *testing.T) {
	secs := standardSections()
	rd, err := open(t, writeFile(t, testHeader, secs))
	if err != nil {
		t.Fatal(err)
	}
	h := rd.Header
	if h.Width != 1280 || h.Height != 720 || h.FPSNum != 30000 || h.FPSDen != 1001 ||
		h.Timescale != 30000 || h.Duration != 375_000 || h.PixelCodec != CodecH264AnnexB || h.SchemaMinor != 1 {
		t.Fatalf("header mismatch: %+v", h)
	}
	if h.FormatMajor != FormatMajor || h.FormatMinor != FormatMinor {
		t.Fatalf("version not defaulted: %+v", h)
	}
	if len(rd.Sections) != len(secs) {
		t.Fatalf("got %d sections", len(rd.Sections))
	}
	for _, s := range secs {
		e, ok := rd.Find(s.t, s.layer)
		if !ok {
			t.Fatalf("missing %s", s.t)
		}
		got, err := rd.ReadSection(e)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, s.data) {
			t.Fatalf("%s data differs", s.t)
		}
	}
	if err := rd.VerifyAll(); err != nil {
		t.Fatal(err)
	}
}

func TestEmptySemanticSection(t *testing.T) {
	rd, err := open(t, writeFile(t, testHeader, []sec{
		{SectionPixel, LayerNone, []byte("px")},
		{SectionSemantic, LayerAll, nil},
	}))
	if err != nil {
		t.Fatal(err)
	}
	e, ok := rd.FindType(SectionSemantic)
	if !ok || e.Length != 0 {
		t.Fatalf("expected empty semantic section, got %+v ok=%v", e, ok)
	}
	if err := rd.VerifyAll(); err != nil {
		t.Fatal(err)
	}
}

// countingReaderAt records how many bytes were read, to prove random access.
type countingReaderAt struct {
	r io.ReaderAt
	n int64
}

func (c *countingReaderAt) ReadAt(p []byte, off int64) (int, error) {
	n, err := c.r.ReadAt(p, off)
	c.n += int64(n)
	return n, err
}

func TestJumpToSemanticWithoutReadingPixels(t *testing.T) {
	b := writeFile(t, testHeader, standardSections())
	c := &countingReaderAt{r: bytes.NewReader(b)}
	rd, err := NewReader(c, int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	e, _ := rd.Find(SectionSemantic, LayerAll)
	if _, err := rd.ReadSection(e); err != nil {
		t.Fatal(err)
	}
	pixel, _ := rd.FindType(SectionPixel)
	if c.n >= int64(pixel.Length) {
		t.Fatalf("read %d bytes, pixel section alone is %d", c.n, pixel.Length)
	}
}

func TestDetectsCorruption(t *testing.T) {
	good := writeFile(t, testHeader, standardSections())
	rd, _ := open(t, good)
	pixel, _ := rd.FindType(SectionPixel)
	sem, _ := rd.FindType(SectionSemantic)

	tests := []struct {
		name string
		pos  int
		want error
	}{
		{"bad magic", 0, ErrBadMagic},
		{"header field", 20, ErrHeaderCRC},
		{"table entry", HeaderSize + 4, ErrTableCRC},
		{"pixel data", int(pixel.Offset) + 10, ErrSectionCRC},
		{"semantic data", int(sem.Offset), ErrSectionCRC},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := append([]byte(nil), good...)
			b[tc.pos] ^= 0xFF
			r, err := open(t, b)
			if tc.want != ErrSectionCRC {
				if !errors.Is(err, tc.want) {
					t.Fatalf("got %v, want %v", err, tc.want)
				}
				return
			}
			if err != nil {
				t.Fatalf("open failed: %v", err)
			}
			if err := r.VerifyAll(); !errors.Is(err, ErrSectionCRC) {
				t.Fatalf("VerifyAll got %v, want %v", err, ErrSectionCRC)
			}
		})
	}
}

func TestTruncatedFile(t *testing.T) {
	good := writeFile(t, testHeader, standardSections())
	if _, err := open(t, good[:HeaderSize+10]); err == nil {
		t.Fatal("expected error for truncated table")
	}
	if _, err := open(t, good[:10]); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("got %v", err)
	}
	// Table intact but data cut short: sections point outside the file.
	if _, err := open(t, good[:len(good)-5]); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("got %v", err)
	}
}

func TestUnknownSectionIsSkippable(t *testing.T) {
	rd, err := open(t, writeFile(t, testHeader, []sec{
		{SectionType(0x9001), LayerNone, []byte("from the future")},
		{SectionPixel, LayerNone, []byte("px")},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := rd.FindType(SectionPixel); !ok {
		t.Fatal("pixel section not found past an unknown section")
	}
}

func TestPerLayerSemanticSections(t *testing.T) {
	rd, err := open(t, writeFile(t, testHeader, []sec{
		{SectionPixel, LayerNone, []byte("px")},
		{SectionSemantic, LayerEntity, []byte("L0")},
		{SectionSemantic, LayerEvent, []byte("L1")},
		{SectionSemantic, LayerText, []byte("L2")},
	}))
	if err != nil {
		t.Fatal(err)
	}
	for layer, want := range map[Layer]string{LayerEntity: "L0", LayerEvent: "L1", LayerText: "L2"} {
		e, ok := rd.Find(SectionSemantic, layer)
		if !ok {
			t.Fatalf("layer %d missing", layer)
		}
		got, err := rd.ReadSection(e)
		if err != nil || string(got) != want {
			t.Fatalf("layer %d: got %q err=%v", layer, got, err)
		}
	}
	if _, ok := rd.Find(SectionSemantic, LayerEmbedding); ok {
		t.Fatal("L3 should not exist")
	}
}

func TestReaderTooOld(t *testing.T) {
	h := testHeader
	h.FormatMajor, h.FormatMinor = 2, 0
	h.MinReaderMajor, h.MinReaderMinor = 2, 0
	_, err := open(t, writeFile(t, h, standardSections()))
	if !errors.Is(err, ErrReaderTooOld) {
		t.Fatalf("got %v", err)
	}
}

func TestNewerMinorWithOldMinReaderIsReadable(t *testing.T) {
	h := testHeader
	h.FormatMajor, h.FormatMinor = 1, 5
	h.MinReaderMajor, h.MinReaderMinor = 1, 0
	if _, err := open(t, writeFile(t, h, standardSections())); err != nil {
		t.Fatal(err)
	}
}

func TestWriterRejectsCountMismatch(t *testing.T) {
	f, _ := os.Create(filepath.Join(t.TempDir(), "x.virex"))
	defer f.Close()
	w, err := NewWriter(f, testHeader, 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.AddSection(SectionPixel, LayerNone, 0, bytes.NewReader([]byte("a"))); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); !errors.Is(err, ErrSectionCountSkew) {
		t.Fatalf("got %v", err)
	}

	f2, _ := os.Create(filepath.Join(t.TempDir(), "y.virex"))
	defer f2.Close()
	w2, _ := NewWriter(f2, testHeader, 1)
	_ = w2.AddSection(SectionPixel, LayerNone, 0, bytes.NewReader(nil))
	if err := w2.AddSection(SectionSemantic, LayerAll, 0, bytes.NewReader(nil)); !errors.Is(err, ErrSectionCountSkew) {
		t.Fatalf("got %v", err)
	}
}

func TestWriterRejectsZeroFPSDen(t *testing.T) {
	f, _ := os.Create(filepath.Join(t.TempDir(), "z.virex"))
	defer f.Close()
	h := testHeader
	h.FPSDen = 0
	if _, err := NewWriter(f, h, 1); err == nil {
		t.Fatal("expected error")
	}
}

func indexed(t *testing.T, recs []FrameRecord) *FrameIndex {
	t.Helper()
	rd, err := open(t, writeFile(t, testHeader, []sec{
		{SectionPixel, LayerNone, make([]byte, 4000)},
		{SectionTemporalIndex, LayerNone, EncodeFrameIndex(recs)},
	}))
	if err != nil {
		t.Fatal(err)
	}
	e, _ := rd.FindType(SectionTemporalIndex)
	x, err := rd.OpenFrameIndex(e)
	if err != nil || x.Count != len(recs) {
		t.Fatalf("open: %v", err)
	}
	return x
}

// bruteFrameAt is the definition FrameAt must match.
func bruteFrameAt(recs []FrameRecord, pts int64) int {
	best := -1
	for i, r := range recs {
		if r.PTS <= pts && (best < 0 || r.PTS > recs[best].PTS) {
			best = i
		}
	}
	if best < 0 {
		return 0
	}
	return best
}

func TestFrameIndexVFR(t *testing.T) {
	// Key frame every 5 frames, with a variable-frame-rate gap after frame 12.
	var recs []FrameRecord
	pts := int64(0)
	for i := 0; i < 20; i++ {
		fl := uint32(0)
		if i%5 == 0 {
			fl = FrameKey
		}
		recs = append(recs, FrameRecord{PTS: pts, DTS: pts, Offset: uint64(i * 100), Size: 100, Flags: fl})
		pts += 1001
		if i == 12 {
			pts += 2500
		}
	}
	x := indexed(t, recs)
	for _, p := range []int64{-5, 0, 1000, 1001, 7007, 13013, 14000, 17000, 1 << 40} {
		i, err := x.FrameAt(p)
		if err != nil || i != bruteFrameAt(recs, p) {
			t.Fatalf("FrameAt(%d) = %d err=%v, want %d", p, i, err, bruteFrameAt(recs, p))
		}
		k, _ := x.SeekKey(i)
		if r, _ := x.At(k); !r.IsKey() || k > i || i-k >= 5 {
			t.Fatalf("SeekKey(%d) = %d", i, k)
		}
	}
}

func TestFrameIndexBFrames(t *testing.T) {
	// Decode order I P B B P B B ... : PTS is not monotonic, DTS is.
	// Display order position -> decode order: I0 B2 B3 P1 ... modelled as (pts,dts) pairs.
	pairs := [][2]int64{{0, 0}, {3, 1}, {1, 2}, {2, 3}, {6, 4}, {4, 5}, {5, 6}, {9, 7}, {7, 8}, {8, 9}}
	var recs []FrameRecord
	for i, p := range pairs {
		fl := uint32(0)
		if i == 0 {
			fl = FrameKey
		}
		recs = append(recs, FrameRecord{PTS: p[0] * 1001, DTS: p[1]*1001 - 2002, Offset: uint64(i * 100), Size: 100, Flags: fl})
	}
	x := indexed(t, recs)
	for p := int64(-1); p < 10_500; p += 137 {
		i, err := x.FrameAt(p)
		if err != nil || i != bruteFrameAt(recs, p) {
			t.Fatalf("FrameAt(%d) = %d err=%v, want %d", p, i, err, bruteFrameAt(recs, p))
		}
	}
	got, err := x.All()
	if err != nil || len(got) != len(recs) {
		t.Fatal(err)
	}
	for i := range recs {
		if got[i] != recs[i] {
			t.Fatalf("record %d: %+v != %+v (timestamps must round-trip exactly)", i, got[i], recs[i])
		}
	}
}

func TestSegmentedSemanticByTime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.virex")
	f, _ := os.Create(path)
	w, _ := NewWriter(f, testHeader, 4)
	_ = w.AddSection(SectionPixel, LayerNone, 0, bytes.NewReader([]byte("px")))
	for i, name := range []string{"a", "b", "c"} {
		s := int64(i) * 10_000_000
		_ = w.AddSegment(SectionSemantic, LayerEvent, 0, s, s+10_000_000, bytes.NewReader([]byte(name)))
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
	b, _ := os.ReadFile(path)
	rd, err := open(t, b)
	if err != nil {
		t.Fatal(err)
	}
	got := rd.FindRange(SectionSemantic, LayerEvent, 12_000_000, 25_000_000)
	if len(got) != 2 {
		t.Fatalf("want 2 segments, got %d", len(got))
	}
	d0, _ := rd.ReadSection(got[0])
	d1, _ := rd.ReadSection(got[1])
	if string(d0) != "b" || string(d1) != "c" {
		t.Fatalf("got %q %q", d0, d1)
	}
	if n := len(rd.FindRange(SectionSemantic, LayerEvent, 40_000_000, 50_000_000)); n != 0 {
		t.Fatalf("expected none, got %d", n)
	}
}
