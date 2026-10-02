# Phase 1 Summary (logbook)

**Date:** 2026-10-01. **Scope:** `docs/phase-1-foundation.md`. **Result:** every deliverable is built and tested. A few items need a human decision or real footage before the phase can be called signed off (section 6).

## 1. Deliverables

| # | Deliverable | Status | Where |
|---|---|---|---|
| 1 | Repo scaffold, `go build ./...` | Done | whole repo |
| 2 | Video ingestion (Go + FFmpeg) | Done. Tested on H.264 mp4 and mkv sources: frames come out with the real PTS | `codec/encoder` |
| 3 | Frame scheduler / buffer | Done. Bounded channels, back-pressure tested | `codec/scheduler` |
| 4 | Semantic frame sampler | Done. Every N frames, every T seconds, scene change | `codec/sampler` |
| 5 | Pixel pipeline | Done. x264 re-encode or stream copy, written as a separate Annex B stream | `codec/pixel` |
| 6 | SVIR schema v0.1 in Protobuf | Done, awaiting team sign-off | `svir/schema`, `docs/svir-schema-v0.1.md` |
| 7 | VIReX container v0.1 | Done | `codec/container`, `docs/virex-format-v0.1.md` |
| 8 | `virex-encode` and `virex-decode` | Done | `cmd/`, `codec/pipeline`, `codec/decoder` |
| 9 | Test video set | **Partly.** One real clip (`Semantic_Video_Codec.mp4`, 9.5 min) is in the repo; the 8 synthetic clips were removed but can be regenerated. No lecture, street or indoor footage | `tests/testdata/clips/`, `scripts/make-testclips.ps1` |
| C1 | Cost logger | Done. JSON lines per stage | `cost` |
| C3 | `layer` in schema and section table | Done | `svir.proto`, `container` |
| C4 | `PixelRef` on every block | Done, enforced by `schema.Validate` | `svir.proto` |

## 2. Exit criteria

| Criterion | Evidence |
|---|---|
| `virex-encode` / `virex-decode` work on all test clips | `TestRoundTripAllClips` runs all 8 clips; `TestReencodeQuality` covers 4 more runs in re-encode mode |
| Reader jumps to semantic data without reading pixels | `TestSemanticSectionWithoutReadingPixels`: reads under 10 % of the pixel section's size to reach it |
| SVIR `.proto` reviewed, versioned, committed; later changes additive | Versioned and tested (`TestSchemaOnlyGrows`). **Review by the team is still open** |
| Sampler emits frames with correct timestamps | Scene sampler finds the cuts at frames 75, 150, 225 of `c04`; samples are stored with exact PTS in the metadata (`TestSamplesRecordedInMetadata`) |
| Decoded video visually identical to the H.264 encode | Per-frame MD5 of the decoded mp4 equals the stored stream's, for every re-encoded clip. In copy mode it also equals the source (SSIM exactly 1) |
| `pixel_ref`, `layer`, cost log in place | Tests above; `cost` stage records for ingest, pixel, sampling, container |

## 3. What "no quality drop" means here

Two pixel modes (`pixel.mode` in `configs/default.json`):

| Mode | Behaviour | Quality |
|---|---|---|
| `copy` (chosen by `auto` for 8-bit yuv420p H.264 sources) | Keeps the source's own coded frames | **Lossless relative to the source.** Pixels and timestamps are identical |
| `reencode` | libx264, CRF 18, preset medium | Not lossless. SSIM 0.990 to 0.998 and PSNR 40.8 to 49.0 dB on the test clips |

Because the test clips are H.264, `auto` uses copy mode, so the default run changes no pixel. Re-encoding costs quality by definition; CRF 18 was chosen from the experiment in `research/aniket/research/2026-10-01-keyframes-and-x264-settings.md`.

## 4. Baseline numbers (single run each, shared laptop, Windows 10)

Defaults except `-pixel`. Time is the whole command, including a sampling pass that decodes the video once more.

| Clip | Source MB | Copy: .virex MB | Copy: encode s | Reencode: .virex MB (% of source) | Reencode: encode s | Decode s |
|---|---|---|---|---|---|---|
| c01 mandelbrot 720p | 19.77 | 19.78 | 4.4 | 12.15 (61 %) | 18.9 | 0.3 |
| c02 testsrc2 360p | 2.14 | 2.15 | 1.4 | 1.50 (70 %) | 7.5 | 0.1 |
| c03 life 360p | 11.39 | 11.40 | 2.6 | 8.34 (73 %) | 7.8 | 0.1 |
| c04 cuts 360p | 6.30 | 6.31 | 1.9 | 4.86 (77 %) | 5.5 | 0.1 |
| c05 VFR 360p | 1.80 | 1.81 | 1.3 | 1.31 (73 %) | 3.2 | 0.1 |
| c06 with audio 360p | 2.23 | 2.15 (audio dropped) | 1.5 | 1.50 (67 %) | 3.9 | 0.1 |
| c07 noise 360p | 43.82 | 43.83 | 4.1 | 29.33 (67 %) | 19.2 | 0.3 |
| c08 static 360p | 0.015 | 0.024 | 1.3 | 0.024 (157 %) | 2.5 | 0.1 |

Average time per stage over all runs (ms): ingest probe 389, x264 5609 (re-encode runs only), Annex B conversion 235, sampling 1417, container write 62.

Reading the numbers:
- **Container overhead is small but fixed:** about 8 KB for a 10 s clip. The temporal index is 32 bytes per frame (9.6 KB for 300 frames). On the nearly empty clip `c08` that exceeds the video itself. Phase 3 compression can shrink the index.
- **These are not compression results.** The clips are synthetic and the source is already H.264, so "% of source" mostly shows that CRF 18 is a little smaller than a CRF 12 source. The paper's compression claims need real footage and the Phase 3 semantic encoding.
- The timings come from one run each. The 7.5 s for `c02` re-encode is an outlier (the same encode took 1.4 s inside the experiment). Repeat runs before quoting any time.

### First real clip: `Semantic_Video_Codec.mp4`

1280x720, 24 fps, 13,673 frames (9 min 30 s), H.264 plus AAC audio, 24.11 MB. Single runs, same laptop.

| | Copy (default for this clip) | Re-encode (CRF 18) |
|---|---|---|
| Pixel stream | 17.19 MB | 19.41 MB |
| `.virex` file | 17.63 MB | 19.85 MB |
| Encode time | 94 s (92 s of it is the sampling pass) | 129 s (sampling skipped) |
| Decode time | 0.8 s | not timed |
| Timestamps | pts, dts and key flags identical for all 13,673 frames | all 13,673 presentation times identical |
| Pixels | per-frame hashes identical to the source | SSIM 0.9996, PSNR 58.2 dB |
| Audio | dropped with a warning | dropped with a warning |

- The copy-mode pixel stream is smaller than the source file because the source also holds the audio track.
- Scene-change sampling picked 63 of 13,673 frames. Those 63 have not been looked at by a person.
- The sampling pass decodes every frame once more and dominates encode time. Scaling frames down in ffmpeg before the sampler would cut that; not done.
- The CRF 18 re-encode is an encode of an already compressed source, so quality is only meaningful relative to that source.

## 5. Things the tests found (and fixes)

| Finding | Fix |
|---|---|
| `h264_mp4toannexb` does not insert access unit delimiters; frame splitting needs them | Add `h264_metadata=aud=insert` to the filter chain; the splitter rejects streams without them |
| Re-encoding moved a 3 s start offset to 0 | The pipeline adds the source's first-PTS offset back; tested with an offset clip |
| Matroska stores no (or only partial) DTS | DTS is synthesised from the PTS order (smallest valid delay) |
| JPEG frames read straight from a pipe lost their boundaries at 720p | Not used in the pipeline; the benchmark scans for the end-of-image marker |
| A benchmark hung when it stopped reading ffmpeg's pipe | Drain the pipe on error. `encoder.Frames` already stops ffmpeg on cancel |
| `fps` and `r_frame_rate` are wrong for variable frame rate | Never used for times; PTS from ffprobe packets |

## 6. Not done, or needs a decision

1. **Little real footage.** One real clip has been run. The tables in section 4 above come from synthetic clips that are no longer in the repo (`scripts/make-testclips.ps1` regenerates them). Lecture, screen-recording, street and indoor clips are still missing, and the sampler and CRF defaults should be re-checked on them.
2. **Research notes were written by Claude** (Aniket's, Anushka's and Deep's tasks, plus the container follow-up). They are marked draft and need a human review. Deep's survey rests on summaries: the CSGCoT paper itself could not be read (abstract only) and the COCO row is from memory.
3. **Schema sign-off.** `TemporalBlock`, `EmbeddingBlock` and `AttributeBlock` go beyond the phase doc's field list (the last was added after the survey). Freezing v0.1 means agreeing to additive-only changes from now on.
4. **Audio is dropped** with a warning. The container rules for an AUDIO section are fixed (`docs/virex-format-v0.1.md` 4.2); writing and muxing audio is not implemented.
5. **Decoder limits:** H.264 only, 8-bit 4:2:0, one SPS/PPS per stream, one video track. Enforced with clear errors where it can be detected.
6. **Key-frame edge:** open-GOP leading pictures are not special-cased in `SeekKey`.
7. **Windows only tested.** Paths and pipes are handled with explicit `exec.Command` arguments, but Linux/macOS were not run.
8. **Not measured:** peak memory of a long video (design is bounded; only a 10 s clip was run), the 20 to 45 minute lecture mentioned in the tech-stack doc.
9. `go.mod` is now `go 1.23` (the Protobuf library needs it); the docs said 1.22.

## 7. Reproduce

```text
pwsh scripts/make-testclips.ps1
go build -o bin/ ./cmd/...
bin/virex-encode tests/testdata/clips/c05_vfr_360p.mp4 -o c05.virex -cost-log cost.jsonl
bin/virex-decode c05.virex -info
bin/virex-decode c05.virex -o c05.out.mp4
go test ./...
go run ./scripts/framebench tests/testdata/clips/c01_mandelbrot_720p.mp4
```

## 8. Hand-off to Phase 2

Phase 2 receives: sampled frames with exact timestamps (`scheduler.Run` delivers them to a sink function; swap in the ML client there), a schema with `pixel_ref` and `layer` that only grows, a container with a SEMANTIC section (optionally one per layer) waiting to be filled, and a cost logger with `model_calls` and `tokens` fields ready.
