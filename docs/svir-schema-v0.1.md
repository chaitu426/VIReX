# SVIR Schema v0.1

Source of truth: `svir/schema/svir.proto`. Generated Go code: `svir/schema/svir.pb.go`. Helpers: `svir/schema/svir.go` (validation, layers, JSON), hand-made test data: `svir/schema/example.go`.

**Status:** written and tested in Phase 1. It is **frozen only when the team signs it off**. The blocks follow `docs/phase-1-foundation.md` week 3, then were checked against MPEG-7, Visual Genome, COCO, AVA and Key-SG/Delta-SG (`research/deep/research/2026-10-01-schema-survey.md`). That survey added `AttributeBlock`, `RelationBlock.id` and the relation lists in `TemporalBlock`. It is a draft; the CSGCoT paper itself could not be read, only its abstract.

## 1. What is in it

| Message | Layer (default) | Fields (besides `layer` and `pixel_ref`) |
|---|---|---|
| `Entity` | L0 | id, type, first_seen, last_seen |
| `ObjectBlock` | L0 | entity_id, class, bbox, confidence, timestamp |
| `EventBlock` | L1 | type, start, end, entity_ids, region, confidence |
| `ActionBlock` | L1 | subject_id, action, start, end, confidence |
| `RelationBlock` | L1 | subject_id, relation, object_id, start, end, confidence, id |
| `AttributeBlock` | L0 | entity_id, key, value, start, end, confidence |
| `SceneBlock` | L1 | label, context, start, end |
| `TemporalBlock` | L1 | kind (KEY / DELTA), t_start, t_end, base_t, active / added / removed entity ids, active / added / removed relation ids |
| `TextBlock` | L2 | content, bbox, confidence, timestamp |
| `EmbeddingBlock` | L3 | model, vector, caption, timestamp |
| `SVIRDocument` | n/a | schema_major/minor, layer, t_start, t_end, and one repeated field per block type |

`TemporalBlock`, `EmbeddingBlock` and `AttributeBlock` go beyond the field lists in the phase doc. The phase doc asks for "Temporal blocks" and an L3 layer ("embeddings and captions") without listing fields, and the survey showed that objects need attributes. Review them.

## 2. Conventions

- **Common fields** use the same numbers in every block: `layer = 14`, `pixel_ref = 15`. Block-specific fields use 1..13. Adding a field later never collides with them.
- **Time** is `int64` ticks of the container header's `timescale`, so semantic times line up exactly with the pixel timestamps.
- **Boxes** are normalised 0..1, origin top-left, so they do not depend on resolution.
- **C4 `PixelRef`** `{t_start, t_end, frame_id (optional), bbox}` is on every block. `frame_id` is `optional` so that frame 0 differs from "no frame".
- **C3 `layer`** is on every block (L0 entity, L1 event, L2 text, L3 embedding). Numbers 1..4 equal the layer ids in the `.virex` section table; `tests/integration` checks this.
- **Presence:** `confidence` is `optional float`, so "unknown" differs from 0.0.
- **Empty is valid:** an empty `SVIRDocument` is what Phase 1 stores.

`schema.Validate` enforces: layer and pixel_ref on every block, `t_start <= t_end`, boxes and confidences within 0..1, and (optionally) that every entity id and relation id used exists and relation ids are unique.

## 3. Changing the schema safely (Protobuf rules)

This is Deep's Part 2, written from the Protobuf documentation rules and the tests in `svir/schema/schema_test.go`. Deep should check it.

| Change | Safe? | Why |
|---|---|---|
| Add a new field with a **new** number | Yes | Old readers keep it as an unknown field (tested: `TestUnknownFieldsSurvive`); new readers see the default for old files |
| Add a new message or a new repeated field in `SVIRDocument` | Yes | Same reason |
| Add a new enum value | Yes | proto3 enums are open: old readers keep the number. They must treat unknown values as "other" |
| Rename a field | Wire yes, **JSON and Go code no** | The wire uses numbers, but JSON export and code use names |
| Remove a field | Only if its number and name go in `reserved` | Otherwise someone may reuse the number |
| **Change a field number** | **No** | Old files then decode into the wrong field |
| **Reuse a removed number** with another meaning or type | **No** | Old files decode as garbage |
| Change a type (for example `int64` to `string`) | **No** | Different wire encoding |
| Change `optional` to plain (or back) | Wire yes, **meaning no** | Presence is lost: 0 and "absent" become the same |
| Change a scalar to `repeated` | Avoid | Behaves differently on old data |
| Change the meaning of a value but keep the type | **No** | Nothing catches this; add a new field instead |

Defaults: in proto3 a missing scalar reads as 0 / "" / false and is not written when it equals the default. Use `optional` where "unset" must be distinguishable.

`reserved`: lists field numbers and names that must never be used again. There are none yet.

**Freeze guard:** `svir/schema/testdata/fields_v0_1.txt` records every field number, type and enum value. `TestSchemaOnlyGrows` fails if any recorded line changes or disappears. Adding fields passes; regenerate the snapshot with `go test ./svir/schema -update` only when you add them on purpose.

## 4. How it is stored in a `.virex` file

- One `SVIRDocument` per SEMANTIC section, serialised as Protobuf. With the default config there is one section with layer `ALL` (255).
- `split_layers` stores one section per layer (L0..L3) instead; each document carries its layer. `schema.Merge` joins them again. No container change is needed (C3).
- `virex-decode FILE -svir-json OUT.json` exports the data as JSON for inspection.
- Phase 1 writes an empty document, or hand-made data with `--semantic fake`. Nothing in `example.go` comes from a model.

## 5. Open points for the team

1. Confirm the `TemporalBlock`, `EmbeddingBlock` and `AttributeBlock` shapes. In particular, read the CSGCoT paper to check that Delta-SG does not also need attribute changes.
2. Should class and action names be strings (now) or ids into a dictionary? Phase 3 compression will want dictionary ids; strings are easier to start with. A later `uint32` field can sit next to the string, so this is not blocking.
3. A human should review the survey (it was written from summaries, and the COCO row from memory) before the freeze.
