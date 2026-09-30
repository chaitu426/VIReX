# Phase 2 — Semantic Extraction and Temporal Engine

**Project:** VIReX
**Suggested duration:** 6–7 weeks (the heaviest phase)
**Phase goal:** Turn sampled frames into a structured, time-aware SVIR: objects, text, persistent entities, actions, events and relations. This is the "semantic encoder" half of the project.

---

## 1. Scope

Follow the MVP from the project document. **Do not** try all modalities.

| In scope (must) | Stretch (only if ahead) | Out of scope |
|---|---|---|
| Object detection (YOLO) | Scene understanding (VLM) | Audio and speech |
| OCR | Pose and gesture | Learned or neural pixel codec |
| Entity tracking | Embeddings for the vector index (needed in Phase 4 if semantic search uses vectors) | Re-identification across cuts |
| Rule-based actions and events | Simple relations | Training your own models |
| Temporal engine | | |

---

## 2. Deliverables

| # | Deliverable | Done when |
|---|---|---|
| 1 | Python ML service with `/detect` and `/ocr` | Accepts an image and returns JSON |
| 2 | gRPC contract between Go and Python (`.proto`) | Go calls Python with a frame and gets detections |
| 3 | Go semantic client (`semantic/objects`, `semantic/ocr`) | Sampled frames are processed in parallel |
| 4 | Entity Builder and Tracker (`temporal/tracker`) | Detections get persistent IDs such as `person_01` |
| 5 | Action and event generator (rule-based) | Emits `ActionBlock` and `EventBlock` with time ranges |
| 6 | Relation detector (simple geometry) | Emits e.g. `standing_near`, `holding` from bbox overlap and distance |
| 7 | Temporal Semantic Engine | Per-frame detections become lifetimes and deltas |
| 8 | Semantic graph builder | Entities, events and relations linked in memory |
| 9 | SVIR generation | Encoder writes a real SVIR for each test clip |
| 10 | Ground-truth labels for 2–3 clips | Used for accuracy metrics in Phase 4 |

---

## 3. Tasks in order

### Week 1 — ML service
- [ ] Set up the Python service (FastAPI first for speed, gRPC after).
- [ ] Load a pretrained YOLO model (Ultralytics YOLOv8 or v11), return `class, bbox, confidence`.
- [ ] Add OCR (PaddleOCR or EasyOCR), return `text, bbox, confidence`.
- [ ] Add a `/health` endpoint and model-version reporting (goes into the container metadata).
- [ ] Benchmark speed on CPU and, if available, GPU. Pick the sampling rate accordingly.

### Week 2 — Go ⇄ Python integration
- [ ] Define `ml.proto` (`DetectObjects`, `RunOCR`). Generate the Go and Python stubs.
- [ ] Go client with timeout, retry, and a worker pool for parallel frames.
- [ ] Map ML output to the SVIR `ObjectBlock` and `TextBlock`.
- [ ] **[C4]** Fill `pixel_ref` on every block: the source frame id, the time range, and the bbox. When tracking merges blocks in Week 3, keep the list of source refs (or the first, last and a representative frame).
- [ ] **[C1]** Send every ML call through the cost logger: latency, model name and version, and for a VLM the tokens used.
- [ ] Run end to end on one clip and dump JSON.

### Week 3 — Tracking and entities
- [ ] Implement a simple tracker. Start with IoU matching plus a Hungarian or greedy assignment, then optionally move to a SORT-style Kalman filter.
- [ ] Entity lifetime: `first_seen`, `last_seen`, with tolerance for a few missed frames.
- [ ] Text tracking: merge repeated OCR results into one `TextBlock` with a time range. Use fuzzy string match, since OCR flickers.
- [ ] Test on a clip with people entering and leaving.

### Week 4 — Actions, events, relations
- [ ] Rule-based events from tracks, for example:
  - `person_entered` / `person_left` (track starts or ends near the frame edge)
  - `scene_changed` (from the sampler)
  - `text_appeared` / `text_changed` (from OCR tracking)
  - `object_moved` (bbox displacement above a threshold)
- [ ] Rule-based actions from motion: `walking` vs `standing` (centroid velocity).
- [ ] Stretch: pose model, then `writing`, `pointing`, `raising_hand`.
- [ ] Relations from geometry: `near` (distance), `holding` (person and object overlap), `pointing_to`.
- [ ] Every event has: type, start, end, entities, region, confidence.

### Week 5 — Temporal engine and graph
- [ ] Temporal dedup: collapse repeated per-frame data into lifetimes.
- [ ] Delta encoding at the semantic level: store only attribute or bbox changes above a threshold.
- [ ] Timeline generation: an ordered list of events per entity.
- [ ] Build the in-memory semantic graph (entity nodes, event and relation edges).
- [ ] **[C3]** Assign each emitted block to its layer: objects and entities to L0, events, actions, relations and scenes to L1, OCR text to L2, embeddings and VLM captions to L3. Validate that every block has a layer.
- [ ] Emit final SVIR and validate it against the `.proto`.

### Week 6 — Pipeline integration and labels
- [ ] Wire into `virex-encode`: the pixel pipeline and semantic pipeline run concurrently and both write into the container.
- [ ] Hand-label ground truth for 2–3 clips (objects at sampled frames, on-screen text, event times). A simple CSV or JSON is enough.
- [ ] Run a first accuracy check (detection precision and recall, OCR character accuracy, event time error).
- [ ] **[C2]** Design the benchmark question format now, while labelling: for each clip write **8–15 questions** (not 1–2), each with `question`, `answer`, `evidence_time_ranges`, `type` (object, text, event, temporal-order, counting, summary, needs-pixels) and `required_layers` (which of L0–L3 should suffice). Store them next to the ground truth, one file per clip.

### Week 7 (buffer / stretch)
- [ ] VLM scene descriptions (LLaVA, Qwen-VL or a hosted API) per scene change.
- [ ] Embedding model (CLIP or a text embedder) for later vector search.
- [ ] Tune thresholds; write up findings.

---

## 4. Exit criteria

- `virex-encode clip.mp4` produces an `.virex` with real SVIR in the semantic section.
- A person in a clip becomes **one entity with a lifetime**, not one detection per frame.
- SVIR JSON for the lecture clip contains recognisable text, objects and at least three event types.
- ML service can be swapped (change the model) without touching Go code.
- Ground truth exists for at least 2 clips.
- Every block has a `layer` (C3) and a `pixel_ref` (C4), and every ML call is in the cost log (C1).
- At least 2 clips have 8+ benchmark questions each with evidence ranges and question types (C2).

## 4a. Paper contributions in this phase

| ID | What to do here |
|---|---|
| C1 | Log time and tokens for every ML call |
| C2 | Write multi-question ground truth with types and evidence ranges |
| C3 | Tag each block with its layer |
| C4 | Populate `pixel_ref` and keep it through tracking and merging |

## 5. Risks and mitigations

| Risk | Mitigation |
|---|---|
| ML too slow on CPU | Lower sampling rate, smaller YOLO model, batch requests, run workers in parallel |
| Track ID switches | Tune IoU threshold, allow gap tolerance, accept imperfect tracking and report it honestly |
| OCR flicker creates noisy text | Fuzzy merge and require N consecutive detections |
| Action recognition too ambitious | Keep rule-based; treat learned action models as future work |
| Python dependency pain on Windows | Use a venv or Docker for the ML service |

## 6. Hand-off to Phase 3

Phase 3 receives: a working semantic encoder that produces a correct, time-aware SVIR per video, plus ground-truth labels.
