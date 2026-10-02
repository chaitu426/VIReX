# Tech Stack — VIReX

Choices follow your architecture document: **Go core, Python ML services, FFmpeg for pixels**. Where the document leaves options open, this file picks one and says why.

---

## 1. Summary

| Layer | Choice | Phase |
|---|---|---|
| Core runtime | **Go 1.22+** | 1–4 |
| ML services | **Python 3.10+** | 2, 4 |
| Video processing | **FFmpeg + ffprobe** | 1 |
| Pixel codec | **H.264 (libx264)**, AV1 optional | 1 |
| Go ⇄ Python | **gRPC** (Protobuf) | 2 |
| Semantic serialisation | **Protobuf first, custom compact binary later** | 1, 3 |
| Object detection | **YOLO (Ultralytics)** | 2 |
| OCR | **PaddleOCR** or EasyOCR | 2 |
| Tracking | **IoU + Hungarian / SORT** | 2 |
| Scene/VLM (stretch) | **LLaVA / Qwen-VL** or hosted API | 2 |
| Embeddings (optional) | **CLIP / sentence-transformers** | 2, 4 |
| Storage | **Local filesystem**, MinIO optional | 1–4 |
| Indexes | **Custom in Go** (temporal, entity, inverted text), brute-force vector | 3 |
| API | **Go net/http** (REST), gRPC optional | 3 |
| Job queue | **In-process Go channels**, Redis/NATS optional | 4 |
| LLM | **Claude API** (or OpenAI / Ollama) behind an interface | 4 |
| Testing | `go test`, `pytest` | all |
| Packaging | **Docker** for the ML service | 2+ |
| Version control | **Git + GitHub** | all |

---

## 2. Core runtime — Go

**Used for:** ingestion, pipeline orchestration, temporal engine, SVIR, container reader/writer, decoder, indexing, API, worker management.

**Why:** goroutines and channels fit a frame pipeline; single binary deployment; strong standard library for networking and binary I/O (`encoding/binary`, `bufio`, `net/http`).

**Libraries:**
| Purpose | Library |
|---|---|
| Protobuf | `google.golang.org/protobuf`, `protoc-gen-go` |
| gRPC | `google.golang.org/grpc` |
| CLI | `spf13/cobra` (or standard `flag`) |
| Logging | `log/slog` |
| Metrics | `prometheus/client_golang` (optional) |
| Compression | `klauspost/compress/zstd` |
| Testing | `testing`, `stretchr/testify` |
| Router | standard `http.ServeMux` (Go 1.22) or `chi` |

---

## 3. ML layer — Python

**Used for:** detection, OCR, VLM, embeddings — isolated behind gRPC so models can be swapped without touching Go.

| Purpose | Choice | Notes |
|---|---|---|
| Object detection | Ultralytics YOLOv8/v11 (`yolov8n` for CPU) | Pretrained COCO, no training needed |
| OCR | PaddleOCR (better accuracy) or EasyOCR (easier install) | Pick one after a quick benchmark |
| Pose (stretch) | YOLO-pose or MediaPipe | For writing/pointing/raising a hand |
| Scene / VLM (stretch) | LLaVA, Qwen-VL, or a hosted vision API | Only on scene changes to control cost |
| Embeddings | CLIP (image), `sentence-transformers` (text) | Only if vector search is built |
| Serving | FastAPI first, then gRPC (`grpcio`) | |
| Runtime | PyTorch, ONNX Runtime (optional speed-up) | |
| Env | `venv` or Docker | Avoids Windows dependency problems |

**Hardware note:** a laptop CPU works with `yolov8n` at 1–2 sampled frames per second. A GPU (or free Colab/Kaggle for batch runs) makes the VLM and larger models practical.

---

## 4. Video processing

- **FFmpeg / ffprobe** called from Go through `os/exec` with piped output. This avoids CGO and Windows build pain.
- **H.264 (libx264)** for the pixel stream: universal playback, easy to verify. Settings: `-crf 23 -preset medium`. Record key-frame positions for seeking.
- **AV1 (libsvtav1 / libaom)** is optional and only for a comparison experiment. Encoding is slow.
- **Quality checks:** SSIM and PSNR through FFmpeg filters.

---

## 5. Serialisation and container

| Stage | Format | Why |
|---|---|---|
| Phase 1–2 | **Protobuf** + JSON export | Stable schema, easy debugging, generated code in Go and Python |
| Phase 3 | **Custom compact binary** (varint, delta, dictionary IDs) | Real semantic compression, the core of "codec" |
| Optional | zstd over the semantic section | Report results with and without |

**Container:** custom `.virex` — magic bytes, header, section table `(type, offset, length)`, sections (pixel, semantic, temporal index, AI index, metadata), CRC32 per section.

Alternatives considered (MessagePack, CBOR) are fine as a stepping stone but add no value over Protobuf plus a custom compact encoder.

---

## 6. Indexing

| Index | Structure | Phase |
|---|---|---|
| Temporal | Sorted `(timestamp → offset)` with sparse checkpoints | 3 |
| Entity | `entity_id → lifetime + blocks` | 3 |
| Event / Text | Inverted index (token → timestamps) | 3 |
| Vector (optional) | Brute-force cosine, HNSW only if time allows | 3–4 |

Keep them custom and simple. Do not add Elasticsearch or a vector database; they hide the work you want to show.

---

## 7. API and AI

- **REST** in Go for the Semantic Video API (see Phase 3 endpoint table), with an **OpenAPI** spec.
- **LLM client interface** in Go: `Complete(ctx, prompt, images) → answer`. Implementations: Claude API, OpenAI, Ollama (local).
- **RAG:** retrieval from the semantic indexes, context formatted as a compact timeline.
- **Agent tools:** `getEvents`, `getText`, `getEntity`, `searchSemantic`, `getFrame`.
- **Demo UI:** a minimal HTML/JS page served by the Go API, or a CLI. Do not spend weeks on the frontend.

---

## 8. Background processing

- Phase 4 starts with **in-process** goroutine workers and channels. That is enough to demonstrate the architecture.
- Upgrade path if there is time: **Redis** (Asynq) or **NATS** for a real queue.
- Features: retries with backoff, job states, progress, partial resume, structured logs, Prometheus-style metrics.

---

## 9. Dev tooling

| Tool | Use |
|---|---|
| Git and GitHub | Version control, one branch per phase, tags at each phase end |
| VS Code or GoLand | Go development |
| Protobuf compiler `protoc`, `buf` (optional) | Schema generation |
| Docker Desktop | Run the Python ML service |
| `golangci-lint`, `go vet`, `-race` | Code quality |
| Postman / curl | API testing |
| Python: `black`, `pytest` | ML service quality |
| Jupyter / matplotlib | Evaluation charts for the report |
| Draw.io / Mermaid | Diagrams (already in your architecture doc) |

---

## 10. Datasets and test material

- Your own short **lecture** and **screen-recording** clips (best for OCR and events).
- Public: **MOT17** or **UA-DETRAC** (tracking), **Charades** or **AVA** (actions), **YouTube lecture** clips (check licences), **COCO** class list for reference.
- Start with 5–8 clips of 1–5 minutes; add one 20–45 minute lecture for the compression and random-access experiments.

---

## 11. Installation checklist

```text
[ ] Go 1.22+
[ ] FFmpeg (with libx264) on PATH, plus ffprobe
[ ] protoc + protoc-gen-go + protoc-gen-go-grpc
[ ] Python 3.10+ with venv
[ ] pip: ultralytics, paddleocr (or easyocr), fastapi, uvicorn, grpcio, grpcio-tools
[ ] Docker Desktop (optional but recommended)
[ ] Git
[ ] LLM access: an API key, or Ollama for local models
```

---

## 12. Decisions to confirm with your guide

1. Is a **Go + Python** split acceptable, or should everything be Python for simplicity?
2. **CPU only** or GPU available? This decides the model sizes and sampling rate.
3. Which **LLM** provider is permitted and budgeted?
4. Expected **evaluation depth** (number of videos, questions, need for a paper).




I'll split that into lighter reads: the Phase 2 doc first, then a quick environment check.

  Read 1 file (ctrl+o to expand)

● Bash(python --version 2>&1; pip list 2>/dev/null | grep -i -E
      "^(torch|ultralytics|fastapi|uvicorn|grpcio|paddleocr|easyocr|onnxruntime|opencv)" ; nvidia-smi -L 2>&1…)
  ⎿  Python 3.14.7
     /usr/bin/bash: line 1: nvidia-smi: command not found
     fastapi
     … +6 lines (ctrl+o to expand)
  ⎿  Allowed by auto mode classifier
● Bash(pip index versions torch 2>&1 | head -3; pip index versions ultralytics 2>&1 | head -2; py -0 2>&1 | head -8)
  ⎿  torch (2.14.1)
     Available versions: 2.14.1, 2.14.0, 2.13.0, 2.12.1, 2.12.0, 2.11.0, 2.10.0, 2.9.1, 2.9.0
     ultralytics (8.4.171)
     … +60 lines (ctrl+o to expand)
  ⎿  (timeout 1m 30s)
  ⎿  Allowed by auto mode classifier

● Phase 1 is done on my side, and Phase 2's first step doesn't depend on anything outstanding. But a few things should be
  fixed first, because they get harder once Phase 2 code piles on top.

  What Phase 2 needs from Phase 1, and has  - Sampled frames: they arrive with exact timestamps. The scheduler's sink function is where the ML client plugs in.
  - Schema: it has pixel_ref and layer on every block, so Phase 2 can fill ObjectBlock, TextBlock and the rest.  - Container: it can hold the semantic data, as one section or one per layer.
  - Cost logger: it has model_calls and tokens fields ready.

  Before starting, in order of how much they matter
  1. Nothing is committed. All Phase 1 work is uncommitted. I'd commit it on a phase-1 branch and tag it first. I haven't     done that because you didn't ask.
  2. Tests quietly lost coverage. When we deleted the synthetic clips, many tests started skipping instead of failing.
     I'd make them generate small clips on demand, so the safety net is real again before Phase 2 changes the pipeline.
  3. The ML environment is empty.
     - Installed: no torch, ultralytics or OCR packages. Python is 3.14.7 only.
     - Wheels: pip lists torch and ultralytics versions for this Python, so installation looks feasible. I didn't install       anything.
     - PaddleOCR on 3.14: I haven't checked it, so EasyOCR may be the fallback.
     - GPU: this machine has none (nvidia-smi isn't present), so it's CPU only. yolov8n at 1–2 sampled frames per second
       is realistic. The sampler picked only 63 frames from the 9.5-minute clip.
  4. Footage for ground truth. Phase 2 needs clips with people entering and leaving, on-screen text and events, plus hand     labels for 2–3 of them. We have one clip whose content I haven't looked at, so the team needs to supply real
     lecture, screen or street footage.
  5. Human sign-offs. The schema freeze, including the CSGCoT check, and the review of the research notes I drafted are
     still open. Phase 2 can start without them because schema changes stay additive, but the freeze should be settled
     before Phase 3.

  My recommendation
  - Do first: items 1 and 2, which I can do in a few minutes.
  - Then start Phase 2 Week 1: the FastAPI /detect, /ocr and /health service, with a CPU speed benchmark.
  - Decision for you: do you want that, and should I commit and tag Phase 1 first?
