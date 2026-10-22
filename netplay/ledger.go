package netplay

// LedgerHandler receives the presentation changes the EventLedger computes.
type LedgerHandler interface {
	// Start begins the effect for e. late is how many frames before the
	// newest simulated frame the event happened: start the effect that far
	// into its animation, sound or particle life.
	Start(e Event, late int64)
	// Cancel undoes an effect that a rollback showed never happened:
	// stop its sound, remove its particles, undo its camera shake.
	Cancel(e Event)
}

// EventLedger reconciles presentation effects after rollbacks. Events are
// keyed by (Frame, Kind, Source); two events with one key in a frame are
// told apart by emission order. After each re-simulation, call Reconcile with
// the complete event list for the re-simulated frames:
//
//   - an event the ledger has not seen starts, late by its frame offset;
//   - a tracked event that is no longer emitted is cancelled;
//   - an unchanged event is left alone (no callback).
//
// An event whose key survives but whose Data changed is cancelled and
// restarted, because its effect was built from the old payload.
type EventLedger struct {
	h       LedgerHandler
	live    []Event
	matched []bool
	keep    []bool
	pairs   []int
	idx     []int
	retain  int64
}

// NewEventLedger returns a ledger that reports to h. Tracked events older
// than retain frames behind the newest frame are forgotten (default 64 when
// retain is 0); they can no longer be rolled back.
func NewEventLedger(h LedgerHandler, retain int64) *EventLedger {
	if retain == 0 {
		retain = 64
	}
	return &EventLedger{h: h, retain: retain}
}

// Tracked returns the events the ledger currently believes happened. The
// slice is only valid until the next Reconcile.
func (l *EventLedger) Tracked() []Event { return l.live }

// Reconcile compares events, the complete list for frames [from, to), with
// what the ledger tracked in that range. to-1 is the newest simulated frame.
// Callbacks run in this order: all Cancels, then all Starts.
func (l *EventLedger) Reconcile(from, to int64, events []Event) {
	inRange := func(f int64) bool { return f >= from && f < to }
	l.matched = grow(l.matched, len(l.live))
	l.pairs = growInt(l.pairs, len(events))
	for i := range l.matched {
		l.matched[i] = false
	}
	// 1. Pair each emitted event with the first unpaired tracked event of
	// the same key.
	for k := range events {
		e := &events[k]
		l.pairs[k] = -1
		if !inRange(e.Frame) {
			continue
		}
		for i := range l.live {
			t := &l.live[i]
			if !l.matched[i] && t.Frame == e.Frame && t.Kind == e.Kind && t.Source == e.Source {
				l.matched[i], l.pairs[k] = true, i
				break
			}
		}
	}
	// 2. Cancel tracked events in range that nothing paired with, and the
	// old version of any event whose payload changed.
	for i := range l.live {
		if inRange(l.live[i].Frame) && !l.matched[i] {
			l.h.Cancel(l.live[i])
		}
	}
	for k := range events {
		if i := l.pairs[k]; i >= 0 && l.live[i].Data != events[k].Data {
			l.h.Cancel(l.live[i])
		}
	}
	// 3. Drop the vanished from the tracked set, then start what is new or
	// changed. Unchanged events produce no callback.
	n := 0
	keep := l.keep[:0]
	for i := range l.live {
		keep = append(keep, !inRange(l.live[i].Frame) || l.matched[i])
	}
	l.keep = keep
	// survivors keep their order; pairs must follow the compaction
	idx := growInt(l.idx, len(l.live))
	l.idx = idx
	for i := range l.live {
		if l.keep[i] {
			idx[i] = n
			l.live[n] = l.live[i]
			n++
		} else {
			idx[i] = -1
		}
	}
	l.live = l.live[:n]
	for k := range events {
		e := &events[k]
		if !inRange(e.Frame) {
			continue
		}
		if i := l.pairs[k]; i >= 0 {
			j := idx[i]
			if l.live[j].Data != e.Data {
				l.live[j].Data = e.Data
				l.h.Start(*e, to-1-e.Frame)
			}
			continue
		}
		l.live = append(l.live, *e)
		l.h.Start(*e, to-1-e.Frame)
	}
	// 4. Forget events too old to roll back.
	n = 0
	for _, t := range l.live {
		if t.Frame >= to-l.retain {
			l.live[n] = t
			n++
		}
	}
	l.live = l.live[:n]
}

func grow(b []bool, n int) []bool {
	if cap(b) < n {
		return make([]bool, n, 2*n+8)
	}
	return b[:n]
}

func growInt(b []int, n int) []int {
	if cap(b) < n {
		return make([]int, n, 2*n+8)
	}
	return b[:n]
}
