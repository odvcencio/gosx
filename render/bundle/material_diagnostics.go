package bundle

import (
	"fmt"
	"m31labs.dev/gosx/engine"
)

// MaterialDiagnostics describes custom mesh programs that this renderer carries
// but cannot execute. The renderer currently uses its built-in standard shader.
// Program availability never upgrades that fallback into an execution claim.
func MaterialDiagnostics(b engine.RenderBundle) []engine.RenderDiagnostic {
	var out []engine.RenderDiagnostic
	for i, material := range b.Materials {
		if material.Kind != "custom" && material.ShaderBackend == "" {
			continue
		}
		var targets []string
		for _, target := range []string{"wgsl", "glsl", "metal", "gles"} {
			if _, ok := material.ShaderProgram(target); ok {
				targets = append(targets, target)
			}
		}
		out = append(out, engine.RenderDiagnostic{
			Severity: "warning", Code: "scene.native.custom_material_fallback", Backend: "native",
			Message: fmt.Sprintf("material %d: custom mesh execution is unsupported; using the standard material (retained targets: %v)", i, targets),
		})
	}
	return out
}
