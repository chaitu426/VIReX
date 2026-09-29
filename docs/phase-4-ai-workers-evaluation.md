# Phase 4 — AI Integration, Background Workers, Evaluation and Final Delivery

**Project:** Semantic Video Codec (SVC)
**Suggested duration:** 5–6 weeks
**Phase goal:** Prove the idea. Let an LLM or agent answer questions from the semantic stream, make encoding asynchronous and robust, measure the system against a conventional pipeline, and deliver the report, demo and viva material.

---

## 1. Deliverables

| # | Deliverable | Done when |
|---|---|---|
| 1 | `QueryVideo(question)` with LLM | Returns an answer with timestamps as evidence |
| 2 | Semantic Video RAG | Retrieves relevant semantic blocks and sends only those to the LLM |
| 3 | Optional pixel verification | Agent can request specific frames when semantics are not enough |
| 4 | Background processing | Job scheduler, task queue, workers, retries, job status |
| 5 | Observability | Logs and metrics for each stage |
| 6 | Baseline pipeline | The "conventional" way: decode frames → run vision models → send to LLM |
| 7 | Benchmark results | Tables and charts for all the metrics below |
| 8 | Demo | Live demo of encode → query → answer |
| 9 | Report, slides, README | Final submission package |

---

## 2. Tasks in order

### Week 1 — AI query path
- [ ] Implement the query flow from the architecture doc (section 11):
  1. Take the question.
  2. Search the temporal, text and event indexes (plus vector search if built).
  3. Resolve timestamps, entities and events into a compact structured context.
  4. Send the context to an LLM.
  5. Return the answer and the timestamps used.
- [ ] Parse simple time hints from the question (for example "between 5 and 10 minutes") into a time range.
- [ ] Build a **context formatter**: turn semantic blocks into short, token-efficient text (a timeline listing, not raw JSON).
- [ ] Add an LLM client behind an interface, so the provider can be swapped (Claude API, OpenAI, or a local model through Ollama).

### Week 2 — RAG, agent and verification
- [ ] Semantic search: hybrid keyword and embedding search if embeddings exist, otherwise keyword only.
- [ ] Add a small agent loop with tools: `getEvents`, `getText`, `getEntity`, `searchSemantic`, `getFrame`.
- [ ] Visual verification: when the answer needs pixels (for example "what colour is the shirt?"), the agent calls `getFrame(t)` and passes the frame to a vision-capable model.
- [ ] Add tasks that show off the design: video summarisation and timeline summary from the semantic stream alone.
- [ ] Simple demo UI (a minimal web page, or CLI): upload video, show timeline, ask a question.

### Week 3 — Background processing
- [ ] Implement the architecture doc's section 12 with Go goroutines and a queue. Start simple (in-process queue); Redis or NATS only if needed.
- [ ] Job model: `queued → running → done / failed`, with per-video progress.
- [ ] Workers: object detection, OCR (and scene/embedding if built), then the aggregator that feeds the temporal engine and the SVC writer.
- [ ] Retries with backoff, a failure handler, partial processing (resume from the last finished chunk).
- [ ] Endpoints: `POST /videos` (upload), `GET /jobs/{id}`.
- [ ] Structured logging plus basic metrics (frames/sec, queue depth, stage latency).

### Week 4 — Evaluation
Compare **SVC** against the **conventional pipeline** on the same clips and questions.

**Processing**
- encoding time, decoding time, semantic extraction time
- query latency, random semantic access latency

**Storage**
- original size, `.svc` size, pixel stream size, semantic stream size, index size
- semantic compression ratio (raw SVIR → compressed)

**Semantic quality** (against your Phase 2 ground truth)
- object detection precision and recall
- OCR accuracy
- event detection accuracy (time error)
- tracking accuracy
- retrieval precision and recall

**AI**
- tokens used per question
- time to answer
- number of vision-model calls
- answer accuracy on a fixed question set

Suggested experiment: write **20–30 questions** about 3–4 videos with known answers. Run each through both pipelines. Record tokens, latency and correctness.

Expected headline result to test (do not assume it): *after the one-time encode cost, questions need far fewer vision-model calls and tokens.* Report the encode cost honestly, including the break-even point (how many queries before SVC wins).

### Week 5 — Hardening and write-up
- [ ] Fix defects found in evaluation. Add tests. Run `go vet` and race detection.
- [ ] Write the report (structure below).
- [ ] Prepare slides and a 5–7 minute demo script. Record a backup demo video in case the live one fails.
- [ ] Prepare viva Q&A (see section 5).

### Week 6 (buffer)
- [ ] Final polish, tagging release v1.0, final README with setup steps.

---

## 3. Report structure

1. Abstract
2. Introduction and problem statement
3. Related work (H.264/AV1, MPEG-7, video-language models, video RAG, scene graphs)
4. System architecture (reuse your diagrams)
5. SVIR and container design
6. Semantic encoder and temporal engine
7. Semantic compression
8. Decoder, indexes and API
9. AI integration
10. Evaluation methodology and results
11. Limitations
12. Future work (learned codec, audio, streaming, hardware acceleration)
13. Conclusion
14. References and appendices (schemas, config, sample outputs)

## 4. Exit criteria

- Natural-language questions are answered from the `.svc` with cited timestamps.
- Uploading a video triggers async processing with visible job status and retry on failure.
- A results table compares SVC vs the baseline on storage, latency, tokens and accuracy.
- Report, slides, demo and repo are ready.

## 5. Likely viva questions

- *How is this different from mp4 plus a JSON sidecar?* Semantic data is compressed, delta-encoded, indexed, time-synchronised and randomly accessible inside the same container.
- *Why not write your own pixel codec?* It is out of scope; the contribution is the semantic layer. A learned pixel codec is future work.
- *What if the detector is wrong?* Confidence values are stored, model versions are recorded, and the pixel stream is kept for verification.
- *What is the cost?* A one-time encode cost, paid back over repeated queries (show the break-even).
- *Why Go and Python?* Go for concurrency and container/decoder speed, Python for the ML ecosystem, joined by gRPC.

## 6. Risks and mitigations

| Risk | Mitigation |
|---|---|
| LLM API cost or availability | Cache answers; support a local model; keep questions few but well-chosen |
| Weak baseline makes the comparison unfair | Build a reasonable baseline (same models, same sampling) and say so |
| Not enough time for all workers | Keep workers to detection and OCR; treat the rest as documented future work |
| Live demo fails | Record a backup video and keep a pre-encoded `.svc` ready |
