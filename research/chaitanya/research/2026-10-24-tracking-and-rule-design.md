# Tracking, text merging and the rule-based events

| Field | Value |
|---|---|
| **Topic** | How per-frame detections become entities, events, actions and relations, and why each default was chosen |
| **Date** | 2026-10-24 |
| **Author** | Claude, working for Chaitanya (Phase 2) |
| **Task** | Phase 2, Week 3–5 |
| **Status** | draft. Everything here is tested on **synthetic** tracks only; no real footage and no real detector output has been used yet. |

## 1. Question
How do we turn noisy, irregularly sampled detections into "one person = one entity with a lifetime", and derive events, actions and relations with simple rules?

## 2. Short answer
- **Tracker**: per-class Hungarian assignment (own implementation, checked against brute force) on a cost of IoU plus a centre-distance gate, with a half-speed constant-velocity prediction. All gaps are in **seconds**, because the sampler's frames are irregular.
- **Text**: OCR lines are merged by box overlap *and* fuzzy text similarity; the track's text is a confidence-weighted vote over all readings, so flicker like `lntro` loses to `Intro`.
- **Events/actions/relations**: deterministic rules with two-threshold hysteresis, minimum durations and gap tolerance. Rules and numbers are in `docs/phase-2-rules.md`.
- **SVIR**: lifetimes in `Entity`, delta-coded `ObjectBlock`s, KEY/DELTA `TemporalBlock`s, names as `AttributeBlock key=name` (no schema change).

## 3. Sources
- Own design and tests: `temporal/tracker`, `temporal/engine`, `temporal/graph`.
- Background (from memory, not re-read for this note): SORT (Bewley et al. 2016) for IoU + Hungarian + Kalman tracking; Visual Genome for the attribute idea; CSGCoT for the key/delta scene-graph idea already used in `svir.proto`. **Treat these as pointers, not citations.**

## 4. Findings and decisions

### 4.1 Why the tracker is not plain SORT
SORT assumes a frame every 33 ms. Here the sampler picks a frame about once per second (scene cut or `max_gap_seconds`). A walking person moves further than their own width between samples, so box overlap alone is zero and the track would break. Three changes:
| Change | Reason | Test |
|---|---|---|
| Centre-distance gate (`dist_gate` 0.6 mean diagonals) with a size-similarity check | links boxes that no longer overlap | `TestFastWalkerKeepsIdentityAcrossOneSecondGaps` |
| Prediction moves the last box along its velocity for at most 1 s at half speed | stops two people crossing from swapping ids, cannot run away from a stopped object | `TestPeopleCrossingKeepIdentity` |
| Gaps in seconds (`max_gap_seconds` 3), not frames | sampling is irregular; 3 s bridges a couple of missed samples at one sample per second | `TestMissedFramesAreBridgedButLongGapsSplit` |

`MinHits` 2 drops one-frame detections (flicker). Classes never mix: a flicker between `tv` and `laptop` makes two tracks. **That is a known weakness** to be reported honestly. No re-identification across long absences or cuts (out of scope per the phase doc); `break_on_cut` exists but is off, because a person stays the same person when the slide behind them changes.

**Sampler consequence.** With `scene_change` sampling and the Phase 1 `max_gap_seconds: 10`, tracks fragment. `samplingWarnings` prints a warning when the sampler's longest gap exceeds the tracker's, `virex-encode -semantic ml` tightens the gap to 1 s by default, and `configs/phase2.json` sets it explicitly. In the end-to-end test the sampler picked a frame every 1.02 s.

### 4.2 Text merging
OCR boxes of static text jitter by a few pixels, so IoU ≥ 0.3 is the spatial test; text similarity is `1 - edit distance / longer length` over lower-cased, punctuation-free text, ≥ 0.7. Greedy matching by score is enough because a frame has few lines. `MinHits` 2 removes most OCR noise, since noise rarely repeats in the same place. Cost: a title shown for a single sample is lost. `text_changed` fires when a new track starts within 3 s and at least 30 % box overlap after another, different text ended there; else `text_appeared`. A flicker bad enough to fall below 0.7 similarity would be reported as a change. Not measured on real OCR output.

### 4.3 Walking vs standing
Speed is net centre displacement over time divided by the person's **box height**, so it does not depend on camera distance. Defaults: start walking above 0.30 heights/s, stop below 0.15 (hysteresis); each pair's speed is smoothed over its two neighbours; stretches shorter than 1 s are absorbed by their neighbours. For a person about 1.7 m tall, 0.30 heights/s is about 0.5 m/s, a slow walk. **These numbers are reasoned, not tuned** on real footage. Other classes get `object_moved` events instead (thresholds in frame widths/s), because a phone's box jitter relative to its own size would look like walking.

### 4.4 Relations
`near`: centre distance below 1.0 (enter) or 1.4 (exit) times the sum of the two half-diagonals. `holding`: ≥ 50 % of a small object's box inside the person's box (kept down to 30 %), object area ≤ 35 % of the person's, class in a COCO list of hand-sized items. Only pairs with a person as subject are compared, to avoid a quadratic explosion of "chair near table". **2D overlap cannot tell holding from standing in front of or behind** an object: expect false positives. `pointing_to` needs pose and is **not implemented**; `standing_near` can be derived from `near` plus a `standing` action at query time.

### 4.5 Entering and leaving
`<class>_entered` / `_left` fire when the first / last sighting touches the left or right frame edge (5 % margin); `_appeared` / `_disappeared` otherwise. The bottom edge is ignored because cameras crop people there. Sightings within 2 s of the start or end of the video emit nothing for that end (the person was already there / is still there).

### 4.6 What goes into SVIR
| Output | Where | Notes |
|---|---|---|
| Lifetime | `Entity.first_seen/last_seen` | `pixel_ref` = whole lifetime, frame and box of the most confident sighting (C4) |
| Name `person_01` | `AttributeBlock key=name` | avoids changing the not-yet-signed-off schema |
| Boxes | `ObjectBlock`, delta-coded | first, last, and any box that moved/resized > 0.02 of the frame since the last kept one; `bbox_delta: 0` keeps all |
| Text | `TextBlock` per text track | `timestamp` = first reading, `pixel_ref` spans first..last reading |
| Shots | `SceneBlock label=shot_NN` | no description without a VLM |
| Graph state | `TemporalBlock` KEY every 30 s, DELTA against the last KEY | a test rebuilds the active set from KEY+DELTA and compares with ground truth |
| Layers | L0 entities/objects/attributes, L1 events/actions/relations/scenes/temporals, L2 text | by `schema.Normalize`; `Validate` rejects a block without layer or pixel_ref (C3, C4) |

## 5. Experiments
```text
go test -count=1 ./temporal/...
ok  virex/temporal/engine
ok  virex/temporal/tracker
```
Includes: Hungarian vs brute force on 300 random matrices; two people crossing; walker with sub-overlap steps; flicker dropped; a 30 s scripted lecture (person walks in, stands, holds a phone, walks out; slide text changes at a scene cut) producing `person_entered`, `person_left`, `scene_changed`, `text_appeared`, `text_changed`, actions walking/standing/walking, `holding`, and a valid, byte-for-byte reproducible SVIR.

## 6. Decision / recommendation
Keep the rules as the reproducible baseline and **tune them on real labelled clips** (Phase 2 Week 6). Report ID switches and false events honestly. Next measurable steps: ID-switch count and fragmentation per clip, precision/recall of event types against the ground truth in `tests/testdata/ground-truth/README.md`.

## 7. Open questions
1. Every threshold above is untuned on real footage.
2. Should `object_moved` and `holding` be restricted further (they will be the noisiest rules)?
3. A scene cut that only changes a slide currently produces `scene_changed`; the paper may want a separate `slide_changed` (needs the text rule combined with the cut).

## 8. Glossary
- **IoU**: Intersection over Union (box overlap, 0..1).
- **Hungarian algorithm**: optimal one-to-one assignment (Kuhn–Munkres).
- **SORT**: Simple Online and Realtime Tracking.
- **Hysteresis**: two thresholds, one to switch on and a looser one to switch off, so a value hovering at the limit does not flicker.
- **KEY / DELTA**: full snapshot / changes against the last snapshot.
- **OCR**: Optical Character Recognition. **C3 / C4**: paper contributions (layers / pixel references).
