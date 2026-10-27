package game

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"m31labs.dev/gosx/hub"
	"m31labs.dev/gosx/sim"
)

// firing is one run of a cadence system: the tick it ran on and the game time
// it was handed.
type firing struct {
	Tick  uint64
	Delta time.Duration
}

// cadenceLog collects the runs of the two cadence systems under test.
type cadenceLog struct{ every, interval []firing }

// newCadenceRuntime builds a runtime with an Every and an EveryInterval system
// that record into log. The interval is not a whole number of steps, so each
// run passes on an overshoot. A nil codec keeps the default snapshot.
func newCadenceRuntime(log *cadenceLog, step time.Duration, codec SnapshotCodec) *Runtime {
	return New(Config{
		FixedStep: step,
		Snapshot:  codec,
		Systems: []System{
			Every(4, Func("every", PhaseFixedUpdate, recordInto(&log.every))),
			EveryInterval(5*step/2, Func("interval", PhaseFixedUpdate, recordInto(&log.interval))),
		},
	})
}

// recordInto returns a system function that appends each of its runs to dst.
func recordInto(dst *[]firing) func(*Context) error {
	return func(ctx *Context) error {
		*dst = append(*dst, firing{ctx.Tick, ctx.Delta})
		return nil
	}
}

// play steps rt through n frames of delta real time and returns each frame.
func play(t *testing.T, rt *Runtime, n int, delta time.Duration) []Frame {
	t.Helper()
	frames := make([]Frame, 0, n)
	for i := 0; i < n; i++ {
		frame, err := rt.Step(delta)
		if err != nil {
			t.Fatal(err)
		}
		frames = append(frames, frame)
	}
	return frames
}

// requireSameRuns fails when the restored runs of either cadence differ from
// the uninterrupted ones. Both must have run, or the comparison proves nothing.
func requireSameRuns(t *testing.T, want, got cadenceLog) {
	t.Helper()
	if len(want.every) == 0 || len(want.interval) == 0 {
		t.Fatalf("the uninterrupted run must run both cadences: %+v", want)
	}
	if !slices.Equal(want.every, got.every) {
		t.Errorf("Every runs differ\n uninterrupted %v\n restored      %v", want.every, got.every)
	}
	if !slices.Equal(want.interval, got.interval) {
		t.Errorf("EveryInterval runs differ\n uninterrupted %v\n restored      %v", want.interval, got.interval)
	}
}

// requireSameFrames fails at the first frame of got that differs from want.
func requireSameFrames(t *testing.T, want, got []Frame) {
	t.Helper()
	for i := range want {
		if want[i] != got[i] {
			t.Fatalf("frame %d after the snapshot differs\n uninterrupted %+v\n restored      %+v", i+1, want[i], got[i])
		}
	}
}

// A runtime restored from a snapshot must run Every and EveryInterval on the
// same ticks, with the same gathered time, as the runtime that wrote it. One
// fixed step per frame leaves the clock no remainder, so only the cadence
// accumulators can differ.
func TestRestoreKeepsCadenceAccumulators(t *testing.T) {
	const step = 2 * time.Hour
	codecs := []struct {
		name string
		make func() SnapshotCodec
	}{
		{"default", func() SnapshotCodec { return nil }},
		{"savecodec", func() SnapshotCodec { return NewSaveCodec().SnapshotCodec() }},
	}
	for _, c := range codecs {
		t.Run(c.name, func(t *testing.T) {
			var straight, restored cadenceLog
			rt := newCadenceRuntime(&straight, step, c.make())
			play(t, rt, 10, step) // mid-way through both Every(4) and the interval
			snapshot := rt.Snapshot()
			straight = cadenceLog{}
			want := play(t, rt, 30, step)

			other := newCadenceRuntime(&restored, step, c.make())
			if err := other.RestoreSnapshot(snapshot); err != nil {
				t.Fatal(err)
			}
			got := play(t, other, 30, step)

			requireSameFrames(t, want, got)
			requireSameRuns(t, straight, restored)
		})
	}
}

// The clock holds a partial fixed step and a sub-nanosecond fraction between
// frames. A real frame of 33 ms plus 1 ns at a time scale of 0.5 scales to
// 16.5 ms plus half a nanosecond, so a frame runs one or two 10 ms steps and
// both remainders are set. Scales that are not exact in binary check that the
// fraction survives the JSON round trip bit for bit.
func TestRestoreKeepsClockRemainder(t *testing.T) {
	const frame = 33*time.Millisecond + 1
	for _, scale := range []float64{0.5, 0.3, 1.0 / 3, 1.7} {
		t.Run(fmt.Sprintf("scale %v", scale), func(t *testing.T) {
			build := func() *Runtime {
				rt := New(Config{FixedStep: 10 * time.Millisecond})
				rt.SetTimeScale(scale)
				return rt
			}
			rt := build()
			play(t, rt, 5, frame)
			snapshot := rt.Snapshot()
			want := play(t, rt, 200, frame)

			other := build()
			if err := other.RestoreSnapshot(snapshot); err != nil {
				t.Fatal(err)
			}
			got := play(t, other, 200, frame)

			requireSameFrames(t, want, got)
		})
	}
}

// Rolling a running runtime back to a snapshot (rollback netcode) must drop the
// clock remainder and cadence accumulators of the frames it discards.
func TestRestoreRewindsRunningRuntime(t *testing.T) {
	const frame = 33*time.Millisecond + 1
	var log cadenceLog
	rt := newCadenceRuntime(&log, 10*time.Millisecond, nil)
	rt.SetTimeScale(0.5)
	play(t, rt, 7, frame)
	snapshot := rt.Snapshot()
	log = cadenceLog{}
	want := play(t, rt, 30, frame)
	wantRuns := log

	if err := rt.RestoreSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	log = cadenceLog{}
	got := play(t, rt, 30, frame)

	requireSameFrames(t, want, got)
	requireSameRuns(t, wantRuns, log)
}

// A snapshot records the fixed step that wrote its clock remainder. A runtime
// with the same step takes the remainder back exactly. A runtime with another
// step cannot use it, because the remainder can be longer than the new step, so
// it starts the remainder from zero as for a version 1 snapshot, and the
// snapshot still loads, world and cadence times and all. Each frame leaves a
// different remainder, so the sweep meets values on both sides of the shorter
// step.
func TestRestoreAcrossFixedStepChange(t *testing.T) {
	cases := []struct {
		name      string
		saveCodec bool
		step      time.Duration // the step that writes the snapshots
		other     time.Duration // the step that restores them
		interval  time.Duration // of the EveryInterval system, which is not a whole number of steps
		scale     float64
		frame     time.Duration // real time per frame
	}{
		{"default codec, shorter step", false, 10 * time.Millisecond, 5 * time.Millisecond, 25 * time.Millisecond, 0.5, 33*time.Millisecond + 1},
		{"default codec, longer step", false, 10 * time.Millisecond, 20 * time.Millisecond, 25 * time.Millisecond, 0.5, 33*time.Millisecond + 1},
		{"SaveCodec, shorter step", true, time.Hour, 30 * time.Minute, 150 * time.Minute, 2.5, time.Hour},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			build := func(step time.Duration) (*Runtime, EntityID) {
				w, _, tower := buildWorld()
				cfg := Config{World: w, FixedStep: step, Systems: []System{
					EveryInterval(c.interval, Func("gather", PhaseFixedUpdate, func(*Context) error { return nil })),
				}}
				if c.saveCodec {
					cfg.Snapshot = newTestCodec(t).SnapshotCodec()
				}
				rt := New(cfg)
				rt.SetTimeScale(c.scale)
				return rt, tower
			}
			const n = 40
			writer, tower := build(c.step)
			frames := make([]Frame, n)
			snapshots := make([][]byte, n)
			gathered := make([][]CadenceState, n)
			for i := range snapshots {
				SetComponent(writer.World(), tower, health{HP: i}) // tells the snapshots apart
				frames[i] = play(t, writer, 1, c.frame)[0]
				snapshots[i], gathered[i] = writer.Snapshot(), writer.cadenceStates()
			}

			same, _ := build(c.step)
			for i := 0; i < n-1; i++ {
				if err := same.RestoreSnapshot(snapshots[i]); err != nil {
					t.Fatalf("snapshot %d, same step: %v", i, err)
				}
				if got := same.cadenceStates(); !slices.Equal(got, gathered[i]) {
					t.Fatalf("snapshot %d, same step: gathered %+v, want %+v", i, got, gathered[i])
				}
				if got := play(t, same, 1, c.frame)[0]; got != frames[i+1] {
					t.Fatalf("frame after snapshot %d, same step\n uninterrupted %+v\n restored      %+v", i, frames[i+1], got)
				}
			}

			other, otherTower := build(c.other)
			refused := 0
			var first error
			for i := 0; i < n; i++ {
				if err := other.RestoreSnapshot(snapshots[i]); err != nil {
					if first == nil {
						first = err
					}
					refused++
					continue
				}
				if frame := other.Frame(); frame.Index != frames[i].Index || frame.Time != frames[i].Time {
					t.Fatalf("snapshot %d: restored frame %d at %v, want %d at %v", i, frame.Index, frame.Time, frames[i].Index, frames[i].Time)
				}
				if other.clock.accumulator != 0 || other.clock.scaleFrac != 0 {
					t.Fatalf("snapshot %d: the remainder of another step was kept: %v %v", i, other.clock.accumulator, other.clock.scaleFrac)
				}
				if got := other.cadenceStates(); !slices.Equal(got, gathered[i]) {
					t.Fatalf("snapshot %d: gathered %+v, want %+v", i, got, gathered[i])
				}
				if c.saveCodec {
					if got, _ := GetComponent[health](other.World(), otherTower); got.HP != i {
						t.Fatalf("snapshot %d did not restore the world: tower HP %d", i, got.HP)
					}
				}
				play(t, other, 1, c.frame) // the restored runtime must run
			}
			if refused > 0 {
				t.Errorf("%d of %d snapshots were refused after the step changed from %v to %v; the first: %v", refused, n, c.step, c.other, first)
			}
		})
	}
}

// The format is part of every save, so pin its keys. This runtime is after one
// frame: 10 ms of the 16.5 ms scaled delta ran as a step, 6.5 ms and half a
// nanosecond remain, and Every(4) has gathered one step.
func TestSnapshotWritesVersion2AndStateStaysLean(t *testing.T) {
	// base is the version 1 shape of State, and the start of every snapshot.
	const base = `"frame":1,"timeSeconds":0.01,"timeNanos":10000000,"timeScale":0.5,"paused":false`
	const restoreOnly = `"fixedStepNanos":10000000,"accumulatorNanos":6500000,"scaleFrac":0.5,` +
		`"cadence":[{"system":"every","kind":"every","param":4,"depth":0,"gatheredNanos":10000000}]`
	const world = `"world":{"version":1,"next":0}`

	includeWorld := NewSaveCodec()
	includeWorld.IncludeWorldInState = true
	cases := []struct {
		name            string
		codec           SnapshotCodec
		snapshot, state string
	}{
		{"default codec", nil,
			`{"version":2,` + base + `,` + restoreOnly + `}`,
			`{` + base + `}`},
		{"SaveCodec", NewSaveCodec().SnapshotCodec(),
			`{"version":2,` + base + `,` + restoreOnly + `,` + world + `}`,
			`{` + base + `}`},
		// The world joins State on request, and nothing else does.
		{"SaveCodec with the world in State", includeWorld.SnapshotCodec(),
			`{"version":2,` + base + `,` + restoreOnly + `,` + world + `}`,
			`{` + base + `,` + world + `}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rt := New(Config{FixedStep: 10 * time.Millisecond, Snapshot: c.codec, Systems: []System{
				Every(4, Func("every", PhaseFixedUpdate, func(*Context) error { return nil })),
			}})
			rt.SetTimeScale(0.5)
			play(t, rt, 1, 33*time.Millisecond+1)

			if got := string(rt.Snapshot()); got != c.snapshot {
				t.Errorf("snapshot\n got  %s\n want %s", got, c.snapshot)
			}
			// State is broadcast every tick, so it keeps the version 1 shape.
			if got := string(rt.State()); got != c.state {
				t.Errorf("state\n got  %s\n want %s", got, c.state)
			}
		})
	}
}

// A snapshot written before version 2 existed (taken from the code that wrote
// it: three frames at scale 0.5) still loads. It carries no remainder, so the
// clock restarts its remainder from zero as it always did, and it leaves the
// wrappers' gathered time alone.
func TestRestoreReadsVersion1Snapshot(t *testing.T) {
	const version1 = `{"frame":3,"timeSeconds":0.04,"timeNanos":40000000,"timeScale":0.5,"paused":false}`
	var log cadenceLog
	rt := newCadenceRuntime(&log, 10*time.Millisecond, nil)
	rt.SetTimeScale(0.75)
	play(t, rt, 1, 33*time.Millisecond+1) // two steps, 4.75 ms and 0.75 ns left over
	gathered := rt.cadenceStates()
	if rt.clock.accumulator == 0 || rt.clock.scaleFrac == 0 || gathered[0].GatheredNanos == 0 {
		t.Fatalf("the runtime must hold state the snapshot lacks: %+v %+v", rt.clock, gathered)
	}

	if err := rt.RestoreSnapshot([]byte(version1)); err != nil {
		t.Fatal(err)
	}
	frame := rt.Frame()
	if frame.Index != 3 || frame.Time != 40*time.Millisecond || frame.Steps != 4 || rt.TimeScale() != 0.5 || rt.Paused() {
		t.Fatalf("frame %+v scale %v paused %v", frame, rt.TimeScale(), rt.Paused())
	}
	if rt.clock.accumulator != 0 || rt.clock.scaleFrac != 0 {
		t.Fatalf("clock remainder %v %v, want zero", rt.clock.accumulator, rt.clock.scaleFrac)
	}
	if got := rt.cadenceStates(); !slices.Equal(got, gathered) {
		t.Fatalf("version 1 restore changed the wrappers: %+v, was %+v", got, gathered)
	}
}

// midRun returns a runtime part-way through a run, with state in the clock and
// in both wrappers.
func midRun(t *testing.T) *Runtime {
	t.Helper()
	var log cadenceLog
	rt := newCadenceRuntime(&log, 10*time.Millisecond, nil)
	rt.SetTimeScale(0.5)
	play(t, rt, 3, 33*time.Millisecond+1)
	return rt
}

// The same holds for a SaveCodec snapshot, which carries a world. This one is
// from the code that wrote it: five one-hour ticks at scale 3 over the world
// that buildWorld makes.
func TestSaveCodecRestoresVersion1Snapshot(t *testing.T) {
	const version1 = `{"frame":5,"timeSeconds":54000,"timeNanos":54000000000000,"timeScale":3,"paused":false,` +
		`"world":{"version":1,"next":4,"entities":[{"id":1,"name":"hero"},{"id":2},{"id":4,"name":"tower"}],` +
		`"columns":[{"name":"health","entities":[2,4],"values":[{"hp":7},{"hp":100}]},` +
		`{"name":"transform","entities":[1,4],"values":[{"position":{"x":1,"y":2,"z":3}},{"position":{"x":9}}]}],` +
		`"resources":{"calendar":{"Day":41}}}}`
	rt := New(Config{FixedStep: time.Hour, Snapshot: newTestCodec(t).SnapshotCodec()})
	if err := rt.RestoreSnapshot([]byte(version1)); err != nil {
		t.Fatal(err)
	}
	if frame := rt.Frame(); frame.Index != 5 || frame.Time != 15*time.Hour || frame.Steps != 15 || rt.TimeScale() != 3 {
		t.Fatalf("frame %+v scale %v", frame, rt.TimeScale())
	}
	w := rt.World()
	tower, ok := w.Entity("tower")
	if !ok {
		t.Fatal("tower missing")
	}
	if got, _ := GetComponent[health](w, tower); got.HP != 100 {
		t.Fatalf("tower health %+v", got)
	}
	if cal, _ := GetResource[calendar](w); cal.Day != 41 {
		t.Fatalf("calendar %+v", cal)
	}
}

// Restore is a sim.Simulation method and cannot return an error, so
// RestoreSnapshot must say why a snapshot was refused and leave the runtime as
// it was.
func TestRestoreSnapshotReportsBadSnapshots(t *testing.T) {
	// midRun has a 10 ms step, which a version 2 snapshot must record.
	const v2 = `{"version":2,"fixedStepNanos":10000000,"frame":1,`
	cases := []struct{ name, snapshot, want string }{
		{"empty", ``, "empty snapshot"},
		{"not JSON", `{"frame":`, "decode snapshot"},
		{"wrong type", `{"frame":"seven"}`, "decode snapshot"},
		{"newer version", `{"version":3,"frame":1}`, "version 3 is not supported"},
		{"negative version", `{"version":-1,"frame":1}`, "version -1 is not supported"},
		{"no fixed step", `{"version":2,"frame":1}`, "fixed step 0s is not positive"},
		{"negative fixed step", `{"version":2,"fixedStepNanos":-10000000,"frame":1}`, "fixed step -10ms is not positive"},
		{"accumulator of a whole step", v2 + `"accumulatorNanos":10000000}`, "accumulator"},
		{"negative accumulator", v2 + `"accumulatorNanos":-1}`, "accumulator"},
		{"scale fraction of one", v2 + `"scaleFrac":1}`, "scale fraction"},
		{"negative scale fraction", v2 + `"scaleFrac":-0.25}`, "scale fraction"},
		{"negative gathered time", v2 + `"cadence":[{"system":"every","gatheredNanos":-5}]}`, "negative gathered time"},
		// A clock that passed the end of a Duration on the code before the clock
		// stopped there wrote a negative time. That code loaded it with a step count
		// wrapped to about 1.8e19, which shifts every Every schedule and wraps to zero
		// after a few million steps, so this refuses it.
		{"negative time", `{"frame":1,"timeNanos":-1}`, "time -1ns is negative"},
		{"negative time in a version 2 snapshot", v2 + `"timeNanos":-3600000000000}`, "is negative"},
		{"negative time in seconds", `{"frame":1,"timeSeconds":-0.5}`, "timeSeconds"},
		{"time in seconds beyond a Duration", `{"frame":1,"timeSeconds":1e300}`, "timeSeconds"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rt := midRun(t)
			before := rt.Snapshot()
			err := rt.RestoreSnapshot([]byte(c.snapshot))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error %v, want one containing %q", err, c.want)
			}
			rt.Restore([]byte(c.snapshot)) // the sim.Simulation form must also leave it alone
			if after := rt.Snapshot(); !bytes.Equal(after, before) {
				t.Fatalf("a refused snapshot changed the runtime\n before %s\n after  %s", before, after)
			}
		})
	}

	rt := midRun(t)
	var syntax *json.SyntaxError
	if err := rt.RestoreSnapshot([]byte(`{"frame":`)); !errors.As(err, &syntax) {
		t.Fatalf("a decode error must wrap the JSON error: %v", err)
	}
	var nilRuntime *Runtime
	if err := nilRuntime.RestoreSnapshot(rt.Snapshot()); err == nil {
		t.Fatal("a nil runtime accepted a snapshot")
	}
	nilRuntime.Restore(rt.Snapshot()) // must not panic
}

// Restore must accept all that the runtime writes, including the edge: a wrapper
// that has gathered the largest Duration.
func TestRestoreAcceptsGatheredTimeUpToTheEdge(t *testing.T) {
	const v2 = `{"version":2,"fixedStepNanos":10000000,"frame":1,"cadence":`
	for _, c := range []struct{ name, cadence string }{
		{"every wrapper at the largest Duration", `[{"system":"e","kind":"every","param":4,"depth":0,"gatheredNanos":9223372036854775807}]`},
		{"unknown kind", `[{"system":"x","kind":"future","param":0,"depth":0,"gatheredNanos":5}]`},
	} {
		t.Run(c.name, func(t *testing.T) {
			if err := midRun(t).RestoreSnapshot([]byte(v2 + c.cadence + `}`)); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// A SaveCodec restore replaces the world, and nothing can undo that. It must
// check everything that can fail first, so a refused snapshot changes neither
// the world nor the clock.
func TestSaveCodecRestoreSnapshotRefusesWithoutChanges(t *testing.T) {
	codec := newTestCodec(t)
	// setup returns a runtime that has moved on from a good snapshot, with a
	// world that the good snapshot would overwrite.
	setup := func() (rt *Runtime, tower EntityID, good []byte) {
		w, _, tower := buildWorld()
		rt = New(Config{World: w, FixedStep: time.Hour, Snapshot: codec.SnapshotCodec()})
		rt.Tick(nil)
		good = rt.Snapshot()
		SetComponent(w, tower, health{HP: 1})
		for i := 0; i < 3; i++ {
			rt.Tick(nil)
		}
		return rt, tower, good
	}
	edit := func(good []byte, f func(*RuntimeState)) []byte {
		var state RuntimeState
		if err := json.Unmarshal(good, &state); err != nil {
			t.Fatal(err)
		}
		f(&state)
		data, err := json.Marshal(state)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	cases := []struct {
		name string
		edit func(good []byte) []byte
		want string
	}{
		{"not JSON", func([]byte) []byte { return []byte(`{"frame":`) }, "decode snapshot"},
		{"unregistered component", func(good []byte) []byte {
			return edit(good, func(s *RuntimeState) {
				s.World.Columns = append(s.World.Columns, ColumnSave{Name: "from-the-future", Entities: []EntityID{1}, Values: []json.RawMessage{json.RawMessage(`1`)}})
			})
		}, "unregistered component"},
		{"world that does not decode", func(good []byte) []byte {
			return edit(good, func(s *RuntimeState) { s.World.Columns[0].Values[0] = json.RawMessage(`"not an object"`) })
		}, "load component"},
		{"good world, bad clock", func(good []byte) []byte {
			return edit(good, func(s *RuntimeState) { s.AccumulatorNanos = int64(time.Hour) })
		}, "accumulator"},
		{"good world, negative time", func(good []byte) []byte {
			return edit(good, func(s *RuntimeState) { *s.TimeNanos = -1 })
		}, "is negative"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rt, tower, good := setup()
			before := rt.Snapshot()
			err := rt.RestoreSnapshot(c.edit(good))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error %v, want one containing %q", err, c.want)
			}
			if after := rt.Snapshot(); !bytes.Equal(after, before) {
				t.Fatalf("a refused snapshot changed the runtime\n before %s\n after  %s", before, after)
			}
			if got, _ := GetComponent[health](rt.World(), tower); got.HP != 1 {
				t.Fatalf("a refused snapshot replaced the world: tower HP %d", got.HP)
			}
		})
	}

	rt, tower, good := setup()
	if err := rt.RestoreSnapshot(good); err != nil {
		t.Fatal(err)
	}
	if got, _ := GetComponent[health](rt.World(), tower); got.HP != 100 {
		t.Fatalf("the good snapshot did not restore the world: tower HP %d", got.HP)
	}
}

// Restore gives each wrapper the time gathered by the wrapper that agrees with
// it on system name, kind, parameter and nesting, so a system added, removed,
// moved or changed since the snapshot does not shift the rest.
func TestRestoreMatchesCadenceByIdentity(t *testing.T) {
	const step = time.Hour
	noop := func(*Context) error { return nil }
	gather := func(name string, interval time.Duration) System {
		return EveryInterval(interval, Func(name, PhaseFixedUpdate, noop))
	}
	hours := func(n int64) int64 { return int64(time.Duration(n) * time.Hour) }
	// interval and every describe a wrapper that has gathered n hours.
	interval := func(name string, d time.Duration, depth int, n int64) CadenceState {
		return CadenceState{System: name, Kind: "interval", Param: int64(d), Depth: depth, GatheredNanos: hours(n)}
	}
	every := func(name string, ticks int64, depth int, n int64) CadenceState {
		return CadenceState{System: name, Kind: "every", Param: ticks, Depth: depth, GatheredNanos: hours(n)}
	}
	run := func(steps int, systems ...System) []byte {
		rt := New(Config{FixedStep: step, Systems: systems})
		for i := 0; i < steps; i++ {
			rt.Tick(nil)
		}
		return rt.Snapshot()
	}
	restore := func(t *testing.T, snapshot []byte, systems ...System) []CadenceState {
		t.Helper()
		rt := New(Config{FixedStep: step, Systems: systems})
		for i := 0; i < 5; i++ {
			rt.Tick(nil) // gathered time that the restore must replace
		}
		if err := rt.RestoreSnapshot(snapshot); err != nil {
			t.Fatal(err)
		}
		return rt.cadenceStates()
	}
	check := func(t *testing.T, name string, snapshot []byte, want []CadenceState, systems ...System) {
		t.Helper()
		if got := restore(t, snapshot, systems...); !slices.Equal(got, want) {
			t.Errorf("%s: gathered\n got  %+v\n want %+v", name, got, want)
		}
	}

	t.Run("different names", func(t *testing.T) {
		// After 11 steps a (3 h) has gathered 2 h, b (5 h) 1 h and c (7 h) 4 h.
		a, b, c := 3*time.Hour, 5*time.Hour, 7*time.Hour
		snapshot := run(11, gather("a", a), gather("b", b), gather("c", c))
		check(t, "same order", snapshot,
			[]CadenceState{interval("a", a, 0, 2), interval("b", b, 0, 1), interval("c", c, 0, 4)},
			gather("a", a), gather("b", b), gather("c", c))
		check(t, "moved", snapshot,
			[]CadenceState{interval("c", c, 0, 4), interval("a", a, 0, 2), interval("b", b, 0, 1)},
			gather("c", c), gather("a", a), gather("b", b))
		check(t, "added", snapshot,
			[]CadenceState{interval("a", a, 0, 2), interval("d", 9*time.Hour, 0, 0), interval("b", b, 0, 1), interval("c", c, 0, 4)},
			gather("a", a), gather("d", 9*time.Hour), gather("b", b), gather("c", c))
		check(t, "removed", snapshot,
			[]CadenceState{interval("a", a, 0, 2), interval("c", c, 0, 4)},
			gather("a", a), gather("c", c))
	})

	t.Run("one name", func(t *testing.T) {
		// Three wrappers run a system named x. After 11 steps the 3 h interval
		// holds 2 h and the 5 h interval holds 1 h. Every(1000) never runs the
		// interval inside it, so it holds all 11 h and the inner one holds nothing.
		x3, x5 := 3*time.Hour, 5*time.Hour
		nested := func() System { return Every(1000, gather("x", x3)) }
		snapshot := run(11, gather("x", x3), gather("x", x5), nested())
		check(t, "same order", snapshot,
			[]CadenceState{interval("x", x3, 0, 2), interval("x", x5, 0, 1), every("x", 1000, 0, 11), interval("x", x3, 1, 0)},
			gather("x", x3), gather("x", x5), nested())
		// The 3 h interval at depth 0 and the one at depth 1 differ only in depth.
		check(t, "reversed", snapshot,
			[]CadenceState{every("x", 1000, 0, 11), interval("x", x3, 1, 0), interval("x", x5, 0, 1), interval("x", x3, 0, 2)},
			nested(), gather("x", x5), gather("x", x3))
	})

	t.Run("changed wrapper", func(t *testing.T) {
		// A wrapper that is now another kind or has another parameter is another
		// wrapper: it starts from zero and the rest keep their time.
		a, b := 3*time.Hour, 5*time.Hour
		snapshot := run(11, gather("a", a), gather("b", b))
		check(t, "other interval", snapshot,
			[]CadenceState{interval("a", 4*time.Hour, 0, 0), interval("b", b, 0, 1)},
			gather("a", 4*time.Hour), gather("b", b))
		check(t, "other kind", snapshot,
			[]CadenceState{every("a", 3, 0, 0), interval("b", b, 0, 1)},
			Every(3, Func("a", PhaseFixedUpdate, noop)), gather("b", b))
		// Every(2) holds 1 h after 11 steps. An interval of 2 ns shares its
		// parameter, 2, and only its kind tells the two apart.
		check(t, "other kind, same parameter", run(11, Every(2, Func("k", PhaseFixedUpdate, noop))),
			[]CadenceState{interval("k", 2, 0, 0)},
			EveryInterval(2, Func("k", PhaseFixedUpdate, noop)))
	})

	t.Run("same identity", func(t *testing.T) {
		// The two inner wrappers agree on every field, but Every(2) runs its inner
		// one on even steps and Every(3) on multiples of 3, so after 11 steps they
		// hold 2 h and 0. They are matched in order.
		d := 3 * time.Hour
		systems := func() []System {
			return []System{Every(2, gather("d", d)), Every(3, gather("d", d))}
		}
		snapshot := run(11, systems()...)
		want := []CadenceState{every("d", 2, 0, 1), interval("d", d, 1, 2), every("d", 3, 0, 2), interval("d", d, 1, 0)}
		check(t, "same order", snapshot, want, systems()...)
	})
}

// Restore matches a wrapper by its system name, kind, parameter and nesting. By
// name alone, a build that reorders or unwraps same-named wrappers would hand
// one wrapper another's time, and the system would see a wrong ctx.Delta.
func TestRestoreKeepsSameNamedWrappersApart(t *testing.T) {
	const step = time.Hour

	t.Run("swapped order", func(t *testing.T) {
		// Both run a system named tick. After 11 steps Every(24) holds 11 h and
		// the 90 minute interval, which ran on step 10, holds 1 h.
		build := func(log *cadenceLog, swapped bool) *Runtime {
			every := Every(24, Func("tick", PhaseFixedUpdate, recordInto(&log.every)))
			interval := EveryInterval(90*time.Minute, Func("tick", PhaseFixedUpdate, recordInto(&log.interval)))
			if swapped {
				return New(Config{FixedStep: step, Systems: []System{interval, every}})
			}
			return New(Config{FixedStep: step, Systems: []System{every, interval}})
		}
		var straight, restored cadenceLog
		rt := build(&straight, false)
		for i := 0; i < 11; i++ {
			rt.Tick(nil)
		}
		snapshot := rt.Snapshot()
		straight = cadenceLog{}
		for i := 0; i < 50; i++ {
			rt.Tick(nil)
		}

		other := build(&restored, true)
		if err := other.RestoreSnapshot(snapshot); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 50; i++ {
			other.Tick(nil)
		}
		requireSameRuns(t, straight, restored)
	})

	t.Run("outer wrapper removed", func(t *testing.T) {
		// Every(1000) never runs the interval inside it, so after 11 steps it
		// holds 11 h and the interval holds nothing. A bare interval of 3 h has
		// no matching entry and starts from zero: its first run comes on step 14,
		// not on the first step after the restore.
		var nested, bare cadenceLog
		wrapped := New(Config{FixedStep: step, Systems: []System{
			Every(1000, EveryInterval(3*time.Hour, Func("n", PhaseFixedUpdate, recordInto(&nested.interval)))),
		}})
		for i := 0; i < 11; i++ {
			wrapped.Tick(nil)
		}

		alone := New(Config{FixedStep: step, Systems: []System{
			EveryInterval(3*time.Hour, Func("n", PhaseFixedUpdate, recordInto(&bare.interval))),
		}})
		if err := alone.RestoreSnapshot(wrapped.Snapshot()); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 3; i++ {
			alone.Tick(nil)
		}
		if want := []firing{{Tick: 14, Delta: 3 * time.Hour}}; !slices.Equal(bare.interval, want) {
			t.Fatalf("runs %v, want %v", bare.interval, want)
		}
	})
}

// A codec written before RestoreSnapshot existed has no way to report an error;
// RestoreSnapshot must still call it, and a codec that can report one must have
// its error returned.
func TestRestoreSnapshotUsesCodecs(t *testing.T) {
	plain := &plainCodec{}
	rt := New(Config{Snapshot: plain})
	if err := rt.RestoreSnapshot([]byte("saved")); err != nil || string(plain.restored) != "saved" {
		t.Fatalf("plain codec: error %v, restored %q", err, plain.restored)
	}

	var restored []byte
	funcs := New(Config{Snapshot: SnapshotCodecFuncs{RestoreFunc: func(_ *Runtime, data []byte) { restored = data }}})
	if err := funcs.RestoreSnapshot([]byte("saved")); err != nil || string(restored) != "saved" {
		t.Fatalf("RestoreFunc codec: error %v, restored %q", err, restored)
	}

	boom := errors.New("boom")
	checked := New(Config{Snapshot: SnapshotCodecFuncs{RestoreSnapshotFunc: func(*Runtime, []byte) error { return boom }}})
	if err := checked.RestoreSnapshot([]byte("saved")); !errors.Is(err, boom) {
		t.Fatalf("RestoreSnapshotFunc error %v", err)
	}
	checked.Restore([]byte("saved")) // the void form drops the error but still calls the codec

	// With both functions set, RestoreSnapshotFunc is the restore from every
	// entry point: the runtime's, the void form and the codec's own Restore.
	var plainCalls, checkedCalls int
	both := SnapshotCodecFuncs{
		RestoreFunc:         func(*Runtime, []byte) { plainCalls++ },
		RestoreSnapshotFunc: func(*Runtime, []byte) error { checkedCalls++; return boom },
	}
	rtBoth := New(Config{Snapshot: both})
	if err := rtBoth.RestoreSnapshot([]byte("saved")); !errors.Is(err, boom) {
		t.Fatalf("both set: error %v", err)
	}
	rtBoth.Restore([]byte("saved"))
	both.Restore(rtBoth, []byte("saved"))
	if plainCalls != 0 || checkedCalls != 3 {
		t.Fatalf("both set: RestoreFunc ran %d times and RestoreSnapshotFunc %d, want 0 and 3", plainCalls, checkedCalls)
	}
}

type plainCodec struct{ restored []byte }

func (c *plainCodec) Snapshot(*Runtime) []byte        { return nil }
func (c *plainCodec) Restore(_ *Runtime, data []byte) { c.restored = data }
func (c *plainCodec) State(*Runtime) []byte           { return nil }

// A runner loads a save through Restore. When the runtime refuses the snapshot,
// the runner must keep its frame and its replay log: after the load they would
// describe a state that the runtime never reached.
func TestRunnerRestoreKeepsFrameWhenRuntimeRefuses(t *testing.T) {
	rt := midRun(t)
	runner := NewRunner(hub.New("refused"), rt, sim.Options{})
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
	runner := NewRunner(hub.New("checked"), rt, sim.Options{})
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
	runner := NewRunner(hub.New("running"), rt, sim.Options{TickRate: 1000})
	runner.Start()
	defer runner.Stop()
	deadline := time.Now().Add(150 * time.Millisecond)
	for time.Now().Before(deadline) {
		if err := runner.RestoreSnapshot(7, snapshot); err != nil {
			t.Fatal(err)
		}
	}
}
