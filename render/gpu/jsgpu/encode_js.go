//go:build js && wasm

// encode_js.go translates render/gpu descriptor structs into the WebGPU
// dictionary literals expected by the JS API. Keeping the mapping here
// isolates the WebGPU-specific strings from the rest of the backend.
package jsgpu

import "github.com/odvcencio/gosx/render/gpu"

func encodeFilterMode(f gpu.FilterMode) string {
	if f == gpu.FilterLinear {
		return "linear"
	}
	return "nearest"
}

func encodeAddressMode(a gpu.AddressMode) string {
	switch a {
	case gpu.AddressRepeat:
		return "repeat"
	case gpu.AddressMirrorRepeat:
		return "mirror-repeat"
	}
	return "clamp-to-edge"
}

func encodeTextureUsage(u gpu.TextureUsage) int {
	// WebGPU GPUTextureUsage numeric values (stable, part of the spec).
	const (
		usageCopySrc          = 0x01
		usageCopyDst          = 0x02
		usageTextureBinding   = 0x04
		usageStorageBinding   = 0x08
		usageRenderAttachment = 0x10
	)
	var out int
	if u.Has(gpu.TextureUsageCopySrc) {
		out |= usageCopySrc
	}
	if u.Has(gpu.TextureUsageCopyDst) {
		out |= usageCopyDst
	}
	if u.Has(gpu.TextureUsageTextureBinding) {
		out |= usageTextureBinding
	}
	if u.Has(gpu.TextureUsageStorageBinding) {
		out |= usageStorageBinding
	}
	if u.Has(gpu.TextureUsageRenderAttachment) {
		out |= usageRenderAttachment
	}
	return out
}

func encodeBufferUsage(u gpu.BufferUsage) int {
	// WebGPU GPUBufferUsage numeric values (stable, part of the spec).
	const (
		usageMapRead      = 0x0001
		usageMapWrite     = 0x0002
		usageCopySrc      = 0x0004
		usageCopyDst      = 0x0008
		usageIndex        = 0x0010
		usageVertex       = 0x0020
		usageUniform      = 0x0040
		usageStorage      = 0x0080
		usageIndirect     = 0x0100
		usageQueryResolve = 0x0200
	)
	var out int
	if u.Has(gpu.BufferUsageVertex) {
		out |= usageVertex
	}
	if u.Has(gpu.BufferUsageIndex) {
		out |= usageIndex
	}
	if u.Has(gpu.BufferUsageUniform) {
		out |= usageUniform
	}
	if u.Has(gpu.BufferUsageStorage) {
		out |= usageStorage
	}
	if u.Has(gpu.BufferUsageCopyDst) {
		out |= usageCopyDst
	}
	if u.Has(gpu.BufferUsageCopySrc) {
		out |= usageCopySrc
	}
	if u.Has(gpu.BufferUsageIndirect) {
		out |= usageIndirect
	}
	return out
}

func encodeVertexFormat(f gpu.VertexFormat) string {
	switch f {
	case gpu.VertexFormatFloat32:
		return "float32"
	case gpu.VertexFormatFloat32x2:
		return "float32x2"
	case gpu.VertexFormatFloat32x3:
		return "float32x3"
	case gpu.VertexFormatFloat32x4:
		return "float32x4"
	case gpu.VertexFormatUint32:
		return "uint32"
	case gpu.VertexFormatUint32x2:
		return "uint32x2"
	case gpu.VertexFormatUint32x3:
		return "uint32x3"
	case gpu.VertexFormatUint32x4:
		return "uint32x4"
	case gpu.VertexFormatUint8x4Unorm:
		return "unorm8x4"
	}
	return ""
}

func encodeStepMode(m gpu.VertexStepMode) string {
	if m == gpu.StepInstance {
		return "instance"
	}
	return "vertex"
}

func encodeVertexBuffers(layouts []gpu.VertexBufferLayout) []any {
	out := make([]any, 0, len(layouts))
	for _, l := range layouts {
		attrs := make([]any, 0, len(l.Attributes))
		for _, a := range l.Attributes {
			attrs = append(attrs, map[string]any{
				"shaderLocation": a.ShaderLocation,
				"offset":         a.Offset,
				"format":         encodeVertexFormat(a.Format),
			})
		}
		out = append(out, map[string]any{
			"arrayStride": l.ArrayStride,
			"stepMode":    encodeStepMode(l.StepMode),
			"attributes":  attrs,
		})
	}
	return out
}

func encodePrimitive(p gpu.PrimitiveState) map[string]any {
	return map[string]any{
		"topology":  encodeTopology(p.Topology),
		"cullMode":  encodeCullMode(p.CullMode),
		"frontFace": encodeFrontFace(p.FrontFace),
	}
}

func encodeTopology(t gpu.PrimitiveTopology) string {
	switch t {
	case gpu.TopologyTriangleStrip:
		return "triangle-strip"
	case gpu.TopologyLineList:
		return "line-list"
	case gpu.TopologyLineStrip:
		return "line-strip"
	case gpu.TopologyPointList:
		return "point-list"
	}
	return "triangle-list"
}

func encodeCullMode(c gpu.CullMode) string {
	switch c {
	case gpu.CullFront:
		return "front"
	case gpu.CullBack:
		return "back"
	}
	return "none"
}

func encodeFrontFace(f gpu.FrontFace) string {
	if f == gpu.FrontFaceCW {
		return "cw"
	}
	return "ccw"
}

func encodeIndexFormat(f gpu.IndexFormat) string {
	if f == gpu.IndexFormatUint32 {
		return "uint32"
	}
	return "uint16"
}

func encodeColorTargets(targets []gpu.ColorTargetState) []any {
	out := make([]any, 0, len(targets))
	for _, t := range targets {
		target := map[string]any{
			"format": encodeTextureFormat(t.Format),
		}
		if t.Blend != nil {
			target["blend"] = map[string]any{
				"color": encodeBlendComponent(t.Blend.Color),
				"alpha": encodeBlendComponent(t.Blend.Alpha),
			}
		}
		if t.WriteMask != 0 {
			target["writeMask"] = encodeWriteMask(t.WriteMask)
		}
		out = append(out, target)
	}
	return out
}

func encodeBlendComponent(c gpu.BlendComponent) map[string]any {
	return map[string]any{
		"srcFactor": encodeBlendFactor(c.SrcFactor),
		"dstFactor": encodeBlendFactor(c.DstFactor),
		"operation": encodeBlendOp(c.Operation),
	}
}

func encodeBlendFactor(f gpu.BlendFactor) string {
	switch f {
	case gpu.BlendOne:
		return "one"
	case gpu.BlendSrcAlpha:
		return "src-alpha"
	case gpu.BlendOneMinusSrcAlpha:
		return "one-minus-src-alpha"
	case gpu.BlendDstAlpha:
		return "dst-alpha"
	case gpu.BlendOneMinusDstAlpha:
		return "one-minus-dst-alpha"
	}
	return "zero"
}

func encodeBlendOp(o gpu.BlendOp) string {
	switch o {
	case gpu.BlendOpSubtract:
		return "subtract"
	case gpu.BlendOpReverseSubtract:
		return "reverse-subtract"
	case gpu.BlendOpMin:
		return "min"
	case gpu.BlendOpMax:
		return "max"
	}
	return "add"
}

func encodeWriteMask(m gpu.ColorWriteMask) int {
	// WebGPU GPUColorWrite bit values are stable.
	const (
		wRed   = 0x1
		wGreen = 0x2
		wBlue  = 0x4
		wAlpha = 0x8
	)
	var out int
	if m&gpu.ColorWriteRed != 0 {
		out |= wRed
	}
	if m&gpu.ColorWriteGreen != 0 {
		out |= wGreen
	}
	if m&gpu.ColorWriteBlue != 0 {
		out |= wBlue
	}
	if m&gpu.ColorWriteAlpha != 0 {
		out |= wAlpha
	}
	return out
}

func encodeDepthStencil(ds gpu.DepthStencilState) map[string]any {
	return map[string]any{
		"format":            encodeTextureFormat(ds.Format),
		"depthWriteEnabled": ds.DepthWriteEnabled,
		"depthCompare":      encodeCompare(ds.DepthCompare),
	}
}

func encodeCompare(c gpu.CompareFunc) string {
	switch c {
	case gpu.CompareNever:
		return "never"
	case gpu.CompareLess:
		return "less"
	case gpu.CompareLessEqual:
		return "less-equal"
	case gpu.CompareEqual:
		return "equal"
	case gpu.CompareGreater:
		return "greater"
	case gpu.CompareGreaterEqual:
		return "greater-equal"
	case gpu.CompareNotEqual:
		return "not-equal"
	}
	return "always"
}

func encodeTextureFormat(f gpu.TextureFormat) string {
	switch f {
	case gpu.FormatRGBA8Unorm:
		return "rgba8unorm"
	case gpu.FormatRGBA8UnormSRGB:
		return "rgba8unorm-srgb"
	case gpu.FormatBGRA8Unorm:
		return "bgra8unorm"
	case gpu.FormatBGRA8UnormSRGB:
		return "bgra8unorm-srgb"
	case gpu.FormatRGBA16Float:
		return "rgba16float"
	case gpu.FormatRGBA32Float:
		return "rgba32float"
	case gpu.FormatDepth16Unorm:
		return "depth16unorm"
	case gpu.FormatDepth24Plus:
		return "depth24plus"
	case gpu.FormatDepth24PlusStencil8:
		return "depth24plus-stencil8"
	case gpu.FormatDepth32Float:
		return "depth32float"
	case gpu.FormatR32Uint:
		return "r32uint"
	}
	return ""
}

// parseCanvasFormat is the reverse of encodeTextureFormat for the limited set
// of formats navigator.gpu.getPreferredCanvasFormat() actually returns.
func parseCanvasFormat(s string) gpu.TextureFormat {
	switch s {
	case "rgba8unorm":
		return gpu.FormatRGBA8Unorm
	case "bgra8unorm":
		return gpu.FormatBGRA8Unorm
	}
	return gpu.FormatBGRA8Unorm // sensible default
}

// encodeCanvasFormat is a tiny wrapper for swap-chain configuration so the
// call site reads symmetrically with parseCanvasFormat.
func encodeCanvasFormat(f gpu.TextureFormat) string { return encodeTextureFormat(f) }

func encodeLoadOp(op gpu.LoadOp) string {
	if op == gpu.LoadOpClear {
		return "clear"
	}
	return "load"
}

func encodeStoreOp(op gpu.StoreOp) string {
	if op == gpu.StoreOpDiscard {
		return "discard"
	}
	return "store"
}

func encodeRenderPassDesc(desc gpu.RenderPassDesc) map[string]any {
	color := make([]any, 0, len(desc.ColorAttachments))
	for _, a := range desc.ColorAttachments {
		view, _ := a.View.(*textureView)
		att := map[string]any{
			"view":    view.js,
			"loadOp":  encodeLoadOp(a.LoadOp),
			"storeOp": encodeStoreOp(a.StoreOp),
		}
		if a.LoadOp == gpu.LoadOpClear {
			att["clearValue"] = map[string]any{
				"r": a.ClearValue.R,
				"g": a.ClearValue.G,
				"b": a.ClearValue.B,
				"a": a.ClearValue.A,
			}
		}
		color = append(color, att)
	}
	out := map[string]any{"colorAttachments": color}
	if ds := desc.DepthStencilAttachment; ds != nil {
		view, _ := ds.View.(*textureView)
		out["depthStencilAttachment"] = map[string]any{
			"view":            view.js,
			"depthLoadOp":     encodeLoadOp(ds.DepthLoadOp),
			"depthStoreOp":    encodeStoreOp(ds.DepthStoreOp),
			"depthClearValue": ds.DepthClearValue,
		}
	}
	if desc.Label != "" {
		out["label"] = desc.Label
	}
	return out
}
