package capability

import (
	"strings"
	"testing"
)

// TestCustomShaderHasNoFlatCellOnPurpose records a deliberate NON-DECISION, the
// way TestRenderBundlesAreNotACapabilityCell records one for performance
// features. A later reader must not add the row.
//
// Every other Feature answers "does this backend implement X". custom-shader
// cannot, because it is not a renderer capability at all. Both renderers
// implement custom shading completely, each in its own language. What varies is
// the AUTHOR'S material.
//
// So a flat cell has no honest value:
//
//	Matrix[custom-shader][webgpu] = true
//	    claims WebGPU serves a material that ships only VertexGLSL. It cannot;
//	    the WebGPU renderer reads customVertexWGSL and never compiles GLSL.
//	Matrix[custom-shader][webgpu] = false
//	    claims WebGPU serves no custom material, even one that ships WGSL. It
//	    does; getSelenaPipeline compiles the authored module.
//
// The same argument holds for WebGL2 with the languages swapped. A boolean per
// backend cannot express a fact that varies per material, so the row is absent
// and ShaderResolver answers the per-material question instead.
//
// FeatureCustomShader still exists as a NAME. Props.SceneIR uses it as the
// CapReason.Feature on the post-filter exclusion, so the author reads
// "custom-shader excludes webgpu" rather than an unexplained missing backend.
func TestCustomShaderHasNoFlatCellOnPurpose(t *testing.T) {
	if row, exists := Matrix[FeatureCustomShader]; exists {
		t.Fatalf("custom-shader gained a Matrix row (%v). A flat cell cannot answer a per-material "+
			"question: read the reasoning above, then either delete the row or explain which material "+
			"the cell describes", row)
	}
	// The name must survive, because it labels the post-filter reason.
	if FeatureCustomShader != "custom-shader" {
		t.Errorf("FeatureCustomShader = %q; the CapReason label in scene/scene_ir.go depends on it",
			FeatureCustomShader)
	}
	// A row would also break the drift guard, which demands a manifest key for
	// every Matrix feature. Neither manifest carries "custom-shader", and that
	// absence is correct rather than an omission.
	for _, path := range []string{
		"../../client/js/bootstrap-src/16-scene-webgl.capabilities.json",
		"../../client/js/bootstrap-src/16a-scene-webgpu.capabilities.json",
	} {
		manifest, err := loadManifest(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if _, present := manifest["custom-shader"]; present {
			t.Errorf("%s declares custom-shader. A manifest boolean cannot describe a per-material "+
				"fact either; remove the key or explain which material it describes", path)
		}
	}
}

// TestPresenceResolverMatchesRendererLanguages corroborates the resolver against
// renderer source, which is the same standard a Matrix cell must meet.
//
// PresenceResolver claims GLSL serves WebGL and WGSL serves WebGPU. That claim is
// only true while each renderer reads exactly its own language and neither
// transpiles. The named fields are the ones the renderers actually read.
func TestPresenceResolverMatchesRendererLanguages(t *testing.T) {
	webgl := readRenderer(t, webglRendererPath)
	webgpu := readRenderer(t, webgpuRendererPath)

	// WebGL2 reads the GLSL fields.
	for _, symbol := range []string{
		"var vertexSource = material.customVertex;",
		"material.customFragment",
	} {
		if !strings.Contains(webgl, symbol) {
			t.Errorf("expected %q in %s: PresenceResolver maps GLSL to WebGL2 because of it",
				symbol, webglRendererPath)
		}
	}
	// And it must not compile WGSL, or the resolver's GLSL-only claim is wrong.
	for _, absent := range []string{"customVertexWGSL", "customFragmentWGSL"} {
		if strings.Contains(webgl, absent) {
			t.Errorf("%s now reads %q; if WebGL2 can consume WGSL, PresenceResolver must change",
				webglRendererPath, absent)
		}
	}

	// WebGPU reads the WGSL fields.
	for _, symbol := range []string{
		"entry.customVertexWGSL",
		"entry.customFragmentWGSL",
	} {
		if !strings.Contains(webgpu, symbol) {
			t.Errorf("expected %q in %s: PresenceResolver maps WGSL to WebGPU because of it",
				symbol, webgpuRendererPath)
		}
	}

	// The resolver's own behaviour must match those two facts.
	got := PresenceResolver{}.Serves(CustomMaterialSources{GLSL: true})
	if got[BackendWebGPU] {
		t.Error("PresenceResolver serves WebGPU from GLSL alone; the WebGPU renderer compiles no GLSL")
	}
	got = PresenceResolver{}.Serves(CustomMaterialSources{WGSL: true})
	if got[BackendWebGL] {
		t.Error("PresenceResolver serves WebGL2 from WGSL alone; the WebGL2 renderer compiles no WGSL")
	}
}

// TestPresenceResolverCanvas2DClaimIsOverbroad records a SECOND finding, so it
// does not stay buried in a report.
//
// PresenceResolver returns BackendCanvas2D: true unconditionally, on the reading
// that "Canvas2D never needs a custom shader". The stronger fact is that Canvas2D
// never draws the MESH the custom material belongs to. createSceneCanvasRenderer
// in 18-scene-canvas.ts draws line segments and point sprites only; it rasterizes
// no triangle.
//
// So "served" is the wrong word for what happens. Canvas2D draws nothing, and the
// verdict records no gap.
//
// The record still stands today for a narrow reason, and this test pins that
// reason rather than the claim. chooseSceneBackend in 20a-scene-mount-backend.js
// prefers WebGPU, then WebGL2, and reaches Canvas2D only when both GPU contexts
// are unavailable. A custom-material mesh on that path draws nothing whichever
// value the resolver returns, so flipping it to false would exclude a
// last-resort backend from a scene that may also hold lines and points it CAN
// draw. Excluding it can empty Capable; leaving it cannot.
//
// The honest fix is not a resolver change. It is a mesh-rasterization feature
// that Canvas2D reads false, which would exclude Canvas2D from any mesh scene for
// the real reason. That row needs a name in both renderer manifests, so this test
// records the gap and names the fix.
// The check below used to SKIP when the resolver stopped serving Canvas2D. That
// inverted the point of the test. Serving Canvas2D is the safe answer whichever
// way the renderer goes: with no mesh path the claim stands for the narrow reason
// above, and with a mesh path it stands on its own terms. The dangerous change is
// the resolver going false, because that excludes a last-resort backend and can
// empty Capable. So that change must FAIL here, not disable the test.
func TestPresenceResolverCanvas2DClaimIsOverbroad(t *testing.T) {
	served := PresenceResolver{}.Serves(CustomMaterialSources{})
	if !served[BackendCanvas2D] {
		t.Fatal("PresenceResolver no longer serves Canvas2D. Excluding Canvas2D can empty " +
			"Capable, and a custom-material mesh draws nothing on that path whichever value the " +
			"resolver returns. Re-read the reasoning above: the honest fix is a " +
			"mesh-rasterization feature that Canvas2D reads false, not a resolver change.")
	}
	canvas := readRenderer(t, canvasRendererPath)

	// Canvas2D draws exactly two things. Both must stay named, because the whole
	// argument rests on the list being short and complete.
	for _, symbol := range []string{
		"renderSceneCanvasPoints(ctx2d, bundle, cssWidth, cssHeight);",
		"renderSceneCanvasWorldBundle(ctx2d, bundle, cssWidth, cssHeight);",
	} {
		if !strings.Contains(canvas, symbol) {
			t.Errorf("expected %q in %s: the Canvas2D draw list is the evidence for this finding",
				symbol, canvasRendererPath)
		}
	}
	// And it must draw no triangle. Any of these appearing means Canvas2D grew a
	// mesh path, and then the resolver's claim becomes defensible on its own terms.
	for _, absent := range []string{
		"drawTriangle",
		"rasterizeTriangle",
		"customFragment",
		"customVertex",
	} {
		if strings.Contains(canvas, absent) {
			t.Errorf("%s now contains %q; Canvas2D may rasterize meshes or read custom shaders, "+
				"so re-check the PresenceResolver Canvas2D claim", canvasRendererPath, absent)
		}
	}
	// The recommended fix must not have been added silently as a half-measure.
	if _, exists := Matrix[Feature("mesh-rasterization")]; exists {
		t.Error("a mesh-rasterization row appeared; move this finding into its own corroboration test " +
			"and confirm both renderer manifests carry the key")
	}
}
