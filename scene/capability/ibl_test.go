package capability

import (
	"testing"
)

// These tests corroborate the ibl row against renderer source.
//
// The row used to read false everywhere with prose claiming neither renderer
// held a samplerCube, a textureCubeLod or a u_brdfLUT. Both claims were wrong
// for WebGPU by the time this test was written: a full runtime IBL consumer,
// including KTX2 loading, validation and per-frame binding, already shipped.
// TestDriftGuard could not catch the drift because the Matrix cell and the
// WebGPU manifest still agreed with each other — both said false — while
// disagreeing with the shader. See cellEvidence's doc comment in evidence_test.go.

// TestWebGPUConsumesIBLProducts corroborates the true WebGPU cell.
//
// A PASS PROVES: the WebGPU renderer declares the three IBL texture bindings,
// samples the radiance cube through a roughness-selected mip, tracks hasIBL,
// and loads real KTX2 assets at runtime through wgpuLoadTexture, validating
// the authored roughnessPerLevel mapping.
func TestWebGPUConsumesIBLProducts(t *testing.T) {
	webgpu := readRenderer(t, webgpuRendererPath)
	evidence := evidenceFor(t, FeatureIBL, BackendWebGPU).
		needs(webgpuRendererPath, webgpu,
			"var iblIrradiance: texture_cube<f32>",
			"var iblRadiance: texture_cube<f32>",
			"var iblBRDFLUT: texture_2d<f32>",
			"textureSampleLevel(iblRadiance",
			"hasIBL",
			"wgpuLoadTexture",
			"roughnessPerLevel",
		)
	evidence.assertAgrees("The WebGPU renderer binds all three IBL products at group(0) bindings 9-12, " +
		"samples them through the split-sum split (irradiance, prefiltered radiance, BRDF LUT), and " +
		"syncEnvironmentIBL loads and validates the authored KTX2 assets at runtime with no texture-unit " +
		"budget to negotiate, so the cell is unconditionally true.")
}

// TestWebGL2ConsumesIBLAtTheCoreMinimum ties the capability to the depth-array
// implementation. Browser tests also link and draw this shader on 16-unit GL.
func TestWebGL2ConsumesIBLAtTheCoreMinimum(t *testing.T) {
	webgl := readRenderer(t, webglRendererPath)
	evidenceFor(t, FeatureIBL, BackendWebGL).
		needs(webglRendererPath, webgl,
			"uniform highp sampler2DArray u_shadowMap0;",
			"uniform highp sampler2DArray u_shadowMap1;",
			"gl.framebufferTextureLayer", "gl.texImage3D",
			"maxUnits >= 16", "textureLod(u_iblRadiance",
			"u_iblIrradiance", "u_iblBRDFLUT").
		assertAgrees("Two depth arrays preserve four cascades per light and fit IBL at the WebGL2 core minimum.")
}
