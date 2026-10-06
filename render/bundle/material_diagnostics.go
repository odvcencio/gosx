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
		if (material.Kind != "custom" && material.ShaderBackend == "") || !materialReferenced(b, i) {
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

func materialReferenced(b engine.RenderBundle, index int) bool {
	for _, object := range b.MeshObjects {
		if object.MaterialIndex == index && object.VertexCount > 0 {
			return true
		}
	}
	for _, mesh := range b.InstancedMeshes {
		if mesh.MaterialIndex == index && mesh.InstanceCount > 0 && len(mesh.Transforms) > 0 {
			return true
		}
	}
	for _, surface := range b.Surfaces {
		if surface.MaterialIndex == index && surface.VertexCount > 0 {
			return true
		}
	}
	return false
}
