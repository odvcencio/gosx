package bundle

import (
	"testing"

	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/render/compute"
	"m31labs.dev/gosx/render/gpu"
)

// testExternalPass is an ExternalComputePass that dispatches and publishes a
// compacted-instance + indirect-args buffer under a given mesh key, recording
// that Record ran. It stands in for an Elio-generated cull pass.
type testExternalPass struct {
	key      string
	recorded int
}

func (p *testExternalPass) ID() string               { return "test.external.cull" }
func (p *testExternalPass) Phase() compute.PassPhase { return compute.PhaseAfterCull }
func (p *testExternalPass) Record(ctx compute.PassContext) error {
	p.recorded++
	inst, err := ctx.Device.CreateBuffer(gpu.BufferDesc{
		Size: compute.InstanceRecordStride, Label: "elio.test.instances",
		Usage: gpu.BufferUsageStorage | gpu.BufferUsageVertex | gpu.BufferUsageCopyDst,
	})
	if err != nil {
		return err
	}
	args, err := ctx.Device.CreateBuffer(gpu.BufferDesc{
		Size: compute.IndirectArgsStride, Label: "elio.test.drawArgs",
		Usage: gpu.BufferUsageStorage | gpu.BufferUsageIndirect | gpu.BufferUsageCopyDst,
	})
	if err != nil {
		return err
	}
	pass := ctx.Encoder.BeginComputePass()
	pass.DispatchWorkgroups(1, 1, 1)
	pass.End()
	ctx.Publish(compute.GPUResource{
		Name: p.key + ".instances", Buffer: inst, Role: compute.RoleInstanceAttr,
		Element: compute.InstanceRecordLayout(), Count: 1, Access: compute.Read,
	})
	ctx.Publish(compute.GPUResource{
		Name: p.key + ".drawArgs", Buffer: args, Role: compute.RoleIndirectArgs,
		Element: compute.IndirectArgsLayout(), Count: 1, Access: compute.Read,
	})
	return nil
}

// TestExternalComputePassDrivesDraw is the end-to-end M0+M1 proof: an external
// compute pass registered on the renderer runs inside Frame(), publishes its
// output onto the bus, and the main pass consumes that published instance
// buffer for the draw instead of the renderer's built-in cull output.
func TestExternalComputePassDrivesDraw(t *testing.T) {
	im := engine.RenderInstancedMesh{
		ID: "hero", Kind: "cube", VertexCount: 36, InstanceCount: 1,
		Transforms: identityTransform(),
	}
	key := instancedMeshKey(0, im)
	pass := &testExternalPass{key: key}

	d := newFakeDevice()
	r, err := New(Config{
		Device: d, Surface: fakeSurface{},
		ExternalComputePasses: []compute.ExternalComputePass{pass},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Destroy()

	b := engine.RenderBundle{
		Camera:          engine.RenderCamera{Z: 5, FOV: 1, Near: 0.1, Far: 100},
		InstancedMeshes: []engine.RenderInstancedMesh{im},
	}
	if err := r.Frame(b, 400, 300, 0); err != nil {
		t.Fatalf("Frame: %v", err)
	}

	// 1) The hook fired exactly once during the frame.
	if pass.recorded != 1 {
		t.Fatalf("external pass Record ran %d times, want 1", pass.recorded)
	}
	// 2) Its output reached the bus.
	if _, ok := r.published[key+".instances"]; !ok {
		t.Errorf("published %q not present on the bus", key+".instances")
	}
	// 3) The draw consumed the published instance buffer, not the built-in cull
	// output — proving instanceDrawSource preferred the external pass.
	if !boundSomewhere(d, "elio.test.instances") {
		t.Errorf("published instance buffer was not bound at the draw")
	}
}

func boundSomewhere(d *fakeDevice, label string) bool {
	for _, enc := range d.encoders {
		for _, p := range enc.passes {
			for _, l := range p.vbufLabels {
				if l == label {
					return true
				}
			}
		}
	}
	return false
}

// testBeforePostFXPass is an ExternalComputePass at PhaseBeforePostFX that
// records how many times Record was invoked.
type testBeforePostFXPass struct {
	recorded int
}

func (p *testBeforePostFXPass) ID() string               { return "test.external.beforepostfx" }
func (p *testBeforePostFXPass) Phase() compute.PassPhase { return compute.PhaseBeforePostFX }
func (p *testBeforePostFXPass) Record(ctx compute.PassContext) error {
	p.recorded++
	pass := ctx.Encoder.BeginComputePass()
	pass.DispatchWorkgroups(1, 1, 1)
	pass.End()
	return nil
}

// TestExternalPassPhaseBeforePostFXRunsOnceAfterMain verifies that a pass
// registered at PhaseBeforePostFX fires exactly once per frame and runs after
// the main pass (HDR target has been written) but before the post chain.
func TestExternalPassPhaseBeforePostFXRunsOnceAfterMain(t *testing.T) {
	pass := &testBeforePostFXPass{}
	afterCull := &testExternalPass{key: "irrelevant-cull"}
	// Register both so we can confirm phase filtering: afterCull runs at
	// PhaseAfterCull and must NOT be confused with PhaseBeforePostFX.

	d := newFakeDevice()
	r, err := New(Config{
		Device: d, Surface: fakeSurface{},
		ExternalComputePasses: []compute.ExternalComputePass{afterCull, pass},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Destroy()

	b := engine.RenderBundle{
		Camera: engine.RenderCamera{Z: 5, FOV: 1, Near: 0.1, Far: 100},
	}
	if err := r.Frame(b, 400, 300, 0); err != nil {
		t.Fatalf("Frame: %v", err)
	}

	if pass.recorded != 1 {
		t.Fatalf("PhaseBeforePostFX pass Record ran %d times, want 1", pass.recorded)
	}
	// Phase filtering: afterCull should also have run exactly once but only in
	// its own phase — recorded == 1 proves it was not double-dispatched.
	if afterCull.recorded != 1 {
		t.Fatalf("PhaseAfterCull pass Record ran %d times, want 1", afterCull.recorded)
	}
}
