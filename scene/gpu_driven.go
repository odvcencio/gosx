package scene

// GPUDriven opts a scene into GPU-driven instancing on the WebGPU backend.
//
// When set, the WebGPU renderer culls every eligible InstancedMesh on the GPU
// with one compute dispatch per view: the camera, plus each shadow light when
// ShadowCulling is on. Survivors are drawn with indirect draws that read their
// instance records from a storage buffer, so per-instance colors survive the
// cull.
//
// The mode changes no pixels. Every cull is conservative: an instance that
// covers a pixel is always drawn. WebGL, Canvas and headless rendering ignore
// the field, and so does a WebGPU device that cannot run it.
//
// An InstancedMesh is eligible when it draws in the opaque pass, has at least
// one instance, and has no authored CullKernelWGSL. Every other mesh keeps the
// renderer's existing path.
type GPUDriven struct {
	// Occlusion turns on two-phase hierarchical-Z occlusion culling for the
	// camera. The main pass first draws what was visible last frame, builds a
	// depth pyramid from that depth, then culls again and draws only the
	// instances that just became visible. It costs one compute pass and one
	// extra render pass per frame. It pays off when large occluders hide many
	// instances.
	Occlusion bool
	// ShadowCulling culls shadow casters against each shadow light's frustum
	// before the shadow pass draws them. Nil means true.
	ShadowCulling *bool
}

// GPUDrivenIR is the wire form of GPUDriven. ShadowCulling is always resolved,
// so the client never has to know the default.
type GPUDrivenIR struct {
	Occlusion     bool `json:"occlusion,omitempty"`
	ShadowCulling bool `json:"shadowCulling"`
}

// sceneIR lowers the authored mode. A nil receiver means the scene did not opt
// in, and lowers to nil so the wire carries nothing.
func (g *GPUDriven) sceneIR() *GPUDrivenIR {
	if g == nil {
		return nil
	}
	shadowCulling := true
	if g.ShadowCulling != nil {
		shadowCulling = *g.ShadowCulling
	}
	return &GPUDrivenIR{Occlusion: g.Occlusion, ShadowCulling: shadowCulling}
}

// legacyProps mirrors the reflection marshal of GPUDrivenIR for the map-tree
// path. TestSceneIRDirectMarshalMatchesLegacy pins the two against each other.
func (g *GPUDrivenIR) legacyProps() map[string]any {
	out := map[string]any{"shadowCulling": g.ShadowCulling}
	if g.Occlusion {
		out["occlusion"] = true
	}
	return out
}
