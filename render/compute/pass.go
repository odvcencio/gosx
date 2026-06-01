package compute

import "m31labs.dev/gosx/render/gpu"

// PassPhase names a fixed insertion point in the renderer's frame, between the
// renderer's own passes. External passes record in registration order within a
// phase. (A general render-graph with arbitrary dependencies is deferred; these
// fixed phases cover the M0–M2 needs: cull, pre-main geometry, screen-space.)
type PassPhase int

const (
	// PhaseAfterCull runs after the built-in GPU cull, before shadow passes.
	// Outputs published here (instance buffers, indirect args) are available to
	// the main lit pass.
	PhaseAfterCull PassPhase = iota
	// PhaseBeforeMain runs after particle updates, immediately before the main
	// lit pass — for geometry/skinning compute that feeds the draw.
	PhaseBeforeMain
	// PhaseBeforePostFX runs after the main pass, before post-FX — for
	// screen-space compute over the HDR target.
	PhaseBeforePostFX
)

// PassContext is handed to an ExternalComputePass during frame recording. The
// pass records its dispatch onto Encoder — the frame's single command encoder,
// which WebGPU auto-synchronizes against the render passes that follow — and
// announces any buffer it produced via Publish, so later passes can resolve it
// by name on the bus.
//
// Queue writes (uniform/instance uploads, indirect-arg resets) must happen
// before the pass opens its compute pass on Encoder; Record is the correct
// place for them because no pass is open on entry.
type PassContext struct {
	Device  gpu.Device
	Encoder gpu.CommandEncoder
	Frame   uint64
	Publish func(GPUResource)
}

// ExternalComputePass is a render-coupled compute pass contributed from outside
// the renderer — the integration point for Elio-generated kernels (and, later,
// in-frame Manta inference). The renderer calls Phase() to place it and
// Record() to let it dispatch.
//
// Record must open and close its own compute pass on ctx.Encoder
// (BeginComputePass/End) and must not leave a pass open. Buffers it intends to
// feed into rendering are announced via ctx.Publish.
type ExternalComputePass interface {
	// ID is a stable identifier for diagnostics and output naming.
	ID() string
	// Phase places this pass in the frame.
	Phase() PassPhase
	// Record dispatches the pass and publishes its render-input resources.
	Record(ctx PassContext) error
}
