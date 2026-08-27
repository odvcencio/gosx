package motion

import (
	"math"
	"testing"
)

// --- weights helpers --------------------------------------------------------

// weightsChannel builds a Property:"weights" channel for node with the given
// per-key weight vectors and interpolation.
func weightsChannel(node int, interp string, times, flatValues []float64, weightCount int) ClipChannel {
	return ClipChannel{
		Node:        node,
		Property:    "weights",
		Interp:      interp,
		Times:       times,
		Values:      flatValues,
		WeightCount: weightCount,
	}
}

// trsChannel builds a translation LINEAR channel on node.
func trsChannel(node int, times, flatValues []float64) ClipChannel {
	return ClipChannel{
		Node:     node,
		Property: "translation",
		Interp:   "LINEAR",
		Times:    times,
		Values:   flatValues,
	}
}

const (
	wTol = 1e-9
	// morphPropID returns the stable PropID for weight i: 1000+i.
	// (morphIDBase is private in production; mirror the contract here.)
	weightProp0 = 1000
)

func weightPropID(i int) int { return weightProp0 + i }

// --- five-weight LINEAR / STEP ----------------------------------------------

func TestWeightsFiveLinearAndStep(t *testing.T) {
	// Two keys, five weights. Key0: [0, -0.5, 2, 0.25, 1.5]
	// Key1: [1, 0.5, 0, -0.75, 3]. Midpoint of LINEAR is the average.
	k0 := []float64{0, -0.5, 2, 0.25, 1.5}
	k1 := []float64{1, 0.5, 0, -0.75, 3}
	flat := append(append([]float64{}, k0...), k1...)

	tl, dur := BuildClipTimeline([]ClipChannel{
		weightsChannel(0, "LINEAR", []float64{0, 2}, flat, 5),
	})
	if tl == nil {
		t.Fatal("nil timeline")
	}
	if math.Abs(dur-2) > wTol {
		t.Fatalf("duration=%v want 2", dur)
	}

	// Arity/IDs at t=0: five scalar tracks with TargetID=0, PropID=1000+i.
	for i := 0; i < 5; i++ {
		arity, v := evalOne(t, tl, 0, 0, weightPropID(i))
		if arity != ArityScalar {
			t.Fatalf("weight %d arity=%v want scalar", i, arity)
		}
		if len(v) != 1 {
			t.Fatalf("weight %d len=%d", i, len(v))
		}
		if math.Abs(v[0]-k0[i]) > wTol {
			t.Fatalf("weight %d at t=0: got %v want %v", i, v[0], k0[i])
		}
	}

	// LINEAR midpoint: distinct per-component expected values, including
	// negative and >1 components.
	wantMid := []float64{0.5, 0, 1, -0.25, 2.25}
	for i := 0; i < 5; i++ {
		_, v := evalOne(t, tl, 1, 0, weightPropID(i))
		if len(v) != 1 {
			t.Fatalf("weight %d mid len=%d", i, len(v))
		}
		if math.Abs(v[0]-wantMid[i]) > wTol {
			t.Fatalf("weight %d mid: got %v want %v", i, v[0], wantMid[i])
		}
	}

	// STEP holds key0 until the second key time.
	tls, durs := BuildClipTimeline([]ClipChannel{
		weightsChannel(0, "STEP", []float64{0, 2}, flat, 5),
	})
	if tls == nil {
		t.Fatal("nil STEP timeline")
	}
	if math.Abs(durs-2) > wTol {
		t.Fatalf("STEP duration=%v want 2", durs)
	}
	for i := 0; i < 5; i++ {
		_, v := evalOne(t, tls, 1.9999, 0, weightPropID(i))
		if len(v) != 1 || math.Abs(v[0]-k0[i]) > wTol {
			t.Fatalf("STEP weight %d at 1.9999: got %v want %v", i, v, k0[i])
		}
	}
}

// --- five-weight CUBICSPLINE ------------------------------------------------

// Hermite basis evaluated by hand: h00,h10,h01,h11 at u.
func hermite(t *testing.T, u, p0, m0, p1, m1 float64) float64 {
	t.Helper()
	u2 := u * u
	u3 := u2 * u
	h00 := 2*u3 - 3*u2 + 1
	h10 := u3 - 2*u2 + u
	h01 := -2*u3 + 3*u2
	h11 := u3 - u2
	return h00*p0 + h10*m0 + h01*p1 + h11*m1
}

func TestWeightsFiveCubicSpline(t *testing.T) {
	// Two keys, five weights, key interval [0, 3] (non-unit).
	// Layout per key: in-tangent vector, value vector, out-tangent vector.
	key0Val := []float64{0, 1, -0.5, 0.25, 2}
	key0In := []float64{0.5, -1, 0.25, 2, -0.5}
	key0Out := []float64{1.5, 0.5, -1, 0.75, 1}
	key1Val := []float64{1, 0, 1.5, -1, 0.5}
	key1In := []float64{-0.25, 1, 0.5, -2, 0.25}
	key1Out := []float64{0.75, -0.5, 2, 1, -1}

	// glTF cubic spline values are key-major: [in0, val0, out0, in1, val1, out1]
	var flat []float64
	for _, v := range [][]float64{key0In, key0Val, key0Out, key1In, key1Val, key1Out} {
		flat = append(flat, v...)
	}

	tl, dur := BuildClipTimeline([]ClipChannel{
		weightsChannel(0, "CUBICSPLINE", []float64{0, 3}, flat, 5),
	})
	if tl == nil {
		t.Fatal("nil timeline")
	}
	if math.Abs(dur-3) > wTol {
		t.Fatalf("duration=%v want 3", dur)
	}

	// Sample at u = 0.5 (t = 1.5). Tangents scaled by key interval 3.
	u := 0.5
	for i := 0; i < 5; i++ {
		want := hermite(t, u, key0Val[i], 3*key0Out[i], key1Val[i], 3*key1In[i])
		_, v := evalOne(t, tl, 1.5, 0, weightPropID(i))
		if len(v) != 1 {
			t.Fatalf("weight %d cubic len=%d", i, len(v))
		}
		if math.Abs(v[0]-want) > 1e-6 {
			t.Fatalf("weight %d cubic at u=0.5: got %v want %v", i, v[0], want)
		}
	}

	// Endpoint values must match the key values exactly.
	for i := 0; i < 5; i++ {
		_, v0 := evalOne(t, tl, 0, 0, weightPropID(i))
		if len(v0) != 1 || math.Abs(v0[0]-key0Val[i]) > wTol {
			t.Fatalf("weight %d cubic t=0: got %v want %v", i, v0, key0Val[i])
		}
		_, v1 := evalOne(t, tl, 3, 0, weightPropID(i))
		if len(v1) != 1 || math.Abs(v1[0]-key1Val[i]) > wTol {
			t.Fatalf("weight %d cubic t=3: got %v want %v", i, v1, key1Val[i])
		}
	}
}

// --- mixed TRS + weights, two clips, Mixer blend -----------------------------

func TestWeightsMixedTRSAndBlend(t *testing.T) {
	// Clip A: node 0 weights (2) + node 0 translation; node 1 weights (2).
	clipA := []ClipChannel{
		weightsChannel(0, "LINEAR", []float64{0, 2},
			[]float64{0, 0, 1, 1}, 2),
		weightsChannel(1, "LINEAR", []float64{0, 2},
			[]float64{10, 20, 30, 40}, 2),
		trsChannel(0, []float64{0, 2}, []float64{0, 0, 0, 10, 0, 0}),
	}
	tlA, _ := BuildClipTimeline(clipA)
	if tlA == nil {
		t.Fatal("nil clipA timeline")
	}

	// Stable IDs: node i weight j → (target=i, prop=1000+j), regardless of
	// channel order in the clip.
	_, v := evalOne(t, tlA, 1, 1, weightPropID(0))
	if len(v) != 1 || math.Abs(v[0]-20) > wTol {
		t.Fatalf("clipA node1 w0 mid: got %v want 20", v)
	}
	// TRS still present with unchanged behavior.
	arity, tv := evalOne(t, tlA, 1, 0, 0)
	if arity.Width() != 3 || len(tv) != 3 {
		t.Fatalf("translation arity/len %v %d", arity, len(tv))
	}
	if math.Abs(tv[0]-5) > wTol {
		t.Fatalf("translation mid x=%v want 5", tv[0])
	}

	// Clip B: reversed channel ordering, different weight values for the
	// shared node-0 weights; unrelated node 1 TRS only.
	clipB := []ClipChannel{
		trsChannel(1, []float64{0, 2}, []float64{0, 0, 0, 0, 4, 0}),
		weightsChannel(0, "LINEAR", []float64{0, 2},
			[]float64{100, 200, 300, 400}, 2),
	}
	tlB, _ := BuildClipTimeline(clipB)
	if tlB == nil {
		t.Fatal("nil clipB timeline")
	}

	// Mixer: play both, blend shared node-0 weights.
	m := NewMixer()
	m.AddClip("a", tlA, 2)
	m.AddClip("b", tlB, 2)
	m.Play("a", PlayOptions{})
	m.Play("b", PlayOptions{})
	out := NewWriteBuf(64)
	m.Update(1, Policy{}, out) // both at full weight, t=1

	// Shared node-0 weights blend 50/50: clipA w0 mid = 0.5,
	// clipB w0 mid = 200 → (0.5+200)/2 = 100.25.
	wa, w0, ok := findWrite(out, 0, weightPropID(0))
	if !ok {
		t.Fatal("no blended write for node0 w0")
	}
	if wa != ArityScalar || len(w0) != 1 {
		t.Fatalf("node0 w0 arity/len: %v %d", wa, len(w0))
	}
	if math.Abs(w0[0]-100.25) > wTol {
		t.Fatalf("blend w0: got %v want 100.25", w0[0])
	}
	wa, w1, ok := findWrite(out, 0, weightPropID(1))
	if !ok {
		t.Fatal("no blended write for node0 w1")
	}
	if wa != ArityScalar || len(w1) != 1 {
		t.Fatalf("node0 w1 arity/len: %v %d", wa, len(w1))
	}
	// clipA w1 mid = 0.5, clipB w1 mid = 300 → 150.25.
	if math.Abs(w1[0]-150.25) > wTol {
		t.Fatalf("blend w1: got %v want 150.25", w1[0])
	}
	// Unrelated node-1 weights come only from clipA.
	wa, n1w, ok := findWrite(out, 1, weightPropID(0))
	if !ok {
		t.Fatal("no write for node1 w0")
	}
	if wa != ArityScalar || len(n1w) != 1 {
		t.Fatalf("node1 w0 arity/len: %v %d", wa, len(n1w))
	}
	if math.Abs(n1w[0]-20) > wTol {
		t.Fatalf("node1 w0: got %v want 20", n1w[0])
	}
	// Unrelated node-1 TRS comes only from clipB (y = 2 at mid).
	ta, n1t, ok := findWrite(out, 1, 0)
	if !ok || ta.Width() != 3 || len(n1t) != 3 {
		t.Fatalf("node1 translation missing/short: %v %v", ta, n1t)
	}
	if math.Abs(n1t[1]-2) > wTol {
		t.Fatalf("node1 translation y: got %v want 2", n1t[1])
	}
}

// --- malformed layout table --------------------------------------------------

func TestWeightsMalformedLayouts(t *testing.T) {
	validTRS := trsChannel(9, []float64{0, 1}, []float64{0, 0, 0, 1, 2, 3})

	cases := []struct {
		name string
		ch   ClipChannel
	}{
		{"no times", ClipChannel{Node: 0, Property: "weights", Interp: "LINEAR", Values: []float64{0, 1}, WeightCount: 2}},
		{"no values", ClipChannel{Node: 0, Property: "weights", Interp: "LINEAR", Times: []float64{0, 1}, WeightCount: 2}},
		{"zero count", ClipChannel{Node: 0, Property: "weights", Interp: "LINEAR", Times: []float64{0}, Values: []float64{}, WeightCount: 0}},
		{"negative count", ClipChannel{Node: 0, Property: "weights", Interp: "LINEAR", Times: []float64{0}, Values: []float64{0}, WeightCount: -1}},
		{"truncated linear", ClipChannel{Node: 0, Property: "weights", Interp: "LINEAR", Times: []float64{0, 1}, Values: []float64{0, 1, 2}, WeightCount: 2}},
		{"extra linear", ClipChannel{Node: 0, Property: "weights", Interp: "LINEAR", Times: []float64{0, 1}, Values: []float64{0, 1, 2, 3, 4, 5}, WeightCount: 2}},
		{"truncated cubic", ClipChannel{Node: 0, Property: "weights", Interp: "CUBICSPLINE", Times: []float64{0, 1}, Values: []float64{0, 1, 2, 3, 4, 5}, WeightCount: 2}},
		{"extra cubic", ClipChannel{Node: 0, Property: "weights", Interp: "CUBICSPLINE", Times: []float64{0, 1}, Values: make([]float64, 13), WeightCount: 2}},
		// Huge count with tiny data: must be rejected without a huge
		// allocation. 1<<30 fits in int32/int on all supported platforms,
		// so no out-of-range constant conversion to int occurs.
		{"huge count tiny data", ClipChannel{Node: 0, Property: "weights", Interp: "LINEAR", Times: []float64{0}, Values: []float64{1}, WeightCount: 1 << 30}},
		// Maximal platform int with tiny data: per-key size arithmetic must
		// reject this via overflow-safe checks instead of wrapping to a
		// small count. math.MaxInt is platform-sized, so it converts to int
		// on 32-bit and 64-bit (including wasm32) alike.
		{"max int count tiny data", ClipChannel{Node: 0, Property: "weights", Interp: "LINEAR", Times: []float64{0}, Values: []float64{1}, WeightCount: math.MaxInt}},
		{"invalid node", ClipChannel{Node: -5, Property: "weights", Interp: "LINEAR", Times: []float64{0, 1}, Values: []float64{0, 1, 2, 3}, WeightCount: 2}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Sibling valid TRS channel must survive the malformed one.
			tl, _ := BuildClipTimeline([]ClipChannel{tc.ch, validTRS})
			if tl == nil {
				t.Fatal("nil timeline")
			}
			// Eval appends to the buffer instead of resetting it, so use a
			// fresh buffer per sampled time and validate that time's
			// expected sibling translation separately.
			samples := []struct {
				t    float64
				want [3]float64
			}{
				{0, [3]float64{0, 0, 0}},
				{0.5, [3]float64{0.5, 1, 1.5}},
				{1, [3]float64{1, 2, 3}},
			}
			for _, s := range samples {
				// Must not panic.
				buf := NewWriteBuf(16)
				Eval(tl, s.t, Policy{}, buf)
				// The valid sibling TRS channel is preserved with the
				// expected translation for this time.
				ta, tv, ok := findWrite(buf, 9, 0)
				if !ok {
					t.Fatalf("t=%v: valid TRS sibling lost", s.t)
				}
				if ta.Width() != 3 || len(tv) != 3 {
					t.Fatalf("t=%v: TRS sibling arity/len: %v %d", s.t, ta, len(tv))
				}
				for c := 0; c < 3; c++ {
					if math.Abs(tv[c]-s.want[c]) > wTol {
						t.Fatalf("t=%v: TRS sibling[%d]=%v want %v", s.t, c, tv[c], s.want[c])
					}
				}
				// The malformed weight channel must emit no tracks at all:
				// the buffer holds exactly one write, the sibling TRS
				// record (target, prop, arity header + 3 components).
				if n := len(buf.Writes()); n != 6 {
					t.Fatalf("t=%v: malformed channel produced stray writes: %d write elements", s.t, n)
				}
			}
		})
	}
}

// --- TRS trailing-values guard ----------------------------------------------

func TestTRSAcceptsTrailingValues(t *testing.T) {
	// TRS channels accept trailing Values: components beyond the consumed
	// per-key vec3 blocks are ignored.
	ch := ClipChannel{
		Node:     0,
		Property: "translation",
		Interp:   "LINEAR",
		Times:    []float64{0, 1},
		Values:   []float64{0, 0, 0, 9, 9, 9, 99, 99, 99, 7, 7}, // 11 floats: trailing extras
	}
	tl, dur := BuildClipTimeline([]ClipChannel{ch})
	if tl == nil {
		t.Fatal("nil timeline for TRS with trailing values")
	}
	if math.Abs(dur-1) > wTol {
		t.Fatalf("duration=%v want 1", dur)
	}
	_, v := evalOne(t, tl, 0.5, 0, 0)
	if len(v) != 3 {
		t.Fatalf("translation len=%d want 3", len(v))
	}
	if math.Abs(v[0]-4.5) > wTol || math.Abs(v[1]-4.5) > wTol || math.Abs(v[2]-4.5) > wTol {
		t.Fatalf("translation mid=%v want [4.5 4.5 4.5]", v)
	}
}
