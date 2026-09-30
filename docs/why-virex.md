# Why VIReX

**What this file answers:** why build this, what problem it solves, what is new about it, and whether anyone has done it before.

> **Honesty note.** The prior-art section is based on a web search done on 2026-09-30. It is a good first pass, not a formal literature review. Before you claim novelty in a report or paper, run a proper search (Google Scholar, arXiv, IEEE Xplore, ACM DL) using the keywords at the end of this file.

---

## 1. The problem

Video is stored as pixels. Pixels are built for human eyes, but more and more of the "viewers" are machines: LLMs, search systems, analytics jobs, agents.

Today, when you ask an AI a question about a video, it usually has to:

1. Decode the video.
2. Sample frames.
3. Run a vision model (or feed images to a multimodal LLM) on those frames.
4. Answer, then **throw the understanding away**.

Ask a second question and it does the whole thing again.

| Pain point | Why it hurts |
|---|---|
| **Repeated cost** | Every question re-pays the vision cost. A 30 minute video sampled at 2 fps is about 3.6k frames; one published comparison puts dense methods at ~4M visual tokens per video. |
| **Slow** | Answers wait on frame decoding and model inference at query time. |
| **Lossy by sampling** | Sampling frames can miss short events, small text and fast actions. |
| **No random access to meaning** | You can seek to `12:40`, but you cannot ask the file "when did the slide title change?" |
| **Understanding is not portable** | Extracted metadata usually lives in a separate database, not in the video file, and is not part of any format. |

## 2. The idea

**Encode once, ask many times.** Do the expensive AI work a single time at encode time, and store the result *inside the video container* as a compact, indexed, queryable **semantic stream** next to the normal pixel stream.

```text
Conventional:   video ──► [vision model on frames] ──► answer      (repeated per question)

VIReX:          video ──► encode once ──► .virex file
                                            ├─ pixel stream (H.264, playable)
                                            ├─ semantic stream (SVIR: entities, events, text, relations)
                                            └─ indexes (temporal, entity, text, optional vector)

                question ──► indexes ──► few facts ──► LLM ──► answer with timestamps
```

The `.virex` file is still a normal, playable video. It additionally *knows what is in it*.

## 3. What it solves

- **Cheaper repeated questions.** After a one-time encode cost, each question needs far fewer vision-model calls and tokens. (This is a hypothesis to measure in Phase 4, not a proven result. Report the break-even point honestly.)
- **Faster answers.** Queries hit indexes and a small structured stream, not raw frames.
- **Fine-grained retrieval.** "When did the speaker write X on the board?" becomes an index lookup with a timestamp.
- **Citable answers.** Every answer points at timestamps and entity IDs, so it can be checked.
- **Portable understanding.** The semantic layer travels with the file and has a defined format, instead of living in a vendor's database.
- **Compression of meaning.** Semantic information (what happened) is stored in far fewer bytes than the pixels that show it. You can measure this: pixel stream size vs semantic stream size.

## 4. Who would use it

- Lecture and meeting archives (search "when was topic X explained").
- Screen recordings and software walkthroughs (OCR + UI events).
- Surveillance and industrial footage (entities, tracks, events).
- Agents and LLM apps that need to reason over many videos cheaply.

---

## 5. Has anyone done this before?

**Short answer: the pieces exist, the exact combination does not appear to.** Several groups have done parts of this. Treat the pieces as related work to cite and learn from, not as competitors you can ignore.

### 5.1 Closest prior art

| Area | What exists | How VIReX differs |
|---|---|---|
| **Compressed scene graphs for video LLMs** | *CSGCoT* uses a compressed scene graph modelled on video codecs, with **Key-SG** for key frames and **Delta-SG** for delta frames. It reports comparable accuracy to state of the art with over 62% lower latency on hour-long videos. [Paper](https://doi.org/10.1145/3746027.3754765) | **Closest idea to VIReX.** It is a *method* for answering questions, not a file format. VIReX makes the representation a persistent container with sections, indexes and a decoder API. |
| **Semantic event graphs** | *Semantic Event Graphs for Long-Form Video QA* follows Video → Symbols → Graphs → Language, arguing semantic compression can replace dense visual prompting. [arXiv](https://arxiv.org/pdf/2601.06097) | Same philosophy, research pipeline. No container, random access, or API. |
| **Structured memory for long video** | *Building a Mind Palace* (environment-grounded semantic graphs, [arXiv](https://arxiv.org/pdf/2501.04336)), *VideoAgent* (temporal + object memory, [arXiv](https://arxiv.org/pdf/2403.11481)), *LangRepo* (structured language repository), *VideoARM* (hierarchical memory, [arXiv](https://arxiv.org/pdf/2512.12360)). | These build memory **at query/analysis time inside a model pipeline**. VIReX bakes it into a file at encode time. |
| **Video Coding for Machines (MPEG VCM / FCM)** | MPEG standardises compression for machine vision. Track 1 (Feature Coding) compresses intermediate neural features sent from an edge device to a server. [Overview](https://arxiv.org/abs/2001.03569), [FCM article](https://streaminglearningcenter.com/articles/real-time-feature-coding-for-machines-inside-the-new-mpeg-standard.html) | VCM compresses **features for a machine to finish a task**. It does not store human-readable, queryable semantics (entities, events, text) with an index. Different goal, closest *standards* neighbour. |
| **Video indexing and video databases** | Long history of object/OCR/face indexing and metadata databases (e.g. commercial video indexers, patents on object detection metadata and video database query). [Example survey](https://www.researchgate.net/publication/2304226_Indexes_for_User_Access_to_Large_Video_Databases) | These keep metadata in an **external database**. VIReX stores it **in the container** with CRC-checked sections and defined formats. |
| **Long-video token efficiency** | Adaptive keyframe sampling, learnable retrieval ([arXiv](https://arxiv.org/pdf/2312.04931)), GOPAgen reporting ~1/70 of the visual tokens of a dense baseline for a 15 minute video ([arXiv](https://arxiv.org/pdf/2606.06532)). | Same goal (fewer tokens). They select frames per query; VIReX precomputes meaning once. These are also your **baselines** in Phase 4. |

### 5.2 Where the gap is

Nothing found combines all of these:

1. A **file format / container** (not just an algorithm) that holds pixels **and** a semantic stream.
2. A **compact binary encoding** of the semantic layer with reported compression numbers.
3. **Built-in indexes** (temporal, entity, text) inside the file for random access.
4. A **decoder and API** that rebuilds the semantic graph and serves LLM tools with citations.
5. An end-to-end **evaluation vs a conventional frame-sampling pipeline** on storage, latency, tokens and accuracy.

Each of these exists somewhere. The integrated, open, codec-style treatment is the contribution.

## 6. Why it is novel (and what not to claim)

**Defensible claims**

- A **codec-style container for semantic video**: pixel stream + semantic stream + indexes in one file.
- Treating **semantic information as a first-class compressed stream** with measured size and cost trade-offs.
- **Encode once, query many** as the design principle, with an honest break-even analysis.
- An open, reproducible reference implementation and evaluation.

**Do not claim**

- "First to use scene graphs or structured memory for video LLMs." That is not true; see 5.1.
- "First video indexing system." Also not true.
- "Better than X" before Phase 4 measures it. The token-saving result is a hypothesis.

**Suggested framing:** *"Prior work shows structured semantic representations can replace dense frames for video QA. VIReX turns that representation into a persistent, indexed, random-access container format with a decoder API, and evaluates the encode-once trade-off."*

## 6a. Paper contributions (C1–C4)

The paper's headline is a **semantic rate–distortion** framework: rate = bytes of the semantic stream plus index, distortion = accuracy lost versus the full-pixel pipeline. Four contributions carry it. Each phase file has a "Paper contributions in this phase" section that says what to build and record.

| ID | Contribution | Built in | Measured in |
|---|---|---|---|
| **C1** | **Amortized-cost model:** the query count N at which encode-once beats re-analysing per query | cost logging from Phase 1 | Phase 4 |
| **C2** | **Multi-query benchmark:** many questions per video, with full cost accounting (tokens, latency, bytes) | question design in Phase 2, ground truth in Phase 2 | Phase 4 |
| **C3** | **Scalable semantic layers:** L0 entities/objects, L1 events/actions/relations, L2 text, L3 embeddings/captions; decode only the layers a question needs | schema in Phase 1, layer assignment in Phase 2, layered stream in Phase 3 | Phase 4 (rate–accuracy curves) |
| **C4** | **Grounded fallback:** every semantic fact carries a pointer to its pixels so an agent can verify it or fetch the real frame | `PixelRef` in Phase 1, filled in Phase 2, served in Phase 3 | Phase 4 |

## 7. Risks to novelty

| Risk | Mitigation |
|---|---|
| A paper or product already ships a semantic video container | Do a real literature search (keywords below) before writing claims. Update this file with what you find. |
| CSGCoT-style methods are close enough to be called incremental | Lead with the **format + index + API + evaluation**, and cite CSGCoT as the nearest idea. |
| Encode cost outweighs savings for few queries | Report the break-even query count, not just the win. |
| Semantic stream loses detail the question needs | Keep a fallback: the pixel stream stays, so the system can look at frames when the semantics are insufficient. |

## 8. Keywords for a full literature search

`semantic video compression`, `video coding for machines`, `feature coding for machines`, `compressed scene graph video`, `semantic event graph video QA`, `video knowledge graph indexing`, `video database management system`, `queryable video representation`, `structured memory long video LLM`, `video token compression`, `MPEG-7 semantic description`, `MPEG CDVA`

Note: **MPEG-7** (a standard for describing multimedia content with metadata) and **CDVA** (compact descriptors for video analysis) are older standards in the same neighbourhood and are worth reading and citing.

## 9. Sources used

- [Accelerating Long Video Understanding via Compressed Scene Graph (CSGCoT)](https://doi.org/10.1145/3746027.3754765)
- [Semantic Event Graphs for Long-Form Video QA](https://arxiv.org/pdf/2601.06097)
- [Building a Mind Palace](https://arxiv.org/pdf/2501.04336)
- [VideoAgent](https://arxiv.org/pdf/2403.11481)
- [VideoARM](https://arxiv.org/pdf/2512.12360)
- [GOPAgen](https://arxiv.org/pdf/2606.06532)
- [Long Video Understanding with Learnable Retrieval](https://arxiv.org/pdf/2312.04931)
- [Video Coding for Machines: Collaborative Compression and Intelligent Analytics](https://arxiv.org/abs/2001.03569)
- [Real-Time Feature Coding for Machines (MPEG)](https://streaminglearningcenter.com/articles/real-time-feature-coding-for-machines-inside-the-new-mpeg-standard.html)
- [Indexes for User Access to Large Video Databases](https://www.researchgate.net/publication/2304226_Indexes_for_User_Access_to_Large_Video_Databases)
