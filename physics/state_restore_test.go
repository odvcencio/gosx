package physics

import (
	"encoding/json"
	"math"
	"math/rand"
	"testing"
)

// A restore must reproduce the world that wrote the snapshot bit for bit, or
// rollback netcode drifts. Tumbling bodies make the rotations non-trivial.
func TestRestoreIsBitExactForRotatingBodies(t *testing.T) {
	build := func() *World {
		w := NewWorld(WorldConfig{Gravity: Vec3{Y: -9.81}, FixedTimestep: 1.0 / 60, DisableWarmStart: true})
		w.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 20, Height: 1, Depth: 20, Offset: Vec3{Y: -0.5}})
		for i := 0; i < 4; i++ {
			b := w.AddBody(BodyConfig{Mass: 1, Friction: 0.5, Position: Vec3{X: float64(i) * 0.3, Y: 0.6 + float64(i)*1.1},
				AngularVelocity: Vec3{X: 1, Z: float64(i)}})
			b.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 1, Height: 1, Depth: 1})
		}
		return w
	}
	original := build()
	var snapshot []byte
	for i := 0; i < 240; i++ {
		if i == 120 {
			snapshot = original.Snapshot()
		}
		original.StepFixed()
	}
	restored := build()
	restored.Restore(snapshot)
	for i := 120; i < 240; i++ {
		restored.StepFixed()
	}
	want, _ := json.Marshal(original.StateSnapshot())
	got, _ := json.Marshal(restored.StateSnapshot())
	if string(got) != string(want) {
		t.Fatalf("restored world drifted from the original:\n got %s\nwant %s", got, want)
	}

	// A hand-written, non-unit rotation is still normalized.
	restored.ApplyState(WorldState{Bodies: []BodyState{{Index: 1 + 1, Rotation: Quat{W: 2}}}})
	if r := restored.bodies[1].Rotation; r != IdentityQuat() {
		t.Fatalf("non-unit rotation restored as %+v", r)
	}
}

// ApplyState must hand back a snapshot rotation with the same bits.
// Renormalizing a unit quaternion changes its last bit about one time in
// thirty, so 200 random rotations catch a restore that renormalizes.
func TestApplyStateKeepsRotationBits(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	w := NewWorld(WorldConfig{Gravity: Vec3{}, FixedTimestep: 1.0 / 60})
	state := WorldState{}
	for i := 0; i < 200; i++ {
		body := w.AddBody(BodyConfig{Mass: 1})
		q := QuatFromAxisAngle(Vec3{X: rng.Float64() - 0.5, Y: rng.Float64() - 0.5, Z: rng.Float64() - 0.5}, rng.Float64()*6)
		q = q.Mul(QuatFromAxisAngle(Vec3{X: 1}, 0.01)).Normalize()
		state.Bodies = append(state.Bodies, BodyState{Index: body.index, Rotation: q})
	}
	w.ApplyState(state)
	for i, item := range state.Bodies {
		got := w.bodies[i].Rotation
		if math.Float64bits(got.X) != math.Float64bits(item.Rotation.X) || math.Float64bits(got.Y) != math.Float64bits(item.Rotation.Y) ||
			math.Float64bits(got.Z) != math.Float64bits(item.Rotation.Z) || math.Float64bits(got.W) != math.Float64bits(item.Rotation.W) {
			t.Fatalf("body %d: restored rotation %+v, snapshot %+v", i, got, item.Rotation)
		}
	}
}
