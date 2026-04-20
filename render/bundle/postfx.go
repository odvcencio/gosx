package bundle

import (
	"fmt"

	"github.com/odvcencio/gosx/render/gpu"
)

// hdrFormat is the intermediate HDR color target format. RGBA16Float is the
// R4 baseline — plenty of headroom for physically-based lighting, no GPU
// native-compression tricks yet (R5 adds RGB9E5 / HDR10 compressed paths).
const hdrFormat = gpu.FormatRGBA16Float

// presentWGSL renders a full-screen triangle that samples the HDR
// intermediate target, applies the Narkowicz ACES filmic curve as a tone
// map, and writes the result to the swap chain.
//
// The full-screen triangle technique avoids the seam a fullscreen quad
// would introduce at the diagonal and runs with zero vertex buffers.
const presentWGSL = `
struct VSOut {
  @builtin(position) pos : vec4<f32>,
  @location(0) uv : vec2<f32>,
};

@group(0) @binding(0) var hdrTexture : texture_2d<f32>;
@group(0) @binding(1) var hdrSampler : sampler;

@vertex
fn vs_main(@builtin(vertex_index) vid : u32) -> VSOut {
  // Oversized triangle: covers the [-1,1] viewport with a single primitive.
  // UVs derived so texcoords map 0..1 across the visible rect.
  var pos = array<vec2<f32>, 3>(
    vec2<f32>(-1.0, -1.0),
    vec2<f32>( 3.0, -1.0),
    vec2<f32>(-1.0,  3.0),
  );
  var uv = array<vec2<f32>, 3>(
    vec2<f32>(0.0, 1.0),
    vec2<f32>(2.0, 1.0),
    vec2<f32>(0.0, -1.0),
  );
  var out : VSOut;
  out.pos = vec4<f32>(pos[vid], 0.0, 1.0);
  out.uv = uv[vid];
  return out;
}

// ACES filmic tone mapper — Narkowicz 2015 "ACES Filmic Tone Mapping Curve"
// approximation. Good tradeoff of compute cost vs. match to reference ACES;
// R5 can upgrade to the full Hill fit or RGB channel-independent shapers.
fn acesFilmic(x : vec3<f32>) -> vec3<f32> {
  let a = 2.51;
  let b = 0.03;
  let c = 2.43;
  let d = 0.59;
  let e = 0.14;
  return clamp((x * (a * x + b)) / (x * (c * x + d) + e),
               vec3<f32>(0.0), vec3<f32>(1.0));
}

@fragment
fn fs_main(in : VSOut) -> @location(0) vec4<f32> {
  let hdr = textureSample(hdrTexture, hdrSampler, in.uv).rgb;
  let mapped = acesFilmic(hdr);
  return vec4<f32>(mapped, 1.0);
}
`

// presentResources groups the HDR sampler and the present bind group that
// binds the HDR texture view + sampler.
type presentResources struct {
	sampler gpu.Sampler
}

// buildPresentPipeline constructs the tone-mapping composite pipeline. The
// pipeline has no vertex buffers and two bind-group entries: the HDR
// texture view (rebuilt on every resize) and the sampler (shared, stable).
func (r *Renderer) buildPresentPipeline() error {
	shader, err := r.device.CreateShaderModule(gpu.ShaderDesc{
		SourceWGSL: presentWGSL,
		Label:      "bundle.present",
	})
	if err != nil {
		return fmt.Errorf("bundle.buildPresentPipeline: %w", err)
	}
	pipeline, err := r.device.CreateRenderPipeline(gpu.RenderPipelineDesc{
		Vertex: gpu.VertexStageDesc{
			Module:     shader,
			EntryPoint: "vs_main",
		},
		Fragment: gpu.FragmentStageDesc{
			Module:     shader,
			EntryPoint: "fs_main",
			Targets: []gpu.ColorTargetState{
				{Format: r.surfaceFormat, WriteMask: gpu.ColorWriteAll},
			},
		},
		Primitive: gpu.PrimitiveState{
			Topology:  gpu.TopologyTriangleList,
			CullMode:  gpu.CullNone,
			FrontFace: gpu.FrontFaceCCW,
		},
		AutoLayout: true,
		Label:      "bundle.present",
	})
	if err != nil {
		return fmt.Errorf("bundle.buildPresentPipeline: %w", err)
	}
	r.presentPipeline = pipeline
	r.presentBGLayout = pipeline.GetBindGroupLayout(0)
	return nil
}

// buildPresentSampler creates the linear-filtering color sampler used by
// the present pass to read the HDR intermediate.
func (r *Renderer) buildPresentSampler() error {
	samp, err := r.device.CreateSampler(gpu.SamplerDesc{
		MagFilter: gpu.FilterLinear,
		MinFilter: gpu.FilterLinear,
		AddressU:  gpu.AddressClampToEdge,
		AddressV:  gpu.AddressClampToEdge,
		AddressW:  gpu.AddressClampToEdge,
		Label:     "bundle.present.sampler",
	})
	if err != nil {
		return fmt.Errorf("bundle.buildPresentSampler: %w", err)
	}
	r.presentSampler = samp
	return nil
}

// ensureHDR allocates or resizes the HDR intermediate color target to match
// the current surface dimensions, and rebuilds the present bind group that
// references its view.
func (r *Renderer) ensureHDR(width, height int) (gpu.TextureView, error) {
	if r.hdrTex != nil && r.hdrWidth == width && r.hdrHeight == height {
		return r.hdrView, nil
	}
	if r.hdrTex != nil {
		r.hdrTex.Destroy()
		r.hdrTex = nil
	}
	tex, err := r.device.CreateTexture(gpu.TextureDesc{
		Width:  width,
		Height: height,
		Format: hdrFormat,
		Usage:  gpu.TextureUsageRenderAttachment | gpu.TextureUsageTextureBinding,
		Label:  "bundle.hdr",
	})
	if err != nil {
		return nil, fmt.Errorf("bundle.ensureHDR: %w", err)
	}
	r.hdrTex = tex
	r.hdrView = tex.CreateView()
	r.hdrWidth = width
	r.hdrHeight = height

	// Rebuild the present bind group every resize — views are frozen at
	// creation time, so a new HDR texture demands a new bind group.
	bg, err := r.device.CreateBindGroup(gpu.BindGroupDesc{
		Layout: r.presentBGLayout,
		Entries: []gpu.BindGroupEntry{
			{Binding: 0, TextureView: r.hdrView},
			{Binding: 1, Sampler: r.presentSampler},
		},
		Label: "bundle.present.bindgroup",
	})
	if err != nil {
		return nil, fmt.Errorf("bundle.ensureHDR (bind group): %w", err)
	}
	r.presentBindGrp = bg
	return r.hdrView, nil
}

// recordPresentPass writes the tone-mapped HDR image to the swap chain.
// The present pass runs after the main lit pass and is the only pass in the
// frame that writes the surface — keeps the swap-chain flow trivial and
// leaves the HDR texture available for bloom / other post-FX taps.
func (r *Renderer) recordPresentPass(enc gpu.CommandEncoder, surfaceView gpu.TextureView) {
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{
		ColorAttachments: []gpu.RenderPassColorAttachment{{
			View:       surfaceView,
			LoadOp:     gpu.LoadOpClear,
			StoreOp:    gpu.StoreOpStore,
			ClearValue: gpu.Color{R: 0, G: 0, B: 0, A: 1},
		}},
		Label: "bundle.present",
	})
	pass.SetPipeline(r.presentPipeline)
	pass.SetBindGroup(0, r.presentBindGrp)
	pass.Draw(3, 1, 0, 0)
	pass.End()
}
