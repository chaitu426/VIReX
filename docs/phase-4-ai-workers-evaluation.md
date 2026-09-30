# Phase 4 — AI Integration, Background Workers, Evaluation and Final Delivery

**Project:** VIReX
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
- [ ] **[C4]** Make the fallback a measured policy, not an afterthought. Add the tool `getEvidence(block_id)` and three modes to compare: `semantic-only`, `semantic + verify-on-low-confidence`, `semantic + verify-always`. Log every fallback call. Every answer cites block ids so it can be checked.
- [ ] **[C3]** Add a layer-selection step: start with L0+L1, add L2 (text) or L3 only when the question type needs them. Compare this adaptive mode to "always all layers".
- [ ] Add tasks that show off the design: video summarisation and timeline summary from the semantic stream alone.
- [ ] Simple demo UI (a minimal web page, or CLI): upload video, show timeline, ask a question.

### Week 3 — Background processing
- [ ] Implement the architecture doc's section 12 with Go goroutines and a queue. Start simple (in-process queue); Redis or NATS only if needed.
- [ ] Job model: `queued → running → done / failed`, with per-video progress.
- [ ] Workers: object detection, OCR (and scene/embedding if built), then the aggregator that feeds the temporal engine and the VIReX writer.
- [ ] Retries with backoff, a failure handler, partial processing (resume from the last finished chunk).
- [ ] Endpoints: `POST /videos` (upload), `GET /jobs/{id}`.
- [ ] Structured logging plus basic metrics (frames/sec, queue depth, stage latency).

### Week 4 — Evaluation
Compare **VIReX** against the **conventional pipeline** on the same clips and questions.

**Processing**
- encoding time, decoding time, semantic extraction time
- query latency, random semantic access latency

**Storage**
- original size, `.virex` size, pixel stream size, semantic stream size, index size
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

Suggested experiment: use the **multi-question benchmark** started in Phase 2 (C2): **8–15 questions per clip across 5–8 clips, about 60–100 questions in total**, covering the question types listed in Phase 2. Run each through both pipelines. Record tokens, latency and correctness. This replaces the earlier "20–30 questions on 3–4 videos" plan, because amortization needs many questions per video.

Expected headline result to test (do not assume it): *after the one-time encode cost, questions need far fewer vision-model calls and tokens.* Report the encode cost honestly, including the break-even point (how many queries before VIReX wins).

#### Paper experiments (C1–C4)

| ID | Experiment | Output |
|---|---|---|
| **C1** | Fit the cost model `total_cost(N) = encode_cost + N · query_cost_virex` against `N · query_cost_baseline`. Solve for the break-even N per clip, in tokens, in seconds and in money. Validate by running real query sequences (N = 1, 5, 10, 25, 50) and checking that measured cost matches the model. | Break-even table and a cost-versus-N chart with both lines |
| **C2** | Publish the benchmark: clips (or links and licences), questions, answers, evidence ranges, question types, scoring script, and the cost-accounting format. Report results per question type. | Benchmark folder in the repo, ready to release |
| **C3** | For each layer set (L0, L0+1, L0+1+2, all) record bytes (from the Phase 3 rate table) and answer accuracy. Plot **rate (bytes) versus accuracy**, with the conventional pipeline as a reference point. Add the adaptive layer-selection mode. Report which question types need which layers. | The headline rate–accuracy figure |
| **C4** | Compare the three fallback modes: accuracy, tokens, fallback rate, and grounding precision (does the cited block really contain the fact?). Also report the byte cost of `pixel_ref`. | Fallback table and a grounding-precision number |

Hold out some clips as a **test set** that you do not tune on.

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
5. SVIR and container design, including scalable semantic layers (C3)
6. Semantic encoder and temporal engine
7. Semantic compression and the rate–distortion framing (C3)
8. Decoder, indexes, API and grounded evidence (C4)
9. AI integration and the fallback policy (C4)
10. Evaluation: the benchmark (C2), the amortized-cost model (C1), rate–accuracy results (C3), grounding results (C4)
11. Limitations
12. Future work (learned codec, audio, streaming, hardware acceleration)
13. Conclusion
14. References and appendices (schemas, config, sample outputs)

## 4. Exit criteria

- Natural-language questions are answered from the `.virex` with cited timestamps.
- Uploading a video triggers async processing with visible job status and retry on failure.
- A results table compares VIReX vs the baseline on storage, latency, tokens and accuracy.
- C1: a break-even N per clip, checked against measured query sequences.
- C2: a released-ready multi-question benchmark with a scoring script.
- C3: the rate–accuracy figure across layer sets, with the baseline as a reference point.
- C4: the three fallback modes compared, with a grounding-precision number.
- Report, slides, demo and repo are ready.

## 4a. Paper contributions in this phase

| ID | What to do here |
|---|---|
| C1 | Fit and validate the break-even model |
| C2 | Finish and package the benchmark, report by question type |
| C3 | Rate–accuracy curves, adaptive layer selection |
| C4 | Fallback-mode comparison, grounding precision |

Also budget time in Week 5 to prepare the release: license, README for the benchmark, the `.virex` spec as a standalone document, and a reproducible script that regenerates every table and figure.

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
| Live demo fails | Record a backup video and keep a pre-encoded `.virex` ready |
