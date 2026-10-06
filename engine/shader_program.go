package engine

// ShaderProgram carries a target's emitted source without implying that a host
// can execute it. WGSL and Metal use Source; GLSL and GLES use Vertex/Fragment.
// The containing shaderLayout descriptor supplies entry points and bindings.
type ShaderProgram struct {
	Source   string `json:"source,omitempty"`
	Vertex   string `json:"vertex,omitempty"`
	Fragment string `json:"fragment,omitempty"`
}

// ShaderProgramFromLayout reads a target from shaderLayout.programs. It accepts
// both authored Go values and descriptors decoded from JSON.
func ShaderProgramFromLayout(layout map[string]any, target string) (ShaderProgram, bool) {
	programs, ok := layout["programs"].(map[string]any)
	if !ok {
		return ShaderProgram{}, false
	}
	var program ShaderProgram
	switch value := programs[target].(type) {
	case ShaderProgram:
		program = value
	case map[string]any:
		program.Source, _ = value["source"].(string)
		program.Vertex, _ = value["vertex"].(string)
		program.Fragment, _ = value["fragment"].(string)
	default:
		return ShaderProgram{}, false
	}
	valid := false
	switch target {
	case "wgsl", "metal":
		valid = program.Source != ""
	case "glsl", "gles":
		valid = program.Vertex != "" && program.Fragment != ""
	}
	return program, valid
}

// ShaderProgram returns a retained target artifact for a native adapter.
func (m RenderMaterial) ShaderProgram(target string) (ShaderProgram, bool) {
	return ShaderProgramFromLayout(m.ShaderLayout, target)
}
