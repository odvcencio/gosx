package physics

import "slices"

// Trigger events.
//
// A trigger collider (ColliderConfig.IsTrigger) never pushes anything. Each
// step the world lists the trigger pairs that touch and compares the list with
// the previous step's:
//
//	TriggerEnter  the pair touches now and did not last step
//	TriggerStay   the pair touched last step and still does
//	TriggerExit   the pair touched last step and does not now
//
// Events come out sorted by trigger collider index, then by other collider
// index, so two runs of the same simulation see the same events in the same
// order. A trigger pairs with dynamic bodies, with kinematic bodies (a
// character, a moving platform) and, when the trigger itself rides a kinematic
// body, with static geometry. Layers filter trigger pairs like any other pair.
//
// Removing a collider ends its overlaps without an exit event: the code that
// removed it already knows.

// TriggerPhase says which edge of an overlap a TriggerEvent reports.
type TriggerPhase uint8

const (
	// TriggerEnter: the pair started touching this step.
	TriggerEnter TriggerPhase = iota + 1
	// TriggerStay: the pair touched last step and still does.
	TriggerStay
	// TriggerExit: the pair stopped touching this step.
	TriggerExit
)

// String names the phase.
func (p TriggerPhase) String() string {
	switch p {
	case TriggerEnter:
		return "enter"
	case TriggerStay:
		return "stay"
	case TriggerExit:
		return "exit"
	default:
		return "unknown"
	}
}

// TriggerEvent reports one trigger pair edge.
type TriggerEvent struct {
	Phase TriggerPhase
	// Trigger is the trigger collider. When both colliders are triggers it is
	// the one with the lower collider index.
	Trigger *Collider
	// Other is the collider that touches it.
	Other *Collider
	// Step is the world's fixed step count when the event happened.
	Step uint64
}

// Involves reports whether c is either side of the event and returns the
// other side.
func (e TriggerEvent) Involves(c *Collider) (*Collider, bool) {
	switch c {
	case e.Trigger:
		return e.Other, true
	case e.Other:
		return e.Trigger, true
	}
	return nil, false
}

// TriggerPairState is one touching trigger pair in a WorldState, by collider
// index.
type TriggerPairState struct {
	Trigger int `json:"trigger"`
	Other   int `json:"other"`
}

type triggerPair struct {
	trigger *Collider
	other   *Collider
	// The indexes are copied so the sort key stays valid even after a
	// removal resets a collider's index.
	triggerIndex int
	otherIndex   int
}

func compareTriggerPairs(a, b triggerPair) int {
	if a.triggerIndex != b.triggerIndex {
		return a.triggerIndex - b.triggerIndex
	}
	return a.otherIndex - b.otherIndex
}

func makeTriggerPair(a, b *Collider) triggerPair {
	trigger, other := a, b
	switch {
	case a.IsTrigger && b.IsTrigger:
		if b.index < a.index {
			trigger, other = b, a
		}
	case b.IsTrigger:
		trigger, other = b, a
	}
	return triggerPair{trigger: trigger, other: other, triggerIndex: trigger.index, otherIndex: other.index}
}

// updateTriggers turns this step's trigger manifolds into the new overlap set
// and the step's events, then hands each event to the trigger handler.
func (w *World) updateTriggers() {
	w.triggerPrev, w.triggerPairs = w.triggerPairs, w.triggerPrev[:0]
	for i := range w.contacts {
		m := &w.contacts[i]
		if !m.IsTrigger() || m.PointCount == 0 || m.ColliderA == nil || m.ColliderB == nil {
			continue
		}
		w.triggerPairs = append(w.triggerPairs, makeTriggerPair(m.ColliderA, m.ColliderB))
	}
	slices.SortFunc(w.triggerPairs, compareTriggerPairs)
	// A mesh trigger can touch through several triangles: one pair each.
	w.triggerPairs = slices.CompactFunc(w.triggerPairs, func(a, b triggerPair) bool {
		return a.triggerIndex == b.triggerIndex && a.otherIndex == b.otherIndex
	})

	clear(w.triggerEvents)
	w.triggerEvents = w.triggerEvents[:0]
	prev, cur := w.triggerPrev, w.triggerPairs
	i, j := 0, 0
	for i < len(prev) || j < len(cur) {
		switch {
		case j >= len(cur) || (i < len(prev) && compareTriggerPairs(prev[i], cur[j]) < 0):
			w.appendTriggerEvent(TriggerExit, prev[i])
			i++
		case i >= len(prev) || compareTriggerPairs(prev[i], cur[j]) > 0:
			w.appendTriggerEvent(TriggerEnter, cur[j])
			j++
		default:
			w.appendTriggerEvent(TriggerStay, cur[j])
			i++
			j++
		}
	}
	if w.triggerHandler == nil {
		return
	}
	for _, event := range w.triggerEvents {
		w.triggerHandler(event)
	}
}

func (w *World) appendTriggerEvent(phase TriggerPhase, pair triggerPair) {
	w.triggerEvents = append(w.triggerEvents, TriggerEvent{
		Phase:   phase,
		Trigger: pair.trigger,
		Other:   pair.other,
		Step:    w.steps,
	})
}

// TriggerEvents returns the trigger events of the most recent step, in order.
// The slice belongs to the world and is overwritten by the next step; copy
// what you keep.
func (w *World) TriggerEvents() []TriggerEvent {
	if w == nil {
		return nil
	}
	return w.triggerEvents
}

// SetTriggerHandler installs fn to receive every trigger event at the end of
// the step that produced it, in order. Pass nil to remove it. The handler runs
// after the step finished moving bodies, so it may add or remove bodies and
// colliders; the changes apply from the next step.
func (w *World) SetTriggerHandler(fn func(TriggerEvent)) {
	if w != nil {
		w.triggerHandler = fn
	}
}

// TriggerOverlaps appends every trigger pair that touched in the most recent
// step, sorted like the events, and returns the result.
func (w *World) TriggerOverlaps(out []TriggerPairState) []TriggerPairState {
	if w == nil {
		return out
	}
	for _, pair := range w.triggerPairs {
		out = append(out, TriggerPairState{Trigger: pair.triggerIndex, Other: pair.otherIndex})
	}
	return out
}

// restoreTriggers replaces the overlap set from a snapshot. Pairs naming a
// collider the world does not hold are dropped.
func (w *World) restoreTriggers(pairs []TriggerPairState, byIndex map[int]*Collider) {
	clear(w.triggerPairs)
	w.triggerPairs = w.triggerPairs[:0]
	for _, pair := range pairs {
		trigger := byIndex[pair.Trigger]
		other := byIndex[pair.Other]
		if trigger == nil || other == nil {
			continue
		}
		w.triggerPairs = append(w.triggerPairs, makeTriggerPair(trigger, other))
	}
	slices.SortFunc(w.triggerPairs, compareTriggerPairs)
	clear(w.triggerEvents)
	w.triggerEvents = w.triggerEvents[:0]
}

// pruneTriggers drops overlaps that name a removed collider.
func (w *World) pruneTriggers(removed map[*Collider]struct{}) {
	kept := w.triggerPairs[:0]
	for _, pair := range w.triggerPairs {
		_, dropTrigger := removed[pair.trigger]
		_, dropOther := removed[pair.other]
		if !dropTrigger && !dropOther {
			kept = append(kept, pair)
		}
	}
	clear(w.triggerPairs[len(kept):])
	w.triggerPairs = kept
}
