# Phase 1 — Foundation: Ingestion, Pixel Pipeline, SVIR Schema, Container Skeleton

**Project:** VIReX
**Suggested duration:** 4–5 weeks
**Phase goal:** Have a working Go skeleton that takes a video in, produces a pixel stream, defines the semantic data model, and writes and reads a minimal `.virex` file. No AI yet.

---

## 1. Why this phase first

Every later phase depends on three things being stable:

1. Frames with **reliable timestamps** (pixels and semantics are aligned by time).
2. The **SVIR schema** (what a semantic block looks like).
3. A **container** that can hold more than one stream.

If these change late, everything built on them breaks. Get them right first, and keep the semantic content trivial.

---

## 2. Deliverables

| # | Deliverable | Done when |
|---|---|---|
| 1 | Repo scaffold matching the architecture doc (`cmd/`, `codec/`, `semantic/`, `temporal/`, `svir/`, `index/`, `api/`, `workers/`, `storage/`, `tests/`, `docs/`) | `go build ./...` passes |
| 2 | Video ingestion (Go + FFmpeg) | Any mp4 gives a `(frame_id, timestamp, image)` stream |
| 3 | Frame scheduler / buffer | Frames flow through a bounded channel and don't blow up memory |
| 4 | Semantic frame sampler | Configurable: every N frames, every T seconds, or on scene change |
| 5 | Pixel pipeline | FFmpeg encodes to H.264 and the pixel stream is written as a separate file |
| 6 | SVIR schema v0.1 in Protobuf | `.proto` covers Entity, Object, Text, Action, Event, Relation, Scene and Temporal blocks |
| 7 | VIReX container v0.1 | Header, pixel stream and an (empty) semantic stream are written and read back |
| 8 | CLI `virex-encode` and `virex-decode` (pixel only) | `virex-decode` rebuilds a playable mp4 from `.virex` |
| 9 | Test video set | 5–8 short clips (lecture, screen recording, street, indoor) |

---

## 3. Tasks in order

### Week 1 — Setup and ingestion
- [x] Install Go, FFmpeg, Protobuf compiler, Python 3.10+, Git. Create the repo and `README.md`.
- [x] Create the folder structure from the architecture doc (section 18).
- [x] Write `codec/encoder/ingest.go`. It runs `ffprobe` for FPS, resolution, duration and codec, then reads frames through an `ffmpeg` pipe (raw RGB or JPEG) into Go.
- [x] Attach a timestamp and frame id to every frame: `timestamp = frame_id / fps`. Handle variable frame rate by using the real PTS from ffprobe.
- [x] Unit test: frame count and last timestamp match ffprobe.

### Week 2 — Scheduler, sampler, pixel pipeline
- [x] Build the frame scheduler with goroutines and buffered channels. Add back-pressure.
- [x] Build the semantic sampler with three strategies: fixed interval, time interval, scene-change (frame-difference or histogram threshold).
- [x] Pixel pipeline: run the FFmpeg H.264 (libx264) encode with configurable CRF and preset. Detect key frames by reading I-frame positions with ffprobe and record them for the temporal index later.
- [x] Save the compressed pixel stream to disk.

### Week 3 — SVIR schema
- [x] Write `svir/schema/svir.proto`. Use the block model from the architecture doc (section 4). Example fields:
  - `Entity { id, type, first_seen, last_seen }`
  - `ObjectBlock { entity_id, class, bbox, confidence, timestamp }`
  - `TextBlock { content, bbox, confidence, timestamp }`
  - `ActionBlock { subject_id, action, start, end }`
  - `EventBlock { type, start, end, entity_ids, region, confidence }`
  - `RelationBlock { subject_id, relation, object_id, start, end }`
  - `SceneBlock { label, context, start, end }`
- [x] **[C4]** Add a shared `PixelRef { t_start, t_end, frame_id, bbox }` message and give every block above a `pixel_ref` field, so any semantic fact can be traced back to its pixels.
- [x] **[C3]** Add a `layer` field (enum `L0_ENTITY`, `L1_EVENT`, `L2_TEXT`, `L3_EMBEDDING`) to every block. Layers: L0 = entities and objects, L1 = events, actions, relations, scenes, L2 = text, L3 = embeddings and captions.
- [x] Add `schema_version` and generate Go code (`protoc-gen-go`).
- [x] Write a JSON export helper for SVIR so you can inspect and debug it.
- [x] Write hand-made fake SVIR data for one clip so you can test the container before any ML exists.

### Week 4 — Container v0.1 and CLIs
- [x] Define the binary layout: `magic bytes | header | section table | pixel section | semantic section | index section | metadata`.
- [x] Header fields: version, resolution, fps, duration, pixel codec, semantic schema version.
- [x] The section table holds `(type, offset, length)` for each section, so any section can be read without scanning the file.
- [x] **[C3]** Reserve a `layer` id in the section table entry, so the semantic section can later be split into one stream per layer without changing the container layout.
- [x] Write `codec/container/writer.go` and `reader.go`.
- [x] Add a CRC32 per section.
- [x] `virex-encode input.mp4 -o out.virex` and `virex-decode out.virex -o out.mp4`.
- [x] Round-trip test: the decoded video plays and matches the source (check with SSIM or PSNR through FFmpeg).

### Week 5 (buffer)
- [x] Fix bugs, write docs, tidy the code.
- [x] Record baseline numbers: encode time, size of source vs pixel stream vs `.virex`.
- [x] **[C1]** Add a small cost logger (`cost/`) that records, per run and per stage: wall time, CPU time, bytes in and out, and (from Phase 2) model calls and tokens. Write it as JSON lines. Every later phase reports through it, so the amortization model has real data.
- [x] Write the Phase 1 summary for your logbook.

---

## 4. Exit criteria (all must be true)

- `virex-encode` and `virex-decode` work on all test clips.
- The container reader can jump to the semantic section without reading the pixel section.
- SVIR `.proto` is reviewed, versioned and committed. Later changes must be additive.
- Sampler emits frames with correct timestamps.
- The decoded video is visually identical to the H.264 encode.
- Schema v0.1 has `pixel_ref` (C4) and `layer` (C3) on every block, and the cost logger (C1) writes a per-stage record for every encode.

## 4a. Paper contributions in this phase

| ID | What to do here |
|---|---|
| C1 | Cost logger with per-stage time, bytes and (later) tokens |
| C3 | `layer` enum in the schema, `layer` id in the section table |
| C4 | `PixelRef` message on every block |
| C2 | Nothing yet |

These are cheap now and painful to retrofit later, which is why they belong in the schema freeze.

## 5. Risks and mitigations

| Risk | Mitigation |
|---|---|
| Timestamp drift on variable-FPS video | Always use PTS from ffprobe, never assume constant FPS |
| Memory blow-up decoding long videos | Bounded channels, stream frames, never load the whole video |
| Schema keeps changing | Freeze v0.1 at the end of week 3, only add optional fields after |
| Windows path and pipe issues with FFmpeg | Use `exec.Command` with explicit args, test early on Windows |

## 6. Hand-off to Phase 2

Phase 2 receives: sampled frames with timestamps, a frozen SVIR schema, a container that has a semantic slot waiting to be filled.
