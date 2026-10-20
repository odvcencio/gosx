package telemetry

import (
	"math"

	"m31labs.dev/gosx/internal/telemetryfields"
	"m31labs.dev/gosx/internal/telemetryrecord"
	"m31labs.dev/gosx/telemetry/schema"
)

func activityFieldBytes(v schema.Activity, extra schema.Fields) (int, error) {
	total := 0
	views := append([]schema.Fields{v.Fields, extra}, participantFieldViews(v.Participants)...)
	for _, fields := range views {
		encoded, err := telemetryfields.AppendFields(nil, fields)
		if err != nil {
			return 0, err
		}
		total += len(encoded)
	}
	return total, nil
}
func (e *activityEntity) eventDropped(reason string) {
	e.mu.Lock()
	if !e.final && !e.kind.owner.activities.stopping.Load() {
		e.eventsDropped = incrementParticipant(e.eventsDropped)
		e.eventVersion++
		e.dirty = true
		if reason != "" {
			e.kind.owner.core.dropped[reason].Add(1)
		}
	}
	e.mu.Unlock()
}
func (a *Activity[A, P, E]) Event(name string, fields E) error {
	if a == nil || a.entity == nil {
		return nil
	}
	e := a.entity
	t := e.kind.owner
	s := t.activities
	e.mu.Lock()
	if e.final || s.stopping.Load() {
		e.mu.Unlock()
		return ErrClosed
	}
	if e.eventsAccepted >= uint64(t.opts.Activities.MaxEvents) {
		e.eventsDropped = incrementParticipant(e.eventsDropped)
		e.eventVersion++
		e.dirty = true
		t.core.dropped["event_cap"].Add(1)
		e.mu.Unlock()
		return ErrCapacity
	}
	original, version, accepted := e.record, e.eventVersion, e.eventsAccepted
	e.mu.Unlock()
	release, err := s.transaction()
	if err != nil {
		e.eventDropped("")
		return err
	}
	defer release()
	staged, err := encodeActivityFields(e.kind, a.kind.event, fields)
	if err != nil {
		e.eventDropped("")
		return err
	}
	v, _ := original.Activity()
	n, err := activityFieldBytes(v, staged)
	if err != nil || n > t.opts.Activities.MaxFieldBytes {
		e.eventDropped("field_budget")
		if err != nil {
			return err
		}
		return ErrFieldBudget
	}
	now, err := t.activityNow()
	if err != nil {
		e.eventDropped("")
		return err
	}
	s.eventPublish.Lock()
	defer s.eventPublish.Unlock()
	s.mu.Lock()
	if s.sequence == math.MaxUint64 {
		s.mu.Unlock()
		e.eventDropped("")
		return ErrCapacity
	}
	s.sequence++
	seq := s.sequence
	s.mu.Unlock()
	projected := schema.ActivityEvent{ActivityID: e.recordID, Seq: accepted + 1, Name: activityValue(name, e.kind.events), ObservedAt: now.Wall, Codec: schema.Codec{Name: a.kind.event.name, Version: a.kind.event.version}, Fields: staged}
	record, err := telemetryrecord.NewActivityEvent(schema.Envelope{Schema: schema.SchemaVersion, Stream: s.stream, Boot: s.boot, Seq: seq, Type: schema.TypeActivityEvent, State: schema.StateEvent, At: now.Wall}, projected)
	if err != nil {
		e.eventDropped("")
		return err
	}
	if telemetryrecord.EncodedLen(record) > t.opts.Activities.MaxRecordBytes {
		e.eventDropped("field_budget")
		return ErrFieldBudget
	}
	e.mu.Lock()
	if e.final || s.stopping.Load() {
		e.mu.Unlock()
		return ErrClosed
	}
	if e.record.Envelope().Revision != original.Envelope().Revision || e.eventVersion != version {
		e.eventsDropped = incrementParticipant(e.eventsDropped)
		e.eventVersion++
		e.dirty = true
		e.mu.Unlock()
		return ErrConflict
	}
	if err = s.events.enqueue(record); err != nil {
		e.eventsDropped = incrementParticipant(e.eventsDropped)
		e.eventVersion++
		e.dirty = true
		t.core.dropped["queue"].Add(1)
		e.mu.Unlock()
		return err
	}
	e.eventsAccepted++
	e.eventVersion++
	e.dirty = true
	e.lastTouch = now.Monotonic
	e.mu.Unlock()
	t.wakeActivityWorker()
	return nil
}
