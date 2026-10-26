package motion

import (
	"math"
	"testing"
)

func springRuntimeTrack(id int, from, to float64) *Track {
	return &Track{TargetID: id, Gen: &Generator{Kind: GenSpring, Base: Vec2V(from, to), Spring: Spring{Mass: 1, Stiffness: 400, Damping: 2, Velocity: -.1}}}
}

func TestRuntimePreparedSpringMatchesEvalAcrossSeeksAndPolicy(t *testing.T) {
	track := springRuntimeTrack(1, -.2, 1.4)
	invalid := springRuntimeTrack(3, 0, 1)
	invalid.Gen.Base = ScalarV(0)
	timeline := &Timeline{Children: []Positioned{
		{At: Position{Val: 3}, Sub: &Timeline{Children: []Positioned{{At: Position{Val: 2}, Track: track}}}},
		{Track: &Track{TargetID: 2, Keys: []Key{{Value: ScalarV(0)}, {T: 2, Value: ScalarV(1)}}}},
		{Track: invalid},
	}}
	runtime := NewRuntime()
	handle, err := runtime.Load(EncodeProgram(timeline, nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(runtime.programs[handle].springs) != 1 {
		t.Fatal("decoded spring was not prepared, or malformed spring was prepared")
	}
	reference := NewWriteBuf(64)
	output := make([]float64, 64)
	for _, reduced := range []bool{false, true, false} {
		for _, at := range []float64{-1, 0, .003, .5, 2, 8.007, .13, 11, math.NaN()} {
			reference.Reset()
			Eval(timeline, at, Policy{ReducedMotion: reduced}, reference)
			n, err := runtime.Tick(handle, at, reduced, output)
			if err != nil || n != len(reference.Writes()) {
				t.Fatalf("runtime result length: %d, %v", n, err)
			}
			for i, value := range reference.Writes() {
				if math.Float64bits(output[i]) != math.Float64bits(value) {
					t.Fatalf("at %g, reduced=%v, value %d: %g vs %g", at, reduced, i, output[i], value)
				}
			}
		}
	}
	if allocations := testing.AllocsPerRun(1000, func() { _, _ = runtime.Tick(handle, 8.007, false, output) }); allocations != 0 {
		t.Fatalf("prepared runtime allocated %g times", allocations)
	}
}

func TestRuntimeSpringTablesShareAndStayWithinBudget(t *testing.T) {
	timeline := &Timeline{}
	for i := 0; i < 256; i++ {
		timeline.Children = append(timeline.Children, Positioned{Track: springRuntimeTrack(i, 0, float64(i+1))})
	}
	// A duplicate after budget exhaustion must still reuse the earlier table.
	duplicate := springRuntimeTrack(256, 0, 1)
	timeline.Children = append(timeline.Children, Positioned{Track: duplicate})
	prepared := prepareRuntimeSprings(timeline)
	unique := make(map[*PreparedSpring]bool)
	bytes := 0
	for _, sampler := range prepared {
		if !unique[sampler] {
			unique[sampler] = true
			bytes += len(sampler.checkpoints) * 16
		}
	}
	if bytes > runtimeSpringCheckpointBytes || len(prepared) == 0 || len(prepared) >= len(timeline.Children) {
		t.Fatalf("unbounded preparation or no fallback: bytes=%d prepared=%d", bytes, len(prepared))
	}
	if prepared[duplicate] == nil || prepared[duplicate] != prepared[timeline.Children[0].Track] {
		t.Fatal("duplicate after budget exhaustion did not reuse the table")
	}
	// Check fallback output at a time with many Euler steps still remaining.
	runtime := NewRuntime()
	handle, err := runtime.Load(EncodeProgram(timeline, nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	output := make([]float64, len(timeline.Children)*4)
	n, err := runtime.Tick(handle, 8.007, false, output)
	if err != nil || n != len(output) {
		t.Fatal(n, err)
	}
	for i, child := range timeline.Children {
		gen := child.Track.Gen
		if got, want := output[i*4+3], gen.Spring.Value(gen.Base.F[0], gen.Base.F[1], 8.007); got != want {
			t.Fatalf("cached/fallback mismatch at track %d: %.20g vs %.20g", i, got, want)
		}
	}
}

func TestRuntimeSpringSharingPreservesSignedZeroAndDefaults(t *testing.T) {
	first := springRuntimeTrack(1, 0, 1)
	second := springRuntimeTrack(2, 0, 1)
	first.Gen.Spring, second.Gen.Spring = Spring{}, Spring{Mass: 1, Stiffness: 100, Damping: 10}
	negative := springRuntimeTrack(3, math.Copysign(0, -1), 1)
	negative.Gen.Spring = Spring{}
	timeline := &Timeline{Children: []Positioned{{Track: first}, {Track: second}, {Track: negative}}}
	prepared := prepareRuntimeSprings(timeline)
	if prepared[first] == nil || prepared[first] != prepared[second] || prepared[first] == prepared[negative] {
		t.Fatal("default sharing or signed-zero isolation is incorrect")
	}
	if math.Float64bits(prepared[negative].Value(0)) != math.Float64bits(negative.Gen.Base.F[0]) {
		t.Fatal("shared preparation lost the endpoint's sign")
	}
	plain := &Timeline{Children: []Positioned{{Track: &Track{Keys: []Key{{Value: ScalarV(1)}}}}}}
	if prepareRuntimeSprings(plain) != nil {
		t.Fatal("ordinary keyframe programs acquired a spring cache")
	}
}
