# Phase 2 Summary (logbook)

**Date:** 2026-10-24. **Scope:** `docs/phase-2-semantic-extraction.md`, with the ML models in Docker containers. **Result:** everything is written. The Go side is tested end to end against a fake ML server, and the Python service passes its 17 tests (fake backends) and was run together with the real Go encoder over gRPC. **The Docker images and the real models (YOLO, EasyOCR) have never been built or run** (the dev machine has no Docker daemon). First job on the Docker machine: section 4.

## 1. Deliverables

| # | Deliverable | Status | Where |
|---|---|---|---|
| 1 | Python ML service with `/detect` and `/ocr` | Done with fake backends: 17 pytest tests pass, and the real Go encoder ran against it. Real models and Docker **not run**. gRPC is the main API; HTTP `/health`, `/info`, `/detect`, `/ocr` for debugging. Fake backends for testing without models | `ml-service/` |
| 2 | gRPC contract Go ⇄ Python | Done. Go stubs committed and generated; Python stubs generated at build time from the same file | `proto/ml/v1/ml.proto` |
| 3 | Go semantic client, frames in parallel | Done, tested over real gRPC | `semantic/mlclient`, `objects`, `ocr`, `analyze` |
| 4 | Entity builder and tracker | Done, tested (synthetic) | `temporal/tracker` |
| 5 | Action and event generator | Done: 6 entity/text/scene event kinds plus `object_moved`, `walking`/`standing`. Pose actions (`writing`, `pointing`, `raising_hand`) not done (stretch) | `temporal/engine` |
| 6 | Relation detector | Done: `near`, `holding`. `pointing_to` not done (needs pose) | `temporal/engine/relations.go` |
| 7 | Temporal semantic engine | Done: lifetimes, bbox delta coding, KEY/DELTA blocks | `temporal/engine`, `temporal/graph` |
| 8 | Semantic graph builder | Done: entities, events, actions, relations, shots, per-entity timeline, `ActiveAt` | `temporal/graph` |
| 9 | SVIR generation from a real clip | Done, tested with a real ffmpeg clip and the fake ML server. **Not yet run with real models** | `codec/pipeline/semantic.go` |
| 10 | Ground-truth labels for 2–3 clips | **Not done.** Needs real footage. Format and the benchmark-question format (C2) are fixed | `tests/testdata/ground-truth/README.md` |
| C1 | Every ML call in the cost log | Done: one record per call with model name and version; OCR skipped for unchanged frames is logged with 0 calls | `cost`, `semantic/analyze` |
| C2 | Question format | Format only (see 10) | ground-truth README |
| C3 | Layer on every block | Done, enforced by `schema.Validate` | `temporal/graph/svir.go` |
| C4 | `pixel_ref` kept through merging | Done: entity = lifetime + most confident frame and box; text = first..last reading + best frame | `temporal/graph/svir.go` |
| Week 7 | VLM captions, embeddings | Not started (stretch) | |

## 2. Exit criteria
| Criterion | Evidence |
|---|---|
| `virex-encode clip.mp4` produces a `.virex` with real SVIR | `TestEncodeWithML` (fake models). **Real models: not yet run** |
| A person becomes one entity with a lifetime | same test: 1 person entity, 3.07 s to 10.24 s, 6 object blocks (the box changes while walking) instead of one per sampled frame |
| At least three event types | the test produces six |
| ML service can be swapped without touching Go | by construction (env vars; two typed services). Swap not yet tried |
| Ground truth for 2 clips; 8+ questions each | **Not met** |
| Every block has layer and pixel_ref; every ML call in the cost log | `schema.Validate`; cost-log count in `TestEncodeWithML` |

## 3. What was built (short)
- **Contract**: `ObjectDetector.Detect/Info`, `TextReader.Read/Info`; raw RGB24 frames (JPEG optional); normalised boxes; model info on every reply.
- **Containers**: one `ml-service/Dockerfile` (build context = repo root, CPU torch by default, weights baked in, non-root user, health check on `/health`); `docker-compose.yml` starts `detector` (:50051) and `ocr` (:50052); overrides `docker-compose.gpu.yml` and `docker-compose.fake.yml`.
- **Go**: retries only transient errors, per-attempt timeouts, fail-fast with a helpful message when a container is down or on the wrong port; ordered parallel analysis with bounded memory; OCR skipped for unchanged frames; the semantic pass runs **at the same time** as the pixel pipeline (Week 6 item) and replaces the separate sampling pass.
- **Config**: `configs/phase2.json`; CLI `-semantic ml`, `-ml-detect`, `-ml-ocr`, `-ml-workers`, `-ml-no-ocr`, `-ml-no-detect`. The container metadata now lists the models used, the pass statistics and the engine config.
- **Changes to Phase 1 files**: `cost.Record` got an optional `model` field; `go.mod` is now `go 1.24.0` (gRPC needs it); `pipeline.Config` got `ml` and `engine`; the tool version is `virex-encode 0.2`. The SVIR schema is **unchanged**.

## 4. Run it on the Docker machine

```text
# 0. Python tests first: no models, no Docker (needs PyPI access)
pip install -r ml-service/requirements-dev.txt
pytest ml-service/tests

# 1. Prove the plumbing with the fake models (starts in seconds)
docker compose -f docker-compose.yml -f docker-compose.fake.yml up -d --build
docker compose ps                     # both "healthy"
go build -o bin/ ./cmd/...
bin/virex-encode tests/testdata/clips/Semantic_Video_Codec.mp4 -semantic ml -cost-log cost.jsonl
docker compose down

# 2. Real models (first build downloads ~2 GB)
docker compose build
docker compose up -d
curl -s -X POST --data-binary @frame.jpg -H "Content-Type: image/jpeg" localhost:8001/detect
curl -s -X POST --data-binary @frame.jpg -H "Content-Type: image/jpeg" localhost:8002/ocr
bin/virex-encode clip.mp4 -config configs/phase2.json -cost-log cost.jsonl
bin/virex-decode clip.virex -svir-json -
```
Note: the fake backends only find red/blue/green rectangles, so a real clip gives empty results with them; use a generated clip or `go test ./codec/pipeline/` for that check.

## 5. Things to settle on the Docker machine (all unverified)
1. `pytest ml-service/tests`, then `docker compose build`. Likely first failures: a pip resolver conflict (requirements are ranges, not pins; pin them once it works), the `test -f /models/yolov8n.pt` build guard (weights saved elsewhere), the EasyOCR model download.
2. Real speed: ms per frame for YOLOv8n and EasyOCR on CPU (the cost log has it), then the right sampling rate and `workers`. Each model is serialised inside its container, so more `workers` only overlap network time; scale throughput with more containers or a GPU.
3. Whether EasyOCR wants RGB or BGR numpy input (the service sends BGR).
4. The PaddleOCR backend, if wanted.
5. A look at the real output: do `person_entered`, `holding` and `text_changed` fire sensibly? Tune `engine.*`.
6. The static-frame OCR threshold (`ml.static_levels`) on a real lecture.

## 6. Not done, or needs a decision
1. Ground truth, benchmark questions, accuracy numbers: need real footage and hand labelling.
2. Week 7 stretch: VLM captions, embeddings, pose.
3. Tracker weaknesses: class flips split tracks, no re-identification after long absences or cuts, `holding` cannot tell in front of from in hand.
4. Unit tests exist for Go only; there is no gRPC test of the real Python server from Go (needs both running).
5. The Python gRPC port has no authentication or TLS; compose binds it to 127.0.0.1.
6. The Go module requirement moved from 1.23 to 1.24.
7. Ultralytics is AGPL-3.0; check before distributing the image.
8. Research notes written by Claude, draft, need a human review: `research/chaitanya/research/2026-10-24-*.md`.
