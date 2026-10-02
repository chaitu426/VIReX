# Tasks — Anushka

**Topic:** Getting frames and timestamps out of a video
**Time:** 2–3 days

## Goal
The first step of VIReX is to read a video and pass each frame, with its exact time, to our Go program. We need to know the best way to do that.

## What to find out
**Part 1 — How do we get frames into Go?**
FFmpeg decodes the video and sends frames to Go through a **pipe** (a stream of data between two programs). Compare three frame formats: **raw RGB**, **JPEG** and **PNG**. For each one, note the speed, memory use and how easy the Go code is. Use the same test clip for all three.

**Part 2 — How do we get the real time of each frame?**
Some videos do not have a steady frame rate, so we cannot use `frame number ÷ FPS`. Each frame has a **PTS** (the exact time it is shown).
1. Which `ffprobe` command shows the PTS of every frame?
2. What does the output look like? Paste an example and mark the fields we need.
3. Try one normal video and one variable-frame-rate video (a phone screen recording often is). Show the difference.

## What to hand in
- Your research notes (see below).
- A small **Go program** that reads a video and prints `frame number, time` for each frame.
- A **table** comparing RGB, JPEG and PNG.
- The exact **commands** we should use.

Read first: `docs/phase-1-foundation.md` (Week 1 and 2).

## Full forms
| Short | Full form |
|---|---|
| FFmpeg | Fast Forward MPEG (a tool to decode and encode video) |
| ffprobe | the FFmpeg tool that shows information about a video |
| PTS | Presentation Time Stamp (when a frame is shown) |
| FPS | Frames Per Second |
| VFR | Variable Frame Rate |
| RGB | Red, Green, Blue |
| JPEG | Joint Photographic Experts Group (compressed image) |
| PNG | Portable Network Graphics (lossless image) |
| JSON | JavaScript Object Notation (a text data format) |

## How to write your research
Save notes in `research/anushka/research/`.

- One file per topic, named `YYYY-MM-DD-topic.md` (for example `2026-10-01-rgb-vs-jpeg-vs-png.md`).
- Copy `research/TEMPLATE.md` and fill it in.
- Paste the exact commands you ran and the results.
- End each note with what you recommend.

## Progress
| # | Note | Status |
|---|---|---|
| 1 | RGB vs JPEG vs PNG | draft by chaitanya |
| 2 | ffprobe and PTS | draft by chaitanya |
| 3 | Recommended commands | draft by chaitanya |
