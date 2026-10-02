package decoder

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"

	"virex/codec/container"
)

// MP4Params describe the video track.
type MP4Params struct {
	Width, Height int
	Timescale     uint32 // ticks per second; also used as the movie timescale
	Duration      uint64 // presentation end in ticks (largest pts + duration)
}

var be = binary.BigEndian

// WriteMP4 builds a playable MP4 from an H.264 Annex B stream and its frame index. It
// copies the coded frames unchanged (no re-encoding), and rebuilds the original
// timestamps exactly: DTS goes into stts, the PTS-DTS offset into ctts, key frames
// into stss and any start offset into an edit list.
//
// Limits (all hold for streams made by virex-encode): 8-bit 4:2:0, one set of
// SPS/PPS for the whole stream, one video track, no audio.
func WriteMP4(w io.WriteSeeker, p MP4Params, recs []container.FrameRecord, pixel io.ReaderAt) error {
	if len(recs) == 0 {
		return fmt.Errorf("decoder: no frames")
	}
	if p.Timescale == 0 || p.Width <= 0 || p.Height <= 0 {
		return fmt.Errorf("decoder: bad video parameters %+v", p)
	}

	// ftyp
	if _, err := w.Write(box("ftyp", []byte("isom"), u32(512), []byte("isom"), []byte("iso2"), []byte("avc1"), []byte("mp41"))); err != nil {
		return err
	}
	// mdat with a 64-bit size field, patched once the size is known.
	mdatStart, _ := w.Seek(0, io.SeekCurrent)
	if _, err := w.Write(append(append(u32(1), "mdat"...), make([]byte, 8)...)); err != nil {
		return err
	}
	dataStart := mdatStart + 16

	var sps, pps []byte
	sizes := make([]uint32, len(recs))
	var written int64
	for i, r := range recs {
		au := make([]byte, r.Size)
		if _, err := pixel.ReadAt(au, int64(r.Offset)); err != nil {
			return fmt.Errorf("decoder: frame %d: %w", i, err)
		}
		var sample bytes.Buffer
		for _, nal := range splitNALs(au) {
			switch nal[0] & 0x1F {
			case 9: // access unit delimiter: not stored in MP4
			case 7:
				if sps == nil {
					sps = append([]byte(nil), nal...)
				} else if !bytes.Equal(sps, nal) {
					return fmt.Errorf("decoder: frame %d: SPS changes mid-stream, not supported", i)
				}
			case 8:
				if pps == nil {
					pps = append([]byte(nil), nal...)
				} else if !bytes.Equal(pps, nal) {
					return fmt.Errorf("decoder: frame %d: PPS changes mid-stream, not supported", i)
				}
			default:
				sample.Write(u32(uint32(len(nal))))
				sample.Write(nal)
			}
		}
		if sample.Len() == 0 {
			return fmt.Errorf("decoder: frame %d has no picture data", i)
		}
		n, err := w.Write(sample.Bytes())
		if err != nil {
			return err
		}
		sizes[i] = uint32(n)
		written += int64(n)
	}
	if sps == nil || pps == nil {
		return fmt.Errorf("decoder: stream has no SPS/PPS")
	}
	// Patch the mdat size, then append moov.
	end, _ := w.Seek(0, io.SeekCurrent)
	if _, err := w.Seek(mdatStart+8, io.SeekStart); err != nil {
		return err
	}
	if _, err := w.Write(u64(uint64(written) + 16)); err != nil {
		return err
	}
	if _, err := w.Seek(end, io.SeekStart); err != nil {
		return err
	}
	_, err := w.Write(moov(p, recs, sizes, sps, pps, uint64(dataStart)))
	return err
}

// splitNALs returns the NAL units (without start codes) of one Annex B access unit.
func splitNALs(au []byte) [][]byte {
	var starts []int // index of the first byte after each start code
	for i := 0; i+2 < len(au); i++ {
		if au[i] == 0 && au[i+1] == 0 && au[i+2] == 1 {
			starts = append(starts, i+3)
			i += 2
		}
	}
	var out [][]byte
	for k, s := range starts {
		end := len(au)
		if k+1 < len(starts) {
			end = starts[k+1] - 3
		}
		// A zero byte before the next start code belongs to the (four-byte) start code
		// or is trailing_zero_8bits; neither is part of the NAL.
		for end > s && au[end-1] == 0 {
			end--
		}
		if end > s {
			out = append(out, au[s:end])
		}
	}
	return out
}

func moov(p MP4Params, recs []container.FrameRecord, sizes []uint32, sps, pps []byte, dataStart uint64) []byte {
	n := len(recs)
	dts0 := recs[0].DTS
	minPTS, maxPTS := recs[0].PTS, recs[0].PTS
	for _, r := range recs {
		if r.PTS < minPTS {
			minPTS = r.PTS
		}
		if r.PTS > maxPTS {
			maxPTS = r.PTS
		}
	}

	// stts: durations from DTS differences, run-length encoded. The last frame lasts
	// until the presentation end (or repeats the previous duration if that is unknown).
	durs := make([]uint32, n)
	for i := 0; i+1 < n; i++ {
		durs[i] = uint32(recs[i+1].DTS - recs[i].DTS)
	}
	last := int64(p.Duration) - maxPTS
	if last <= 0 {
		last = 1
		if n > 1 {
			last = int64(durs[n-2])
		}
	}
	durs[n-1] = uint32(last)
	var mediaDur uint64
	var stts bytes.Buffer
	var runs uint32
	for i := 0; i < n; {
		j := i
		for j < n && durs[j] == durs[i] {
			j++
		}
		stts.Write(u32(uint32(j - i)))
		stts.Write(u32(durs[i]))
		runs++
		mediaDur += uint64(j-i) * uint64(durs[i])
		i = j
	}

	// ctts (version 0): PTS - DTS, always >= 0 for our streams.
	var ctts bytes.Buffer
	var cruns uint32
	for i := 0; i < n; {
		off := uint32(recs[i].PTS - recs[i].DTS)
		j := i
		for j < n && uint32(recs[j].PTS-recs[j].DTS) == off {
			j++
		}
		ctts.Write(u32(uint32(j - i)))
		ctts.Write(u32(off))
		cruns++
		i = j
	}

	var stss bytes.Buffer
	var keys uint32
	for i, r := range recs {
		if r.IsKey() {
			stss.Write(u32(uint32(i + 1)))
			keys++
		}
	}
	stsz := new(bytes.Buffer)
	for _, s := range sizes {
		stsz.Write(u32(s))
	}

	// Media time starts at DTS0 = 0 after shifting; the first shown frame is at
	// minPTS - DTS0. An empty edit puts it at presentation time minPTS.
	mediaTime := minPTS - dts0
	presEnd := int64(p.Duration)
	if presEnd < maxPTS+1 {
		presEnd = maxPTS + 1
	}
	var edits [][3]int64 // {segment_duration, media_time, rate}
	if minPTS > 0 {
		edits = append(edits, [3]int64{minPTS, -1, 1})
	}
	edits = append(edits, [3]int64{presEnd - minPTS, mediaTime, 1})
	elst := new(bytes.Buffer)
	elst.Write(fullBox(1, 0))
	elst.Write(u32(uint32(len(edits))))
	for _, e := range edits {
		elst.Write(u64(uint64(e[0])))
		elst.Write(u64(uint64(e[1])))
		elst.Write(u16(1))
		elst.Write(u16(0))
	}

	avcC := new(bytes.Buffer)
	avcC.Write([]byte{1, sps[1], sps[2], sps[3], 0xFF, 0xE1})
	avcC.Write(u16(uint16(len(sps))))
	avcC.Write(sps)
	avcC.WriteByte(1)
	avcC.Write(u16(uint16(len(pps))))
	avcC.Write(pps)
	switch sps[1] { // High-family profiles carry chroma and bit depth in avcC
	case 100, 110, 122, 244, 44, 83, 86, 118, 128, 138, 139, 134, 135:
		avcC.Write([]byte{0xFC | 1, 0xF8 | 0, 0xF8 | 0, 0}) // 4:2:0, 8-bit luma and chroma, no SPS extensions
	}

	avc1 := new(bytes.Buffer)
	avc1.Write(make([]byte, 6))
	avc1.Write(u16(1)) // data reference index
	avc1.Write(make([]byte, 16))
	avc1.Write(u16(uint16(p.Width)))
	avc1.Write(u16(uint16(p.Height)))
	avc1.Write(u32(0x00480000))
	avc1.Write(u32(0x00480000))
	avc1.Write(u32(0))
	avc1.Write(u16(1))
	avc1.Write(make([]byte, 32)) // compressor name
	avc1.Write(u16(0x18))
	avc1.Write(u16(0xFFFF))
	avc1.Write(box("avcC", avcC.Bytes()))

	co64 := new(bytes.Buffer)
	co64.Write(fullBox(0, 0))
	co64.Write(u32(1))
	co64.Write(u64(dataStart))

	stbl := box("stbl",
		box("stsd", fullBox(0, 0), u32(1), box("avc1", avc1.Bytes())),
		box("stts", fullBox(0, 0), u32(runs), stts.Bytes()),
		box("ctts", fullBox(0, 0), u32(cruns), ctts.Bytes()),
		box("stss", fullBox(0, 0), u32(keys), stss.Bytes()),
		box("stsz", fullBox(0, 0), u32(0), u32(uint32(n)), stsz.Bytes()),
		box("stsc", fullBox(0, 0), u32(1), u32(1), u32(uint32(n)), u32(1)),
		box("co64", co64.Bytes()),
	)

	matrix := []byte{0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x40, 0, 0, 0}
	mvhd := box("mvhd", fullBox(1, 0), u64(0), u64(0), u32(p.Timescale), u64(uint64(presEnd)),
		u32(0x00010000), u16(0x0100), make([]byte, 10), matrix, make([]byte, 24), u32(2))
	tkhd := box("tkhd", fullBox(1, 3), u64(0), u64(0), u32(1), u32(0), u64(uint64(presEnd)),
		make([]byte, 8), u16(0), u16(0), u16(0), u16(0), matrix,
		u32(uint32(p.Width)<<16), u32(uint32(p.Height)<<16))
	mdhd := box("mdhd", fullBox(1, 0), u64(0), u64(0), u32(p.Timescale), u64(mediaDur), u16(0x55C4), u16(0))
	hdlr := box("hdlr", fullBox(0, 0), u32(0), []byte("vide"), make([]byte, 12), []byte("VideoHandler\x00"))
	minf := box("minf",
		box("vmhd", fullBox(0, 1), make([]byte, 8)),
		box("dinf", box("dref", fullBox(0, 0), u32(1), box("url ", fullBox(0, 1)))),
		stbl)
	trak := box("trak", tkhd, box("edts", box("elst", elst.Bytes())), box("mdia", mdhd, hdlr, minf))
	return box("moov", mvhd, trak)
}

func box(typ string, parts ...[]byte) []byte {
	size := 8
	for _, p := range parts {
		size += len(p)
	}
	b := make([]byte, 0, size)
	b = append(b, u32(uint32(size))...)
	b = append(b, typ...)
	for _, p := range parts {
		b = append(b, p...)
	}
	return b
}

func fullBox(version byte, flags uint32) []byte {
	return []byte{version, byte(flags >> 16), byte(flags >> 8), byte(flags)}
}

func u16(v uint16) []byte { b := make([]byte, 2); be.PutUint16(b, v); return b }
func u32(v uint32) []byte { b := make([]byte, 4); be.PutUint32(b, v); return b }
func u64(v uint64) []byte { b := make([]byte, 8); be.PutUint64(b, v); return b }
