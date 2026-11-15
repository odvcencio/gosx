//go:build !js || !wasm

package game

// These tests drive a Runtime from a sim.Runner, which needs the hub server
// and so builds only for server targets.

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"m31labs.dev/gosx/hub"
	"m31labs.dev/gosx/sim"
)

// A runner loads a save through Restore. When the runtime refuses the snapshot,
// the runner must keep its frame and its replay log: after the load they would
// describe a state that the runtime never reached.
func TestRunnerRestoreKeepsFrameWhenRuntimeRefuses(t *testing.T) {
	rt := midRun(t)
	runner := sim.New(hub.New("refused"), rt, sim.Options{})
	runner.Restore(7, rt.Snapshot())
	if runner.Frame() != 7 {
		t.Fatalf("a good snapshot left the runner at frame %d", runner.Frame())
	}
	before := runner.Replay()

	runner.Restore(42, []byte(`{"version":3,"frame":9}`))
	if runner.Frame() != 7 {
		t.Errorf("a refused snapshot moved the runner to frame %d", runner.Frame())
	}
	if after := runner.Replay(); after.InitialFrame != before.InitialFrame || !bytes.Equal(after.Initial, before.Initial) {
		t.Errorf("a refused snapshot restarted the replay log at frame %d with %s", after.InitialFrame, after.Initial)
	}
}

// Runner.RestoreSnapshot returns why the runtime refuses a snapshot, and with a
// good one it sets the runtime, the frame and the replay log together.
func TestRunnerRestoreSnapshotReportsRefusal(t *testing.T) {
	rt := midRun(t)
	good := rt.Snapshot()
	runner := sim.New(hub.New("checked"), rt, sim.Options{})
	if err := runner.RestoreSnapshot(7, good); err != nil {
		t.Fatal(err)
	}
	if log := runner.Replay(); runner.Frame() != 7 || log.InitialFrame != 7 || !bytes.Equal(log.Initial, good) {
		t.Fatalf("a good snapshot: frame %d, log %d %s", runner.Frame(), log.InitialFrame, log.Initial)
	}
	before, state := runner.Replay(), rt.Snapshot()

	err := runner.RestoreSnapshot(42, []byte(`{"version":3,"frame":9}`))
	if err == nil || !strings.Contains(err.Error(), "version 3 is not supported") {
		t.Fatalf("error %v, want the runtime's reason", err)
	}
	if after := runner.Replay(); runner.Frame() != 7 || after.InitialFrame != 7 || !bytes.Equal(after.Initial, before.Initial) {
		t.Errorf("a refused snapshot changed the runner: frame %d, log %d %s", runner.Frame(), after.InitialFrame, after.Initial)
	}
	if after := rt.Snapshot(); !bytes.Equal(after, state) {
		t.Errorf("a refused snapshot changed the runtime\n before %s\n after  %s", state, after)
	}
}

// A save loads into a running game through Runner.RestoreSnapshot, which takes
// the lock that a tick holds. Calling Runtime.RestoreSnapshot beside the tick
// instead is a data race, which -race reports.
func TestRunnerRestoreSnapshotWhileRunning(t *testing.T) {
	rt := midRun(t)
	snapshot := rt.Snapshot()
	runner := sim.New(hub.New("running"), rt, sim.Options{TickRate: 1000})
	runner.Start()
	defer runner.Stop()
	deadline := time.Now().Add(150 * time.Millisecond)
	for time.Now().Before(deadline) {
		if err := runner.RestoreSnapshot(7, snapshot); err != nil {
			t.Fatal(err)
		}
	}
}
