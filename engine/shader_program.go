package engine

// ShaderProgram carries a target's emitted source without implying that a host
// can execute it. WGSL and Metal use Source; GLSL and GLES use Vertex/Fragment.
// The containing shaderLayout descriptor supplies entry points and bindings.
type ShaderProgram struct {
	Source   string `json:"source,omitempty"`
	Vertex   string `json:"vertex,omitempty"`
	Fragment string `json:"fragment,omitempty"`
}
