package bundle

import (
	"fmt"
	"strings"

	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/render/gpu"
)

type nativePostFXKind int

const (
	nativePostFXSSAO nativePostFXKind = iota + 1
	nativePostFXDOF
	nativePostFXVignette
	nativePostFXColorGrade
)

type nativePostFXEffect struct {
	kind       nativePostFXKind
	intensity  float64
	radius     float64
	bias       float64
	focus      float64
	aperture   float64
	maxBlur    float64
	exposure   float64
	contrast   float64
	saturation float64
}

type nativePostFXResources struct {
	width, height int
	tex           gpu.Texture
	view          gpu.TextureView

	ssaoUniform       gpu.Buffer
	dofUniform        gpu.Buffer
	vignetteUniform   gpu.Buffer
	colorGradeUniform gpu.Buffer

	ssaoFromHDR           gpu.BindGroup
	ssaoFromScratch       gpu.BindGroup
	dofFromHDR            gpu.BindGroup
	dofFromScratch        gpu.BindGroup
	vignetteFromHDR       gpu.BindGroup
	vignetteFromScratch   gpu.BindGroup
	colorGradeFromHDR     gpu.BindGroup
	colorGradeFromScratch gpu.BindGroup
}

const ssaoWGSL = `
struct VSOut {
  @builtin(position) pos : vec4<f32>,
  @location(0) uv : vec2<f32>,
};

@group(0) @binding(0) var srcTexture : texture_2d<f32>;
@group(0) @binding(1) var srcSampler : sampler;
@group(0) @binding(2) var depthTexture : texture_depth_2d;

struct SSAOUniforms {
  params : vec4<f32>, // x=radius pixels, y=intensity, z=bias
};
@group(0) @binding(3) var<uniform> ssao : SSAOUniforms;

fn depthAt(uv : vec2<f32>) -> f32 {
  let dims = vec2<i32>(textureDimensions(depthTexture));
  let xy = clamp(vec2<i32>(uv * vec2<f32>(dims)), vec2<i32>(0, 0), dims - vec2<i32>(1, 1));
  return textureLoad(depthTexture, xy, 0);
}

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
  out.uv = uv[vid];
  return out;
}

@fragment
fn fs_main(in : VSOut) -> @location(0) vec4<f32> {
  let dims = vec2<f32>(textureDimensions(srcTexture, 0));
  let inv = 1.0 / max(dims, vec2<f32>(1.0, 1.0));
  let color = textureSample(srcTexture, srcSampler, in.uv).rgb;
  let centerDepth = depthAt(in.uv);
  let radius = max(ssao.params.x, 1.0);
  let intensity = max(ssao.params.y, 0.0);
  let bias = max(ssao.params.z, 0.0);
  var occlusion = 0.0;
  let offsets = array<vec2<f32>, 8>(
    vec2<f32>( 1.0,  0.0),
    vec2<f32>(-1.0,  0.0),
    vec2<f32>( 0.0,  1.0),
    vec2<f32>( 0.0, -1.0),
    vec2<f32>( 0.707,  0.707),
    vec2<f32>(-0.707,  0.707),
    vec2<f32>( 0.707, -0.707),
    vec2<f32>(-0.707, -0.707),
  );
  for (var i = 0; i < 8; i = i + 1) {
    let sampleDepth = depthAt(in.uv + offsets[i] * inv * radius);
    occlusion = occlusion + select(0.0, 1.0, sampleDepth < centerDepth - bias);
  }
  occlusion = clamp(occlusion / 8.0 * intensity, 0.0, 0.85);
  return vec4<f32>(color * (1.0 - occlusion), 1.0);
}
`

const dofWGSL = `
struct VSOut {
  @builtin(position) pos : vec4<f32>,
  @location(0) uv : vec2<f32>,
};

@group(0) @binding(0) var srcTexture : texture_2d<f32>;
@group(0) @binding(1) var srcSampler : sampler;
@group(0) @binding(2) var depthTexture : texture_depth_2d;

struct DOFUniforms {
  params : vec4<f32>, // x=focus depth, y=aperture, z=max blur pixels
};
@group(0) @binding(3) var<uniform> dof : DOFUniforms;

fn depthAt(uv : vec2<f32>) -> f32 {
  let dims = vec2<i32>(textureDimensions(depthTexture));
  let xy = clamp(vec2<i32>(uv * vec2<f32>(dims)), vec2<i32>(0, 0), dims - vec2<i32>(1, 1));
  return textureLoad(depthTexture, xy, 0);
}

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
  out.uv = uv[vid];
  return out;
}

@fragment
fn fs_main(in : VSOut) -> @location(0) vec4<f32> {
  let dims = vec2<f32>(textureDimensions(srcTexture, 0));
  let inv = 1.0 / max(dims, vec2<f32>(1.0, 1.0));
  let depth = depthAt(in.uv);
  let focusDepth = clamp(dof.params.x, 0.0, 1.0);
  let aperture = max(dof.params.y, 0.0);
  let maxBlur = max(dof.params.z, 0.0);
  let blur = clamp(abs(depth - focusDepth) * aperture * maxBlur, 0.0, maxBlur);
  var color = textureSample(srcTexture, srcSampler, in.uv).rgb * 0.30;
  color = color + textureSample(srcTexture, srcSampler, in.uv + inv * vec2<f32>( blur, 0.0)).rgb * 0.14;
  color = color + textureSample(srcTexture, srcSampler, in.uv + inv * vec2<f32>(-blur, 0.0)).rgb * 0.14;
  color = color + textureSample(srcTexture, srcSampler, in.uv + inv * vec2<f32>(0.0,  blur)).rgb * 0.14;
  color = color + textureSample(srcTexture, srcSampler, in.uv + inv * vec2<f32>(0.0, -blur)).rgb * 0.14;
  color = color + textureSample(srcTexture, srcSampler, in.uv + inv * vec2<f32>( blur,  blur)).rgb * 0.11;
  color = color + textureSample(srcTexture, srcSampler, in.uv + inv * vec2<f32>(-blur, -blur)).rgb * 0.11;
  return vec4<f32>(color, 1.0);
}
`

const vignetteWGSL = `
struct VSOut {
  @builtin(position) pos : vec4<f32>,
  @location(0) uv : vec2<f32>,
};

@group(0) @binding(0) var srcTexture : texture_2d<f32>;
@group(0) @binding(1) var srcSampler : sampler;
@group(0) @binding(2) var depthTexture : texture_depth_2d;

struct VignetteUniforms {
  params : vec4<f32>, // x=intensity
};
@group(0) @binding(3) var<uniform> vignette : VignetteUniforms;

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
  out.uv = uv[vid];
  return out;
}

@fragment
fn fs_main(in : VSOut) -> @location(0) vec4<f32> {
  let color = textureSample(srcTexture, srcSampler, in.uv).rgb;
  let center = in.uv - vec2<f32>(0.5, 0.5);
  let dist = length(center);
  let amount = max(vignette.params.x, 0.0);
  let v = 1.0 - smoothstep(0.3, 0.7, dist * amount);
  return vec4<f32>(color * v, 1.0);
}
`

const colorGradeWGSL = `
struct VSOut {
  @builtin(position) pos : vec4<f32>,
  @location(0) uv : vec2<f32>,
};

@group(0) @binding(0) var srcTexture : texture_2d<f32>;
@group(0) @binding(1) var srcSampler : sampler;
@group(0) @binding(2) var depthTexture : texture_depth_2d;

struct ColorGradeUniforms {
  params : vec4<f32>, // x=exposure, y=contrast, z=saturation
};
@group(0) @binding(3) var<uniform> grade : ColorGradeUniforms;

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
  out.uv = uv[vid];
  return out;
}

@fragment
fn fs_main(in : VSOut) -> @location(0) vec4<f32> {
  var color = textureSample(srcTexture, srcSampler, in.uv).rgb;
  color = color * max(grade.params.x, 0.0);
  color = mix(vec3<f32>(0.5, 0.5, 0.5), color, max(grade.params.y, 0.0));
  let gray = dot(color, vec3<f32>(0.2126, 0.7152, 0.0722));
  color = mix(vec3<f32>(gray, gray, gray), color, max(grade.params.z, 0.0));
  return vec4<f32>(clamp(color, vec3<f32>(0.0), vec3<f32>(1.0)), 1.0);
}
`

func (r *Renderer) buildNativePostFXPipelines() error {
	if err := r.buildSSAOPipeline(); err != nil {
		return err
	}
	if err := r.buildDOFPipeline(); err != nil {
		return err
	}
	if err := r.buildVignettePipeline(); err != nil {
		return err
	}
	return r.buildColorGradePipeline()
}

func (r *Renderer) buildSSAOPipeline() error {
	shader, err := r.device.CreateShaderModule(gpu.ShaderDesc{SourceWGSL: ssaoWGSL, Label: "bundle.postfx.ssao"})
	if err != nil {
		return fmt.Errorf("bundle.buildSSAOPipeline: %w", err)
	}
	pipeline, err := r.device.CreateRenderPipeline(gpu.RenderPipelineDesc{
		Vertex:   gpu.VertexStageDesc{Module: shader, EntryPoint: "vs_main"},
		Fragment: gpu.FragmentStageDesc{Module: shader, EntryPoint: "fs_main", Targets: []gpu.ColorTargetState{{Format: r.hdrFormat, WriteMask: gpu.ColorWriteAll}}},
		Primitive: gpu.PrimitiveState{
			Topology: gpu.TopologyTriangleList,
			CullMode: gpu.CullNone,
		},
		AutoLayout: true,
		Label:      "bundle.postfx.ssao",
	})
	if err != nil {
		return fmt.Errorf("bundle.buildSSAOPipeline: %w", err)
	}
	r.ssaoPipeline = pipeline
	r.ssaoBGLayout = pipeline.GetBindGroupLayout(0)
	return nil
}

func (r *Renderer) buildDOFPipeline() error {
	shader, err := r.device.CreateShaderModule(gpu.ShaderDesc{SourceWGSL: dofWGSL, Label: "bundle.postfx.dof"})
	if err != nil {
		return fmt.Errorf("bundle.buildDOFPipeline: %w", err)
	}
	pipeline, err := r.device.CreateRenderPipeline(gpu.RenderPipelineDesc{
		Vertex:   gpu.VertexStageDesc{Module: shader, EntryPoint: "vs_main"},
		Fragment: gpu.FragmentStageDesc{Module: shader, EntryPoint: "fs_main", Targets: []gpu.ColorTargetState{{Format: r.hdrFormat, WriteMask: gpu.ColorWriteAll}}},
		Primitive: gpu.PrimitiveState{
			Topology: gpu.TopologyTriangleList,
			CullMode: gpu.CullNone,
		},
		AutoLayout: true,
		Label:      "bundle.postfx.dof",
	})
	if err != nil {
		return fmt.Errorf("bundle.buildDOFPipeline: %w", err)
	}
	r.dofPipeline = pipeline
	r.dofBGLayout = pipeline.GetBindGroupLayout(0)
	return nil
}

func (r *Renderer) buildVignettePipeline() error {
	shader, err := r.device.CreateShaderModule(gpu.ShaderDesc{SourceWGSL: vignetteWGSL, Label: "bundle.postfx.vignette"})
	if err != nil {
		return fmt.Errorf("bundle.buildVignettePipeline: %w", err)
	}
	pipeline, err := r.device.CreateRenderPipeline(gpu.RenderPipelineDesc{
		Vertex:   gpu.VertexStageDesc{Module: shader, EntryPoint: "vs_main"},
		Fragment: gpu.FragmentStageDesc{Module: shader, EntryPoint: "fs_main", Targets: []gpu.ColorTargetState{{Format: r.hdrFormat, WriteMask: gpu.ColorWriteAll}}},
		Primitive: gpu.PrimitiveState{
			Topology: gpu.TopologyTriangleList,
			CullMode: gpu.CullNone,
		},
		AutoLayout: true,
		Label:      "bundle.postfx.vignette",
	})
	if err != nil {
		return fmt.Errorf("bundle.buildVignettePipeline: %w", err)
	}
	r.vignettePipeline = pipeline
	r.vignetteBGLayout = pipeline.GetBindGroupLayout(0)
	return nil
}

func (r *Renderer) buildColorGradePipeline() error {
	shader, err := r.device.CreateShaderModule(gpu.ShaderDesc{SourceWGSL: colorGradeWGSL, Label: "bundle.postfx.colorGrade"})
	if err != nil {
		return fmt.Errorf("bundle.buildColorGradePipeline: %w", err)
	}
	pipeline, err := r.device.CreateRenderPipeline(gpu.RenderPipelineDesc{
		Vertex:   gpu.VertexStageDesc{Module: shader, EntryPoint: "vs_main"},
		Fragment: gpu.FragmentStageDesc{Module: shader, EntryPoint: "fs_main", Targets: []gpu.ColorTargetState{{Format: r.hdrFormat, WriteMask: gpu.ColorWriteAll}}},
		Primitive: gpu.PrimitiveState{
			Topology: gpu.TopologyTriangleList,
			CullMode: gpu.CullNone,
		},
		AutoLayout: true,
		Label:      "bundle.postfx.colorGrade",
	})
	if err != nil {
		return fmt.Errorf("bundle.buildColorGradePipeline: %w", err)
	}
	r.colorGradePipeline = pipeline
	r.colorGradeBGLayout = pipeline.GetBindGroupLayout(0)
	return nil
}

func (r *Renderer) ensureNativePostFX(width, height int, depthView gpu.TextureView) (bool, error) {
	resized := false
	if r.nativePostFX == nil || r.nativePostFX.width != width || r.nativePostFX.height != height {
		destroyNativePostFXResources(r.nativePostFX)
		tex, err := r.device.CreateTexture(gpu.TextureDesc{
			Width:  width,
			Height: height,
			Format: r.hdrFormat,
			Usage:  gpu.TextureUsageRenderAttachment | gpu.TextureUsageTextureBinding,
			Label:  "bundle.nativePostFX.hdr",
		})
		if err != nil {
			return false, fmt.Errorf("bundle.ensureNativePostFX: %w", err)
		}
		ssaoUniform, err := r.device.CreateBuffer(gpu.BufferDesc{
			Size:  16,
			Usage: gpu.BufferUsageUniform | gpu.BufferUsageCopyDst,
			Label: "bundle.postfx.ssao.uniform",
		})
		if err != nil {
			tex.Destroy()
			return false, fmt.Errorf("bundle.ensureNativePostFX: %w", err)
		}
		dofUniform, err := r.device.CreateBuffer(gpu.BufferDesc{
			Size:  16,
			Usage: gpu.BufferUsageUniform | gpu.BufferUsageCopyDst,
			Label: "bundle.postfx.dof.uniform",
		})
		if err != nil {
			tex.Destroy()
			ssaoUniform.Destroy()
			return false, fmt.Errorf("bundle.ensureNativePostFX: %w", err)
		}
		vignetteUniform, err := r.device.CreateBuffer(gpu.BufferDesc{
			Size:  16,
			Usage: gpu.BufferUsageUniform | gpu.BufferUsageCopyDst,
			Label: "bundle.postfx.vignette.uniform",
		})
		if err != nil {
			tex.Destroy()
			ssaoUniform.Destroy()
			dofUniform.Destroy()
			return false, fmt.Errorf("bundle.ensureNativePostFX: %w", err)
		}
		colorGradeUniform, err := r.device.CreateBuffer(gpu.BufferDesc{
			Size:  16,
			Usage: gpu.BufferUsageUniform | gpu.BufferUsageCopyDst,
			Label: "bundle.postfx.colorGrade.uniform",
		})
		if err != nil {
			tex.Destroy()
			ssaoUniform.Destroy()
			dofUniform.Destroy()
			vignetteUniform.Destroy()
			return false, fmt.Errorf("bundle.ensureNativePostFX: %w", err)
		}
		r.nativePostFX = &nativePostFXResources{
			width:             width,
			height:            height,
			tex:               tex,
			view:              tex.CreateView(),
			ssaoUniform:       ssaoUniform,
			dofUniform:        dofUniform,
			vignetteUniform:   vignetteUniform,
			colorGradeUniform: colorGradeUniform,
		}
		resized = true
	}
	if err := r.rebuildNativePostFXBindGroups(depthView); err != nil {
		return resized, err
	}
	return resized, nil
}

func (r *Renderer) rebuildNativePostFXBindGroups(depthView gpu.TextureView) error {
	res := r.nativePostFX
	if res == nil {
		return nil
	}
	destroyNativePostFXBindGroups(res)
	var err error
	res.ssaoFromHDR, err = r.createNativePostFXBindGroup(r.ssaoBGLayout, r.hdrView, depthView, res.ssaoUniform, "bundle.postfx.ssao.bg.hdr")
	if err != nil {
		return err
	}
	res.ssaoFromScratch, err = r.createNativePostFXBindGroup(r.ssaoBGLayout, res.view, depthView, res.ssaoUniform, "bundle.postfx.ssao.bg.scratch")
	if err != nil {
		return err
	}
	res.dofFromHDR, err = r.createNativePostFXBindGroup(r.dofBGLayout, r.hdrView, depthView, res.dofUniform, "bundle.postfx.dof.bg.hdr")
	if err != nil {
		return err
	}
	res.dofFromScratch, err = r.createNativePostFXBindGroup(r.dofBGLayout, res.view, depthView, res.dofUniform, "bundle.postfx.dof.bg.scratch")
	if err != nil {
		return err
	}
	res.vignetteFromHDR, err = r.createNativePostFXBindGroup(r.vignetteBGLayout, r.hdrView, depthView, res.vignetteUniform, "bundle.postfx.vignette.bg.hdr")
	if err != nil {
		return err
	}
	res.vignetteFromScratch, err = r.createNativePostFXBindGroup(r.vignetteBGLayout, res.view, depthView, res.vignetteUniform, "bundle.postfx.vignette.bg.scratch")
	if err != nil {
		return err
	}
	res.colorGradeFromHDR, err = r.createNativePostFXBindGroup(r.colorGradeBGLayout, r.hdrView, depthView, res.colorGradeUniform, "bundle.postfx.colorGrade.bg.hdr")
	if err != nil {
		return err
	}
	res.colorGradeFromScratch, err = r.createNativePostFXBindGroup(r.colorGradeBGLayout, res.view, depthView, res.colorGradeUniform, "bundle.postfx.colorGrade.bg.scratch")
	if err != nil {
		return err
	}
	return nil
}

func (r *Renderer) createNativePostFXBindGroup(layout gpu.BindGroupLayout, source, depth gpu.TextureView, uniform gpu.Buffer, label string) (gpu.BindGroup, error) {
	bg, err := r.device.CreateBindGroup(gpu.BindGroupDesc{
		Layout: layout,
		Entries: []gpu.BindGroupEntry{
			{Binding: 0, TextureView: source},
			{Binding: 1, Sampler: r.presentSampler},
			{Binding: 2, TextureView: depth},
			{Binding: 3, Buffer: uniform, Size: 16},
		},
		Label: label,
	})
	if err != nil {
		return nil, fmt.Errorf("bundle.createNativePostFXBindGroup: %w", err)
	}
	return bg, nil
}

func resolveNativePostFXEffects(b engine.RenderBundle) []nativePostFXEffect {
	effects := make([]nativePostFXEffect, 0, len(b.PostEffects))
	seenSSAO := false
	seenDOF := false
	seenVignette := false
	seenColorGrade := false
	for _, effect := range b.PostEffects {
		switch strings.ToLower(strings.TrimSpace(effect.Kind)) {
		case "ssao":
			if seenSSAO {
				continue
			}
			seenSSAO = true
			effects = append(effects, nativePostFXEffect{
				kind:      nativePostFXSSAO,
				radius:    postEffectNumber(effect, "radius", 6),
				intensity: postEffectNumber(effect, "intensity", 0.6, "strength"),
				bias:      postEffectNumber(effect, "bias", 0.001),
			})
		case "dof":
			if seenDOF {
				continue
			}
			seenDOF = true
			effects = append(effects, nativePostFXEffect{
				kind:     nativePostFXDOF,
				focus:    postEffectNumber(effect, "focusDistance", 6),
				aperture: postEffectNumber(effect, "aperture", 0.05),
				maxBlur:  postEffectNumber(effect, "maxBlur", 5),
			})
		case "vignette":
			if seenVignette {
				continue
			}
			seenVignette = true
			effects = append(effects, nativePostFXEffect{
				kind:      nativePostFXVignette,
				intensity: postEffectNumber(effect, "intensity", 1),
			})
		case "colorgrade", "color-grade":
			if seenColorGrade {
				continue
			}
			seenColorGrade = true
			effects = append(effects, nativePostFXEffect{
				kind:       nativePostFXColorGrade,
				exposure:   postEffectNumber(effect, "exposure", 1),
				contrast:   postEffectNumber(effect, "contrast", 1),
				saturation: postEffectNumber(effect, "saturation", 1),
			})
		}
	}
	return effects
}

func postEffectNumber(effect engine.RenderPostEffect, name string, fallback float64, aliases ...string) float64 {
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

func (r *Renderer) configureNativePostFX(effects []nativePostFXEffect) {
	if r.nativePostFX == nil {
		return
	}
	for _, effect := range effects {
		switch effect.kind {
		case nativePostFXSSAO:
			r.device.Queue().WriteBuffer(r.nativePostFX.ssaoUniform, 0, float32sToBytes([]float32{
				float32(maxFloat(effect.radius, 1)),
				float32(maxFloat(effect.intensity, 0)),
				float32(maxFloat(effect.bias, 0)),
				0,
			}))
		case nativePostFXDOF:
			focusDepth := effect.focus / (effect.focus + 1)
			if focusDepth < 0 {
				focusDepth = 0
			}
			if focusDepth > 1 {
				focusDepth = 1
			}
			r.device.Queue().WriteBuffer(r.nativePostFX.dofUniform, 0, float32sToBytes([]float32{
				float32(focusDepth),
				float32(maxFloat(effect.aperture, 0)),
				float32(maxFloat(effect.maxBlur, 0)),
				0,
			}))
		case nativePostFXVignette:
			r.device.Queue().WriteBuffer(r.nativePostFX.vignetteUniform, 0, float32sToBytes([]float32{
				float32(maxFloat(effect.intensity, 0)),
				0,
				0,
				0,
			}))
		case nativePostFXColorGrade:
			r.device.Queue().WriteBuffer(r.nativePostFX.colorGradeUniform, 0, float32sToBytes([]float32{
				float32(maxFloat(effect.exposure, 0)),
				float32(maxFloat(effect.contrast, 0)),
				float32(maxFloat(effect.saturation, 0)),
				0,
			}))
		}
	}
}

func (r *Renderer) recordNativePostFXPasses(enc gpu.CommandEncoder, effects []nativePostFXEffect) (gpu.TextureView, bool) {
	if len(effects) == 0 || r.nativePostFX == nil {
		return r.hdrView, false
	}
	sourceScratch := false
	for _, effect := range effects {
		outputView := r.nativePostFX.view
		if sourceScratch {
			outputView = r.hdrView
		}
		label := "bundle.postfx"
		var pipeline gpu.RenderPipeline
		var bg gpu.BindGroup
		switch effect.kind {
		case nativePostFXSSAO:
			label = "bundle.postfx.ssao"
			pipeline = r.ssaoPipeline
			if sourceScratch {
				bg = r.nativePostFX.ssaoFromScratch
			} else {
				bg = r.nativePostFX.ssaoFromHDR
			}
		case nativePostFXDOF:
			label = "bundle.postfx.dof"
			pipeline = r.dofPipeline
			if sourceScratch {
				bg = r.nativePostFX.dofFromScratch
			} else {
				bg = r.nativePostFX.dofFromHDR
			}
		case nativePostFXVignette:
			label = "bundle.postfx.vignette"
			pipeline = r.vignettePipeline
			if sourceScratch {
				bg = r.nativePostFX.vignetteFromScratch
			} else {
				bg = r.nativePostFX.vignetteFromHDR
			}
		case nativePostFXColorGrade:
			label = "bundle.postfx.colorGrade"
			pipeline = r.colorGradePipeline
			if sourceScratch {
				bg = r.nativePostFX.colorGradeFromScratch
			} else {
				bg = r.nativePostFX.colorGradeFromHDR
			}
		default:
			continue
		}
		pass := enc.BeginRenderPass(gpu.RenderPassDesc{
			ColorAttachments: []gpu.RenderPassColorAttachment{{
				View:       outputView,
				LoadOp:     gpu.LoadOpClear,
				StoreOp:    gpu.StoreOpStore,
				ClearValue: gpu.Color{R: 0, G: 0, B: 0, A: 1},
			}},
			Label: label,
		})
		pass.SetPipeline(pipeline)
		pass.SetBindGroup(0, bg)
		pass.Draw(3, 1, 0, 0)
		pass.End()
		sourceScratch = !sourceScratch
	}
	if sourceScratch {
		return r.nativePostFX.view, true
	}
	return r.hdrView, false
}

func maxFloat(value, fallback float64) float64 {
	if value > fallback {
		return value
	}
	return fallback
}

func destroyNativePostFXResources(res *nativePostFXResources) {
	if res == nil {
		return
	}
	destroyNativePostFXBindGroups(res)
	if res.tex != nil {
		res.tex.Destroy()
	}
	if res.ssaoUniform != nil {
		res.ssaoUniform.Destroy()
	}
	if res.dofUniform != nil {
		res.dofUniform.Destroy()
	}
	if res.vignetteUniform != nil {
		res.vignetteUniform.Destroy()
	}
	if res.colorGradeUniform != nil {
		res.colorGradeUniform.Destroy()
	}
}

func destroyNativePostFXBindGroups(res *nativePostFXResources) {
	if res == nil {
		return
	}
	for _, bg := range []gpu.BindGroup{
		res.ssaoFromHDR, res.ssaoFromScratch,
		res.dofFromHDR, res.dofFromScratch,
		res.vignetteFromHDR, res.vignetteFromScratch,
		res.colorGradeFromHDR, res.colorGradeFromScratch,
	} {
		if bg != nil {
			bg.Destroy()
		}
	}
	res.ssaoFromHDR = nil
	res.ssaoFromScratch = nil
	res.dofFromHDR = nil
	res.dofFromScratch = nil
	res.vignetteFromHDR = nil
	res.vignetteFromScratch = nil
	res.colorGradeFromHDR = nil
	res.colorGradeFromScratch = nil
}
