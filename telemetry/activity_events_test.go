//go:build !js || !wasm

package telemetry

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"m31labs.dev/gosx/internal/telemetryrecord"
	"m31labs.dev/gosx/telemetry/schema"
)

func TestRecordQueueKeepsConfiguredEntryCapacity(t *testing.T) {
	const entries = 4096
	metadata := recordQueueMetadataBytes(entries)
	q := newRecordQueue(entries, metadata+1)
	if q == nil || len(q.slots) != entries || q.used.Load() != metadata {
		t.Fatal("queue silently scaled its entry capacity")
	}
	if q := newRecordQueue(entries, metadata-1); q != nil {
		t.Fatal("queue allocated incompatible metadata")
	}
	tel := &Telemetry{opts: Defaults()}
	tel.opts.Limits.MaxQueuedBytes = int64(tel.opts.Activities.MaxRecordBytes) + 256
	err := tel.initializeActivities()
	var config *ConfigError
	if !errors.As(err, &config) || config.Field != "queue_bytes" || config.Code != "incompatible_reservations" {
		t.Fatalf("incompatible caps were accepted: %v", err)
	}
	if tel.activities != nil || tel.miscBytes.Load() != 0 {
		t.Fatal("incompatible caps allocated or published state")
	}
	tel.opts.Limits.MaxQueuedRecords = 1
	tel.opts.Limits.MaxQueuedBytes += recordQueueMetadataBytes(1)
	if err := tel.initializeActivities(); err != nil {
		t.Fatal("compatible lowered caps rejected", err)
	}
	if len(tel.activities.events.slots) != 1 {
		t.Fatal("lowered entry capacity changed")
	}
}

func eventFixture(t testing.TB) (*Activity[NoFields, NoFields, int], *Telemetry) {
	t.Helper()
	tel, _ := lifecycleOwner(t)
	codec := DomainCodec[int]{Name: "round", Version: 1, Fields: []FieldDefinition{{Name: "score", Type: FieldInt}}, Encode: func(f *FieldSet, v int) error { return f.Int("score", int64(v)) }}
	kind, err := newActivityKind(tel, "match", ActivityKindOptions{Events: []string{"round"}}, ActivityCodecs[NoFields, NoFields, int]{Event: codec})
	if err != nil {
		t.Fatal(err)
	}
	activity, err := kind.begin(ActivityStart[NoFields]{})
	if err != nil {
		t.Fatal(err)
	}
	return activity, tel
}
func TestActivityEventsAreCopiedBoundedAndCounted(t *testing.T) {
	a, tel := eventFixture(t)
	tel.opts.Activities.MaxEvents = 2
	if err := a.Event("round", 1); err != nil {
		t.Fatal(err)
	}
	if err := a.Event("private-event-name", 2); err != nil {
		t.Fatal(err)
	}
	if err := a.Event("round", 3); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	for i, name := range []string{"round", "other"} {
		record, ok := tel.activities.events.take()
		if !ok {
			t.Fatal("event missing")
		}
		event, ok := record.ActivityEvent()
		if !ok || event.Name != name || event.Seq != uint64(i+1) || event.Fields[0].Int != int64(i+1) {
			t.Fatal(event)
		}
		event.Fields[0].Int = 99
		copy, _ := record.ActivityEvent()
		if copy.Fields[0].Int == 99 {
			t.Fatal("event view retained mutable fields")
		}
		tel.activities.events.release(record)
	}
	v, _ := a.Snapshot()
	if v.EventsAccepted != 2 || v.EventsDropped != 1 {
		t.Fatal(v)
	}
	r, err := a.End(ActivityEnd[NoFields]{})
	if err != nil {
		t.Fatal(err)
	}
	tel.collectActivityReceipts()
	if err = r.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err = a.Event("round", 4); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	final, _ := a.Snapshot()
	if final.EventsAccepted != 2 || final.EventsDropped != 1 {
		t.Fatal(final)
	}
}
func TestActivityEventQueueFailureDoesNotClaimAcceptance(t *testing.T) {
	a, tel := eventFixture(t)
	tel.activities.events = newRecordQueue(1, 4<<20)
	if err := a.Event("round", 1); err != nil {
		t.Fatal(err)
	}
	if err := a.Event("round", 2); !errors.Is(err, ErrQueueFull) {
		t.Fatal(err)
	}
	tel.drainActivityEvents()
	if err := a.Event("round", 3); err != nil {
		t.Fatal(err)
	}
	v, _ := a.Snapshot()
	if v.EventsAccepted != 2 || v.EventsDropped != 1 {
		t.Fatal(v)
	}
	record, _ := tel.activities.events.take()
	event, _ := record.ActivityEvent()
	if event.Seq != 2 || event.Fields[0].Int != 3 {
		t.Fatal(event)
	}
	tel.activities.events.release(record)
}
func TestActivityEventAggregateFieldsAndEncoderRollback(t *testing.T) {
	a, tel := eventFixture(t)
	tel.opts.Activities.MaxFieldBytes = 3
	if err := a.Event("round", 1); !errors.Is(err, ErrFieldBudget) {
		t.Fatal(err)
	}
	v, _ := a.Snapshot()
	if v.EventsAccepted != 0 || v.EventsDropped != 1 {
		t.Fatal(v)
	}
	if _, ok := tel.activities.events.take(); ok {
		t.Fatal("invalid projection reached worker")
	}
	tel.opts.Activities.MaxFieldBytes = 4 << 10
	a.kind.event.encode = func(*FieldSet, int) error { panic("private-encoder-canary") }
	if err := a.Event("round", 2); err == nil {
		t.Fatal("panic admitted")
	}
	v, _ = a.Snapshot()
	if v.EventsAccepted != 0 || v.EventsDropped != 2 {
		t.Fatal(v)
	}
}
func queueFixtureRecord(t testing.TB) schema.Record {
	t.Helper()
	at := time.Unix(100, 0)
	record, err := telemetryrecord.NewActivityEvent(schema.Envelope{Stream: "00000000000000000000000000000001", Boot: "00000000000000000000000000000002", Seq: 1, State: schema.StateEvent, At: at}, schema.ActivityEvent{ActivityID: "00000000000000000000000000000003", Seq: 1, Name: "round", ObservedAt: at, Codec: schema.Codec{Name: "none", Version: 1}, Fields: schema.Fields{}})
	if err != nil {
		t.Fatal(err)
	}
	return record
}
func TestRecordQueueRetainsInFlightCountAndByteCharge(t *testing.T) {
	record := queueFixtureRecord(t)
	q := newRecordQueue(1, 4<<20)
	baseline := q.used.Load()
	if err := q.enqueue(record); err != nil {
		t.Fatal(err)
	}
	taken, ok := q.take()
	if !ok {
		t.Fatal("empty queue")
	}
	if q.used.Load() != baseline+queueRecordCharge(record) {
		t.Fatal("worker payload lost its charge")
	}
	if err := q.enqueue(record); !errors.Is(err, ErrQueueFull) {
		t.Fatal(err)
	}
	q.release(taken)
	if q.used.Load() != baseline {
		t.Fatal("worker did not release payload charge")
	}
	if err := q.enqueue(record); err != nil {
		t.Fatal(err)
	}
}
func TestRecordQueueAllocationAndByteBound(t *testing.T) {
	record := queueFixtureRecord(t)
	q := newRecordQueue(2, 4<<20)
	q.maxBytes = q.used.Load() + queueRecordCharge(record)
	if err := q.enqueue(record); err != nil {
		t.Fatal(err)
	}
	if err := q.enqueue(record); !errors.Is(err, ErrQueueFull) {
		t.Fatal("byte cap admitted", err)
	}
	r, _ := q.take()
	q.release(r)
	allocations := testing.AllocsPerRun(100, func() {
		if err := q.enqueue(record); err != nil {
			t.Fatal(err)
		}
		r, ok := q.take()
		if !ok {
			t.Fatal("empty")
		}
		q.release(r)
	})
	if allocations != 0 {
		t.Fatal("warm queue added allocations", allocations)
	}
	for _, slot := range q.slots {
		if telemetryrecord.Valid(slot) {
			t.Fatal("removed queue slot retained record")
		}
	}
}
func BenchmarkRecordQueue(b *testing.B) {
	r := queueFixtureRecord(b)
	q := newRecordQueue(4096, 4<<20)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if err := q.enqueue(r); err != nil {
			b.Fatal(err)
		}
		record, ok := q.take()
		if !ok {
			b.Fatal("empty")
		}
		q.release(record)
	}
}

func TestRecordQueueConcurrentAdmissionsStayWithinBothCaps(t *testing.T) {
	record := queueFixtureRecord(t)
	q := newRecordQueue(8, 4<<20)
	baseline := q.used.Load()
	q.maxBytes = baseline + 2*queueRecordCharge(record)
	var admitted, processed atomic.Uint64
	done, drained := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(drained)
		producersDone := false
		for {
			r, ok := q.take()
			if ok {
				processed.Add(1)
				q.release(r)
				continue
			}
			if producersDone {
				return
			}
			select {
			case <-done:
				// The earlier empty take can precede the final admission.
				// Recheck only after every producer has finished.
				producersDone = true
			default:
				runtime.Gosched()
			}
		}
	}()
	var writers sync.WaitGroup
	for i := 0; i < 16; i++ {
		writers.Add(1)
		go func() {
			defer writers.Done()
			for j := 0; j < 64; j++ {
				err := q.enqueue(record)
				if err == nil {
					admitted.Add(1)
				} else if !errors.Is(err, ErrQueueFull) {
					t.Errorf("enqueue: %v", err)
				}
				q.mu.Lock()
				if q.count+q.inFlight > len(q.slots) || q.used.Load() > q.maxBytes {
					t.Error("queue exceeded retained cap")
				}
				q.mu.Unlock()
			}
		}()
	}
	writers.Wait()
	close(done)
	<-drained
	if processed.Load() != admitted.Load() || q.used.Load() != baseline || q.count != 0 || q.inFlight != 0 {
		t.Fatal(admitted.Load(), processed.Load(), q.used.Load())
	}
}

func FuzzActivityTransitions(f *testing.F) {
	f.Add([]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9})
	f.Add([]byte{3, 3, 4, 4, 5, 5, 7, 7, 9, 9})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 64 {
			data = data[:64]
		}
		a, tel := eventFixture(t)
		tel.opts.Activities.MaxEvents = 4
		tel.opts.Activities.MaxParticipants = 2
		var seat *Participant[NoFields]
		var receipt Receipt
		var terminal *schema.Activity
		ref := participantRef(tel, 1, false)
		for _, op := range data {
			switch op % 10 {
			case 0:
				a.Touch()
			case 1:
				_ = a.Set(NoFields{})
			case 2:
				if seat == nil {
					seat, _ = a.Participant(ParticipantStart[NoFields]{Seat: 0})
				}
			case 3:
				if seat != nil {
					_ = seat.Joined(ref)
				}
			case 4:
				if seat != nil {
					_ = seat.Left(ref, "private-reason")
				}
			case 5:
				_ = a.Event("round", int(op))
			case 6:
				_, _ = a.Checkpoint()
			case 7:
				r, err := a.End(ActivityEnd[NoFields]{})
				if err == nil {
					if receipt.state != nil && r.state != receipt.state {
						t.Fatal("identical final changed operation identity")
					}
					receipt = r
				}
			case 8:
				tel.drainActivityEvents()
				tel.collectActivityReceipts()
			case 9:
				tel.opts.Clock.(interface{ Advance(time.Duration) error }).Advance(time.Millisecond)
			}
			v, err := a.Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			if v.EventsAccepted > 4 || len(v.Participants) > 3 || tel.activities.events.used.Load() > tel.opts.Limits.MaxQueuedBytes {
				t.Fatal("bounded transition invariant")
			}
			if terminal != nil && (v.EventsAccepted != terminal.EventsAccepted || v.EventsDropped != terminal.EventsDropped || v.ElapsedMS != terminal.ElapsedMS) {
				t.Fatal("terminal projection changed")
			}
			if receipt.state != nil && terminal == nil {
				copy := v
				terminal = &copy
			}
		}
		tel.drainActivityEvents()
		tel.collectActivityReceipts()
	})
}

func TestActivityEventsExistingWorkerConsumesMemoryQueue(t *testing.T) {
	a, tel := eventFixture(t)
	tel.done = make(chan struct{})
	ticker := &controlledTicker{ch: make(chan time.Time)}
	tel.ticker = ticker
	tel.ticks = ticker.ch
	go tel.run()
	t.Cleanup(func() {
		if err := tel.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	baseline := tel.activities.events.used.Load()
	if err := a.Event("round", 1); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		q := tel.activities.events
		q.mu.Lock()
		empty := q.count == 0 && q.inFlight == 0 && q.used.Load() == baseline
		q.mu.Unlock()
		if empty {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("existing worker retained memory event")
		}
		runtime.Gosched()
	}
	view, err := a.Snapshot()
	if err != nil || view.EventsAccepted != 1 {
		t.Fatal(view, err)
	}
}
