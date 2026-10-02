package container

import (
	"fmt"
	"io"
	"sort"
)

// The TEMPORAL_INDEX section is a fixed-record frame table with one record per coded
// frame, stored in DECODE order (the same order the frames sit in the PIXEL section).
// Fixed records let a reader binary search with ReadAt and never load the table whole.
// Layout (little-endian):
//
//	u32 record_size (32) | u32 reserved | u64 count | count x record
//	record: i64 pts | i64 dts | u64 offset (inside PIXEL section) | u32 size | u32 flags
//
// pts and dts are in header Timescale ticks, copied from the source, so a decoder
// can rebuild the original timestamps exactly, including variable frame rate and
// B-frame reordering. dts is non-decreasing; pts is not when B-frames are used.
const (
	frameIndexHeaderSize = 16
	FrameRecordSize      = 32

	FrameKey uint32 = 1 << 0 // random-access point (decoding can start here)

	// MaxReorder bounds how far PTS order can differ from decode order. H.264,
	// H.265 and AV1 allow at most 16 frames of reordering; FrameAt scans that far.
	MaxReorder = 32
)

// FrameRecord locates one coded frame inside the PIXEL section.
type FrameRecord struct {
	PTS    int64
	DTS    int64
	Offset uint64
	Size   uint32
	Flags  uint32
}

func (f FrameRecord) IsKey() bool { return f.Flags&FrameKey != 0 }

// EncodeFrameIndex serialises records, which must be in decode order.
func EncodeFrameIndex(recs []FrameRecord) []byte {
	b := make([]byte, frameIndexHeaderSize+len(recs)*FrameRecordSize)
	le.PutUint32(b[0:], FrameRecordSize)
	le.PutUint64(b[8:], uint64(len(recs)))
	for i, r := range recs {
		p := b[frameIndexHeaderSize+i*FrameRecordSize:]
		le.PutUint64(p[0:], uint64(r.PTS))
		le.PutUint64(p[8:], uint64(r.DTS))
		le.PutUint64(p[16:], r.Offset)
		le.PutUint32(p[24:], r.Size)
		le.PutUint32(p[28:], r.Flags)
	}
	return b
}

// FrameIndex reads the temporal index in place.
type FrameIndex struct {
	r     io.ReaderAt
	recSz int64
	Count int
}

// OpenFrameIndex opens a TEMPORAL_INDEX section for lookups without reading it all.
func (rd *Reader) OpenFrameIndex(e SectionEntry) (*FrameIndex, error) {
	if e.Type != SectionTemporalIndex || e.Length < frameIndexHeaderSize {
		return nil, fmt.Errorf("%w: not a temporal index", ErrCorrupt)
	}
	sr := rd.Open(e)
	hb := make([]byte, frameIndexHeaderSize)
	if _, err := sr.ReadAt(hb, 0); err != nil {
		return nil, err
	}
	recSz := int64(le.Uint32(hb[0:]))
	count := le.Uint64(hb[8:])
	if recSz < FrameRecordSize || count > uint64(e.Length) ||
		frameIndexHeaderSize+int64(count)*recSz > int64(e.Length) {
		return nil, fmt.Errorf("%w: bad temporal index", ErrCorrupt)
	}
	return &FrameIndex{r: sr, recSz: recSz, Count: int(count)}, nil
}

// At returns record i (decode order).
func (x *FrameIndex) At(i int) (FrameRecord, error) {
	if i < 0 || i >= x.Count {
		return FrameRecord{}, fmt.Errorf("container: frame %d out of range", i)
	}
	b := make([]byte, FrameRecordSize)
	if _, err := x.r.ReadAt(b, frameIndexHeaderSize+int64(i)*x.recSz); err != nil {
		return FrameRecord{}, err
	}
	return FrameRecord{
		PTS:    int64(le.Uint64(b[0:])),
		DTS:    int64(le.Uint64(b[8:])),
		Offset: le.Uint64(b[16:]),
		Size:   le.Uint32(b[24:]),
		Flags:  le.Uint32(b[28:]),
	}, nil
}

// All reads every record. Use it when rebuilding a whole file, not for lookups.
func (x *FrameIndex) All() ([]FrameRecord, error) {
	out := make([]FrameRecord, x.Count)
	for i := range out {
		r, err := x.At(i)
		if err != nil {
			return nil, err
		}
		out[i] = r
	}
	return out, nil
}

// FrameAt returns the decode-order index of the frame being shown at time pts: the
// frame with the greatest PTS <= pts. It returns the first frame if pts is earlier
// than all of them. Because PTS >= DTS, only frames with DTS <= pts can qualify, so
// it binary searches on DTS and then scans back at most MaxReorder frames.
func (x *FrameIndex) FrameAt(pts int64) (int, error) {
	var ioErr error
	n := sort.Search(x.Count, func(i int) bool {
		r, err := x.At(i)
		if err != nil && ioErr == nil {
			ioErr = err
		}
		return r.DTS > pts
	})
	if ioErr != nil {
		return 0, ioErr
	}
	best, bestPTS := -1, int64(0)
	for i := n - 1; i >= 0 && i >= n-MaxReorder; i-- {
		r, err := x.At(i)
		if err != nil {
			return 0, err
		}
		if r.PTS <= pts && (best < 0 || r.PTS > bestPTS) {
			best, bestPTS = i, r.PTS
		}
	}
	if best < 0 {
		return 0, nil
	}
	return best, nil
}

// SeekKey returns the decode-order index of the nearest key frame at or before
// frame i: where decoding must start to display frame i.
func (x *FrameIndex) SeekKey(i int) (int, error) {
	for ; i > 0; i-- {
		r, err := x.At(i)
		if err != nil {
			return 0, err
		}
		if r.IsKey() {
			return i, nil
		}
	}
	return 0, nil
}
