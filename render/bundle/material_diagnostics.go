//go:build !js || !wasm

package bundle

import (
	"fmt"

	"m31labs.dev/gosx/engine"
)

// MaterialDiagnostics describes custom mesh programs that this renderer carries
// but cannot execute. The renderer currently uses its built-in standard shader.
// Program availability never upgrades that fallback into an execution claim.
func MaterialDiagnostics(b engine.RenderBundle) []engine.RenderDiagnostic {
	var cache materialDiagnosticsCache
	return cache.update(b)
}

// Cache only the warning's value (material index and available target mask).
// RenderBundle slices and nested shader layouts belong to the caller and may be
// mutated in place. Recheck current geometry and programs on every frame instead
// of using their addresses or lengths as an invalidation key.
type materialDiagnosticsCache struct {
	referenced []bool
	entries    []materialDiagnosticEntry
	out        []engine.RenderDiagnostic
}

type materialDiagnosticEntry struct {
	targets    uint8
	diagnostic engine.RenderDiagnostic
}

var diagnosticTargets = [...]string{"wgsl", "glsl", "metal", "gles"}

func (c *materialDiagnosticsCache) update(b engine.RenderBundle) []engine.RenderDiagnostic {
	c.out = c.out[:0]
	c.out = appendPostEffectDiagnostics(c.out, b)
	hasCustom := false
	for i := range b.Materials {
		if isCustomMaterial(&b.Materials[i]) {
			hasCustom = true
			break
		}
	}
	if !hasCustom {
		return c.out
	}

	count := len(b.Materials)
	if cap(c.referenced) < count {
		c.referenced = make([]bool, count)
	} else {
		c.referenced = c.referenced[:count]
		clear(c.referenced)
	}
	if len(c.entries) < count {
		c.entries = append(c.entries, make([]materialDiagnosticEntry, count-len(c.entries))...)
	}

	// One membership pass keeps work linear in geometry plus materials, rather
	// than scanning every object once for every custom material.
	mark := func(index int) {
		if index >= 0 && index < count {
			c.referenced[index] = true
		}
	}
	for i := range b.MeshObjects {
		object := &b.MeshObjects[i]
		if object.VertexCount > 0 {
			mark(object.MaterialIndex)
		}
	}
	for i := range b.InstancedMeshes {
		mesh := &b.InstancedMeshes[i]
		if mesh.InstanceCount > 0 && len(mesh.Transforms) > 0 {
			mark(mesh.MaterialIndex)
		}
	}
	for i := range b.Surfaces {
		surface := &b.Surfaces[i]
		if surface.VertexCount > 0 {
			mark(surface.MaterialIndex)
		}
	}

	for i := range b.Materials {
		material := &b.Materials[i]
		if !c.referenced[i] || !isCustomMaterial(material) {
			continue
		}
		// Bit 4 distinguishes an initialized entry with no retained programs.
		targets := uint8(1 << len(diagnosticTargets))
		for bit, target := range diagnosticTargets {
			if _, ok := engine.ShaderProgramFromLayout(material.ShaderLayout, target); ok {
				targets |= 1 << bit
			}
		}
		entry := &c.entries[i]
		if entry.targets != targets {
			var names []string
			for bit, target := range diagnosticTargets {
				if targets&(1<<bit) != 0 {
					names = append(names, target)
				}
			}
			entry.targets = targets
			entry.diagnostic = engine.RenderDiagnostic{
				Severity: "warning", Code: "scene.native.custom_material_fallback", Backend: "native",
				Message: fmt.Sprintf("material %d: custom mesh execution is unsupported; using the standard material (retained targets: %v)", i, names),
			}
		}
		c.out = append(c.out, entry.diagnostic)
	}
	return c.out
}

func isCustomMaterial(material *engine.RenderMaterial) bool {
	return material.Kind == "custom" || material.ShaderBackend != ""
}
