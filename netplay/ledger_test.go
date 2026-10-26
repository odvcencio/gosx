package netplay

import (
	"fmt"
	"testing"
	"time"
)

type ledgerLog struct {
	starts  []Event
	late    []int64
	cancels []Event
	active  map[string]int // key -> live count
}

func evKey(e Event) string { return fmt.Sprintf("%d/%d/%d/%x", e.Frame, e.Kind, e.Source, e.Data) }

func newLedgerLog() *ledgerLog { return &ledgerLog{active: map[string]int{}} }

func (l *ledgerLog) Start(e Event, late int64) {
	l.starts = append(l.starts, e)
	l.late = append(l.late, late)
	l.active[evKey(e)]++
}

func (l *ledgerLog) Cancel(e Event) {
	l.cancels = append(l.cancels, e)
	l.active[evKey(e)]--
}

func ev(frame int64, kind uint16, src uint32, d byte) Event {
	e := Event{Frame: frame, Kind: kind, Source: src}
	e.Data[0] = d
	return e
}

func TestLedgerNewEventStartsLateByFrameOffset(t *testing.T) {
	log := newLedgerLog()
	l := NewEventLedger(log, 0)
	l.Reconcile(10, 14, []Event{ev(10, 1, 1, 0), ev(13, 2, 1, 0)})
	if len(log.starts) != 2 || log.late[0] != 3 || log.late[1] != 0 {
		t.Fatalf("starts %v late %v, want offsets 3 and 0", log.starts, log.late)
	}
}

func TestLedgerVanishedEventIsCancelledAndUnchangedLeftAlone(t *testing.T) {
	log := newLedgerLog()
	l := NewEventLedger(log, 0)
	a, b := ev(10, 1, 1, 0), ev(11, 1, 2, 0)
	l.Reconcile(10, 12, []Event{a, b})
	log.starts, log.cancels = nil, nil
	// A rollback re-simulates 10..12; the hit at 11 no longer happens.
	l.Reconcile(10, 13, []Event{a})
	if len(log.starts) != 0 {
		t.Fatalf("unchanged event restarted: %v", log.starts)
	}
	if len(log.cancels) != 1 || log.cancels[0] != b {
		t.Fatalf("cancels = %v, want only %v", log.cancels, b)
	}
}

func TestLedgerIgnoresEventsOutsideReconciledRange(t *testing.T) {
	log := newLedgerLog()
	l := NewEventLedger(log, 0)
	l.Reconcile(0, 10, []Event{ev(2, 1, 1, 0)})
	l.Reconcile(8, 12, nil) // does not cover frame 2
	if len(log.cancels) != 0 {
		t.Fatalf("cancelled an event outside the range: %v", log.cancels)
	}
}

func TestLedgerSameFrameTradeKeepsBothEvents(t *testing.T) {
	log := newLedgerLog()
	l := NewEventLedger(log, 0)
	p0, p1 := ev(20, 1, 1, 10), ev(20, 1, 2, 10)
	l.Reconcile(20, 21, []Event{p0, p1})
	log.starts = nil
	l.Reconcile(18, 22, []Event{p0, p1})
	if len(log.starts) != 0 || len(log.cancels) != 0 {
		t.Fatalf("trade disturbed: starts %v cancels %v", log.starts, log.cancels)
	}
	// Two events with one key in one frame are told apart by order.
	d1, d2 := ev(30, 1, 1, 1), ev(30, 1, 1, 2)
	l.Reconcile(30, 31, []Event{d1, d2})
	if n := log.active[evKey(d1)] + log.active[evKey(d2)]; n != 2 {
		t.Fatalf("duplicate-key events active = %d", n)
	}
}

func TestLedgerChangedPayloadCancelsAndRestarts(t *testing.T) {
	log := newLedgerLog()
	l := NewEventLedger(log, 0)
	l.Reconcile(5, 6, []Event{ev(5, 1, 1, 10)})
	l.Reconcile(5, 7, []Event{ev(5, 1, 1, 20)})
	if len(log.cancels) != 1 || len(log.starts) != 2 || log.starts[1].Data[0] != 20 {
		t.Fatalf("starts %v cancels %v", log.starts, log.cancels)
	}
	if log.active[evKey(ev(5, 1, 1, 10))] != 0 || log.active[evKey(ev(5, 1, 1, 20))] != 1 {
		t.Fatalf("active %v", log.active)
	}
}

func TestLedgerForgetsEventsPastRetention(t *testing.T) {
	l := NewEventLedger(newLedgerLog(), 10)
	l.Reconcile(0, 1, []Event{ev(0, 1, 1, 0)})
	l.Reconcile(50, 51, nil)
	if len(l.Tracked()) != 0 {
		t.Fatalf("tracked %v", l.Tracked())
	}
}

// TestLedgerFollowsRealRollbacks drives a ledger from a session that rolls
// back constantly and checks that, once frames are confirmed, the effects
// the ledger left running are exactly the events of the true simulation.
func TestLedgerFollowsRealRollbacks(t *testing.T) {
	net := NewLossyNet(NetConfig{Seed: 21, Delay: [2]time.Duration{5 * frameDur, 5 * frameDur}, Jitter: 15 * time.Millisecond})
	script := randomInputs(21, 3000)
	var ss [2]*Session[pad]
	logs := [2]*ledgerLog{newLedgerLog(), newLedgerLog()}
	for i := 0; i < 2; i++ {
		s, err := NewSession(Config[pad]{Game: newFightGame(), Codec: padCodec{}, Transport: net.Endpoint(i), LocalPlayer: i})
		if err != nil {
			t.Fatal(err)
		}
		ss[i] = s
	}
	ledgers := [2]*EventLedger{NewEventLedger(logs[0], 0), NewEventLedger(logs[1], 0)}
	var buf []Event
	var now time.Duration
	for k := 0; k < 1500; k++ {
		for i := 0; i < 2; i++ {
			net.SetNow(now)
			s := ss[i]
			s.Advance(now, script[s.Frame()][i])
			if s.ChangedFrom() >= 0 { // a rollback can happen on a stalled tick too
				buf = s.EventsFrom(s.ChangedFrom(), buf[:0])
				ledgers[i].Reconcile(s.ChangedFrom(), s.Frame(), buf)
			}
		}
		now += frameDur
	}
	// Truth: replay the confirmed inputs on a plain game, with its events.
	// The sessions' confirmed inputs are the script, delayed by D=0 here.
	truth := map[string]int{}
	ref := newFightGame()
	in := make([]pad, 2)
	var evs []Event
	limit := ss[0].ConfirmedFrame() - 12
	for f := int64(0); f < limit; f++ {
		in[0], in[1] = script[f][0], script[f][1]
		ref.Step(in)
		evs = ref.Events(evs[:0])
		for _, e := range evs {
			e.Frame = f
			truth[evKey(e)]++
		}
	}
	if len(truth) < 10 {
		t.Fatalf("test game produced only %d distinct events", len(truth))
	}
	for i := 0; i < 2; i++ {
		got := map[string]int{}
		for k, v := range logs[i].active {
			var f int64
			fmt.Sscanf(k, "%d/", &f)
			if f < limit && v != 0 {
				got[k] = v
			}
		}
		for k, v := range truth {
			if got[k] != v {
				t.Errorf("peer %d: event %s live %d times, want %d", i, k, got[k], v)
			}
		}
		for k, v := range got {
			if truth[k] != v {
				t.Errorf("peer %d: ghost event %s live %d times", i, k, v)
			}
		}
		if len(logs[i].cancels) == 0 {
			t.Errorf("peer %d: no cancels; rollbacks never changed an event", i)
		}
	}
}
