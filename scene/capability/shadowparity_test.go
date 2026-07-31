package capability

import (
	"strings"
	"testing"
)

// These tests corroborate the shadow-cascades and point-light-shadow rows
// against renderer source, the way water_shadow_test.go does for the water
// shadow passes. readRenderer, webgpuRendererPath and webglRendererPath come
// from lights_test.go.
//
// Both rows land with today's truth, ahead of the implementation PRs that
// flip the WebGPU cells (PR3 for shadow-cascades, PR5 for point-light-shadow).
// evidenceFor reads the renderer FIRST and demands the cell match, in
// whichever direction, so a stale cell fails this test instead of a manifest
// drift check that cannot see it.

// TestShadowCascadesCellsMatchRendererSource corroborates BOTH halves of the
// shadow-cascades row.
//
// WebGL2 already carries the parallel-split shadow map (PSSM) fit: the
// per-cascade splits and the per-slot cascade-selection branch.
//
// WebGPU renders one texture_depth_2d shadow map per slot with a single
// whole-scene ortho fit and no cascades, so the cell must read false until the
// renderer gains a texture_depth_2d_array slot. This test fails the day that
// symbol appears with the cell still false, which is exactly the flip signal:
// move the cell to true, update 16a-scene-webgpu.capabilities.json in the same
// commit, and add the positive WGSL string markers below.
func TestShadowCascadesCellsMatchRendererSource(t *testing.T) {
	webgl := readRenderer(t, webglRendererPath)
	evidenceFor(t, FeatureShadowCascades, BackendWebGL).
		needs(webglRendererPath, webgl, "u_shadowCascadeSplits0", "shadowFactorSlot0").
		assertAgrees("WebGL2 already splits the view frustum into cascades with a per-slot PSSM fit " +
			"and selects the active cascade by comparing view depth against u_shadowCascadeSplits0/1")

	webgpu := readRenderer(t, webgpuRendererPath)
	evidenceFor(t, FeatureShadowCascades, BackendWebGPU).
		needs(webgpuRendererPath, webgpu, "texture_depth_2d_array").
		assertAgrees("WebGPU renders one texture_depth_2d shadow map per slot with a single whole-scene " +
			"ortho fit and no per-cascade split; the cell flips to true only once the renderer gains a " +
			"texture_depth_2d_array slot and the shared PSSM fit from 16c-scene-shared-pbr.js")
}

// TestPointLightShadowIsWebGPUOnlyAfterPR5 corroborates BOTH halves of the
// point-light-shadow row.
//
// Neither renderer has a cube-shadow path yet, so both cells start false. The
// WebGPU cell is a staging value: it flips once the six-face cube depth path
// lands (bindings 13 and 14). The WebGL2 cell is a permanent record, not a
// staging value — see the reasoning pinned below.
func TestPointLightShadowIsWebGPUOnlyAfterPR5(t *testing.T) {
	webgpu := readRenderer(t, webgpuRendererPath)
	evidenceFor(t, FeaturePointLightShadow, BackendWebGPU).
		needs(webgpuRendererPath, webgpu, "texture_depth_cube", "renderPointShadowCubeFace").
		assertAgrees("no cube-shadow depth texture or per-face render pass exists in 16a-scene-webgpu.js " +
			"yet; the cell flips to true only once the six-face cube depth path lands")

	webgl := readRenderer(t, webglRendererPath)
	evidenceFor(t, FeaturePointLightShadow, BackendWebGL).
		refutedBy(webglRendererPath, webgl, "samplerCubeShadow").
		assertAgrees("the WebGL2 fallback renderer has no cube-shadow sampler; a real samplerCubeShadow " +
			"would need to flip the cell instead of just leaving this comment stale")

	// Pin WHY the WebGL2 cell is false, and that the reason is a resource
	// ceiling rather than a missing renderer feature. Material maps take
	// units 0-5, the worst-case CSM allocation takes units 6-13, and IBL
	// takes 3 more — 18 units against a 16-unit spec-minimum device, the same
	// gate TestIBLIsFalseOnBothBackends pins for the ibl cell. A
	// samplerCubeShadow would need a 19th unit on a renderer that is already
	// over budget before it exists.
	for _, marker := range []string{"maxUnits >= 18", "fragment-texture-units<18"} {
		if !strings.Contains(webgl, marker) {
			t.Errorf("expected the staged WebGL2 resource gate %q to still be present; "+
				"it is why point-light-shadow stays false on WebGL2 rather than becoming a staging value", marker)
		}
	}
}

// TestShadowFeaturesDegradeRatherThanExclude pins the policy half, the same
// shape as TestWaterObjectMeshShadowPassDegradesRatherThanExcludes.
//
// A scene that loses a shadow term still renders the same geometry — a
// degraded image, not a different scene. Requiring shadow-cascades would
// exclude WebGPU today; requiring point-light-shadow would exclude WebGL2
// forever and empty Capable on every non-WebGPU browser, the exact failure
// water_shadow_test.go pins for the water mesh-shadow pass.
func TestShadowFeaturesDegradeRatherThanExclude(t *testing.T) {
	pol := DefaultPolicy()
	if pol.Required[FeatureShadowCascades] {
		t.Fatal("shadow-cascades must not be required: it would exclude WebGPU while its cell is false")
	}
	if pol.Required[FeaturePointLightShadow] {
		t.Fatal("point-light-shadow must not be required: it would exclude WebGL2 permanently")
	}

	caps := Verdict([]Feature{FeatureShadowCascades, FeaturePointLightShadow}, nil, pol)

	capable := map[Backend]bool{}
	for _, b := range caps.Capable {
		capable[b] = true
	}
	if !capable[BackendWebGPU] {
		t.Errorf("WebGPU must stay capable; Capable=%v", caps.Capable)
	}
	if !capable[BackendWebGL] {
		t.Errorf("WebGL2 must stay capable and merely degraded; Capable=%v", caps.Capable)
	}

	wantWebGPU := map[Feature]bool{FeatureShadowCascades: true, FeaturePointLightShadow: true}
	wantWebGL := map[Feature]bool{FeaturePointLightShadow: true}
	for _, f := range caps.Degraded[BackendWebGPU] {
		if !wantWebGPU[f] {
			t.Errorf("unexpected WebGPU degradation %q; Degraded=%v", f, caps.Degraded[BackendWebGPU])
		}
		delete(wantWebGPU, f)
	}
	if len(wantWebGPU) > 0 {
		t.Errorf("WebGPU is missing degradations: %v; Degraded=%v", wantWebGPU, caps.Degraded[BackendWebGPU])
	}
	for _, f := range caps.Degraded[BackendWebGL] {
		if !wantWebGL[f] {
			t.Errorf("unexpected WebGL2 degradation %q; Degraded=%v", f, caps.Degraded[BackendWebGL])
		}
		delete(wantWebGL, f)
	}
	if len(wantWebGL) > 0 {
		t.Errorf("WebGL2 is missing degradations: %v; Degraded=%v", wantWebGL, caps.Degraded[BackendWebGL])
	}

	for _, reason := range caps.Reasons {
		if reason.Excludes != "" {
			t.Errorf("no shadow feature may exclude a backend; got %+v", reason)
		}
	}
}
