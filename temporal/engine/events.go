package engine

import (
	"sort"

	"virex/temporal/graph"
	"virex/temporal/tracker"
)

// EventConfig tunes the rules that turn tracks, text and cuts into events.
type EventConfig struct {
	// EdgeMargin is how close to a frame edge (fraction of the frame) a box must start or
	// end to count as entering or leaving through that edge (default 0.05).
	EdgeMargin float64 `json:"edge_margin"`
	// EdgeSides lists the edges that count, as letters: l(eft) r(ight) t(op) b(ottom).
	// Default "lr". The bottom is left out because cameras crop people there all the
	// time, and a cropped torso touching the bottom edge is not someone walking in.
	EdgeSides string `json:"edge_sides"`
	// BoundarySeconds: an entity first seen within this long of the start of the video,
	// or last seen within this long of its end, was already there / is still there, so
	// no appear or leave event fires (default 2).
	BoundarySeconds float64 `json:"boundary_seconds"`
	// NoAppearEvents turns off <class>_appeared / <class>_disappeared (the versions of
	// entered / left for sightings away from the frame edge).
	NoAppearEvents bool `json:"no_appear_events"`
	// TextChangeSeconds: new text that starts within this long after other text ended
	// in the same place is a change of that text, not new text (default 3).
	TextChangeSeconds float64 `json:"text_change_seconds"`
}

func (c EventConfig) withDefaults() EventConfig {
	if c.EdgeMargin <= 0 {
		c.EdgeMargin = 0.05
	}
	if c.EdgeSides == "" {
		c.EdgeSides = "lr"
	}
	if c.BoundarySeconds <= 0 {
		c.BoundarySeconds = 2
	}
	if c.TextChangeSeconds <= 0 {
		c.TextChangeSeconds = 3
	}
	return c
}

func (c EventConfig) atEdge(b tracker.Box) bool {
	for _, s := range c.EdgeSides {
		switch s {
		case 'l':
			if b.X <= c.EdgeMargin {
				return true
			}
		case 'r':
			if b.Right() >= 1-c.EdgeMargin {
				return true
			}
		case 't':
			if b.Y <= c.EdgeMargin {
				return true
			}
		case 'b':
			if b.Bottom() >= 1-c.EdgeMargin {
				return true
			}
		}
	}
	return false
}

// entityEvents emits <class>_entered / <class>_left for tracks that start or end at the
// frame edge, and <class>_appeared / <class>_disappeared for the rest. Tracks that
// touch the start or end of the video emit nothing for that end.
func entityEvents(cfg EventConfig, tracks []*tracker.Track, stream span) []*graph.Event {
	cfg = cfg.withDefaults()
	var out []*graph.Event
	for _, t := range tracks {
		first, last := t.First(), t.Last()
		emit := func(edgeName, inName string, o tracker.Obs) {
			name := inName
			if cfg.atEdge(o.Box) {
				name = edgeName
			} else if cfg.NoAppearEvents {
				return
			}
			b := o.Box
			out = append(out, &graph.Event{
				Type: slug(t.Class) + "_" + name, Start: moment(o), End: moment(o),
				Entities: []int{t.ID}, Region: &b, Frame: o.Frame,
			})
		}
		if first.Time-stream.start > cfg.BoundarySeconds {
			emit("entered", "appeared", first)
		}
		if stream.end-last.Time > cfg.BoundarySeconds {
			emit("left", "disappeared", last)
		}
	}
	return out
}

// textEvents emits text_appeared for new text and text_changed when new text starts in
// the same place shortly after other, different text ended there.
func textEvents(cfg EventConfig, texts []*tracker.TextTrack) []*graph.Event {
	cfg = cfg.withDefaults()
	var out []*graph.Event
	claimed := map[int]bool{} // text tracks already replaced by a successor
	for _, b := range texts {
		bf := b.First()
		var pred *tracker.TextTrack
		bestIoU := 0.3
		for _, a := range texts {
			if a == b || claimed[a.ID] || a.Last().Time > bf.Time || bf.Time-a.Last().Time > cfg.TextChangeSeconds {
				continue
			}
			if tracker.Normalize(a.Text) == tracker.Normalize(b.Text) {
				continue
			}
			if iou := tracker.IoU(a.Box(), bf.Box); iou >= bestIoU {
				pred, bestIoU = a, iou
			}
		}
		typ := "text_appeared"
		if pred != nil {
			claimed[pred.ID] = true
			typ = "text_changed"
		}
		box := bf.Box
		out = append(out, &graph.Event{
			Type: typ, Start: graph.Moment{Ticks: bf.Ticks, Sec: bf.Time}, End: graph.Moment{Ticks: bf.Ticks, Sec: bf.Time},
			Region: &box, Frame: bf.Frame,
		})
	}
	return out
}

// sceneEvents emits scene_changed at every cut the sampler reported.
func sceneEvents(cuts []cut) []*graph.Event {
	var out []*graph.Event
	for _, c := range cuts {
		out = append(out, &graph.Event{Type: "scene_changed", Start: c.at, End: c.at, Frame: c.frame})
	}
	return out
}

// shots splits the video at the cuts. The first shot starts at the first sampled frame.
func shots(cuts []cut, end graph.Moment, firstFrame int, first graph.Moment) []*graph.Shot {
	starts := []cut{{frame: firstFrame, at: first}}
	for _, c := range cuts {
		if c.at.Ticks > first.Ticks {
			starts = append(starts, c)
		}
	}
	sort.SliceStable(starts, func(i, j int) bool { return starts[i].at.Ticks < starts[j].at.Ticks })
	var out []*graph.Shot
	for i, s := range starts {
		to := end
		if i+1 < len(starts) {
			to = starts[i+1].at
		}
		out = append(out, &graph.Shot{Index: i + 1, Start: s.at, End: to, Frame: s.frame})
	}
	return out
}
