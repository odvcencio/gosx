package capability

// CustomMaterialSources records which shading languages a custom material
// provides. GLSL serves WebGL; WGSL serves WebGPU.
type CustomMaterialSources struct{ GLSL, WGSL bool }

// ShaderResolver answers which backends a custom material can serve. The v1
// implementation is a presence check (PresenceResolver). A future
// transpiling frontend can implement the same interface and return all-true
// when it can compile the missing language. No caller changes when that lands.
type ShaderResolver interface {
	Serves(src CustomMaterialSources) map[Backend]bool
}

// PresenceResolver implements ShaderResolver via a simple presence check:
// a backend is served iff the material ships the shading language that backend
// requires (GLSL → WebGL, WGSL → WebGPU). Canvas2D never needs a custom shader
// and is always considered served.
type PresenceResolver struct{}

func (PresenceResolver) Serves(src CustomMaterialSources) map[Backend]bool {
	return map[Backend]bool{
		BackendWebGL:    src.GLSL,
		BackendWebGPU:   src.WGSL,
		BackendCanvas2D: true,
	}
}
