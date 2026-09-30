# Project Structure — VIReX

The code is a **Go module** named `virex` (Go core) plus a **Python ML service**. Each folder has one job. The "Phase" column says when you first write code there.

```text
clproject/
├── go.mod                     Go module "virex" (Go 1.22)
├── cmd/                       Command-line programs (one folder each)
│   ├── virex-encode/            video -> .virex
│   ├── virex-decode/            .virex -> video
│   ├── virex-query/             ask questions of a .virex from the terminal
│   └── virex-server/            HTTP API and upload server
├── codec/                     The codec itself
│   ├── encoder/                 ingestion with FFmpeg, drives the pipelines
│   ├── decoder/                 rebuilds frames and the semantic graph
│   ├── container/               .virex reader and writer
│   ├── scheduler/               frame flow with bounded channels
│   ├── sampler/                 which frames get semantic analysis
│   └── pixel/                   H.264 pixel stream and key frames
├── semantic/                  Go clients for the ML service
│   ├── objects/  ocr/  scene/  embedding/
├── temporal/                  Turning detections into meaning over time
│   ├── tracker/  engine/  graph/  compression/
├── svir/schema/               SVIR Protobuf definitions (svir.proto) and generated code
├── index/                     Indexes stored in the file
│   ├── temporal/  entity/  text/  vector/
├── api/                       Semantic Video API
├── workers/                   Background jobs, queue, retries
├── storage/                   Files on disk
├── cost/                      Cost logger (C1)
├── proto/                     Other Protobuf files, for example ml.proto (Go <-> Python)
├── ml-service/                Python ML service (FastAPI, later gRPC)
│   ├── app/main.py              /health endpoint to start
│   └── requirements.txt
├── configs/                   Config files (sampling rate, CRF, model names)
├── scripts/                   Helper scripts (setup, run, benchmark)
├── tests/
│   ├── integration/             end-to-end Go tests
│   └── testdata/
│       ├── clips/               test videos (not committed, see .gitignore)
│       └── ground-truth/        hand-made labels and benchmark questions (C2)
├── benchmark/                 Benchmark package for release (C2)
├── docs/                      Phase plans, tech stack, why-virex, this file
├── research/                  Team research notes, one folder per person
├── arch-digrams/              Architecture diagrams (HTML)
└── .archify/                  Diagram build output
```

## Who calls whom

```text
cmd/virex-encode
   └─ codec/encoder ─┬─ codec/scheduler ─ codec/sampler ─ semantic/* ──(gRPC)──> ml-service
                     │                                        │
                     │                                   temporal/tracker, engine, graph
                     │                                        │
                     │                                   temporal/compression ── svir/schema
                     └─ codec/pixel (FFmpeg H.264)                 │
                                          └──────────> codec/container  ──> .virex file

cmd/virex-decode / virex-query / virex-server
   └─ codec/decoder ── codec/container ── index/*
   └─ api, workers (server only)
```

Rules to keep it clean:
- `svir/schema` and `codec/container` depend on nothing else in the project.
- `semantic/*` only talks to the ML service. It never touches the container.
- `cmd/*` only wires things together. Put real logic in packages.
- The `codec` packages must not import `api` or `workers`.

## What each phase touches

| Phase | Folders you start filling in |
|---|---|
| 1 | `codec/encoder`, `codec/scheduler`, `codec/sampler`, `codec/pixel`, `codec/container`, `svir/schema`, `cost`, `cmd/virex-encode`, `cmd/virex-decode`, `tests` |
| 2 | `ml-service`, `proto`, `semantic/*`, `temporal/tracker`, `temporal/engine`, `temporal/graph`, `tests/testdata/ground-truth` |
| 3 | `temporal/compression`, `index/*`, `codec/decoder`, `api`, `cmd/virex-query` |
| 4 | `workers`, `cmd/virex-server`, `storage`, `benchmark` |

## Conventions
- **Names:** Go packages are lowercase, one word. Commands are `virex-<verb>`.
- **Each Go folder has a `doc.go`** that states the package's job. Delete or extend it when you add real code.
- **Tests** sit next to the code as `*_test.go`. Only end-to-end tests go in `tests/integration`.
- **Test videos** are not committed. Keep the list of clips and where to get them in `tests/testdata/clips/README.md`.
- **Generated code** (Protobuf) goes next to its `.proto` file and is committed.
- **Module path:** `virex` is a local name. If the code moves to GitHub, change it once in `go.mod` and update the imports.

## Setup checklist

```text
[ ] Install Go 1.22+          then run:  go build ./...
[ ] Install FFmpeg + ffprobe  (with libx264), add to PATH
[ ] Install protoc, protoc-gen-go, protoc-gen-go-grpc
[ ] Python 3.10+:  cd ml-service && python -m venv .venv && .venv\Scripts\activate && pip install -r requirements.txt
[ ] Run the ML service:  uvicorn app.main:app --reload   (check http://localhost:8000/health)
```

None of Go, FFmpeg or protoc is installed on this machine yet, so `go build ./...` has not been run.
