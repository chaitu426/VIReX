# Phase 2 rule reference

Every rule is deterministic: the same frames give the same SVIR. Defaults below are the values in `configs/phase2.json`; all are tunable under `engine` in the config. **None has been tuned on real footage yet** (see `research/chaitanya/research/2026-10-24-tracking-and-rule-design.md`).

## Tracking (`engine.tracker`, `engine.text`)
| Rule | Default |
|---|---|
| Detection joins a track if IoU ≥ `match_iou`, or the centre moved ≤ `dist_gate` mean diagonals and the size is within 2.5x | 0.3 / 0.6 |
| A track survives `max_gap_seconds` without a detection | 3 s |
| A track needs `min_hits` sightings to exist | 2 |
| Text reading joins a text track if box IoU ≥ `min_iou` and text similarity ≥ `min_similarity` | 0.3 / 0.7 |
| Text track survives `max_gap_seconds`; needs `min_hits` readings and `min_chars` characters | 2 s / 2 / 2 |

## Events
| Event type | Fires when |
|---|---|
| `<class>_entered` | a track's first sighting touches the left/right frame edge (`edge_margin` 0.05, `edge_sides` "lr") and is more than `boundary_seconds` (2) after the start of the video |
| `<class>_left` | same for the last sighting, more than 2 s before the end |
| `<class>_appeared` / `_disappeared` | same, but away from the edge (turn off with `no_appear_events`) |
| `scene_changed` | the sampler picked the frame because of a cut |
| `text_appeared` | a confirmed text track starts |
| `text_changed` | a text track starts ≤ 3 s after a different text ended in the same place (IoU ≥ 0.3) |
| `object_moved` | a non-person object moves ≥ 0.03 of the frame during a moving stretch |

`<class>` is the class name with spaces as underscores (`person`, `cell_phone`).

## Actions (classes in `motion.classes`, default `person`)
`walking` / `standing` from smoothed speed in body heights per second: start walking ≥ 0.30, stop < 0.15, stretches shorter than 1 s are absorbed. Confidence is the mean detector confidence over the stretch.

## Relations (subject must be in `relations.subjects`, default `person`)
| Relation | Rule |
|---|---|
| `near` | centre distance ≤ 1.0 (start) / 1.4 (keep) x the two half-diagonals added; reported once per pair (lower entity id is subject) |
| `holding` | person and a hand-sized COCO class (`hold_classes`); ≥ 50 % (start) / 30 % (keep) of the object's box inside the person's; object area ≤ 35 % of the person's |
| `pointing_to` | **not implemented** (needs pose) |

A relation needs 2 sightings and survives 3 s of failing or missing pair. `standing_near` can be derived at query time (`near` while the subject has a `standing` action).

## SVIR layout
Entities carry the lifetime; `AttributeBlock name=person_01` carries the readable id; boxes are delta-coded (`svir.bbox_delta` 0.02); shots are `SceneBlock shot_NN`; `TemporalBlock` KEY every `svir.key_interval_seconds` (30) with DELTA blocks against it. Layers: L0 entities/objects/attributes, L1 events/actions/relations/scenes/temporals, L2 text, L3 reserved for Phase 2 Week 7 (embeddings, captions).
