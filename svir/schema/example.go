package schema

// Example returns hand-made SVIR for an imaginary lecture clip, so the container and
// the tools can be tested before any ML exists. Times are in ticks; timescale is
// ticks per second; durationSec is the clip length in seconds. Nothing here comes
// from a real model.
func Example(timescale int64, durationSec float64) *SVIRDocument {
	t := func(sec float64) int64 { return int64(sec * float64(timescale)) }
	end := t(durationSec)
	ref := func(a, b float64, frame int64, box *BBox) *PixelRef {
		return &PixelRef{TStart: t(a), TEnd: t(b), FrameId: &frame, Bbox: box}
	}
	box := func(x, y, w, h float32) *BBox { return &BBox{X: x, Y: y, W: w, H: h} }
	f := func(v float32) *float32 { return &v }

	d := NewDocument()
	d.TEnd = end
	d.Entities = []*Entity{
		{Id: 1, Type: "person", FirstSeen: 0, LastSeen: end, PixelRef: ref(0, durationSec, 0, nil)},
		{Id: 2, Type: "slide", FirstSeen: t(durationSec * 0.2), LastSeen: end, PixelRef: ref(durationSec*0.2, durationSec, 60, nil)},
	}
	d.Objects = []*ObjectBlock{
		{EntityId: 1, Class: "person", Bbox: box(0.10, 0.20, 0.30, 0.70), Confidence: f(0.97), Timestamp: t(1), PixelRef: ref(1, 1, 30, box(0.10, 0.20, 0.30, 0.70))},
		{EntityId: 2, Class: "screen", Bbox: box(0.50, 0.10, 0.45, 0.60), Confidence: f(0.91), Timestamp: t(durationSec * 0.3), PixelRef: ref(durationSec*0.3, durationSec*0.3, 90, box(0.50, 0.10, 0.45, 0.60))},
	}
	d.Texts = []*TextBlock{
		{Content: "Intro to Compression", Bbox: box(0.52, 0.12, 0.40, 0.08), Confidence: f(0.95), Timestamp: t(durationSec * 0.3), PixelRef: ref(durationSec*0.3, durationSec*0.3, 90, box(0.52, 0.12, 0.40, 0.08))},
	}
	d.Actions = []*ActionBlock{
		{SubjectId: 1, Action: "speaking", Start: t(0.5), End: end, Confidence: f(0.80), PixelRef: ref(0.5, durationSec, 15, nil)},
	}
	d.Events = []*EventBlock{
		{Type: "slide_change", Start: t(durationSec * 0.2), End: t(durationSec * 0.2), EntityIds: []uint64{2}, Region: box(0.5, 0.1, 0.45, 0.6), Confidence: f(0.90), PixelRef: ref(durationSec*0.2, durationSec*0.2, 60, box(0.5, 0.1, 0.45, 0.6))},
	}
	d.Relations = []*RelationBlock{
		{Id: 1, SubjectId: 1, Relation: "left_of", ObjectId: 2, Start: 0, End: end, PixelRef: ref(0, durationSec, 0, nil)},
	}
	d.Scenes = []*SceneBlock{
		{Label: "lecture hall", Context: "A speaker stands beside a projected slide.", Start: 0, End: end, PixelRef: ref(0, durationSec, 0, nil)},
	}
	d.Temporals = []*TemporalBlock{
		{Kind: TemporalKind_TEMPORAL_KEY, TStart: 0, TEnd: t(durationSec * 0.2), ActiveEntityIds: []uint64{1}, PixelRef: ref(0, durationSec*0.2, 0, nil)},
		{Kind: TemporalKind_TEMPORAL_DELTA, TStart: t(durationSec * 0.2), TEnd: end, BaseT: 0, AddedEntityIds: []uint64{2}, AddedRelationIds: []uint64{1}, PixelRef: ref(durationSec*0.2, durationSec, 60, nil)},
	}
	d.Attributes = []*AttributeBlock{
		{EntityId: 2, Key: "state", Value: "showing title slide", Start: t(durationSec * 0.2), End: end, Confidence: f(0.85), PixelRef: ref(durationSec*0.2, durationSec, 60, nil)},
	}
	d.Embeddings = []*EmbeddingBlock{
		{Model: "none (hand-made)", Vector: []float32{0.1, 0.2, 0.3, 0.4}, Caption: "A person next to a slide", Timestamp: t(durationSec * 0.3), PixelRef: ref(durationSec*0.3, durationSec*0.3, 90, nil)},
	}
	Normalize(d)
	return d
}
