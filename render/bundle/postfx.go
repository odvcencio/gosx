package bundle

import (
	"fmt"
	"strings"

	"github.com/odvcencio/gosx/render/gpu"
)

const (
	defaultHDRMemoryBudgetBytes = 64 << 20
	defaultHDRBudgetPixels      = 1920 * 1080
	postFXFormat                = gpu.FormatRGBA8Unorm
)

func selectHDRFormat(device gpu.Device, budgetBytes int) gpu.TextureFormat {
	if budgetBytes <= 0 {
		budgetBytes = defaultHDRMemoryBudgetBytes
	}
	if gpu.FormatRGBA16FloatSupported(device) &&
		defaultHDRBudgetPixels*hdrBytesPerPixel(gpu.FormatRGBA16Float) <= budgetBytes {
		return gpu.FormatRGBA16Float
	}
	if gpu.FormatRGB9E5UfloatSupported(device) {
		return gpu.FormatRGB9E5Ufloat
	}
	if gpu.FormatRGBA16FloatSupported(device) {
		return gpu.FormatRGBA16Float
	}
	return gpu.FormatRGBA8Unorm
}

func hdrBytesPerPixel(format gpu.TextureFormat) int {
	switch format {
	case gpu.FormatRGBA16Float:
		return 8
	case gpu.FormatRGBA32Float:
		return 16
	case gpu.FormatRGB9E5Ufloat, gpu.FormatRGB10A2Unorm,
		gpu.FormatRGBA8Unorm, gpu.FormatBGRA8Unorm,
		gpu.FormatRGBA8UnormSRGB, gpu.FormatBGRA8UnormSRGB:
		return 4
	default:
		return 0
	}
}

func surfaceUsesHDR10(format gpu.TextureFormat) bool {
	return format == gpu.FormatRGB10A2Unorm
}

// presentWGSL — historically the ACES-only present shader. Now that bloom
// is wired in, the actual present pipeline is built from composePresentWGSL
// (see bloom.go). Kept here for reference + as a fallback if we ever want
// to disable bloom via a config flag.

// presentResources groups the HDR sampler and the present bind group that
// binds the HDR texture view + sampler.
type presentResources struct {
	sampler gpu.Sampler
}

// buildPresentPipeline constructs the tone-mapping composite pipeline. It
// writes a display-linear LDR image into postFXFormat; the dedicated FXAA pass
// then filters that image into the swap chain.
func (r *Renderer) buildPresentPipeline() error {
	shader, err := r.device.CreateShaderModule(gpu.ShaderDesc{
		SourceWGSL: composePresentWGSL,
		Label:      "bundle.present.compose",
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
				{Format: postFXFormat, WriteMask: gpu.ColorWriteAll},
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

func (r *Renderer) buildFXAAPipeline() error {
	shader, err := r.device.CreateShaderModule(gpu.ShaderDesc{
		SourceWGSL: fxaa311WGSL(surfaceUsesHDR10(r.surfaceFormat)),
		Label:      "bundle.postfx.fxaa311",
	})
	if err != nil {
		return fmt.Errorf("bundle.buildFXAAPipeline: %w", err)
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
		Label:      "bundle.fxaa311",
	})
	if err != nil {
		return fmt.Errorf("bundle.buildFXAAPipeline: %w", err)
	}
	r.fxaaPipeline = pipeline
	r.fxaaBGLayout = pipeline.GetBindGroupLayout(0)
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
		Format: r.hdrFormat,
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

	// Allocate the GPU picking id buffer alongside HDR. R32Uint so a single
	// readback yields an exact u32 pick ID with no alpha / unpacking.
	if r.idBufferTex != nil {
		r.idBufferTex.Destroy()
	}
	idTex, err := r.device.CreateTexture(gpu.TextureDesc{
		Width:  width,
		Height: height,
		Format: gpu.FormatR32Uint,
		Usage:  gpu.TextureUsageRenderAttachment | gpu.TextureUsageCopySrc,
		Label:  "bundle.pickIdBuffer",
	})
	if err != nil {
		return nil, fmt.Errorf("bundle.ensureHDR (idBuffer): %w", err)
	}
	r.idBufferTex = idTex
	r.idBufferView = idTex.CreateView()

	// Rebuilding the present bind group happens in ensureBloom because the
	// bind group references BOTH the HDR view and the bloom chain's view.
	return r.hdrView, nil
}

func (r *Renderer) ensurePostFX(width, height int) error {
	if r.postFXTex != nil && r.postFXWidth == width && r.postFXHeight == height {
		return nil
	}
	if r.postFXTex != nil {
		r.postFXTex.Destroy()
		r.postFXTex = nil
	}
	tex, err := r.device.CreateTexture(gpu.TextureDesc{
		Width:  width,
		Height: height,
		Format: postFXFormat,
		Usage:  gpu.TextureUsageRenderAttachment | gpu.TextureUsageTextureBinding,
		Label:  "bundle.postfx.ldr",
	})
	if err != nil {
		return fmt.Errorf("bundle.ensurePostFX: %w", err)
	}
	r.postFXTex = tex
	r.postFXView = tex.CreateView()
	r.postFXWidth = width
	r.postFXHeight = height

	bg, err := r.device.CreateBindGroup(gpu.BindGroupDesc{
		Layout: r.fxaaBGLayout,
		Entries: []gpu.BindGroupEntry{
			{Binding: 0, TextureView: r.postFXView},
			{Binding: 1, Sampler: r.presentSampler},
		},
		Label: "bundle.fxaa311.bg",
	})
	if err != nil {
		return fmt.Errorf("bundle.ensurePostFX (fxaa bg): %w", err)
	}
	r.fxaaBindGrp = bg
	return nil
}

// recordPresentPass writes HDR + optional bloom into the LDR post-FX target.
// The swap-chain write is intentionally isolated in recordFXAAPass so FXAA can
// evaluate final display luminance instead of pre-tonemap HDR values.
func (r *Renderer) recordPresentPass(enc gpu.CommandEncoder) {
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{
		ColorAttachments: []gpu.RenderPassColorAttachment{{
			View:       r.postFXView,
			LoadOp:     gpu.LoadOpClear,
			StoreOp:    gpu.StoreOpStore,
			ClearValue: gpu.Color{R: 0, G: 0, B: 0, A: 1},
		}},
		Label: "bundle.present.compose",
	})
	pass.SetPipeline(r.presentPipeline)
	pass.SetBindGroup(0, r.presentBindGrp)
	pass.Draw(3, 1, 0, 0)
	pass.End()
}

func (r *Renderer) recordFXAAPass(enc gpu.CommandEncoder, surfaceView gpu.TextureView) {
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{
		ColorAttachments: []gpu.RenderPassColorAttachment{{
			View:       surfaceView,
			LoadOp:     gpu.LoadOpClear,
			StoreOp:    gpu.StoreOpStore,
			ClearValue: gpu.Color{R: 0, G: 0, B: 0, A: 1},
		}},
		Label: "bundle.fxaa311",
	})
	pass.SetPipeline(r.fxaaPipeline)
	pass.SetBindGroup(0, r.fxaaBindGrp)
	pass.Draw(3, 1, 0, 0)
	pass.End()
}

const fxaa311WGSLTemplate = `
const useHDR10 : bool = {{HDR10}};

struct VSOut {
  @builtin(position) pos : vec4<f32>,
  @location(0) uv : vec2<f32>,
};

@group(0) @binding(0) var srcTexture : texture_2d<f32>;
@group(0) @binding(1) var srcSampler : sampler;

@vertex
fn vs_main(@builtin(vertex_index) vid : u32) -> VSOut {
  var p = array<vec2<f32>, 3>(
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
  out.pos = vec4<f32>(p[vid], 0.0, 1.0);
  out.uv  = uv[vid];
  return out;
}

fn greenLuma(c : vec3<f32>) -> f32 {
  return c.g;
}

fn sampleRGB(uv : vec2<f32>) -> vec3<f32> {
  return textureSample(srcTexture, srcSampler, uv).rgb;
}

fn pqEncodeChannel(v : f32) -> f32 {
  let m1 = 2610.0 / 16384.0;
  let m2 = 2523.0 / 32.0;
  let c1 = 3424.0 / 4096.0;
  let c2 = 2413.0 / 128.0;
  let c3 = 2392.0 / 128.0;
  let vp = pow(clamp(v, 0.0, 1.0), m1);
  return pow((c1 + c2 * vp) / (1.0 + c3 * vp), m2);
}

fn encodeOutput(c : vec3<f32>) -> vec3<f32> {
  if (useHDR10) {
    return vec3<f32>(pqEncodeChannel(c.r), pqEncodeChannel(c.g), pqEncodeChannel(c.b));
  }
  return c;
}

@fragment
fn fs_main(in : VSOut) -> @location(0) vec4<f32> {
  let dims = vec2<f32>(textureDimensions(srcTexture, 0));
  let rcpFrame = 1.0 / dims;
  let uv = in.uv;

  let rgbNW = sampleRGB(uv + vec2<f32>(-1.0, -1.0) * rcpFrame);
  let rgbNE = sampleRGB(uv + vec2<f32>( 1.0, -1.0) * rcpFrame);
  let rgbSW = sampleRGB(uv + vec2<f32>(-1.0,  1.0) * rcpFrame);
  let rgbSE = sampleRGB(uv + vec2<f32>( 1.0,  1.0) * rcpFrame);
  let rgbM  = sampleRGB(uv);

  let lumaNW = greenLuma(rgbNW);
  let lumaNE = greenLuma(rgbNE);
  let lumaSW = greenLuma(rgbSW);
  let lumaSE = greenLuma(rgbSE);
  let lumaM  = greenLuma(rgbM);

  let lumaMin = min(lumaM, min(min(lumaNW, lumaNE), min(lumaSW, lumaSE)));
  let lumaMax = max(lumaM, max(max(lumaNW, lumaNE), max(lumaSW, lumaSE)));

  var dir = vec2<f32>(
    -((lumaNW + lumaNE) - (lumaSW + lumaSE)),
     ((lumaNW + lumaSW) - (lumaNE + lumaSE)),
  );

  let reduceMul = 1.0 / 8.0;
  let reduceMin = 1.0 / 128.0;
  let spanMax = 8.0;
  let dirReduce = max((lumaNW + lumaNE + lumaSW + lumaSE) * (0.25 * reduceMul), reduceMin);
  let rcpDirMin = 1.0 / (min(abs(dir.x), abs(dir.y)) + dirReduce);
  dir = clamp(dir * rcpDirMin, vec2<f32>(-spanMax), vec2<f32>(spanMax)) * rcpFrame;

  let rgbA = 0.5 * (
    sampleRGB(uv + dir * (1.0 / 3.0 - 0.5)) +
    sampleRGB(uv + dir * (2.0 / 3.0 - 0.5)));
  let rgbB = rgbA * 0.5 + 0.25 * (
    sampleRGB(uv + dir * -0.5) +
    sampleRGB(uv + dir *  0.5));

  let lumaB = greenLuma(rgbB);
  let color = select(rgbB, rgbA, lumaB < lumaMin || lumaB > lumaMax);
  return vec4<f32>(encodeOutput(color), 1.0);
}
`

func fxaa311WGSL(hdr10 bool) string {
	value := "false"
	if hdr10 {
		value = "true"
	}
	return strings.Replace(fxaa311WGSLTemplate, "{{HDR10}}", value, 1)
}
