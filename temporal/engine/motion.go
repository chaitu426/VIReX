package engine

import (
	"math"
	"sort"

	"virex/temporal/graph"
	"virex/temporal/tracker"
)

// MotionConfig tunes walking/standing actions and object_moved events.
type MotionConfig struct {
	// Classes get walking / standing actions (default ["person"]). Every other class gets
	// object_moved events instead.
	Classes []string `json:"classes"`
	// WalkEnter and WalkExit are the speed, in body heights per second, at which a
	// person starts and stops walking (defaults 0.30 and 0.15). Two thresholds
	// (hysteresis) stop a speed hovering around one value from flapping.
	WalkEnter float64 `json:"walk_enter"`
	WalkExit  float64 `json:"walk_exit"`
	// MinSegmentSeconds: a stretch of walking or standing shorter than this is absorbed
	// by its neighbours (default 1).
	MinSegmentSeconds float64 `json:"min_segment_seconds"`
	// ObjectEnter and ObjectExit are the same thresholds for other objects, in frame
	// widths per second (defaults 0.04 and 0.015).
	ObjectEnter float64 `json:"object_enter"`
	ObjectExit  float64 `json:"object_exit"`
	// ObjectMinMove is how far (fraction of the frame) an object must travel during a
	// movement for an object_moved event (default 0.03).
	ObjectMinMove float64 `json:"object_min_move"`
}

func (c MotionConfig) withDefaults() MotionConfig {
	if c.Classes == nil {
		c.Classes = []string{"person"}
	}
	if c.WalkEnter <= 0 {
		c.WalkEnter = 0.30
	}
	if c.WalkExit <= 0 {
		c.WalkExit = 0.15
	}
	if c.MinSegmentSeconds <= 0 {
		c.MinSegmentSeconds = 1
	}
	if c.ObjectEnter <= 0 {
		c.ObjectEnter = 0.04
	}
	if c.ObjectExit <= 0 {
		c.ObjectExit = 0.015
	}
	if c.ObjectMinMove <= 0 {
		c.ObjectMinMove = 0.03
	}
	return c
}

// segment is a run of observations in one motion state: obs[i0]..obs[i1] inclusive.
type segment struct {
	moving bool
	i0, i1 int
}

func dur(obs []tracker.Obs, s segment) float64 { return obs[s.i1].Time - obs[s.i0].Time }

// segments splits a track into moving and still stretches.
//
// Speed between consecutive sightings is net displacement over time, divided by a
// reference size (a person's height) so it does not depend on how far from the camera
// they are. Each pair's speed is smoothed over its neighbours, because box jitter makes
// single pairs noisy, then run through a two-threshold state machine, and finally
// stretches shorter than minSeg are absorbed into their neighbours.
func segments(obs []tracker.Obs, relative bool, enter, exit, minSeg float64) []segment {
	n := len(obs)
	if n < 2 {
		return nil
	}
	dist := make([]float64, n-1)
	dt := make([]float64, n-1)
	ref := make([]float64, n-1)
	for k := 0; k < n-1; k++ {
		dist[k] = tracker.CenterDistance(obs[k].Box, obs[k+1].Box)
		dt[k] = math.Max(obs[k+1].Time-obs[k].Time, 1e-3)
		ref[k] = 1
		if relative {
			ref[k] = math.Max((obs[k].Box.H+obs[k+1].Box.H)/2, 1e-3)
		}
	}
	speed := make([]float64, n-1)
	for k := range speed {
		var d, t, r, cnt float64
		for j := max(k-1, 0); j <= min(k+1, n-2); j++ {
			d += dist[j]
			t += dt[j]
			r += ref[j]
			cnt++
		}
		speed[k] = d / t / (r / cnt)
	}

	var segs []segment
	moving := false
	for k := range speed {
		switch {
		case !moving && speed[k] >= enter:
			moving = true
		case moving && speed[k] < exit:
			moving = false
		}
		if len(segs) > 0 && segs[len(segs)-1].moving == moving {
			segs[len(segs)-1].i1 = k + 1
		} else {
			segs = append(segs, segment{moving, k, k + 1})
		}
	}

	// Absorb short stretches, shortest first, until none is left.
	for len(segs) > 1 {
		si, sd := -1, minSeg
		for i, s := range segs {
			if d := dur(obs, s); d < sd {
				si, sd = i, d
			}
		}
		if si < 0 {
			break
		}
		segs[si].moving = !segs[si].moving
		merged := segs[:1]
		for _, s := range segs[1:] {
			if last := &merged[len(merged)-1]; last.moving == s.moving {
				last.i1 = s.i1
			} else {
				merged = append(merged, s)
			}
		}
		segs = merged
	}
	return segs
}

// motion derives actions (walking / standing) and object_moved events from the tracks.
func motion(cfg MotionConfig, tracks []*tracker.Track) ([]*graph.Action, []*graph.Event) {
	cfg = cfg.withDefaults()
	var acts []*graph.Action
	var evs []*graph.Event
	for _, t := range tracks {
		person := inSet(cfg.Classes, t.Class)
		enter, exit := cfg.ObjectEnter, cfg.ObjectExit
		if person {
			enter, exit = cfg.WalkEnter, cfg.WalkExit
		}
		for _, s := range segments(t.Obs, person, enter, exit, cfg.MinSegmentSeconds) {
			if dur(t.Obs, s) < cfg.MinSegmentSeconds {
				continue // the whole track is shorter than a segment: say nothing
			}
			a, b := t.Obs[s.i0], t.Obs[s.i1]
			if person {
				box := a.Box
				name := "standing"
				if s.moving {
					name = "walking"
				}
				acts = append(acts, &graph.Action{
					Subject: t.ID, Name: name, Start: moment(a), End: moment(b),
					Conf: meanConf(t.Obs[s.i0 : s.i1+1]), Frame: a.Frame, Box: &box,
				})
				continue
			}
			if s.moving && tracker.CenterDistance(a.Box, b.Box) >= cfg.ObjectMinMove {
				region := tracker.Union(a.Box, b.Box)
				evs = append(evs, &graph.Event{
					Type: "object_moved", Start: moment(a), End: moment(b),
					Entities: []int{t.ID}, Region: &region, Frame: a.Frame,
				})
			}
		}
	}
	sort.SliceStable(acts, func(i, j int) bool { return acts[i].Start.Ticks < acts[j].Start.Ticks })
	return acts, evs
}

// meanConf is the average detector confidence over the sightings of a segment. The
// rules themselves are deterministic, so this is the only uncertainty we can pass on.
func meanConf(obs []tracker.Obs) float64 {
	var s float64
	for _, o := range obs {
		s += o.Conf
	}
	return s / float64(len(obs))
}
