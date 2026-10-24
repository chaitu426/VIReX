package graph

import (
	"fmt"
	"math"
	"sort"

	"virex/svir/schema"
	"virex/temporal/tracker"
)

// SVIROptions control how the graph is written out.
type SVIROptions struct {
	// BBoxDelta is the smallest box change (any of x, y, w, h, as a fraction of the
	// frame) that earns an entity a new ObjectBlock. Smaller moves are implied by the
	// entity's lifetime. 0 keeps every observation.
	BBoxDelta float64 `json:"bbox_delta"`
	// KeyIntervalSeconds is how often a full TemporalBlock snapshot (KEY) is written;
	// between snapshots only changes against it (DELTA) are. Default 30.
	KeyIntervalSeconds float64 `json:"key_interval_seconds"`
}

// DefaultSVIROptions are the values used when nothing is configured.
func DefaultSVIROptions() SVIROptions {
	return SVIROptions{BBoxDelta: 0.02, KeyIntervalSeconds: 30}
}

func (o SVIROptions) withDefaults() SVIROptions {
	if o.KeyIntervalSeconds <= 0 {
		o.KeyIntervalSeconds = 30
	}
	return o
}

func pbox(b tracker.Box) *schema.BBox {
	b = b.Clamp()
	return &schema.BBox{X: float32(b.X), Y: float32(b.Y), W: float32(b.W), H: float32(b.H)}
}

func pboxPtr(b *tracker.Box) *schema.BBox {
	if b == nil {
		return nil
	}
	return pbox(*b)
}

// pref builds the pixel_ref every block needs (C4): the time range, the frame and the
// region the fact was derived from.
func pref(t0, t1 int64, frame int, box *tracker.Box) *schema.PixelRef {
	r := &schema.PixelRef{TStart: t0, TEnd: max(t0, t1), Bbox: pboxPtr(box)}
	if frame != NoFrame {
		f := int64(frame)
		r.FrameId = &f
	}
	return r
}

func conf(c float64) *float32 {
	v := float32(math.Min(math.Max(c, 0), 1))
	return &v
}

func confPtr(c *float64) *float32 {
	if c == nil {
		return nil
	}
	return conf(*c)
}

// keptObs returns the observations worth an ObjectBlock: the first, the last, and every
// one whose box moved or resized by more than delta since the last kept one.
func keptObs(t *tracker.Track, delta float64) []tracker.Obs {
	if delta <= 0 || len(t.Obs) <= 2 {
		return t.Obs
	}
	out := []tracker.Obs{t.Obs[0]}
	for _, o := range t.Obs[1 : len(t.Obs)-1] {
		if boxChange(out[len(out)-1].Box, o.Box) > delta {
			out = append(out, o)
		}
	}
	return append(out, t.Last())
}

func boxChange(a, b tracker.Box) float64 {
	return math.Max(math.Max(math.Abs(a.X-b.X), math.Abs(a.Y-b.Y)), math.Max(math.Abs(a.W-b.W), math.Abs(a.H-b.H)))
}

// ToSVIR writes the graph as an SVIR document. Every block gets its layer (C3, by block
// type) and a pixel_ref back to the frame it came from (C4). The result is checked with
// schema.Validate before it is returned.
func (g *Graph) ToSVIR(opt SVIROptions) (*schema.SVIRDocument, error) {
	opt = opt.withDefaults()
	if g.byID == nil {
		g.Index()
	}
	if err := g.Validate(); err != nil {
		return nil, err
	}
	d := schema.NewDocument()
	d.TStart, d.TEnd = 0, g.Duration.Ticks

	for _, e := range g.Entities {
		t := e.Track
		best := t.Best()
		d.Entities = append(d.Entities, &schema.Entity{
			Id: uint64(t.ID), Type: t.Class,
			FirstSeen: t.First().Ticks, LastSeen: t.Last().Ticks,
			// The representative frame: the most confident sighting.
			PixelRef: pref(t.First().Ticks, t.Last().Ticks, best.Frame, &best.Box),
		})
		d.Attributes = append(d.Attributes, &schema.AttributeBlock{
			EntityId: uint64(t.ID), Key: "name", Value: t.Name(),
			Start: t.First().Ticks, End: t.Last().Ticks,
			PixelRef: pref(t.First().Ticks, t.Last().Ticks, best.Frame, &best.Box),
		})
		for _, o := range keptObs(t, opt.BBoxDelta) {
			b := o.Box
			d.Objects = append(d.Objects, &schema.ObjectBlock{
				EntityId: uint64(t.ID), Class: t.Class, Bbox: pbox(b), Confidence: conf(o.Conf),
				Timestamp: o.Ticks, PixelRef: pref(o.Ticks, o.Ticks, o.Frame, &b),
			})
		}
	}

	for _, tt := range g.Texts {
		best := tt.Best()
		box := best.Box
		first, last := tt.First(), tt.Last()
		d.Texts = append(d.Texts, &schema.TextBlock{
			Content: tt.Text, Bbox: pbox(box), Confidence: conf(tt.Conf), Timestamp: first.Ticks,
			// The block lives from first to last reading; the frame is the best reading.
			PixelRef: pref(first.Ticks, last.Ticks, best.Frame, &box),
		})
	}

	for _, a := range g.Actions {
		d.Actions = append(d.Actions, &schema.ActionBlock{
			SubjectId: uint64(a.Subject), Action: a.Name, Start: a.Start.Ticks, End: a.End.Ticks, Confidence: conf(a.Conf),
			PixelRef: pref(a.Start.Ticks, a.End.Ticks, a.Frame, a.Box),
		})
	}
	for _, e := range g.Events {
		ev := &schema.EventBlock{
			Type: e.Type, Start: e.Start.Ticks, End: e.End.Ticks, Region: pboxPtr(e.Region), Confidence: confPtr(e.Conf),
			PixelRef: pref(e.Start.Ticks, e.End.Ticks, e.Frame, e.Region),
		}
		for _, id := range e.Entities {
			ev.EntityIds = append(ev.EntityIds, uint64(id))
		}
		d.Events = append(d.Events, ev)
	}
	for _, r := range g.Relations {
		box := r.Box
		d.Relations = append(d.Relations, &schema.RelationBlock{
			Id: uint64(r.ID), SubjectId: uint64(r.Subject), Relation: r.Name, ObjectId: uint64(r.Object),
			Start: r.Start.Ticks, End: r.End.Ticks, Confidence: conf(r.Conf),
			PixelRef: pref(r.Start.Ticks, r.End.Ticks, r.Frame, &box),
		})
	}
	for _, s := range g.Shots {
		d.Scenes = append(d.Scenes, &schema.SceneBlock{
			// No description without a vision-language model; the label only numbers the shot.
			Label: fmt.Sprintf("shot_%02d", s.Index), Start: s.Start.Ticks, End: s.End.Ticks,
			PixelRef: pref(s.Start.Ticks, s.End.Ticks, s.Frame, nil),
		})
	}
	d.Temporals = g.temporalBlocks(opt)

	schema.Normalize(d)
	if err := schema.Validate(d, true); err != nil {
		return nil, fmt.Errorf("graph: generated SVIR is invalid: %w", err)
	}
	return d, nil
}

// temporalBlocks writes the scene-graph state over time as KEY snapshots and DELTA
// blocks against the last snapshot (the Key-SG / Delta-SG idea of the schema).
func (g *Graph) temporalBlocks(opt SVIROptions) []*schema.TemporalBlock {
	type span struct {
		from, to int64 // [from, to) in ticks
		id       uint64
	}
	var ents, rels []span
	pts := map[int64]bool{}
	for _, e := range g.Entities {
		s := span{e.First().Ticks, e.Last().Ticks + 1, uint64(e.ID())}
		ents = append(ents, s)
		pts[s.from], pts[s.to] = true, true
	}
	for _, r := range g.Relations {
		s := span{r.Start.Ticks, r.End.Ticks + 1, uint64(r.ID)}
		rels = append(rels, s)
		pts[s.from], pts[s.to] = true, true
	}
	if len(pts) == 0 {
		return nil
	}
	times := make([]int64, 0, len(pts))
	for t := range pts {
		times = append(times, t)
	}
	sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })

	active := func(spans []span, t int64) map[uint64]bool {
		m := map[uint64]bool{}
		for _, s := range spans {
			if s.from <= t && t < s.to {
				m[s.id] = true
			}
		}
		return m
	}
	sorted := func(m map[uint64]bool) []uint64 {
		out := make([]uint64, 0, len(m))
		for id := range m {
			out = append(out, id)
		}
		sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
		return out
	}
	diff := func(a, b map[uint64]bool) []uint64 { // in a, not in b
		m := map[uint64]bool{}
		for id := range a {
			if !b[id] {
				m[id] = true
			}
		}
		return sorted(m)
	}

	keyEvery := int64(opt.KeyIntervalSeconds * float64(g.Timescale))
	var out []*schema.TemporalBlock
	var keyT int64
	var keyEnts, keyRels map[uint64]bool
	haveKey := false
	for i := 0; i+1 < len(times); i++ {
		t0, t1 := times[i], times[i+1]
		ce, cr := active(ents, t0), active(rels, t0)
		if len(ce) == 0 && len(cr) == 0 {
			haveKey = false // a gap: the next segment starts a fresh snapshot
			continue
		}
		pr := pref(t0, t1, NoFrame, nil)
		if !haveKey || t0-keyT >= keyEvery {
			haveKey, keyT, keyEnts, keyRels = true, t0, ce, cr
			out = append(out, &schema.TemporalBlock{
				Kind: schema.TemporalKind_TEMPORAL_KEY, TStart: t0, TEnd: t1,
				ActiveEntityIds: sorted(ce), ActiveRelationIds: sorted(cr), PixelRef: pr,
			})
			continue
		}
		out = append(out, &schema.TemporalBlock{
			Kind: schema.TemporalKind_TEMPORAL_DELTA, TStart: t0, TEnd: t1, BaseT: keyT,
			AddedEntityIds: diff(ce, keyEnts), RemovedEntityIds: diff(keyEnts, ce),
			AddedRelationIds: diff(cr, keyRels), RemovedRelationIds: diff(keyRels, cr), PixelRef: pr,
		})
	}
	return out
}
