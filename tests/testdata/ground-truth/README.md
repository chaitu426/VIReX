# Ground truth and benchmark questions (Phase 2, deliverable 10 and C2)

**Status: format only. No clip has been labelled yet**; labelling needs real footage (a lecture, a screen recording, a street or indoor clip with people entering and leaving), which the repo does not have.

One pair of files per clip, named after the clip: `<clip>.truth.json` and `<clip>.questions.json`.

## `<clip>.truth.json`
Times are seconds of video time (the same clock as the sampled frames).

```json
{
  "clip": "lecture01.mp4",
  "objects": [
    {"time": 12.0, "class": "person", "box": {"x": 0.10, "y": 0.20, "w": 0.30, "h": 0.70}}
  ],
  "tracks": [
    {"name": "person_01", "class": "person", "first_seen": 4.0, "last_seen": 301.5}
  ],
  "text": [
    {"start": 10.0, "end": 95.0, "content": "Intro to Compression"}
  ],
  "events": [
    {"type": "person_entered", "time": 4.0},
    {"type": "text_changed", "time": 95.0}
  ]
}
```
Label `objects` at the sampled frames (the encoder's metadata lists them: `virex-decode clip.virex -info`), only for classes the detector knows (COCO). `tracks` is who is who over time, to count ID switches. Use the event types of `docs/phase-2-rules.md`.

## `<clip>.questions.json` (8 to 15 per clip)
```json
[
  {
    "id": "q01",
    "question": "What is the title of the first slide?",
    "answer": "Intro to Compression",
    "evidence_time_ranges": [[10.0, 95.0]],
    "type": "text",
    "required_layers": ["L2"]
  }
]
```
`type` is one of `object`, `text`, `event`, `temporal-order`, `counting`, `summary`, `needs-pixels`. `required_layers` lists which of L0..L3 should be enough to answer. Include a few `needs-pixels` questions on purpose: they show where semantic data is not enough.

## What gets measured from these (Phase 2 Week 6, Phase 4)
Detection precision and recall at sampled frames (IoU ≥ 0.5), OCR character accuracy, event time error, ID switches per track, question accuracy per `type` and per layer.
