# Phase 2 demo: what was built, with what, why, and how it is tested

Date: 2026-10-24. Read this first; details are in `docs/phase-2-summary.md` (status), `docs/phase-2-rules.md` (rule numbers) and `research/chaitanya/research/2026-10-24-*.md` (findings).

## 0. The idea in one picture

```text
video.mp4
   |
   +--> pixel pipeline (Phase 1, FFmpeg/x264) ------------------------------+
   |                                                                        v
   +--> decode + sampler (Phase 1) --> semantic/analyze --gRPC--> [detector container :50051  YOLO]
                                        (parallel, ordered)  \--> [ocr container      :50052  EasyOCR]
                                              |
                                              v
                              temporal/engine: tracker + text tracker + rules
                                              |
                                              v
                              temporal/graph --> SVIR --> .virex semantic section
```
Both halves (pixels and meaning) run at the same time and end up in one `.virex` file.

## 1. Step by step

### Step 1 - The contract between Go and Python (`proto/ml/v1/ml.proto`)
- **What:** two gRPC services: `ObjectDetector` (`Detect`, `Info`) and `TextReader` (`Read`, `Info`). A frame is sent as raw RGB24 (JPEG optional). Boxes come back normalised 0..1, the same convention as SVIR.
- **Used:** Protobuf + gRPC (`protoc`, `protoc-gen-go`, `protoc-gen-go-grpc`).
- **Why:** the tech-stack doc chose gRPC. Two services (not one) so OCR (slow) and detection (fast) can be scaled, switched off or moved to a GPU separately. Raw RGB because it has no codec cost and no quality loss (OCR needs sharp text). Every reply carries the model name and version, for the cost log and file metadata.
- **How:** Go stubs are generated and committed; Python stubs are generated from the same file at build time, so the two sides cannot drift.

### Step 2 - The Python ML service (`ml-service/`)
- **What:** a gRPC server hosting the detector and/or OCR depending on `VIREX_ROLE` (`detector`, `ocr`, `all`), a small HTTP API (`/health`, `/info`, `/detect`, `/ocr`) for debugging with curl, and model backends: Ultralytics YOLO (`yolov8n`), EasyOCR, optional PaddleOCR, and fake backends (find red/blue/green rectangles).
- **Used:** Python, `grpcio`, FastAPI, NumPy, Pillow, Ultralytics, EasyOCR.
- **Why:** the models are swappable by environment variable (`VIREX_DETECTOR_MODEL`, `VIREX_OCR_BACKEND`), so Go never changes when a model does. Fake backends let the whole pipeline be tested without torch or weights. Models load before the port opens, so "healthy" means "ready". Each model is used one call at a time (not thread safe); invalid images return `INVALID_ARGUMENT`, a wrong container returns `UNIMPLEMENTED`.
- **How:** `app/config.py` (env), `app/core.py` (shared logic), `app/grpc_server.py`, `app/http_app.py`, `app/backends/*`, `app/main.py`.

### Step 3 - Docker (`ml-service/Dockerfile`, `docker-compose*.yml`)
- **What:** one image, started twice: `detector` (:50051) and `ocr` (:50052). Overrides: `docker-compose.gpu.yml` (CUDA torch) and `docker-compose.fake.yml` (test doubles, starts in seconds).
- **Why:** you asked for the ML services in containers. One image keeps builds simple; CPU-only torch keeps it small; weights are baked in so containers start offline and reproducibly; a non-root user, health check, memory limit and ports bound to 127.0.0.1 are safe defaults.
- **How:** build context is the repo root (the `.proto` is shared). The build fails loudly if the YOLO weights are not where expected.
- **Status:** the three compose files pass `docker compose config`. **The image has not been built** (no Docker daemon here).

### Step 4 - Go transport (`semantic/mlclient`)
- **What:** connections, image encoding (RGB or JPEG), per-attempt timeout, retries with exponential backoff and jitter.
- **Why:** retry only what can succeed again (`UNAVAILABLE`, `DEADLINE_EXCEEDED`, `ABORTED`, `RESOURCE_EXHAUSTED`); never retry bad input. A stopped container fails the encode in about 20 s with "unreachable... try `docker compose ps`" instead of after minutes of work; a container that is up but still loading is waited for.
- **How:** `Conn.Do` wraps every call; `openML` in `codec/pipeline` checks readiness before any video work.

### Step 5 - Typed clients (`semantic/objects`, `semantic/ocr`)
- **What:** `Detector` and `Reader` interfaces with gRPC implementations returning Go types (class, confidence, box, model info, call statistics).
- **Why:** the rest of the code depends on interfaces, so tests and other models can substitute their own.

### Step 6 - Parallel, ordered analysis (`semantic/analyze`)
- **What:** a worker pool that analyses sampled frames in parallel and hands results on **in frame order**. Detection and OCR of one frame run side by side.
- **Why/how:** bounded queues keep Phase 1's promise of bounded memory however long the video is (`Submit` blocks when full). Errors cancel the run; by default one failed frame aborts the encode (a silently incomplete SVIR is worse than none), `max_frame_errors` can allow a few.
- **OCR reuse:** a lecture slide stays up for minutes while frames keep being sampled and OCR is the slowest call. A frame whose every cell of a 48x27 brightness grid moved less than 6 levels since the last OCR'd frame reuses its text. The test uses the worst cell, not the average, so a new bullet point always triggers OCR.
- **Cost log (C1):** one record per ML call with model name and version; skipped OCR is logged with 0 calls.

### Step 7 - Tracking (`temporal/tracker`)
- **What:** an object tracker (per-class Hungarian assignment, IoU + centre-distance gate, half-speed motion prediction, gaps in seconds, `min_hits` against flicker) and a text tracker (box overlap + fuzzy text similarity, confidence-weighted vote over readings).
- **Why:** the sampler gives about one frame per second, so plain SORT-style overlap would break a walking person's track. Gaps are in seconds because sampling is irregular. The Hungarian solver is our own code, checked against brute force.
- **Result:** a person becomes one entity `person_01` with first/last seen, not one detection per frame.

### Step 8 - Rules (`temporal/engine`)
- **What:** deterministic events (`person_entered/left`, `_appeared/_disappeared`, `scene_changed`, `text_appeared`, `text_changed`, `object_moved`), actions (`walking`, `standing`), relations (`near`, `holding`). Numbers: `docs/phase-2-rules.md`.
- **Why:** the phase doc says keep it rule based. Two thresholds (hysteresis), minimum durations and gap tolerance stop values hovering at a limit from flickering.
- **Not done:** `pointing_to` and pose actions (need a pose model).

### Step 9 - Graph and SVIR (`temporal/graph`)
- **What:** an in-memory graph (entities, events, actions, relations, shots, texts) with per-entity timelines, then SVIR generation.
- **How:** lifetimes in `Entity`; readable names as `AttributeBlock name=person_01` (no schema change); boxes delta-coded (only changes above 0.02 kept); text blocks span first..last reading; shots as `SceneBlock`; `TemporalBlock` KEY snapshots plus DELTA blocks. Every block gets its layer (C3) and a `pixel_ref` with time range, frame and box (C4); `schema.Validate` rejects anything else.

### Step 10 - Pipeline and CLI (`codec/pipeline`, `cmd/virex-encode`)
- **What:** `-semantic ml` runs the semantic pass **concurrently** with the pixel pipeline; the pass replaces the old sampling pass. The file metadata now lists the models, pass statistics and engine config. Flags: `-ml-detect`, `-ml-ocr`, `-ml-workers`, `-ml-no-ocr`, `-ml-no-detect`; full settings in `configs/phase2.json`.
- **Safety:** a warning appears if the sampler's longest gap exceeds what the tracker can bridge; `-semantic ml` tightens the sampler gap to 1 s by default.

### Step 11 - Docs and research
`docs/phase-2-summary.md`, `docs/phase-2-rules.md`, `tests/testdata/ground-truth/README.md` (label and benchmark-question format, C2) and two research notes in `research/chaitanya/research/`.

## 2. Try it

```text
# Python tests (fake backends, no Docker, no models)
pip install -r ml-service/requirements-dev.txt
pytest ml-service/tests

# Go tests
go test ./...

# Plumbing check with fake models in Docker (on the Docker machine)
docker compose -f docker-compose.yml -f docker-compose.fake.yml up -d --build
docker compose ps
go build -o bin/ ./cmd/...
bin/virex-encode clip.mp4 -semantic ml -cost-log cost.jsonl
bin/virex-decode clip.virex -svir-json -

# Real models
docker compose down && docker compose up -d --build
bin/virex-encode clip.mp4 -config configs/phase2.json -cost-log cost.jsonl
```
The fake backends only see solid red/blue/green rectangles, so use them with a generated clip (`codec/pipeline/semantic_test.go` has the ffmpeg recipe), not real footage.

## 3. Tests run so far and what they prove

| Suite | Tests | Result | What it proves |
|---|---|---|---|
| `ml-service/tests` (pytest, fake backends, Python 3.14) | 17 (14 functions, some parametrised) | all pass | image decoding and rejection of bad input; gRPC detect/read with RGB and JPEG; class filter; `INVALID_ARGUMENT`; `UNIMPLEMENTED` on a wrong role; gRPC health; HTTP `/health`, `/detect`, `/ocr`; config validation |
| `semantic/mlclient` | 11 | pass | real gRPC round trip; JPEG payload is smaller; retry then success; give up after retries; bad input not retried; per-attempt timeout; cancel stops retrying; helpful errors for a down container and a wrong role |
| `semantic/analyze` | 9 | pass | results in frame order despite jittery latency; at most `workers` calls in flight; unchanged slides reuse OCR but a new line of text is read; reuse can be disabled; failures fatal by default, tolerable on request; handler error stops the run; cancel does not hang |
| `temporal/tracker` | 22 | pass | Hungarian equals brute force on 300 random cases; one person = one entity; fast walker and crossing people keep identity; flicker dropped; gaps bridged or split; OCR flicker merged into one text track; text change starts a new track |
| `temporal/engine` | 16 | pass | a scripted 30 s lecture gives the expected events, actions and relations; SVIR valid and byte-for-byte reproducible; delta coding drops jitter but keeps the lifetime; KEY+DELTA rebuild the true active set; relation hysteresis and hold rules |
| `codec/pipeline` | 8 (6 semantic + 2 config) | pass | real ffmpeg clip -> real gRPC -> `.virex` -> read back: 1 person entity (3.07-10.24 s), `person_entered`, `scene_changed`, `text_appeared`, `holding`; one cost record per ML call and the server saw the same call counts; split layers merge back valid; fails fast when ML is down; waits for a model still loading; sparse sampling warns |
| Phase 1 suites (`container`, `encoder`, `pixel`, `sampler`, `scheduler`, `cost`, `svir/schema`) | existing | all pass in the full `go test ./...` run | no regression |
| Cross-language run (manual) | 1 | worked | real Python gRPC server + real Go `virex-encode -semantic ml` on a generated clip: 1 person entity, `person_entered`, `person_disappeared`, `text_appeared`; 14 detect calls, 8 OCR calls, 6 reused |
| `docker compose config` | 3 files | valid | the compose files parse |

Note: `go test ./...` was run in full before the last few small edits; afterwards `go vet ./...` and the two config tests were re-run.

## 4. Expectations: what should happen on the Docker machine

| Check | Expected | Why it might not hold |
|---|---|---|
| `pytest ml-service/tests` | 17 pass | needs PyPI access; image uses Python 3.11 |
| `docker compose build` | succeeds, about 2 GB download | pip resolver conflict (requirements are ranges, not pins), weights saved elsewhere (the build guard fails loudly), EasyOCR download |
| fake override up, `docker compose ps` | both `healthy` within about a minute | port clash on 50051/50052 |
| `virex-encode ... -semantic ml` with fake models on the generated clip | the same result as the manual run above | |
| real models, `curl .../detect` on a frame with a person | a `person` detection with confidence | CPU speed unknown |
| real models, a real lecture clip | entities with lifetimes, text blocks, at least 3 event types, the cost log has one record per call | thresholds are untuned; expect some wrong events |
| speed | unknown. YOLOv8n should be tens of ms per frame on CPU; EasyOCR is probably seconds per frame, which is why OCR reuse exists. Measure it from `cost.jsonl` | |

## 5. Honest limits
- Real models, the Docker image and real footage have not been run, so accuracy is unknown and every threshold is a reasoned guess.
- Tracker weaknesses: a class flicker (`tv`/`laptop`) splits a track; no re-identification after long absences; `holding` is 2D overlap and cannot tell in front of from in hand.
- Ground-truth labels and benchmark questions are not made (they need real footage); only the format exists.
- Week 7 stretch (VLM captions, embeddings, pose) is not started.
- The gRPC port has no authentication or TLS; compose binds it to 127.0.0.1.
- Ultralytics is AGPL-3.0; check before distributing the image.
- Changes to Phase 1 files: `cost.Record` got an optional `model` field, `go.mod` is now `go 1.24.0`, `pipeline.Config` got `ml` and `engine`. The SVIR schema is unchanged.
