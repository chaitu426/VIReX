# Phase 3 — Semantic Compression, Indexes, Decoder and Semantic Video API

**Project:** VIReX
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
| 6 | VIReX reader and semantic decoder | Rebuilds the semantic graph from the stream |
| 7 | Pixel/semantic synchroniser | `GetFrame(t)` returns pixels plus the matching semantics |
| 8 | Unified Video Object | One Go type exposing frames, objects, text, actions, events, relations, scene, timeline |
| 9 | Semantic Video API (Go HTTP, gRPC optional) | Endpoints below respond correctly |
| 10 | `virex-query` CLI | Query a `.virex` from the terminal |

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
- [ ] **[C3]** Encode each layer (L0–L3) as its **own stream** with its own dictionary and delta state, so a layer can be decoded on its own. Store the stream in its own section using the `layer` id from the section table. Record the compressed size of each layer separately: these are the "rate" points for the rate–accuracy curve.
- [ ] **[C4]** Encode `pixel_ref` compactly (frame id as a delta, bbox as a delta), and measure how many bytes grounding costs. Report it as its own line item.

### Week 3 — Indexes and container v1.0
- [ ] Temporal index: sorted array of `(timestamp, block offset)` with sparse checkpoints for random access.
- [ ] Entity index: `entity_id → lifetime + block offsets`.
- [ ] Event index and text index: inverted index (token → list of timestamps and blocks).
- [ ] Optional vector index: brute-force cosine first; move to HNSW only if there is time.
- [ ] Finalise the container: section table, CRC, `schema_version`, model versions.
- [ ] Support **semantic random access**: read the index, then only the needed blocks, never the whole semantic stream.
- [ ] **[C3]** Give each layer its own index (or a layer field in the shared index) so a query can touch only the layers it needs. A file that has only L0 and L1 must still decode and query correctly.

### Week 4 — Decoder and synchroniser
- [ ] `codec/container/reader.go`: open the file, read the header and section table.
- [ ] Semantic decoder: rebuild the entity table, events, relations and the graph.
- [ ] Pixel decoder: FFmpeg-based frame extraction at a requested timestamp (seek to the previous key frame using the stored key-frame positions).
- [ ] Synchroniser: given `t`, return frame plus active entities, text, actions, events and relations at `t`.
- [ ] Define the `UnifiedVideo` type and its methods.
- [ ] **[C3]** Add layer-selective decoding: `Open(file, layers=[L0,L1])` loads only those streams. Measure decode time and bytes read per layer set.
- [ ] **[C4]** Add `GetEvidence(block_id)`, which returns the block's `pixel_ref` plus the decoded frame and (optionally) the cropped bbox region, so any fact can be checked against pixels.

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
| `GetEvidence(block_id)` | `GET /video/{id}/evidence/{block_id}` (C4) |
| Layer selection on any read | add `?layers=L0,L1` (C3) |
| `QueryVideo(question)` | stub now, real in Phase 4 |

- [ ] Add an OpenAPI spec and simple API tests.
- [ ] `virex-query` CLI wraps the same functions.
- [ ] Keyword search over the text and event indexes (semantic/vector search comes in Phase 4).

### Week 6 (buffer)
- [ ] Measure: compression ratio at each stage, random-access latency, decode time.
- [ ] **[C3]** Produce the **rate table**: cumulative bytes for L0, L0+L1, L0+L1+L2, all layers, per clip. Phase 4 adds the accuracy column.
- [ ] **[C1]** Add semantic-compression time and the cost of building indexes to the cost log.
- [ ] Fix bugs and update the docs.
- [ ] Tag container format v1.0.

---

## 3. Exit criteria

- `.virex` files contain compressed SVIR plus indexes, and decode correctly on all test clips.
- Semantic stream is **measurably smaller** than the raw JSON/Protobuf (record the ratio).
- `GetFrame(10.52)` returns pixels and the correct semantics for that time.
- Query for a single time range does **not** read the whole semantic stream.
- API is documented and every endpoint has at least one test.
- A file can be opened with a subset of layers and still answer queries on those layers (C3), and the per-layer size table exists.
- `GetEvidence` returns the right frame and region for a sample of blocks (C4).

## 3a. Paper contributions in this phase

| ID | What to do here |
|---|---|
| C3 | One stream and index per layer, layer-selective decoding, the rate table (bytes per layer set) |
| C4 | Compact `pixel_ref` encoding, `GetEvidence` endpoint, cost of grounding in bytes |
| C1 | Log compression and indexing cost |
| C2 | Nothing new; use the question files from Phase 2 for API tests |

## 4. Risks and mitigations

| Risk | Mitigation |
|---|---|
| Custom binary format bugs | Keep Protobuf as a reference and round-trip test every clip |
| Pixel seek accuracy | Use the stored key-frame positions and decode forward to the exact frame |
| Compression gains look small | Long videos with stable entities compress far better; test on a long lecture clip and report per-clip ratios honestly |
| Vector index scope creep | Brute-force cosine is enough for a college project |

## 5. Hand-off to Phase 4

Phase 4 receives: a complete encoder, container, decoder and API. What remains is AI consumption, background processing, evaluation and the report.
