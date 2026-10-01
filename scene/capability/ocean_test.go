package capability

import "testing"

func TestGPUBackendsDrawTheOcean(t *testing.T) {
	webgpu := readRenderer(t, webgpuRendererPath)
	webgl := readRenderer(t, webglRendererPath)
	evidenceFor(t, FeatureOcean, BackendWebGPU).
		needs(webgpuRendererPath, webgpu, "WGSL_SCENE_OCEAN", "wgpuCreateOceanRenderer", "wgpuOceanDraw(oceanResources, mainPass, oceanOpts)").
		assertAgrees("WebGPU draws the ocean in the main pass after opaque geometry")
	evidenceFor(t, FeatureOcean, BackendWebGL).
		needs(webglRendererPath, webgl, "SCENE_OCEAN_VERTEX_GLSL", "createSceneOceanWebGLRenderer", "sceneOceanWebGLDraw(oceanResources, gl,").
		assertAgrees("WebGL2 draws the ocean after opaque geometry")
	if DefaultPolicy().Required[FeatureOcean] || Supports(BackendCanvas2D, FeatureOcean) {
		t.Fatalf("%s must degrade to no ocean on Canvas2D and must not be required", FeatureOcean)
	}
}
