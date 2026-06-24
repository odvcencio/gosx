package motion

import (
	"errors"
	"testing"
)

// buildRuntimeProgram builds a minimal timeline with a vec3 keyframe track (TargetID=0,
// PropID=0) and a GenSpin track (TargetID=0, PropID=1), encodes it, and returns the blob.
// targetRefs=["mesh0"], propRefs=["position","rotation"].
func buildRuntimeProgram() (blob []byte, tl *Timeline, targetRefs, propRefs []string) {
	targetRefs = []string{"mesh0"}
	propRefs = []string{"position", "rotation"}

	tl = &Timeline{
		ID: "rt-test",
		Children: []Positioned{
			{
				At: Position{Kind: PosAbs, Val: 0},
				Track: &Track{
					TargetID: 0,
					PropID:   0,
					Keys: []Key{
						{T: 0, Value: Vec3V(0, 0, 0)},
						{T: 1, Value: Vec3V(10, 0, 0)},
					},
					Interp: InterpLinear,
				},
			},
			{
				At: Position{Kind: PosAbs, Val: 0},
				Track: &Track{
					TargetID: 0,
					PropID:   1,
					Gen: &Generator{
						Kind: GenSpin,
						Spin: [3]float64{0, 1.0, 0},
					},
				},
			},
		},
	}
	blob = EncodeProgram(tl, targetRefs, propRefs)
	return blob, tl, targetRefs, propRefs
}

// TestRuntimeLoad verifies Load returns a handle >= 1 and no error.
func TestRuntimeLoad(t *testing.T) {
	rt := NewRuntime()
	blob, _, _, _ := buildRuntimeProgram()
	handle, err := rt.Load(blob)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if handle < 1 {
		t.Fatalf("expected handle >= 1, got %d", handle)
	}
}

// TestRuntimeLoadBadBytes verifies Load rejects bad wire bytes.
func TestRuntimeLoadBadBytes(t *testing.T) {
	rt := NewRuntime()
	_, err := rt.Load([]byte{0x00, 0x01, 0x02})
	if err == nil {
		t.Fatal("expected error for bad wire bytes, got nil")
	}
}

// TestRuntimeRoundTripEval: Load then Tick must produce the same floats as direct Eval.
func TestRuntimeRoundTripEval(t *testing.T) {
	blob, tl, _, _ := buildRuntimeProgram()
	rt := NewRuntime()
	handle, err := rt.Load(blob)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	out := make([]float64, 64)
	n, err := rt.Tick(handle, 0.5, false, out)
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if n <= 0 {
		t.Fatalf("Tick returned n=%d, expected > 0", n)
	}

	// Direct Eval for comparison.
	ref := NewWriteBuf(64)
	Eval(tl, 0.5, Policy{ReducedMotion: false}, ref)
	want := ref.Writes()

	if n != len(want) {
		t.Fatalf("n mismatch: Tick=%d, Eval=%d", n, len(want))
	}
	for i, v := range want {
		if out[i] != v {
			t.Errorf("index %d: Tick=%v, Eval=%v", i, out[i], v)
		}
	}
}

// TestRuntimeReducedMotion: Tick with reduced=true must differ from reduced=false for
// animated tracks (the keyframe track clamps to last key; spin gen emits identity quat).
func TestRuntimeReducedMotion(t *testing.T) {
	blob, _, _, _ := buildRuntimeProgram()
	rt := NewRuntime()
	handle, err := rt.Load(blob)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	outFull := make([]float64, 64)
	nFull, err := rt.Tick(handle, 0.5, false, outFull)
	if err != nil {
		t.Fatalf("Tick(false): %v", err)
	}

	outReduced := make([]float64, 64)
	nReduced, err := rt.Tick(handle, 0.5, true, outReduced)
	if err != nil {
		t.Fatalf("Tick(true): %v", err)
	}

	if nFull != nReduced {
		t.Fatalf("reduced-motion changed write count: full=%d reduced=%d", nFull, nReduced)
	}

	// At least one float must differ between reduced and full.
	same := true
	for i := 0; i < nFull; i++ {
		if outFull[i] != outReduced[i] {
			same = false
			break
		}
	}
	if same {
		t.Errorf("reduced-motion Tick produced identical output to full Tick; expected differences")
	}
}

// TestRuntimeDeterminism: two Ticks at the same t produce identical output.
func TestRuntimeDeterminism(t *testing.T) {
	blob, _, _, _ := buildRuntimeProgram()
	rt := NewRuntime()
	handle, err := rt.Load(blob)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	out1 := make([]float64, 64)
	n1, err := rt.Tick(handle, 0.5, false, out1)
	if err != nil {
		t.Fatalf("Tick 1: %v", err)
	}

	out2 := make([]float64, 64)
	n2, err := rt.Tick(handle, 0.5, false, out2)
	if err != nil {
		t.Fatalf("Tick 2: %v", err)
	}

	if n1 != n2 {
		t.Fatalf("n mismatch: %d vs %d", n1, n2)
	}
	for i := 0; i < n1; i++ {
		if out1[i] != out2[i] {
			t.Errorf("index %d differs: %v vs %v", i, out1[i], out2[i])
		}
	}
}

// TestRuntimeTruncation: a too-small out slice returns the full n (> len(out))
// and fills only the first len(out) values without panic.
func TestRuntimeTruncation(t *testing.T) {
	blob, _, _, _ := buildRuntimeProgram()
	rt := NewRuntime()
	handle, err := rt.Load(blob)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// First get the full count.
	full := make([]float64, 64)
	nFull, err := rt.Tick(handle, 0.5, false, full)
	if err != nil {
		t.Fatalf("Tick full: %v", err)
	}

	// Now truncate to 2 elements — must not panic, must return nFull, must fill first 2.
	small := make([]float64, 2)
	nSmall, err := rt.Tick(handle, 0.5, false, small)
	if err != nil {
		t.Fatalf("Tick small: %v", err)
	}
	if nSmall != nFull {
		t.Fatalf("truncated Tick returned n=%d, expected %d", nSmall, nFull)
	}
	// The first 2 elements of small must match the first 2 of full.
	for i := 0; i < 2; i++ {
		if small[i] != full[i] {
			t.Errorf("truncated[%d]=%v, full[%d]=%v", i, small[i], i, full[i])
		}
	}
}

// TestRuntimeUnknownHandle: Tick with an unknown handle returns an error.
func TestRuntimeUnknownHandle(t *testing.T) {
	rt := NewRuntime()
	out := make([]float64, 64)
	_, err := rt.Tick(999, 0.5, false, out)
	if err == nil {
		t.Fatal("expected error for unknown handle, got nil")
	}
}

// TestRuntimeZeroAlloc: after a warmup Tick, subsequent Ticks must not allocate.
func TestRuntimeZeroAlloc(t *testing.T) {
	blob, _, _, _ := buildRuntimeProgram()
	rt := NewRuntime()
	handle, err := rt.Load(blob)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	out := make([]float64, 64)
	// Warmup tick (may grow the internal WriteBuf).
	if _, err := rt.Tick(handle, 0.5, false, out); err != nil {
		t.Fatalf("warmup Tick: %v", err)
	}

	allocs := testing.AllocsPerRun(1000, func() {
		rt.Tick(handle, 0.5, false, out) //nolint:errcheck
	})
	if allocs != 0 {
		t.Errorf("expected 0 allocs per Tick after warmup, got %v", allocs)
	}
}

// TestRuntimeRefs: TargetRefs and PropRefs return the correct strings.
func TestRuntimeRefs(t *testing.T) {
	blob, _, wantTargets, wantProps := buildRuntimeProgram()
	rt := NewRuntime()
	handle, err := rt.Load(blob)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	targets, ok := rt.TargetRefs(handle)
	if !ok {
		t.Fatal("TargetRefs: handle not found")
	}
	if !equalStrings(targets, wantTargets) {
		t.Errorf("TargetRefs: got %v, want %v", targets, wantTargets)
	}

	props, ok := rt.PropRefs(handle)
	if !ok {
		t.Fatal("PropRefs: handle not found")
	}
	if !equalStrings(props, wantProps) {
		t.Errorf("PropRefs: got %v, want %v", props, wantProps)
	}

	// Unknown handle → ok=false.
	_, ok = rt.TargetRefs(999)
	if ok {
		t.Error("TargetRefs(999): expected ok=false for unknown handle")
	}
	_, ok = rt.PropRefs(999)
	if ok {
		t.Error("PropRefs(999): expected ok=false for unknown handle")
	}
}

// TestRuntimeUnload: after Unload, Tick returns an error and Refs return ok=false.
func TestRuntimeUnload(t *testing.T) {
	blob, _, _, _ := buildRuntimeProgram()
	rt := NewRuntime()
	handle, err := rt.Load(blob)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	rt.Unload(handle)

	out := make([]float64, 64)
	_, err = rt.Tick(handle, 0.5, false, out)
	if err == nil {
		t.Fatal("expected error after Unload, got nil")
	}
	_, ok := rt.TargetRefs(handle)
	if ok {
		t.Error("TargetRefs: expected ok=false after Unload")
	}
}

// TestRuntimeMultiplePrograms: two programs loaded into the same runtime are independent.
func TestRuntimeMultiplePrograms(t *testing.T) {
	rt := NewRuntime()

	blob1, tl1, _, _ := buildRuntimeProgram()
	h1, err := rt.Load(blob1)
	if err != nil {
		t.Fatalf("Load 1: %v", err)
	}

	// Build a second, different program (scalar only).
	tl2 := &Timeline{
		Children: []Positioned{
			{
				At: Position{Kind: PosAbs, Val: 0},
				Track: &Track{
					TargetID: 1,
					PropID:   0,
					Keys: []Key{
						{T: 0, Value: ScalarV(0)},
						{T: 1, Value: ScalarV(100)},
					},
					Interp: InterpLinear,
				},
			},
		},
	}
	blob2 := EncodeProgram(tl2, []string{"other"}, []string{"scale"})
	h2, err := rt.Load(blob2)
	if err != nil {
		t.Fatalf("Load 2: %v", err)
	}

	if h1 == h2 {
		t.Fatalf("handles must be distinct, both = %d", h1)
	}

	out := make([]float64, 64)

	// h1 must match direct eval of tl1.
	n1, _ := rt.Tick(h1, 0.5, false, out)
	ref1 := NewWriteBuf(64)
	Eval(tl1, 0.5, Policy{}, ref1)
	if n1 != len(ref1.Writes()) {
		t.Errorf("h1 n=%d, want %d", n1, len(ref1.Writes()))
	}

	// h2 must match direct eval of tl2.
	n2, _ := rt.Tick(h2, 0.5, false, out)
	ref2 := NewWriteBuf(64)
	Eval(tl2, 0.5, Policy{}, ref2)
	if n2 != len(ref2.Writes()) {
		t.Errorf("h2 n=%d, want %d", n2, len(ref2.Writes()))
	}

	// The two programs produce different counts.
	if n1 == n2 && n1 == 0 {
		t.Error("both programs produced 0 writes — test is useless")
	}
}

// TestRuntimeHandlesStartAt1: first loaded handle is >= 1 (not 0).
func TestRuntimeHandlesStartAt1(t *testing.T) {
	rt := NewRuntime()
	blob, _, _, _ := buildRuntimeProgram()
	h, _ := rt.Load(blob)
	if h < 1 {
		t.Fatalf("first handle = %d, want >= 1", h)
	}
}

// TestRuntimeSequentialHandles: handles are assigned sequentially.
func TestRuntimeSequentialHandles(t *testing.T) {
	rt := NewRuntime()
	blob, _, _, _ := buildRuntimeProgram()
	h1, _ := rt.Load(blob)
	h2, _ := rt.Load(blob)
	h3, _ := rt.Load(blob)
	if h2 != h1+1 || h3 != h2+1 {
		t.Errorf("handles not sequential: %d, %d, %d", h1, h2, h3)
	}
}

// TestRuntimeTickNilOut: Tick with nil out must not panic; n returns full count.
func TestRuntimeTickNilOut(t *testing.T) {
	blob, _, _, _ := buildRuntimeProgram()
	rt := NewRuntime()
	handle, err := rt.Load(blob)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Tick(nil out) panicked: %v", r)
		}
	}()
	n, err := rt.Tick(handle, 0.5, false, nil)
	if err != nil {
		t.Fatalf("Tick(nil): %v", err)
	}
	if n <= 0 {
		t.Errorf("Tick(nil): expected n>0, got %d", n)
	}
}

// Ensure errors package is used (compile-time check).
var _ = errors.New
