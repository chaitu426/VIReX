package decoder

import (
	"fmt"
	"io"

	"virex/codec/container"
)

// Info is what Decode reports about the file it rebuilt.
type Info struct {
	Header container.Header
	Frames int
	Verify bool
}

// Decode rebuilds an MP4 from an open .virex file. It checks every section's CRC32
// first (unless skipVerify), then copies the pixel stream into MP4 with the original
// timestamps from the temporal index. The semantic section is not needed to play the
// video and is not touched.
func Decode(rd *container.Reader, out io.WriteSeeker, skipVerify bool) (Info, error) {
	h := rd.Header
	if h.PixelCodec != container.CodecH264AnnexB {
		return Info{}, fmt.Errorf("decoder: pixel codec %d is not supported (only H.264 Annex B)", h.PixelCodec)
	}
	px, ok := rd.FindType(container.SectionPixel)
	if !ok {
		return Info{}, fmt.Errorf("decoder: file has no PIXEL section")
	}
	ix, ok := rd.FindType(container.SectionTemporalIndex)
	if !ok {
		return Info{}, fmt.Errorf("decoder: file has no TEMPORAL_INDEX section")
	}
	if !skipVerify {
		for _, e := range []container.SectionEntry{px, ix} {
			if err := rd.Verify(e); err != nil {
				return Info{}, err
			}
		}
	}
	fi, err := rd.OpenFrameIndex(ix)
	if err != nil {
		return Info{}, err
	}
	recs, err := fi.All()
	if err != nil {
		return Info{}, err
	}
	for i, r := range recs {
		if r.Offset+uint64(r.Size) > px.Length {
			return Info{}, fmt.Errorf("decoder: frame %d lies outside the pixel section", i)
		}
	}
	err = WriteMP4(out, MP4Params{
		Width: int(h.Width), Height: int(h.Height), Timescale: h.Timescale, Duration: h.Duration,
	}, recs, rd.Open(px))
	return Info{Header: h, Frames: len(recs), Verify: !skipVerify}, err
}
