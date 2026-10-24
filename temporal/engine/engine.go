// Package engine is the temporal semantic engine: it turns the per-frame detections and
// OCR lines of a video into entities with lifetimes, events, actions and relations, and
// links them in a semantic graph.
//
// Everything here is rule based and deterministic: the same frames in give the same
// graph out. The ML models only supply detections; meaning over time comes from the
// rules in this package, which are documented in docs/phase-2-rules.md.
package engine

import (
	"fmt"
	"math"
	"strings"

	"virex/temporal/graph"
	"virex/temporal/tracker"
)

// Frame is one analysed frame, as the engine wants it.
type Frame struct {
	ID         int
	Ticks      int64   // presentation time in container ticks
	Time       float64 // presentation time in seconds
	Reason     string  // the sampler's reason: "scene" marks a scene change
	Detections []tracker.Det
	Lines      []tracker.TextLine
}

// ReasonScene is the sampler reason that marks a scene change.
const ReasonScene = "scene"

// Config groups the knobs of every stage. The zero value gives the defaults, which are
// the values in DefaultConfig.
type Config struct {
	Tracker   tracker.Config     `json:"tracker"`
	Text      tracker.TextConfig `json:"text"`
	Events    EventConfig        `json:"events"`
	Motion    MotionConfig       `json:"motion"`
	Relations RelationConfig     `json:"relations"`
	SVIR      graph.SVIROptions  `json:"svir"`
}

// DefaultConfig returns the defaults, with SVIR options filled in. Zero fields of the
// other groups are filled in by the stage that uses them.
func DefaultConfig() Config {
	return Config{SVIR: graph.DefaultSVIROptions()}
}

// Engine consumes frames in time order and builds the graph at the end.
type Engine struct {
	cfg       Config
	timescale int64

	objects *tracker.Tracker
	texts   *tracker.TextTracker

	frames      int
	first       float64 // time of the first frame, seconds
	firstFrame  int
	firstMoment graph.Moment
	last        float64
	lastTick    int64
	cuts        []cut
}

type cut struct {
	frame int
	at    graph.Moment
}

// New builds an engine. timescale is the container's ticks per second.
func New(cfg Config, timescale int64) *Engine {
	return &Engine{cfg: cfg, timescale: timescale, objects: tracker.New(cfg.Tracker), texts: tracker.NewText(cfg.Text)}
}

// Add feeds the next frame. Frames must arrive in presentation order.
func (e *Engine) Add(f Frame) error {
	tf := tracker.Frame{ID: f.ID, Ticks: f.Ticks, Time: f.Time, Cut: f.Reason == ReasonScene}
	if err := e.objects.Update(tf, f.Detections); err != nil {
		return err
	}
	if err := e.texts.Update(tf, f.Lines); err != nil {
		return err
	}
	if e.frames == 0 {
		e.first, e.firstFrame = f.Time, f.ID
		e.firstMoment = graph.Moment{Ticks: f.Ticks, Sec: f.Time}
	}
	e.frames++
	e.last, e.lastTick = f.Time, f.Ticks
	if tf.Cut {
		e.cuts = append(e.cuts, cut{f.ID, graph.Moment{Ticks: f.Ticks, Sec: f.Time}})
	}
	return nil
}

// Frames is how many frames were added.
func (e *Engine) Frames() int { return e.frames }

// Finish ends all tracking and builds the semantic graph. durationTicks is the length of
// the video; it decides which sightings at the very end count as "still there" rather
// than "left".
func (e *Engine) Finish(durationTicks int64) (*graph.Graph, error) {
	if e.timescale <= 0 {
		return nil, fmt.Errorf("engine: timescale must be positive")
	}
	end := math.Max(float64(durationTicks)/float64(e.timescale), e.last)
	endTicks := max(durationTicks, e.lastTick)

	g := &graph.Graph{
		Timescale: e.timescale,
		Duration:  graph.Moment{Ticks: endTicks, Sec: end},
	}
	tracks := e.objects.Finish()
	g.Texts = e.texts.Finish()
	for _, t := range tracks {
		g.Entities = append(g.Entities, &graph.Entity{Track: t})
	}
	if e.frames == 0 {
		g.Index()
		return g, nil
	}

	stream := span{start: e.first, end: end}
	g.Events = append(g.Events, entityEvents(e.cfg.Events, tracks, stream)...)
	g.Events = append(g.Events, textEvents(e.cfg.Events, g.Texts)...)
	g.Events = append(g.Events, sceneEvents(e.cuts)...)
	g.Shots = shots(e.cuts, graph.Moment{Ticks: g.Duration.Ticks, Sec: end}, e.firstFrame, e.firstMoment)

	acts, evs := motion(e.cfg.Motion, tracks)
	g.Actions = acts
	g.Events = append(g.Events, evs...)
	g.Relations = relations(e.cfg.Relations, tracks)

	g.Index()
	assignRelationIDs(g)
	if err := g.Validate(); err != nil {
		return nil, err
	}
	return g, nil
}

// span is a time range in seconds.
type span struct{ start, end float64 }

// slug turns a class name into the form used in event types: "cell phone" -> "cell_phone".
func slug(class string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(class)), " ", "_")
}

func moment(o tracker.Obs) graph.Moment { return graph.Moment{Ticks: o.Ticks, Sec: o.Time} }

func inSet(set []string, s string) bool {
	for _, x := range set {
		if x == s {
			return true
		}
	}
	return false
}
