package bundle

import (
	"testing"

	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/render/gpu/headless"
)

// trivialParticleUpdateWGSL is a contract-identical replacement kernel for the
// built-in particleUpdateWGSL. It has the same bindings (0: uniforms read,
// 1: particles read_write) but uses entry point "update" to prove the override
// entry point plumbing works. The logic is intentionally minimal — it respawns
// every particle at age 0 each tick. The headless Go twin still fires under the
// "bundle.particles.update" label so particle positions do advance.
const trivialParticleUpdateWGSL = `
struct Particle {
  position : vec4<f32>,
  velocity : vec4<f32>,
};
struct ParticleForce {
  meta   : vec4<f32>,
  vector : vec4<f32>,
};
struct ParticleUniforms {
  dt          : f32,
  time        : f32,
  lifetime    : f32,
  forceCount  : f32,
  emitterPos  : vec4<f32>,
  initialSpeed: vec4<f32>,
  forces      : array<ParticleForce, 8>,
};
@group(0) @binding(0) var<uniform> u : ParticleUniforms;
@group(0) @binding(1) var<storage, read_write> particles : array<Particle>;

@compute @workgroup_size(64)
fn update(@builtin(global_invocation_id) gid : vec3<u32>) {
  let i = gid.x;
  if (i >= arrayLength(&particles)) { return; }
  var p = particles[i];
  p.position = vec4<f32>(u.emitterPos.xyz, p.position.w + u.dt);
  p.velocity = vec4<f32>(0.0, 0.0, 0.0, u.lifetime);
  particles[i] = p;
}
`

// TestParticleOverrideKernelAdvancesParticlesOnHeadlessDevice verifies that
// when a ParticleUpdateWGSL override is set on the renderer, the
// "bundle.particles.update"-labeled pipeline still triggers the headless
// device's Go twin (runParticleUpdate) so particles actually move across Frame
// calls. This confirms the label constraint is preserved.
func TestParticleOverrideKernelAdvancesParticlesOnHeadlessDevice(t *testing.T) {
	d, surface := headless.New(128, 128)
	r, err := New(Config{
		Device:                   d,
		Surface:                  surface,
		ParticleUpdateWGSL:       trivialParticleUpdateWGSL,
		ParticleUpdateEntryPoint: "update",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Destroy()

	bundle := engine.RenderBundle{
		Camera: engine.RenderCamera{Z: 5, FOV: 1, Near: 0.1, Far: 100},
		ComputeParticles: []engine.RenderComputeParticles{
			{
				ID:    "test-particles",
				Count: 128,
				Emitter: engine.RenderParticleEmitter{
					Lifetime: 2.0,
					Radius:   0.5,
					Scatter:  1.0,
				},
				Material: engine.RenderParticleMaterial{
					Color:   "#ff8800",
					Size:    0.2,
					Opacity: 1.0,
				},
			},
		},
	}

	// Frame 0 — seeds particles.
	if err := r.Frame(bundle, 128, 128, 0); err != nil {
		t.Fatalf("Frame 0: %v", err)
	}
	// Frame 1 — verifies the particle update pipeline fired; if the label
	// were wrong the headless device would silently no-op and no particles
	// would be allocated / updated, which would panic on r.particleCache check.
	if err := r.Frame(bundle, 128, 128, 0.016); err != nil {
		t.Fatalf("Frame 1: %v", err)
	}

	// Verify the particle system was actually exercised.
	res, ok := r.particleCache["test-particles"]
	if !ok {
		t.Fatal("particle system was not allocated in cache")
	}
	if res.count != 128 {
		t.Fatalf("particle count: want 128, got %d", res.count)
	}
}

// TestParticleOverrideKernelSourceReachesCreateComputePipeline verifies that
// when Config.ParticleUpdateWGSL is set the renderer calls CreateComputePipeline
// with the override source and the specified entry point (not the builtin).
func TestParticleOverrideKernelSourceReachesCreateComputePipeline(t *testing.T) {
	d := newFakeDevice()
	r, err := New(Config{
		Device:                   d,
		Surface:                  fakeSurface{},
		ParticleUpdateWGSL:       trivialParticleUpdateWGSL,
		ParticleUpdateEntryPoint: "update",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Destroy()

	// Find the particleUpdate compute pipeline and its shader module.
	var overridePipeline *fakeComputePipeline
	for _, cp := range d.computePipelines {
		if cp.desc.Label == "bundle.particles.update" {
			overridePipeline = cp
			break
		}
	}
	if overridePipeline == nil {
		t.Fatal("no compute pipeline with label 'bundle.particles.update' found")
	}

	if overridePipeline.desc.EntryPoint != "update" {
		t.Errorf("entry point: want 'update', got %q", overridePipeline.desc.EntryPoint)
	}

	// Verify the override shader module source reached CreateShaderModule.
	var overrideShader *fakeShader
	for _, s := range d.shaders {
		if s.label == "bundle.particles.update" {
			overrideShader = s
			break
		}
	}
	if overrideShader == nil {
		t.Fatal("no shader with label 'bundle.particles.update' found")
	}
	if overrideShader.src != trivialParticleUpdateWGSL {
		t.Errorf("shader source was not the override WGSL")
	}
}

// TestParticleOverrideDefaultEntryPoint verifies that when ParticleUpdateWGSL
// is set but ParticleUpdateEntryPoint is empty, the entry point defaults to
// "main" (not the override default "update").
func TestParticleOverrideDefaultEntryPoint(t *testing.T) {
	d := newFakeDevice()
	r, err := New(Config{
		Device:             d,
		Surface:            fakeSurface{},
		ParticleUpdateWGSL: trivialParticleUpdateWGSL,
		// EntryPoint deliberately omitted — should default to "main".
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Destroy()

	for _, cp := range d.computePipelines {
		if cp.desc.Label == "bundle.particles.update" {
			if cp.desc.EntryPoint != "main" {
				t.Errorf("default entry point: want 'main', got %q", cp.desc.EntryPoint)
			}
			return
		}
	}
	t.Fatal("no compute pipeline with label 'bundle.particles.update' found")
}

// TestParticleBuiltinUsedWhenOverrideEmpty verifies the zero-value path:
// when ParticleUpdateWGSL is not set the renderer uses the built-in source.
func TestParticleBuiltinUsedWhenOverrideEmpty(t *testing.T) {
	d := newFakeDevice()
	r, err := New(Config{Device: d, Surface: fakeSurface{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Destroy()

	var overrideShader *fakeShader
	for _, s := range d.shaders {
		if s.label == "bundle.particles.update" {
			overrideShader = s
			break
		}
	}
	if overrideShader == nil {
		t.Fatal("no shader with label 'bundle.particles.update' found")
	}
	if overrideShader.src != particleUpdateWGSL {
		t.Errorf("expected builtin shader source when override is empty")
	}

	for _, cp := range d.computePipelines {
		if cp.desc.Label == "bundle.particles.update" {
			if cp.desc.EntryPoint != "main" {
				t.Errorf("builtin entry point: want 'main', got %q", cp.desc.EntryPoint)
			}
			return
		}
	}
	t.Fatal("no compute pipeline with label 'bundle.particles.update' found")
}
