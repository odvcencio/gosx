package scene

// ShaderWarmup lists materials and draw variants that a scene uses after its
// first frame, so the renderer compiles them at boot instead of on first use.
//
// A browser compiles a shader program the first time something draws with
// it. Both Scene3D backends compile in the background (WebGL2 through
// KHR_parallel_shader_compile, WebGPU through createRenderPipelineAsync), but
// a frame still waits for every program it needs, so an object that spawns
// mid-game with a new material freezes the scene while its program links.
// Listing that material here moves the work to boot: the first frame waits
// until every listed variant is compiled, and later first uses find the
// program in the renderer's cache.
//
// Materials already on visible objects compile for the first frame anyway.
// List what is not on screen at boot: spawned objects, hidden objects,
// material swaps, and the skinned, double-sided or instanced copies of a
// material that already shows.
//
// The mount reports progress for a loading screen: the attributes
// data-gosx-scene3d-shaders ("compiling" or "ready"),
// data-gosx-scene3d-shaders-done and data-gosx-scene3d-shaders-total, and
// the bubbling "gosx:scene3d:shader-progress" event with detail
// {done, total, failed, ready, backend}. In Go, Surface.ShaderWarmupProgress
// feeds the boot gate:
//
//	gate.Add(boot.CountTask("shaders", 1, surface.ShaderWarmupProgress))
type ShaderWarmup struct {
	Materials []WarmupMaterial
}

// WarmupMaterial is one material in one draw variant. Add one entry per
// variant the scene uses; a material drawn both static and skinned needs two.
type WarmupMaterial struct {
	// Material is the material to compile: a CustomMaterial from Selena
	// (CompileSelenaMaterial, MaterialFromSelenaArtifact) or a standard one.
	Material Material
	// Skinned compiles the skinned-mesh variant (glTF skins, Elio rigs).
	Skinned bool
	// DoubleSided compiles the variant without back-face culling.
	DoubleSided bool
	// CastShadow also compiles the shadow-pass variant for this material.
	CastShadow bool
	// Instances, when above zero, compiles the instanced-mesh variant for an
	// InstancedMesh of that many instances. Large counts can select a GPU
	// culling variant on WebGPU, so give the count the scene will use.
	Instances int
}

// legacyProps lowers the warm-up list into the mount prop shape the browser
// runtime reads (props.shaderWarmup).
func (w *ShaderWarmup) legacyProps() map[string]any {
	if w == nil {
		return nil
	}
	materials := make([]any, 0, len(w.Materials))
	for _, entry := range w.Materials {
		if entry.Material == nil {
			continue
		}
		record := map[string]any{}
		if fields := entry.Material.legacyMaterial(); len(fields) > 0 {
			record["material"] = fields
		} else {
			record["material"] = map[string]any{}
		}
		if entry.Skinned {
			record["skinned"] = true
		}
		if entry.DoubleSided {
			record["doubleSided"] = true
		}
		if entry.CastShadow {
			record["castShadow"] = true
		}
		setInt(record, "instances", entry.Instances)
		materials = append(materials, record)
	}
	if len(materials) == 0 {
		return nil
	}
	return map[string]any{"materials": materials}
}

// shaderWarmupGateCounts maps published warm-up progress onto boot gate
// counts. Only the "ready" phase completes the task: while compiling, done
// stays below total even when every program has linked, because readiness
// means a frame drew with all of them.
func shaderWarmupGateCounts(phase string, done, total int) (int, int) {
	if phase == "ready" {
		if total < 1 {
			total = 1
		}
		return total, total
	}
	if total < 1 {
		return 0, 1
	}
	if done >= total {
		done = total - 1
	}
	if done < 0 {
		done = 0
	}
	return done, total
}
