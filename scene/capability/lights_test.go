package capability

import (
	"os"
	"strings"
	"testing"
)

// These tests corroborate every light-related Matrix cell against renderer
// source, the way gpupicking_test.go does for gpu-picking.
//
// The drift guard in drift_test.go only proves that Matrix agrees with two
// hand-written manifests. It cannot catch a manifest that lies about the
// renderer; that exact failure already happened with gpu-picking. So a cell
// added here must name the symbols that make it true, and a cell left false
// must show that the implementation is genuinely absent.

const (
	webgpuRendererPath = "../../client/runtime/scene3d/webgpu.ts"
	webglRendererPath  = "../../client/runtime/scene3d/webgl.ts"
	sharedPbrPath      = "../../client/js/bootstrap-src/16c-scene-shared-pbr.ts"
	sceneGraphPath     = "../scene.go"
)

func readRenderer(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// TestEveryLoweredLightKindIsAccountedFor reads the light lowerers in
// scene/scene.go and demands that each Kind it writes into LightIR is either
// exactly rendered on both GPU backends or mapped to a Matrix feature.
//
// This is the guard against the defect this file was written for. The WebGPU
// renderer used to shade only ambient and directional; spot, hemisphere,
// rect-area and light-probe all reached the shader as point lights, with no
// Matrix cell and no diagnostic to say so.
func TestEveryLoweredLightKindIsAccountedFor(t *testing.T) {
	kinds := loweredLightKinds(t)
	if len(kinds) != 7 {
		t.Fatalf("scene.go lowers %d light kinds, want 7: %v", len(kinds), kinds)
	}

	// The kinds both GPU backends shade with the same math. Each one is
	// corroborated by a test below, so an empty feature list is a claim with
	// evidence behind it.
	exact := map[string]bool{
		"ambient":     true,
		"directional": true,
		"point":       true,
		"spot":        true,
		"hemisphere":  true,
	}
	// The kinds that carry a real gap. Each must map to at least one feature.
	gapped := map[string]bool{
		"rect-area":   true,
		"light-probe": true,
	}

	for _, kind := range kinds {
		features := LightKindFeatures(kind)
		switch {
		case exact[kind]:
			if len(features) != 0 {
				t.Errorf("kind %q is exactly rendered but claims features %v", kind, features)
			}
		case gapped[kind]:
			if len(features) == 0 {
				t.Errorf("kind %q has a known gap but LightKindFeatures returns nothing", kind)
			}
			for _, f := range features {
				if _, ok := Matrix[f]; !ok {
					t.Errorf("kind %q maps to feature %q, which has no Matrix row", kind, f)
				}
			}
		default:
			t.Errorf("scene.go lowers light kind %q, which this test does not classify; "+
				"decide whether it renders exactly or needs a Matrix feature", kind)
		}
	}
}

// loweredLightKinds pulls the Kind literal out of every LightIR the graph
// lowerer appends. Reading the source keeps the list honest: add a light type
// to scene.go and this test fails until the capability side says what it costs.
func loweredLightKinds(t *testing.T) []string {
	t.Helper()
	source := readRenderer(t, sceneGraphPath)
	const anchor = "l.lights = append(l.lights, LightIR{"
	var kinds []string
	cursor := 0
	for {
		at := strings.Index(source[cursor:], anchor)
		if at < 0 {
			break
		}
		at += cursor
		cursor = at + len(anchor)
		rest := source[cursor:]
		field := strings.Index(rest, "Kind:")
		if field < 0 {
			t.Fatalf("a LightIR literal at offset %d has no Kind field", at)
		}
		quoted := rest[field:]
		open := strings.Index(quoted, `"`)
		if open < 0 {
			t.Fatalf("the Kind field at offset %d is not a string literal", at)
		}
		closing := strings.Index(quoted[open+1:], `"`)
		if closing < 0 {
			t.Fatalf("the Kind field at offset %d is unterminated", at)
		}
		kinds = append(kinds, quoted[open+1:open+1+closing])
	}
	return kinds
}

// assertLightKindClaimMatchesRenderers demands that the LightKindFeatures answer
// for kind agree with renderer source, in whichever direction.
//
// LightKindFeatures returning an empty list is a claim: "both GPU backends shade
// this kind exactly, so it costs the author nothing". missing holds the evidence
// symbols the renderers do NOT carry. Empty missing means the claim holds, so an
// empty feature list is right and a non-empty one is wrong. Non-empty missing
// means the opposite.
//
// The two spot and hemisphere tests below used to skip when the feature list
// stopped being empty, so adding a feature deleted the check that the renderers
// still match. This reads the renderers first, exactly as the Matrix cell tests
// now do.
func assertLightKindClaimMatchesRenderers(t *testing.T, kind string, missing []string) {
	t.Helper()
	features := LightKindFeatures(kind)
	claimsExact := len(features) == 0
	rendererIsExact := len(missing) == 0

	switch {
	case rendererIsExact && !claimsExact:
		t.Fatalf("both renderers carry every %s term, yet LightKindFeatures(%q) returns %v.\n"+
			"An exactly rendered kind must claim no feature, or the author reads a gap that is not there. "+
			"Either drop the mapping or say which term the renderers lost.", kind, kind, features)
	case !rendererIsExact && claimsExact:
		t.Fatalf("LightKindFeatures(%q) claims no capability gap, but the renderers are missing: %s\n"+
			"A kind that is not shaded exactly on both backends owes the author a Matrix feature, "+
			"or the gap goes unreported.", kind, strings.Join(sortedCopy(missing), ", "))
	}
	// Both agree. Name the direction, so a reader of a passing run knows which
	// half of the contract held.
	t.Logf("%s: renderers exact=%v, LightKindFeatures=%v", kind, rendererIsExact, features)
}

// TestSpotLightImplementedOnBothBackends corroborates the empty feature list
// for spot lights. Both renderers must carry the same three cone terms. If
// either drops out, LightKindFeatures owes spot a feature.
func TestSpotLightImplementedOnBothBackends(t *testing.T) {
	webgpu := readRenderer(t, webgpuRendererPath)
	webgl := readRenderer(t, webglRendererPath)

	missing := missingSymbols(webgpuRendererPath, webgpu,
		"fn spotConeAttenuation(",
		"let outerCos = cos(angle);",
		"let innerCos = cos(angle * (1.0 - penumbra));",
		"clamp((cosAngle - outerCos) / max(innerCos - outerCos, 0.001), 0.0, 1.0)",
		"lightType == 3u",
		`case "spot": return 3;`,
	)
	missing = append(missing, missingSymbols(webglRendererPath, webgl,
		"float outerCos = cos(u_lightAngles[i]);",
		"float innerCos = cos(u_lightAngles[i] * (1.0 - u_lightPenumbras[i]));",
		"clamp((cosAngle - outerCos) / max(innerCos - outerCos, 0.001), 0.0, 1.0)",
	)...)

	assertLightKindClaimMatchesRenderers(t, "spot", missing)
}

// TestHemisphereLightImplementedOnBothBackends corroborates the empty feature
// list for hemisphere lights: both renderers blend sky against ground with the
// same normal-Y term.
func TestHemisphereLightImplementedOnBothBackends(t *testing.T) {
	webgpu := readRenderer(t, webgpuRendererPath)
	webgl := readRenderer(t, webglRendererPath)

	missing := missingSymbols(webgpuRendererPath, webgpu,
		"lightType == 4u",
		"let hBlend = N.y * 0.5 + 0.5;",
		"mix(light.groundPenumbra.rgb, lightColor, hBlend)",
		`case "hemisphere": return 4;`,
	)
	missing = append(missing, missingSymbols(webglRendererPath, webgl,
		"float hBlend = N.y * 0.5 + 0.5;",
		"mix(u_lightGroundColors[i], lightColor, hBlend)",
	)...)

	assertLightKindClaimMatchesRenderers(t, "hemisphere", missing)
}

// TestRectAreaLightWebGPUEvidence ties the true cell to the shipped renderer.
// The named symbols are the load-bearing parts of the polygon form factor:
//
//   - sceneWebGPURectAreaBasis:    builds the two in-plane edge vectors
//   - areaHalfWidth/areaHalfHeight: the Light struct fields that carry them
//   - rectAreaFormFactor:          the four corners and the visibility test
//   - ltcEdgeVectorFormFactor:     the edge integral, with three.js constants
//   - ltcClippedSphereFormFactor:  the closing term
//   - lightType == 5u:             the shader branch that runs it
//
// The test reads the renderer FIRST and then demands the cell match, in whichever
// direction. It used to skip on a false cell, so flipping this cell to false —
// which would silently stop reporting a real rect-area shape as supported —
// deleted the check instead of failing it.
func TestRectAreaLightWebGPUEvidence(t *testing.T) {
	source := readRenderer(t, webgpuRendererPath)

	evidenceFor(t, FeatureRectAreaLight, BackendWebGPU).
		needs(webgpuRendererPath, source,
			"function sceneWebGPURectAreaBasis(",
			"areaHalfWidth: vec4f,",
			"areaHalfHeight: vec4f,",
			"fn rectAreaFormFactor(",
			"fn ltcEdgeVectorFormFactor(",
			"fn ltcClippedSphereFormFactor(",
			"fn rectAreaLightRadiance(",
			"lightType == 5u",
			`case "rect-area": return 5;`,
			// The three.js edge-integral fit. These constants are what make the
			// diffuse term exact rather than a guess.
			"0.8543985 + (0.4965155 + 0.0145206 * y) * y",
			"3.4175940 + (4.1616724 + y) * y",
		).
		assertAgrees("a rect-area light needs the in-plane basis, the two shape fields, the polygon " +
			"form factor with its edge integral, and the shader branch that runs them")
}

// TestRectAreaLightWebGLIsFalse records why the WebGL cell is false. The WebGL2
// renderer folds kind "rect-area" onto light type 2, the point light, so the
// authored Width and Height never reach a fragment. This test fails if WebGL
// gains a rect-area path, which is the moment the cell must flip.
// The test reads the renderer FIRST and then demands the cell match. It used to
// skip when the cell read true, which is the one case worth catching: a true
// rect-area cell on WebGL2 would report a point-light image as an exact one.
func TestRectAreaLightWebGLIsFalse(t *testing.T) {
	source := readRenderer(t, webglRendererPath)

	// A rect-area path would need the shape uniforms or the linearly transformed
	// cosine (LTC) evaluation. Their absence is the evidence for the false cell,
	// so any of them appearing must flip it.
	evidenceFor(t, FeatureRectAreaLight, BackendWebGL).
		refutedBy(webglRendererPath, source, "u_lightWidths", "u_lightHeights", "LTC_Evaluate").
		assertAgrees("the WebGL2 renderer folds kind \"rect-area\" onto light type 2, the point light, " +
			"so the authored Width and Height never reach a fragment")

	// And pin WHY it is false. The fold onto the point light is the mechanism,
	// so a reader does not have to rediscover it.
	anchor := strings.Index(source, `} else if (kind === "rect-area") {`)
	if anchor < 0 {
		t.Fatalf("the WebGL rect-area branch moved; re-check Matrix[rect-area-light][webgl]")
	}
	branch := source[anchor : anchor+120]
	if !strings.Contains(branch, "lightType = 2;") {
		t.Errorf("the WebGL rect-area branch no longer maps to the point light: %q\n"+
			"If WebGL2 gained a real rect-area shape, flip the cell, fix the manifest and "+
			"rewrite this test to corroborate the new path.", branch)
	}
}

// TestRectAreaSpecularUnimplementedEverywhere corroborates the false cells.
// three.js shapes a rect-area specular highlight from two fitted lookup
// tables. Neither renderer uploads them, so neither may claim the feature.
// The test fails when a table appears, which is the moment a cell can flip.
func TestRectAreaSpecularUnimplementedEverywhere(t *testing.T) {
	for backend, path := range map[Backend]string{
		BackendWebGPU: webgpuRendererPath,
		BackendWebGL:  webglRendererPath,
	} {
		if Matrix[FeatureRectAreaSpecular][backend] {
			t.Errorf("Matrix[rect-area-specular][%s] is true; add the LTC table evidence to this test", backend)
			continue
		}
		source := readRenderer(t, path)
		for _, table := range []string{"ltc_1", "ltc_2", "LTC_Uv", "ltcTexture"} {
			if strings.Contains(source, table) {
				t.Errorf("%s references %q; an LTC table may exist now, so re-check Matrix[rect-area-specular][%s]",
					path, table, backend)
			}
		}
	}
	// WebGPU substitutes a representative point. Name it, so the gap is a
	// recorded approximation and not an accident.
	source := readRenderer(t, webgpuRendererPath)
	if !strings.Contains(source, "fn rectAreaRepresentativePoint(") {
		t.Error("the WebGPU rect-area specular stand-in disappeared; re-check what it does now")
	}
	if !strings.Contains(source, `code: "rect-area-specular"`) {
		t.Errorf("the renderer must report the rect-area specular gap to the author; "+
			"see sceneWebGPULightIssues in %s", webgpuRendererPath)
	}
}

// TestLightProbeSHBidirectionalEvidence ties the true GPU cells to the
// shipped renderers, in whichever direction. The named symbols are the
// load-bearing parts of the SH path: the shared structural validation and
// double-precision aggregation, each renderer's nine-coefficient upload, and
// the per-fragment second-order evaluation against the shading normal.
// Coefficientless and malformed probes keep the legacy ambient fallback.
func TestLightProbeSHBidirectionalEvidence(t *testing.T) {
	sharedPbr := readRenderer(t, sharedPbrPath)
	evidenceFor(t, FeatureLightProbeSH, BackendWebGPU).
		needs(sharedPbrPath, sharedPbr,
			"function scenePBRProbeCoefficientsValid(",
			"function scenePBRProbeAggregate(",
		).
		needs(webgpuRendererPath, readRenderer(t, webgpuRendererPath),
			"hasProbeSH: u32,",
			"probeSH: array<vec4f, 9>",
			"scenePBRProbeAggregate(lightArray, count, _sceneWebGPUProbeAgg)",
			"device.queue.writeBuffer(envUniformBuffer, 68, _envUniformF, 17, 39)",
			"if (env.hasProbeSH != 0u) {",
			"ps0 * 0.886227",
			"ps8 * (0.429043 * (N.x * N.x - N.y * N.y))",
		).
		assertAgrees("a WebGPU probe needs the shared valid9 aggregate, the nine-coefficient " +
			"EnvUniforms tail upload, and the per-fragment SH irradiance evaluation")

	evidenceFor(t, FeatureLightProbeSH, BackendWebGL).
		needs(webglRendererPath, readRenderer(t, webglRendererPath),
			"uniform vec3 u_probeSH[9];",
			"uniform bool u_hasProbeSH;",
			"probeIrr = max(u_probeSH[0] * 0.886227",
			"+ u_probeSH[8] * (0.429043 * (N.x * N.x - N.y * N.y))",
		).
		assertAgrees("a WebGL2 probe needs the u_probeSH uniform array and the per-fragment " +
			"SH irradiance evaluation in the fragment shader")
}

// TestLightFeaturesNeverExcludeWebGPU pins the gate's shape. Every feature in
// DefaultPolicy().Required is WebGPU-true today, so no wire-detectable feature
// can drop WebGPU from Capable; the gate can only degrade it. The new light
// features must not change that, or one RectAreaLight would push every page
// onto WebGL2 — the same failure gpu-picking caused.
func TestLightFeaturesNeverExcludeWebGPU(t *testing.T) {
	features := []Feature{FeatureRectAreaLight, FeatureRectAreaSpecular, FeatureLightProbeSH}
	policy := DefaultPolicy()
	for _, f := range features {
		if policy.Required[f] {
			t.Fatalf("feature %q is required, so an unsupported cell would EXCLUDE a backend; "+
				"a light-shading gap must degrade, not exclude", f)
		}
	}
	caps := Verdict(features, nil, policy)
	capable := map[Backend]bool{}
	for _, b := range caps.Capable {
		capable[b] = true
	}
	if !capable[BackendWebGPU] {
		t.Errorf("the light features must keep WebGPU capable; Capable=%v", caps.Capable)
	}
	if !capable[BackendWebGL] {
		t.Errorf("the light features must keep WebGL capable; Capable=%v", caps.Capable)
	}
	for _, reason := range caps.Reasons {
		if reason.Excludes != "" {
			t.Errorf("no light feature may exclude a backend; got %+v", reason)
		}
	}
	// WebGPU degrades only on rect-area-specular: light-probe SH now
	// evaluates on both GPU backends. WebGL degrades on rect-area-light and
	// rect-area-specular, because it has no rect-area shape.
	if got := len(caps.Degraded[BackendWebGPU]); got != 1 {
		t.Errorf("WebGPU must degrade on exactly rect-area-specular; got %v", caps.Degraded[BackendWebGPU])
	}
	if got := len(caps.Degraded[BackendWebGL]); got != 2 {
		t.Errorf("WebGL must degrade on exactly the two rect-area features; got %v", caps.Degraded[BackendWebGL])
	}
}

// TestLightKindFeaturesNormalizesInput keeps the mapping usable from wire data,
// where a kind may arrive with different case or stray whitespace.
func TestLightKindFeaturesNormalizesInput(t *testing.T) {
	for _, kind := range []string{"rect-area", "Rect-Area", "  rect-area  ", "RECT-AREA"} {
		if len(LightKindFeatures(kind)) != 2 {
			t.Errorf("LightKindFeatures(%q) must resolve to the rect-area features", kind)
		}
	}
	for _, kind := range []string{"", "   ", "unknown", "box"} {
		if got := LightKindFeatures(kind); got != nil {
			t.Errorf("LightKindFeatures(%q) = %v, want nil", kind, got)
		}
	}
}
