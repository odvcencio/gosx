// Package replay records a deterministic game cheaply and plays it back with
// scrubbing, pause, slow motion and step controls.
//
// A Recorder stores each tick's inputs and, every Config.Interval ticks, a
// keyframe snapshot of the simulation. A Player seeks to any tick by restoring
// the nearest earlier keyframe and re-simulating the inputs forward. Seeking
// backward is the same operation, so reverse playback needs no extra state.
//
// The simulation must be deterministic: its state after tick N must depend
// only on its snapshot at an earlier tick and the inputs since. Anything that
// is not in the snapshot (wall-clock time, map iteration order, unseeded
// randomness) breaks exact replay. game.Runtime satisfies this when its
// systems are deterministic and its snapshot holds all game state.
//
// Tick numbering: tick N is the state after N calls to Tick since the
// recording began, plus Config.StartTick.
//
// The package imports sim/simcore for snapshots, and game/camera and scene
// for the replay camera rig (Rig). It builds for js/wasm without the hub,
// net/http or the .gsx compiler; the browser dependency test in
// game/deps_test.go checks that.
package replay
