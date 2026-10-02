# Getting frames and real timestamps into Go

| Field | Value |
|---|---|
| **Topic** | RGB vs JPEG vs PNG through an ffmpeg pipe, and real frame times with ffprobe PTS |
| **Date** | 2026-10-01 |
| **Author** | Claude, running Anushka's task (Anushka has not started it) |
| **Task** | Tasks — Chaitanya, notes 1–3 |
| **Status** | draft, needs a human review |

## 1. Question
1. Which frame format should ffmpeg pipe into Go: raw RGB, JPEG or PNG?
2. How do we get the real time of each frame, including for variable-frame-rate (VFR) video?

## 2. Short answer
- Use **raw RGB24** for the in-process pipeline (sampler, scene detection). It was the fastest in Go and the simplest code. Encode **JPEG only for the sampled frames** that go to the ML service.
- Get frame times from **packet PTS** (`ffprobe -show_entries packet=pts,...`), never `frame / fps`. The VFR clip had gaps from 0.033 s to 0.100 s that `frame / fps` would get wrong.

## 3. Sources
- Own experiments below (ffmpeg 9.0.2, ffprobe, Go 1.26, Windows 10).
- Code: `scripts/framebench`, `scripts/printframes`, `codec/encoder/ingest.go`.

## 4. Findings

### 4.1 Format comparison
Same clips for all formats, one run each, 300 frames, `-fps_mode passthrough`, decoding in Go until a decoded image is available. Command: `go run ./scripts/framebench CLIP`.

**c02 testsrc2, 640x360**
| Format | Wall s | Frames/s | Pipe MB | Peak Go heap MB |
|---|---|---|---|---|
| rgb24 | 0.76 | 396 | 207 | 4.7 |
| jpeg (q:v 2) | 1.85 | 162 | 7.7 | 4.2 |
| png | 3.17 | 95 | 13.3 | 5.2 |

**c01 mandelbrot, 1280x720**
| Format | Wall s | Frames/s | Pipe MB | Peak Go heap MB |
|---|---|---|---|---|
| rgb24 | 3.07 | 98 | 829 | 12.3 |
| jpeg (q:v 2) | 6.76 | 44 | 41.6 | 7.0 |
| png | 17.50 | 17 | 234.7 | 13.1 |

Other points:
| | RGB24 | JPEG | PNG |
|---|---|---|---|
| Go code | `io.ReadFull` of `w*h*3` bytes | Needs frame framing, then `image/jpeg` | `image/png` |
| Data loss | None | Lossy | None |
| Pipe volume | Large (2.8 MB per 720p frame) | Small | Medium |
| Decoded form in Go | Plain bytes | `*image.YCbCr` (luma plane is free for scene detection) | `*image.RGBA` / `NRGBA` |

Pitfalls found:
- **JPEG framing is fragile.** Decoding JPEGs straight from the pipe with `jpeg.Decode` failed at frame 26 of the 720p clip (`invalid JPEG format: missing SOI marker`) while the 360p clip passed. The fix used in the benchmark is to scan for the end-of-image marker `FF D9` (safe, because `FF` inside data is stuffed as `FF 00`) and decode each buffer.
- **Never stop reading the pipe on an error.** The first benchmark hung because ffmpeg blocked on a full pipe after a decode error. Drain stdout before `Wait()`.
- At end of stream, `jpeg.Decode` and `png.Decode` return `unexpected EOF`; peek one byte first to detect the end cleanly.
- RGB memory is set by the channel buffer, not by the clip: buffer of 8 frames at 720p is about 22 MB.

### 4.2 Real frame times
Command used by `codec/encoder.Packets`:
```text
ffprobe -v error -select_streams v:0 -show_entries packet=pts,dts,size,pos,flags -of compact=p=0 FILE
```
Output (c02, B-frames on, packets in decode order):
```text
pts=0|dts=-1024|size=3523|pos=48|flags=K__
pts=2048|dts=-512|size=632|pos=3571|flags=___
pts=512|dts=0|size=163|pos=4203|flags=___
```
Fields we need: **`pts`** (when the frame is shown, in `time_base` ticks), **`dts`** (decode order), **`flags`** (`K` = key frame). The stream `time_base` (here `1/15360`) converts ticks to seconds. Frames come out of ffmpeg in display order, so frame *i* gets the *i*-th smallest PTS.

Normal vs VFR (`go run ./scripts/printframes CLIP`):
```text
c02 (constant)              c05 (variable)
frame,pts,time_s            frame,pts,time_s
0,0,0.000000                0,0,0.000000
1,512,0.033333              1,512,0.033333
2,1024,0.066667             2,1024,0.066667
3,1536,0.100000             3,2048,0.133333   <- gap
4,2048,0.133333             4,3072,0.200000   <- gap
```
| Clip | Frames | Smallest gap s | Largest gap s | `r_frame_rate` | `avg_frame_rate` |
|---|---|---|---|---|---|
| c02 constant | 300 | 0.033333 | 0.033334 | 30/1 | 30/1 |
| c05 variable | 234 | 0.033333 | 0.100000 | 30/1 | 540/23 |

`frame / fps` at frame 4 of c05 gives 0.133 s, but the real time is 0.200 s. `r_frame_rate` stays 30/1 on the VFR clip, so it must not be trusted either.

## 5. Experiments
Commands: `go run ./scripts/framebench tests/testdata/clips/c01_mandelbrot_720p.mp4` (and c02), `go run ./scripts/printframes tests/testdata/clips/c05_vfr_360p.mp4`. Clips come from `scripts/make-testclips.ps1`. Single runs on a shared laptop, so ratios matter more than absolute numbers. `codec/encoder/ingest_test.go` checks the frame count and every PTS against an independent `ffprobe frame=pts` query, for a constant and a variable clip.

## 6. Decision / recommendation
| Use | Format |
|---|---|
| Sampler, scene detection, anything inside Go | `ffmpeg ... -f rawvideo -pix_fmt rgb24 pipe:1` (current code) |
| Frames sent to the ML service | Encode only sampled frames to JPEG in Go (`image/jpeg`, quality 90+) or PNG if small text must stay exact |
| Frame times | Packet PTS from ffprobe, mapped by sorted order |

Rejected: PNG for the main stream (6x slower than RGB at 720p), JPEG for the main stream (lossy and slower than RGB, plus the framing issue).

## 7. Open questions
1. 720p RGB is 83 MB/s through the pipe. If that matters on long videos, add `-vf scale=` or `format=gray` for the sampler stream. Not measured.
2. JPEG quality for OCR needs a test in Phase 2.
3. Real phone screen recordings have not been tried; c05 is simulated VFR.

## 8. Glossary
| Short | Full form |
|---|---|
| PTS / DTS | Presentation / Decode Time Stamp |
| VFR | Variable Frame Rate |
| RGB | Red, Green, Blue |
| JPEG | Joint Photographic Experts Group |
| PNG | Portable Network Graphics |
| SOI / EOI | Start / End Of Image marker (JPEG) |
