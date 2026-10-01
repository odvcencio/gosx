package capability

import "testing"

func TestGPUBackendsRefractOpaqueScene(t *testing.T) {
	webgpu := readRenderer(t, webgpuRendererPath)
	webgl := readRenderer(t, webglRendererPath)
	evidenceFor(t, FeatureTransmission, BackendWebGPU).
		needs(webgpuRendererPath, webgpu, "WGSL_TRANSMISSION", "refract(-V, N, 1.0 / ior)", "textureSampleLevel(transmissionScene", "transmissionResources.capture(encoder, postTarget.colorView)").
		assertAgrees("WebGPU captures opaque HDR color before transmitting glass")
	evidenceFor(t, FeatureTransmission, BackendWebGL).
		needs(webglRendererPath, webgl, "GLSL_TRANSMISSION", "refract(-V, N, 1.0 / u_volume.y)", "textureLod(u_transmissionScene", "transmissionResources.capture(renderTarget)").
		assertAgrees("WebGL2 captures opaque HDR color before transmitting glass")
	if Supports(BackendCanvas2D, FeatureTransmission) || DefaultPolicy().Required[FeatureTransmission] {
		t.Fatal("transmission must allow a declared Canvas2D degradation")
	}
}
