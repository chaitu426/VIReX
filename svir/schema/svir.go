package schema

import (
	"fmt"
	"math"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// Schema version written into every document and into the .virex header.
const (
	VersionMajor = 0
	VersionMinor = 1
)

// NewDocument returns an empty document stamped with the current schema version.
func NewDocument() *SVIRDocument {
	return &SVIRDocument{SchemaMajor: VersionMajor, SchemaMinor: VersionMinor}
}

// Marshal serialises a document to Protobuf bytes (what the SEMANTIC section holds).
func Marshal(d *SVIRDocument) ([]byte, error) {
	return proto.MarshalOptions{Deterministic: true}.Marshal(d)
}

// Unmarshal parses Protobuf bytes. Empty input gives an empty document.
func Unmarshal(b []byte) (*SVIRDocument, error) {
	d := &SVIRDocument{}
	if err := proto.Unmarshal(b, d); err != nil {
		return nil, err
	}
	return d, nil
}

// ToJSON exports a document as indented JSON for inspection and debugging.
func ToJSON(d *SVIRDocument) ([]byte, error) {
	return protojson.MarshalOptions{Multiline: true, Indent: "  ", UseProtoNames: true}.Marshal(d)
}

// FromJSON parses the output of ToJSON.
func FromJSON(b []byte) (*SVIRDocument, error) {
	d := &SVIRDocument{}
	if err := (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(b, d); err != nil {
		return nil, err
	}
	return d, nil
}

// DefaultLayer is the layer a block type belongs to when its layer field is unset.
func DefaultLayer(block proto.Message) Layer {
	switch block.(type) {
	case *Entity, *ObjectBlock, *AttributeBlock:
		return Layer_L0_ENTITY
	case *EventBlock, *ActionBlock, *RelationBlock, *SceneBlock, *TemporalBlock:
		return Layer_L1_EVENT
	case *TextBlock:
		return Layer_L2_TEXT
	case *EmbeddingBlock:
		return Layer_L3_EMBEDDING
	}
	return Layer_LAYER_UNSPECIFIED
}

// block is what every SVIR block has in common.
type block interface {
	proto.Message
	GetLayer() Layer
	GetPixelRef() *PixelRef
}

// blocks lists every block in the document, in a fixed order.
func blocks(d *SVIRDocument) []block {
	var out []block
	for _, b := range d.Entities {
		out = append(out, b)
	}
	for _, b := range d.Objects {
		out = append(out, b)
	}
	for _, b := range d.Texts {
		out = append(out, b)
	}
	for _, b := range d.Actions {
		out = append(out, b)
	}
	for _, b := range d.Events {
		out = append(out, b)
	}
	for _, b := range d.Relations {
		out = append(out, b)
	}
	for _, b := range d.Scenes {
		out = append(out, b)
	}
	for _, b := range d.Temporals {
		out = append(out, b)
	}
	for _, b := range d.Embeddings {
		out = append(out, b)
	}
	for _, b := range d.Attributes {
		out = append(out, b)
	}
	return out
}

// Normalize fills in the default layer on every block whose layer is unset.
func Normalize(d *SVIRDocument) {
	for _, e := range d.Entities {
		setLayer(&e.Layer, e)
	}
	for _, b := range d.Objects {
		setLayer(&b.Layer, b)
	}
	for _, b := range d.Texts {
		setLayer(&b.Layer, b)
	}
	for _, b := range d.Actions {
		setLayer(&b.Layer, b)
	}
	for _, b := range d.Events {
		setLayer(&b.Layer, b)
	}
	for _, b := range d.Relations {
		setLayer(&b.Layer, b)
	}
	for _, b := range d.Scenes {
		setLayer(&b.Layer, b)
	}
	for _, b := range d.Temporals {
		setLayer(&b.Layer, b)
	}
	for _, b := range d.Embeddings {
		setLayer(&b.Layer, b)
	}
	for _, b := range d.Attributes {
		setLayer(&b.Layer, b)
	}
}

func setLayer(l *Layer, b proto.Message) {
	if *l == Layer_LAYER_UNSPECIFIED {
		*l = DefaultLayer(b)
	}
}

// Validate checks the rules every v0.1 document must meet:
//   - every block has a layer and a pixel_ref (C3, C4) with t_start <= t_end;
//   - boxes lie inside 0..1 and confidences inside 0..1;
//   - if checkRefs is set, every entity id used by a block exists in Entities.
func Validate(d *SVIRDocument, checkRefs bool) error {
	for i, b := range blocks(d) {
		name := fmt.Sprintf("%T #%d", b, i)
		if b.GetLayer() == Layer_LAYER_UNSPECIFIED {
			return fmt.Errorf("svir: %s has no layer", name)
		}
		pr := b.GetPixelRef()
		if pr == nil {
			return fmt.Errorf("svir: %s has no pixel_ref", name)
		}
		if pr.TStart > pr.TEnd {
			return fmt.Errorf("svir: %s pixel_ref has t_start > t_end", name)
		}
		if err := checkBox(pr.Bbox); err != nil {
			return fmt.Errorf("svir: %s pixel_ref: %w", name, err)
		}
	}
	for i, o := range d.Objects {
		if err := firstErr(checkBox(o.Bbox), checkConf(o.Confidence)); err != nil {
			return fmt.Errorf("svir: ObjectBlock #%d: %w", i, err)
		}
	}
	for i, t := range d.Texts {
		if err := firstErr(checkBox(t.Bbox), checkConf(t.Confidence)); err != nil {
			return fmt.Errorf("svir: TextBlock #%d: %w", i, err)
		}
	}
	for i, a := range d.Actions {
		if a.Start > a.End {
			return fmt.Errorf("svir: ActionBlock #%d has start > end", i)
		}
		if err := checkConf(a.Confidence); err != nil {
			return fmt.Errorf("svir: ActionBlock #%d: %w", i, err)
		}
	}
	for i, e := range d.Events {
		if e.Start > e.End {
			return fmt.Errorf("svir: EventBlock #%d has start > end", i)
		}
		if err := firstErr(checkBox(e.Region), checkConf(e.Confidence)); err != nil {
			return fmt.Errorf("svir: EventBlock #%d: %w", i, err)
		}
	}
	for i, r := range d.Relations {
		if r.Start > r.End {
			return fmt.Errorf("svir: RelationBlock #%d has start > end", i)
		}
	}
	for i, a := range d.Attributes {
		if a.Start > a.End {
			return fmt.Errorf("svir: AttributeBlock #%d has start > end", i)
		}
		if err := checkConf(a.Confidence); err != nil {
			return fmt.Errorf("svir: AttributeBlock #%d: %w", i, err)
		}
	}
	for i, s := range d.Scenes {
		if s.Start > s.End {
			return fmt.Errorf("svir: SceneBlock #%d has start > end", i)
		}
	}
	for i, e := range d.Entities {
		if e.FirstSeen > e.LastSeen {
			return fmt.Errorf("svir: Entity #%d has first_seen > last_seen", i)
		}
	}
	if !checkRefs {
		return nil
	}
	ids := map[uint64]bool{}
	for _, e := range d.Entities {
		ids[e.Id] = true
	}
	need := func(what string, i int, id uint64) error {
		if !ids[id] {
			return fmt.Errorf("svir: %s #%d refers to unknown entity %d", what, i, id)
		}
		return nil
	}
	for i, o := range d.Objects {
		if err := need("ObjectBlock", i, o.EntityId); err != nil {
			return err
		}
	}
	for i, a := range d.Actions {
		if err := need("ActionBlock", i, a.SubjectId); err != nil {
			return err
		}
	}
	for i, a := range d.Attributes {
		if err := need("AttributeBlock", i, a.EntityId); err != nil {
			return err
		}
	}
	relIDs := map[uint64]bool{}
	for i, r := range d.Relations {
		if r.Id == 0 {
			continue
		}
		if relIDs[r.Id] {
			return fmt.Errorf("svir: RelationBlock #%d repeats id %d", i, r.Id)
		}
		relIDs[r.Id] = true
	}
	for i, t := range d.Temporals {
		for _, list := range [][]uint64{t.ActiveRelationIds, t.AddedRelationIds, t.RemovedRelationIds} {
			for _, id := range list {
				if !relIDs[id] {
					return fmt.Errorf("svir: TemporalBlock #%d refers to unknown relation %d", i, id)
				}
			}
		}
		for _, list := range [][]uint64{t.ActiveEntityIds, t.AddedEntityIds, t.RemovedEntityIds} {
			for _, id := range list {
				if err := need("TemporalBlock", i, id); err != nil {
					return err
				}
			}
		}
	}
	for i, r := range d.Relations {
		if err := firstErr(need("RelationBlock", i, r.SubjectId), need("RelationBlock", i, r.ObjectId)); err != nil {
			return err
		}
	}
	for i, e := range d.Events {
		for _, id := range e.EntityIds {
			if err := need("EventBlock", i, id); err != nil {
				return err
			}
		}
	}
	return nil
}

func firstErr(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}

func checkBox(b *BBox) error {
	if b == nil {
		return nil
	}
	for _, v := range []float32{b.X, b.Y, b.W, b.H} {
		if math.IsNaN(float64(v)) || v < 0 || v > 1 {
			return fmt.Errorf("bbox value %v outside 0..1", v)
		}
	}
	if b.X+b.W > 1.0001 || b.Y+b.H > 1.0001 {
		return fmt.Errorf("bbox extends past the frame")
	}
	return nil
}

func checkConf(c *float32) error {
	if c != nil && (math.IsNaN(float64(*c)) || *c < 0 || *c > 1) {
		return fmt.Errorf("confidence %v outside 0..1", *c)
	}
	return nil
}

// SplitByLayer returns one document per layer present, each stamped with that layer
// (C3). The .virex writer stores each as its own SEMANTIC section. Call Normalize first.
func SplitByLayer(d *SVIRDocument) map[Layer]*SVIRDocument {
	out := map[Layer]*SVIRDocument{}
	get := func(l Layer) *SVIRDocument {
		if out[l] == nil {
			out[l] = &SVIRDocument{
				SchemaMajor: d.SchemaMajor, SchemaMinor: d.SchemaMinor,
				Layer: l, TStart: d.TStart, TEnd: d.TEnd,
			}
		}
		return out[l]
	}
	for _, b := range d.Entities {
		x := get(b.Layer)
		x.Entities = append(x.Entities, b)
	}
	for _, b := range d.Objects {
		x := get(b.Layer)
		x.Objects = append(x.Objects, b)
	}
	for _, b := range d.Texts {
		x := get(b.Layer)
		x.Texts = append(x.Texts, b)
	}
	for _, b := range d.Actions {
		x := get(b.Layer)
		x.Actions = append(x.Actions, b)
	}
	for _, b := range d.Events {
		x := get(b.Layer)
		x.Events = append(x.Events, b)
	}
	for _, b := range d.Relations {
		x := get(b.Layer)
		x.Relations = append(x.Relations, b)
	}
	for _, b := range d.Scenes {
		x := get(b.Layer)
		x.Scenes = append(x.Scenes, b)
	}
	for _, b := range d.Temporals {
		x := get(b.Layer)
		x.Temporals = append(x.Temporals, b)
	}
	for _, b := range d.Embeddings {
		x := get(b.Layer)
		x.Embeddings = append(x.Embeddings, b)
	}
	for _, b := range d.Attributes {
		x := get(b.Layer)
		x.Attributes = append(x.Attributes, b)
	}
	return out
}

// Merge combines documents (for example the per-layer sections of one file).
func Merge(docs ...*SVIRDocument) *SVIRDocument {
	m := NewDocument()
	for _, d := range docs {
		m.Entities = append(m.Entities, d.Entities...)
		m.Objects = append(m.Objects, d.Objects...)
		m.Texts = append(m.Texts, d.Texts...)
		m.Actions = append(m.Actions, d.Actions...)
		m.Events = append(m.Events, d.Events...)
		m.Relations = append(m.Relations, d.Relations...)
		m.Scenes = append(m.Scenes, d.Scenes...)
		m.Temporals = append(m.Temporals, d.Temporals...)
		m.Embeddings = append(m.Embeddings, d.Embeddings...)
		m.Attributes = append(m.Attributes, d.Attributes...)
	}
	return m
}
