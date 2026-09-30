# Tasks — Aniket

**Topic:** Key frames, video quality settings, and FFmpeg on Windows

## Goal
VIReX stores a normal H.264 video next to the semantic data. We need to know where the key frames are, which encoder settings to use, and what goes wrong on Windows.

## What to find out
**Part 1 — Key frames**
An **I-frame** is a frame stored on its own. To jump to any time in a video, we start from the I-frame just before it.
1. Which `ffprobe` command lists the I-frames with their times and positions?
2. Paste an example of the output.

**Part 2 — Encoder settings and quality**
1. **CRF**: what does it control? What values are sensible?
2. **Preset**: what is the trade-off between speed and file size?
3. **GOP size**: the distance between key frames. What is the trade-off?
4. How do we measure quality with **SSIM** and **PSNR**? Find the FFmpeg command for each.
5. Experiment: encode one clip at three CRF values (for example 20, 23, 28). Record file size, encode time, SSIM and PSNR in a table.



## What to hand in
- Your research notes (see below).
- The **`ffprobe` command** for key frames.
- The **experiment table**.
- A short **Windows checklist**.
- Recommended **default settings** for VIReX.

Read first: `docs/phase-1-foundation.md` (Week 2) and `docs/tech-stack.md` (section 4).

## Full forms
| Short | Full form |
|---|---|
| FFmpeg | Fast Forward MPEG (a tool to decode and encode video) |
| ffprobe | the FFmpeg tool that shows information about a video |
| H.264 | a common video format, also called AVC (Advanced Video Coding) |
| x264 | the encoder that makes H.264 video |
| I-frame | Intra-coded frame (a frame stored on its own) |
| GOP | Group of Pictures (frames from one key frame to the next) |
| CRF | Constant Rate Factor (quality setting; lower means better quality and a bigger file) |
| SSIM | Structural Similarity Index Measure (1.0 means identical) |
| PSNR | Peak Signal-to-Noise Ratio (higher is better) |
| PATH | the list of folders Windows searches when you run a program |

## How to write your research
Save notes in `research/aniket/research/`.

- One file per topic, named `YYYY-MM-DD-topic.md` (for example `2026-10-01-keyframes-ffprobe.md`).
- Copy `research/TEMPLATE.md` and fill it in.
- Paste the exact commands you ran and the results.
- End each note with what you recommend.

## Progress
| # | Note | Status |
|---|---|---|
| 1 | Key frames with ffprobe | not started |
| 2 | CRF, preset and GOP experiment | not started |
| 3 | SSIM and PSNR | not started |
| 4 | Recommended defaults | not started |
