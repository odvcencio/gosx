//gosx:deterministic

// Package teamai is a deterministic toolkit for team and squad AI: formations,
// timed paths, steering and interception, an assignment executor driven by
// rules, and seeded contest resolution.
//
// A game describes its formations, paths, rules, assignments and plans as data
// (a [Spec], usually loaded from JSON with [LoadJSON]) and compiles them once
// into a [Book]. An [Executor] then runs one assignment per agent at the game's
// fixed step. The same Book, the same [World] view and the same executor state
// give the same commands on every platform GoSX targets (js/wasm, amd64,
// arm64), so the server and a predicting client agree bit for bit.
//
// The package works in the ground plane. [Vec2] X is the lateral world axis and
// Y is the second ground axis (world Z in a Y-up 3D scene). Heights, jumping and
// collision belong to the host and its physics.
//
// # The pieces
//
//	Frame, Rect          a movable reference: origin spot, facing, strength
//	                     side, mirror, lateral band; and the field bounds
//	Formation, Slot      alignment slots relative to a frame, assigned to
//	                     agents by role
//	Path, Segment        timed segments (line, curve, hold, decide) with speed
//	                     profiles, landmarks, marks and rule-chosen branches
//	Seek, Arrive,        steering and interception math; every function is
//	Intercept, Cutoff,   pure and allocation-free
//	Separation
//	Predicate, Chooser   the rule interface over a read-only per-agent Query
//	Executor             runs assignments, returns per-agent Commands and
//	                     Events, and snapshots its state
//	ContestModel         seeded, rating-weighted one-on-one resolution
//
// # Determinism
//
// The package is marked //gosx:deterministic and passes the GoSX deterministic
// linter (gosx detlint ./game/teamai): every float product that feeds an
// addition is rounded by an explicit conversion, transcendental math goes
// through package det, nothing ranges over a map, nothing reads the clock, and
// random draws come from [Mix], a stateless hash of a seed and integer keys.
// Agents are stepped in index order and each reads only the World given for the
// tick, so the order of evaluation never changes a result.
//
// # Costs
//
// [Executor.Step] does not allocate in steady state. It does no work for an
// agent without an assignment, and a decision or interrupt costs one predicate
// call per branch. See docs/team-ai.md for measured per-tick costs.
package teamai
