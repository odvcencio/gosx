package bundle

import (
	"fmt"
	"math"
	"strings"
	"sync"

	"github.com/odvcencio/gosx/engine"
	"github.com/odvcencio/gosx/render/gpu"
)

// particleState is the GPU-side layout of one particle: 32 bytes total.
// Matches the WGSL Particle struct in particleUpdateWGSL.
//
//	position : vec4<f32>  — xyz = world position, w = age in seconds
//	velocity : vec4<f32>  — xyz = velocity, w = lifetime in seconds
const particleStride = 32

// particleUpdateWGSL integrates particle state each frame. New particles
// and respawns (when age ≥ lifetime) emit at the configured emitter
// position with a pseudo-random velocity scaled by the emitter radius.
// A compact force graph is applied every tick.
const particleUpdateWGSL = `
struct Particle {
  position : vec4<f32>,
  velocity : vec4<f32>,
};

struct ParticleForce {
  meta   : vec4<f32>, // x kind, y strength, z frequency, w unused
  vector : vec4<f32>, // xyz direction/target/axis, w unused
};

struct ParticleUniforms {
  dt          : f32,
  time        : f32,
  lifetime    : f32,
  forceCount  : f32,
  emitterPos  : vec4<f32>, // xyz pos, w radius
  initialSpeed: vec4<f32>, // x initialSpeed, yzw pad
  forces      : array<ParticleForce, 8>,
};

const PARTICLE_MAX_FORCES : u32 = 8u;
const PARTICLE_FORCE_GRAVITY : u32 = 1u;
const PARTICLE_FORCE_DRAG : u32 = 2u;
const PARTICLE_FORCE_WIND : u32 = 3u;
const PARTICLE_FORCE_ATTRACTOR : u32 = 4u;
const PARTICLE_FORCE_VORTEX : u32 = 5u;
const PARTICLE_FORCE_TURBULENCE : u32 = 6u;

@group(0) @binding(0) var<uniform> u : ParticleUniforms;
@group(0) @binding(1) var<storage, read_write> particles : array<Particle>;

// Dirt-cheap hash that gets us just enough randomness for respawn.
fn hash13(p : vec3<f32>) -> f32 {
  var p3 = fract(p * 0.1031);
  p3 = p3 + dot(p3, p3.yzx + 33.33);
  return fract((p3.x + p3.y) * p3.z);
}

fn forceVectorOrDefault(v : vec3<f32>, fallback : vec3<f32>) -> vec3<f32> {
  if (dot(v, v) <= 0.00000001) {
    return fallback;
  }
  return v;
}

fn particleForceAcceleration(f : ParticleForce, pos : vec3<f32>, time : f32) -> vec3<f32> {
  let kind = u32(f.meta.x + 0.5);
  let strength = f.meta.y;
  let v = f.vector.xyz;
  if (kind == PARTICLE_FORCE_GRAVITY) {
    let g = forceVectorOrDefault(v, vec3<f32>(0.0, -1.0, 0.0));
    return g * strength;
  }
  if (kind == PARTICLE_FORCE_WIND) {
    let wind = forceVectorOrDefault(v, vec3<f32>(1.0, 0.0, 0.0));
    return wind * strength;
  }
  if (kind == PARTICLE_FORCE_ATTRACTOR) {
    let delta = v - pos;
    let dist = length(delta);
    if (dist <= 0.0001) {
      return vec3<f32>(0.0);
    }
    return (delta / dist) * strength;
  }
  if (kind == PARTICLE_FORCE_VORTEX) {
    let axis = normalize(forceVectorOrDefault(v, vec3<f32>(0.0, 1.0, 0.0)));
    let radial = pos - axis * dot(pos, axis);
    let dist = length(radial);
    if (dist <= 0.0001) {
      return vec3<f32>(0.0);
    }
    return normalize(cross(radial, axis)) * strength;
  }
  if (kind == PARTICLE_FORCE_TURBULENCE) {
    var freq = abs(f.meta.z);
    if (freq <= 0.000001) {
      freq = 1.0;
    }
    let nx = sin(pos.x * freq + time * 1.3) * cos(pos.z * freq + time * 0.7);
    let ny = sin(pos.y * freq + time * 0.9) * cos(pos.x * freq + time * 1.1);
    let nz = sin(pos.z * freq + time * 1.7) * cos(pos.y * freq + time * 0.5);
    return vec3<f32>(nx, ny, nz) * strength;
  }
  return vec3<f32>(0.0);
}

@compute @workgroup_size(64)
fn main(@builtin(global_invocation_id) gid : vec3<u32>) {
  let i = gid.x;
  if (i >= arrayLength(&particles)) { return; }
  var p = particles[i];

  let newAge = p.position.w + u.dt;
  if (newAge >= p.velocity.w || p.velocity.w <= 0.0) {
    // Respawn at emitter with pseudo-random direction.
    let seed = vec3<f32>(f32(i), u.time, u.time * 1.37);
    let rx = hash13(seed) * 2.0 - 1.0;
    let ry = hash13(seed + vec3<f32>(1.7, 2.3, 3.1));
    let rz = hash13(seed + vec3<f32>(4.1, 5.3, 6.7)) * 2.0 - 1.0;
    let dir = normalize(vec3<f32>(rx, ry * 0.4 + 0.3, rz));
    let offset = vec3<f32>(rx, hash13(seed + vec3<f32>(9.1, 3.3, 7.7)) * 2.0 - 1.0, rz) * u.emitterPos.w;
    p.position = vec4<f32>(u.emitterPos.xyz + offset, 0.0);
    p.velocity = vec4<f32>(dir * u.initialSpeed.x, u.lifetime);
  } else {
    var acceleration = vec3<f32>(0.0);
    var drag = 0.0;
    for (var fi = 0u; fi < PARTICLE_MAX_FORCES; fi = fi + 1u) {
      if (f32(fi) >= u.forceCount) {
        break;
      }
      let force = u.forces[fi];
      let kind = u32(force.meta.x + 0.5);
      if (kind == PARTICLE_FORCE_DRAG) {
        drag = drag + force.meta.y;
      } else {
        acceleration = acceleration + particleForceAcceleration(force, p.position.xyz, u.time);
      }
    }
    let dragFactor = clamp(1.0 - drag * u.dt, 0.0, 1.0);
    let newVel = p.velocity.xyz * dragFactor + acceleration * u.dt;
    let newPos = p.position.xyz + newVel * u.dt;
    p.position = vec4<f32>(newPos, newAge);
    p.velocity = vec4<f32>(newVel, p.velocity.w);
  }
  particles[i] = p;
}
`

// particleRenderWGSL draws each particle as a camera-facing billboarded
// quad. The vertex shader reads per-instance particle state from the
// storage buffer using the builtin instance_index — a single draw call
// handles the whole system.
//
// Fragment shader emits HDR color — the bloom + tone-map chain picks it
// up automatically so particles naturally glow at high intensities.
const particleRenderWGSL = `
struct Particle {
  position : vec4<f32>,
  velocity : vec4<f32>,
};

struct ParticleSceneUniforms {
  viewProj      : mat4x4<f32>,
  cameraPos     : vec4<f32>,
  colorStart    : vec4<f32>, // rgb + intensity
  colorEnd      : vec4<f32>, // rgb + intensity
  sizeStartEnd  : vec4<f32>, // x = size start, y = size end, zw pad
};

@group(0) @binding(0) var<uniform> scene : ParticleSceneUniforms;
@group(0) @binding(1) var<storage, read> particles : array<Particle>;

struct VSOut {
  @builtin(position) pos : vec4<f32>,
  @location(0) color     : vec4<f32>,
  @location(1) localUV   : vec2<f32>,
};

@vertex
fn vs_main(
  @builtin(vertex_index) vid : u32,
  @builtin(instance_index) iid : u32,
) -> VSOut {
  // Two-triangle quad, corners in local-uv-like space.
  var quad = array<vec2<f32>, 6>(
    vec2<f32>(-1.0, -1.0),
    vec2<f32>( 1.0, -1.0),
    vec2<f32>( 1.0,  1.0),
    vec2<f32>(-1.0, -1.0),
    vec2<f32>( 1.0,  1.0),
    vec2<f32>(-1.0,  1.0),
  );
  let corner = quad[vid];

  let p = particles[iid];
  let age = p.position.w;
  let lifetime = max(p.velocity.w, 0.0001);
  let t = clamp(age / lifetime, 0.0, 1.0);

  // Billboard axes: right = normalized(cross(up, forward)), up' = cross(forward, right).
  let toCam = normalize(scene.cameraPos.xyz - p.position.xyz);
  let worldUp = vec3<f32>(0.0, 1.0, 0.0);
  var right = cross(worldUp, toCam);
  let rLen = length(right);
  if (rLen < 1e-4) {
    // Looking straight up/down — fall back to world-X as the tangent.
    right = vec3<f32>(1.0, 0.0, 0.0);
  } else {
    right = right / rLen;
  }
  let bUp = cross(toCam, right);

  let size = mix(scene.sizeStartEnd.x, scene.sizeStartEnd.y, t);
  let world = p.position.xyz + (right * corner.x + bUp * corner.y) * size;

  var out : VSOut;
  out.pos = scene.viewProj * vec4<f32>(world, 1.0);
  let rgb = mix(scene.colorStart.rgb * scene.colorStart.a,
                scene.colorEnd.rgb   * scene.colorEnd.a,   t);
  // Alpha fades at spawn + death so the billboards don't pop in/out.
  let edgeFade = smoothstep(0.0, 0.15, t) * (1.0 - smoothstep(0.85, 1.0, t));
  out.color = vec4<f32>(rgb, edgeFade);
  out.localUV = corner;
  return out;
}

struct ParticleFSOut {
  @location(0) color  : vec4<f32>,
  @location(1) pickId : u32,
};

@fragment
fn fs_main(in : VSOut) -> ParticleFSOut {
  // Soft disc: radial falloff keeps the quad corners from looking blocky.
  let d = length(in.localUV);
  let softness = smoothstep(1.0, 0.0, d);
  let rgb = in.color.rgb * softness;
  let alpha = in.color.a * softness;
  var out : ParticleFSOut;
  out.color  = vec4<f32>(rgb * alpha, alpha);
  // Particles aren't pickable in R4.
  out.pickId = 0u;
  return out;
}
`

const (
	particleMaxForces          = 8
	particleForceStride        = 32
	particleUniformForceOffset = 48
)

// particleUniformSize matches ParticleUniforms in particleUpdateWGSL.
// 3 vec4 blocks (48) + 8 force records (256) = 304 bytes.
const particleUniformSize = particleUniformForceOffset + particleMaxForces*particleForceStride

const (
	particleForceNone = iota
	particleForceGravity
	particleForceDrag
	particleForceWind
	particleForceAttractor
	particleForceVortex
	particleForceTurbulence
)

var (
	particleForceKindsMu sync.RWMutex
	particleForceAliases = map[string]int{}
)

// RegisterParticleForceKind maps a custom force kind to an existing particle
// shader force. It returns a cleanup function that restores the previous alias.
func RegisterParticleForceKind(kind string, target string) func() {
	key := particleForceKindKey(kind)
	targetKind := particleForceKind(target)
	if key == "" || targetKind == particleForceNone {
		return func() {}
	}

	particleForceKindsMu.Lock()
	previous, hadPrevious := particleForceAliases[key]
	particleForceAliases[key] = targetKind
	particleForceKindsMu.Unlock()

	return func() {
		particleForceKindsMu.Lock()
		if hadPrevious {
			particleForceAliases[key] = previous
		} else {
			delete(particleForceAliases, key)
		}
		particleForceKindsMu.Unlock()
	}
}

func particleForceKindKey(kind string) string {
	key := strings.ToLower(strings.TrimSpace(kind))
	if key == "" {
		return ""
	}
	for _, r := range key {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			continue
		}
		return ""
	}
	return key
}

// particleSceneUniformSize matches ParticleSceneUniforms. mat4 (64) + 4
// vec4 (64) = 128 bytes.
const particleSceneUniformSize = 128

// particleResources holds a system's GPU state.
type particleResources struct {
	count            int
	particleBuf      gpu.Buffer // storage buffer of Particle[]
	updateUniformBuf gpu.Buffer // ParticleUniforms
	sceneUniformBuf  gpu.Buffer // ParticleSceneUniforms
	updateBindGrp    gpu.BindGroup
	renderBindGrp    gpu.BindGroup
	// initialized flag so the first-frame respawn logic seeds lifetimes.
	initialized bool
}

// buildParticlePipelines constructs the compute (state integration) and
// render (billboarded quad) pipelines used for compute particles.
func (r *Renderer) buildParticlePipelines() error {
	compShader, err := r.device.CreateShaderModule(gpu.ShaderDesc{
		SourceWGSL: particleUpdateWGSL,
		Label:      "bundle.particles.update",
	})
	if err != nil {
		return fmt.Errorf("bundle.buildParticlePipelines (compute): %w", err)
	}
	comp, err := r.device.CreateComputePipeline(gpu.ComputePipelineDesc{
		Module:     compShader,
		EntryPoint: "main",
		AutoLayout: true,
		Label:      "bundle.particles.update",
	})
	if err != nil {
		return fmt.Errorf("bundle.buildParticlePipelines (compute pipeline): %w", err)
	}
	r.particleUpdatePipeline = comp
	r.particleUpdateBGLayout = comp.GetBindGroupLayout(0)

	renderShader, err := r.device.CreateShaderModule(gpu.ShaderDesc{
		SourceWGSL: particleRenderWGSL,
		Label:      "bundle.particles.render",
	})
	if err != nil {
		return fmt.Errorf("bundle.buildParticlePipelines (render): %w", err)
	}
	render, err := r.device.CreateRenderPipeline(gpu.RenderPipelineDesc{
		Vertex: gpu.VertexStageDesc{
			Module:     renderShader,
			EntryPoint: "vs_main",
		},
		Fragment: gpu.FragmentStageDesc{
			Module:     renderShader,
			EntryPoint: "fs_main",
			Targets: []gpu.ColorTargetState{
				{
					Format:    r.hdrFormat,
					WriteMask: gpu.ColorWriteAll,
					// Additive blend in HDR — particles glow when summed.
					Blend: &gpu.BlendState{
						Color: gpu.BlendComponent{SrcFactor: gpu.BlendOne, DstFactor: gpu.BlendOne, Operation: gpu.BlendOpAdd},
						Alpha: gpu.BlendComponent{SrcFactor: gpu.BlendOne, DstFactor: gpu.BlendOne, Operation: gpu.BlendOpAdd},
					},
				},
				{
					// Pick target: particles aren't pickable, but the main
					// pass requires matching attachments across pipelines.
					Format: gpu.FormatR32Uint, WriteMask: gpu.ColorWriteAll,
				},
			},
		},
		Primitive: gpu.PrimitiveState{
			Topology:  gpu.TopologyTriangleList,
			CullMode:  gpu.CullNone,
			FrontFace: gpu.FrontFaceCCW,
		},
		// Read depth so particles are occluded, but don't write it — keeps
		// overlapping particles from z-fighting with each other.
		DepthStencil: &gpu.DepthStencilState{
			Format:            r.depthFormat,
			DepthWriteEnabled: false,
			DepthCompare:      gpu.CompareLessEqual,
		},
		AutoLayout: true,
		Label:      "bundle.particles.render",
	})
	if err != nil {
		return fmt.Errorf("bundle.buildParticlePipelines (render pipeline): %w", err)
	}
	r.particleRenderPipeline = render
	r.particleRenderBGLayout = render.GetBindGroupLayout(0)
	return nil
}

// ensureParticleSystem allocates per-system GPU resources (state buffer,
// uniforms, bind groups). Existing systems at the same count are reused
// across frames; a changed count triggers reallocation.
func (r *Renderer) ensureParticleSystem(id string, count int) (*particleResources, error) {
	if count <= 0 {
		return nil, nil
	}
	res, ok := r.particleCache[id]
	if ok && res.count == count {
		return res, nil
	}
	if res != nil {
		destroyParticleResources(res)
	}

	particleBytes := count * particleStride
	stateBuf, err := r.device.CreateBuffer(gpu.BufferDesc{
		Size:  particleBytes,
		Usage: gpu.BufferUsageStorage | gpu.BufferUsageCopyDst,
		Label: "bundle.particles.state:" + id,
	})
	if err != nil {
		return nil, fmt.Errorf("bundle.ensureParticleSystem: %w", err)
	}
	// Seed the state buffer with lifetime=0 so every particle respawns on
	// frame 0 — that spreads them across random positions by tick two.
	r.device.Queue().WriteBuffer(stateBuf, 0, make([]byte, particleBytes))

	updateBuf, err := r.device.CreateBuffer(gpu.BufferDesc{
		Size:  particleUniformSize,
		Usage: gpu.BufferUsageUniform | gpu.BufferUsageCopyDst,
		Label: "bundle.particles.updateUniforms:" + id,
	})
	if err != nil {
		stateBuf.Destroy()
		return nil, fmt.Errorf("bundle.ensureParticleSystem: %w", err)
	}
	sceneBuf, err := r.device.CreateBuffer(gpu.BufferDesc{
		Size:  particleSceneUniformSize,
		Usage: gpu.BufferUsageUniform | gpu.BufferUsageCopyDst,
		Label: "bundle.particles.sceneUniforms:" + id,
	})
	if err != nil {
		stateBuf.Destroy()
		updateBuf.Destroy()
		return nil, fmt.Errorf("bundle.ensureParticleSystem: %w", err)
	}

	updateBG, err := r.device.CreateBindGroup(gpu.BindGroupDesc{
		Layout: r.particleUpdateBGLayout,
		Entries: []gpu.BindGroupEntry{
			{Binding: 0, Buffer: updateBuf, Size: particleUniformSize},
			{Binding: 1, Buffer: stateBuf, Size: particleBytes},
		},
		Label: "bundle.particles.updateBG:" + id,
	})
	if err != nil {
		stateBuf.Destroy()
		updateBuf.Destroy()
		sceneBuf.Destroy()
		return nil, fmt.Errorf("bundle.ensureParticleSystem: %w", err)
	}
	renderBG, err := r.device.CreateBindGroup(gpu.BindGroupDesc{
		Layout: r.particleRenderBGLayout,
		Entries: []gpu.BindGroupEntry{
			{Binding: 0, Buffer: sceneBuf, Size: particleSceneUniformSize},
			{Binding: 1, Buffer: stateBuf, Size: particleBytes},
		},
		Label: "bundle.particles.renderBG:" + id,
	})
	if err != nil {
		stateBuf.Destroy()
		updateBuf.Destroy()
		sceneBuf.Destroy()
		return nil, fmt.Errorf("bundle.ensureParticleSystem: %w", err)
	}

	fresh := &particleResources{
		count:            count,
		particleBuf:      stateBuf,
		updateUniformBuf: updateBuf,
		sceneUniformBuf:  sceneBuf,
		updateBindGrp:    updateBG,
		renderBindGrp:    renderBG,
	}
	r.particleCache[id] = fresh
	return fresh, nil
}

func destroyParticleResources(p *particleResources) {
	if p == nil {
		return
	}
	if p.particleBuf != nil {
		p.particleBuf.Destroy()
	}
	if p.updateUniformBuf != nil {
		p.updateUniformBuf.Destroy()
	}
	if p.sceneUniformBuf != nil {
		p.sceneUniformBuf.Destroy()
	}
}

// recordParticleUpdates runs one compute dispatch per particle system,
// integrating state from time t-dt to t. Must be recorded before the main
// render pass since writeBuffer for the uniforms has to happen outside an
// open render pass encoder.
func (r *Renderer) recordParticleUpdates(enc gpu.CommandEncoder, b engine.RenderBundle, dt, tSeconds float64, viewProj mat4, cameraPos [4]float32) error {
	if len(b.ComputeParticles) == 0 {
		return nil
	}

	for _, cp := range b.ComputeParticles {
		if cp.Count <= 0 {
			continue
		}
		res, err := r.ensureParticleSystem(cp.ID, cp.Count)
		if err != nil {
			return err
		}
		if res == nil {
			continue
		}

		forces := particleForcesFromRender(cp.Forces)
		lifetime := float32(cp.Emitter.Lifetime)
		if lifetime <= 0 {
			lifetime = 2.5
		}
		radius := float32(cp.Emitter.Radius)
		if radius <= 0 {
			radius = 0.5
		}
		initialSpeed := float32(cp.Emitter.Scatter)
		if initialSpeed <= 0 {
			initialSpeed = 3.0
		}

		r.device.Queue().WriteBuffer(res.updateUniformBuf, 0, encodeParticleUpdateUniforms(
			float32(dt), float32(tSeconds), lifetime,
			[4]float32{float32(cp.Emitter.X), float32(cp.Emitter.Y), float32(cp.Emitter.Z), radius},
			[4]float32{initialSpeed, 0, 0, 0},
			forces,
		))

		// Render-pass uniforms. Colors from RenderParticleMaterial with
		// warm-orange default so unset emitters still read visually.
		colorStart := parseCSSColor(cp.Material.Color, [3]float32{1.0, 0.5, 0.15})
		colorEnd := parseCSSColor(cp.Material.ColorEnd, [3]float32{0.9, 0.1, 0.02})
		intensityStart := float32(cp.Material.Opacity)
		if intensityStart <= 0 {
			intensityStart = 1.0
		}
		intensityEnd := float32(cp.Material.OpacityEnd)
		if intensityEnd < 0 {
			intensityEnd = 0.0
		}
		sizeStart := float32(cp.Material.Size)
		if sizeStart <= 0 {
			sizeStart = 0.4
		}
		sizeEnd := float32(cp.Material.SizeEnd)
		if sizeEnd <= 0 {
			sizeEnd = 0.05
		}
		r.device.Queue().WriteBuffer(res.sceneUniformBuf, 0, encodeParticleSceneUniforms(
			viewProj, cameraPos,
			[4]float32{colorStart[0], colorStart[1], colorStart[2], intensityStart},
			[4]float32{colorEnd[0], colorEnd[1], colorEnd[2], intensityEnd},
			[4]float32{sizeStart, sizeEnd, 0, 0},
		))
	}

	pass := enc.BeginComputePass()
	pass.SetPipeline(r.particleUpdatePipeline)
	for _, cp := range b.ComputeParticles {
		if cp.Count <= 0 {
			continue
		}
		res, ok := r.particleCache[cp.ID]
		if !ok {
			continue
		}
		pass.SetBindGroup(0, res.updateBindGrp)
		groups := (cp.Count + 63) / 64
		pass.DispatchWorkgroups(groups, 1, 1)
	}
	pass.End()
	return nil
}

// drawParticles is invoked inside the main render pass after lit instanced
// meshes — particles benefit from additive blending and depth-test-only
// (not write) so they composite over opaque geometry.
func (r *Renderer) drawParticles(pass gpu.RenderPassEncoder, b engine.RenderBundle) {
	if len(b.ComputeParticles) == 0 {
		return
	}
	pass.SetPipeline(r.particleRenderPipeline)
	for _, cp := range b.ComputeParticles {
		if cp.Count <= 0 {
			continue
		}
		res, ok := r.particleCache[cp.ID]
		if !ok {
			continue
		}
		pass.SetBindGroup(0, res.renderBindGrp)
		// 6 vertices per billboard quad, one instance per particle.
		pass.Draw(6, cp.Count, 0, 0)
	}
}

type particleForceUniform struct {
	kind      int
	strength  float32
	vector    [3]float32
	frequency float32
}

func encodeParticleUpdateUniforms(dt, time, lifetime float32,
	emitterPos, initialSpeed [4]float32, forces []particleForceUniform) []byte {
	out := make([]byte, particleUniformSize)
	forceCount := len(forces)
	if forceCount > particleMaxForces {
		forceCount = particleMaxForces
	}
	copy(out[0:16], float32sToBytes([]float32{dt, time, lifetime, float32(forceCount)}))
	copy(out[16:32], float32sToBytes(emitterPos[:]))
	copy(out[32:48], float32sToBytes(initialSpeed[:]))
	for i := 0; i < forceCount; i++ {
		force := forces[i]
		off := particleUniformForceOffset + i*particleForceStride
		copy(out[off:off+16], float32sToBytes([]float32{
			float32(force.kind),
			force.strength,
			force.frequency,
			0,
		}))
		copy(out[off+16:off+32], float32sToBytes([]float32{
			force.vector[0],
			force.vector[1],
			force.vector[2],
			0,
		}))
	}
	return out
}

func particleForcesFromRender(src []engine.RenderParticleForce) []particleForceUniform {
	gravity := particleForceUniform{kind: particleForceGravity, strength: 9.8}
	drag := particleForceUniform{kind: particleForceDrag, strength: 0.1}
	extras := make([]particleForceUniform, 0, len(src))

	for _, f := range src {
		kind := particleForceKind(f.Kind)
		if kind == particleForceNone {
			continue
		}
		force := particleForceFromRender(kind, f)
		switch kind {
		case particleForceGravity:
			gravity = force
		case particleForceDrag:
			drag = force
		default:
			extras = append(extras, force)
		}
	}

	out := make([]particleForceUniform, 0, particleMaxForces)
	out = append(out, gravity, drag)
	for _, force := range extras {
		if len(out) >= particleMaxForces {
			break
		}
		out = append(out, force)
	}
	return out
}

func particleForceFromRender(kind int, f engine.RenderParticleForce) particleForceUniform {
	frequency := float32(f.Frequency)
	if frequency == 0 {
		frequency = 1
	}
	return particleForceUniform{
		kind:      kind,
		strength:  float32(f.Strength),
		vector:    [3]float32{float32(f.X), float32(f.Y), float32(f.Z)},
		frequency: frequency,
	}
}

func particleForceKind(kind string) int {
	key := particleForceKindKey(kind)
	switch key {
	case "gravity":
		return particleForceGravity
	case "drag":
		return particleForceDrag
	case "wind":
		return particleForceWind
	case "attractor", "attract":
		return particleForceAttractor
	case "vortex", "orbit":
		return particleForceVortex
	case "turbulence":
		return particleForceTurbulence
	default:
		particleForceKindsMu.RLock()
		alias := particleForceAliases[key]
		particleForceKindsMu.RUnlock()
		return alias
	}
}

func encodeParticleSceneUniforms(viewProj mat4, cameraPos, colorStart, colorEnd, sizeStartEnd [4]float32) []byte {
	out := make([]byte, particleSceneUniformSize)
	copy(out[0:64], float32sToBytes(viewProj[:]))
	copy(out[64:80], float32sToBytes(cameraPos[:]))
	copy(out[80:96], float32sToBytes(colorStart[:]))
	copy(out[96:112], float32sToBytes(colorEnd[:]))
	copy(out[112:128], float32sToBytes(sizeStartEnd[:]))
	return out
}

// unused math helpers surfaced for tests / diagnostics. Kept public to the
// package so other R4 phases (emitter shape libraries, force catalogs) can
// share them without re-deriving.
var _ = math.Sin
