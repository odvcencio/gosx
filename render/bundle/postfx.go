package bundle

import (
	"fmt"

	"github.com/odvcencio/gosx/render/gpu"
)

// hdrFormat is the intermediate HDR color target format. RGBA16Float is the
// R4 baseline — plenty of headroom for physically-based lighting, no GPU
// native-compression tricks yet (R5 adds RGB9E5 / HDR10 compressed paths).
const hdrFormat = gpu.FormatRGBA16Float

// presentWGSL — historically the ACES-only present shader. Now that bloom
// is wired in, the actual present pipeline is built from composePresentWGSL
// (see bloom.go). Kept here for reference + as a fallback if we ever want
// to disable bloom via a config flag.

// presentResources groups the HDR sampler and the present bind group that
// binds the HDR texture view + sampler.
type presentResources struct {
	sampler gpu.Sampler
}

// buildPresentPipeline constructs the tone-mapping composite pipeline. The
// pipeline has no vertex buffers and four bind-group entries: HDR view +
// sampler, bloom view + sampler. The HDR and bloom views are rebuilt on
// every surface resize.
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
