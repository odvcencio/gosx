package jsgpu

import (
	"reflect"
	"testing"

	"m31labs.dev/gosx/render/gpu"
)

// The jsgpu backend held no tests at all. Every function below turns a Go
// descriptor into a WebGPU dictionary value, and a wrong string or a wrong bit
// fails at run time inside the browser with a message that names the WebGPU
// call, not the Go constant that produced it. These tests read the mapping back
// on the host, where a failure names the exact case.

func TestEncodeTextureFormatCoversEveryDeclaredFormat(t *testing.T) {
	cases := map[gpu.TextureFormat]string{
		gpu.FormatRGBA8Unorm:          "rgba8unorm",
		gpu.FormatRGBA8UnormSRGB:      "rgba8unorm-srgb",
		gpu.FormatBGRA8Unorm:          "bgra8unorm",
		gpu.FormatBGRA8UnormSRGB:      "bgra8unorm-srgb",
		gpu.FormatRGBA16Float:         "rgba16float",
		gpu.FormatRGBA32Float:         "rgba32float",
		gpu.FormatRGB10A2Unorm:        "rgb10a2unorm",
		gpu.FormatRGB9E5Ufloat:        "rgb9e5ufloat",
		gpu.FormatR32Uint:             "r32uint",
		gpu.FormatDepth16Unorm:        "depth16unorm",
		gpu.FormatDepth24Plus:         "depth24plus",
		gpu.FormatDepth24PlusStencil8: "depth24plus-stencil8",
		gpu.FormatDepth32Float:        "depth32float",
	}
	for format, want := range cases {
		if got := encodeTextureFormat(format); got != want {
			t.Errorf("encodeTextureFormat(%v) = %q, want %q", format, got, want)
		}
	}
}

// TestEncodeTextureFormatNamesEveryCompressedFormat guards the block-compressed
// tables. A mistyped name here does not fail until a device that supports the
// feature loads a KTX2 texture, which is the hardest failure to trace back.
func TestEncodeTextureFormatNamesEveryCompressedFormat(t *testing.T) {
	compressed := []gpu.TextureFormat{
		gpu.FormatBC7RGBAUnorm, gpu.FormatBC7RGBAUnormSRGB,
		gpu.FormatETC2RGB8Unorm, gpu.FormatETC2RGB8UnormSRGB,
		gpu.FormatETC2RGBA8Unorm, gpu.FormatETC2RGBA8UnormSRGB,
		gpu.FormatASTC4x4Unorm, gpu.FormatASTC4x4UnormSRGB,
		gpu.FormatASTC6x6Unorm, gpu.FormatASTC6x6UnormSRGB,
		gpu.FormatASTC8x8Unorm, gpu.FormatASTC8x8UnormSRGB,
	}
	seen := map[string]gpu.TextureFormat{}
	for _, format := range compressed {
		name := encodeTextureFormat(format)
		if name == "" {
			t.Errorf("encodeTextureFormat(%v) returned an empty name", format)
			continue
		}
		if other, ok := seen[name]; ok {
			t.Errorf("formats %v and %v both encode to %q", other, format, name)
		}
		seen[name] = format
	}
}

func TestParseCanvasFormatReversesEncode(t *testing.T) {
	for _, format := range []gpu.TextureFormat{
		gpu.FormatRGBA8Unorm, gpu.FormatBGRA8Unorm, gpu.FormatRGB10A2Unorm,
	} {
		if got := parseCanvasFormat(encodeCanvasFormat(format)); got != format {
			t.Errorf("round trip of %v produced %v", format, got)
		}
	}
	// getPreferredCanvasFormat never returns anything else, and a surface has
	// to keep working when it does.
	if got := parseCanvasFormat("something-new"); got != gpu.FormatBGRA8Unorm {
		t.Errorf("unknown canvas format fell back to %v, want bgra8unorm", got)
	}
}

func TestEncodeTextureUsageOrsEveryFlag(t *testing.T) {
	// The WebGPU GPUTextureUsage bit values, written out so a change to the Go
	// constants cannot quietly renumber them.
	const (
		copySrc          = 0x01
		copyDst          = 0x02
		textureBinding   = 0x04
		storageBinding   = 0x08
		renderAttachment = 0x10
	)
	cases := []struct {
		usage gpu.TextureUsage
		want  int
	}{
		{gpu.TextureUsageCopySrc, copySrc},
		{gpu.TextureUsageCopyDst, copyDst},
		{gpu.TextureUsageTextureBinding, textureBinding},
		{gpu.TextureUsageStorageBinding, storageBinding},
		{gpu.TextureUsageRenderAttachment, renderAttachment},
		{
			gpu.TextureUsageRenderAttachment | gpu.TextureUsageTextureBinding,
			renderAttachment | textureBinding,
		},
	}
	for _, tc := range cases {
		if got := encodeTextureUsage(tc.usage); got != tc.want {
			t.Errorf("encodeTextureUsage(%v) = %#x, want %#x", tc.usage, got, tc.want)
		}
	}
}

func TestEncodeBufferUsageOrsEveryFlag(t *testing.T) {
	const (
		mapRead  = 0x0001
		mapWrite = 0x0002
		copySrc  = 0x0004
		copyDst  = 0x0008
		index    = 0x0010
		vertex   = 0x0020
		uniform  = 0x0040
		storage  = 0x0080
		indirect = 0x0100
	)
	cases := []struct {
		usage gpu.BufferUsage
		want  int
	}{
		{gpu.BufferUsageMapRead, mapRead},
		{gpu.BufferUsageMapWrite, mapWrite},
		{gpu.BufferUsageCopySrc, copySrc},
		{gpu.BufferUsageCopyDst, copyDst},
		{gpu.BufferUsageIndex, index},
		{gpu.BufferUsageVertex, vertex},
		{gpu.BufferUsageUniform, uniform},
		{gpu.BufferUsageStorage, storage},
		{gpu.BufferUsageIndirect, indirect},
		// The combination the GPU-driven cull asks for: the compute pass writes
		// it as storage and the draw reads it as indirect args.
		{
			gpu.BufferUsageStorage | gpu.BufferUsageIndirect | gpu.BufferUsageCopyDst,
			storage | indirect | copyDst,
		},
	}
	for _, tc := range cases {
		if got := encodeBufferUsage(tc.usage); got != tc.want {
			t.Errorf("encodeBufferUsage(%v) = %#x, want %#x", tc.usage, got, tc.want)
		}
	}
}

func TestEncodeVertexBuffersCarriesLayoutAndAttributes(t *testing.T) {
	got := encodeVertexBuffers([]gpu.VertexBufferLayout{{
		ArrayStride: 80,
		StepMode:    gpu.StepInstance,
		Attributes: []gpu.VertexAttribute{
			{ShaderLocation: 4, Offset: 0, Format: gpu.VertexFormatFloat32x4},
			{ShaderLocation: 8, Offset: 64, Format: gpu.VertexFormatUint32x4},
		},
	}})
	want := []any{map[string]any{
		"arrayStride": 80,
		"stepMode":    "instance",
		"attributes": []any{
			map[string]any{"shaderLocation": 4, "offset": 0, "format": "float32x4"},
			map[string]any{"shaderLocation": 8, "offset": 64, "format": "uint32x4"},
		},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("encodeVertexBuffers produced\n %#v\nwant\n %#v", got, want)
	}
}

func TestEncodeColorTargetsCarriesBlendAndWriteMask(t *testing.T) {
	got := encodeColorTargets([]gpu.ColorTargetState{{
		Format: gpu.FormatRGBA16Float,
		Blend: &gpu.BlendState{
			Color: gpu.BlendComponent{
				SrcFactor: gpu.BlendSrcAlpha,
				DstFactor: gpu.BlendOne,
				Operation: gpu.BlendOpAdd,
			},
			Alpha: gpu.BlendComponent{
				SrcFactor: gpu.BlendZero,
				DstFactor: gpu.BlendOne,
				Operation: gpu.BlendOpAdd,
			},
		},
		WriteMask: gpu.ColorWriteRed | gpu.ColorWriteAlpha,
	}})
	if len(got) != 1 {
		t.Fatalf("encodeColorTargets returned %d targets, want 1", len(got))
	}
	target, ok := got[0].(map[string]any)
	if !ok {
		t.Fatalf("target is %T, want a dictionary", got[0])
	}
	if target["format"] != "rgba16float" {
		t.Errorf("format = %v, want rgba16float", target["format"])
	}
	blend, ok := target["blend"].(map[string]any)
	if !ok {
		t.Fatalf("blend is %T, want a dictionary", target["blend"])
	}
	wantColor := map[string]any{
		"srcFactor": "src-alpha", "dstFactor": "one", "operation": "add",
	}
	if !reflect.DeepEqual(blend["color"], wantColor) {
		t.Errorf("blend.color = %#v, want %#v", blend["color"], wantColor)
	}
	// Red is bit 0 and alpha is bit 3 in GPUColorWrite.
	if target["writeMask"] != 0x1|0x8 {
		t.Errorf("writeMask = %v, want %v", target["writeMask"], 0x1|0x8)
	}
}

// TestEncodeColorTargetsOmitsBlendWhenAbsent checks the opaque path. WebGPU
// rejects a blend member that is present but null, so an absent BlendState has
// to leave the key out entirely.
func TestEncodeColorTargetsOmitsBlendWhenAbsent(t *testing.T) {
	got := encodeColorTargets([]gpu.ColorTargetState{{Format: gpu.FormatBGRA8Unorm}})
	target := got[0].(map[string]any)
	if _, present := target["blend"]; present {
		t.Error("an opaque target still carries a blend member")
	}
}

func TestEncodeDepthStencilCarriesCompareAndWrite(t *testing.T) {
	got := encodeDepthStencil(gpu.DepthStencilState{
		Format:            gpu.FormatDepth24Plus,
		DepthWriteEnabled: true,
		DepthCompare:      gpu.CompareLessEqual,
	})
	want := map[string]any{
		"format":            "depth24plus",
		"depthWriteEnabled": true,
		"depthCompare":      "less-equal",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("encodeDepthStencil produced\n %#v\nwant\n %#v", got, want)
	}
}

func TestEncodeCompareNamesEveryFunction(t *testing.T) {
	cases := map[gpu.CompareFunc]string{
		gpu.CompareNever:        "never",
		gpu.CompareLess:         "less",
		gpu.CompareEqual:        "equal",
		gpu.CompareLessEqual:    "less-equal",
		gpu.CompareGreater:      "greater",
		gpu.CompareNotEqual:     "not-equal",
		gpu.CompareGreaterEqual: "greater-equal",
		gpu.CompareAlways:       "always",
	}
	for compare, want := range cases {
		if got := encodeCompare(compare); got != want {
			t.Errorf("encodeCompare(%v) = %q, want %q", compare, got, want)
		}
	}
}

func TestEncodePrimitiveCarriesTopologyCullAndFrontFace(t *testing.T) {
	got := encodePrimitive(gpu.PrimitiveState{
		Topology:  gpu.TopologyTriangleStrip,
		CullMode:  gpu.CullBack,
		FrontFace: gpu.FrontFaceCW,
	})
	if got["topology"] != "triangle-strip" {
		t.Errorf("topology = %v, want triangle-strip", got["topology"])
	}
	if got["cullMode"] != "back" {
		t.Errorf("cullMode = %v, want back", got["cullMode"])
	}
	if got["frontFace"] != "cw" {
		t.Errorf("frontFace = %v, want cw", got["frontFace"])
	}
}

func TestEncodeTextureViewDescOmitsUnsetMembers(t *testing.T) {
	// WebGPU treats a present member as a request. A zero mip count means "all
	// remaining levels", so writing zero would ask for no levels at all.
	empty := encodeTextureViewDesc(gpu.TextureViewDesc{})
	if len(empty) != 0 {
		t.Errorf("an empty view descriptor produced %#v, want no members", empty)
	}
	cube := encodeTextureViewDesc(gpu.TextureViewDesc{
		Dimension:       gpu.TextureViewDimensionCube,
		BaseArrayLayer:  0,
		ArrayLayerCount: 6,
		Label:           "env",
	})
	want := map[string]any{
		"dimension":       "cube",
		"arrayLayerCount": 6,
		"label":           "env",
	}
	if !reflect.DeepEqual(cube, want) {
		t.Errorf("cube view descriptor produced\n %#v\nwant\n %#v", cube, want)
	}
}

func TestEncodeLoadAndStoreOps(t *testing.T) {
	if got := encodeLoadOp(gpu.LoadOpClear); got != "clear" {
		t.Errorf("encodeLoadOp(clear) = %q", got)
	}
	if got := encodeLoadOp(gpu.LoadOpLoad); got != "load" {
		t.Errorf("encodeLoadOp(load) = %q", got)
	}
	if got := encodeStoreOp(gpu.StoreOpStore); got != "store" {
		t.Errorf("encodeStoreOp(store) = %q", got)
	}
	if got := encodeStoreOp(gpu.StoreOpDiscard); got != "discard" {
		t.Errorf("encodeStoreOp(discard) = %q", got)
	}
}

func TestEncodeFilterAndAddressModes(t *testing.T) {
	if got := encodeFilterMode(gpu.FilterLinear); got != "linear" {
		t.Errorf("encodeFilterMode(linear) = %q", got)
	}
	if got := encodeFilterMode(gpu.FilterNearest); got != "nearest" {
		t.Errorf("encodeFilterMode(nearest) = %q", got)
	}
	cases := map[gpu.AddressMode]string{
		gpu.AddressRepeat:       "repeat",
		gpu.AddressMirrorRepeat: "mirror-repeat",
		gpu.AddressClampToEdge:  "clamp-to-edge",
	}
	for mode, want := range cases {
		if got := encodeAddressMode(mode); got != want {
			t.Errorf("encodeAddressMode(%v) = %q, want %q", mode, got, want)
		}
	}
}

func TestEncodeIndexFormat(t *testing.T) {
	if got := encodeIndexFormat(gpu.IndexFormatUint16); got != "uint16" {
		t.Errorf("encodeIndexFormat(uint16) = %q", got)
	}
	if got := encodeIndexFormat(gpu.IndexFormatUint32); got != "uint32" {
		t.Errorf("encodeIndexFormat(uint32) = %q", got)
	}
}

// TestOpenOnTheHostReportsUnsupported pins the stub contract. Server-side and
// test code imports jsgpu without a build-tag dance, so Open has to fail
// predictably rather than panic.
func TestOpenOnTheHostReportsUnsupported(t *testing.T) {
	device, surface, err := Open("canvas")
	if err == nil {
		t.Fatal("Open succeeded off the browser")
	}
	if device != nil || surface != nil {
		t.Fatalf("Open returned device %v and surface %v alongside its error", device, surface)
	}
}
