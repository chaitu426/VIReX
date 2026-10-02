# Tasks — Chaitanya

**Topic:** Container format (how the `.virex` file is laid out)
**Time:** 3–4 days

## Goal
VIReX keeps the video and its meaning in one file. We must decide how that file is organised before anyone can write code to save or open it.

## What to find out
1. How are **MP4** and **Matroska** files organised (header, sections, index)?
2. Can we hide our data inside a normal MP4 so players still play it? Or do we make our own format?
3. What should our file layout be? (header, a table of sections, a checksum per section)
4. How do we handle new versions of the format later?

## What to hand in
- Your research notes (see below).
- A **decision**: custom `.virex` or MP4-based, and why.
- A simple **diagram** of the file layout.

Read first: `docs/phase-1-foundation.md` (Week 4) and `docs/tech-stack.md` (section 5).

## Full forms
| Short | Full form |
|---|---|
| MP4 | MPEG-4 Part 14 (video file format) |
| MPEG | Moving Picture Experts Group |
| ISO BMFF | ISO Base Media File Format (the "box" structure MP4 uses) |
| MKV | Matroska (an open video file format) |
| CRC32 | Cyclic Redundancy Check, 32-bit (a checksum to detect damaged data) |

## How to write your research
Save notes in `research/chaitanya/research/`.

- One file per topic, named `YYYY-MM-DD-topic.md` (for example `2026-10-01-mp4-structure.md`).
- Copy `research/TEMPLATE.md` and fill it in.
- Add the links of your sources.
- End each note with what you recommend.

## Progress
| # | Note | Status |
|---|---|---|
| 1 | MP4 and Matroska structure | done (2026-9-30-structure.md) |
| 2 | Custom data inside MP4 | done (same note) |
| 3 | `.virex` file layout | done, final in docs/virex-format-v0.1.md |
| 4 | Final decision | done: custom `.virex` |
