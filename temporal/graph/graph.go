// Package graph holds the in-memory semantic graph: entities (nodes) linked by events,
// actions and relations (edges), plus the text seen on screen and the shots of the
// video. The temporal engine builds it; ToSVIR turns it into the SVIR document that goes
// into the .virex file.
package graph

import (
	"fmt"
	"sort"

	"virex/temporal/tracker"
)

// Moment is one point in time, in both units the pipeline needs: container ticks (what
// SVIR stores) and seconds (what rules and thresholds use).
type Moment struct {
	Ticks int64
	Sec   float64
}

// NoFrame marks a block that is not tied to one frame.
const NoFrame = -1

// Entity is a node: one tracked object with a lifetime.
type Entity struct {
	Track *tracker.Track
}

// ID is the SVIR entity id.
func (e *Entity) ID() int { return e.Track.ID }

// Name is the readable id, such as person_01.
func (e *Entity) Name() string { return e.Track.Name() }

// First and Last are when the entity was first and last seen.
func (e *Entity) First() Moment { o := e.Track.First(); return Moment{o.Ticks, o.Time} }
func (e *Entity) Last() Moment  { o := e.Track.Last(); return Moment{o.Ticks, o.Time} }

// Event is something that happened over an interval (Start == End for an instant).
type Event struct {
	Type     string
	Start    Moment
	End      Moment
	Entities []int        // entity ids involved
	Region   *tracker.Box // where, if known
	Conf     *float64     // nil = unknown (rule fired deterministically)
	Frame    int          // frame the rule fired on, or NoFrame
}

// Action is an entity doing something over an interval, such as walking.
type Action struct {
	Subject int
	Name    string
	Start   Moment
	End     Moment
	Conf    float64
	Frame   int
	Box     *tracker.Box
}

// Relation is subject --Name--> object over an interval.
type Relation struct {
	ID      int
	Subject int
	Name    string
	Object  int
	Start   Moment
	End     Moment
	Conf    float64
	Frame   int
	Box     tracker.Box // union of the two boxes at the frame it started
}

// Shot is a stretch of video between two scene changes.
type Shot struct {
	Index int // 1-based
	Start Moment
	End   Moment
	Frame int // first sampled frame of the shot
}

// Graph is the whole semantic content of one video.
type Graph struct {
	Timescale int64  // ticks per second
	Duration  Moment // length of the video

	Entities  []*Entity
	Texts     []*tracker.TextTrack
	Events    []*Event
	Actions   []*Action
	Relations []*Relation
	Shots     []*Shot

	byID map[int]*Entity
}

// Index builds the lookup tables and puts every list in time order. Call it after the
// lists are filled and before querying.
func (g *Graph) Index() {
	g.byID = make(map[int]*Entity, len(g.Entities))
	for _, e := range g.Entities {
		g.byID[e.ID()] = e
	}
	sort.SliceStable(g.Events, func(i, j int) bool {
		a, b := g.Events[i], g.Events[j]
		if a.Start.Ticks != b.Start.Ticks {
			return a.Start.Ticks < b.Start.Ticks
		}
		return a.Type < b.Type
	})
	sort.SliceStable(g.Actions, func(i, j int) bool {
		a, b := g.Actions[i], g.Actions[j]
		if a.Start.Ticks != b.Start.Ticks {
			return a.Start.Ticks < b.Start.Ticks
		}
		return a.Subject < b.Subject
	})
	sort.SliceStable(g.Relations, func(i, j int) bool {
		a, b := g.Relations[i], g.Relations[j]
		if a.Start.Ticks != b.Start.Ticks {
			return a.Start.Ticks < b.Start.Ticks
		}
		return a.ID < b.ID
	})
}

// Entity returns the entity with this id, or nil.
func (g *Graph) Entity(id int) *Entity { return g.byID[id] }

// Validate checks that every edge points at an entity that exists and every interval is
// well formed. The temporal engine produces valid graphs; this guards future rules.
func (g *Graph) Validate() error {
	if g.byID == nil {
		g.Index()
	}
	need := func(what string, id int) error {
		if g.byID[id] == nil {
			return fmt.Errorf("graph: %s refers to unknown entity %d", what, id)
		}
		return nil
	}
	for i, e := range g.Events {
		if e.End.Ticks < e.Start.Ticks {
			return fmt.Errorf("graph: event #%d (%s) ends before it starts", i, e.Type)
		}
		for _, id := range e.Entities {
			if err := need("event "+e.Type, id); err != nil {
				return err
			}
		}
	}
	for i, a := range g.Actions {
		if a.End.Ticks < a.Start.Ticks {
			return fmt.Errorf("graph: action #%d (%s) ends before it starts", i, a.Name)
		}
		if err := need("action "+a.Name, a.Subject); err != nil {
			return err
		}
	}
	seen := map[int]bool{}
	for i, r := range g.Relations {
		if r.End.Ticks < r.Start.Ticks {
			return fmt.Errorf("graph: relation #%d (%s) ends before it starts", i, r.Name)
		}
		if r.ID == 0 || seen[r.ID] {
			return fmt.Errorf("graph: relation #%d has missing or repeated id %d", i, r.ID)
		}
		seen[r.ID] = true
		if err := firstErr(need("relation "+r.Name, r.Subject), need("relation "+r.Name, r.Object)); err != nil {
			return err
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

// ItemKind says what a timeline item is.
type ItemKind string

const (
	ItemAppears  ItemKind = "appears"
	ItemLeaves   ItemKind = "leaves"
	ItemEvent    ItemKind = "event"
	ItemAction   ItemKind = "action"
	ItemRelation ItemKind = "relation"
)

// Item is one entry in an entity's timeline.
type Item struct {
	Kind  ItemKind
	Start Moment
	End   Moment
	Label string // event type, action name, or "relation other_entity"
	With  int    // the other entity of a relation, else 0
}

// Timeline is everything that happened to one entity, in time order: when it appeared
// and left, the events it took part in, its actions and its relations.
func (g *Graph) Timeline(entityID int) []Item {
	e := g.Entity(entityID)
	if e == nil {
		return nil
	}
	items := []Item{
		{Kind: ItemAppears, Start: e.First(), End: e.First(), Label: e.Name()},
		{Kind: ItemLeaves, Start: e.Last(), End: e.Last(), Label: e.Name()},
	}
	for _, ev := range g.Events {
		for _, id := range ev.Entities {
			if id == entityID {
				items = append(items, Item{Kind: ItemEvent, Start: ev.Start, End: ev.End, Label: ev.Type})
				break
			}
		}
	}
	for _, a := range g.Actions {
		if a.Subject == entityID {
			items = append(items, Item{Kind: ItemAction, Start: a.Start, End: a.End, Label: a.Name})
		}
	}
	for _, r := range g.Relations {
		switch entityID {
		case r.Subject:
			items = append(items, Item{Kind: ItemRelation, Start: r.Start, End: r.End, Label: r.Name, With: r.Object})
		case r.Object:
			items = append(items, Item{Kind: ItemRelation, Start: r.Start, End: r.End, Label: r.Name + " (inverse)", With: r.Subject})
		}
	}
	rank := map[ItemKind]int{ItemAppears: 0, ItemEvent: 1, ItemAction: 2, ItemRelation: 3, ItemLeaves: 4}
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if a.Start.Ticks != b.Start.Ticks {
			return a.Start.Ticks < b.Start.Ticks
		}
		return rank[a.Kind] < rank[b.Kind]
	})
	return items
}

// ActiveAt returns the entities alive and the relations holding at time sec.
func (g *Graph) ActiveAt(sec float64) (entities []*Entity, relations []*Relation) {
	for _, e := range g.Entities {
		if e.First().Sec <= sec && sec <= e.Last().Sec {
			entities = append(entities, e)
		}
	}
	for _, r := range g.Relations {
		if r.Start.Sec <= sec && sec <= r.End.Sec {
			relations = append(relations, r)
		}
	}
	return
}
