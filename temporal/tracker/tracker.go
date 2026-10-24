package tracker

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// Det is one detection in one frame.
type Det struct {
	Class string
	Conf  float64
	Box   Box
}

// Frame identifies the frame a batch of detections came from.
type Frame struct {
	ID    int     // 0-based display-order frame index
	Ticks int64   // presentation time in container ticks
	Time  float64 // presentation time in seconds; must not decrease between calls
	Cut   bool    // the sampler saw a scene cut at this frame
}

// Obs is one detection that was assigned to a track.
type Obs struct {
	Frame int
	Ticks int64
	Time  float64
	Box   Box
	Conf  float64
}

// Track is one object followed over time.
type Track struct {
	ID    int // unique, 1-based, assigned at Finish in order of first appearance
	Seq   int // 1-based count within the class: person_01 is Seq 1
	Class string
	Obs   []Obs // in time order, one per frame at most
}

// First and Last observation.
func (t *Track) First() Obs { return t.Obs[0] }
func (t *Track) Last() Obs  { return t.Obs[len(t.Obs)-1] }

// Duration is last seen minus first seen, in seconds.
func (t *Track) Duration() float64 { return t.Last().Time - t.First().Time }

// Name is the readable id, such as person_01 or cell_phone_02.
func (t *Track) Name() string {
	return fmt.Sprintf("%s_%02d", strings.ReplaceAll(strings.ToLower(strings.TrimSpace(t.Class)), " ", "_"), t.Seq)
}

// Best is the most confident observation: the representative frame of the track.
func (t *Track) Best() Obs {
	b := t.Obs[0]
	for _, o := range t.Obs[1:] {
		if o.Conf > b.Conf {
			b = o
		}
	}
	return b
}

// Config tunes the object tracker. Zero numeric fields take the defaults below.
type Config struct {
	// MatchIoU is the overlap that links a detection to a track (default 0.3).
	MatchIoU float64 `json:"match_iou"`
	// DistGate also links detections whose centre moved less than this many mean box
	// diagonals, if the size is similar. People move far between samples a second
	// apart, so overlap alone would break their tracks (default 0.6).
	DistGate float64 `json:"dist_gate"`
	// MaxGapSeconds is how long a track survives without a detection (default 3, so a
	// couple of missed samples at one sample per second are bridged).
	// It is time, not frames, because sampled frames are irregularly spaced.
	MaxGapSeconds float64 `json:"max_gap_seconds"`
	// MinHits is how many frames must see an object before it becomes a track; fewer
	// are treated as detector flicker and dropped (default 2).
	MinHits int `json:"min_hits"`
	// MinConf drops detections below this confidence (default 0: the server filters).
	MinConf float64 `json:"min_conf"`
	// BreakOnCut ends every track at a scene cut. Off by default: a person stays the
	// same person when the slide behind them changes.
	BreakOnCut bool `json:"break_on_cut"`
	// NoPredict turns off the constant-velocity box prediction.
	NoPredict bool `json:"no_predict"`
}

func (c Config) withDefaults() Config {
	if c.MatchIoU <= 0 {
		c.MatchIoU = 0.3
	}
	if c.DistGate <= 0 {
		c.DistGate = 0.6
	}
	if c.MaxGapSeconds <= 0 {
		c.MaxGapSeconds = 3
	}
	if c.MinHits < 1 {
		c.MinHits = 2
	}
	return c
}

// sizeRatioMax is the biggest area ratio two boxes may have and still be "the same size".
const sizeRatioMax = 2.5

// Tracker follows detections over time. Not safe for concurrent use.
type Tracker struct {
	cfg      Config
	active   []*Track
	done     []*Track
	lastTime float64
	started  bool
}

// New builds a tracker.
func New(cfg Config) *Tracker { return &Tracker{cfg: cfg.withDefaults()} }

// Update feeds one frame's detections. Frames must come in time order.
func (t *Tracker) Update(f Frame, dets []Det) error {
	if t.started && f.Time < t.lastTime {
		return fmt.Errorf("tracker: frame %d at %.3fs is earlier than the previous frame (%.3fs)", f.ID, f.Time, t.lastTime)
	}
	t.started, t.lastTime = true, f.Time

	if f.Cut && t.cfg.BreakOnCut {
		t.closeAll()
	}

	var kept []Det
	for _, d := range dets {
		if d.Conf >= t.cfg.MinConf && d.Box.Area() > 0 {
			kept = append(kept, d)
		}
	}

	// Association is per class: a "person" never turns into a "chair".
	classes := map[string]bool{}
	for _, d := range kept {
		classes[d.Class] = true
	}
	var alive []*Track
	for _, tr := range t.active {
		if f.Time-tr.Last().Time <= t.cfg.MaxGapSeconds {
			alive = append(alive, tr)
		} else {
			t.retire(tr)
		}
	}
	t.active = alive
	for _, tr := range t.active {
		classes[tr.Class] = true
	}
	names := make([]string, 0, len(classes))
	for c := range classes {
		names = append(names, c)
	}
	sort.Strings(names) // deterministic output

	for _, class := range names {
		var ds []Det
		for _, d := range kept {
			if d.Class == class {
				ds = append(ds, d)
			}
		}
		var trs []*Track
		for _, tr := range t.active {
			if tr.Class == class {
				trs = append(trs, tr)
			}
		}
		t.associate(f, class, ds, trs)
	}
	return nil
}

func (t *Tracker) associate(f Frame, class string, ds []Det, trs []*Track) {
	matched := make([]bool, len(ds))
	if len(ds) > 0 && len(trs) > 0 {
		cost := make([][]float64, len(ds))
		for i, d := range ds {
			cost[i] = make([]float64, len(trs))
			for j, tr := range trs {
				cost[i][j] = t.cost(f, d.Box, tr)
			}
		}
		for i, j := range Assign(cost) {
			if j < 0 || cost[i][j] >= forbidden/2 {
				continue
			}
			trs[j].Obs = append(trs[j].Obs, Obs{Frame: f.ID, Ticks: f.Ticks, Time: f.Time, Box: ds[i].Box, Conf: ds[i].Conf})
			matched[i] = true
		}
	}
	for i, d := range ds {
		if !matched[i] {
			t.active = append(t.active, &Track{Class: class, Obs: []Obs{{Frame: f.ID, Ticks: f.Ticks, Time: f.Time, Box: d.Box, Conf: d.Conf}}})
		}
	}
}

// predicted moves the track's last box along its recent velocity, for at most one
// second and at half speed: enough to catch a walker, too little to run away from a
// stopped one.
func (t *Tracker) predicted(f Frame, tr *Track) Box {
	last := tr.Last()
	if t.cfg.NoPredict || len(tr.Obs) < 2 {
		return last.Box
	}
	prev := tr.Obs[len(tr.Obs)-2]
	dt := last.Time - prev.Time
	if dt < 0.05 {
		return last.Box
	}
	lx, ly := last.Box.Center()
	px, py := prev.Box.Center()
	ahead := math.Min(f.Time-last.Time, 1.0) * 0.5
	b := last.Box
	b.X += (lx - px) / dt * ahead
	b.Y += (ly - py) / dt * ahead
	return b
}

// cost is negative similarity for pairs that may be linked, forbidden otherwise.
func (t *Tracker) cost(f Frame, d Box, tr *Track) float64 {
	last := tr.Last().Box
	pred := t.predicted(f, tr)
	iou := math.Max(IoU(d, last), IoU(d, pred))
	dist := math.Min(CenterDistance(d, last), CenterDistance(d, pred))
	reach := t.cfg.DistGate * (d.Diag() + last.Diag()) / 2
	sizeOK := false
	if a, b := d.Area(), last.Area(); a > 0 && b > 0 {
		r := a / b
		sizeOK = r <= sizeRatioMax && r >= 1/sizeRatioMax
	}
	near := sizeOK && dist <= reach
	if iou < t.cfg.MatchIoU && !near {
		return forbidden
	}
	closeness := 0.0
	if reach > 0 {
		closeness = math.Max(0, 1-dist/reach)
	}
	return -(iou + 0.5*closeness)
}

func (t *Tracker) retire(tr *Track) {
	if len(tr.Obs) >= t.cfg.MinHits {
		t.done = append(t.done, tr)
	}
}

func (t *Tracker) closeAll() {
	for _, tr := range t.active {
		t.retire(tr)
	}
	t.active = nil
}

// Finish ends every track and returns the confirmed ones in order of first appearance,
// with IDs and per-class sequence numbers assigned.
func (t *Tracker) Finish() []*Track {
	t.closeAll()
	out := t.done
	t.done = nil
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i].First(), out[j].First()
		if a.Time != b.Time {
			return a.Time < b.Time
		}
		if out[i].Class != out[j].Class {
			return out[i].Class < out[j].Class
		}
		return a.Box.X < b.Box.X
	})
	seq := map[string]int{}
	for i, tr := range out {
		tr.ID = i + 1
		seq[tr.Class]++
		tr.Seq = seq[tr.Class]
	}
	return out
}
