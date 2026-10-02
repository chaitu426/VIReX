# How others describe video content, and what VIReX copies

| Field | Value |
|---|---|
| **Topic** | MPEG-7, Visual Genome, COCO, AVA and Key-SG/Delta-SG compared on fields, time, position and links, plus what went into SVIR v0.1 |
| **Date** | 2026-10-01 |
| **Author** | Claude, running Deep's task (Deep has not started it) |
| **Task** | Tasks — Chaitanya, notes 1–4 and 6 (note 5, Protobuf rules, is in `docs/svir-schema-v0.1.md` section 3) |
| **Status** | draft, needs a human review |

## 1. Question
How do other systems store what is in a video (fields, time, position, links between things), and what should our Protobuf schema copy?

## 2. Short answer
- Visual Genome gave us **attributes** and **relations as subject–predicate–object with ids**. AVA gave us **one action per person per time, with a person id that persists**. CSGCoT gave us **key and delta scene-graph states**, which we extended to carry relation changes. MPEG-7 and COCO mostly confirmed choices (time ranges, boxes, ids) and showed what to avoid (XML weight, image-only data).
- Three gaps in the first draft were fixed in v0.1 because of this survey: `AttributeBlock`, `RelationBlock.id`, and relation lists in `TemporalBlock`.

## 3. Sources and how reliable they are
- [MPEG-7 and Multimedia Database Systems (Kosch)](https://sigmodrecord.org/publications/sigmodRecord/0206/5.kosch2-new.pdf) and search results on MPEG-7 description schemes — read on 2026-10-01 — Descriptors (D) and Description Schemes (DS); `VideoSegment` with `MediaTime` and `TemporalDecomposition`; XML-based DDL. *Summary level only.*
- [Visual Genome paper](https://arxiv.org/pdf/1602.07332) and the [Python driver](https://github.com/ranjaykrishna/visual_genome_python_driver) — read via summaries on 2026-10-01 — objects, attributes, relationships, region descriptions, scene graphs; driver fields for Image, Region, QA. *Object, attribute and relationship field names were not listed by the driver page.*
- [AVA download page](https://sites.research.google/gr/ava/download/) — read on 2026-10-01 — CSV columns `video_id, middle_frame_timestamp, person_box, action_id, person_id`; seconds from video start; normalised corner boxes; 1-second annotations. *Read directly.*
- [CSGCoT, ACM DL (doi 10.1145/3746027.3754765)](https://doi.org/10.1145/3746027.3754765) — **only the abstract-level summary from a search result**; the ACM page returned 403. CSGCoT = Compressed Scene Graph-enabled Chain-of-Thought. Key-SG for key frames/segments, Delta-SG for delta segments, plus a Key-SG detector, a Delta-SG generator and an SG-Query manager that turns scene graphs into prompts. **The exact node and edge fields were not read.**
- COCO: the website's format page and the cocoapi README did not give the format when fetched. **The COCO row below is from general knowledge and was not re-verified here.**

## 4. Comparison

| | MPEG-7 | Visual Genome | COCO | AVA | Key-SG / Delta-SG |
|---|---|---|---|---|---|
| Unit | Description of a segment or region | One image | One image | One person at one second | A video segment |
| Time | `MediaTime` (time point plus duration) on `VideoSegment`; segments nest by temporal decomposition | None (still images) | None | `middle_frame_timestamp`, seconds from start; 1 s steps | Per segment (key or delta) |
| Position | Spatial regions inside segments | Box per object (x, y, width, height, pixels); region boxes with text | `bbox` = [x, y, w, h] in pixels, plus segmentation polygons or masks | Person box, normalised corners (x1, y1, x2, y2) | Not read |
| Identity | Ids inside the XML | Object ids; names plus WordNet synsets | Annotation id, `category_id` into a `categories` list | `person_id`, kept across frames | Objects in the graph |
| Links | Description-scheme relations, hierarchy | Relationships: subject, predicate, object; attributes "object is [quality]" | None | None (one row per action) | Scene-graph edges (relations); Delta stores changes against a Key |
| Format | XML (verbose) | JSON | JSON | CSV | Method, not a file format |

## 5. What SVIR takes from each

| Idea | From | In SVIR v0.1 |
|---|---|---|
| Time ranges on everything | MPEG-7, AVA | `start`/`end` on interval blocks, `timestamp` on point blocks, in exact ticks (not seconds) |
| Normalised boxes | AVA | `BBox` 0..1, resolution independent. Stored as x, y, w, h like COCO and Visual Genome rather than AVA's corners |
| Entity ids that persist over time | AVA `person_id`, VG object ids | `Entity.id`, used by every block |
| One fact per record, several per box | AVA | One `ActionBlock` per action; the same entity can have many |
| Relations as subject–predicate–object | Visual Genome | `RelationBlock`, now with an `id` so other blocks can point at it |
| Attributes separate from objects | Visual Genome | **New** `AttributeBlock` (entity, key, value, interval) |
| Free-text region descriptions | Visual Genome | Covered by `EmbeddingBlock.caption` (with `pixel_ref.bbox`) and `SceneBlock.context` |
| Key and delta states | CSGCoT | `TemporalBlock` KEY/DELTA; **extended** with relation id lists (`active`, `added`, `removed`) |
| Segment hierarchy | MPEG-7 | Not now. A `parent` field can be added later without breaking files |

Deliberately not copied:
- **XML and the MPEG-7 descriptor library:** too heavy for the compression goal; Protobuf is compact.
- **Segmentation masks (COCO):** large and not needed for the Phase 2 questions. Could be added as a new optional field.
- **WordNet synsets and category tables:** useful for a dictionary in Phase 3. A `class_id` next to the string `class` can be added then.
- **Seconds as floats:** floats cannot hold every frame time exactly.

## 6. Decision / recommendation
Keep SVIR as in `svir/schema/svir.proto` (now with `AttributeBlock`, `RelationBlock.id` and relation lists in `TemporalBlock`). The freeze snapshot confirms the change only added fields. Before final sign-off a human should read the CSGCoT paper itself to check that `TemporalBlock` matches its Key-SG/Delta-SG definition.

## 7. Open questions
1. Do Key-SG and Delta-SG in the paper store attributes as well as relations? If so, add attribute id lists to `TemporalBlock`.
2. Do we need object keypoints or masks in Phase 2 (pose is a stretch goal in the tech-stack doc)?
3. Should scenes nest (scene contains shots)? MPEG-7 does.

## 8. Glossary
| Short | Full form |
|---|---|
| MPEG-7 | Multimedia Content Description Interface |
| D / DS | Descriptor / Description Scheme (MPEG-7) |
| DDL | Data Definition Language (MPEG-7, based on XML Schema) |
| COCO | Common Objects in Context |
| AVA | Atomic Visual Actions |
| CSGCoT | Compressed Scene Graph-enabled Chain-of-Thought |
| Key-SG / Delta-SG | Key-frame / Delta-frame Scene Graph |
| WordNet synset | A set of synonyms with one meaning in the WordNet lexicon |
| bbox | bounding box |
