package compute

import (
	"testing"

	"m31labs.dev/gosx/render/gpu"
)

// TestInstanceRecordLayoutMatchesTheRendererContract pins the shape the lit and
// the skinned pipelines expect at vertex slot 4. An external kernel that writes
// InstanceRecords must drop into the renderer's DrawIndirect path with no
// renderer change, so the stride, the shader locations, and the offsets are all
// part of the contract.
func TestInstanceRecordLayoutMatchesTheRendererContract(t *testing.T) {
	layout := InstanceRecordLayout()
	if layout.Stride != InstanceRecordStride {
		t.Fatalf("stride = %d, want %d", layout.Stride, InstanceRecordStride)
	}
	if layout.Stride != 80 {
		t.Fatalf("stride = %d; a column-major mat4 plus vec4<u32> is 80 bytes", layout.Stride)
	}
	want := []FieldDesc{
		{Name: "m0", Location: 4, Offset: 0, Format: gpu.VertexFormatFloat32x4},
		{Name: "m1", Location: 5, Offset: 16, Format: gpu.VertexFormatFloat32x4},
		{Name: "m2", Location: 6, Offset: 32, Format: gpu.VertexFormatFloat32x4},
		{Name: "m3", Location: 7, Offset: 48, Format: gpu.VertexFormatFloat32x4},
		{Name: "pickData", Location: 8, Offset: 64, Format: gpu.VertexFormatUint32x4},
	}
	if len(layout.Fields) != len(want) {
		t.Fatalf("fields = %d, want %d", len(layout.Fields), len(want))
	}
	for i, field := range layout.Fields {
		if field != want[i] {
			t.Errorf("field %d = %+v, want %+v", i, field, want[i])
		}
	}
}

// TestIndirectArgsLayoutMatchesWebGPU pins the four u32 words WebGPU reads from
// an indirect draw-args buffer, in order.
func TestIndirectArgsLayoutMatchesWebGPU(t *testing.T) {
	layout := IndirectArgsLayout()
	if layout.Stride != IndirectArgsStride {
		t.Fatalf("stride = %d, want %d", layout.Stride, IndirectArgsStride)
	}
	if IndirectArgsStride != 16 {
		t.Fatalf("IndirectArgsStride = %d; four u32 words are 16 bytes", IndirectArgsStride)
	}
	wantNames := []string{"vertexCount", "instanceCount", "firstVertex", "firstInstance"}
	if len(layout.Fields) != len(wantNames) {
		t.Fatalf("fields = %d, want %d", len(layout.Fields), len(wantNames))
	}
	for i, field := range layout.Fields {
		if field.Name != wantNames[i] {
			t.Errorf("field %d name = %q, want %q", i, field.Name, wantNames[i])
		}
		if field.Offset != i*4 {
			t.Errorf("field %q offset = %d, want %d", field.Name, field.Offset, i*4)
		}
		if field.Format != gpu.VertexFormatUint32 {
			t.Errorf("field %q format = %v, want Uint32", field.Name, field.Format)
		}
	}
}

// TestVertexAttributesProjectEveryField checks the projection into the renderer's
// gpu types keeps every location and offset, so no call site has to hand-match
// them.
func TestVertexAttributesProjectEveryField(t *testing.T) {
	layout := InstanceRecordLayout()
	attrs := layout.VertexAttributes()
	if len(attrs) != len(layout.Fields) {
		t.Fatalf("attributes = %d, want %d", len(attrs), len(layout.Fields))
	}
	for i, attr := range attrs {
		field := layout.Fields[i]
		if attr.ShaderLocation != field.Location || attr.Offset != field.Offset ||
			attr.Format != field.Format {
			t.Errorf("attribute %d = %+v, want location %d offset %d format %v",
				i, attr, field.Location, field.Offset, field.Format)
		}
	}
}

// TestVertexBufferLayoutCarriesStepMode checks the step mode reaches the
// pipeline layout. Instance records must advance once per instance, not once per
// vertex, or every instance draws on top of the first.
func TestVertexBufferLayoutCarriesStepMode(t *testing.T) {
	layout := InstanceRecordLayout().VertexBufferLayout(gpu.StepInstance)
	if layout.StepMode != gpu.StepInstance {
		t.Errorf("step mode = %v, want StepInstance", layout.StepMode)
	}
	if layout.ArrayStride != InstanceRecordStride {
		t.Errorf("array stride = %d, want %d", layout.ArrayStride, InstanceRecordStride)
	}
	if len(layout.Attributes) != 5 {
		t.Errorf("attributes = %d, want 5", len(layout.Attributes))
	}

	perVertex := InstanceRecordLayout().VertexBufferLayout(gpu.StepVertex)
	if perVertex.StepMode != gpu.StepVertex {
		t.Errorf("step mode = %v, want StepVertex", perVertex.StepMode)
	}
}

// TestEmptyLayoutProjectsEmptyAttributes keeps the projection safe for a
// resource that declares no fields.
func TestEmptyLayoutProjectsEmptyAttributes(t *testing.T) {
	var layout ElementLayout
	if got := layout.VertexAttributes(); len(got) != 0 {
		t.Errorf("attributes = %v, want none", got)
	}
	if got := layout.VertexBufferLayout(gpu.StepInstance); len(got.Attributes) != 0 {
		t.Errorf("buffer layout attributes = %v, want none", got.Attributes)
	}
}

// TestPassPhasesAreDistinctAndOrdered pins the phase order the renderer relies
// on: cull, then pre-main geometry, then screen-space.
func TestPassPhasesAreDistinctAndOrdered(t *testing.T) {
	if !(PhaseAfterCull < PhaseBeforeMain && PhaseBeforeMain < PhaseBeforePostFX) {
		t.Fatalf("phases out of order: %d %d %d",
			PhaseAfterCull, PhaseBeforeMain, PhaseBeforePostFX)
	}
}

// recordingPass is a minimal ExternalComputePass used to check the interface is
// satisfiable with a value receiver and that Publish reaches the caller.
type recordingPass struct {
	published []GPUResource
}

func (p *recordingPass) ID() string       { return "compute.test.pass" }
func (p *recordingPass) Phase() PassPhase { return PhaseAfterCull }
func (p *recordingPass) Record(ctx PassContext) error {
	ctx.Publish(GPUResource{
		Name:    "mesh.instances",
		Role:    RoleInstanceAttr,
		Element: InstanceRecordLayout(),
		Count:   4,
		Access:  Read,
	})
	return nil
}

// TestPassContextPublishReachesTheHost checks the Publish callback contract an
// external kernel uses to hand its output to the draw.
func TestPassContextPublishReachesTheHost(t *testing.T) {
	pass := &recordingPass{}
	var got []GPUResource
	ctx := PassContext{
		Frame:   7,
		Publish: func(res GPUResource) { got = append(got, res) },
	}
	if err := pass.Record(ctx); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("published %d resources, want 1", len(got))
	}
	if got[0].Name != "mesh.instances" || got[0].Role != RoleInstanceAttr {
		t.Fatalf("published %+v", got[0])
	}
	if got[0].Element.Stride != InstanceRecordStride {
		t.Fatalf("published stride = %d, want %d", got[0].Element.Stride, InstanceRecordStride)
	}
	pass.published = got
}

// TestExternalComputePassInterfaceIsSatisfied is a compile-time guard that the
// interface stays implementable from outside this package's own types.
func TestExternalComputePassInterfaceIsSatisfied(t *testing.T) {
	var _ ExternalComputePass = &recordingPass{}
}
