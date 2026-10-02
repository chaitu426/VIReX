package container

import (
	"bytes"
	"fmt"
	"hash/crc32"
	"io"
	"sort"
)

// Reader reads a .virex file through io.ReaderAt, so it can jump to any section
// without reading the others.
type Reader struct {
	r        io.ReaderAt
	size     int64
	Header   Header
	Sections []SectionEntry
}

// NewReader parses and verifies the header and section table. Section data is not read.
func NewReader(r io.ReaderAt, size int64) (*Reader, error) {
	if size < HeaderSize {
		return nil, ErrCorrupt
	}
	hb := make([]byte, HeaderSize)
	if _, err := r.ReadAt(hb, 0); err != nil {
		return nil, err
	}
	if string(hb[0:4]) != Magic {
		return nil, ErrBadMagic
	}
	if checksum(hb[:68]) != le.Uint32(hb[68:]) {
		return nil, ErrHeaderCRC
	}
	h := Header{
		FormatMajor:    le.Uint16(hb[4:]),
		FormatMinor:    le.Uint16(hb[6:]),
		MinReaderMajor: le.Uint16(hb[8:]),
		MinReaderMinor: le.Uint16(hb[10:]),
		Flags:          le.Uint32(hb[16:]),
		Width:          le.Uint32(hb[20:]),
		Height:         le.Uint32(hb[24:]),
		FPSNum:         le.Uint32(hb[28:]),
		FPSDen:         le.Uint32(hb[32:]),
		Timescale:      le.Uint32(hb[36:]),
		Duration:       le.Uint64(hb[40:]),
		PixelCodec:     le.Uint16(hb[48:]),
		SchemaMajor:    le.Uint16(hb[50:]),
		SchemaMinor:    le.Uint16(hb[52:]),
	}
	if h.MinReaderMajor > FormatMajor || (h.MinReaderMajor == FormatMajor && h.MinReaderMinor > FormatMinor) {
		return nil, fmt.Errorf("%w: needs reader %d.%d, this is %d.%d",
			ErrReaderTooOld, h.MinReaderMajor, h.MinReaderMinor, FormatMajor, FormatMinor)
	}

	headerSize := int64(le.Uint32(hb[12:]))
	count := int(le.Uint16(hb[54:]))
	entrySize := int(le.Uint16(hb[56:]))
	if headerSize < HeaderSize || entrySize < SectionEntrySize {
		return nil, ErrCorrupt
	}
	tableLen := int64(count) * int64(entrySize)
	if headerSize+tableLen > size {
		return nil, ErrCorrupt
	}
	tb := make([]byte, tableLen)
	if _, err := r.ReadAt(tb, headerSize); err != nil {
		return nil, err
	}
	if checksum(tb) != le.Uint32(hb[64:]) {
		return nil, ErrTableCRC
	}
	entries := make([]SectionEntry, count)
	for i := range entries {
		e := decodeEntry(tb[i*entrySize:])
		if e.Offset > uint64(size) || e.Length > uint64(size)-e.Offset {
			return nil, fmt.Errorf("%w: section %d lies outside the file", ErrCorrupt, i)
		}
		entries[i] = e
	}
	return &Reader{r: r, size: size, Header: h, Sections: entries}, nil
}

// Find returns the first section with the given type and layer.
func (rd *Reader) Find(t SectionType, layer Layer) (SectionEntry, bool) {
	for _, e := range rd.Sections {
		if e.Type == t && e.Layer == layer {
			return e, true
		}
	}
	return SectionEntry{}, false
}

// FindRange returns, in time order, every section of the type and layer whose
// time range overlaps [start, end]. A section with range 0,0 covers everything.
func (rd *Reader) FindRange(t SectionType, layer Layer, start, end int64) []SectionEntry {
	var out []SectionEntry
	for _, e := range rd.Sections {
		if e.Type != t || e.Layer != layer {
			continue
		}
		whole := e.Start == 0 && e.End == 0
		if whole || (e.Start <= end && e.End >= start) {
			out = append(out, e)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Start < out[j].Start })
	return out
}

// FindType returns the first section of a type, any layer.
func (rd *Reader) FindType(t SectionType) (SectionEntry, bool) {
	for _, e := range rd.Sections {
		if e.Type == t {
			return e, true
		}
	}
	return SectionEntry{}, false
}

// Open returns a reader over a section's bytes without verifying its CRC.
func (rd *Reader) Open(e SectionEntry) *io.SectionReader {
	return io.NewSectionReader(rd.r, int64(e.Offset), int64(e.Length))
}

// Verify streams a section and checks its CRC32.
func (rd *Reader) Verify(e SectionEntry) error {
	h := crc32.NewIEEE()
	if _, err := io.Copy(h, rd.Open(e)); err != nil {
		return err
	}
	if h.Sum32() != e.CRC32 {
		return fmt.Errorf("%w: %s", ErrSectionCRC, e.Type)
	}
	return nil
}

// ReadSection reads a whole section into memory and verifies its CRC32.
func (rd *Reader) ReadSection(e SectionEntry) ([]byte, error) {
	var buf bytes.Buffer
	buf.Grow(int(e.Length))
	if _, err := io.Copy(&buf, rd.Open(e)); err != nil {
		return nil, err
	}
	if checksum(buf.Bytes()) != e.CRC32 {
		return nil, fmt.Errorf("%w: %s", ErrSectionCRC, e.Type)
	}
	return buf.Bytes(), nil
}

// VerifyAll checks the CRC32 of every section.
func (rd *Reader) VerifyAll() error {
	for _, e := range rd.Sections {
		if err := rd.Verify(e); err != nil {
			return err
		}
	}
	return nil
}
