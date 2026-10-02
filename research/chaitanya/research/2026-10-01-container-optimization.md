# Is the `.virex` container good enough for a real codec?

| Field | Value |
|---|---|
| **Topic** | Review of the v0.1 container (from the 2026-9-30 note) against the Phase 1 doc and against how MP4/Matroska do random access |
| **Date** | 2026-10-01 |
| **Author** | Claude, reviewing Chaitanya's design |
| **Task** | Tasks — Chaitanya, Container format (follow-up) |
| **Status** | draft |

## 1. Question
Is the container optimised, and can it carry a major codec (H.264, H.265, AV1)? Does it meet `docs/phase-1-foundation.md`? What should change?

## 2. Short answer
It meets every Phase 1 deliverable and exit criterion. It was not yet good for random access or for more than H.264: the temporal index was optional and undefined, Annex B carries no timestamps, and the semantic data was one blob. Four changes fix that (section 6). The container still carries bytes only, so any codec fits as long as it has an id.

## 3. Sources
- [Matroska — Cues](https://www.matroska.org/technical/cues.html) — read on 2026-10-01 — every video key frame should have a cue point (time, track, cluster position), sorted by time.
- [FFmpeg formats documentation](https://ffmpeg.org/ffmpeg-formats.html) — read on 2026-10-01 — `h264`/`hevc`/`obu` raw muxers; MP4 `faststart` (index at the front) and fragmented MP4 (readable while writing, lower memory).
- Search results on raw H.264 elementary streams, 2026-10-01 — Annex B streams carry no timestamps or index, so seeking by time is unsupported. MP4 uses `stts` (durations) and `stss` (sync samples) to seek to the nearest key frame.
- Search results on Go `hash/crc32`, 2026-10-01 — the IEEE polynomial has a CLMUL fast path and Castagnoli has an SSE4.2 path, so CRC cost is not a concern.
- Earlier note: `2026-9-30-structure.md`.

## 4. Findings

### 4.1 Check against the Phase 1 doc
| Phase 1 requirement | Status |
|---|---|
| Magic, header, section table, pixel, semantic, index, metadata | Met |
| Header: version, resolution, fps, duration, pixel codec, schema version | Met |
| Table `(type, offset, length)` | Met |
| `layer` id in the table (C3) | Met |
| CRC32 per section | Met |
| Empty semantic section is valid | Met, tested |
| Reader jumps to semantic without reading pixels | Met, tested by counting bytes read |
| `virex-decode` rebuilds a playable mp4 | Possible only for constant frame rate (see below) |

### 4.2 Weak points found
| # | Problem | Evidence |
|---|---|---|
| 1 | Annex B has no timestamps and no index. Seeking means scanning from the start, and variable-frame-rate video cannot be rebuilt from `fps` alone | Raw-stream demuxers have no seek and no PTS. MP4 needs `stts`/`stss`, Matroska needs Cues |
| 2 | Temporal index was optional and its layout was not defined | Phase 3 needs frame-accurate access and range queries |
| 3 | One semantic blob per file | A query for one minute would decode the whole semantic stream |
| 4 | Only H.264 had an id | The container is codec-agnostic, but readers need to know which bitstream form is inside |
| 5 | No place for audio | Lecture clips carry speech; the decoder would drop it silently |

### 4.3 Things that are fine
- **CRC32 speed:** Go has hardware paths for both polynomials. No change.
- **Section table at the front:** same idea as MP4 `faststart`. A reader needs one small read to find everything.
- **Sections written in one pass:** acceptable for files. A streaming or fragmented layout (as in fragmented MP4) is a future option, not needed for Phase 1–4.
- **Codec independence:** the pixel section is opaque bytes. H.265 and AV1 only need an id (`ffmpeg -f hevc`, `-f obu`).

## 5. Experiments
`go test ./codec/container/` passes with new tests for frame lookup (including a variable-frame-rate gap), time-segmented semantic sections and range queries. No performance benchmark has been run yet; benchmark against a 20–45 minute clip in Week 5 before claiming any speed.

## 6. Decision / recommendation
Applied to `docs/virex-format-v0.1.md` and `codec/container`:

1. **Per-frame temporal index, required.** Fixed 32-byte records `(pts, dts, offset, size, flags)` in decode order, binary-searchable with `ReadAt`. About 3.5 MB per hour at 30 fps.
2. **Time range on each table entry** (`start`, `end`), so semantic data can be split into time segments with their own CRC. Entry size is now 40 bytes; readers use `section_entry_size`, so this stays extensible.
3. **Codec ids** for H.265 (Annex B) and AV1 (OBU stream).
4. **AUDIO section rules fixed** (ADTS AAC, first-sample start tick), not written in Phase 1.
5. **Timescale ticks instead of microseconds**, plus DTS in the index, so the original timestamps can be rebuilt exactly.

Rejected for now: switching to CRC32C (no gain), chunked CRC inside the pixel section (the index plus per-segment CRC covers the need; revisit if corruption localisation matters), fragmented streaming layout (not needed yet).

The encoder must fill the index while it writes the pixel stream. ffprobe gives PTS and key-frame flags per packet (`-show_packets`).

### Decided after review
- `virex-decode` **rebuilds exact PTS/DTS from the index**. Plan and verification: `docs/virex-format-v0.1.md` section 7b. Not implemented yet.
- Audio is **kept in mind**: the section and its rules are fixed now; writing it comes later.

## 7. Open questions
1. Pixel-section integrity per GOP, or section-level only?

## 8. Glossary
| Short | Full form |
|---|---|
| PTS | Presentation Timestamp |
| GOP | Group Of Pictures (key frame and the frames that depend on it) |
| OBU | Open Bitstream Unit (AV1) |
| CLMUL | Carry-less multiplication CPU instruction |
| ASR | Automatic Speech Recognition |
| VFR | Variable Frame Rate |
