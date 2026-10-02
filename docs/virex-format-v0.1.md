# `.virex` Container Format — v0.1 (frozen for Phase 1)

Source: `research/chaitanya/research/2026-9-30-structure.md`. This file turns that research into exact byte-level decisions and answers its open questions (section 17). Implementation: `codec/container`.

**Decision:** custom `.virex` container (not MP4 with hidden data). The pixel section holds an H.264 stream made by FFmpeg. An MP4 export for ordinary players is a later, separate command.

## 1. Rules

- All integers are **little-endian**. All offsets are **absolute from the start of the file**.
- Layout: `header (72 B) | section table | section data ...`
- Section data is stored back to back, in table order. Readers must use `offset` and `length`, never assume order.
- The container version and the SVIR schema version are separate numbers.

## 2. Header (72 bytes)

| Offset | Size | Field | Notes |
|---|---|---|---|
| 0 | 4 | `magic` | ASCII `VIRX` |
| 4 | 2 | `format_major` | 1 |
| 6 | 2 | `format_minor` | 0 |
| 8 | 2 | `min_reader_major` | lowest reader major that may open this file |
| 10 | 2 | `min_reader_minor` | |
| 12 | 4 | `header_size` | 72. A reader seeks to `header_size` to find the section table |
| 16 | 4 | `flags` | 0 in v0.1 |
| 20 | 4 | `width` | pixels |
| 24 | 4 | `height` | pixels |
| 28 | 4 | `fps_num` | nominal frame rate numerator (informational only) |
| 32 | 4 | `fps_den` | nominal frame rate denominator (never 0) |
| 36 | 4 | `timescale` | ticks per second. **Every timestamp in the file is in these ticks** |
| 40 | 8 | `duration` | in ticks |
| 48 | 2 | `pixel_codec` | 1 = H.264 Annex B, 2 = H.265 Annex B, 3 = AV1 OBU stream |
| 50 | 2 | `schema_major` | SVIR schema version |
| 52 | 2 | `schema_minor` | |
| 54 | 2 | `section_count` | |
| 56 | 2 | `section_entry_size` | 40 in v0.1 |
| 58 | 6 | reserved | 0 |
| 64 | 4 | `table_crc32` | CRC32 of the whole section table |
| 68 | 4 | `header_crc32` | CRC32 of header bytes 0..67 |

**Timescale.** Timestamps are integer ticks, not microseconds, because microseconds cannot hold 1/30000 s exactly (33366.67 us). Use the source video stream's `time_base` denominator from ffprobe when its numerator is 1 (for example 15360 or 90000). Otherwise use 90000 and round.

## 3. Section table

`section_count` entries of `section_entry_size` bytes (40), starting at `header_size`.

| Offset | Size | Field | Notes |
|---|---|---|---|
| 0 | 2 | `type` | see below |
| 2 | 1 | `layer` | see below |
| 3 | 1 | `flags` | bit 0 = section data is zstd-compressed. Others 0 |
| 4 | 8 | `offset` | absolute file offset |
| 12 | 8 | `length` | bytes |
| 20 | 4 | `crc32` | CRC32 (IEEE) of the stored bytes |
| 24 | 8 | `start` | first tick this section covers (signed). 0 with `end` 0 = whole stream |
| 32 | 8 | `end` | last tick this section covers |

Several sections may share a type and layer. Giving each a time range lets the semantic data be split into time segments, so a query for minute 30 reads one small segment (with its own CRC) instead of the whole semantic stream.

**Types:** `1` PIXEL, `2` SEMANTIC, `3` TEMPORAL_INDEX, `4` METADATA, `5` AUDIO (reserved, not written in Phase 1). `6..0x7FFF` reserved for later VIReX sections. `0x8000+` private/experimental.

**Layers (C3):** `0` NONE (not a semantic section), `1` L0_ENTITY, `2` L1_EVENT, `3` L2_TEXT, `4` L3_EMBEDDING, `255` ALL. Values 1–4 match the `Layer` enum in `svir.proto`. In v0.1 the single SEMANTIC section uses `ALL`. Later it can be split into one section per layer with no layout change.

## 4. Section contents

| Section | Content in v0.1 |
|---|---|
| PIXEL | H.264 **Annex B** elementary stream. Every frame starts with an access unit delimiter (NAL type 9), which makes frame boundaries findable without a decoder. `virex-encode` produces it with `h264_mp4toannexb,h264_metadata=aud=insert`. Plain `h264_mp4toannexb` does **not** add delimiters |
| SEMANTIC | Protobuf SVIR (`schema_major.minor`). May be empty (length 0) in the skeleton |
| TEMPORAL_INDEX | Per-frame table, **required** (see 4.1). Gives real PTS and byte offsets |
| METADATA | Optional JSON (source name, encoder settings, tool version) |

### 4.1 TEMPORAL_INDEX layout

An H.264 Annex B stream has no timestamps and no index, so seeking in it means scanning from the start. The temporal index fixes that and keeps the source's exact timing. It has one record per coded frame, **in decode order** (the order of the PIXEL section), with fixed-size records so a reader can binary search it with `ReadAt` instead of loading it.

```text
u32 record_size (32) | u32 reserved | u64 count | count x record
record: i64 pts | i64 dts | u64 offset (inside PIXEL section) | u32 size | u32 flags
flags:  bit 0 = key frame (random-access point)
```

`pts` and `dts` are the original presentation and decode timestamps in header ticks. `dts` never decreases. `pts` does when B-frames reorder. Both are needed to rebuild the stream: an MP4 muxer needs DTS plus the PTS-DTS offset per frame. Size: 32 B per frame, about 3.5 MB for one hour at 30 fps, which is negligible next to the video.

Lookup: `FrameAt(pts)` finds the frame being shown (binary search on DTS, then a scan of at most 32 frames, the reorder limit). `SeekKey(i)` finds the key frame to start decoding from. Known limit: open-GOP leading pictures that display before their key frame are not special-cased.

### 4.2 AUDIO section (reserved, rules fixed now)

Phase 1 does not write audio, but the layout is fixed so adding it later changes nothing:

- One AUDIO section per audio track, `layer = NONE`. Phase 1 will handle the first track only.
- Content: an **ADTS AAC** stream. It is self-describing (sample rate and channels in every frame header) and its frame timing is implicit (1024 samples per frame).
- The entry's `start` is the timestamp (in ticks) of the first audio sample, so the audio/video offset is kept. `end` is the end tick.
- The encoder stream-copies AAC. Other audio codecs are transcoded to AAC. This is lossy, so say so in the paper.
- Audio is also the input for speech recognition later (a text layer, `L2`).

## 5. Integrity

- Per-section CRC32 for every section, plus one CRC32 for the table and one for the header.
- CRC32 detects accidental damage only. It is not tamper protection. A hash or signature section can be added later as a new type.
- `ReadSection` verifies the CRC. A reader that only wants to jump somewhere can use `OpenSection` (no CRC) and call `Verify` when it wants.

## 6. Versioning rules

1. **Unknown section types are skipped** using `offset`/`length`. Never parse them.
2. **Compatible changes** (new section types, new header flags, new Protobuf fields) bump the minor number. Old fields never change meaning.
3. **Breaking changes** bump `format_major`. `min_reader_*` says the oldest reader that can still open the file. A reader refuses a file when its own version is below `min_reader`.
4. The header can grow: readers use `header_size` and `section_entry_size` instead of constants, and ignore extra bytes.
5. SVIR evolves on its own (`schema_major.minor`). Schema changes after Phase 1 freeze must be additive.

## 7. Answers to the research open questions

| # | Question | Decision |
|---|---|---|
| 1 | Header field widths | Fixed widths in section 2 |
| 2 | Offsets absolute or relative | Absolute from file start, `uint64` |
| 3 | CRC32 on the section table | Yes, in the header (`table_crc32`), plus `header_crc32` |
| 4 | `layer` value for non-semantic sections | `0` NONE; `255` ALL |
| 5 | Temporal index mandatory | **Required**, per-frame, decode order, with PTS and DTS (section 4.1) |
| 6 | Semantic section zstd | Per-section flag bit 0, off by default |
| 7 | H.264 representation | Annex B elementary stream |
| 8 | MP4 export | Yes, later, as `virex-export`. Not part of the container |
| 9 | Versioning policy | Section 6 above |
| 10 | Hash/signature | Separate future section type, not CRC32 |

**Variable frame rate:** frame times always come from the TEMPORAL_INDEX, not from the `fps` header fields (those are only a nominal rate).

## 7b. Decode: rebuilding exact timestamps

**Decision:** `virex-decode` rebuilds the mp4 with the original PTS and DTS from the index, in the original timescale. It does not assume a constant frame rate.

**How (implemented, `codec/decoder`):** FFmpeg cannot take per-frame timestamps for a raw Annex B pipe, so the decoder writes the MP4 itself in Go from the PIXEL section and the index. It copies frames unchanged and writes `ftyp`, `mdat` (64-bit size) and `moov` with `stts` (DTS differences), `ctts` (PTS minus DTS), `stss` (key frames), `stsz`, `stsc`, `co64`, an edit list (an empty edit when the first frame is not at time 0) and an `avcC` built from the stream's SPS/PPS. Limits: 8-bit 4:2:0, one SPS/PPS for the whole stream, one video track, no audio.

**Verification (run, passes):** for every test clip, `ffprobe -show_packets` gives identical `pts`, `dts` and `flags` for the source and the decoded file, and the per-frame MD5 of the decoded pixels equals the source's (`tests/integration`). Also tested on a source that starts at 3 s and on a Matroska source (millisecond time base, no DTS stored; DTS is then synthesised from the PTS order).

**Encode side:** take `pts`, `dts`, `size`, key flag and `time_base` per packet from `ffprobe -show_packets`. Do not compute times from `frame_id / fps`.

## 7a. Changes from Chaitanya's research design

| Change | Why |
|---|---|
| Timestamps are ticks of a stored timescale, not microseconds | Exact rebuild of source timing, including 1/30000 s frame times |
| Temporal index required, per-frame, decode order, PTS and DTS, binary-searchable | Annex B has no timestamps or index; without this there is no frame-accurate seek and VFR breaks. Matroska references every video key frame in Cues, MP4 uses `stts`/`stss`/`stco` for the same job |
| `start`/`end` (ticks) on each table entry | Lets semantic data be stored as time segments with their own CRC, which Phase 3 range queries need |
| Codec ids for H.265 and AV1 | The container only carries bytes and does not care which codec made them. A new codec needs a new id, not a new layout |
| AUDIO section rules fixed (ADTS AAC, start tick) | Lecture videos carry speech. The decoder should not silently drop audio later |

See `research/chaitanya/research/2026-10-01-container-optimization.md`.

## 8. Diagram

```text
offset 0   ┌──────────────────────────────┐
           │ HEADER (72 B)  "VIRX" ...    │  header_crc32, table_crc32
72         ├──────────────────────────────┤
           │ SECTION TABLE (n x 40 B)     │  type, layer, flags, offset, length, crc32, start, end
72+40n     ├──────────────────────────────┤
           │ PIXEL     H.264 Annex B      │
           ├──────────────────────────────┤
           │ SEMANTIC  SVIR protobuf      │  layer = ALL (v0.1), may be time segments
           ├──────────────────────────────┤
           │ TEMPORAL_INDEX (per frame)   │
           ├──────────────────────────────┤
           │ AUDIO (optional, ADTS AAC)   │
           ├──────────────────────────────┤
           │ METADATA (optional, JSON)    │
           └──────────────────────────────┘
```
