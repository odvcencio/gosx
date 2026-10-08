package telemetry

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"math"
	"sync"
	"time"

	"m31labs.dev/gosx/internal/telemetryfields"
	"m31labs.dev/gosx/internal/telemetryrecord"
	"m31labs.dev/gosx/telemetry/schema"
)

type ActivityID string
type ActivityStart[A any] struct {
	Dimensions [2]string
	Fields     A
	ParentID   ActivityID
	Loop       *Loop
}
type ActivityEnd[A any] struct {
	Outcome, Reason string
	Fields          A
}
type Activity[A, P, E any] struct {
	kind   *ActivityKind[A, P, E]
	entity *activityEntity
}
type activityEntity struct {
	mu               sync.Mutex
	kind             *activityKindCore
	record           schema.Record
	recordID         string
	tuple            [2]string
	start, lastTouch time.Duration
	loop             *Loop
	dirty, final     bool
	receipt          Receipt
}

const activitySlotBytes = int64(32 << 10)
const activityMetadataBytes = int64(1024)

func encodeActivityFields[T any](kind *activityKindCore, codec *compiledDomainCodec[T], value T) (schema.Fields, error) {
	fields, err := codec.encodeFields(kind.owner.activities.pool, value)
	if err != nil {
		kind.meters.records[0][2].Add(1)
		if errors.Is(err, ErrFieldBudget) {
			kind.owner.core.dropped["field_budget"].Add(1)
		}
	}
	return fields, err
}

func (s *activityState) transaction() (func(), error) {
	for {
		used := s.transactions.Load()
		if used >= 8 {
			return nil, &domainEncodingError{code: "codec_busy", cause: ErrCapacity}
		}
		if s.transactions.CompareAndSwap(used, used+1) {
			return func() { s.transactions.Add(-1) }, nil
		}
	}
}
func (t *Telemetry) activityID() (id string, err error) {
	defer func() {
		if recover() != nil {
			id = ""
			err = invalid("entropy", "unavailable")
		}
	}()
	var value [16]byte
	reader := t.opts.Entropy
	if reader == nil {
		reader = rand.Reader
	}
	s := t.activities
	s.entropy.Lock()
	defer s.entropy.Unlock()
	if _, err = io.ReadFull(reader, value[:]); err != nil {
		return "", &setupError{config: &ConfigError{Field: "entropy", Code: "unavailable"}, cause: err}
	}
	return hex.EncodeToString(value[:]), nil
}
func (t *Telemetry) activityNow() (Instant, error) {
	now, err := readClock(t.opts.Clock)
	if err != nil {
		return now, err
	}
	if now.Wall.IsZero() {
		return now, invalid("clock", "wall")
	}
	return now, nil
}
func activityElapsed(v *schema.Activity, e *activityEntity, now Instant, final bool) {
	elapsed := now.Monotonic - e.start
	if elapsed < 0 {
		elapsed = 0
		v.ClockAdjusted = true
	}
	v.ElapsedMS = float64(elapsed) / float64(time.Millisecond)
	v.UpdatedAt = now.Wall
	wall := now.Wall.Sub(v.StartedAt)
	delta := wall - elapsed
	if delta > time.Second || delta < -time.Second {
		v.ClockAdjusted = true
	}
	if final {
		v.EndedAt = now.Wall
		if v.EndedAt.Before(v.StartedAt) {
			v.EndedAt = v.StartedAt
			v.ClockAdjusted = true
		}
	}
	if e.loop != nil {
		h := e.loop.Health()
		if h.Available {
			v.TickHealth = &h
		}
	}
}
func (k *activityKindCore) buildActivityRecord(v schema.Activity, state schema.RecordState, revision uint64, now Instant, reserveHealth bool) (schema.Record, error) {
	t := k.owner
	if err := k.validateFinalCapacity(v, reserveHealth); err != nil {
		return schema.Record{}, err
	}
	s := t.activities
	s.mu.Lock()
	if s.sequence == math.MaxUint64 {
		s.mu.Unlock()
		return schema.Record{}, ErrCapacity
	}
	s.sequence++
	seq := s.sequence
	s.mu.Unlock()
	envelope := schema.Envelope{Schema: schema.SchemaVersion, Stream: s.stream, Boot: s.boot, Seq: seq, Type: schema.TypeActivity, State: state, At: now.Wall, Revision: revision}
	record, err := telemetryrecord.NewActivity(envelope, v)
	if err != nil {
		return schema.Record{}, err
	}
	if telemetryrecord.EncodedLen(record) > t.opts.Activities.MaxRecordBytes || telemetryrecord.RetainedBytes(record)+activityMetadataBytes > activitySlotBytes {
		return schema.Record{}, ErrFieldBudget
	}
	return record, nil
}
func (k *ActivityKind[A, P, E]) begin(start ActivityStart[A]) (*Activity[A, P, E], error) {
	if k == nil || k.core == nil {
		return &Activity[A, P, E]{}, nil
	}
	t := k.core.owner
	s := t.activities
	if !t.Enabled() {
		return nil, ErrClosed
	}
	if s.stopping.Load() {
		return nil, ErrClosed
	}
	if start.Loop != nil {
		l := start.Loop
		if l.kind == nil || l.kind.owner != t {
			return nil, invalid("activity_loop", "foreign")
		}
		if l.slot == nil {
			if l.closed.Load() {
				return nil, ErrClosed
			}
		} else {
			l.slot.mu.Lock()
			open := l.slot.epoch == l.epoch && l.slot.kind == l.kind
			l.slot.mu.Unlock()
			if !open {
				return nil, ErrClosed
			}
		}
	}
	release, err := s.transaction()
	if err != nil {
		return nil, err
	}
	defer release()
	fields, err := encodeActivityFields(k.core, k.activity, start.Fields)
	if err != nil {
		return nil, err
	}
	now, err := t.activityNow()
	if err != nil {
		return nil, err
	}
	id, err := t.activityID()
	if err != nil {
		return nil, err
	}
	v := schema.Activity{ID: id, ParentID: string(start.ParentID), Kind: k.core.name, Dimensions: k.core.schemaDimensions(k.core.dimensionTuple(start.Dimensions)), StartedAt: now.Wall, UpdatedAt: now.Wall, Codec: schema.Codec{Name: k.activity.name, Version: k.activity.version}, Fields: fields}
	if t.opts.Identity.App != "" {
		v.Identity = &schema.Identity{App: t.opts.Identity.App, Version: t.opts.Identity.Version, Revision: t.opts.Identity.Revision}
	}
	entity := &activityEntity{kind: k.core, start: now.Monotonic, lastTouch: now.Monotonic, loop: start.Loop, dirty: true, tuple: k.core.dimensionTuple(start.Dimensions)}
	record, err := k.core.buildActivityRecord(v, schema.StateOpen, 1, now, start.Loop != nil)
	if err != nil {
		return nil, err
	}
	entity.record = record
	entity.recordID = id
	s.mu.Lock()
	defer s.mu.Unlock()
	if !t.Enabled() || s.stopping.Load() {
		return nil, ErrClosed
	}
	if len(s.live) >= t.opts.Activities.MaxOpen {
		t.core.dropped["activity_cap"].Add(1)
		return nil, ErrCapacity
	}
	if s.live[id] != nil {
		return nil, invalid("entropy", "collision")
	}
	if start.ParentID != "" && s.live[string(start.ParentID)] == nil {
		return nil, invalid("activity_parent", "unknown")
	}
	if start.Loop != nil {
		if start.Loop.kind == nil || start.Loop.kind.owner != t {
			return nil, invalid("activity_loop", "foreign")
		}
		if s.attached[start.Loop] != nil {
			return nil, ErrConflict
		}
		s.attached[start.Loop] = entity
	}
	s.live[id] = entity
	s.bytes.Add(activitySlotBytes)
	t.activityStartMetric(k.core, entity.tuple)
	return &Activity[A, P, E]{kind: k, entity: entity}, nil
}
func (a *Activity[A, P, E]) ID() ActivityID {
	if a == nil || a.entity == nil {
		return ""
	}
	return ActivityID(a.entity.recordID)
}
func (a *Activity[A, P, E]) Snapshot() (schema.Activity, error) {
	if a == nil || a.entity == nil {
		return schema.Activity{}, nil
	}
	e := a.entity
	e.mu.Lock()
	r := e.record
	final := e.final
	e.mu.Unlock()
	v, _ := r.Activity()
	if !final {
		now, err := e.kind.owner.activityNow()
		if err != nil {
			return schema.Activity{}, err
		}
		activityElapsed(&v, e, now, false)
	}
	return v, nil
}
func activityProjection(e *activityEntity) (schema.Record, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.final || e.kind.owner.activities.stopping.Load() {
		return schema.Record{}, ErrClosed
	}
	return e.record, nil
}
func (a *Activity[A, P, E]) Set(fields A) error {
	if a == nil || a.entity == nil {
		return nil
	}
	e := a.entity
	t := e.kind.owner
	s := t.activities
	release, err := s.transaction()
	if err != nil {
		return err
	}
	defer release()
	original, err := activityProjection(e)
	if err != nil {
		return err
	}
	staged, err := encodeActivityFields(e.kind, a.kind.activity, fields)
	if err != nil {
		return err
	}
	v, _ := original.Activity()
	v.Fields = staged
	if original.Envelope().Revision == math.MaxUint64 {
		return ErrCapacity
	}
	now, err := t.activityNow()
	if err != nil {
		return err
	}
	activityElapsed(&v, e, now, false)
	r, err := e.kind.buildActivityRecord(v, schema.StateCheckpoint, original.Envelope().Revision+1, now, e.loop != nil)
	if err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.final || s.stopping.Load() {
		return ErrClosed
	}
	if e.record.Envelope().Revision != original.Envelope().Revision {
		return ErrConflict
	}
	e.record = r
	e.dirty = true
	e.lastTouch = now.Monotonic
	return nil
}
func (a *Activity[A, P, E]) Touch() {
	if a == nil || a.entity == nil {
		return
	}
	e := a.entity
	now, err := e.kind.owner.activityNow()
	if err != nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.final && !e.kind.owner.activities.stopping.Load() && now.Monotonic > e.lastTouch {
		e.lastTouch = now.Monotonic
	}
}
func sameActivityFields(a, b schema.Fields) bool {
	x, err := telemetryfields.AppendFields(nil, a)
	if err != nil {
		return false
	}
	y, err := telemetryfields.AppendFields(nil, b)
	return err == nil && bytes.Equal(x, y)
}
func activityValue(value string, declared []string, framework ...string) string {
	for _, v := range declared {
		if v == value {
			return v
		}
	}
	for _, v := range framework {
		if v == value {
			return v
		}
	}
	return "other"
}
func (a *Activity[A, P, E]) End(end ActivityEnd[A]) (Receipt, error) {
	if a == nil || a.entity == nil {
		return Receipt{}, nil
	}
	e := a.entity
	t := e.kind.owner
	s := t.activities
	release, err := s.transaction()
	if err != nil {
		return Receipt{}, err
	}
	defer release()
	staged, err := encodeActivityFields(e.kind, a.kind.activity, end.Fields)
	if err != nil {
		return Receipt{}, err
	}
	outcome := activityValue(end.Outcome, e.kind.outcomes)
	reason := activityValue(end.Reason, e.kind.reasons)
	e.mu.Lock()
	original := e.record
	terminal := e.final
	e.mu.Unlock()
	v, _ := original.Activity()
	if terminal {
		if v.Outcome != outcome || v.Reason != reason || !sameActivityFields(v.Fields, staged) {
			return Receipt{}, ErrConflict
		}
		e.mu.Lock()
		receipt := e.receipt
		e.mu.Unlock()
		return receipt, nil
	}
	if s.stopping.Load() {
		return Receipt{}, ErrClosed
	}
	v.Fields = staged
	v.Outcome = outcome
	v.Reason = reason
	if original.Envelope().Revision == math.MaxUint64 {
		return Receipt{}, ErrCapacity
	}
	now, err := t.activityNow()
	if err != nil {
		return Receipt{}, err
	}
	activityElapsed(&v, e, now, true)
	r, err := e.kind.buildActivityRecord(v, schema.StateFinal, original.Envelope().Revision+1, now, e.loop != nil)
	if err != nil {
		return Receipt{}, err
	}
	receipt := newMemoryReceipt()
	e.mu.Lock()
	if e.final {
		frozen, existing := e.record, e.receipt
		e.mu.Unlock()
		v, _ := frozen.Activity()
		if v.Outcome == outcome && v.Reason == reason && sameActivityFields(v.Fields, staged) {
			return existing, nil
		}
		return Receipt{}, ErrConflict
	}
	if e.record.Envelope().Revision != original.Envelope().Revision {
		e.mu.Unlock()
		return Receipt{}, ErrConflict
	}
	if s.stopping.Load() {
		e.mu.Unlock()
		return Receipt{}, ErrClosed
	}
	e.record = r
	e.final = true
	e.dirty = false
	e.receipt = receipt
	e.mu.Unlock()
	t.activityFinishMetric(e, v)
	t.wakeActivityWorker()
	return receipt, nil
}
func (a *Activity[A, P, E]) Checkpoint() (Receipt, error) {
	if a == nil || a.entity == nil {
		return Receipt{}, nil
	}
	now, err := a.entity.kind.owner.activityNow()
	if err != nil {
		return Receipt{}, err
	}
	return checkpointActivity(a.entity, now)
}
func checkpointActivity(e *activityEntity, now Instant) (Receipt, error) {
	s := e.kind.owner.activities
	release, err := s.transaction()
	if err != nil {
		return Receipt{}, err
	}
	defer release()
	e.mu.Lock()
	if e.final || !e.dirty {
		receipt := e.receipt
		e.mu.Unlock()
		return receipt, nil
	}
	if s.stopping.Load() {
		e.mu.Unlock()
		return Receipt{}, ErrClosed
	}
	original := e.record
	e.mu.Unlock()
	v, _ := original.Activity()
	activityElapsed(&v, e, now, false)
	if original.Envelope().Revision == math.MaxUint64 {
		return Receipt{}, ErrCapacity
	}
	r, err := e.kind.buildActivityRecord(v, schema.StateCheckpoint, original.Envelope().Revision+1, now, e.loop != nil)
	if err != nil {
		return Receipt{}, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.final {
		return e.receipt, nil
	}
	if s.stopping.Load() {
		return Receipt{}, ErrClosed
	}
	if e.record.Envelope().Revision != original.Envelope().Revision {
		return Receipt{}, ErrConflict
	}
	receipt := newMemoryReceipt()
	receipt.complete(nil)
	e.record = r
	e.receipt = receipt
	e.dirty = false
	return receipt, nil
}
func (t *Telemetry) wakeActivityWorker() {
	select {
	case t.wake <- struct{}{}:
	default:
	}
}
func (t *Telemetry) collectActivityReceipts() {
	s := t.activities
	if s == nil {
		return
	}
	// State lock is released before entity locks; codec and loop callbacks are
	// never called with either owner lock held.
	s.mu.Lock()
	var items [256]*activityEntity
	n := 0
	for _, e := range s.live {
		items[n] = e
		n++
	}
	s.mu.Unlock()
	for _, e := range items[:n] {
		e.mu.Lock()
		final, receipt, id, loop := e.final, e.receipt, e.recordID, e.loop
		e.mu.Unlock()
		if final {
			s.mu.Lock()
			committed := false
			if s.live[id] == e {
				committed = true
				delete(s.live, id)
				delete(s.attached, loop)
				s.bytes.Add(-activitySlotBytes)
			}
			s.mu.Unlock()
			if committed {
				e.kind.meters.records[1][1].Add(1)
			}
		}
		receipt.complete(nil)
	}
}

// Reserve framework-owned final growth synchronously. Application projection
// changes can fail, but subsequent health/count/clock fields must still fit.
func (k *activityKindCore) validateFinalCapacity(v schema.Activity, reserveHealth bool) error {
	t := k.owner
	total := 0
	for _, f := range append([]schema.Fields{v.Fields}, participantFieldViews(v.Participants)...) {
		b, err := telemetryfields.AppendFields(nil, f)
		if err != nil {
			return err
		}
		total += len(b)
	}
	if total > t.opts.Activities.MaxFieldBytes {
		return ErrFieldBudget
	}
	longest := func(current string, values []string) string {
		for _, x := range values {
			if len(x) > len(current) {
				current = x
			}
		}
		return current
	}
	v.Outcome = longest("interrupted", k.outcomes)
	v.Reason = longest("server_shutdown", k.reasons)
	v.UpdatedAt = time.Date(9999, 12, 31, 23, 59, 59, 999999999, time.UTC)
	v.EndedAt = v.UpdatedAt
	v.ElapsedMS = float64(time.Duration(math.MaxInt64)) / float64(time.Millisecond)
	v.ClockAdjusted = true
	v.EventsAccepted = math.MaxUint64
	v.EventsDropped = math.MaxUint64
	if reserveHealth {
		maxMS := float64(time.Duration(math.MaxInt64)) / float64(time.Millisecond)
		v.TickHealth = &schema.TickHealth{Available: true, Samples: math.MaxUint64, P50MS: maxMS, P99MS: maxMS, MaxMS: maxMS, Overruns: math.MaxUint64, OverflowSamples: math.MaxUint64, BudgetMS: maxMS}
	}
	v.Participants = append([]schema.Participant(nil), v.Participants...)
	for i := range v.Participants {
		p := &v.Participants[i]
		p.Joins = math.MaxUint64
		p.Leaves = math.MaxUint64
		p.Reconnects = math.MaxUint64
		p.SeatPresenceMS = v.ElapsedMS
		p.LinksTruncated = true
		if p.Human {
			p.Client = &schema.Client{Platform: "chromeos", Browser: "samsung", Device: "desktop"}
		}
		p.Reasons = nil
		for _, reason := range uniqueHubValues(append(append([]string(nil), k.reasons...), "process_restart", "server_shutdown", "idle", "other")) {
			p.Reasons = append(p.Reasons, schema.ReasonCount{Reason: reason, Count: math.MaxUint64})
		}
	}
	envelope := schema.Envelope{Schema: schema.SchemaVersion, Stream: "00000000000000000000000000000000", Boot: "00000000000000000000000000000000", Seq: math.MaxUint64, Type: schema.TypeActivity, State: schema.StateFinal, At: v.UpdatedAt, Revision: math.MaxUint64}
	r, err := telemetryrecord.NewActivity(envelope, v)
	if err != nil {
		return err
	}
	if telemetryrecord.EncodedLen(r) > t.opts.Activities.MaxRecordBytes || telemetryrecord.RetainedBytes(r)+activityMetadataBytes > activitySlotBytes {
		return ErrFieldBudget
	}
	return nil
}
func participantFieldViews(participants []schema.Participant) []schema.Fields {
	out := make([]schema.Fields, len(participants))
	for i, p := range participants {
		out[i] = p.Fields
	}
	return out
}
