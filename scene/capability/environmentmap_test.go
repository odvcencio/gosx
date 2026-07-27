package capability

import (
	"strings"
	"testing"
)

// These tests corroborate the environment-map row against renderer source.
//
// The row exists because the ibl row reads as parity and is not. Both ibl cells
// are false, and both are right: neither browser backend runs a split-sum fit.
// Underneath that symmetry one backend samples the authored image and the other
// never opens it, and no cell said so.
//
// readRenderer, webgpuRendererPath and webglRendererPath come from lights_test.go.

// environmentMapIdentifiers are the three authored environment fields a renderer
// must name to read the image. They are lower case because both readings fold
// case: the WebGL2 renderer spells envMap as envMap, u_envMap and ENVMAP in
// different places, and a case-sensitive count would under-report it.
var environmentMapIdentifiers = []string{"envmap", "envintensity", "envrotation"}

// TestWebGL2ReadsTheEnvironmentMap corroborates the true cell.
//
// A PASS PROVES: the WebGL2 renderer names all three authored environment fields
// and carries the sampler and the two taps that consume them.
//
// A PASS DOES NOT PROVE: that the result is image-based lighting. It is not. The
// renderer taps one tone-mapped equirectangular texture twice and scales the
// second tap by (1.0 - roughness * 0.65), a factor with no derivation. The ibl
// row above stays false for that reason, and render/bundle/lit.go carries the
// same factor.
func TestWebGL2ReadsTheEnvironmentMap(t *testing.T) {
	webgl := strings.ToLower(readRenderer(t, webglRendererPath))
	evidence := evidenceFor(t, FeatureEnvironmentMap, BackendWebGL).
		needs(webglRendererPath, webgl, environmentMapIdentifiers...).
		needs(webglRendererPath, webgl,
			"uniform sampler2d u_envmap;",
			"u_envintensity",
			"u_envrotation",
		)
	evidence.assertAgrees("The WebGL2 renderer declares an environment sampler, uploads the intensity and " +
		"the rotation, and samples the texture, so it reads the authored image. That is what this cell asks.")
}

// TestWebGPUReadsNoEnvironmentMap corroborates the false cell.
//
// A PASS PROVES: the browser WebGPU renderer names none of the three authored
// environment fields, in any letter case, anywhere in the file. A viewer on the
// preferred backend therefore sees no contribution from an authored env-map,
// while the same scene shows one on WebGL2 and one in a poster.
//
// A PASS DOES NOT PROVE: that adding the three identifiers would close the gap.
// The renderer also needs a cube texture, a sampler, three uniform lanes and the
// two taps. This guard is the tripwire, not the specification.
//
// WHEN THIS TEST FAILS because the renderer gained the feature: flip the WebGPU
// cell to true, update 16a-scene-webgpu.capabilities.json in the same change, and
// rewrite this test to corroborate the new answer with the symbols that landed.
// Do not weaken the guard to keep it green.
func TestWebGPUReadsNoEnvironmentMap(t *testing.T) {
	webgpu := strings.ToLower(readRenderer(t, webgpuRendererPath))
	evidence := evidenceFor(t, FeatureEnvironmentMap, BackendWebGPU).
		refutedBy(webgpuRendererPath, webgpu, environmentMapIdentifiers...)
	evidence.assertAgrees("The browser WebGPU renderer names envMap, envIntensity and envRotation zero times, " +
		"so it cannot read the authored image. Its whole environment response is the hemisphere ambient term.")
}

// TestEnvironmentMapExcludesNoBackend pins that the row reports and does not
// gate.
//
// A PASS PROVES: environment-map is absent from DefaultPolicy().Required, so a
// scene with an authored env-map still lists WebGPU under Capable and lists the
// gap under Degraded.
//
// A PASS DOES NOT PROVE: that degrading is the right answer forever. It is the
// right answer while the gap is a missing term rather than a broken frame. A
// WebGPU page with no environment map still draws every other surface correctly,
// and a blank canvas would be worse. gpucull_test.go records the same reasoning
// for gpu-cull.
func TestEnvironmentMapExcludesNoBackend(t *testing.T) {
	if DefaultPolicy().Required[FeatureEnvironmentMap] {
		t.Fatal("environment-map entered DefaultPolicy().Required. A required feature EXCLUDES every backend " +
			"whose cell is false, so an authored env-map would push every viewer without WebGL2 to a blank " +
			"canvas over one missing lighting term. Remove it, or state why a blank frame beats a dimmer one.")
	}
	caps := Verdict([]Feature{FeatureEnvironmentMap}, nil, DefaultPolicy())
	var sawWebGPU bool
	for _, backend := range caps.Capable {
		if backend == BackendWebGPU {
			sawWebGPU = true
		}
	}
	if !sawWebGPU {
		t.Fatalf("WebGPU dropped out of Capable for a scene whose only feature is environment-map; got %v", caps.Capable)
	}
	var degraded bool
	for _, feature := range caps.Degraded[BackendWebGPU] {
		if feature == FeatureEnvironmentMap {
			degraded = true
		}
	}
	if !degraded {
		t.Fatalf("WebGPU is capable but reports no environment-map degradation; the author gets no warning. Degraded[webgpu]=%v",
			caps.Degraded[BackendWebGPU])
	}
}
