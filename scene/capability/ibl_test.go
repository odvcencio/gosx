package capability

import (
	"strings"
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

// TestWebGL2GatesIBLOnTwentyUnits corroborates the false WebGL2 cell WITH
// the evidence that the consumer exists and stays gated, not evidence that it
// is missing. A cell answers an unconditional question; scenePBRHDRIBLAvailable
// answers a conditional one, and the two must not be conflated.
func TestWebGL2GatesIBLOnTwentyUnits(t *testing.T) {
	webgl := readRenderer(t, webglRendererPath)
	missingConsumer := missingSymbols(webglRendererPath, webgl,
		"u_iblIrradiance", "u_iblRadiance", "u_iblBRDFLUT", "GOSX_HDR_IBL",
	)
	if len(missingConsumer) > 0 {
		t.Fatalf("WebGL2 IBL consumer symbols are missing, so the gate below cannot be read as "+
			"'consumer exists, cell stays false, reason is the unit gate': %v", missingConsumer)
	}
	missingGate := missingSymbols(webglRendererPath, webgl,
		"maxUnits >= 20", "scenePBRHDRIBLAvailable",
	)
	if len(missingGate) > 0 {
		t.Fatalf("the 20 fragment-texture-unit gate moved or was renamed; update this test and the "+
			"Matrix[ibl][webgl] prose together: %v", missingGate)
	}
	if Matrix[FeatureIBL][BackendWebGL] {
		t.Fatal("Matrix[ibl][webgl] is true, but the WebGL2 consumer only activates at " +
			">= 20 fragment texture units (scenePBRHDRIBLAvailable). An unconditional cell " +
			"must answer for the minimum spec device, so it must stay false. Either the gate " +
			"was removed (flip the cell) or this cell was flipped by mistake (flip it back).")
	}
}

// TestWebGL2IBLGateHasSHFallbackReason pins the diagnostics reason string a
// gated device must report once the SH9 irradiance fallback lands (PR-8). It
// stays a refutedBy read — the fallback is not required to exist yet — so
// this test only fails if the reason string appears with a different spelling
// than the one PR-8 is committed to.
func TestWebGL2IBLGateHasSHFallbackReason(t *testing.T) {
	webgl := strings.ToLower(readRenderer(t, webglRendererPath))
	if strings.Contains(webgl, "sh-irradiance") && !strings.Contains(webgl, "fragment-texture-units<20 -> sh-irradiance") {
		t.Fatal(`found a "sh-irradiance" reason string that does not match the pinned spelling ` +
			`"fragment-texture-units<20 -> sh-irradiance"; keep the diagnostics reason stable so a ` +
			`caller can match on it`)
	}
}
