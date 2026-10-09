package telemetry

import (
	"math"
	"time"

	"m31labs.dev/gosx/telemetry/schema"
)

// SessionRef is an opaque framework-owned transport association. A zero value
// cannot identify a connection. Only consented refs from this owner carry
// persistent session/client information; arbitrary application IDs are absent.
type SessionRef struct {
	owner     *Telemetry
	token     [16]byte
	id, visit string
	client    schema.Client
	permitted bool
}

// ID returns only a permitted framework association. An unconsented or
// disabled session reference exposes no correlation identifier.
func (r SessionRef) ID() string {
	if !r.Valid() {
		return ""
	}
	return r.id
}
func (r SessionRef) Valid() bool {
	return r.owner != nil && r.token != [16]byte{} && r.permitted && r.owner.opts.Sessions.Enabled && len(r.id) == 32
}

type ParticipantStart[P any] struct {
	Seat   int
	Role   string
	Human  bool
	Fields P
}
type Participant[P any] struct {
	entity *activityEntity
	id     string
	codec  *compiledDomainCodec[P]
}
type participantPresence struct {
	ref            SessionRef
	active         bool
	entered, total time.Duration
}

func (a *Activity[A, P, E]) Participant(start ParticipantStart[P]) (*Participant[P], error) {
	if a == nil || a.entity == nil {
		return &Participant[P]{}, nil
	}
	e := a.entity
	t := e.kind.owner
	s := t.activities
	if start.Seat < -1 || start.Seat >= t.opts.Activities.MaxParticipants {
		return nil, invalid("participant_seat", "range")
	}
	role := start.Role
	if role == "" && len(e.kind.roles) == 0 {
		role = "none"
	}
	valid := role == "none" && len(e.kind.roles) == 0
	for _, value := range e.kind.roles {
		valid = valid || value == role
	}
	if !valid {
		return nil, invalid("participant_role", "undeclared")
	}
	release, err := s.transaction()
	if err != nil {
		return nil, err
	}
	defer release()
	original, err := activityProjection(e)
	if err != nil {
		return nil, err
	}
	fields, err := encodeActivityFields(e.kind, a.kind.participant, start.Fields)
	if err != nil {
		return nil, err
	}
	id, err := t.activityID()
	if err != nil {
		return nil, err
	}
	v, _ := original.Activity()
	for _, p := range v.Participants {
		if p.Seat == start.Seat || p.ID == id {
			return nil, ErrConflict
		}
	}
	now, err := t.activityNow()
	if err != nil {
		return nil, err
	}
	next := schema.Participant{ID: id, Seat: start.Seat, Role: role, Human: start.Human, Codec: schema.Codec{Name: a.kind.participant.name, Version: a.kind.participant.version}, Fields: fields}
	v.Participants = append(v.Participants, next)
	record, err := participantRevision(e, original, v, now)
	if err != nil {
		return nil, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.final || s.stopping.Load() {
		return nil, ErrClosed
	}
	if e.record.Envelope().Revision != original.Envelope().Revision {
		return nil, ErrConflict
	}
	if e.presence == nil {
		e.presence = make(map[string]participantPresence, t.opts.Activities.MaxParticipants+1)
	}
	e.presence[id] = participantPresence{}
	e.record = record
	e.dirty = true
	e.lastTouch = now.Monotonic
	return &Participant[P]{entity: e, id: id, codec: a.kind.participant}, nil
}
func (p *Participant[P]) ID() string {
	if p == nil {
		return ""
	}
	return p.id
}
func participantRevision(e *activityEntity, original schema.Record, v schema.Activity, now Instant) (schema.Record, error) {
	if original.Envelope().Revision == math.MaxUint64 {
		return schema.Record{}, ErrCapacity
	}
	activityElapsed(&v, e, now, false)
	return e.kind.buildActivityRecord(v, schema.StateCheckpoint, original.Envelope().Revision+1, now, e.loop != nil)
}
func participantIndex(v schema.Activity, id string) int {
	for i, p := range v.Participants {
		if p.ID == id {
			return i
		}
	}
	return -1
}
func (p *Participant[P]) Set(fields P) error {
	if p == nil || p.entity == nil {
		return nil
	}
	e := p.entity
	s := e.kind.owner.activities
	release, err := s.transaction()
	if err != nil {
		return err
	}
	defer release()
	original, err := activityProjection(e)
	if err != nil {
		return err
	}
	staged, err := encodeActivityFields(e.kind, p.codec, fields)
	if err != nil {
		return err
	}
	v, _ := original.Activity()
	index := participantIndex(v, p.id)
	if index < 0 {
		return ErrClosed
	}
	v.Participants[index].Fields = staged
	now, err := e.kind.owner.activityNow()
	if err != nil {
		return err
	}
	record, err := participantRevision(e, original, v, now)
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
	e.record = record
	e.dirty = true
	e.lastTouch = now.Monotonic
	return nil
}
func incrementParticipant(v uint64) uint64 {
	if v == math.MaxUint64 {
		return v
	}
	return v + 1
}
func sameSession(a, b SessionRef) bool { return a.owner == b.owner && a.token == b.token }
func validSession(ref SessionRef, t *Telemetry) bool {
	return ref.owner == t && ref.token != [16]byte{}
}
func sessionPresence(p *schema.Participant, state participantPresence, now time.Duration) {
	elapsed := state.total
	if state.active && now > state.entered {
		elapsed += now - state.entered
	}
	p.SeatPresenceMS = float64(elapsed) / float64(time.Millisecond)
}
func (p *Participant[P]) Joined(ref SessionRef) error { return p.sessionMutation(ref, "", true) }
func (p *Participant[P]) Left(ref SessionRef, reason string) error {
	return p.sessionMutation(ref, reason, false)
}
func (p *Participant[P]) sessionMutation(ref SessionRef, reason string, join bool) error {
	if p == nil || p.entity == nil {
		return nil
	}
	e := p.entity
	t := e.kind.owner
	s := t.activities
	if !validSession(ref, t) {
		return invalid("session_ref", "foreign_or_disabled")
	}
	e.mu.Lock()
	if e.final || s.stopping.Load() {
		e.mu.Unlock()
		return ErrClosed
	}
	original, state := e.record, e.presence[p.id]
	e.mu.Unlock()
	if join && state.active && sameSession(state.ref, ref) {
		return nil
	}
	if !join && (!state.active || !sameSession(state.ref, ref)) {
		return nil
	}
	release, err := s.transaction()
	if err != nil {
		return err
	}
	defer release()
	now, err := t.activityNow()
	if err != nil {
		return err
	}
	v, _ := original.Activity()
	index := participantIndex(v, p.id)
	if index < 0 {
		return ErrClosed
	}
	projected := &v.Participants[index]
	if state.active && now.Monotonic > state.entered {
		state.total += now.Monotonic - state.entered
	}
	state.entered = now.Monotonic
	sessionPresence(projected, participantPresence{total: state.total}, now.Monotonic)
	reconnect := false
	if join {
		reconnect = projected.Joins > 0
		if reconnect {
			projected.Reconnects = incrementParticipant(projected.Reconnects)
		}
		projected.Joins = incrementParticipant(projected.Joins)
		state = participantPresence{ref: SessionRef{owner: t, token: ref.token}, active: true, entered: now.Monotonic, total: state.total}
		if projected.Human && ref.permitted && t.opts.Sessions.Enabled {
			link := schema.SessionLink{ID: ref.id, VisitID: ref.visit}
			found := false
			for i := range projected.Sessions {
				if projected.Sessions[i].ID == ref.id {
					projected.Sessions[i] = link
					found = true
					break
				}
			}
			if !found {
				if len(projected.Sessions) == 16 {
					projected.Sessions = append(projected.Sessions[:0], projected.Sessions[1:]...)
					projected.LinksTruncated = true
				}
				projected.Sessions = append(projected.Sessions, link)
			}
			client := ref.client
			projected.Client = &client
		}
	} else {
		state.active = false
		projected.Leaves = incrementParticipant(projected.Leaves)
		reason = activityValue(reason, e.kind.reasons, "server_shutdown", "process_restart", "idle", "other")
		found := false
		for i := range projected.Reasons {
			if projected.Reasons[i].Reason == reason {
				projected.Reasons[i].Count = incrementParticipant(projected.Reasons[i].Count)
				found = true
				break
			}
		}
		if !found {
			projected.Reasons = append(projected.Reasons, schema.ReasonCount{Reason: reason, Count: 1})
		}
	}
	activityElapsed(&v, e, now, false)
	sessionPresence(&v.Participants[index], state, now.Monotonic)
	if original.Envelope().Revision == math.MaxUint64 {
		return ErrCapacity
	}
	record, err := e.kind.buildActivityRecord(v, schema.StateCheckpoint, original.Envelope().Revision+1, now, e.loop != nil)
	if err != nil {
		return err
	}
	e.mu.Lock()
	if e.final || s.stopping.Load() {
		e.mu.Unlock()
		return ErrClosed
	}
	current := e.presence[p.id]
	if (join && current.active && sameSession(current.ref, ref)) || (!join && (!current.active || !sameSession(current.ref, ref))) {
		e.mu.Unlock()
		return nil
	}
	if e.record.Envelope().Revision != original.Envelope().Revision {
		e.mu.Unlock()
		return ErrConflict
	}
	e.record = record
	e.presence[p.id] = state
	e.dirty = true
	e.lastTouch = now.Monotonic
	e.mu.Unlock()
	if reconnect {
		e.kind.meters.reconnects.Add(1)
	}
	if !join {
		e.kind.meters.leaves[reason].Add(1)
	}
	return nil
}
