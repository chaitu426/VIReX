# Container Format — VIReX

| Field | Value |
|---|---|
| **Topic** | How the `.virex` file is organised, how it compares with MP4/Matroska, how semantic data should be stored, and how the format should evolve |
| **Date** | 2026-09-30 |
| **Author** | Chaitanya |
| **Task** | Tasks — Chaitanya, Container format |
| **Status** | draft |

## 1. Question

1. How are **MP4** and **Matroska** files organised (header, sections, index)?
2. Can we hide our data inside a normal MP4 so players still play it? Or do we make our own format?
3. What should our file layout be (header, a table of sections, a checksum per section)?
4. How do we handle new versions of the format later?

---

## 2. Short answer

VIReX should use a **custom `.virex` container** as specified by the Phase 1 and Tech Stack documents, rather than making `.virex` itself an MP4 file.

The reason is that VIReX needs a container for multiple application-specific sections: a compressed H.264 pixel stream, semantic data, temporal indexes, future AI indexes, and metadata. A custom container gives us direct random access to these sections and lets the section table carry the required `layer` field.

MP4 is still useful as a reference and interoperability format. It already solves media storage, timestamps, sample tables, and seeking. VIReX should reuse the **H.264 bitstream** produced by FFmpeg rather than reimplement video encoding. An optional later export can produce a normal MP4 for ordinary players.

---

## 3. Sources

- [Apple — Atoms / QuickTime File Format](https://developer.apple.com/documentation/quicktime-file-format/atoms) — read on 2026-09-30 — explains the hierarchical box/atom structure, size/type fields, nesting, and skipping unknown atoms for forward compatibility.
- [Apple — Sample Size Atom (`stsz`)](https://developer.apple.com/documentation/quicktime-file-format/sample_size_atom) — read on 2026-09-30 — explains MP4 sample-size tables and the relationship between sample tables and media locations.
- [Apple — QuickTime and ISO Base Media File Format](https://developer.apple.com/documentation/technotes/tn3177-understanding-alternate-audio-track-groups-in-movie-files) — read on 2026-09-30 — confirms that MP4 is a well-known member of the ISO Base Media File Format family.
- [Matroska — Data Layout](https://www.matroska.org/technical/diagram.html) — read on 2026-09-30 — describes the EBML Header, Segment, SeekHead, Info, Tracks, Cluster, Cues, Attachments, and Tags.
- [Matroska — Cues](https://www.matroska.org/technical/cues.html) — read on 2026-09-30 — explains the timestamp-to-cluster index used for seeking.
- [Matroska — Element Ordering](https://www.matroska.org/technical/ordering.html) — read on 2026-09-30 — explains SeekHead and Cues placement and seeking considerations.
- [Matroska — Implementation Recommendations](https://www.matroska.org/technical/matroska_implement.html) — read on 2026-09-30 — provides practical layout recommendations for efficient playback and later editing.
- [C2PA Technical Specification — BMFF `uuid` box](https://spec.c2pa.org/specifications/specifications/2.0/specs/_attachments/C2PA_Specification.pdf) — read on 2026-09-30 — provides a real example of embedding application-specific data in BMFF using a `uuid` box and discusses playback compatibility.
- [RFC 1952 — GZIP File Format Specification](https://www.rfc-editor.org/rfc/rfc1952.html) — read on 2026-09-30 — documents CRC-32 as an integrity check for detecting data corruption.

---

# 4. Findings

## 4.1 MP4 / ISO BMFF structure

MP4 is based on the ISO Base Media File Format (ISO BMFF). Its basic unit is a **box** (also called an atom in the QuickTime terminology). A box has a size and type and may contain data or other boxes. Boxes are hierarchical. Unknown boxes can be skipped using their size, which provides a form of forward compatibility. [Apple — Atoms](https://developer.apple.com/documentation/quicktime-file-format/atoms)

A simplified MP4 structure is:

```text
MP4 / ISO BMFF
│
├── ftyp
│   └── File type / compatibility information
│
├── moov
│   ├── Movie information
│   ├── Video track
│   │   └── Sample tables / timing / locations
│   └── Audio track
│       └── Sample tables / timing / locations
│
└── mdat
    └── Encoded media samples
```

The exact layout can be more complex, but the important idea is that MP4 separates **metadata/index information** from the actual media samples.

The sample-table structures are important for seeking. For example, `stsz` stores sample sizes, while the broader sample table provides information needed to map media time to samples and sample locations. [Apple — Sample Size Atom](https://developer.apple.com/documentation/quicktime-file-format/sample_size_atom)

### MP4 strengths

- Mature media format.
- Existing support in FFmpeg and many players.
- Existing timestamp and sample/index mechanisms.
- Existing codec integration.
- Efficient random access when its indexes are present.
- Extensible box structure.

### MP4 weakness for the VIReX container

VIReX does not only need a video container. It needs a container whose primary abstraction is:

```text
pixel stream
semantic stream
temporal index
AI/entity/text indexes
metadata
```

The Phase 1 design also requires a `layer` identifier in section entries so semantic information can later be split by layer.

A custom section table is therefore simpler for the VIReX implementation than forcing all VIReX sections into MP4's media-track and metadata model.

---

## 4.2 Matroska / MKV structure

Matroska uses **EBML (Extensible Binary Meta Language)** rather than MP4's box structure.

A simplified Matroska file is:

```text
Matroska
│
├── EBML Header
│
└── Segment
    ├── SeekHead
    ├── Info
    ├── Tracks
    ├── Chapters
    ├── Cluster
    ├── Cues
    ├── Attachments
    └── Tags
```

This is the structure shown by the official Matroska documentation. [Matroska — Data Layout](https://www.matroska.org/technical/diagram.html)

Important parts:

| Element | Purpose |
|---|---|
| EBML Header | Identifies the EBML document/document type |
| Segment | Main Matroska container |
| SeekHead | Positions of important top-level elements |
| Info | Segment information |
| Tracks | Audio/video/subtitle track definitions |
| Cluster | Contains media blocks |
| Cues | Time-based seeking index |
| Attachments | Attached files such as fonts/images |
| Tags | Metadata |

`Cues` maps timestamps to cluster positions to make seeking faster. [Matroska — Cues](https://www.matroska.org/technical/cues.html)

Matroska therefore demonstrates an important design principle for VIReX:

> **Separate the data from the index that tells a reader where the data is.**

Matroska also demonstrates that a container can be designed so new information can be added without making the whole format unusable.

---

## 4.3 MP4 vs Matroska vs VIReX

| Requirement | MP4 | Matroska | VIReX |
|---|---|---|---|
| File identification | `ftyp` | EBML Header | `VIRX` magic |
| Main structure | Boxes | EBML elements | Fixed header + section table |
| Media data | `mdat` samples | Clusters/Blocks | Pixel section |
| Media metadata | `moov` | Info/Tracks | Header + metadata section |
| Media seeking | Sample tables | Cues | Temporal index section |
| Custom semantic stream | Possible | Possible | Native requirement |
| Application-specific sections | Possible but requires fitting into MP4 model | Possible | Native requirement |
| Per-section CRC32 | Not the core MP4 model | Supported in parts of Matroska | Explicit requirement |
| Semantic `layer` in section entry | Not naturally part of MP4 | Not the VIReX model | Explicit requirement |
| Implementation for Phase 1 | More format machinery than needed | More format machinery than needed | Small custom Go implementation |

---

# 5. Can VIReX data be put inside a normal MP4?

## Yes, technically.

ISO BMFF has extension mechanisms, and the C2PA specification is a real example of putting application-specific information into a BMFF file using a `uuid` box.

However, there is an important compatibility limitation.

C2PA specifically uses a `uuid` box because some Chromium-based browsers can fail playback when they encounter an unknown top-level box. This demonstrates that:

> **"The container specification allows an extension" does not mean "every player will tolerate every extension."**

Therefore, putting VIReX data into an MP4 could work, but it would need compatibility testing across the players we care about.

### Why not make `.virex` simply be MP4?

Because the project requirement is not just:

```text
video + metadata
```

It is:

```text
video
+ semantic stream
+ temporal index
+ future AI/entity/text indexes
+ metadata
+ per-section CRC32
+ layer-aware section table
```

The custom `.virex` container can make these first-class sections.

### Recommended interoperability strategy

Use:

```text
.virex
    ↓
Canonical VIReX container
    ↓
Contains H.264 pixel stream + semantic/index sections
```

and later optionally support:

```text
.virex
    ↓
virex-export
    ↓
.mp4
    ↓
Ordinary video player
```

This separates the **native VIReX format** from the **ordinary-player interchange format**.

---

# 6. Proposed `.virex` layout

The Phase 1 document already specifies:

```text
magic bytes | header | section table |
pixel section | semantic section |
index section | metadata
```

I recommend keeping this design.

## Proposed layout

```text
┌──────────────────────────────────────────────────┐
│                  .virex FILE                     │
├──────────────────────────────────────────────────┤
│ MAGIC                                            │
│ "VIRX"                                           │
├──────────────────────────────────────────────────┤
│ HEADER                                           │
│                                                  │
│ format_version                                  │
│ minimum_reader_version                           │
│ header_size                                      │
│ flags                                            │
│ width                                            │
│ height                                           │
│ fps / timebase                                   │
│ duration                                         │
│ pixel_codec = H.264                              │
│ semantic_schema_version                          │
│ section_count                                    │
├──────────────────────────────────────────────────┤
│ SECTION TABLE                                    │
│                                                  │
│ Section 0: PIXEL                                 │
│ Section 1: SEMANTIC                              │
│ Section 2: TEMPORAL_INDEX                        │
│ Section 3: METADATA                              │
│ ...                                              │
│                                                  │
│ Each entry:                                      │
│   type                                           │
│   layer                                          │
│   offset                                         │
│   length                                         │
│   CRC32                                          │
├──────────────────────────────────────────────────┤
│ PIXEL SECTION                                    │
│                                                  │
│ H.264 encoded pixel stream                       │
├──────────────────────────────────────────────────┤
│ SEMANTIC SECTION                                 │
│                                                  │
│ Protobuf SVIR v0.1                               │
│ Entity / Object / Text / Action / Event / etc.   │
├──────────────────────────────────────────────────┤
│ TEMPORAL INDEX                                   │
│                                                  │
│ timestamp → pixel offset / keyframe information  │
├──────────────────────────────────────────────────┤
│ METADATA                                         │
│                                                  │
│ Additional file/application metadata             │
└──────────────────────────────────────────────────┘
```

---

# 7. Header

The exact binary sizes should be finalized during implementation, but the logical fields should be:

```text
Header
├── magic
├── format_version
├── minimum_reader_version
├── header_size
├── flags
├── width
├── height
├── fps/timebase
├── duration
├── pixel_codec
├── semantic_schema_version
└── section_count
```

### Why include `minimum_reader_version`?

Suppose VIReX version 2 adds a new optional section.

A version-1 reader may still be able to read the video and old semantic sections.

For example:

```text
format_version = 2
minimum_reader_version = 1
```

means a version-1 reader is allowed to open the file if it knows how to skip the new section.

This is preferable to treating every new feature as a completely incompatible format.

---

# 8. Section table

The section table is the most important part of the custom design.

Each entry should contain:

```text
SectionEntry
├── type
├── layer
├── offset
├── length
└── crc32
```

Conceptually:

```text
┌──────────────┬────────┬────────┬────────┬─────────┐
│ Type         │ Layer  │ Offset │ Length │ CRC32   │
├──────────────┼────────┼────────┼────────┼─────────┤
│ PIXEL        │ N/A    │ ...    │ ...    │ ...     │
│ SEMANTIC     │ L0     │ ...    │ ...    │ ...     │
│ SEMANTIC     │ L1     │ ...    │ ...    │ ...     │
│ TEMPORAL     │ N/A    │ ...    │ ...    │ ...     │
│ METADATA     │ N/A    │ ...    │ ...    │ ...     │
└──────────────┴────────┴────────┴────────┴─────────┘
```

The `offset` and `length` fields allow the reader to jump directly to a section.

This satisfies the Phase 1 exit criterion:

> The container reader can jump to the semantic section without reading the pixel section.

---

# 9. Why `layer` belongs in the section table

The Phase 1 document requires these SVIR layers:

```text
L0_ENTITY
L1_EVENT
L2_TEXT
L3_EMBEDDING
```

The section table should reserve a `layer` field now.

For example:

```text
SEMANTIC | L0_ENTITY | offset | length | crc32
SEMANTIC | L1_EVENT  | offset | length | crc32
SEMANTIC | L2_TEXT   | offset | length | crc32
SEMANTIC | L3_EMBEDDING | offset | length | crc32
```

In v0.1 there may be only one semantic section:

```text
SEMANTIC | layer=ALL
```

Later, it can be split into separate sections without changing the overall container structure.

This directly supports the C3 requirement in the Phase 1 document.

---

# 10. CRC32 per section

CRC32 should be stored for each section.

Example:

```text
Section
├── type
├── layer
├── offset
├── length
├── data
└── CRC32
```

When reading:

```text
stored CRC32
      │
      ▼
read section bytes
      │
      ▼
calculate CRC32
      │
      ├── equal → section passes integrity check
      │
      └── different → section is damaged
```

CRC32 is appropriate for detecting accidental corruption. RFC 1952 describes CRC-32 as a corruption-detection mechanism and recommends validating integrity using the CRC-32 value. [RFC 1952](https://www.rfc-editor.org/rfc/rfc1952.html)

However:

> **CRC32 is not a security mechanism.**

It does not prove that the data was not maliciously modified because someone can modify the data and calculate a new CRC32.

For Phase 1, CRC32 is sufficient because the requirement is corruption detection. Cryptographic hashes or signatures can be added later if VIReX eventually needs authenticity/tamper evidence.

---

# 11. Versioning strategy

VIReX should use **additive evolution** where possible.

## Recommended fields

```text
format_version
minimum_reader_version
semantic_schema_version
```

### Rule 1 — Unknown sections must be skippable

If a reader does not recognize a section type:

```text
read type
    ↓
unknown?
    ↓
use offset + length to skip it
```

It must not interpret unknown bytes as a known structure.

This is consistent with the forward-compatible principle documented for MP4 atoms: an unknown atom should not be interpreted and can be skipped using its size. [Apple — Atoms](https://developer.apple.com/documentation/quicktime-file-format/atoms)

### Rule 2 — Add fields, do not change old fields' meaning

For example:

```text
v0.1
Object:
    entity_id
    class
    bbox
    confidence
```

A future version can add:

```text
v0.2
Object:
    entity_id
    class
    bbox
    confidence
    tracking_source
```

but should not silently change the meaning or encoding of an existing field.

### Rule 3 — Breaking changes require a major format version

For example:

```text
v1 → v1.1
```

for compatible additions.

```text
v1 → v2
```

for incompatible structural changes.

### Rule 4 — SVIR and container versions are separate

The container and semantic schema should not share one version number.

Example:

```text
container format = 1
SVIR schema = 0.2
```

This allows the semantic schema to evolve independently from the physical file container.

---

# 12. Why not use MP4 as the actual `.virex` container?

MP4 is technically capable and is an excellent video container. However, the project documents already define a **custom `.virex` container** with a section table and custom indexes.

Using MP4 directly would introduce unnecessary constraints:

```text
MP4 model
    ↓
adapt semantic/index data to MP4 boxes/tracks/metadata
    ↓
deal with player behavior around extensions
```

The custom design is simpler:

```text
VIReX requirements
    ↓
custom section table
    ↓
pixel + semantic + indexes
```

The project also explicitly wants to demonstrate custom indexing in Go in Phase 3. A custom container makes the boundary between container sections and those indexes straightforward.

---

# 13. Decision matrix

| Decision factor | MP4-based `.virex` | Custom `.virex` |
|---|---|---|
| Existing video ecosystem | **Strong** | Requires export/player integration |
| H.264 support | **Strong** | Store H.264 bitstream |
| Existing media indexing | **Strong** | Must implement VIReX indexes |
| Multiple custom semantic/index sections | Possible | **Natural** |
| `layer` in section table | Awkward | **Natural** |
| Per-section CRC32 | Possible | **Natural** |
| Direct section access | Possible but not VIReX-native | **Direct** |
| Phase 1 specified layout | Different | **Matches** |
| Phase 3 custom indexes | Possible | **Matches architecture** |
| Implementation complexity for VIReX container | Medium | **Low for required v0.1 scope** |
| Ordinary player playback | Better if exported as `.mp4` | Not guaranteed |
| Long-term VIReX-specific evolution | Good | **Better control** |

---

# 14. Final decision / recommendation

## **Decision: Use a custom `.virex` container.**

The `.virex` file should **not be an MP4 file with hidden VIReX data**.

Instead, VIReX should define its own small container:

```text
VIRX header
    ↓
section table
    ↓
H.264 pixel section
    ↓
SVIR semantic section
    ↓
temporal/index sections
    ↓
metadata
```

Each section should have:

```text
type
layer
offset
length
CRC32
```

### Why this is the chosen approach

1. It directly matches the Phase 1 requirement.
2. It makes pixel and semantic streams independent.
3. The reader can jump directly to semantic data.
4. The `layer` field can be added now for C3.
5. CRC32 can validate each section.
6. Future temporal/entity/text/AI indexes can become additional sections.
7. The container can evolve without changing the SVIR schema.
8. The video encoding problem is still handled by FFmpeg/H.264; VIReX does not need to implement a video codec.

### What we reject

**Rejected: making `.virex` simply an MP4 file with a custom metadata box.**

Reason: although MP4 supports extensibility and real-world standards use BMFF `uuid` boxes, the VIReX requirements are broader than metadata. The native format needs a first-class section table, semantic layers, per-section CRC32, and application-specific indexes.

**Rejected: making a completely custom video codec/container.**

Reason: VIReX does not need to reinvent video encoding. FFmpeg already provides H.264 encoding. The custom part should be the VIReX container around the encoded pixel stream and semantic/index data.

---

# 15. Recommended v0.1 layout

The first implementation should be deliberately small:

```text
.virex v0.1

┌─────────────────────────────┐
│ Magic: "VIRX"               │
├─────────────────────────────┤
│ Header                      │
│ - format version            │
│ - minimum reader version    │
│ - width / height            │
│ - fps / timebase             │
│ - duration                  │
│ - H.264 codec               │
│ - SVIR schema version       │
│ - section count             │
├─────────────────────────────┤
│ Section Table               │
│                             │
│ type                        │
│ layer                       │
│ offset                      │
│ length                      │
│ CRC32                       │
├─────────────────────────────┤
│ Pixel Section               │
│ H.264 stream                │
├─────────────────────────────┤
│ Semantic Section            │
│ SVIR Protobuf v0.1          │
│                             │
│ Empty in initial skeleton   │
├─────────────────────────────┤
│ Temporal Index Section      │
│ Optional / minimal in v0.1  │
├─────────────────────────────┤
│ Metadata Section            │
│ Optional                    │
└─────────────────────────────┘
```

For the **minimal Phase 1 skeleton**, it is acceptable for the semantic section to be empty, exactly as the project specification says.

---

# 16. Experiments (if any)

No implementation experiment was run for this research note.

The research was based on inspection of the format specifications/documentation listed in the Sources section.

The first implementation experiment should be done during Week 4:

```text
virex-encode input.mp4 -o out.virex
virex-decode out.virex -o out.mp4
```

Then verify:

```text
1. out.virex can be opened by the VIReX reader.
2. The reader can jump directly to the semantic section.
3. CRC32 checks pass.
4. out.mp4 is playable.
5. H.264 quality is checked using FFmpeg SSIM/PSNR.
```

Do not record an experiment result until the commands have actually been run.

---

# 17. Open questions

1. What exact binary widths should the header fields use?
2. Should offsets be absolute file offsets or relative to the beginning of the section table/file?
3. Should the section table itself have a CRC32?
4. Should `layer` have an `ALL`/`NONE` value for non-semantic sections?
5. Should the temporal index be mandatory in v0.1 or optional?
6. Should the semantic section be Protobuf directly or optionally zstd-compressed?
7. What exact H.264 representation will the pixel section store: Annex B elementary stream or another representation?
8. Should `.virex` eventually have an MP4 export command for ordinary video players?
9. What is the exact major/minor versioning policy the team wants to freeze?
10. Should future cryptographic hashes/signatures be added separately from CRC32?

These should be confirmed before the container writer/reader API is frozen.

---

# 18. Glossary

| Short form | Full form |
|---|---|
| MP4 | MPEG-4 Part 14 |
| MPEG | Moving Picture Experts Group |
| ISO BMFF / ISOBMFF | ISO Base Media File Format |
| MKV | Matroska video file/container |
| EBML | Extensible Binary Meta Language |
| CRC32 | Cyclic Redundancy Check, 32-bit |
| H.264 | Advanced Video Coding (AVC), a video compression standard |
| SVIR | VIReX semantic schema/data representation used by the project |
| PTS | Presentation Timestamp |
| FPS | Frames Per Second |
| AI | Artificial Intelligence |
| UUID | Universally Unique Identifier |
| VLM | Vision-Language Model |
| PSNR | Peak Signal-to-Noise Ratio |
| SSIM | Structural Similarity Index |

---

## Final recommendation

**Implement the custom `.virex` container from the project documents.**

Use MP4/ISO BMFF and Matroska as design references, but do not make `.virex` an MP4 wrapper. Keep FFmpeg/H.264 responsible for the pixel stream, and make the VIReX container responsible for combining that stream with SVIR semantics and future indexes.

The first implementation should therefore be:

```text
VIRX header
      +
section table
      +
H.264 pixel section
      +
SVIR semantic section
      +
temporal/index sections
      +
metadata
      +
CRC32 per section
```
