package physics

import "encoding/json"

// BodyState is the serializable state for one rigid body in a physics world.
type BodyState struct {
	ID              string `json:"id,omitempty"`
	Index           int    `json:"index"`
	Position        Vec3   `json:"position"`
	Rotation        Quat   `json:"rotation"`
	Velocity        Vec3   `json:"velocity,omitempty"`
	AngularVelocity Vec3   `json:"angularVelocity,omitempty"`
}

// WorldState is the compact authoritative state a transport layer broadcasts.
//
// Characters holds every character controller's state. Triggers lists the
// trigger pairs that touched in the last step, which is what decides whether
// the next step reports a pair as entering or staying.
type WorldState struct {
	Bodies     []BodyState        `json:"bodies,omitempty"`
	Characters []CharacterState   `json:"characters,omitempty"`
	Triggers   []TriggerPairState `json:"triggers,omitempty"`
}

// Snapshot returns a restorable physics checkpoint for rollback or replay.
func (w *World) Snapshot() []byte {
	return w.State()
}

// Restore applies a previously captured Snapshot or State payload.
func (w *World) Restore(snapshot []byte) {
	if w == nil || len(snapshot) == 0 {
		return
	}
	var state WorldState
	if err := json.Unmarshal(snapshot, &state); err != nil {
		return
	}
	w.ApplyState(state)
}

// State returns the current authoritative state as JSON for broadcast.
//
// It returns nil for a nil world and nil when the snapshot will not marshal.
// Because the result is usually written straight to a transport, a nil here
// becomes an empty frame rather than an error, and a peer that receives nothing
// cannot tell a marshalling failure from a world that was never stepped. Check
// for nil before broadcasting when a missed frame matters.
//
// The bytes describe the state at the moment of the call. World carries no
// mutex — the package has none — so this is not safe to call while another
// goroutine steps the world. Reading a body's position while the step writes it
// is a data race, not merely a stale frame. Serialize the call with the step,
// or snapshot from the same goroutine that advances the simulation.
func (w *World) State() []byte {
	if w == nil {
		return nil
	}
	data, err := json.Marshal(w.StateSnapshot())
	if err != nil {
		return nil
	}
	return data
}

// StateSnapshot returns a typed copy of the world's dynamic body state, its
// character states and its trigger overlaps.
func (w *World) StateSnapshot() WorldState {
	if w == nil || len(w.bodies) == 0 {
		return WorldState{}
	}
	state := WorldState{Bodies: make([]BodyState, 0, len(w.bodies))}
	if len(w.characters) > 0 {
		state.Characters = make([]CharacterState, 0, len(w.characters))
		for _, c := range w.characters {
			state.Characters = append(state.Characters, c.State())
		}
	}
	if len(w.triggerPairs) > 0 {
		state.Triggers = w.TriggerOverlaps(make([]TriggerPairState, 0, len(w.triggerPairs)))
	}
	for _, body := range w.bodies {
		if body == nil {
			continue
		}
		state.Bodies = append(state.Bodies, BodyState{
			ID:              body.ID,
			Index:           body.index,
			Position:        body.Position,
			Rotation:        body.Rotation,
			Velocity:        body.Velocity,
			AngularVelocity: body.AngularVelocity,
		})
	}
	return state
}

// ApplyState restores matching body transforms and velocities from a
// snapshot, then matching character states, and replaces the trigger overlaps
// with the snapshot's list. Bodies and characters match by index, then by ID;
// ones the snapshot does not name keep their state. A snapshot that names no
// body changes nothing.
func (w *World) ApplyState(state WorldState) {
	if w == nil || len(state.Bodies) == 0 {
		return
	}
	defer w.applyCharactersAndTriggers(state)
	byIndex := make(map[int]*RigidBody, len(w.bodies))
	byID := make(map[string]*RigidBody, len(w.bodies))
	for _, body := range w.bodies {
		if body == nil {
			continue
		}
		byIndex[body.index] = body
		if body.ID != "" {
			byID[body.ID] = body
		}
	}
	for _, item := range state.Bodies {
		body := byIndex[item.Index]
		if body == nil && item.ID != "" {
			body = byID[item.ID]
		}
		if body == nil {
			continue
		}
		body.Position = item.Position
		body.Rotation = restoredRotation(item.Rotation)
		body.Velocity = item.Velocity
		body.AngularVelocity = item.AngularVelocity
		body.Wake()
	}
}

// applyCharactersAndTriggers restores the character and trigger parts of a
// snapshot. It runs after the body states, so a character's position wins
// over its body's.
func (w *World) applyCharactersAndTriggers(state WorldState) {
	byIndex := make(map[int]*Collider, len(w.colliders))
	for _, collider := range w.colliders {
		if collider != nil && collider.index != 0 {
			byIndex[collider.index] = collider
		}
	}
	for _, item := range state.Characters {
		if c := w.characterFor(item); c != nil {
			c.applyState(item, byIndex[item.GroundCollider])
		}
	}
	w.restoreTriggers(state.Triggers, byIndex)
}

// characterFor finds the character a state belongs to, by index and then by
// ID.
func (w *World) characterFor(item CharacterState) *Character {
	for _, c := range w.characters {
		if c.index == item.Index {
			return c
		}
	}
	if item.ID == "" {
		return nil
	}
	for _, c := range w.characters {
		if c.ID == item.ID {
			return c
		}
	}
	return nil
}

// restoredRotation keeps a snapshot rotation bit for bit. Normalizing a
// quaternion that is already unit length changes its last bit about one time
// in thirty, which would make a restored world drift from the one that wrote
// the snapshot. Only a rotation visibly off unit length, such as one typed by
// hand, is normalized.
func restoredRotation(q Quat) Quat {
	length2 := float64(q.X*q.X) + float64(q.Y*q.Y) + float64(q.Z*q.Z) + float64(q.W*q.W)
	if length2 > 1-1e-9 && length2 < 1+1e-9 {
		return q
	}
	return q.Normalize()
}
