package bundle

import (
	"fmt"
	"strings"

	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/render/gpu"
)

const (
	defaultBloomThreshold = 0.8
	defaultBloomIntensity = 0.5
	defaultBloomRadius    = 5.0
	defaultBloomScale     = 0.5
)

// brightPassWGSL selects the part of the HDR image that blooms and writes it to
// a half-res target that feeds the blur chain. One bilinear sample per output
// texel does the downsample, because the target is half the size of the source.
//
// The threshold is a soft knee, not a cut. The shader subtracts the authored
// threshold from the luminance and scales the colour by t/(t+1). Keep it.
//
// WHY THE KNEE. A hard cut is not continuous at the dial: a pixel one part in a
// thousand under the threshold contributes nothing, and the same pixel one part
// over contributes its full colour. A slow camera move then makes a highlight
// snap on. The knee crosses zero smoothly, so the same move fades it in.
//
// The browser copy, WGSL_POST_BLOOM_BRIGHT_FRAGMENT in
// client/js/bootstrap-src/16a-scene-webgpu.js, used the hard cut and adopted the
// knee on 2026-07-27. Both copies now scale by excess/(excess + 1.0), so the
// term is an agreement rather than a difference.
//
// The bright-pass-soft-knee row of brightSharedTerms in
// render/bundle/postfx_drift_test.go pins both copies. Either side reverting to
// a cut fails there.
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

// composePresentWGSL samples the HDR target and the blurred bloom target, adds
// them, and runs the authored tone-map operator into an LDR intermediate.
// Anti-aliasing is intentionally not folded into this shader; the following
// FXAA pass evaluates final display luminance.
//
// This shader is the specification for the CPU path. newComposePresentFragment in
// render/gpu/headless/postfx.go evaluates the same terms per pixel, so a
// headless capture and a poster frame carry the authored curve.
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

struct PresentUniforms {
  params : vec4<f32>, // x = tone-map mode, y = exposure
};
@group(0) @binding(5) var<uniform> present : PresentUniforms;

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

fn reinhardToneMap(x : vec3<f32>) -> vec3<f32> {
  return x / (vec3<f32>(1.0) + x);
}

fn filmicToneMap(x : vec3<f32>) -> vec3<f32> {
  let c = max(vec3<f32>(0.0), x - vec3<f32>(0.004));
  return clamp((c * (6.2 * c + 0.5)) / (c * (6.2 * c + 1.7) + 0.06),
               vec3<f32>(0.0), vec3<f32>(1.0));
}

// applyToneMap selects one operator by the mode lane of the present uniform.
// The mode numbers are the browser numbers, so an authored string reaches the
// same operator on both backends. Read toneMapModeCode below for the table.
//
// Mode 0 is the clamp. It exists because an author who writes
// Environment.ToneMapping = "none" asks for no curve. This shader used to fold
// every unknown mode to ACES, so "none" applied a full filmic curve natively
// and a clamp in the browser. The same scene then read darker on the server.
//
// The two max() calls guard a negative exposure and a negative input. Neither
// can occur today, because configureToneMap forces the exposure above zero and
// the lit pass writes no negative colour.
fn applyToneMap(x : vec3<f32>) -> vec3<f32> {
  let exposed = max(x * max(present.params.y, 0.0), vec3<f32>(0.0));
  let mode = i32(present.params.x);
  if (mode == 0) {
    return clamp(exposed, vec3<f32>(0.0), vec3<f32>(1.0));
  } else if (mode == 2) {
    return reinhardToneMap(exposed);
  } else if (mode == 3) {
    return filmicToneMap(exposed);
  }
  return acesFilmic(exposed);
}

fn toneMapAt(uv : vec2<f32>) -> vec3<f32> {
  let hdr   = textureSample(hdrTexture, hdrSampler, uv).rgb;
  let glow = textureSample(bloomTexture, bloomSampler, uv).rgb;
  return applyToneMap(hdr + glow * bloom.params.y);
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

	paramsUniform  gpu.Buffer // threshold/intensity/scale shared by bloom + present
	presentUniform gpu.Buffer // tone-map mode/exposure used by the compose pass
	blurHUniform   gpu.Buffer // horizontal texel-offset uniform
	blurVUniform   gpu.Buffer // vertical texel-offset uniform

	// Last value written to each uniform above. The caches live here so a
	// rebuilt bloom chain starts with them invalid.
	paramsCache  vec4Cache
	presentCache vec4Cache
	blurHCache   vec4Cache
	blurVCache   vec4Cache
}

type bloomConfig struct {
	enabled   bool
	threshold float64
	intensity float64
	radius    float64
	scale     float64
}

type toneMapConfig struct {
	mode     string
	exposure float64
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
	presentUniform, err := r.device.CreateBuffer(gpu.BufferDesc{
		Size: 16, Usage: gpu.BufferUsageUniform | gpu.BufferUsageCopyDst,
		Label: "bundle.present.tonemap.uniform",
	})
	if err != nil {
		texA.Destroy()
		texB.Destroy()
		paramsUniform.Destroy()
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
		presentUniform.Destroy()
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
		presentUniform.Destroy()
		blurHUniform.Destroy()
		return fmt.Errorf("bundle.ensureBloom: %w", err)
	}

	viewA := texA.CreateView()
	viewB := texB.CreateView()
	next := &bloomResources{
		width: w, height: h,
		surfaceWidth: surfaceWidth, surfaceHeight: surfaceHeight,
		texA:           texA,
		texB:           texB,
		viewA:          viewA,
		viewB:          viewB,
		paramsUniform:  paramsUniform,
		presentUniform: presentUniform,
		blurHUniform:   blurHUniform,
		blurVUniform:   blurVUniform,
	}

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
		destroyBloomResources(next)
		return fmt.Errorf("bundle.ensureBloom: %w", err)
	}
	next.brightBindGrp = brightBG
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
		destroyBloomResources(next)
		return fmt.Errorf("bundle.ensureBloom: %w", err)
	}
	next.blurHBindGrp = blurHBG
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
		destroyBloomResources(next)
		return fmt.Errorf("bundle.ensureBloom: %w", err)
	}
	next.blurVBindGrp = blurVBG

	// Rebuild the compose present bind group to reference the new viewA.
	bg, err := r.device.CreateBindGroup(gpu.BindGroupDesc{
		Layout: r.presentBGLayout,
		Entries: []gpu.BindGroupEntry{
			{Binding: 0, TextureView: r.hdrView},
			{Binding: 1, Sampler: r.presentSampler},
			{Binding: 2, TextureView: viewA},
			{Binding: 3, Sampler: r.presentSampler},
			{Binding: 4, Buffer: paramsUniform, Size: 16},
			{Binding: 5, Buffer: presentUniform, Size: 16},
		},
		Label: "bundle.present.compose.bg",
	})
	if err != nil {
		destroyBloomResources(next)
		return fmt.Errorf("bundle.ensureBloom (compose bg): %w", err)
	}
	if r.bloom != nil {
		destroyBloomResources(r.bloom)
	}
	if r.presentBindGrp != nil {
		r.presentBindGrp.Destroy()
	}
	r.bloom = next
	r.presentBindGrp = bg
	r.bloomSourceKey = "hdr"
	return nil
}

func (r *Renderer) ensureBloomSourceBindGroups(source gpu.TextureView, sourceKey string) error {
	if r.bloom == nil {
		return nil
	}
	if sourceKey == "" {
		sourceKey = "hdr"
	}
	if r.bloomSourceKey == sourceKey && r.bloom.brightBindGrp != nil && r.presentBindGrp != nil {
		return nil
	}
	brightBG, err := r.device.CreateBindGroup(gpu.BindGroupDesc{
		Layout: r.brightBGLayout,
		Entries: []gpu.BindGroupEntry{
			{Binding: 0, TextureView: source},
			{Binding: 1, Sampler: r.presentSampler},
			{Binding: 2, Buffer: r.bloom.paramsUniform, Size: 16},
		},
		Label: "bundle.bloom.bright.bg",
	})
	if err != nil {
		return fmt.Errorf("bundle.ensureBloomSourceBindGroups: %w", err)
	}
	presentBG, err := r.device.CreateBindGroup(gpu.BindGroupDesc{
		Layout: r.presentBGLayout,
		Entries: []gpu.BindGroupEntry{
			{Binding: 0, TextureView: source},
			{Binding: 1, Sampler: r.presentSampler},
			{Binding: 2, TextureView: r.bloom.viewA},
			{Binding: 3, Sampler: r.presentSampler},
			{Binding: 4, Buffer: r.bloom.paramsUniform, Size: 16},
			{Binding: 5, Buffer: r.bloom.presentUniform, Size: 16},
		},
		Label: "bundle.present.compose.bg",
	})
	if err != nil {
		brightBG.Destroy()
		return fmt.Errorf("bundle.ensureBloomSourceBindGroups: %w", err)
	}
	if r.bloom.brightBindGrp != nil {
		r.bloom.brightBindGrp.Destroy()
	}
	if r.presentBindGrp != nil {
		r.presentBindGrp.Destroy()
	}
	r.bloom.brightBindGrp = brightBG
	r.presentBindGrp = presentBG
	r.bloomSourceKey = sourceKey
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

func resolveToneMapConfig(b engine.RenderBundle) toneMapConfig {
	cfg := toneMapConfig{
		mode:     strings.TrimSpace(b.Environment.ToneMapping),
		exposure: b.Environment.Exposure,
	}
	if cfg.mode == "" {
		cfg.mode = "aces"
	}
	if cfg.exposure <= 0 {
		cfg.exposure = 1
	}
	for _, effect := range b.PostEffects {
		kind := strings.ToLower(strings.TrimSpace(effect.Kind))
		if kind != "tonemapping" && kind != "tonemap" && kind != "tone-mapping" {
			continue
		}
		if strings.TrimSpace(effect.Mode) != "" {
			cfg.mode = strings.TrimSpace(effect.Mode)
		}
		if value, ok := effect.Params["exposure"]; ok && value > 0 {
			cfg.exposure = value
		}
		return cfg
	}
	return cfg
}

// toneMapModeCode turns an authored tone-map name into the mode lane of the
// present uniform.
//
// The numbers are the browser numbers. Three browser functions use exactly this
// table, so one authored string reaches one operator on every backend:
//
//   - 0 — "linear" or "none": clamp only, no curve.
//   - 1 — anything else, including the empty string: ACES filmic.
//   - 2 — "reinhard".
//   - 3 — "filmic".
//
// The three are sceneWebGPUToneMapMode in 16a-scene-webgpu.js, and both
// scenePostToneMapMode and sceneToneMapMode in 16-scene-webgl.js. The last of
// the three joined the table on 2026-07-27; before that it mapped neither
// "none" nor "filmic" and it skipped the trim, so a WebGL2 page without a post
// chain answered differently from the same page with one.
//
// This function used to return 0 for every unknown name, and 0 used to mean
// ACES. "none" therefore applied a full ACES curve natively while the browser
// only clamped. TestToneMapModeTablesAgreeAcrossAllFourCopies pins all four
// copies, and TestBrowserToneMapTablesNormalizeTheAuthoredName pins the trim.
func toneMapModeCode(mode string) float32 {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "linear", "none":
		return 0
	case "reinhard":
		return 2
	case "filmic":
		return 3
	default:
		return 1
	}
}

func (r *Renderer) configureBloom(cfg bloomConfig) {
	if r.bloom == nil {
		return
	}
	intensity := cfg.intensity
	if !cfg.enabled {
		intensity = 0
	}
	r.writeVec4IfChanged(&r.bloom.paramsCache, r.bloom.paramsUniform,
		float32(cfg.threshold), float32(intensity), float32(cfg.scale), 0)

	radiusScale := cfg.radius / defaultBloomRadius
	if radiusScale <= 0 {
		radiusScale = 1
	}
	dx := float32(radiusScale) / float32(r.bloom.width)
	dy := float32(radiusScale) / float32(r.bloom.height)
	r.writeVec4IfChanged(&r.bloom.blurHCache, r.bloom.blurHUniform, dx, 0, 0, 0)
	r.writeVec4IfChanged(&r.bloom.blurVCache, r.bloom.blurVUniform, 0, dy, 0, 0)
}

func (r *Renderer) configureToneMap(cfg toneMapConfig) {
	if r.bloom == nil || r.bloom.presentUniform == nil {
		return
	}
	exposure := cfg.exposure
	if exposure <= 0 {
		exposure = 1
	}
	r.writeVec4IfChanged(&r.bloom.presentCache, r.bloom.presentUniform,
		toneMapModeCode(cfg.mode), float32(exposure), 0, 0)
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
		ColorAttachments: r.postColorAttachments(postSlotBloomBright, r.bloom.viewA),
		Label:            "bundle.bloom.bright",
	})
	pass.SetPipeline(r.brightPipeline)
	pass.SetBindGroup(0, r.bloom.brightBindGrp)
	pass.Draw(3, 1, 0, 0)
	pass.End()

	// 2) Horizontal blur — bloom.texA → bloom.texB.
	pass = enc.BeginRenderPass(gpu.RenderPassDesc{
		ColorAttachments: r.postColorAttachments(postSlotBloomBlurH, r.bloom.viewB),
		Label:            "bundle.bloom.blurH",
	})
	pass.SetPipeline(r.blurPipeline)
	pass.SetBindGroup(0, r.bloom.blurHBindGrp)
	pass.Draw(3, 1, 0, 0)
	pass.End()

	// 3) Vertical blur — bloom.texB → bloom.texA (the present pass reads A).
	pass = enc.BeginRenderPass(gpu.RenderPassDesc{
		ColorAttachments: r.postColorAttachments(postSlotBloomBlurV, r.bloom.viewA),
		Label:            "bundle.bloom.blurV",
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
	if b.presentUniform != nil {
		b.presentUniform.Destroy()
	}
	if b.blurHUniform != nil {
		b.blurHUniform.Destroy()
	}
	if b.blurVUniform != nil {
		b.blurVUniform.Destroy()
	}
	if b.brightBindGrp != nil {
		b.brightBindGrp.Destroy()
	}
	if b.blurHBindGrp != nil {
		b.blurHBindGrp.Destroy()
	}
	if b.blurVBindGrp != nil {
		b.blurVBindGrp.Destroy()
	}
}
