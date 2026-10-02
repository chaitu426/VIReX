package container

import (
	"fmt"
	"hash/crc32"
	"io"
)

// Writer writes a .virex file. The number of sections is declared up front so the
// header and section table can be reserved, then section data is streamed after them.
// The header and table are written last (in Close), once every offset and CRC is known.
type Writer struct {
	ws      io.WriteSeeker
	hdr     Header
	count   int
	entries []SectionEntry
	pos     uint64
	closed  bool
}

// NewWriter starts a file with sectionCount sections. Fields left zero in h
// (FormatMajor/Minor, MinReader*) default to the current format version.
func NewWriter(ws io.WriteSeeker, h Header, sectionCount int) (*Writer, error) {
	if sectionCount < 0 || sectionCount > 0xFFFF {
		return nil, fmt.Errorf("container: invalid section count %d", sectionCount)
	}
	if h.FPSDen == 0 {
		return nil, fmt.Errorf("container: fps denominator must not be 0")
	}
	if h.Timescale == 0 {
		return nil, fmt.Errorf("container: timescale must not be 0")
	}
	if h.FormatMajor == 0 {
		h.FormatMajor, h.FormatMinor = FormatMajor, FormatMinor
	}
	if h.MinReaderMajor == 0 {
		h.MinReaderMajor, h.MinReaderMinor = FormatMajor, 0
	}
	start := uint64(HeaderSize + sectionCount*SectionEntrySize)
	if _, err := ws.Seek(int64(start), io.SeekStart); err != nil {
		return nil, err
	}
	return &Writer{ws: ws, hdr: h, count: sectionCount, pos: start}, nil
}

// AddSection streams r into the file as the next section and records its CRC32.
func (w *Writer) AddSection(t SectionType, layer Layer, flags uint8, r io.Reader) error {
	return w.AddSegment(t, layer, flags, 0, 0, r)
}

// AddSegment is AddSection for a section that covers the time range [start, end].
func (w *Writer) AddSegment(t SectionType, layer Layer, flags uint8, start, end int64, r io.Reader) error {
	if w.closed {
		return fmt.Errorf("container: writer is closed")
	}
	if len(w.entries) >= w.count {
		return ErrSectionCountSkew
	}
	h := crc32.NewIEEE()
	n, err := io.Copy(io.MultiWriter(w.ws, h), r)
	if err != nil {
		return err
	}
	w.entries = append(w.entries, SectionEntry{
		Type: t, Layer: layer, Flags: flags,
		Offset: w.pos, Length: uint64(n), CRC32: h.Sum32(),
		Start: start, End: end,
	})
	w.pos += uint64(n)
	return nil
}

// Close writes the section table and header. It does not close the underlying file.
func (w *Writer) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true
	if len(w.entries) != w.count {
		return ErrSectionCountSkew
	}
	table := make([]byte, w.count*SectionEntrySize)
	for i, e := range w.entries {
		encodeEntry(e, table[i*SectionEntrySize:])
	}
	hdr := encodeHeader(w.hdr, w.count, checksum(table))
	if _, err := w.ws.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if _, err := w.ws.Write(hdr); err != nil {
		return err
	}
	if _, err := w.ws.Write(table); err != nil {
		return err
	}
	_, err := w.ws.Seek(int64(w.pos), io.SeekStart)
	return err
}
