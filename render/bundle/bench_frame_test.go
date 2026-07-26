package bundle

import (
	"fmt"
	"testing"

	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/render/gpu/headless"
)

// benchFrameBundle builds a steady-state bundle of meshCount instanced meshes,
// each with instances instances laid out on a grid. Every mesh uses a distinct
// primitive size so the primitive cache holds one entry per mesh, which matches
// a real scene with varied geometry.
func benchFrameBundle(meshCount, instances int) engine.RenderBundle {
	meshes := make([]engine.RenderInstancedMesh, meshCount)
	for m := range meshes {
		transforms := make([]float64, instances*16)
		for i := 0; i < instances; i++ {
			base := i * 16
			transforms[base+0] = 1
			transforms[base+5] = 1
			transforms[base+10] = 1
			transforms[base+15] = 1
			transforms[base+12] = float64(i%32) * 2.5
			transforms[base+13] = float64(m) * 3
			transforms[base+14] = float64(i/32) * -2.5
		}
		meshes[m] = engine.RenderInstancedMesh{
			ID:            fmt.Sprintf("mesh%d", m),
			Kind:          "cube",
			Size:          1 + float64(m)*0.25,
			MaterialIndex: 0,
			VertexCount:   36,
			InstanceCount: instances,
			Transforms:    transforms,
			CastShadow:    true,
		}
	}
	return engine.RenderBundle{
		Background: "#101018",
		// Far plane sized to the grid the bundle lays out. A far plane many
		// times the scene extent stretches the shadow cascades over empty space
		// and hides the real per-frame cost.
		Camera:    engine.RenderCamera{Z: 40, FOV: 1, Near: 0.1, Far: 120},
		Materials: []engine.RenderMaterial{{Kind: "standard", Color: "#cccccc"}},
		Lights: []engine.RenderLight{{
			Kind: "directional", Color: "#ffffff", Intensity: 1,
			DirectionX: -0.4, DirectionY: -1, DirectionZ: -0.3,
		}},
		InstancedMeshes: meshes,
	}
}

// benchFrameShadowless is benchFrame with shadow casting turned off. The
// headless device rasterizes on the CPU, so shadow-map fill dominates its
// numbers; this variant isolates the cull pass plus the main pass.
func benchFrameShadowless(b *testing.B, meshCount, instances, width, height int) {
	b.Helper()
	device, surface := headless.New(width, height)
	r, err := New(Config{Device: device, Surface: surface})
	if err != nil {
		b.Fatalf("New: %v", err)
	}
	defer r.Destroy()

	bundle := benchFrameBundle(meshCount, instances)
	for i := range bundle.InstancedMeshes {
		bundle.InstancedMeshes[i].CastShadow = false
	}
	for i := 0; i < 3; i++ {
		if err := r.Frame(bundle, width, height, float64(i)*0.016); err != nil {
			b.Fatalf("warm-up Frame: %v", err)
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := r.Frame(bundle, width, height, float64(i+3)*0.016); err != nil {
			b.Fatalf("Frame: %v", err)
		}
	}
}

// benchFrame runs the steady-state Frame loop after three warm-up frames so
// one-time resource creation stays out of the measurement.
func benchFrame(b *testing.B, meshCount, instances, width, height int) {
	b.Helper()
	device, surface := headless.New(width, height)
	r, err := New(Config{Device: device, Surface: surface})
	if err != nil {
		b.Fatalf("New: %v", err)
	}
	defer r.Destroy()

	bundle := benchFrameBundle(meshCount, instances)
	for i := 0; i < 3; i++ {
		if err := r.Frame(bundle, width, height, float64(i)*0.016); err != nil {
			b.Fatalf("warm-up Frame: %v", err)
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := r.Frame(bundle, width, height, float64(i+3)*0.016); err != nil {
			b.Fatalf("Frame: %v", err)
		}
	}
}

// benchFrameNull runs the same loop against the null device, which does no GPU
// work at all. Use it to read the renderer's own CPU cost: the headless
// software rasterizer buries it otherwise.
func benchFrameNull(b *testing.B, meshCount, instances int) {
	b.Helper()
	device := newNullDevice()
	r, err := New(Config{Device: device, Surface: fakeSurface{}})
	if err != nil {
		b.Fatalf("New: %v", err)
	}
	defer r.Destroy()

	bundle := benchFrameBundle(meshCount, instances)
	for i := 0; i < 3; i++ {
		if err := r.Frame(bundle, 64, 64, float64(i)*0.016); err != nil {
			b.Fatalf("warm-up Frame: %v", err)
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := r.Frame(bundle, 64, 64, float64(i+3)*0.016); err != nil {
			b.Fatalf("Frame: %v", err)
		}
	}
}

// benchFrameNullShadowless is benchFrameNull with shadow casting turned off.
// The difference between the two reads the cost of the per-cascade work: three
// caster cull dispatches, six uniform writes, and three shadow-pass draws per
// mesh per frame.
func benchFrameNullShadowless(b *testing.B, meshCount, instances int) {
	b.Helper()
	device := newNullDevice()
	r, err := New(Config{Device: device, Surface: fakeSurface{}})
	if err != nil {
		b.Fatalf("New: %v", err)
	}
	defer r.Destroy()

	bundle := benchFrameBundle(meshCount, instances)
	for i := range bundle.InstancedMeshes {
		bundle.InstancedMeshes[i].CastShadow = false
	}
	for i := 0; i < 3; i++ {
		if err := r.Frame(bundle, 64, 64, float64(i)*0.016); err != nil {
			b.Fatalf("warm-up Frame: %v", err)
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := r.Frame(bundle, 64, 64, float64(i+3)*0.016); err != nil {
			b.Fatalf("Frame: %v", err)
		}
	}
}

func BenchmarkFrameNullNoShadow200Mesh1Instance(b *testing.B) {
	benchFrameNullShadowless(b, 200, 1)
}
func BenchmarkFrameNullNoShadow1000Mesh1Instance(b *testing.B) {
	benchFrameNullShadowless(b, 1000, 1)
}

func BenchmarkFrameNull1Mesh1Instance(b *testing.B)     { benchFrameNull(b, 1, 1) }
func BenchmarkFrameNull1Mesh1000Instance(b *testing.B)  { benchFrameNull(b, 1, 1000) }
func BenchmarkFrameNull20Mesh500Instance(b *testing.B)  { benchFrameNull(b, 20, 500) }
func BenchmarkFrameNull1Mesh10000Instance(b *testing.B) { benchFrameNull(b, 1, 10000) }

// The next four hold the instance count at one and raise the mesh count. They
// separate the per-mesh renderer cost — cache lookups, uniform writes, cull
// dispatches, and per-cascade shadow draws — from the per-instance cost.
// Compare them against BenchmarkFrameNull1Mesh10000Instance, which holds the
// mesh count at one and raises the instance count instead.
func BenchmarkFrameNull1Mesh1InstanceEach(b *testing.B)    { benchFrameNull(b, 1, 1) }
func BenchmarkFrameNull50Mesh1InstanceEach(b *testing.B)   { benchFrameNull(b, 50, 1) }
func BenchmarkFrameNull200Mesh1InstanceEach(b *testing.B)  { benchFrameNull(b, 200, 1) }
func BenchmarkFrameNull1000Mesh1InstanceEach(b *testing.B) { benchFrameNull(b, 1000, 1) }

// These two hold the total instance count at 20000 and move it between meshes.
// If per-cascade dispatch count drives the cost, the 1000-mesh split must cost
// far more than the 1-mesh case. If per-instance work drives it, the two match.
func BenchmarkFrameNull1Mesh20000Total(b *testing.B)    { benchFrameNull(b, 1, 20000) }
func BenchmarkFrameNull1000Mesh20000Total(b *testing.B) { benchFrameNull(b, 1000, 20) }

func BenchmarkFrame1Mesh1Instance64(b *testing.B)     { benchFrame(b, 1, 1, 64, 64) }
func BenchmarkFrame1Mesh1000Instance64(b *testing.B)  { benchFrame(b, 1, 1000, 64, 64) }
func BenchmarkFrame20Mesh500Instance64(b *testing.B)  { benchFrame(b, 20, 500, 64, 64) }
func BenchmarkFrame1Mesh1Instance512(b *testing.B)    { benchFrame(b, 1, 1, 512, 512) }
func BenchmarkFrame1Mesh1000Instance512(b *testing.B) { benchFrame(b, 1, 1000, 512, 512) }
func BenchmarkFrame20Mesh500Instance512(b *testing.B) { benchFrame(b, 20, 500, 512, 512) }
func BenchmarkFrame4Mesh1000Instance64(b *testing.B)  { benchFrame(b, 4, 1000, 64, 64) }
func BenchmarkFrame1Mesh10000Instance64(b *testing.B) { benchFrame(b, 1, 10000, 64, 64) }

func BenchmarkFrameNoShadow1Mesh1000Instance64(b *testing.B) {
	benchFrameShadowless(b, 1, 1000, 64, 64)
}
func BenchmarkFrameNoShadow20Mesh500Instance64(b *testing.B) {
	benchFrameShadowless(b, 20, 500, 64, 64)
}

// BenchmarkComputeCascades isolates the per-frame cost of the cascade fit. The
// fit runs once per frame whatever the scene holds, so it shows up as a fixed
// cost that a one-mesh frame notices and a two-hundred-mesh frame does not.
// Reach for this benchmark before trading fit accuracy for speed.
func BenchmarkComputeCascades(b *testing.B) {
	cam := engine.RenderCamera{
		X: 3, Y: 6, Z: -2, RotationX: 0.4, RotationY: 1.1,
		FOV: 1, Near: 0.1, Far: 120,
	}
	light := [3]float32{-0.4, -1, -0.3}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sink = computeCascades(cam, light, defaultCascadeLambda, 16.0/9.0)
	}
}

// sink keeps the compiler from discarding the benchmark body.
var sink cascadeData
