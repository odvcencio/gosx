// Package compute is the render-coupled-compute extension surface for the GoSX
// renderer: the GPU Resource Bus (a typed descriptor over render/gpu buffer and
// texture handles) plus the ExternalComputePass hook (see pass.go) that lets
// out-of-tree systems — Elio-generated kernels today, in-frame Manta inference
// later — record compute passes onto the frame's command encoder and publish
// their outputs as render inputs.
//
// The Bus is the shared data-plane contract spoken by the renderer, Elio
// (render-coupled compute), Selena (presentation, via its binding layout), and
// Manta (inference). It deliberately wraps the existing render/gpu types rather
// than introducing a second GPU abstraction: gpu is the device of record.
package compute

import "m31labs.dev/gosx/render/gpu"

// ResourceRole says how a published bus resource is consumed downstream.
type ResourceRole int

const (
	RoleStorage         ResourceRole = iota // generic read / read-write storage buffer
	RoleInstanceAttr                        // per-instance vertex attributes (e.g. cull output)
	RoleVertex                              // per-vertex attributes
	RoleIndirectArgs                        // indirect draw / dispatch argument buffer
	RoleUniform                             // uniform block
	RoleStorageTexture                      // read / write storage image
	RoleFramebufferView                     // a rendered target, for read-back (inference input)
)

// Access is the read/write intent of a resource at the point of handoff.
type Access int

const (
	Read Access = iota
	ReadWrite
)

// FieldDesc is one typed member of a bus element, addressed by shader location.
// For instance/vertex roles these map 1:1 to gpu.VertexAttribute.
type FieldDesc struct {
	Name     string
	Location int
	Offset   int
	Format   gpu.VertexFormat
}

// ElementLayout is the byte layout of one element of a bus buffer.
type ElementLayout struct {
	Stride int
	Fields []FieldDesc
}

// VertexAttributes projects the layout into the renderer's gpu.VertexAttribute
// form, so a published RoleInstanceAttr/RoleVertex buffer can be bound directly
// as a vertex/instance buffer with no hand-matched offsets.
func (l ElementLayout) VertexAttributes() []gpu.VertexAttribute {
	out := make([]gpu.VertexAttribute, len(l.Fields))
	for i, f := range l.Fields {
		out[i] = gpu.VertexAttribute{ShaderLocation: f.Location, Offset: f.Offset, Format: f.Format}
	}
	return out
}

// VertexBufferLayout returns the gpu layout for binding this resource as a
// vertex/instance buffer slot in a render pipeline.
func (l ElementLayout) VertexBufferLayout(step gpu.VertexStepMode) gpu.VertexBufferLayout {
	return gpu.VertexBufferLayout{ArrayStride: l.Stride, StepMode: step, Attributes: l.VertexAttributes()}
}

// GPUResource is one handle on the bus: a typed buffer with a role and element
// layout that the producer (Elio kernel, Manta) and the consumer (renderer
// draw, Selena material) both understand.
type GPUResource struct {
	Name    string
	Buffer  gpu.Buffer
	Role    ResourceRole
	Element ElementLayout
	Count   int
	Access  Access
}

// --- Canonical, versioned formats (proven against render/bundle/cull.go) ---

// InstanceRecordStride is the byte stride of the engine's instance record: a
// column-major mat4 (64 B) followed by vec4<u32> pick metadata (16 B).
const InstanceRecordStride = 80

// InstanceRecordLayout is the canonical per-instance layout the lit/skinned
// pipelines consume at shader locations 4..8 (matching cull.go output), so an
// Elio kernel that writes InstanceRecords drops into the existing
// SetVertexBuffer(4, …) + DrawIndirect path with no renderer change.
func InstanceRecordLayout() ElementLayout {
	return ElementLayout{
		Stride: InstanceRecordStride,
		Fields: []FieldDesc{
			{Name: "m0", Location: 4, Offset: 0, Format: gpu.VertexFormatFloat32x4},
			{Name: "m1", Location: 5, Offset: 16, Format: gpu.VertexFormatFloat32x4},
			{Name: "m2", Location: 6, Offset: 32, Format: gpu.VertexFormatFloat32x4},
			{Name: "m3", Location: 7, Offset: 48, Format: gpu.VertexFormatFloat32x4},
			{Name: "pickData", Location: 8, Offset: 64, Format: gpu.VertexFormatUint32x4},
		},
	}
}

// IndirectArgsStride is the byte size of a WebGPU indirect draw-args buffer:
// [vertexCount, instanceCount, firstVertex, firstInstance] as 4×u32.
const IndirectArgsStride = 16

// IndirectArgsLayout is the canonical indirect draw-args layout.
func IndirectArgsLayout() ElementLayout {
	return ElementLayout{
		Stride: IndirectArgsStride,
		Fields: []FieldDesc{
			{Name: "vertexCount", Offset: 0, Format: gpu.VertexFormatUint32},
			{Name: "instanceCount", Offset: 4, Format: gpu.VertexFormatUint32},
			{Name: "firstVertex", Offset: 8, Format: gpu.VertexFormatUint32},
			{Name: "firstInstance", Offset: 12, Format: gpu.VertexFormatUint32},
		},
	}
}
