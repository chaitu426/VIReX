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
| [arch-digrams/phase1/phase1-system-flow.html](arch-digrams/phase1/phase1-system-flow.html) | Phase 1 system flow diagram (open in a browser) |

## Quick start
See the setup checklist in [docs/project-structure.md](docs/project-structure.md).

```text
go build -o bin/ ./cmd/...                          # build the commands
pwsh scripts/make-testclips.ps1                     # make the synthetic test clips
bin/virex-encode clip.mp4 -o clip.virex             # video -> .virex
bin/virex-decode clip.virex -o clip.out.mp4         # .virex -> playable mp4
bin/virex-decode clip.virex -info                   # header and sections
bin/virex-decode clip.virex -svir-json -            # semantic data as JSON
go test ./...                                       # all tests (about 2 minutes)
```

Phase 1 is done: ingestion, scheduler, sampler, pixel pipeline, SVIR schema v0.1, container v0.1 and the two commands. See [docs/phase-1-summary.md](docs/phase-1-summary.md). There is no AI yet; the semantic section is empty.

Format specs: [container](docs/virex-format-v0.1.md), [SVIR schema](docs/svir-schema-v0.1.md).
