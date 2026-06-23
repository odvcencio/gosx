package bundle

import (
	"encoding/binary"
	"math"
	"strings"
	"testing"

	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/motion"
)

func TestFrameAppliesTopLevelAnimationToInstancedMeshTransforms(t *testing.T) {
	d := newFakeDevice()
	r, err := New(Config{Device: d, Surface: fakeSurface{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Destroy()

	original := identityTransform()
	b := engine.RenderBundle{
		Camera: engine.RenderCamera{Z: 5, FOV: 1, Near: 0.1, Far: 100},
		InstancedMeshes: []engine.RenderInstancedMesh{{
			ID:            "hero",
			Kind:          "cube",
			VertexCount:   36,
			InstanceCount: 1,
			Transforms:    original,
		}},
		Animations: []engine.RenderAnimation{{
			Name:     "slide",
			Duration: 1,
			Channels: []engine.RenderAnimationChannel{{
				TargetID:      "hero",
				Property:      "translation",
				Times:         []float64{0, 1},
				Values:        []float64{0, 0, 0, 2, 0, 0},
				Interpolation: "LINEAR",
			}},
		}},
	}
	if err := r.Frame(b, 400, 300, 0.5); err != nil {
		t.Fatalf("Frame: %v", err)
	}

	data := latestWriteBytesPrefix(d.queue, "bundle.cull.input:")
	if len(data) < 64 {
		t.Fatalf("missing cull input transform write, got %d bytes", len(data))
	}
	matrix := float32sFromBytes(data[:64])
	if math.Abs(float64(matrix[12]-1)) > 0.0001 || matrix[13] != 0 || matrix[14] != 0 {
		t.Fatalf("animated transform translation = (%v,%v,%v), want (1,0,0)", matrix[12], matrix[13], matrix[14])
	}
	if original[12] != 0 {
		t.Fatalf("Frame mutated caller transform slice: %#v", original)
	}
}

// oldEulerRotationMatrix composes RotX(x)·RotY(y)·RotZ(z) exactly as the
// pre-slerp native path did. Used to assert the new quaternion path is
// equivalent to the old per-axis Euler compose at keyframe endpoints.
func oldEulerRotationMatrix(x, y, z float32) mat4 {
	m := mat4Identity()
	m = mat4Mul(m, mat4RotateX(x))
	m = mat4Mul(m, mat4RotateY(y))
	m = mat4Mul(m, mat4RotateZ(z))
	return m
}

func maxMat4Diff(a, b mat4) float64 {
	var d float64
	for i := 0; i < 16; i++ {
		diff := math.Abs(float64(a[i]) - float64(b[i]))
		if diff > d {
			d = diff
		}
	}
	return d
}

// TestMat4FromQuatMatchesOldEulerCompose pins the quaternion→matrix convention
// to the existing RotX·RotY·RotZ compose. This equivalence is what guarantees
// the slerp path lands on identical endpoints at keyframes.
func TestMat4FromQuatMatchesOldEulerCompose(t *testing.T) {
	cases := [][3]float64{
		{0.3, -0.7, 1.1},
		{1.2, 0.4, -0.9},
		{math.Pi / 2, math.Pi / 2, 0},
		{-0.5, 0.0, 2.0},
		{0.1, 0.2, 0.3},
	}
	for _, c := range cases {
		old := oldEulerRotationMatrix(float32(c[0]), float32(c[1]), float32(c[2]))
		nw := mat4FromQuat(motion.QuatFromEuler(c[0], c[1], c[2]))
		if d := maxMat4Diff(old, nw); d > 1e-5 {
			t.Fatalf("mat4FromQuat(%v) diverges from RotX·RotY·RotZ by %g\n old=%v\n new=%v", c, d, old, nw)
		}
	}
}

// readbackInstanceMatrix runs a single-instance, identity-transform bundle with
// the given animation through Frame and returns the resulting 4x4 instance
// matrix as it was written to the cull input buffer (column-major float32).
func readbackInstanceMatrix(t *testing.T, anim engine.RenderAnimation, timeSeconds float64) mat4 {
	t.Helper()
	d := newFakeDevice()
	r, err := New(Config{Device: d, Surface: fakeSurface{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Destroy()

	b := engine.RenderBundle{
		Camera: engine.RenderCamera{Z: 5, FOV: 1, Near: 0.1, Far: 100},
		InstancedMeshes: []engine.RenderInstancedMesh{{
			ID:            "hero",
			Kind:          "cube",
			VertexCount:   36,
			InstanceCount: 1,
			Transforms:    identityTransform(),
		}},
		Animations: []engine.RenderAnimation{anim},
	}
	if err := r.Frame(b, 400, 300, timeSeconds); err != nil {
		t.Fatalf("Frame: %v", err)
	}
	data := latestWriteBytesPrefix(d.queue, "bundle.cull.input:")
	if len(data) < 64 {
		t.Fatalf("missing cull input transform write, got %d bytes", len(data))
	}
	fs := float32sFromBytes(data[:64])
	var m mat4
	copy(m[:], fs)
	return m
}

// rotationDualAxisClip authors a target with rotationX and rotationY channels
// sharing the same Times, each going 0 → π/2 linearly.
func rotationDualAxisClip() engine.RenderAnimation {
	return engine.RenderAnimation{
		Name:     "spin",
		Duration: 1,
		Channels: []engine.RenderAnimationChannel{
			{
				TargetID:      "hero",
				Property:      "rotationX",
				Times:         []float64{0, 1},
				Values:        []float64{0, math.Pi / 2},
				Interpolation: "LINEAR",
			},
			{
				TargetID:      "hero",
				Property:      "rotationY",
				Times:         []float64{0, 1},
				Values:        []float64{0, math.Pi / 2},
				Interpolation: "LINEAR",
			},
		},
	}
}

// TestNativeRotationSlerpsSharedTimesOrientation is the divergence test: with
// two rotation axes authored together and sampled at the midpoint, the native
// path must follow the orientation geodesic (slerp) rather than lerping each
// Euler axis independently. RED before the slerp restructure, GREEN after.
func TestNativeRotationSlerpsSharedTimesOrientation(t *testing.T) {
	m := readbackInstanceMatrix(t, rotationDualAxisClip(), 0.5)

	qLo := motion.QuatFromEuler(0, 0, 0)
	qHi := motion.QuatFromEuler(math.Pi/2, math.Pi/2, 0)
	oracle := mat4FromQuat(motion.Slerp(qLo, qHi, 0.5))

	if d := maxMat4Diff(m, oracle); d > 1e-5 {
		t.Fatalf("midpoint rotation block diverges from slerp oracle by %g\n got=%v\n oracle=%v", d, m, oracle)
	}
	// Sanity: the old per-axis Euler path would land here instead.
	oldPath := oldEulerRotationMatrix(float32(math.Pi/4), float32(math.Pi/4), 0)
	if maxMat4Diff(m, oldPath) < 1e-5 {
		t.Fatalf("midpoint matched the OLD per-axis Euler compose; slerp not applied: %v", m)
	}
}

// TestNativeRotationEndpointsMatchOldEulerCompose proves no regression at
// keyframes: at t=0 and t=1 the slerp path reproduces the old RotX·RotY·RotZ
// compose exactly (within float epsilon).
func TestNativeRotationEndpointsMatchOldEulerCompose(t *testing.T) {
	// Duration 0 disables the wrap modulo in sampleNativeAnimationClip, so the
	// last keyframe is reached by clamping at t>=Times[last] rather than
	// wrapping back to frame 0.
	clip := rotationDualAxisClip()
	clip.Duration = 0
	for _, tc := range []struct {
		name string
		time float64
		x, y float32
	}{
		{"t0", 0, 0, 0},
		{"t1", 1, float32(math.Pi / 2), float32(math.Pi / 2)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := readbackInstanceMatrix(t, clip, tc.time)
			old := oldEulerRotationMatrix(tc.x, tc.y, 0)
			if d := maxMat4Diff(m, old); d > 1e-5 {
				t.Fatalf("%s rotation block diverges from old compose by %g\n got=%v\n old=%v", tc.name, d, m, old)
			}
		})
	}
}

// TestNativeRotationFallsBackToEulerOnMismatchedTimes proves the safety branch:
// when the rotation axes do NOT share an identical Times array, the path falls
// back to per-axis Euler scalar lerp (no slerp), preserving legacy behavior.
func TestNativeRotationFallsBackToEulerOnMismatchedTimes(t *testing.T) {
	anim := engine.RenderAnimation{
		Name:     "spin",
		Duration: 1,
		Channels: []engine.RenderAnimationChannel{
			{
				TargetID:      "hero",
				Property:      "rotationX",
				Times:         []float64{0, 1},
				Values:        []float64{0, math.Pi / 2},
				Interpolation: "LINEAR",
			},
			{
				// Different Times array → must NOT slerp; fall back to Euler.
				TargetID:      "hero",
				Property:      "rotationY",
				Times:         []float64{0, 0.5, 1},
				Values:        []float64{0, math.Pi / 4, math.Pi / 2},
				Interpolation: "LINEAR",
			},
		},
	}
	m := readbackInstanceMatrix(t, anim, 0.5)
	// Per-axis Euler at t=0.5: rotX lerps 0→π/2 = π/4; rotY hits its π/4 keyframe.
	want := oldEulerRotationMatrix(float32(math.Pi/4), float32(math.Pi/4), 0)
	if d := maxMat4Diff(m, want); d > 1e-5 {
		t.Fatalf("mismatched-Times rotation did not fall back to Euler (diff %g)\n got=%v\n want=%v", d, m, want)
	}
}

func latestWriteBytesPrefix(q *fakeQueue, labelPrefix string) []byte {
	for i := len(q.writes) - 1; i >= 0; i-- {
		buffer, ok := q.writes[i].buffer.(*fakeBuffer)
		if !ok || !strings.HasPrefix(buffer.label, labelPrefix) {
			continue
		}
		return q.writes[i].data
	}
	return nil
}

func float32sFromBytes(data []byte) []float32 {
	out := make([]float32, len(data)/4)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[i*4 : i*4+4]))
	}
	return out
}
