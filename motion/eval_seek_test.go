package motion

import (
	"math"
	"testing"
)

func TestEvalDuplicateKeyframeTimes(t *testing.T) {
	track := keyframeLookupTrack(64)
	for i := range track.Keys {
		track.Keys[i].T = float64(i / 3)
		track.Keys[i].Value = ScalarV(float64(i) * 10)
	}
	out := NewWriteBuf(16)
	for _, sample := range []struct {
		at, linear, step float64
	}{
		{-1, 0, 0}, {0, 0, 0}, {.5, 25, 20},
		{1, 50, 50}, {1.5, 55, 50}, {20, 620, 620},
		{20.5, 625, 620}, {21, 630, 630}, {22, 630, 630},
		{math.Inf(-1), 0, 0}, {math.Inf(1), 630, 630},
		{math.NaN(), math.NaN(), 0},
	} {
		for _, mode := range []Interp{InterpLinear, InterpStep} {
			track.Interp = mode
			want := sample.linear
			if mode == InterpStep {
				want = sample.step
			}
			out.Reset()
			evalTrack(&track, sample.at, 0, Policy{}, out)
			got := out.Writes()[3]
			if got != want && !(math.IsNaN(got) && math.IsNaN(want)) {
				t.Fatalf("at %g, mode %d: %g, want %g", sample.at, mode, got, want)
			}
		}
	}
}

func TestRuntimeLongKeyframeSeeksAndAllocations(t *testing.T) {
	track := keyframeLookupTrack(8192)
	timeline := &Timeline{Children: []Positioned{{At: Position{Val: 5.125}, Sub: &Timeline{
		Children: []Positioned{{At: Position{Val: 1.125}, Track: &track}},
	}}}}
	runtime := NewRuntime()
	handle, err := runtime.Load(EncodeProgram(timeline, []string{"mesh"}, []string{"weight"}))
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Unload(handle)
	var samples []struct{ at, want float64 }
	for _, index := range []int{6001, 2, 4095, 0, 8190, 300, 6001} {
		left, right := track.Keys[index], track.Keys[index+1]
		samples = append(samples, struct{ at, want float64 }{
			6.25 + (left.T+right.T)/2, (left.Value.F[0] + right.Value.F[0]) / 2,
		})
	}
	var out [4]float64
	for _, reduced := range []bool{false, true, false} {
		for _, sample := range samples {
			n, err := runtime.Tick(handle, sample.at, reduced, out[:])
			want := sample.want
			if reduced {
				want = track.Keys[len(track.Keys)-1].Value.F[0]
			}
			if err != nil || n != 4 || out[0] != 0 || out[1] != 0 || out[2] != float64(ArityScalar) || out[3] != want {
				t.Fatalf("seek %g reduced=%v: %v, n=%d err=%v, want %g", sample.at, reduced, out, n, err, want)
			}
		}
	}
	if allocations := testing.AllocsPerRun(1000, func() {
		for _, sample := range samples {
			if _, err := runtime.Tick(handle, sample.at, false, out[:]); err != nil {
				t.Fatal(err)
			}
		}
	}); allocations != 0 {
		t.Fatalf("warm long-track seeks allocate %g times per run, want zero", allocations)
	}
}
