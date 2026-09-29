# Phase 3 — Semantic Compression, Indexes, Decoder and Semantic Video API

**Project:** Semantic Video Codec (SVC)
**Suggested duration:** 5–6 weeks
**Phase goal:** Turn the SVIR into a real *codec-like* stream. Compress it, index it, and add a decoder that produces a Unified Video Representation exposed through an API. This phase is what makes the project "a codec" rather than "mp4 plus JSON".

---

## 1. Deliverables

| # | Deliverable | Done when |
|---|---|---|
| 1 | Semantic compressor (`temporal/compression`) | SVIR is smaller than its raw form and still decodes losslessly, apart from quantised confidence |
| 2 | Compact binary semantic stream | Replaces the plain Protobuf dump |
| 3 | Temporal index | `timestamp → semantic blocks` in O(log n) |
| 4 | AI index | Entity, event, text and (optional) vector indexes stored in the container |
| 5 | Container v1.0 | Header, pixel, semantic, temporal index, AI index and metadata sections |
| 6 | SVC reader and semantic decoder | Rebuilds the semantic graph from the stream |
| 7 | Pixel/semantic synchroniser | `GetFrame(t)` returns pixels plus the matching semantics |
| 8 | Unified Video Object | One Go type exposing frames, objects, text, actions, events, relations, scene, timeline |
| 9 | Semantic Video API (Go HTTP, gRPC optional) | Endpoints below respond correctly |
| 10 | `svc-query` CLI | Query a `.svc` from the terminal |

---

## 2. Tasks in order

### Week 1 — Semantic compression (part 1)
- [ ] Start from the pipeline in the architecture doc (section 6): raw blocks → dedup → entity references → temporal delta → dictionary → quantisation → binary.
- [ ] Duplicate removal: identical consecutive blocks become one block with a range.
- [ ] Entity references: blocks refer to `entity_id` (integer), not to repeated strings.
- [ ] Dictionary encoding: class names, action names, relation names and event types become small integer IDs. The dictionary is stored in the metadata section.
- [ ] Quantise confidence to 8 bits (0–255).

### Week 2 — Semantic compression (part 2) and binary format
- [ ] Temporal delta encoding: timestamps as varint deltas from the previous block; bbox as deltas from the previous bbox of the same entity.
- [ ] Choose a serialisation: **custom varint binary**, or MessagePack/CBOR as a stepping stone. Suggested path: keep Protobuf as a reference and write a compact encoder next to it, so you can compare sizes and prove correctness (decode(encode(x)) == x).
- [ ] Optionally apply zstd or gzip over the stream and report with and without.
- [ ] Property/round-trip tests on every test clip.

### Week 3 — Indexes and container v1.0
- [ ] Temporal index: sorted array of `(timestamp, block offset)` with sparse checkpoints for random access.
- [ ] Entity index: `entity_id → lifetime + block offsets`.
- [ ] Event index and text index: inverted index (token → list of timestamps and blocks).
- [ ] Optional vector index: brute-force cosine first; move to HNSW only if there is time.
- [ ] Finalise the container: section table, CRC, `schema_version`, model versions.
- [ ] Support **semantic random access**: read the index, then only the needed blocks, never the whole semantic stream.

### Week 4 — Decoder and synchroniser
- [ ] `codec/container/reader.go`: open the file, read the header and section table.
- [ ] Semantic decoder: rebuild the entity table, events, relations and the graph.
- [ ] Pixel decoder: FFmpeg-based frame extraction at a requested timestamp (seek to the previous key frame using the stored key-frame positions).
- [ ] Synchroniser: given `t`, return frame plus active entities, text, actions, events and relations at `t`.
- [ ] Define the `UnifiedVideo` type and its methods.

### Week 5 — Semantic Video API
- [ ] Implement in Go (`api/`), matching the architecture doc:

| Function | HTTP example |
|---|---|
| `GetFrame(t)` | `GET /video/{id}/frame?t=10.5` |
| `GetObjects(t)` | `GET /video/{id}/objects?t=10.5` |
| `GetText(t)` | `GET /video/{id}/text?t=10.5` |
| `GetEvents(start,end)` | `GET /video/{id}/events?start=300&end=600` |
| `GetTimeline()` | `GET /video/{id}/timeline` |
| `GetEntity(id)` | `GET /video/{id}/entities/{eid}` |
| `SearchSemantic(q)` | `GET /video/{id}/search?q=person writing on whiteboard` |
| `QueryVideo(question)` | stub now, real in Phase 4 |

- [ ] Add an OpenAPI spec and simple API tests.
- [ ] `svc-query` CLI wraps the same functions.
- [ ] Keyword search over the text and event indexes (semantic/vector search comes in Phase 4).

### Week 6 (buffer)
- [ ] Measure: compression ratio at each stage, random-access latency, decode time.
- [ ] Fix bugs and update the docs.
- [ ] Tag container format v1.0.

---

## 3. Exit criteria

- `.svc` files contain compressed SVIR plus indexes, and decode correctly on all test clips.
- Semantic stream is **measurably smaller** than the raw JSON/Protobuf (record the ratio).
- `GetFrame(10.52)` returns pixels and the correct semantics for that time.
- Query for a single time range does **not** read the whole semantic stream.
- API is documented and every endpoint has at least one test.

## 4. Risks and mitigations

| Risk | Mitigation |
|---|---|
| Custom binary format bugs | Keep Protobuf as a reference and round-trip test every clip |
| Pixel seek accuracy | Use the stored key-frame positions and decode forward to the exact frame |
| Compression gains look small | Long videos with stable entities compress far better; test on a long lecture clip and report per-clip ratios honestly |
| Vector index scope creep | Brute-force cosine is enough for a college project |

## 5. Hand-off to Phase 4

Phase 4 receives: a complete encoder, container, decoder and API. What remains is AI consumption, background processing, evaluation and the report.
