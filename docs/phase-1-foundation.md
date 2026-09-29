# Phase 1 — Foundation: Ingestion, Pixel Pipeline, SVIR Schema, Container Skeleton

**Project:** Semantic Video Codec (SVC)
**Suggested duration:** 4–5 weeks
**Phase goal:** Have a working Go skeleton that takes a video in, produces a pixel stream, defines the semantic data model, and writes and reads a minimal `.svc` file. No AI yet.

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
| 7 | SVC container v0.1 | Header, pixel stream and an (empty) semantic stream are written and read back |
| 8 | CLI `svc-encode` and `svc-decode` (pixel only) | `svc-decode` rebuilds a playable mp4 from `.svc` |
| 9 | Test video set | 5–8 short clips (lecture, screen recording, street, indoor) |

---

## 3. Tasks in order

### Week 1 — Setup and ingestion
- [ ] Install Go, FFmpeg, Protobuf compiler, Python 3.10+, Git. Create the repo and `README.md`.
- [ ] Create the folder structure from the architecture doc (section 18).
- [ ] Write `codec/encoder/ingest.go`. It runs `ffprobe` for FPS, resolution, duration and codec, then reads frames through an `ffmpeg` pipe (raw RGB or JPEG) into Go.
- [ ] Attach a timestamp and frame id to every frame: `timestamp = frame_id / fps`. Handle variable frame rate by using the real PTS from ffprobe.
- [ ] Unit test: frame count and last timestamp match ffprobe.

### Week 2 — Scheduler, sampler, pixel pipeline
- [ ] Build the frame scheduler with goroutines and buffered channels. Add back-pressure.
- [ ] Build the semantic sampler with three strategies: fixed interval, time interval, scene-change (frame-difference or histogram threshold).
- [ ] Pixel pipeline: run the FFmpeg H.264 (libx264) encode with configurable CRF and preset. Detect key frames by reading I-frame positions with ffprobe and record them for the temporal index later.
- [ ] Save the compressed pixel stream to disk.

### Week 3 — SVIR schema
- [ ] Write `svir/schema/svir.proto`. Use the block model from the architecture doc (section 4). Example fields:
  - `Entity { id, type, first_seen, last_seen }`
  - `ObjectBlock { entity_id, class, bbox, confidence, timestamp }`
  - `TextBlock { content, bbox, confidence, timestamp }`
  - `ActionBlock { subject_id, action, start, end }`
  - `EventBlock { type, start, end, entity_ids, region, confidence }`
  - `RelationBlock { subject_id, relation, object_id, start, end }`
  - `SceneBlock { label, context, start, end }`
- [ ] Add `schema_version` and generate Go code (`protoc-gen-go`).
- [ ] Write a JSON export helper for SVIR so you can inspect and debug it.
- [ ] Write hand-made fake SVIR data for one clip so you can test the container before any ML exists.

### Week 4 — Container v0.1 and CLIs
- [ ] Define the binary layout: `magic bytes | header | section table | pixel section | semantic section | index section | metadata`.
- [ ] Header fields: version, resolution, fps, duration, pixel codec, semantic schema version.
- [ ] The section table holds `(type, offset, length)` for each section, so any section can be read without scanning the file.
- [ ] Write `codec/container/writer.go` and `reader.go`.
- [ ] Add a CRC32 per section.
- [ ] `svc-encode input.mp4 -o out.svc` and `svc-decode out.svc -o out.mp4`.
- [ ] Round-trip test: the decoded video plays and matches the source (check with SSIM or PSNR through FFmpeg).

### Week 5 (buffer)
- [ ] Fix bugs, write docs, tidy the code.
- [ ] Record baseline numbers: encode time, size of source vs pixel stream vs `.svc`.
- [ ] Write the Phase 1 summary for your logbook.

---

## 4. Exit criteria (all must be true)

- `svc-encode` and `svc-decode` work on all test clips.
- The container reader can jump to the semantic section without reading the pixel section.
- SVIR `.proto` is reviewed, versioned and committed. Later changes must be additive.
- Sampler emits frames with correct timestamps.
- The decoded video is visually identical to the H.264 encode.

## 5. Risks and mitigations

| Risk | Mitigation |
|---|---|
| Timestamp drift on variable-FPS video | Always use PTS from ffprobe, never assume constant FPS |
| Memory blow-up decoding long videos | Bounded channels, stream frames, never load the whole video |
| Schema keeps changing | Freeze v0.1 at the end of week 3, only add optional fields after |
| Windows path and pipe issues with FFmpeg | Use `exec.Command` with explicit args, test early on Windows |

## 6. Hand-off to Phase 2

Phase 2 receives: sampled frames with timestamps, a frozen SVIR schema, a container that has a semantic slot waiting to be filled.
