// Package physics is a deterministic rigid-body simulation.
//
// Determinism is the point. The same world stepped with the same inputs
// produces bit-identical state on every target GoSX builds for, native and
// WebAssembly alike, which is what lets a server hold authority while a browser
// predicts ahead of it. Everything below follows from that: a fixed timestep,
// an integer solver iteration count, and no wall-clock or map-iteration order
// anywhere in the step.
//
// # The handful that matter
//
//	NewWorld(config)      build a world; WorldConfig carries gravity and the
//	                      fixed timestep
//	World.AddBody         add a rigid body from a BodyConfig
//	World.AddCollider     add collision geometry from a ColliderConfig
//	World.Step(elapsed)   advance by real elapsed time; returns the number of
//	                      fixed steps consumed
//	World.Raycast         query the world along a ray
//
// BuildWorld and BuildWorldChecked construct a whole world from a declarative
// WorldSpec instead, which is the path to use when the layout is data rather
// than code. BuildWorldChecked returns the same world and, alongside it, the
// spec's own diagnostics joined with World.Err — so a spec that quietly dropped
// a body still hands back a usable world and says what it dropped.
//
// # Stepping
//
// Step takes the real time that has passed and runs as many fixed steps as fit,
// keeping the remainder in an accumulator. Passing a variable frame time is
// therefore safe: the simulation still advances in fixed increments, so it stays
// reproducible. StepFixed runs exactly one increment for a caller that owns its
// own clock, and Tick pairs one fixed step with per-player Input.
//
// A frame that arrives late does not make the world take a larger step. It makes
// it take more of them.
//
// # Moving state across a wire
//
// Two pairs, and they are not interchangeable:
//
//	Snapshot / Restore    full fidelity, for rollback and replay
//	State / ApplyState    the dynamic body state, for broadcast
//
// State is the smaller one and is what a server sends to clients each tick.
// Snapshot captures enough to resume the solver exactly, which is what rollback
// needs. StateSnapshot returns the same content as State without the JSON.
//
// None of these take a lock. World carries no mutex — the package has none — so
// a snapshot taken while another goroutine steps the world is a data race, not
// merely a stale frame. Serialize them, or read from the goroutine that steps.
//
// # Defaults worth knowing
//
// Continuous collision detection and contact warm starting are both on.
// WorldConfig.DisableCCD and DisableWarmStart turn them off; warm starting is
// worth disabling only to reproduce a cold solver in a test.
//
// Bodies sleep once they stay slow for WorldConfig.SleepTime, and stop
// consuming solver work until something disturbs them. World.WakeAll overrides
// that when an external change should be felt everywhere at once.
//
// # Checking the result
//
// World.Err reports a configuration problem the constructor tolerated rather
// than panicked on. World.Diagnostics returns what the simulation noticed while
// running. Both are worth reading when a world behaves in a way the
// configuration does not explain.
package physics
