# ML containers and the Go ⇄ Python contract

| Field | Value |
|---|---|
| **Topic** | How the Go core talks to the Python ML models when the models run in Docker containers |
| **Date** | 2026-10-24 |
| **Author** | Claude, working for Chaitanya (Phase 2) |
| **Task** | Phase 2, Week 1–2 (`docs/phase-2-semantic-extraction.md`) |
| **Status** | draft. The Go side and the Python service (with fake backends) are tested, including Go encoder -> real Python gRPC server. The Docker images and the real models were **never built or run** (no Docker daemon). See section 7. |

## 1. Question
How should the Python ML service be shaped so that (a) it runs in Docker, (b) models can be swapped without touching Go, (c) frames travel cheaply, (d) every ML call can be logged for the cost study (C1)?

## 2. Short answer
- **Two gRPC services, one image, two containers**: `ObjectDetector` (detector container, port 50051) and `TextReader` (OCR container, port 50052). Same Dockerfile, different `VIREX_ROLE`. A third role `all` serves both from one container.
- **Raw RGB24 frames over gRPC** by default (JPEG optional). Boxes come back normalised 0..1, the same convention as SVIR, so Go copies them across.
- **Which model runs is environment, not code**: `VIREX_DETECTOR_BACKEND`, `VIREX_DETECTOR_MODEL`, `VIREX_OCR_BACKEND` in `docker-compose.yml`. The Go side only ever sees the `.proto`.
- The Go client retries only transient failures, bounds every attempt with a timeout, and fails the encode in seconds if a container is down.

## 3. Sources
- [Ultralytics predict docs](https://docs.ultralytics.com/modes/predict) — read 2026-10-24 — numpy input is HWC uint8; arrays are treated as **BGR** (also confirmed by an Ultralytics staff answer on community.ultralytics.com), so the service converts RGB→BGR before `predict`.
- [EasyOCR 1.1.9 on PyPI](https://pypi.org/project/easyocr/1.1.9/) — read 2026-10-24 — `Reader(..., model_storage_directory, download_enabled)`, `readtext` accepts a numpy array. **Channel order for arrays is not documented**; community code disagrees (see 7).
- [PaddleOCR upgrade notes](https://www.paddleocr.ai/main/en/update/upgrade_notes.html) and the [Nutrient PaddleOCR guide](https://www.nutrient.io/blog/paddleocr-python-guide/) — read 2026-10-24 — 3.x result fields are `rec_texts`, `rec_scores`, `rec_polys`; the 3.x API differs from 2.x and the schema may change between versions.
- Own code: `proto/ml/v1/ml.proto`, `ml-service/`, `semantic/mlclient`, tests listed in section 5.

## 4. Findings

### 4.1 Why two services and not one `Analyze` call
`Detect` and `OCR` have different cost profiles: YOLOv8n on CPU is tens of milliseconds, EasyOCR on a 720p frame is seconds. Separate services let OCR be scaled, switched off (`ml.no_ocr`) or moved to a GPU on its own. The price is sending the frame twice; on loopback or a Docker bridge this is cheap next to inference. The Go `analyze` package sends both calls for one frame at the same time.

### 4.2 Frame transport
| Format | Size of a 1280x720 frame | Cost |
|---|---|---|
| RGB24 (default) | 2.76 MB (`1280*720*3`) | none; no loss, which matters for OCR |
| JPEG q92 | typically 5–10x smaller (not measured here) | encode in Go, decode in Python, small loss |

A 4K RGB frame is 25 MB, so the client and server both raise the gRPC message limit to 64 MB (`ml.max_message_mb`, `VIREX_MAX_MESSAGE_MB`). Phase 1 already measured raw RGB as the fastest pipe format (`2026-10-01-frames-pipe-and-pts.md`). Use JPEG when the containers are on another machine.

### 4.3 Reliability rules (all covered by tests in `semantic/mlclient`)
| Situation | Behaviour |
|---|---|
| `UNAVAILABLE`, `DEADLINE_EXCEEDED`, `ABORTED`, `RESOURCE_EXHAUSTED` | retried with exponential backoff and jitter (default 2 retries, 200 ms first delay) |
| `INVALID_ARGUMENT`, `INTERNAL` | **not** retried: it would fail the same way |
| One attempt too slow | per-attempt timeout (default 30 s), then retry |
| Parent context cancelled (Ctrl-C) | stops at once, even mid-backoff |
| Container down at start | `openML` fails after `connect_timeout_seconds` (20 s) with "unreachable (is the container running? try `docker compose ps`)" before any video work |
| Container up but model still loading | waits up to `ready_timeout_seconds` (300 s); the Python service loads models *before* opening the port, and `Info.ready` says so |
| Wrong port (OCR address pointing at the detector) | `UNIMPLEMENTED` is turned into a hint about `VIREX_ROLE` |
| A frame fails after retries | by default the encode **fails** (a silently incomplete SVIR is worse); `ml.max_frame_errors` allows skipping a few frames, the tracker bridges the gap |

### 4.4 Cost logging (C1)
Every ML call writes one cost record: stage `ml_detect` or `ml_ocr`, wall time, bytes sent, `model_calls=1`, and the new `model` field ("name (version)"). Server-side inference time and retry count go into `note`. OCR calls skipped by the static-frame rule write `model_calls=0` so the accounting still adds up to one record per sampled frame. `tokens` stays 0 until a VLM is added.

### 4.5 Static-frame OCR reuse
A lecture slide stays up for minutes while the sampler keeps picking frames, and OCR is the slowest call. A frame whose every cell of a 48x27 luma grid moved less than 6 levels since the **last OCR'd frame** reuses that frame's text. The test is on the worst cell, not the average, so a new bullet point (cells change by tens of levels) always triggers OCR. Tested with synthetic frames (`TestStaticFramesReuseOCRButNewTextIsRead`) and in the end-to-end test (an 8 s static text region reused OCR). **The threshold 6 is a guess**; it must be checked on real lecture video (open question 1).

### 4.6 Licences and image size
- Ultralytics is **AGPL-3.0** (enterprise licence available). Fine inside a research deployment; read it before distributing the container.
- CPU-only torch is installed from `https://download.pytorch.org/whl/cpu` to keep the image small; a GPU build is a build argument (`TORCH_INDEX`, see `docker-compose.gpu.yml`).
- EasyOCR was chosen over PaddleOCR for the default because it shares PyTorch with the detector (one framework in the image). PaddleOCR stays available as `VIREX_OCR_BACKEND=paddleocr` with `--build-arg INSTALL_PADDLE=1`; its accuracy edge is the published claim, not measured here.

## 5. Experiments
All on the dev machine (Windows 10, Go 1.26, ffmpeg 9.0.2). The fake ML server (`semantic/mltest`) speaks the real gRPC contract on a real TCP port and "detects" red / blue / green rectangles.

```text
go test -count=1 ./semantic/... ./temporal/... ./codec/pipeline/ ./cost/...
ok  virex/semantic/analyze
ok  virex/semantic/mlclient
ok  virex/temporal/engine
ok  virex/temporal/tracker
ok  virex/codec/pipeline   (real ffmpeg clip -> gRPC -> .virex -> read back)
```
The end-to-end clip (`codec/pipeline/semantic_test.go`) has a red box walking in from the left edge, a blue box in its box, a green box standing for text and a background cut. The `.virex` came back with one `person` entity (3.07 s to 10.24 s), one `cell phone`, events `person_entered`, `scene_changed`, `text_appeared`, `cell_phone_appeared`, `cell_phone_disappeared`, `person_disappeared`, and a `holding` relation, and `docker compose config` validated all three compose files.

## 6. Decision / recommendation
Keep the two-service contract. Build the image on the Docker machine with `docker compose build`, start with the **fake** override first (`-f docker-compose.fake.yml`, starts in seconds, proves ports and contract), then the real models. Run `virex-encode clip.mp4 -semantic ml -cost-log cost.jsonl` and compare the cost log with the numbers below once measured.
Rejected: one combined `Analyze` RPC (couples OCR cost to every detection call); JPEG by default (loses text sharpness); REST/JSON between Go and Python (the doc and tech stack choose gRPC; the HTTP API exists only for debugging with curl).

## 7. Open questions (what the Docker machine must settle)
1. **Real speed on CPU** is unmeasured: YOLOv8n and EasyOCR ms per frame, and the right sampling rate. The cost log of one real run answers it.
2. **The Dockerfile never ran, and neither did the YOLO/EasyOCR/Paddle backends.** The service itself passes `pytest ml-service/tests` (17 tests, fake backends; run on Python 3.14 here, the image uses 3.11), and `virex-encode -semantic ml` against it on a generated clip gave 1 person entity, `person_entered`, `person_disappeared`, `text_appeared`, 14 detect calls, 8 OCR calls and 6 reused.
3. **Where Ultralytics saves `yolov8n.pt`** was not verified. The Dockerfile builds in `/models` and then does `test -f /models/yolov8n.pt`, so a wrong assumption fails the *build* loudly instead of downloading at run time.
4. **EasyOCR channel order for numpy input** is undocumented (sources disagree). The service passes BGR. Check once with a coloured-text image; it only matters slightly because recognition works on grey.
5. **PaddleOCR backend** is written for the 3.x API from the docs only.
6. Static-frame threshold (6 levels) needs real lecture video.
7. First image build downloads about 2 GB (torch, weights); the base `python:3.11-slim` was chosen for wheel availability, not tested.

## 8. Glossary
- **gRPC**: Google Remote Procedure Call, a typed RPC framework over HTTP/2.
- **OCR**: Optical Character Recognition (reading text from a picture).
- **YOLO**: You Only Look Once, a family of real-time object detectors.
- **COCO**: Common Objects in Context, the 80-class dataset the pretrained YOLO models know.
- **AGPL**: GNU Affero General Public License.
- **C1**: paper contribution 1, the cost log.
- **VLM**: Vision-Language Model.
