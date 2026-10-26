package motion

import (
	"math"
	"testing"
)

func constantTrackValues() []Value {
	return []Value{
		ScalarV(math.Copysign(0, -1)), Vec2V(-2, 3), Vec3V(1, -2, 4),
		Vec4V(-1, 2, -3, 4), QuatV(Quat{X: .6, W: .8}), ColorV(.2, .4, .8, 1),
	}
}

func TestEvalConstantTrackClockAndOffsets(t *testing.T) {
	out := NewWriteBuf(16)
	for _, value := range constantTrackValues() {
		for _, mode := range []Interp{InterpLinear, InterpStep, InterpCubicSpline, InterpSlerp} {
			for _, keyTime := range []float64{-2, .5, 2} {
				track := Track{TargetID: 3, PropID: 7, Interp: mode, Keys: []Key{{T: keyTime, Value: value}}}
				for _, offset := range []float64{0, 3, math.NaN(), math.Inf(1), math.Inf(-1)} {
					timeline := &Timeline{Children: []Positioned{{At: Position{Val: offset}, Sub: &Timeline{
						Children: []Positioned{{Track: &track}},
					}}}}
					for _, reduced := range []bool{false, true} {
						for _, at := range []float64{0, -3, .25, 5, math.Inf(-1), math.Inf(1), math.NaN(), .75} {
							out.Reset()
							Eval(timeline, at, Policy{ReducedMotion: reduced}, out)
							writes := out.Writes()
							if len(writes) != 3+value.Arity.Width() || writes[0] != 3 || writes[1] != 7 || writes[2] != float64(value.Arity) {
								t.Fatalf("constant arity %d produced invalid packed writes: %v", value.Arity, writes)
							}
							for i, got := range writes[3:] {
								if math.Float64bits(got) != math.Float64bits(value.F[i]) {
									t.Fatalf("arity=%d mode=%d key=%g offset=%g t=%g reduced=%v: %v, want %v", value.Arity, mode, keyTime, offset, at, reduced, writes[3:], value.F)
								}
							}
						}
					}
				}
			}
		}
	}
	timeline := &Timeline{Children: []Positioned{{Track: &Track{Keys: []Key{{Value: Vec3V(1, 2, 3)}}}}}}
	if allocations := testing.AllocsPerRun(1000, func() {
		out.Reset()
		Eval(timeline, math.NaN(), Policy{}, out)
	}); allocations != 0 {
		t.Fatalf("constant NaN-time samples allocate %g times, want zero", allocations)
	}
}

func TestRuntimeConstantTrackInvalidClock(t *testing.T) {
	runtime := NewRuntime()
	timeline := &Timeline{Children: []Positioned{{Track: &Track{Keys: []Key{{T: .5, Value: ScalarV(42)}}}}}}
	handle, err := runtime.Load(EncodeProgram(timeline, nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Unload(handle)
	var out [4]float64
	for _, at := range []float64{math.NaN(), math.Inf(-1), math.Inf(1), 0, .75} {
		t.Logf("constant-track input=%g expected=[0 0 0 42]", at)
		n, err := runtime.Tick(handle, at, false, out[:])
		if err != nil || n != 4 || out != [4]float64{0, 0, 0, 42} {
			t.Fatalf("constant tick at %g: n=%d err=%v writes=%v", at, n, err, out)
		}
		t.Logf("constant-track input=%g observed=%v", at, out)
	}
}
