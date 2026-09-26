# E3 — CPU-interpreter conformance for the GPU-driven kernels (repo: elio)

Depends on: E1.

## Goal

Execute both kernels on Elio's CPU interpreter (`run.Run`) and pin their
behaviour. Two kinds of test:

- A hand-built scene of eight instances whose survivor sets anyone can derive
  on paper: frustum-only, warm/next-frame/cold two-phase occlusion, a shadow
  light view, distance, contribution, and the shear/orthogonal radius rule.
- A seeded 800-instance scene where the interpreter must agree exactly with
  `gdReference`, a direct Go transcription of the kernel, for every phase.

This is the GPU-free oracle the GoSX side relies on. These exact expectations
also held on real WebGPU (SwiftShader) with 0 mismatches against the
interpreter.

## Memory conventions of `run.Run` (why the fixtures look the way they do)

- Uniform blocks: `map[string]any`. Storage arrays of structs: `[]any` of
  `map[string]any`.
- `f32` vectors: `[]float64`. `u32` vectors (`vec4u`): `[]int64`. `u32`
  scalars: `int64`. Matrices: `run.Mat{Cols: 4, Rows: 4, E: columnMajor}`.
- Atomic and scalar storage arrays (`[]atomic_u32`, `[]u32`, `[]f32`):
  `[]float64`, mutated in place.
- Invocations run sequentially, so atomics are deterministic and survivor
  order equals instance order. Tests still sort survivor lists, because GPU
  order is not deterministic.

## File to create: `stdlib/gpudriven_test.go`

```go
package stdlib

import (
	"math"
	"math/rand"
	"reflect"
	"sort"
	"testing"

	"m31labs.dev/elio/ir"
	"m31labs.dev/elio/run"
)

// ---- shared fixtures ------------------------------------------------------

// gdMat4 is a column-major 4x4 matrix, the layout WGSL mat4x4<f32> uses.
type gdMat4 [16]float64

func gdMul(a, b gdMat4) gdMat4 {
	var o gdMat4
	for c := 0; c < 4; c++ {
		for r := 0; r < 4; r++ {
			s := 0.0
			for k := 0; k < 4; k++ {
				s += a[k*4+r] * b[c*4+k]
			}
			o[c*4+r] = s
		}
	}
	return o
}

// gdPerspective builds the WebGPU (z in [0,1]) projection Scene3D uses: the
// GL perspective matrix with row 2 remapped to 0.5*(row2+row3).
func gdPerspective(fovYDeg, aspect, near, far float64) gdMat4 {
	f := 1 / math.Tan(fovYDeg*math.Pi/360)
	var p gdMat4
	p[0] = f / aspect
	p[5] = f
	p[10] = (far + near) / (near - far)
	p[11] = -1
	p[14] = 2 * far * near / (near - far)
	for _, c := range []int{0, 1, 2, 3} {
		p[c*4+2] = 0.5 * (p[c*4+2] + p[c*4+3])
	}
	return p
}

func gdOrtho(l, r, b, t, n, f float64) gdMat4 {
	var p gdMat4
	p[0] = 2 / (r - l)
	p[5] = 2 / (t - b)
	p[10] = -1 / (f - n)
	p[12] = -(r + l) / (r - l)
	p[13] = -(t + b) / (t - b)
	p[14] = -n / (f - n)
	p[15] = 1
	return p
}

func gdLookAt(eye, target, up [3]float64) gdMat4 {
	sub := func(a, b [3]float64) [3]float64 { return [3]float64{a[0] - b[0], a[1] - b[1], a[2] - b[2]} }
	dot := func(a, b [3]float64) float64 { return a[0]*b[0] + a[1]*b[1] + a[2]*b[2] }
	cross := func(a, b [3]float64) [3]float64 {
		return [3]float64{a[1]*b[2] - a[2]*b[1], a[2]*b[0] - a[0]*b[2], a[0]*b[1] - a[1]*b[0]}
	}
	norm := func(a [3]float64) [3]float64 {
		l := math.Sqrt(dot(a, a))
		return [3]float64{a[0] / l, a[1] / l, a[2] / l}
	}
	z := norm(sub(eye, target))
	x := norm(cross(up, z))
	y := cross(z, x)
	return gdMat4{x[0], y[0], z[0], 0, x[1], y[1], z[1], 0, x[2], y[2], z[2], 0, -dot(x, eye), -dot(y, eye), -dot(z, eye), 1}
}

// gdPlanes is extractFrustumPlanesJS (client/js/bootstrap-src/11-scene-math.ts):
// Gribb-Hartmann on a WebGPU view-projection, near plane = row 2.
func gdPlanes(vp gdMat4) []any {
	row := func(r int) [4]float64 { return [4]float64{vp[r], vp[4+r], vp[8+r], vp[12+r]} }
	r0, r1, r2, r3 := row(0), row(1), row(2), row(3)
	n := func(p [4]float64) []float64 {
		l := math.Sqrt(p[0]*p[0] + p[1]*p[1] + p[2]*p[2])
		return []float64{p[0] / l, p[1] / l, p[2] / l, p[3] / l}
	}
	add := func(a, b [4]float64) [4]float64 {
		return [4]float64{a[0] + b[0], a[1] + b[1], a[2] + b[2], a[3] + b[3]}
	}
	sub := func(a, b [4]float64) [4]float64 {
		return [4]float64{a[0] - b[0], a[1] - b[1], a[2] - b[2], a[3] - b[3]}
	}
	return []any{n(add(r3, r0)), n(sub(r3, r0)), n(add(r3, r1)), n(sub(r3, r1)), n(r2), n(sub(r3, r2))}
}

type gdMeshFixture struct {
	radius     float64
	castShadow int64
	vertex     int64
}

type gdInstanceFixture struct {
	mesh  int
	model gdMat4
}

func gdTranslate(x, y, z, s float64) gdMat4 {
	return gdMat4{s, 0, 0, 0, 0, s, 0, 0, 0, 0, s, 0, x, y, z, 1}
}

// gdHiZ describes a packed pyramid: level k occupies hzb[offset:offset+w*h].
type gdHiZLevel struct{ offset, width, height int }

func gdHiZLevels(w, h int) ([]gdHiZLevel, int) {
	var levels []gdHiZLevel
	lw, lh, off := (w+1)/2, (h+1)/2, 0
	for {
		levels = append(levels, gdHiZLevel{off, lw, lh})
		off += lw * lh
		if (lw == 1 && lh == 1) || len(levels) == 16 {
			break
		}
		lw, lh = (lw+1)/2, (lh+1)/2
	}
	return levels, off
}

type gdScene struct {
	meshes    []gdMeshFixture
	instances []gdInstanceFixture
	width     float64
	height    float64
	eye       [3]float64
}

// gdRun executes the cull kernel for one view and returns the survivor lists
// per (mesh, slot), each sorted ascending, plus the visibility array.
type gdViewInput struct {
	vp          gdMat4
	phase, slot int64
	levels      []gdHiZLevel // nil = occlusion off
	maxDistance float64
	minPixel    float64
}

type gdState struct {
	args       []float64
	visible    []float64
	visibility []float64
	hzb        []float64
	listBase   []int
	count      []int
	first      []int
}

func gdNewState(t *testing.T, s gdScene, visibilityInit float64, hzb []float64) *gdState {
	t.Helper()
	st := &gdState{hzb: hzb}
	if st.hzb == nil {
		st.hzb = []float64{0}
	}
	st.count = make([]int, len(s.meshes))
	for _, in := range s.instances {
		st.count[in.mesh]++
	}
	st.first = make([]int, len(s.meshes))
	st.listBase = make([]int, len(s.meshes))
	first, list := 0, 0
	for m := range s.meshes {
		st.first[m] = first
		st.listBase[m] = list
		first += st.count[m]
		list += st.count[m] * 4
	}
	st.args = make([]float64, len(s.meshes)*16)
	for m, mesh := range s.meshes {
		for slot := 0; slot < 4; slot++ {
			st.args[(m*4+slot)*4] = float64(mesh.vertex)
		}
	}
	st.visible = make([]float64, list)
	st.visibility = make([]float64, len(s.instances))
	for i := range st.visibility {
		st.visibility[i] = visibilityInit
	}
	return st
}

// gdCheckSorted asserts instances are grouped by mesh in ascending mesh order,
// which is the packing contract GoSX uses (firstInstance prefix sums).
func gdCheckSorted(t *testing.T, s gdScene) {
	t.Helper()
	for i := 1; i < len(s.instances); i++ {
		if s.instances[i].mesh < s.instances[i-1].mesh {
			t.Fatal("fixture instances must be grouped by ascending mesh index")
		}
	}
}

func gdRunView(t *testing.T, mod *ir.Module, s gdScene, st *gdState, v gdViewInput) {
	t.Helper()
	gdCheckSorted(t, s)
	instances := make([]any, len(s.instances))
	for i, in := range s.instances {
		e := make([]float64, 16)
		copy(e, in.model[:])
		instances[i] = map[string]any{
			"model": run.Mat{Cols: 4, Rows: 4, E: e}, "color": []float64{1, 1, 1, 1},
			"meshIndex": int64(in.mesh), "pickId": int64(0), "_pad0": int64(0), "_pad1": int64(0),
		}
	}
	meshes := make([]any, len(s.meshes))
	for m, mesh := range s.meshes {
		meshes[m] = map[string]any{
			"sphere": []float64{0, 0, 0, mesh.radius}, "firstInstance": int64(st.first[m]),
			"instanceCount": int64(st.count[m]), "argsBase": int64(m * 4), "listBase": int64(st.listBase[m]),
			"castShadow": mesh.castShadow, "vertexCount": mesh.vertex, "_pad0": int64(0), "_pad1": int64(0),
		}
	}
	levels := make([]any, 16)
	for k := range levels {
		levels[k] = []int64{0, 0, 0, 0}
	}
	for k, l := range v.levels {
		levels[k] = []int64{int64(l.offset), int64(l.width), int64(l.height), 0}
	}
	e := make([]float64, 16)
	copy(e, v.vp[:])
	view := map[string]any{
		"viewProj": run.Mat{Cols: 4, Rows: 4, E: e}, "planes": gdPlanes(v.vp),
		"viewport":  []float64{s.width, s.height, 1 / s.width, 1 / s.height},
		"eye":       []float64{s.eye[0], s.eye[1], s.eye[2], v.maxDistance},
		"params":    []float64{v.minPixel, 0, 0, 0},
		"control":   []int64{int64(len(s.instances)), v.slot, v.phase, int64(len(v.levels))},
		"hzbLevels": levels,
	}
	mem := &run.Memory{Vars: map[string]any{
		"gdView": view, "gdInstances": instances, "gdMeshes": meshes, "gdArgs": st.args,
		"gdVisible": st.visible, "gdVisibility": st.visibility, "gdHzb": st.hzb,
	}}
	if err := run.Run(mod, "cull", len(s.instances), mem); err != nil {
		t.Fatalf("run cull: %v", err)
	}
}

// gdList returns the sorted survivor list for (mesh, slot).
func gdList(st *gdState, mesh, slot int) []int {
	n := int(st.args[(mesh*4+slot)*4+1])
	out := make([]int, n)
	for k := 0; k < n; k++ {
		out[k] = int(st.visible[st.listBase[mesh]+slot*st.count[mesh]+k])
	}
	sort.Ints(out)
	return out
}

func gdMustCull(t *testing.T) *ir.Module {
	t.Helper()
	mod, err := GPUDrivenCull()
	if err != nil {
		t.Fatal(err)
	}
	return mod
}

// ---- tests ----------------------------------------------------------------

// TestGPUDrivenHiZDownsampleClampsOddEdges builds a 5x3 level and reduces it
// to 3x2. The right column and bottom row reuse the edge texel instead of
// reading past the level, so every destination texel is exactly the max of
// the source texels it covers.
func TestGPUDrivenHiZDownsampleClampsOddEdges(t *testing.T) {
	mod, err := GPUDrivenHiZDownsample()
	if err != nil {
		t.Fatal(err)
	}
	src := []float64{
		0.10, 0.20, 0.30, 0.40, 0.95,
		0.50, 0.60, 0.70, 0.80, 0.15,
		0.25, 0.35, 0.45, 0.55, 0.65,
	}
	hzb := make([]float64, len(src)+6)
	copy(hzb, src)
	level := map[string]any{
		"srcOffset": int64(0), "srcWidth": int64(5), "srcHeight": int64(3),
		"dstOffset": int64(15), "dstWidth": int64(3), "dstHeight": int64(2),
		"_pad0": int64(0), "_pad1": int64(0),
	}
	mem := &run.Memory{Vars: map[string]any{"gdLevel": level, "gdHzb": hzb}}
	if err := run.Run(mod, "downsample", 6, mem); err != nil {
		t.Fatalf("run: %v", err)
	}
	want := []float64{0.60, 0.80, 0.95, 0.35, 0.55, 0.65}
	if got := hzb[15:]; !reflect.DeepEqual(got, want) {
		t.Fatalf("dst = %v, want %v", got, want)
	}
}

// gdHandScene is small enough to reason about by hand. The camera sits at
// z=10 looking at the origin with a 90 degree field of view over a 64x64
// target. Mesh 0 casts shadows; mesh 1 does not.
func gdHandScene() gdScene {
	return gdScene{
		meshes: []gdMeshFixture{{radius: 0.5, castShadow: 1, vertex: 36}, {radius: 0.5, castShadow: 0, vertex: 24}},
		instances: []gdInstanceFixture{
			{0, gdTranslate(0, 0, 0, 1)},    // 0: centre of view
			{0, gdTranslate(0, 0, 20, 1)},   // 1: behind the camera
			{0, gdTranslate(100, 0, 0, 1)},  // 2: far off to the side
			{0, gdTranslate(0, 0, -200, 1)}, // 3: past the far plane
			{0, gdTranslate(3, 0, 0, 1)},    // 4: visible, right of centre
			{0, gdTranslate(0, 0, -5, 1)},   // 5: visible, behind the wall below
			{0, gdTranslate(0, 0, 8, 1)},    // 6: close to the camera
			{1, gdTranslate(-3, 0, 0, 1)},   // 7: visible, non-caster mesh
		},
		width: 64, height: 64, eye: [3]float64{0, 0, 10},
	}
}

func gdHandCamera() gdMat4 {
	return gdMul(gdPerspective(90, 1, 0.1, 100), gdLookAt([3]float64{0, 0, 10}, [3]float64{0, 0, 0}, [3]float64{0, 1, 0}))
}

// gdWallPyramid fills every level with the depth of the plane z = -2, as if
// a wall spanning the whole view stood there.
func gdWallPyramid(vp gdMat4, w, h int) ([]gdHiZLevel, []float64) {
	levels, total := gdHiZLevels(w, h)
	clipZ := vp[2]*0 + vp[6]*0 + vp[10]*(-2) + vp[14]
	clipW := vp[3]*0 + vp[7]*0 + vp[11]*(-2) + vp[15]
	depth := clipZ / clipW
	hzb := make([]float64, total)
	for i := range hzb {
		hzb[i] = depth
	}
	return levels, hzb
}

func TestGPUDrivenCullFrustumOnly(t *testing.T) {
	mod := gdMustCull(t)
	s := gdHandScene()
	st := gdNewState(t, s, 1, nil)
	gdRunView(t, mod, s, st, gdViewInput{vp: gdHandCamera(), phase: 0, slot: 0})
	if got, want := gdList(st, 0, 0), []int{0, 4, 5, 6}; !reflect.DeepEqual(got, want) {
		t.Fatalf("mesh 0 slot 0 = %v, want %v", got, want)
	}
	if got, want := gdList(st, 1, 0), []int{7}; !reflect.DeepEqual(got, want) {
		t.Fatalf("mesh 1 slot 0 = %v, want %v", got, want)
	}
	for slot := 1; slot < 4; slot++ {
		if n := st.args[(0*4+slot)*4+1]; n != 0 {
			t.Fatalf("slot %d instanceCount = %v, want 0 (single view must not touch other slots)", slot, n)
		}
	}
	if st.args[0] != 36 || st.args[16] != 24 {
		t.Fatalf("vertexCount lanes changed: %v", st.args)
	}
}

func TestGPUDrivenCullTwoPhaseOcclusion(t *testing.T) {
	mod := gdMustCull(t)
	s := gdHandScene()
	vp := gdHandCamera()
	levels, hzb := gdWallPyramid(vp, 64, 64)

	// Warm start: everything was visible last frame, so the early phase draws
	// every in-frustum instance and the late phase finds nothing new. The late
	// phase clears the visibility of the instance behind the wall.
	st := gdNewState(t, s, 1, hzb)
	gdRunView(t, mod, s, st, gdViewInput{vp: vp, phase: 0, slot: 0, levels: levels})
	gdRunView(t, mod, s, st, gdViewInput{vp: vp, phase: 1, slot: 1, levels: levels})
	if got, want := gdList(st, 0, 0), []int{0, 4, 5, 6}; !reflect.DeepEqual(got, want) {
		t.Fatalf("warm early = %v, want %v", got, want)
	}
	if got := gdList(st, 0, 1); len(got) != 0 {
		t.Fatalf("warm late = %v, want empty", got)
	}
	wantVis := []float64{1, 0, 0, 0, 1, 0, 1, 1}
	if !reflect.DeepEqual(st.visibility, wantVis) {
		t.Fatalf("visibility = %v, want %v", st.visibility, wantVis)
	}

	// Next frame: the early phase now skips instance 5, and the late phase
	// keeps it culled because the wall still hides it.
	next := gdNewState(t, s, 0, hzb)
	copy(next.visibility, st.visibility)
	gdRunView(t, mod, s, next, gdViewInput{vp: vp, phase: 0, slot: 0, levels: levels})
	gdRunView(t, mod, s, next, gdViewInput{vp: vp, phase: 1, slot: 1, levels: levels})
	if got, want := gdList(next, 0, 0), []int{0, 4, 6}; !reflect.DeepEqual(got, want) {
		t.Fatalf("second early = %v, want %v", got, want)
	}
	if got := gdList(next, 0, 1); len(got) != 0 {
		t.Fatalf("second late = %v, want empty", got)
	}

	// Cold start: nothing was visible last frame. The early phase draws
	// nothing; the late phase draws exactly the unoccluded instances.
	cold := gdNewState(t, s, 0, hzb)
	gdRunView(t, mod, s, cold, gdViewInput{vp: vp, phase: 0, slot: 0, levels: levels})
	gdRunView(t, mod, s, cold, gdViewInput{vp: vp, phase: 1, slot: 1, levels: levels})
	if got := gdList(cold, 0, 0); len(got) != 0 {
		t.Fatalf("cold early = %v, want empty", got)
	}
	if got, want := gdList(cold, 0, 1), []int{0, 4, 6}; !reflect.DeepEqual(got, want) {
		t.Fatalf("cold late mesh 0 = %v, want %v", got, want)
	}
	if got, want := gdList(cold, 1, 1), []int{7}; !reflect.DeepEqual(got, want) {
		t.Fatalf("cold late mesh 1 = %v, want %v", got, want)
	}
}

func TestGPUDrivenCullLightView(t *testing.T) {
	mod := gdMustCull(t)
	s := gdHandScene()
	light := gdMul(gdOrtho(-10, 10, -10, 10, 0.1, 100), gdLookAt([3]float64{0, 50, 0}, [3]float64{0, 0, 0}, [3]float64{0, 0, -1}))
	st := gdNewState(t, s, 1, nil)
	gdRunView(t, mod, s, st, gdViewInput{vp: light, phase: 2, slot: 3})
	if got, want := gdList(st, 0, 3), []int{0, 4, 5, 6}; !reflect.DeepEqual(got, want) {
		t.Fatalf("light slot 3 mesh 0 = %v, want %v", got, want)
	}
	if got := gdList(st, 1, 3); len(got) != 0 {
		t.Fatalf("non-caster mesh drew into the light view: %v", got)
	}
	if !reflect.DeepEqual(st.visibility, []float64{1, 1, 1, 1, 1, 1, 1, 1}) {
		t.Fatalf("a light view must not touch visibility: %v", st.visibility)
	}
}

func TestGPUDrivenCullDistanceAndContribution(t *testing.T) {
	mod := gdMustCull(t)
	s := gdHandScene()
	st := gdNewState(t, s, 1, nil)
	gdRunView(t, mod, s, st, gdViewInput{vp: gdHandCamera(), phase: 0, slot: 0, maxDistance: 9})
	if got, want := gdList(st, 0, 0), []int{6}; !reflect.DeepEqual(got, want) {
		t.Fatalf("maxDistance 9 = %v, want %v", got, want)
	}
	st = gdNewState(t, s, 1, nil)
	gdRunView(t, mod, s, st, gdViewInput{vp: gdHandCamera(), phase: 0, slot: 0, minPixel: 6})
	if got, want := gdList(st, 0, 0), []int{6}; !reflect.DeepEqual(got, want) {
		t.Fatalf("minPixel 6 = %v, want %v", got, want)
	}
}

// TestGPUDrivenCullMatchesReference runs a seeded random scene through the
// interpreter and through gdReference, a straight Go transcription of the
// kernel, for every phase. They must agree exactly.
func TestGPUDrivenCullMatchesReference(t *testing.T) {
	mod := gdMustCull(t)
	rnd := rand.New(rand.NewSource(7))
	s := gdScene{
		meshes: []gdMeshFixture{{radius: 0.87, castShadow: 1, vertex: 36}, {radius: 1, castShadow: 0, vertex: 96}},
		width:  96, height: 64, eye: [3]float64{0, 8, 40},
	}
	for m := 0; m < 2; m++ {
		for i := 0; i < 400; i++ {
			sc := 0.5 + rnd.Float64()*1.5
			model := gdTranslate((rnd.Float64()-0.5)*120, rnd.Float64()*4, (rnd.Float64()-0.5)*120, sc)
			s.instances = append(s.instances, gdInstanceFixture{m, model})
		}
	}
	vp := gdMul(gdPerspective(60, 96.0/64.0, 0.1, 300), gdLookAt(s.eye, [3]float64{0, 1, 0}, [3]float64{0, 1, 0}))
	levels, total := gdHiZLevels(96, 64)
	hzb := make([]float64, total)
	for i := range hzb {
		hzb[i] = 0.985 + 0.01*rnd.Float64()
	}
	for _, tc := range []gdViewInput{
		{vp: vp, phase: 0, slot: 0},
		{vp: vp, phase: 0, slot: 0, levels: levels},
		{vp: vp, phase: 1, slot: 1, levels: levels},
		{vp: vp, phase: 0, slot: 0, maxDistance: 45, minPixel: 3},
		{vp: gdMul(gdOrtho(-60, 60, -60, 60, 0.1, 200), gdLookAt([3]float64{20, 90, 20}, [3]float64{0, 0, 0}, [3]float64{0, 0, -1})), phase: 2, slot: 2},
	} {
		for _, visInit := range []float64{0, 1} {
			st := gdNewState(t, s, visInit, append([]float64(nil), hzb...))
			gdRunView(t, mod, s, st, tc)
			want := gdReference(s, tc, visInit, hzb)
			for m := range s.meshes {
				if got := gdList(st, m, int(tc.slot)); !reflect.DeepEqual(got, want[m]) {
					t.Fatalf("phase %d slot %d vis %v mesh %d: interpreter %v != reference %v", tc.phase, tc.slot, visInit, m, got, want[m])
				}
			}
		}
	}
}

// gdReference is the kernel in plain Go (float64), used only as a test oracle.
func gdReference(s gdScene, v gdViewInput, visInit float64, hzb []float64) [][]int {
	out := make([][]int, len(s.meshes))
	for m := range out {
		out[m] = []int{}
	}
	planes := gdPlanes(v.vp)
	for i, in := range s.instances {
		mesh := s.meshes[in.mesh]
		mm := in.model
		c := [3]float64{mm[12], mm[13], mm[14]}
		col := func(k int) [3]float64 { return [3]float64{mm[k*4], mm[k*4+1], mm[k*4+2]} }
		dot3 := func(a, b [3]float64) float64 { return a[0]*b[0] + a[1]*b[1] + a[2]*b[2] }
		c0, c1, c2 := col(0), col(1), col(2)
		l0, l1, l2 := dot3(c0, c0), dot3(c1, c1), dot3(c2, c2)
		d01, d02, d12 := dot3(c0, c1), dot3(c0, c2), dot3(c1, c2)
		scale2 := l0 + l1 + l2
		if d01*d01 <= 0.00000001*l0*l1 && d02*d02 <= 0.00000001*l0*l2 && d12*d12 <= 0.00000001*l1*l2 {
			scale2 = math.Max(l0, math.Max(l1, l2))
		}
		scale := math.Sqrt(scale2)
		r := mesh.radius
		if scale > 0 {
			r = mesh.radius * scale
		}
		keep := true
		if v.phase == 2 && mesh.castShadow == 0 {
			keep = false
		}
		for _, p := range planes {
			pl := p.([]float64)
			if pl[0]*c[0]+pl[1]*c[1]+pl[2]*c[2]+pl[3] < -r {
				keep = false
				break
			}
		}
		if v.phase < 2 {
			if keep && v.maxDistance > 0 {
				d := math.Sqrt((c[0]-s.eye[0])*(c[0]-s.eye[0]) + (c[1]-s.eye[1])*(c[1]-s.eye[1]) + (c[2]-s.eye[2])*(c[2]-s.eye[2]))
				if d-r > v.maxDistance {
					keep = false
				}
			}
			testRect := keep && (v.minPixel > 0 || (v.phase == 1 && len(v.levels) > 0))
			if testRect {
				behind, first := false, true
				var minX, minY, maxX, maxY, minZ float64
				for k := 0; k < 8; k++ {
					sx, sy, sz := -1.0, -1.0, -1.0
					if k%2 == 1 {
						sx = 1
					}
					if (k/2)%2 == 1 {
						sy = 1
					}
					if (k/4)%2 == 1 {
						sz = 1
					}
					x, y, z := c[0]+sx*r, c[1]+sy*r, c[2]+sz*r
					vp := v.vp
					cw := vp[3]*x + vp[7]*y + vp[11]*z + vp[15]
					if cw <= 0.0001 {
						behind = true
						continue
					}
					nx := (vp[0]*x + vp[4]*y + vp[8]*z + vp[12]) / cw
					ny := (vp[1]*x + vp[5]*y + vp[9]*z + vp[13]) / cw
					nz := (vp[2]*x + vp[6]*y + vp[10]*z + vp[14]) / cw
					if first {
						minX, maxX, minY, maxY, minZ, first = nx, nx, ny, ny, nz, false
					} else {
						minX, maxX = math.Min(minX, nx), math.Max(maxX, nx)
						minY, maxY = math.Min(minY, ny), math.Max(maxY, ny)
						minZ = math.Min(minZ, nz)
					}
				}
				if !behind {
					clamp := func(a, lo, hi float64) float64 { return math.Min(math.Max(a, lo), hi) }
					w, h := s.width, s.height
					x0, x1 := clamp((minX*0.5+0.5)*w, 0, w), clamp((maxX*0.5+0.5)*w, 0, w)
					y0, y1 := clamp((0.5-maxY*0.5)*h, 0, h), clamp((0.5-minY*0.5)*h, 0, h)
					extent := math.Max(x1-x0, y1-y0)
					if v.minPixel > 0 && extent < v.minPixel {
						keep = false
					}
					if v.phase == 1 && len(v.levels) > 0 && keep {
						level := int(math.Max(math.Ceil(math.Log2(math.Max(extent, 1)))-1, 0))
						if level >= len(v.levels) {
							level = len(v.levels) - 1
						}
						lv := v.levels[level]
						texel := math.Exp2(float64(level) + 1)
						tx0, tx1 := min(int(x0/texel), lv.width-1), min(int(x1/texel), lv.width-1)
						ty0, ty1 := min(int(y0/texel), lv.height-1), min(int(y1/texel), lv.height-1)
						far := math.Max(math.Max(hzb[lv.offset+ty0*lv.width+tx0], hzb[lv.offset+ty0*lv.width+tx1]),
							math.Max(hzb[lv.offset+ty1*lv.width+tx0], hzb[lv.offset+ty1*lv.width+tx1]))
						if minZ > far {
							keep = false
						}
					}
				}
			}
		}
		if v.phase == 1 {
			if visInit == 1 {
				keep = false
			}
		}
		if v.phase == 0 && len(v.levels) > 0 && visInit == 0 {
			keep = false
		}
		if keep {
			out[in.mesh] = append(out[in.mesh], i)
		}
	}
	return out
}

// TestGPUDrivenCullShearFallsBackToFrobenius pins the scale rule. A sheared
// basis has non-orthogonal columns, so the kernel must use the Frobenius
// bound; a rotated and uniformly scaled basis uses the exact column length.
func TestGPUDrivenCullShearFallsBackToFrobenius(t *testing.T) {
	mod := gdMustCull(t)
	// Box frustum x in [-10, 10] via an orthographic view along -z.
	vp := gdMul(gdOrtho(-10, 10, -10, 10, 0.1, 100), gdLookAt([3]float64{0, 0, 50}, [3]float64{0, 0, 0}, [3]float64{0, 1, 0}))
	sheared := gdMat4{1, 0, 0, 0, 1, 1, 0, 0, 0, 0, 1, 0, 11.2, 0, 0, 1}  // x += y shear
	rotated := gdMat4{0, 1, 0, 0, -1, 0, 0, 0, 0, 0, 1, 0, 11.2, 0, 0, 1} // 90 degree turn about z
	s := gdScene{
		meshes:    []gdMeshFixture{{radius: 1, castShadow: 1, vertex: 36}},
		instances: []gdInstanceFixture{{0, sheared}, {0, rotated}},
		width:     64, height: 64, eye: [3]float64{0, 0, 50},
	}
	st := gdNewState(t, s, 1, nil)
	gdRunView(t, mod, s, st, gdViewInput{vp: vp, phase: 0, slot: 0})
	// Centre x = 11.2 sits 1.2 past the plane x = 10. The sheared instance's
	// Frobenius radius is sqrt(4) = 2 > 1.2, so it survives; the rotated one's
	// exact radius is 1 < 1.2, so it is culled.
	if got, want := gdList(st, 0, 0), []int{0}; !reflect.DeepEqual(got, want) {
		t.Fatalf("survivors = %v, want %v", got, want)
	}
}
```

## Expected results (derivations, so a failure can be diagnosed)

The camera sits at z=10 looking at the origin, with a 90° FOV, a 64×64 target,
and near 0.1 / far 100. All instances use identity rotation and scale 1, so the
radius is exactly 0.5; for an identity basis the orthogonal rule gives scale 1,
not √3.

| Instance | Where | Frustum | Behind wall z=-2 | Light view (ortho ±10 from above) | Distance ≤ 9 | Extent ≥ 6 px |
|---|---|---|---|---|---|---|
| 0 | (0,0,0) | in | no | in | 9.5 → out | ≈3.4 px → out |
| 1 | (0,0,20) | behind camera → out | — | z=20 → out | — | — |
| 2 | (100,0,0) | out | — | out | — | — |
| 3 | (0,0,-200) | past far → out | — | out | — | — |
| 4 | (3,0,0) | in | no | in | out | out |
| 5 | (0,0,-5) | in | **yes** | in | out | out |
| 6 | (0,0,8) | in | no | in | 1.5 → in | ≈21 px → in |
| 7 (mesh 1, no shadow) | (-3,0,0) | in | no | caster filter → out | out | out |

## Verify

```sh
gofmt -l stdlib       # nothing for the new file
go vet ./stdlib
go test ./stdlib -run 'GPUDriven' -count=1 -v
# PASS: HiZDownsampleClampsOddEdges, CullFrustumOnly, CullTwoPhaseOcclusion,
#       CullLightView, CullDistanceAndContribution, CullMatchesReference,
#       CullShearFallsBackToFrobenius (+ E1/E2 tests)
go test ./...
```

## Commit (elio)

`test(stdlib): execute gpu-driven kernels on the cpu interpreter`
