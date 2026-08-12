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

// TestWebGPUReadsTheEnvironmentMap corroborates the true cell.
//
// A PASS PROVES: the WebGPU renderer declares the two envMap bindings (a
// dedicated repeat/clamp-to-edge sampler, not the IBL sampler), ports
// envEquirectUV verbatim so both backends sample the same texel for the same
// direction, and taps it for both the diffuse and specular terms exactly
// where WebGL2 orders the branch: after IBL, before the hemisphere fallback.
//
// A PASS DOES NOT PROVE: that the result is image-based lighting. It is not.
// The renderer taps one texture twice and scales the second tap by
// (1.0 - roughness * 0.65), the same undereived factor WebGL2 and
// render/bundle/lit.go carry. The ibl row stays keyed on the split-sum
// consumer instead.
func TestWebGPUReadsTheEnvironmentMap(t *testing.T) {
	webgpu := readRenderer(t, webgpuRendererPath)
	evidence := evidenceFor(t, FeatureEnvironmentMap, BackendWebGPU).
		needs(webgpuRendererPath, webgpu,
			"var envMapTex: texture_2d<f32>",
			"var envMapSampler: sampler",
			"fn envEquirectUV(dir: vec3f) -> vec2f",
			"env.hasEnvMap != 0u",
			"textureSample(envMapTex, envMapSampler, envEquirectUV(Nr))",
			"textureSample(envMapTex, envMapSampler, envEquirectUV(Rr))",
			"(1.0 - roughness * 0.65)",
		)
	evidence.assertAgrees("The WebGPU renderer binds a dedicated envMap sampler and texture at group(0) " +
		"bindings 13/14, ports envEquirectUV verbatim from the GLSL source, and taps it for both the " +
		"diffuse and specular terms, so it reads the authored legacy image. That is what this cell asks.")
}

// TestEnvironmentMapExcludesNoBackend pins that the row reports and does not
// gate, now that both GPU backends implement it.
//
// A PASS PROVES: environment-map is absent from DefaultPolicy().Required, so a
// scene with an authored env-map lists both GPU backends under Capable with no
// gap, and lists Canvas2D's blanket exclusion under Degraded (it rasterizes no
// triangle, so it reads no environment map either).
//
// A PASS DOES NOT PROVE: that a Canvas2D gap always stays droppable. It is the
// right answer while the gap is a missing term rather than a broken frame.
// gpucull_test.go records the same reasoning for gpu-cull.
func TestEnvironmentMapExcludesNoBackend(t *testing.T) {
	if DefaultPolicy().Required[FeatureEnvironmentMap] {
		t.Fatal("environment-map entered DefaultPolicy().Required. A required feature EXCLUDES every backend " +
			"whose cell is false, so an authored env-map would push a Canvas2D-only viewer to a blank canvas " +
			"over one missing lighting term. Remove it, or state why a blank frame beats a dimmer one.")
	}
	caps := Verdict([]Feature{FeatureEnvironmentMap}, nil, DefaultPolicy())
	for _, want := range []Backend{BackendWebGPU, BackendWebGL, BackendCanvas2D} {
		var found bool
		for _, backend := range caps.Capable {
			if backend == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s dropped out of Capable for a scene whose only feature is environment-map; got %v", want, caps.Capable)
		}
	}
	for _, gpu := range []Backend{BackendWebGPU, BackendWebGL} {
		for _, feature := range caps.Degraded[gpu] {
			if feature == FeatureEnvironmentMap {
				t.Fatalf("%s reports an environment-map degradation, but both GPU backends implement the "+
					"feature now; Degraded[%s]=%v", gpu, gpu, caps.Degraded[gpu])
			}
		}
	}
	var canvasDegraded bool
	for _, feature := range caps.Degraded[BackendCanvas2D] {
		if feature == FeatureEnvironmentMap {
			canvasDegraded = true
		}
	}
	if !canvasDegraded {
		t.Fatalf("Canvas2D is capable but reports no environment-map degradation; the author gets no warning. "+
			"Degraded[canvas2d]=%v", caps.Degraded[BackendCanvas2D])
	}
}
