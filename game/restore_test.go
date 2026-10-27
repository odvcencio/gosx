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
	record := func(dst *[]firing) func(*Context) error {
		return func(ctx *Context) error {
			*dst = append(*dst, firing{ctx.Tick, ctx.Delta})
			return nil
		}
	}
	return New(Config{
		FixedStep: step,
		Snapshot:  codec,
		Systems: []System{
			Every(4, Func("every", PhaseFixedUpdate, record(&log.every))),
			EveryInterval(5*step/2, Func("interval", PhaseFixedUpdate, record(&log.interval))),
		},
	})
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

// The format is part of every save, so pin its keys. This runtime is after one
// frame: 10 ms of the 16.5 ms scaled delta ran as a step, 6.5 ms and half a
// nanosecond remain, and Every(4) has gathered one step.
func TestSnapshotWritesVersion2AndStateStaysLean(t *testing.T) {
	rt := New(Config{FixedStep: 10 * time.Millisecond, Systems: []System{
		Every(4, Func("every", PhaseFixedUpdate, func(*Context) error { return nil })),
	}})
	rt.SetTimeScale(0.5)
	play(t, rt, 1, 33*time.Millisecond+1)

	const snapshot = `{"version":2,"frame":1,"timeSeconds":0.01,"timeNanos":10000000,"timeScale":0.5,"paused":false,` +
		`"accumulatorNanos":6500000,"scaleFrac":0.5,"cadence":[{"system":"every","gatheredNanos":10000000}]}`
	if got := string(rt.Snapshot()); got != snapshot {
		t.Fatalf("snapshot\n got  %s\n want %s", got, snapshot)
	}
	// State is broadcast every tick, so it keeps the version 1 shape.
	const state = `{"frame":1,"timeSeconds":0.01,"timeNanos":10000000,"timeScale":0.5,"paused":false}`
	if got := string(rt.State()); got != state {
		t.Fatalf("state\n got  %s\n want %s", got, state)
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
	cases := []struct{ name, snapshot, want string }{
		{"empty", ``, "empty snapshot"},
		{"not JSON", `{"frame":`, "decode snapshot"},
		{"wrong type", `{"frame":"seven"}`, "decode snapshot"},
		{"newer version", `{"version":3,"frame":1}`, "version 3 is not supported"},
		{"negative version", `{"version":-1,"frame":1}`, "version -1 is not supported"},
		{"accumulator of a whole step", `{"version":2,"frame":1,"accumulatorNanos":10000000}`, "accumulator"},
		{"negative accumulator", `{"version":2,"frame":1,"accumulatorNanos":-1}`, "accumulator"},
		{"scale fraction of one", `{"version":2,"frame":1,"scaleFrac":1}`, "scale fraction"},
		{"negative scale fraction", `{"version":2,"frame":1,"scaleFrac":-0.25}`, "scale fraction"},
		{"negative gathered time", `{"version":2,"frame":1,"cadence":[{"system":"every","gatheredNanos":-5}]}`, "negative gathered time"},
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

// Restore gives each wrapper the time gathered by the wrapper of the same name,
// so a system added, removed or moved since the snapshot does not shift the
// rest.
func TestRestoreMatchesCadenceByName(t *testing.T) {
	const step = time.Hour
	noop := func(*Context) error { return nil }
	gather := func(name string, interval time.Duration) System {
		return EveryInterval(interval, Func(name, PhaseFixedUpdate, noop))
	}
	state := func(name string, hours int64) CadenceState {
		return CadenceState{System: name, GatheredNanos: int64(time.Duration(hours) * time.Hour)}
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
	run := func(steps int, systems ...System) []byte {
		rt := New(Config{FixedStep: step, Systems: systems})
		for i := 0; i < steps; i++ {
			rt.Tick(nil)
		}
		return rt.Snapshot()
	}

	t.Run("different names", func(t *testing.T) {
		// After 11 steps a (3 h) has gathered 2 h, b (5 h) 1 h and c (7 h) 4 h.
		snapshot := run(11, gather("a", 3*time.Hour), gather("b", 5*time.Hour), gather("c", 7*time.Hour))
		cases := []struct {
			name    string
			systems []System
			want    []CadenceState
		}{
			{"same order", []System{gather("a", 3*time.Hour), gather("b", 5*time.Hour), gather("c", 7*time.Hour)},
				[]CadenceState{state("a", 2), state("b", 1), state("c", 4)}},
			{"moved", []System{gather("c", 7*time.Hour), gather("a", 3*time.Hour), gather("b", 5*time.Hour)},
				[]CadenceState{state("c", 4), state("a", 2), state("b", 1)}},
			{"added", []System{gather("a", 3*time.Hour), gather("d", 9*time.Hour), gather("b", 5*time.Hour), gather("c", 7*time.Hour)},
				[]CadenceState{state("a", 2), state("d", 0), state("b", 1), state("c", 4)}},
			{"removed", []System{gather("a", 3*time.Hour), gather("c", 7*time.Hour)},
				[]CadenceState{state("a", 2), state("c", 4)}},
		}
		for _, c := range cases {
			if got := restore(t, snapshot, c.systems...); !slices.Equal(got, c.want) {
				t.Errorf("%s: gathered %+v, want %+v", c.name, got, c.want)
			}
		}
	})

	t.Run("one name twice and nested", func(t *testing.T) {
		// Both x gather every step but reset at different times: after 11 steps
		// x (3 h) holds 2 h and x (5 h) holds 1 h. The outer Every of n never
		// runs, so it holds all 11 h and the interval inside it holds nothing.
		systems := func() []System {
			return []System{
				gather("x", 3*time.Hour), gather("x", 5*time.Hour),
				Every(1000, gather("n", 3*time.Hour)),
			}
		}
		want := []CadenceState{state("x", 2), state("x", 1), state("n", 11), state("n", 0)}
		snapshot := run(11, systems()...)
		if got := restore(t, snapshot, systems()...); !slices.Equal(got, want) {
			t.Fatalf("gathered %+v, want %+v", got, want)
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
}

type plainCodec struct{ restored []byte }

func (c *plainCodec) Snapshot(*Runtime) []byte        { return nil }
func (c *plainCodec) Restore(_ *Runtime, data []byte) { c.restored = data }
func (c *plainCodec) State(*Runtime) []byte           { return nil }
