package engine

import (
	"math"
	"sort"

	"virex/temporal/graph"
	"virex/temporal/tracker"
)

// defaultHoldClasses are the COCO classes small enough to be carried in a hand.
var defaultHoldClasses = []string{
	"cell phone", "book", "cup", "bottle", "wine glass", "remote", "mouse", "umbrella",
	"handbag", "backpack", "suitcase", "knife", "fork", "spoon", "scissors", "toothbrush",
	"tennis racket", "baseball bat", "skateboard", "frisbee", "sports ball", "banana", "apple",
	"sandwich", "orange", "donut", "cake", "teddy bear",
}

// RelationConfig tunes the geometry rules for relations.
type RelationConfig struct {
	// Disabled turns relation detection off.
	Disabled bool `json:"disabled"`
	// Subjects are the classes that can be the subject of a relation (default
	// ["person"]). Pairs of objects that are neither are never compared, which keeps
	// the number of relations from exploding.
	Subjects []string `json:"subjects"`
	// NearEnter / NearExit: two entities are "near" when the distance between their
	// centres is below this many half-diagonals of the two boxes added together: 1 is
	// about touching. Enter is stricter than exit (hysteresis). Defaults 1.0 and 1.4.
	NearEnter float64 `json:"near_enter"`
	NearExit  float64 `json:"near_exit"`
	// HoldClasses are the object classes a person can "hold" (default: small COCO items).
	HoldClasses []string `json:"hold_classes"`
	// HoldEnter / HoldExit: the share of the object's box inside the person's box needed
	// to start and to keep a "holding" relation (defaults 0.5 and 0.3).
	HoldEnter float64 `json:"hold_enter"`
	HoldExit  float64 `json:"hold_exit"`
	// HoldMaxAreaRatio: an object bigger than this share of the person's box is not
	// held by them (default 0.35).
	HoldMaxAreaRatio float64 `json:"hold_max_area_ratio"`
	// MinHits is how many sightings a relation needs before it counts (default 2).
	MinHits int `json:"min_hits"`
	// GapSeconds is how long a relation survives while its pair fails the test or is
	// not both visible (default 3).
	GapSeconds float64 `json:"gap_seconds"`
}

func (c RelationConfig) withDefaults() RelationConfig {
	if c.Subjects == nil {
		c.Subjects = []string{"person"}
	}
	if c.NearEnter <= 0 {
		c.NearEnter = 1.0
	}
	if c.NearExit <= 0 {
		c.NearExit = 1.4
	}
	if c.HoldClasses == nil {
		c.HoldClasses = defaultHoldClasses
	}
	if c.HoldEnter <= 0 {
		c.HoldEnter = 0.5
	}
	if c.HoldExit <= 0 {
		c.HoldExit = 0.3
	}
	if c.HoldMaxAreaRatio <= 0 {
		c.HoldMaxAreaRatio = 0.35
	}
	if c.MinHits < 1 {
		c.MinHits = 2
	}
	if c.GapSeconds <= 0 {
		c.GapSeconds = 3
	}
	return c
}

// sighting is one track seen in one frame.
type sighting struct {
	track *tracker.Track
	obs   tracker.Obs
}

type relKey struct {
	subject, object int
	name            string
}

type relState struct {
	start    tracker.Obs
	box      tracker.Box
	last     tracker.Obs
	hits     int
	scoreSum float64
}

// relations finds near and holding relations from box geometry, frame by frame, with
// hysteresis so a pair hovering at a threshold does not flicker.
func relations(cfg RelationConfig, tracks []*tracker.Track) []*graph.Relation {
	if cfg.Disabled {
		return nil
	}
	cfg = cfg.withDefaults()

	// Group sightings by frame, frames in time order.
	byFrame := map[int][]sighting{}
	frameTime := map[int]float64{}
	for _, t := range tracks {
		for _, o := range t.Obs {
			byFrame[o.Frame] = append(byFrame[o.Frame], sighting{t, o})
			frameTime[o.Frame] = o.Time
		}
	}
	ids := make([]int, 0, len(byFrame))
	for id := range byFrame {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		if frameTime[ids[i]] != frameTime[ids[j]] {
			return frameTime[ids[i]] < frameTime[ids[j]]
		}
		return ids[i] < ids[j]
	})

	var out []*graph.Relation
	active := map[relKey]*relState{}
	closeState := func(k relKey, st *relState) {
		delete(active, k)
		if st.hits < cfg.MinHits {
			return
		}
		out = append(out, &graph.Relation{
			Subject: k.subject, Name: k.name, Object: k.object,
			Start: moment(st.start), End: moment(st.last),
			Conf: st.scoreSum / float64(st.hits), Frame: st.start.Frame, Box: st.box,
		})
	}

	for _, fid := range ids {
		now := frameTime[fid]
		present := byFrame[fid]
		for _, s := range present {
			if !inSet(cfg.Subjects, s.track.Class) {
				continue
			}
			for _, o := range present {
				if o.track == s.track {
					continue
				}
				// near is symmetric: when both are subjects, report it once (lower id first).
				if !(inSet(cfg.Subjects, o.track.Class) && o.track.ID < s.track.ID) {
					step(active, s, o, "near", nearScore(cfg, s, o, active))
				}
				if s.track.Class == "person" && inSet(cfg.HoldClasses, o.track.Class) {
					step(active, s, o, "holding", holdScore(cfg, s, o, active))
				}
			}
		}
		// Close what has not held for longer than the gap.
		for k, st := range active {
			if now-st.last.Time > cfg.GapSeconds {
				closeState(k, st)
			}
		}
	}
	for k, st := range active {
		closeState(k, st)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Start.Ticks != b.Start.Ticks {
			return a.Start.Ticks < b.Start.Ticks
		}
		if a.Subject != b.Subject {
			return a.Subject < b.Subject
		}
		if a.Object != b.Object {
			return a.Object < b.Object
		}
		return a.Name < b.Name
	})
	return out
}

// step records one frame's verdict for one pair and relation. score < 0 means "does not
// hold"; otherwise it is in 0..1.
func step(active map[relKey]*relState, s, o sighting, name string, score float64) {
	k := relKey{s.track.ID, o.track.ID, name}
	st := active[k]
	if score < 0 {
		return
	}
	if st == nil {
		st = &relState{start: s.obs, box: tracker.Union(s.obs.Box, o.obs.Box)}
		active[k] = st
	}
	st.last = s.obs
	st.hits++
	st.scoreSum += score
}

// nearScore is -1 when the pair is not near, else 1 for touching down to 0 at the exit
// threshold. A pair that is already related is judged by the looser exit threshold.
func nearScore(cfg RelationConfig, s, o sighting, active map[relKey]*relState) float64 {
	d := tracker.CenterDistance(s.obs.Box, o.obs.Box) / (s.obs.Box.Diag()/2 + o.obs.Box.Diag()/2)
	limit := cfg.NearEnter
	if active[relKey{s.track.ID, o.track.ID, "near"}] != nil {
		limit = cfg.NearExit
	}
	if math.IsNaN(d) || d > limit {
		return -1
	}
	return math.Max(0, 1-d/cfg.NearExit)
}

// holdScore is -1 unless the object sits mostly inside the person's box and is small
// next to them; else the share of the object that is inside.
func holdScore(cfg RelationConfig, s, o sighting, active map[relKey]*relState) float64 {
	pb, ob := s.obs.Box, o.obs.Box
	if ob.Area() <= 0 || pb.Area() <= 0 || ob.Area()/pb.Area() > cfg.HoldMaxAreaRatio {
		return -1
	}
	inside := tracker.Intersection(pb, ob) / ob.Area()
	limit := cfg.HoldEnter
	if active[relKey{s.track.ID, o.track.ID, "holding"}] != nil {
		limit = cfg.HoldExit
	}
	if inside < limit {
		return -1
	}
	return inside
}

// assignRelationIDs numbers relations 1..n in time order, after Index has sorted them.
func assignRelationIDs(g *graph.Graph) {
	for i, r := range g.Relations {
		r.ID = i + 1
	}
}
