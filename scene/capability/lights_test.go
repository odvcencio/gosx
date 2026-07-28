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
	webgpuRendererPath = "../../client/js/bootstrap-src/16a-scene-webgpu.js"
	webglRendererPath  = "../../client/js/bootstrap-src/16-scene-webgl.js"
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

// rendererFeaturePhaseComplete accepts the two coherent stack phases: every
// implementation symbol is absent in the capability-only slice, or every
// symbol is present once the owning runtime slice lands. A partial
// implementation fails in either phase.
func rendererFeaturePhaseComplete(t *testing.T, label, source string, symbols []string) bool {
	t.Helper()
	present := 0
	for _, symbol := range symbols {
		if strings.Contains(source, symbol) {
			present++
		}
	}
	if present == 0 {
		return false
	}
	if present == len(symbols) {
		return true
	}
	for _, symbol := range symbols {
		if !strings.Contains(source, symbol) {
			t.Errorf("%s is partially implemented; missing %q", label, symbol)
		}
	}
	return false
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

// TestSpotLightImplementedOnBothBackends corroborates the empty feature list
// for spot lights. Both renderers must carry the same three cone terms. If
// either drops out, LightKindFeatures owes spot a feature.
func TestSpotLightImplementedOnBothBackends(t *testing.T) {
	if len(LightKindFeatures("spot")) != 0 {
		t.Skip("spot now maps to a feature; the claim under test is gone")
	}
	webgpu := readRenderer(t, webgpuRendererPath)
	webgpuSymbols := []string{
		"fn spotConeAttenuation(",
		"let outerCos = cos(angle);",
		"let innerCos = cos(angle * (1.0 - penumbra));",
		"clamp((cosAngle - outerCos) / max(innerCos - outerCos, 0.001), 0.0, 1.0)",
		"lightType == 3u",
		`case "spot": return 3;`,
	}
	rendererFeaturePhaseComplete(t, "WebGPU spot light", webgpu, webgpuSymbols)
	webgl := readRenderer(t, webglRendererPath)
	webglSymbols := []string{
		"float outerCos = cos(u_lightAngles[i]);",
		"float innerCos = cos(u_lightAngles[i] * (1.0 - u_lightPenumbras[i]));",
		"clamp((cosAngle - outerCos) / max(innerCos - outerCos, 0.001), 0.0, 1.0)",
	}
	if !rendererFeaturePhaseComplete(t, "WebGL spot light", webgl, webglSymbols) {
		t.Error("WebGL spot-light implementation must remain complete")
	}
}

// TestHemisphereLightImplementedOnBothBackends corroborates the empty feature
// list for hemisphere lights: both renderers blend sky against ground with the
// same normal-Y term.
func TestHemisphereLightImplementedOnBothBackends(t *testing.T) {
	if len(LightKindFeatures("hemisphere")) != 0 {
		t.Skip("hemisphere now maps to a feature; the claim under test is gone")
	}
	webgpu := readRenderer(t, webgpuRendererPath)
	webgpuSymbols := []string{
		"lightType == 4u",
		"let hBlend = N.y * 0.5 + 0.5;",
		"mix(light.groundPenumbra.rgb, lightColor, hBlend)",
		`case "hemisphere": return 4;`,
	}
	rendererFeaturePhaseComplete(t, "WebGPU hemisphere light", webgpu, webgpuSymbols)
	webgl := readRenderer(t, webglRendererPath)
	webglSymbols := []string{
		"float hBlend = N.y * 0.5 + 0.5;",
		"mix(u_lightGroundColors[i], lightColor, hBlend)",
	}
	if !rendererFeaturePhaseComplete(t, "WebGL hemisphere light", webgl, webglSymbols) {
		t.Error("WebGL hemisphere-light implementation must remain complete")
	}
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
func TestRectAreaLightWebGPUEvidence(t *testing.T) {
	if !Matrix[FeatureRectAreaLight][BackendWebGPU] {
		t.Skip("Matrix says WebGPU has no rect-area light; nothing to corroborate")
	}
	source := readRenderer(t, webgpuRendererPath)
	for _, symbol := range []string{
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
	} {
		if !strings.Contains(source, symbol) {
			t.Errorf("Matrix[rect-area-light][webgpu] is true but %s is missing from %s; "+
				"flip the cell back or finish the implementation", symbol, webgpuRendererPath)
		}
	}
}

// TestRectAreaLightWebGLIsFalse records why the WebGL cell is false. The WebGL2
// renderer folds kind "rect-area" onto light type 2, the point light, so the
// authored Width and Height never reach a fragment. This test fails if WebGL
// gains a rect-area path, which is the moment the cell must flip.
func TestRectAreaLightWebGLIsFalse(t *testing.T) {
	if Matrix[FeatureRectAreaLight][BackendWebGL] {
		t.Skip("Matrix says WebGL now shades rect-area lights; corroborate the new path instead")
	}
	source := readRenderer(t, webglRendererPath)
	anchor := strings.Index(source, `} else if (kind === "rect-area") {`)
	if anchor < 0 {
		t.Fatalf("the WebGL rect-area branch moved; re-check Matrix[rect-area-light][webgl]")
	}
	branch := source[anchor : anchor+120]
	if !strings.Contains(branch, "lightType = 2;") {
		t.Errorf("the WebGL rect-area branch no longer maps to the point light: %q", branch)
	}
	// A rect-area path would need the shape uniforms. Their absence is the
	// evidence for the false cell.
	for _, absent := range []string{"u_lightWidths", "u_lightHeights", "LTC_Evaluate"} {
		if strings.Contains(source, absent) {
			t.Errorf("%s appeared in %s; WebGL may now shade rect-area lights, so re-check the cell",
				absent, webglRendererPath)
		}
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
	// Once the WebGPU runtime slice exists, its representative-point stand-in
	// and author diagnostic must land together. Both are absent in the
	// capability-only slice.
	source := readRenderer(t, webgpuRendererPath)
	hasStandIn := strings.Contains(source, "fn rectAreaRepresentativePoint(")
	hasIssue := strings.Contains(source, `code: "rect-area-specular"`)
	if hasStandIn != hasIssue {
		t.Errorf("WebGPU rect-area stand-in and diagnostic must land together: standIn=%v issue=%v", hasStandIn, hasIssue)
	}
}

// TestLightProbeSHUnimplementedEverywhere corroborates the false cells. Neither
// renderer reads LightIR.Coefficients. Both fold a probe into the ambient term,
// which is defensible and equal across backends, but it is not an SH
// evaluation.
func TestLightProbeSHUnimplementedEverywhere(t *testing.T) {
	for backend, path := range map[Backend]string{
		BackendWebGPU: webgpuRendererPath,
		BackendWebGL:  webglRendererPath,
	} {
		if Matrix[FeatureLightProbeSH][backend] {
			t.Errorf("Matrix[light-probe-sh][%s] is true; add the SH evaluation evidence to this test", backend)
			continue
		}
		source := readRenderer(t, path)
		// Reading the coefficients to COUNT them is fine; the WebGPU renderer
		// does that to warn the author. Shading with them would need a
		// spherical-harmonic basis, so look for that instead.
		for _, symbol := range []string{"shBasis", "sphericalHarmonic", "shCoefficients"} {
			if strings.Contains(source, symbol) {
				t.Errorf("%s references %q; SH probe shading may exist now, so re-check Matrix[light-probe-sh][%s]",
					path, symbol, backend)
			}
		}
	}
	// Both backends must fold a probe into ambient, never into point. Point
	// invents a distance falloff a positionless probe cannot have.
	webgpu := readRenderer(t, webgpuRendererPath)
	webgl := readRenderer(t, webglRendererPath)
	anchor := strings.Index(webgl, `} else if (kind === "light-probe") {`)
	if anchor < 0 {
		t.Fatalf("the WebGL light-probe branch moved; re-check the probe fold")
	}
	if !strings.Contains(webgl[anchor:anchor+120], "lightType = 0;") {
		t.Error("the WebGL probe no longer folds into ambient; the two backends have diverged")
	}
	hasTypeCode := strings.Contains(webgpu, "function sceneWebGPULightTypeCode(")
	hasIssues := strings.Contains(webgpu, "function sceneWebGPULightIssues(")
	if hasTypeCode != hasIssues {
		t.Errorf("WebGPU light typing and issue reporting must land together: typeCode=%v issues=%v", hasTypeCode, hasIssues)
	}
	if hasTypeCode {
		if !strings.Contains(webgpu, `case "light-probe": return 0;`) {
			t.Errorf("the WebGPU probe must shade as ambient (code 0); see sceneWebGPULightTypeCode in %s",
				webgpuRendererPath)
		}
		if !strings.Contains(webgpu, `code: "light-probe-sh"`) {
			t.Errorf("the renderer must report ignored coefficients; see sceneWebGPULightIssues in %s",
				webgpuRendererPath)
		}
	}
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
	for _, backend := range []Backend{BackendWebGPU, BackendWebGL} {
		want := 0
		for _, feature := range features {
			if !Matrix[feature][backend] {
				want++
			}
		}
		if got := len(caps.Degraded[backend]); got != want {
			t.Errorf("%s must degrade on %d unsupported light features; got %v", backend, want, caps.Degraded[backend])
		}
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
