package telemetry

import (
	"context"
	"errors"

	"m31labs.dev/gosx/telemetry/schema"
)

func freezeActivity(e *activityEntity, now Instant, reason string) error {
	// Called by the sole maintenance/close owner. Encoding is framework-owned;
	// no application codec is invoked while closing an abandoned activity.
	e.mu.Lock()
	original := e.record
	version := e.eventVersion
	final := e.final
	e.mu.Unlock()
	if final {
		return nil
	}
	v, _ := original.Activity()
	v.Outcome = "interrupted"
	v.Reason = reason
	activityElapsed(&v, e, now, true)
	r, err := e.kind.buildActivityRecord(v, schema.StateFinal, original.Envelope().Revision+1, now, e.loop != nil)
	if err != nil {
		return err
	}
	receipt := newMemoryReceipt()
	e.mu.Lock()
	if e.final {
		e.mu.Unlock()
		return nil
	}
	if e.record.Envelope().Revision != original.Envelope().Revision || e.eventVersion != version {
		e.mu.Unlock()
		return ErrConflict
	}
	if reason == "idle" && now.Monotonic-e.lastTouch < e.kind.owner.opts.Activities.IdleTimeout {
		e.mu.Unlock()
		return nil
	}
	e.record = r
	e.final = true
	e.dirty = false
	e.receipt = receipt
	e.kind.owner.activityFinishMetric(e, v)
	e.mu.Unlock()
	return nil
}
func (t *Telemetry) maintainActivities(now Instant) {
	s := t.activities
	if s == nil || s.stopping.Load() {
		return
	}
	s.mu.Lock()
	if now.Monotonic-s.lastMaintenance < t.opts.Activities.CheckpointInterval {
		s.mu.Unlock()
		return
	}
	s.lastMaintenance = now.Monotonic
	var items [256]*activityEntity
	n := 0
	for _, e := range s.live {
		items[n] = e
		n++
	}
	s.mu.Unlock()
	for _, e := range items[:n] {
		e.mu.Lock()
		idle := !e.final && now.Monotonic-e.lastTouch >= t.opts.Activities.IdleTimeout
		dirty := e.dirty
		e.mu.Unlock()
		if idle {
			_ = freezeActivity(e, now, "idle")
		} else if dirty {
			_, _ = checkpointActivity(e, now)
		}
	}
	t.collectActivityReceipts()
}
func (t *Telemetry) stopActivities(ctx context.Context) error {
	s := t.activities
	if s == nil {
		return nil
	}
	s.stopping.Store(true)
	now, err := t.activityNow()
	if err != nil {
		return err
	}
	var items [256]*activityEntity
	n := 0
	s.mu.Lock()
	for _, e := range s.live {
		items[n] = e
		n++
	}
	s.mu.Unlock()
	var result error
	for _, e := range items[:n] {
		if err := ctx.Err(); err != nil {
			result = errors.Join(result, err)
			break
		}
		if err := freezeActivity(e, now, "server_shutdown"); err != nil {
			result = errors.Join(result, err)
		}
	}
	t.drainActivityEvents()
	t.collectActivityReceipts()
	return result
}

// releaseUnfinishedActivities follows stopped admission and final collection.
// Dropping the remaining owner references needs neither a clock nor codecs.
// Caller-held projections remain caller-owned; no synthetic final is accepted.
func (t *Telemetry) releaseUnfinishedActivities() {
	s := t.activities
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, e := range s.live {
		delete(s.live, id)
		delete(s.attached, e.loop)
		s.bytes.Add(-activitySlotBytes)
		e.kind.open--
		e.kind.meters.open.Set(float64(e.kind.open))
	}
}
