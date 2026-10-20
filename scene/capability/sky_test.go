package capability

import "testing"

func TestGPUBackendsDrawSky(t *testing.T) {
	webgpu := readRenderer(t, webgpuRendererPath)
	webgl := readRenderer(t, webglRendererPath)
	for _, feature := range []Feature{FeatureSkyEnvironment, FeatureSkyGradient, FeatureSkyPhysical} {
		evidenceFor(t, feature, BackendWebGPU).
			needs(webgpuRendererPath, webgpu, "WGSL_SCENE_SKY", "wgpuCreateSkyRenderer", "skyResources.renderer.draw(mainPass").
			assertAgrees("WebGPU draws the sky before world geometry")
		evidenceFor(t, feature, BackendWebGL).
			needs(webglRendererPath, webgl, "SCENE_SKY_FRAGMENT", "createSceneSkyWebGLRenderer", "skyResources.renderer.draw({").
			assertAgrees("WebGL2 draws the sky before world geometry")
		if DefaultPolicy().Required[feature] || Supports(BackendCanvas2D, feature) {
			t.Fatalf("%s must degrade to the flat background on Canvas2D", feature)
		}
		got := Verdict([]Feature{feature}, nil, DefaultPolicy())
		if !backendsEqual(got.Capable, []Backend{BackendWebGPU, BackendWebGL, BackendCanvas2D}) ||
			!degradedEqual(got.Degraded, map[Backend][]Feature{BackendCanvas2D: {feature}}) {
			t.Fatalf("unexpected sky capabilities: %+v", got)
		}
	}
}

func TestGPUBackendsDrawPhysicalSky(t *testing.T) {
	webgpu := readRenderer(t, webgpuRendererPath)
	webgl := readRenderer(t, webglRendererPath)
	evidenceFor(t, FeatureSkyPhysical, BackendWebGPU).
		needs(webgpuRendererPath, webgpu, "sceneSkyPhysicalSource(\"wgsl\")", "gosxPhysicalSky(ray, sky.betaR", "sky.bottom.w == 4").
		assertAgrees("WebGPU evaluates the physical sky in the sky pass")
	evidenceFor(t, FeatureSkyPhysical, BackendWebGL).
		needs(webglRendererPath, webgl, "sceneSkyPhysicalSource(\"glsl\")", "gosxPhysicalSky(ray, u_sky[7]", "u_sky[5].w == 4.").
		assertAgrees("WebGL2 evaluates the physical sky in the sky pass")
}
