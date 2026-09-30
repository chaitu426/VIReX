# Tasks — Deep

**Topic:** Data schema (how we describe what is in a video)
**Time:** 2–3 days

## Goal
VIReX turns a video into facts: objects, text, actions and events, each with a time. The **schema** is the exact list of fields for those facts. We write it in **Protobuf**. After week 3 we can only add fields, never change or remove them, so we want to learn from others first.

## What to find out
**Part 1 — How do others describe video content?**
Look at each of these and note what fields they store, and how they store **time**, **position** and **links between things**:
1. MPEG-7
2. Visual Genome (scene graphs)
3. COCO
4. AVA
5. Key-SG and Delta-SG in the CSGCoT paper: https://doi.org/10.1145/3746027.3754765

**Part 2 — Protobuf rules for safe changes**
1. What happens if a field number is changed or reused?
2. How do optional fields and default values work?
3. How do enums work, and how do we add new values later?
4. What is `reserved` for?
5. Make a small table: which changes are safe, which break old files.

## What to hand in
- Your research notes (see below).
- A **comparison table** of the schemas from Part 1.
- The **safe-changes table** from Part 2.
- A short **recommendation**: what should we copy into our schema?

Read first: `docs/phase-1-foundation.md` (Week 3).

## Full forms
| Short | Full form |
|---|---|
| SVIR | our semantic data format (see `docs/`) |
| Protobuf | Protocol Buffers (a format for defining structured data) |
| MPEG-7 | Multimedia Content Description Interface (a standard for describing video) |
| COCO | Common Objects in Context (an image dataset) |
| AVA | Atomic Visual Actions (a dataset of actions in video) |
| CSGCoT | name of the paper's method; find its meaning in the paper and write it here |
| Key-SG | Key-frame Scene Graph |
| Delta-SG | Delta-frame Scene Graph (only the changes) |
| bbox | bounding box (a rectangle around an object) |

## How to write your research
Save notes in `research/deep/research/`.

- One file per topic, named `YYYY-MM-DD-topic.md` (for example `2026-10-01-coco-annotations.md`).
- Copy `research/TEMPLATE.md` and fill it in.
- Add the links of your sources.
- End each note with what you recommend.

## Progress
| # | Note | Status |
|---|---|---|
| 1 | MPEG-7 | not started |
| 2 | Visual Genome and scene graphs | not started |
| 3 | COCO and AVA | not started |
| 4 | Key-SG and Delta-SG | not started |
| 5 | Protobuf safe changes | not started |
| 6 | Final recommendation | not started |
