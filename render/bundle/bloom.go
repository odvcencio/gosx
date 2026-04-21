package bundle

import (
	"fmt"
	"strings"

	"github.com/odvcencio/gosx/engine"
	"github.com/odvcencio/gosx/render/gpu"
)

const (
	defaultBloomThreshold = 0.8
	defaultBloomIntensity = 0.5
	defaultBloomRadius    = 5.0
	defaultBloomScale     = 0.5
)

// brightPassWGSL does two things in one shader: threshold-filter the HDR
// image (anything dimmer than 1.0 contributes zero) and downsample by
// bilinear sampling a 2x2 tap pattern. The output lives in a half-res RGBA16F
// target that feeds the blur chain.
const brightPassWGSL = `
struct VSOut {
  @builtin(position) pos : vec4<f32>,
  @location(0) uv : vec2<f32>,
};

@group(0) @binding(0) var srcTexture : texture_2d<f32>;
@group(0) @binding(1) var srcSampler : sampler;

struct BloomUniforms {
  params : vec4<f32>, // x = threshold, y = intensity, z = scale
};
@group(0) @binding(2) var<uniform> bloom : BloomUniforms;

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

fn luminance(c : vec3<f32>) -> f32 {
  return dot(c, vec3<f32>(0.2126, 0.7152, 0.0722));
}

@fragment
fn fs_main(in : VSOut) -> @location(0) vec4<f32> {
  let c = textureSample(srcTexture, srcSampler, in.uv).rgb;
  // Soft-knee threshold — anything above bleeds. Keeps bloom tied to scene
  // intensity while letting the bundle carry the artist dial.
  let thresholdedLum = max(luminance(c) - bloom.params.x, 0.0);
  let soft = thresholdedLum / (thresholdedLum + 1.0);
  let bloomColor = c * soft;
  return vec4<f32>(bloomColor, 1.0);
}
`

// blurWGSL is a 1D 9-tap Gaussian used for both the horizontal and vertical
// blur passes. A uniform tells the shader which axis to sample along — the
// texel-size vec2 is just {1/width, 0} or {0, 1/height}.
const blurWGSL = `
struct VSOut {
  @builtin(position) pos : vec4<f32>,
  @location(0) uv : vec2<f32>,
};

struct BlurUniforms {
  texelOffset : vec4<f32>, // .xy = (dx, dy) in normalized UV space
};

@group(0) @binding(0) var srcTexture : texture_2d<f32>;
@group(0) @binding(1) var srcSampler : sampler;
@group(0) @binding(2) var<uniform> blur : BlurUniforms;

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

@fragment
fn fs_main(in : VSOut) -> @location(0) vec4<f32> {
  // Pre-computed 9-tap Gaussian weights (sigma ≈ 2.0, kernel radius 4).
  let w0 = 0.227027;
  let w1 = 0.194594;
  let w2 = 0.121621;
  let w3 = 0.054054;
  let w4 = 0.016216;
  let off = blur.texelOffset.xy;
  var sum = textureSample(srcTexture, srcSampler, in.uv).rgb * w0;
  sum = sum + textureSample(srcTexture, srcSampler, in.uv + off * 1.0).rgb * w1;
  sum = sum + textureSample(srcTexture, srcSampler, in.uv - off * 1.0).rgb * w1;
  sum = sum + textureSample(srcTexture, srcSampler, in.uv + off * 2.0).rgb * w2;
  sum = sum + textureSample(srcTexture, srcSampler, in.uv - off * 2.0).rgb * w2;
  sum = sum + textureSample(srcTexture, srcSampler, in.uv + off * 3.0).rgb * w3;
  sum = sum + textureSample(srcTexture, srcSampler, in.uv - off * 3.0).rgb * w3;
  sum = sum + textureSample(srcTexture, srcSampler, in.uv + off * 4.0).rgb * w4;
  sum = sum + textureSample(srcTexture, srcSampler, in.uv - off * 4.0).rgb * w4;
  return vec4<f32>(sum, 1.0);
}
`

// composePresentWGSL samples HDR + bloom and applies ACES tone mapping into
// an LDR intermediate. Anti-aliasing is intentionally not folded into this
// shader; the following FXAA pass evaluates final display luminance.
const composePresentWGSL = `
struct VSOut {
  @builtin(position) pos : vec4<f32>,
  @location(0) uv : vec2<f32>,
};

@group(0) @binding(0) var hdrTexture   : texture_2d<f32>;
@group(0) @binding(1) var hdrSampler   : sampler;
@group(0) @binding(2) var bloomTexture : texture_2d<f32>;
@group(0) @binding(3) var bloomSampler : sampler;

struct BloomUniforms {
  params : vec4<f32>, // x = threshold, y = intensity, z = scale
};
@group(0) @binding(4) var<uniform> bloom : BloomUniforms;

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

fn acesFilmic(x : vec3<f32>) -> vec3<f32> {
  let a = 2.51;
  let b = 0.03;
  let c = 2.43;
  let d = 0.59;
  let e = 0.14;
  return clamp((x * (a * x + b)) / (x * (c * x + d) + e),
               vec3<f32>(0.0), vec3<f32>(1.0));
}

fn toneMapAt(uv : vec2<f32>) -> vec3<f32> {
  let hdr   = textureSample(hdrTexture, hdrSampler, uv).rgb;
  let glow = textureSample(bloomTexture, bloomSampler, uv).rgb;
  return acesFilmic(hdr + glow * bloom.params.y);
}

@fragment
fn fs_main(in : VSOut) -> @location(0) vec4<f32> {
  return vec4<f32>(toneMapAt(in.uv), 1.0);
}
`

// bloomResources holds the state for the bloom chain — two ping-pong
// half-res render targets, their views, per-pass bind groups, and the tiny
// blur uniforms.
type bloomResources struct {
	width, height               int
	surfaceWidth, surfaceHeight int
	texA, texB                  gpu.Texture
	viewA, viewB                gpu.TextureView

	brightBindGrp gpu.BindGroup // reads HDR → writes texA
	blurHBindGrp  gpu.BindGroup // reads texA → writes texB
	blurVBindGrp  gpu.BindGroup // reads texB → writes texA

	paramsUniform gpu.Buffer // threshold/intensity/scale shared by bloom + present
	blurHUniform  gpu.Buffer // horizontal texel-offset uniform
	blurVUniform  gpu.Buffer // vertical texel-offset uniform
}

type bloomConfig struct {
	enabled   bool
	threshold float64
	intensity float64
	radius    float64
	scale     float64
}

// buildBloomPipelines constructs the three bloom pipelines (bright pass +
// two Gaussian blurs). Bind group layouts are captured for later bind group
// creation.
func (r *Renderer) buildBloomPipelines() error {
	if err := r.buildBrightPassPipeline(); err != nil {
		return err
	}
	if err := r.buildBlurPipeline(); err != nil {
		return err
	}
	return nil
}

func (r *Renderer) buildBrightPassPipeline() error {
	shader, err := r.device.CreateShaderModule(gpu.ShaderDesc{
		SourceWGSL: brightPassWGSL,
		Label:      "bundle.bloom.bright",
	})
	if err != nil {
		return fmt.Errorf("bundle.buildBrightPassPipeline: %w", err)
	}
	pipeline, err := r.device.CreateRenderPipeline(gpu.RenderPipelineDesc{
		Vertex: gpu.VertexStageDesc{Module: shader, EntryPoint: "vs_main"},
		Fragment: gpu.FragmentStageDesc{Module: shader, EntryPoint: "fs_main",
			Targets: []gpu.ColorTargetState{
				{Format: r.hdrFormat, WriteMask: gpu.ColorWriteAll},
			}},
		Primitive:  gpu.PrimitiveState{Topology: gpu.TopologyTriangleList, CullMode: gpu.CullNone},
		AutoLayout: true,
		Label:      "bundle.bloom.bright",
	})
	if err != nil {
		return fmt.Errorf("bundle.buildBrightPassPipeline: %w", err)
	}
	r.brightPipeline = pipeline
	r.brightBGLayout = pipeline.GetBindGroupLayout(0)
	return nil
}

func (r *Renderer) buildBlurPipeline() error {
	shader, err := r.device.CreateShaderModule(gpu.ShaderDesc{
		SourceWGSL: blurWGSL,
		Label:      "bundle.bloom.blur",
	})
	if err != nil {
		return fmt.Errorf("bundle.buildBlurPipeline: %w", err)
	}
	pipeline, err := r.device.CreateRenderPipeline(gpu.RenderPipelineDesc{
		Vertex: gpu.VertexStageDesc{Module: shader, EntryPoint: "vs_main"},
		Fragment: gpu.FragmentStageDesc{Module: shader, EntryPoint: "fs_main",
			Targets: []gpu.ColorTargetState{
				{Format: r.hdrFormat, WriteMask: gpu.ColorWriteAll},
			}},
		Primitive:  gpu.PrimitiveState{Topology: gpu.TopologyTriangleList, CullMode: gpu.CullNone},
		AutoLayout: true,
		Label:      "bundle.bloom.blur",
	})
	if err != nil {
		return fmt.Errorf("bundle.buildBlurPipeline: %w", err)
	}
	r.blurPipeline = pipeline
	r.blurBGLayout = pipeline.GetBindGroupLayout(0)
	return nil
}

// ensureBloom (re)allocates the bloom chain when the surface resizes. Two
// half-res HDR textures + three bind groups + two tiny uniform buffers are
// rebuilt; the old set is destroyed cleanly.
func (r *Renderer) ensureBloom(surfaceWidth, surfaceHeight int, cfg bloomConfig) error {
	w := max(1, int(float64(surfaceWidth)*cfg.scale))
	h := max(1, int(float64(surfaceHeight)*cfg.scale))

	if r.bloom != nil && r.bloom.width == w && r.bloom.height == h && r.bloom.surfaceWidth == surfaceWidth && r.bloom.surfaceHeight == surfaceHeight {
		return nil
	}
	if r.bloom != nil {
		destroyBloomResources(r.bloom)
		r.bloom = nil
	}

	texA, err := r.device.CreateTexture(gpu.TextureDesc{
		Width: w, Height: h, Format: r.hdrFormat,
		Usage: gpu.TextureUsageRenderAttachment | gpu.TextureUsageTextureBinding,
		Label: "bundle.bloom.A",
	})
	if err != nil {
		return fmt.Errorf("bundle.ensureBloom: %w", err)
	}
	texB, err := r.device.CreateTexture(gpu.TextureDesc{
		Width: w, Height: h, Format: r.hdrFormat,
		Usage: gpu.TextureUsageRenderAttachment | gpu.TextureUsageTextureBinding,
		Label: "bundle.bloom.B",
	})
	if err != nil {
		texA.Destroy()
		return fmt.Errorf("bundle.ensureBloom: %w", err)
	}
	paramsUniform, err := r.device.CreateBuffer(gpu.BufferDesc{
		Size: 16, Usage: gpu.BufferUsageUniform | gpu.BufferUsageCopyDst,
		Label: "bundle.bloom.params.uniform",
	})
	if err != nil {
		texA.Destroy()
		texB.Destroy()
		return fmt.Errorf("bundle.ensureBloom: %w", err)
	}
	blurHUniform, err := r.device.CreateBuffer(gpu.BufferDesc{
		Size: 16, Usage: gpu.BufferUsageUniform | gpu.BufferUsageCopyDst,
		Label: "bundle.bloom.blurH.uniform",
	})
	if err != nil {
		texA.Destroy()
		texB.Destroy()
		paramsUniform.Destroy()
		return fmt.Errorf("bundle.ensureBloom: %w", err)
	}
	blurVUniform, err := r.device.CreateBuffer(gpu.BufferDesc{
		Size: 16, Usage: gpu.BufferUsageUniform | gpu.BufferUsageCopyDst,
		Label: "bundle.bloom.blurV.uniform",
	})
	if err != nil {
		texA.Destroy()
		texB.Destroy()
		paramsUniform.Destroy()
		blurHUniform.Destroy()
		return fmt.Errorf("bundle.ensureBloom: %w", err)
	}

	viewA := texA.CreateView()
	viewB := texB.CreateView()

	brightBG, err := r.device.CreateBindGroup(gpu.BindGroupDesc{
		Layout: r.brightBGLayout,
		Entries: []gpu.BindGroupEntry{
			{Binding: 0, TextureView: r.hdrView},
			{Binding: 1, Sampler: r.presentSampler},
			{Binding: 2, Buffer: paramsUniform, Size: 16},
		},
		Label: "bundle.bloom.bright.bg",
	})
	if err != nil {
		texA.Destroy()
		texB.Destroy()
		paramsUniform.Destroy()
		blurHUniform.Destroy()
		blurVUniform.Destroy()
		return fmt.Errorf("bundle.ensureBloom: %w", err)
	}
	blurHBG, err := r.device.CreateBindGroup(gpu.BindGroupDesc{
		Layout: r.blurBGLayout,
		Entries: []gpu.BindGroupEntry{
			{Binding: 0, TextureView: viewA},
			{Binding: 1, Sampler: r.presentSampler},
			{Binding: 2, Buffer: blurHUniform, Size: 16},
		},
		Label: "bundle.bloom.blurH.bg",
	})
	if err != nil {
		texA.Destroy()
		texB.Destroy()
		paramsUniform.Destroy()
		blurHUniform.Destroy()
		blurVUniform.Destroy()
		return fmt.Errorf("bundle.ensureBloom: %w", err)
	}
	blurVBG, err := r.device.CreateBindGroup(gpu.BindGroupDesc{
		Layout: r.blurBGLayout,
		Entries: []gpu.BindGroupEntry{
			{Binding: 0, TextureView: viewB},
			{Binding: 1, Sampler: r.presentSampler},
			{Binding: 2, Buffer: blurVUniform, Size: 16},
		},
		Label: "bundle.bloom.blurV.bg",
	})
	if err != nil {
		texA.Destroy()
		texB.Destroy()
		paramsUniform.Destroy()
		blurHUniform.Destroy()
		blurVUniform.Destroy()
		return fmt.Errorf("bundle.ensureBloom: %w", err)
	}

	r.bloom = &bloomResources{
		width: w, height: h,
		surfaceWidth: surfaceWidth, surfaceHeight: surfaceHeight,
		texA: texA, texB: texB,
		viewA: viewA, viewB: viewB,
		brightBindGrp: brightBG,
		blurHBindGrp:  blurHBG,
		blurVBindGrp:  blurVBG,
		paramsUniform: paramsUniform,
		blurHUniform:  blurHUniform,
		blurVUniform:  blurVUniform,
	}

	// Rebuild the compose present bind group to reference the new viewA.
	bg, err := r.device.CreateBindGroup(gpu.BindGroupDesc{
		Layout: r.presentBGLayout,
		Entries: []gpu.BindGroupEntry{
			{Binding: 0, TextureView: r.hdrView},
			{Binding: 1, Sampler: r.presentSampler},
			{Binding: 2, TextureView: viewA},
			{Binding: 3, Sampler: r.presentSampler},
			{Binding: 4, Buffer: paramsUniform, Size: 16},
		},
		Label: "bundle.present.compose.bg",
	})
	if err != nil {
		return fmt.Errorf("bundle.ensureBloom (compose bg): %w", err)
	}
	r.presentBindGrp = bg
	return nil
}

func resolveBloomConfig(b engine.RenderBundle) bloomConfig {
	cfg := bloomConfig{
		threshold: defaultBloomThreshold,
		intensity: 0,
		radius:    defaultBloomRadius,
		scale:     defaultBloomScale,
	}
	for _, effect := range b.PostEffects {
		if !strings.EqualFold(strings.TrimSpace(effect.Kind), "bloom") {
			continue
		}
		cfg.enabled = true
		cfg.threshold = bloomEffectNumber(effect, "threshold", defaultBloomThreshold)
		cfg.intensity = bloomEffectNumber(effect, "intensity", defaultBloomIntensity, "strength")
		cfg.radius = bloomEffectNumber(effect, "radius", defaultBloomRadius)
		cfg.scale = bloomEffectNumber(effect, "scale", defaultBloomScale)
		if cfg.scale <= 0 || cfg.scale > 1 {
			cfg.scale = defaultBloomScale
		}
		return cfg
	}
	return cfg
}

func bloomEffectNumber(effect engine.RenderPostEffect, name string, fallback float64, aliases ...string) float64 {
	var direct float64
	switch name {
	case "threshold":
		direct = effect.Threshold
	case "intensity":
		direct = effect.Intensity
	case "radius":
		direct = effect.Radius
	case "scale":
		direct = effect.Scale
	}
	if direct > 0 {
		return direct
	}
	for _, key := range append([]string{name}, aliases...) {
		if value, ok := effect.Params[key]; ok && value > 0 {
			return value
		}
	}
	return fallback
}

func (r *Renderer) configureBloom(cfg bloomConfig) {
	if r.bloom == nil {
		return
	}
	intensity := cfg.intensity
	if !cfg.enabled {
		intensity = 0
	}
	r.device.Queue().WriteBuffer(r.bloom.paramsUniform, 0, float32sToBytes([]float32{
		float32(cfg.threshold),
		float32(intensity),
		float32(cfg.scale),
		0,
	}))

	radiusScale := cfg.radius / defaultBloomRadius
	if radiusScale <= 0 {
		radiusScale = 1
	}
	dx := float32(radiusScale) / float32(r.bloom.width)
	dy := float32(radiusScale) / float32(r.bloom.height)
	r.device.Queue().WriteBuffer(r.bloom.blurHUniform, 0, float32sToBytes([]float32{dx, 0, 0, 0}))
	r.device.Queue().WriteBuffer(r.bloom.blurVUniform, 0, float32sToBytes([]float32{0, dy, 0, 0}))
}

// recordBloomPasses runs the three bloom passes between the main HDR pass
// and the present pass. All passes render into half-resolution targets
// (bloom.texA / bloom.texB) and each pipeline only needs a single fullscreen
// triangle draw — ~1-2 ms on commodity hardware.
func (r *Renderer) recordBloomPasses(enc gpu.CommandEncoder) {
	if r.bloom == nil {
		return
	}
	// 1) Bright pass — HDR → bloom.texA.
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{
		ColorAttachments: []gpu.RenderPassColorAttachment{{
			View: r.bloom.viewA, LoadOp: gpu.LoadOpClear, StoreOp: gpu.StoreOpStore,
			ClearValue: gpu.Color{R: 0, G: 0, B: 0, A: 1},
		}},
		Label: "bundle.bloom.bright",
	})
	pass.SetPipeline(r.brightPipeline)
	pass.SetBindGroup(0, r.bloom.brightBindGrp)
	pass.Draw(3, 1, 0, 0)
	pass.End()

	// 2) Horizontal blur — bloom.texA → bloom.texB.
	pass = enc.BeginRenderPass(gpu.RenderPassDesc{
		ColorAttachments: []gpu.RenderPassColorAttachment{{
			View: r.bloom.viewB, LoadOp: gpu.LoadOpClear, StoreOp: gpu.StoreOpStore,
			ClearValue: gpu.Color{R: 0, G: 0, B: 0, A: 1},
		}},
		Label: "bundle.bloom.blurH",
	})
	pass.SetPipeline(r.blurPipeline)
	pass.SetBindGroup(0, r.bloom.blurHBindGrp)
	pass.Draw(3, 1, 0, 0)
	pass.End()

	// 3) Vertical blur — bloom.texB → bloom.texA (the present pass reads A).
	pass = enc.BeginRenderPass(gpu.RenderPassDesc{
		ColorAttachments: []gpu.RenderPassColorAttachment{{
			View: r.bloom.viewA, LoadOp: gpu.LoadOpClear, StoreOp: gpu.StoreOpStore,
			ClearValue: gpu.Color{R: 0, G: 0, B: 0, A: 1},
		}},
		Label: "bundle.bloom.blurV",
	})
	pass.SetPipeline(r.blurPipeline)
	pass.SetBindGroup(0, r.bloom.blurVBindGrp)
	pass.Draw(3, 1, 0, 0)
	pass.End()
}

func destroyBloomResources(b *bloomResources) {
	if b == nil {
		return
	}
	if b.texA != nil {
		b.texA.Destroy()
	}
	if b.texB != nil {
		b.texB.Destroy()
	}
	if b.paramsUniform != nil {
		b.paramsUniform.Destroy()
	}
	if b.blurHUniform != nil {
		b.blurHUniform.Destroy()
	}
	if b.blurVUniform != nil {
		b.blurVUniform.Destroy()
	}
}
