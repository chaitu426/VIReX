package engine

import (
	"bytes"
	"testing"

	"virex/svir/schema"
	"virex/temporal/graph"
	"virex/temporal/tracker"
)

const ts = 1000 // ticks per second in tests

type scene struct {
	person *tracker.Box // nil = not visible
	phone  *tracker.Box
	text   string
	cut    bool
}

func bx(x, y, w, h float64) *tracker.Box { return &tracker.Box{X: x, Y: y, W: w, H: h} }

// lecture builds a 30 s clip sampled once per second:
//
//	t=5..8    a person walks in from the left edge (0.15 frame widths/s = 0.375 heights/s)
//	t=9..21   they stand at x=0.45
//	t=12..19  a phone is in their hand
//	t=22..24  they walk out through the right edge
//	t=2..13   slide text "Intro to Compression", t=14..29 "Summary" (scene cut at t=14)
func lecture(sec int) scene {
	t := float64(sec)
	var s scene
	switch {
	case t >= 5 && t <= 8:
		s.person = bx(0.15*(t-5), 0.3, 0.1, 0.4)
	case t >= 9 && t <= 21:
		s.person = bx(0.45, 0.3, 0.1, 0.4)
	case t >= 22 && t <= 24:
		s.person = bx(0.45+0.15*(t-21), 0.3, 0.1, 0.4)
	}
	if t >= 12 && t <= 19 {
		s.phone = bx(0.47, 0.5, 0.03, 0.05) // inside the person's box
	}
	switch {
	case t >= 2 && t <= 13:
		s.text = "Intro to Compression"
	case t >= 14 && t <= 29:
		s.text = "Summary"
	}
	s.cut = t == 14
	return s
}

func runLecture(t *testing.T, cfg Config) *graph.Graph {
	t.Helper()
	e := New(cfg, ts)
	for sec := 0; sec <= 29; sec++ {
		sc := lecture(sec)
		f := Frame{ID: sec * 24, Ticks: int64(sec) * ts, Time: float64(sec), Reason: "interval"}
		if sc.cut {
			f.Reason = ReasonScene
		}
		if sc.person != nil {
			f.Detections = append(f.Detections, tracker.Det{Class: "person", Conf: 0.9, Box: *sc.person})
		}
		if sc.phone != nil {
			f.Detections = append(f.Detections, tracker.Det{Class: "cell phone", Conf: 0.8, Box: *sc.phone})
		}
		if sc.text != "" {
			f.Lines = []tracker.TextLine{{Text: sc.text, Conf: 0.9, Box: tracker.Box{X: 0.2, Y: 0.05, W: 0.6, H: 0.08}}}
		}
		if err := e.Add(f); err != nil {
			t.Fatal(err)
		}
	}
	g, err := e.Finish(30 * ts)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func findEvents(g *graph.Graph, typ string) []*graph.Event {
	var out []*graph.Event
	for _, e := range g.Events {
		if e.Type == typ {
			out = append(out, e)
		}
	}
	return out
}

func TestLectureEvents(t *testing.T) {
	g := runLecture(t, DefaultConfig())

	if len(g.Entities) != 2 {
		t.Fatalf("%d entities, want person + phone", len(g.Entities))
	}
	if g.Entities[0].Name() != "person_01" || g.Entities[1].Name() != "cell_phone_01" {
		t.Fatalf("names: %s %s", g.Entities[0].Name(), g.Entities[1].Name())
	}

	enter := findEvents(g, "person_entered")
	if len(enter) != 1 || enter[0].Start.Sec != 5 || enter[0].Entities[0] != 1 {
		t.Fatalf("person_entered: %+v", enter)
	}
	left := findEvents(g, "person_left")
	if len(left) != 1 || left[0].Start.Sec != 24 {
		t.Fatalf("person_left: %+v", left)
	}
	if n := len(findEvents(g, "person_appeared")) + len(findEvents(g, "person_disappeared")); n != 0 {
		t.Fatalf("edge crossings reported as in-frame appearances: %d", n)
	}

	cut := findEvents(g, "scene_changed")
	if len(cut) != 1 || cut[0].Start.Sec != 14 {
		t.Fatalf("scene_changed: %+v", cut)
	}
	if len(g.Shots) != 2 || g.Shots[0].Start.Sec != 0 || g.Shots[1].Start.Sec != 14 || g.Shots[1].End.Sec != 30 {
		t.Fatalf("shots: %+v %+v", g.Shots[0], g.Shots[len(g.Shots)-1])
	}

	app := findEvents(g, "text_appeared")
	chg := findEvents(g, "text_changed")
	if len(app) != 1 || app[0].Start.Sec != 2 || len(chg) != 1 || chg[0].Start.Sec != 14 {
		t.Fatalf("text events: appeared %+v changed %+v", app, chg)
	}
	if len(g.Texts) != 2 || g.Texts[0].Text != "Intro to Compression" || g.Texts[1].Text != "Summary" {
		t.Fatalf("texts: %+v", g.Texts)
	}
}

func TestLectureActions(t *testing.T) {
	g := runLecture(t, DefaultConfig())
	type a struct {
		name       string
		start, end float64
	}
	var got []a
	for _, x := range g.Actions {
		got = append(got, a{x.Name, x.Start.Sec, x.End.Sec})
	}
	want := []a{{"walking", 5, 8}, {"standing", 8, 21}, {"walking", 21, 24}}
	if len(got) != len(want) {
		t.Fatalf("actions %v, want %v", got, want)
	}
	for i := range want {
		// Boundaries may land on either side of the sighting where the speed changes.
		if got[i].name != want[i].name || abs(got[i].start-want[i].start) > 1 || abs(got[i].end-want[i].end) > 1 {
			t.Fatalf("actions %v, want %v", got, want)
		}
	}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func TestLectureRelations(t *testing.T) {
	g := runLecture(t, DefaultConfig())
	var hold *graph.Relation
	for _, r := range g.Relations {
		if r.Name == "holding" {
			hold = r
		}
	}
	if hold == nil {
		t.Fatalf("no holding relation in %+v", g.Relations)
	}
	if hold.Subject != 1 || hold.Object != 2 || hold.Start.Sec != 12 || hold.End.Sec != 19 {
		t.Fatalf("holding: %+v", hold)
	}
	if hold.ID == 0 {
		t.Fatal("relation without id")
	}
	var near bool
	for _, r := range g.Relations {
		if r.Name == "near" && r.Subject == 1 && r.Object == 2 {
			near = true
		}
	}
	if !near {
		t.Fatal("a held phone is also near the person")
	}
}

func TestLectureSVIRIsValidAndComplete(t *testing.T) {
	g := runLecture(t, DefaultConfig())
	d, err := g.ToSVIR(graph.DefaultSVIROptions())
	if err != nil {
		t.Fatal(err)
	}
	// Validate is called inside ToSVIR; check the C3 / C4 promises again, explicitly.
	if err := schema.Validate(d, true); err != nil {
		t.Fatal(err)
	}
	if len(d.Entities) != 2 || len(d.Texts) != 2 || len(d.Actions) == 0 || len(d.Events) == 0 || len(d.Relations) == 0 || len(d.Scenes) != 2 {
		t.Fatalf("counts: ent %d text %d act %d ev %d rel %d scene %d", len(d.Entities), len(d.Texts), len(d.Actions), len(d.Events), len(d.Relations), len(d.Scenes))
	}
	types := map[string]bool{}
	for _, e := range d.Events {
		types[e.Type] = true
	}
	for _, want := range []string{"person_entered", "person_left", "scene_changed", "text_appeared", "text_changed"} {
		if !types[want] {
			t.Errorf("missing event type %s (have %v)", want, types)
		}
	}
	if len(types) < 3 {
		t.Fatalf("exit criterion: at least three event types, got %v", types)
	}
	// Layers (C3).
	for _, x := range d.Objects {
		if x.Layer != schema.Layer_L0_ENTITY {
			t.Fatal("object not in L0")
		}
	}
	for _, x := range d.Texts {
		if x.Layer != schema.Layer_L2_TEXT {
			t.Fatal("text not in L2")
		}
	}
	for _, x := range d.Events {
		if x.Layer != schema.Layer_L1_EVENT {
			t.Fatal("event not in L1")
		}
	}
	// The person is named by an attribute.
	var named bool
	for _, a := range d.Attributes {
		if a.Key == "name" && a.Value == "person_01" && a.EntityId == 1 {
			named = true
		}
	}
	if !named {
		t.Fatal("no name attribute for person_01")
	}
	// Each entity carries a representative frame (C4).
	for _, e := range d.Entities {
		if e.PixelRef.FrameId == nil || e.PixelRef.Bbox == nil {
			t.Fatalf("entity %d has no representative frame/box", e.Id)
		}
	}
	// Round trip through the wire format.
	b, err := schema.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	back, err := schema.Unmarshal(b)
	if err != nil || schema.Validate(back, true) != nil {
		t.Fatalf("round trip: %v", err)
	}
}

func TestEngineIsDeterministic(t *testing.T) {
	enc := func() []byte {
		d, err := runLecture(t, DefaultConfig()).ToSVIR(graph.DefaultSVIROptions())
		if err != nil {
			t.Fatal(err)
		}
		b, err := schema.Marshal(d)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	if !bytes.Equal(enc(), enc()) {
		t.Fatal("two runs on the same input differ")
	}
}

func TestPersonPresentFromTheStartDoesNotEnter(t *testing.T) {
	e := New(DefaultConfig(), ts)
	for sec := 0; sec <= 20; sec++ {
		_ = e.Add(Frame{ID: sec, Ticks: int64(sec) * ts, Time: float64(sec),
			Detections: []tracker.Det{{Class: "person", Conf: 0.9, Box: tracker.Box{X: 0.02, Y: 0.3, W: 0.1, H: 0.5}}}})
	}
	g, err := e.Finish(20 * ts)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Events) != 0 {
		t.Fatalf("events for someone who was always there: %+v", g.Events)
	}
}

func TestAppearAwayFromEdge(t *testing.T) {
	e := New(DefaultConfig(), ts)
	for sec := 0; sec <= 30; sec++ {
		f := Frame{ID: sec, Ticks: int64(sec) * ts, Time: float64(sec)}
		if sec >= 10 && sec <= 20 {
			f.Detections = []tracker.Det{{Class: "cell phone", Conf: 0.9, Box: tracker.Box{X: 0.5, Y: 0.5, W: 0.05, H: 0.08}}}
		}
		_ = e.Add(f)
	}
	g, _ := e.Finish(30 * ts)
	if len(findEvents(g, "cell_phone_appeared")) != 1 || len(findEvents(g, "cell_phone_disappeared")) != 1 {
		t.Fatalf("events: %+v", g.Events)
	}
	cfg := DefaultConfig()
	cfg.Events.NoAppearEvents = true
	e = New(cfg, ts)
	for sec := 0; sec <= 30; sec++ {
		f := Frame{ID: sec, Ticks: int64(sec) * ts, Time: float64(sec)}
		if sec >= 10 && sec <= 20 {
			f.Detections = []tracker.Det{{Class: "cell phone", Conf: 0.9, Box: tracker.Box{X: 0.5, Y: 0.5, W: 0.05, H: 0.08}}}
		}
		_ = e.Add(f)
	}
	g, _ = e.Finish(30 * ts)
	if len(g.Events) != 0 {
		t.Fatalf("NoAppearEvents ignored: %+v", g.Events)
	}
}

func TestObjectMovedEvent(t *testing.T) {
	e := New(DefaultConfig(), ts)
	for sec := 0; sec <= 30; sec++ {
		x := 0.2
		switch {
		case sec >= 12 && sec <= 16:
			x = 0.2 + 0.06*float64(sec-12) // slides right 0.24 in 4 s
		case sec > 16:
			x = 0.44
		}
		_ = e.Add(Frame{ID: sec, Ticks: int64(sec) * ts, Time: float64(sec),
			Detections: []tracker.Det{{Class: "laptop", Conf: 0.9, Box: tracker.Box{X: x, Y: 0.5, W: 0.2, H: 0.15}}}})
	}
	g, _ := e.Finish(30 * ts)
	mv := findEvents(g, "object_moved")
	if len(mv) != 1 {
		t.Fatalf("object_moved events: %+v", mv)
	}
	if mv[0].Start.Sec < 11 || mv[0].Start.Sec > 13 || mv[0].End.Sec < 15 || mv[0].End.Sec > 17 {
		t.Fatalf("movement interval %v..%v", mv[0].Start.Sec, mv[0].End.Sec)
	}
	if len(g.Actions) != 0 {
		t.Fatalf("a laptop got actions: %+v", g.Actions)
	}
}

func TestNoFramesGivesAnEmptyValidDocument(t *testing.T) {
	g, err := New(DefaultConfig(), ts).Finish(10 * ts)
	if err != nil {
		t.Fatal(err)
	}
	d, err := g.ToSVIR(graph.DefaultSVIROptions())
	if err != nil || d.TEnd != 10*ts {
		t.Fatalf("%v %+v", err, d)
	}
}

func TestOutOfOrderFramesAreRejected(t *testing.T) {
	e := New(DefaultConfig(), ts)
	if err := e.Add(Frame{ID: 5, Time: 5, Ticks: 5 * ts}); err != nil {
		t.Fatal(err)
	}
	if err := e.Add(Frame{ID: 1, Time: 1, Ticks: ts}); err == nil {
		t.Fatal("want error")
	}
}

func TestTimelineOrdersEverythingAboutAnEntity(t *testing.T) {
	g := runLecture(t, DefaultConfig())
	items := g.Timeline(1)
	if len(items) < 5 {
		t.Fatalf("timeline too short: %+v", items)
	}
	if items[0].Kind != graph.ItemAppears || items[len(items)-1].Kind != graph.ItemLeaves {
		t.Fatalf("first/last: %+v / %+v", items[0], items[len(items)-1])
	}
	for i := 1; i < len(items); i++ {
		if items[i].Start.Ticks < items[i-1].Start.Ticks {
			t.Fatalf("not in time order at %d", i)
		}
	}
	if g.Timeline(99) != nil {
		t.Fatal("unknown entity has a timeline")
	}
	ents, rels := g.ActiveAt(15)
	if len(ents) != 2 || len(rels) == 0 {
		t.Fatalf("active at 15s: %d entities %d relations", len(ents), len(rels))
	}
	if ents, _ := g.ActiveAt(3); len(ents) != 0 {
		t.Fatal("nobody is on screen at 3 s")
	}
}

func TestDeltaEncodingDropsSmallBoxChanges(t *testing.T) {
	e := New(DefaultConfig(), ts)
	for sec := 0; sec <= 20; sec++ {
		// A person standing still with 0.2 % jitter.
		j := 0.002 * float64(sec%3)
		_ = e.Add(Frame{ID: sec, Ticks: int64(sec) * ts, Time: float64(sec),
			Detections: []tracker.Det{{Class: "person", Conf: 0.9, Box: tracker.Box{X: 0.4 + j, Y: 0.3, W: 0.1, H: 0.5}}}})
	}
	g, _ := e.Finish(20 * ts)
	full, _ := g.ToSVIR(graph.SVIROptions{BBoxDelta: 0})
	delta, _ := g.ToSVIR(graph.SVIROptions{BBoxDelta: 0.02})
	if len(full.Objects) != 21 {
		t.Fatalf("full: %d", len(full.Objects))
	}
	if len(delta.Objects) != 2 { // first and last only
		t.Fatalf("delta-coded: %d object blocks, want 2", len(delta.Objects))
	}
	if delta.Entities[0].FirstSeen != 0 || delta.Entities[0].LastSeen != 20*ts {
		t.Fatal("lifetime must survive delta coding")
	}
}

func TestTemporalBlocksReconstructTheActiveSet(t *testing.T) {
	g := runLecture(t, DefaultConfig())
	d, err := g.ToSVIR(graph.SVIROptions{BBoxDelta: 0.02, KeyIntervalSeconds: 8})
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Temporals) == 0 {
		t.Fatal("no temporal blocks")
	}
	keys := map[int64]*schema.TemporalBlock{}
	var nKey, nDelta int
	for _, b := range d.Temporals {
		if b.Kind == schema.TemporalKind_TEMPORAL_KEY {
			keys[b.TStart] = b
			nKey++
		} else {
			nDelta++
		}
	}
	if nKey < 2 || nDelta == 0 {
		t.Fatalf("keys %d deltas %d: expected several snapshots with deltas between", nKey, nDelta)
	}
	for _, b := range d.Temporals {
		// Rebuild the entity set from KEY + DELTA and compare with the ground truth.
		set := map[uint64]bool{}
		if b.Kind == schema.TemporalKind_TEMPORAL_KEY {
			for _, id := range b.ActiveEntityIds {
				set[id] = true
			}
		} else {
			base := keys[b.BaseT]
			if base == nil {
				t.Fatalf("delta at %d refers to missing key %d", b.TStart, b.BaseT)
			}
			for _, id := range base.ActiveEntityIds {
				set[id] = true
			}
			for _, id := range b.RemovedEntityIds {
				delete(set, id)
			}
			for _, id := range b.AddedEntityIds {
				set[id] = true
			}
		}
		ents, _ := g.ActiveAt(float64(b.TStart) / ts)
		if len(ents) != len(set) {
			t.Fatalf("at %d: rebuilt %v, truth %d entities", b.TStart, set, len(ents))
		}
		for _, e := range ents {
			if !set[uint64(e.ID())] {
				t.Fatalf("at %d: entity %d missing from rebuilt set", b.TStart, e.ID())
			}
		}
	}
}

func TestRelationHysteresisAndHoldRules(t *testing.T) {
	person := tracker.Box{X: 0.4, Y: 0.2, W: 0.15, H: 0.6}
	build := func(objBox func(sec int) tracker.Box, class string) []*graph.Relation {
		e := New(DefaultConfig(), ts)
		for sec := 0; sec <= 12; sec++ {
			_ = e.Add(Frame{ID: sec, Ticks: int64(sec) * ts, Time: float64(sec), Detections: []tracker.Det{
				{Class: "person", Conf: 0.9, Box: person},
				{Class: class, Conf: 0.9, Box: objBox(sec)},
			}})
		}
		g, err := e.Finish(12 * ts)
		if err != nil {
			t.Fatal(err)
		}
		var out []*graph.Relation
		for _, r := range g.Relations {
			if r.Name == "holding" {
				out = append(out, r)
			}
		}
		return out
	}
	// A book drifts slowly out of the person's box: fully inside until t=6, 33 % inside at
	// t=10, 17 % at t=11. Holding starts at 50 % but is kept down to 30 %, so the last
	// sighting is t=10; with a single 50 % threshold it would have ended at t=9.
	drift := func(sec int) tracker.Box {
		return tracker.Box{X: 0.43 + 0.01*float64(sec), Y: 0.5, W: 0.06, H: 0.05}
	}
	got := build(drift, "book")
	if len(got) != 1 || got[0].Start.Sec != 0 || got[0].End.Sec != 10 {
		t.Fatalf("hysteresis: %+v", got)
	}
	// An object that only ever sits at 40 % inside never starts a relation.
	if got := build(func(int) tracker.Box { return tracker.Box{X: 0.55 - 0.024, Y: 0.5, W: 0.06, H: 0.05} }, "book"); len(got) != 0 {
		t.Fatalf("40%% overlap started a relation: %+v", got)
	}
	// A large object (a tv as big as the person) is never held.
	if got := build(func(int) tracker.Box { return tracker.Box{X: 0.4, Y: 0.2, W: 0.15, H: 0.55} }, "tv"); len(got) != 0 {
		t.Fatalf("a tv was held: %+v", got)
	}
	// A chair is not in the holdable list.
	if got := build(func(int) tracker.Box { return tracker.Box{X: 0.45, Y: 0.5, W: 0.04, H: 0.05} }, "chair"); len(got) != 0 {
		t.Fatalf("a chair was held: %+v", got)
	}
	// A phone far away is neither near nor held.
	g := New(DefaultConfig(), ts)
	for sec := 0; sec <= 12; sec++ {
		_ = g.Add(Frame{ID: sec, Ticks: int64(sec) * ts, Time: float64(sec), Detections: []tracker.Det{
			{Class: "person", Conf: 0.9, Box: person},
			{Class: "cell phone", Conf: 0.9, Box: tracker.Box{X: 0.9, Y: 0.8, W: 0.04, H: 0.05}},
		}})
	}
	gr, _ := g.Finish(12 * ts)
	if len(gr.Relations) != 0 {
		t.Fatalf("far-apart pair related: %+v", gr.Relations)
	}
}

func TestRelationsCanBeDisabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Relations.Disabled = true
	g := runLecture(t, cfg)
	if len(g.Relations) != 0 {
		t.Fatal("relations not disabled")
	}
}

func TestStandingPersonWithJitterIsOneStandingAction(t *testing.T) {
	e := New(DefaultConfig(), ts)
	for sec := 0; sec <= 20; sec++ {
		j := 0.003 * float64((sec*7)%5)
		_ = e.Add(Frame{ID: sec, Ticks: int64(sec) * ts, Time: float64(sec),
			Detections: []tracker.Det{{Class: "person", Conf: 0.9, Box: tracker.Box{X: 0.4 + j, Y: 0.3 + j, W: 0.1, H: 0.5}}}})
	}
	g, _ := e.Finish(20 * ts)
	if len(g.Actions) != 1 || g.Actions[0].Name != "standing" || g.Actions[0].Start.Sec != 0 || g.Actions[0].End.Sec != 20 {
		t.Fatalf("actions: %+v", g.Actions)
	}
}
