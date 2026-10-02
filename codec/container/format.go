package container

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
)

// Format constants. See docs/virex-format-v0.1.md.
const (
	Magic = "VIRX"

	FormatMajor = 1
	FormatMinor = 0

	HeaderSize       = 72
	SectionEntrySize = 40
)

// SectionType identifies what a section holds.
type SectionType uint16

const (
	SectionPixel         SectionType = 1
	SectionSemantic      SectionType = 2
	SectionTemporalIndex SectionType = 3
	SectionMetadata      SectionType = 4
	SectionAudio         SectionType = 5 // reserved: lectures carry speech (ASR later)
)

func (t SectionType) String() string {
	switch t {
	case SectionPixel:
		return "PIXEL"
	case SectionSemantic:
		return "SEMANTIC"
	case SectionTemporalIndex:
		return "TEMPORAL_INDEX"
	case SectionMetadata:
		return "METADATA"
	case SectionAudio:
		return "AUDIO"
	}
	return fmt.Sprintf("UNKNOWN(%d)", uint16(t))
}

// Layer is the SVIR layer a section carries. Values 1-4 match the Layer enum in svir.proto.
type Layer uint8

const (
	LayerNone      Layer = 0
	LayerEntity    Layer = 1
	LayerEvent     Layer = 2
	LayerText      Layer = 3
	LayerEmbedding Layer = 4
	LayerAll       Layer = 255
)

// Section flag bits.
const (
	FlagZstd uint8 = 1 << 0
)

// Pixel codecs.
const (
	CodecH264AnnexB uint16 = 1
	CodecH265AnnexB uint16 = 2
	CodecAV1OBU     uint16 = 3 // AV1 low-overhead OBU stream (ffmpeg -f obu)
)

// Header holds the stream-level facts stored at the start of the file.
type Header struct {
	FormatMajor    uint16
	FormatMinor    uint16
	MinReaderMajor uint16
	MinReaderMinor uint16
	Flags          uint32
	Width          uint32
	Height         uint32
	FPSNum         uint32
	FPSDen         uint32
	Timescale      uint32 // ticks per second; every timestamp in the file is in these ticks
	Duration       uint64 // in ticks
	PixelCodec     uint16
	SchemaMajor    uint16
	SchemaMinor    uint16
}

// SectionEntry is one row of the section table.
type SectionEntry struct {
	Type   SectionType
	Layer  Layer
	Flags  uint8
	Offset uint64
	Length uint64
	CRC32  uint32
	// Start/End give the time range (ticks) a section covers, so a semantic stream
	// can be split into time segments. 0,0 means "whole stream". For an AUDIO
	// section, Start is the timestamp of its first sample.
	Start int64
	End   int64
}

var (
	ErrBadMagic         = errors.New("container: not a .virex file (bad magic)")
	ErrHeaderCRC        = errors.New("container: header checksum mismatch")
	ErrTableCRC         = errors.New("container: section table checksum mismatch")
	ErrSectionCRC       = errors.New("container: section checksum mismatch")
	ErrReaderTooOld     = errors.New("container: file needs a newer reader")
	ErrCorrupt          = errors.New("container: corrupt or truncated file")
	ErrSectionNotFound  = errors.New("container: section not found")
	ErrSectionCountSkew = errors.New("container: number of sections written does not match the declared count")
)

var le = binary.LittleEndian

func checksum(b []byte) uint32 { return crc32.ChecksumIEEE(b) }

// encodeHeader returns the 64-byte header. tableCRC covers the encoded section table.
func encodeHeader(h Header, sectionCount int, tableCRC uint32) []byte {
	b := make([]byte, HeaderSize)
	copy(b[0:4], Magic)
	le.PutUint16(b[4:], h.FormatMajor)
	le.PutUint16(b[6:], h.FormatMinor)
	le.PutUint16(b[8:], h.MinReaderMajor)
	le.PutUint16(b[10:], h.MinReaderMinor)
	le.PutUint32(b[12:], HeaderSize)
	le.PutUint32(b[16:], h.Flags)
	le.PutUint32(b[20:], h.Width)
	le.PutUint32(b[24:], h.Height)
	le.PutUint32(b[28:], h.FPSNum)
	le.PutUint32(b[32:], h.FPSDen)
	le.PutUint32(b[36:], h.Timescale)
	le.PutUint64(b[40:], h.Duration)
	le.PutUint16(b[48:], h.PixelCodec)
	le.PutUint16(b[50:], h.SchemaMajor)
	le.PutUint16(b[52:], h.SchemaMinor)
	le.PutUint16(b[54:], uint16(sectionCount))
	le.PutUint16(b[56:], SectionEntrySize)
	// b[58:64] reserved
	le.PutUint32(b[64:], tableCRC)
	le.PutUint32(b[68:], checksum(b[:68]))
	return b
}

func encodeEntry(e SectionEntry, b []byte) {
	le.PutUint16(b[0:], uint16(e.Type))
	b[2] = byte(e.Layer)
	b[3] = e.Flags
	le.PutUint64(b[4:], e.Offset)
	le.PutUint64(b[12:], e.Length)
	le.PutUint32(b[20:], e.CRC32)
	le.PutUint64(b[24:], uint64(e.Start))
	le.PutUint64(b[32:], uint64(e.End))
}

func decodeEntry(b []byte) SectionEntry {
	return SectionEntry{
		Type:   SectionType(le.Uint16(b[0:])),
		Layer:  Layer(b[2]),
		Flags:  b[3],
		Offset: le.Uint64(b[4:]),
		Length: le.Uint64(b[12:]),
		CRC32:  le.Uint32(b[20:]),

		Start: int64(le.Uint64(b[24:])),
		End:   int64(le.Uint64(b[32:])),
	}
}
