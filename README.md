# VIReX

A video codec that stores the **meaning** of a video (objects, text, events, when they happen) inside the video file, so an AI can answer questions without re-analysing the pixels each time.

**Encode once, ask many times.**

## Documents
| File | What it is |
|---|---|
| [docs/why-virex.md](docs/why-virex.md) | Why the project exists, prior work, what is novel |
| [docs/tech-stack.md](docs/tech-stack.md) | Technology choices |
| [docs/project-structure.md](docs/project-structure.md) | Folders, modules and setup |
| [docs/phase-1-foundation.md](docs/phase-1-foundation.md) | Phase 1: ingestion, schema, container |
| [docs/phase-2-semantic-extraction.md](docs/phase-2-semantic-extraction.md) | Phase 2: detection, tracking, events |
| [docs/phase-3-compression-decoder-api.md](docs/phase-3-compression-decoder-api.md) | Phase 3: compression, indexes, API |
| [docs/phase-4-ai-workers-evaluation.md](docs/phase-4-ai-workers-evaluation.md) | Phase 4: AI, workers, evaluation |
| [research/](research/) | Team research notes |

## Quick start
See the setup checklist in [docs/project-structure.md](docs/project-structure.md).

```text
go build ./...                          # build all Go packages and commands
cd ml-service && uvicorn app.main:app   # start the ML service
```
