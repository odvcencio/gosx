package bundle

import "m31labs.dev/gosx/engine"

// canvasBoardBenchTransforms builds n synthetic 2D node positions on a grid.
// Returned slice has length n; each entry is (x, y) in world units centered
// around the origin. Used by the bench file to feed the 2D MVP path.
func canvasBoardBenchTransforms(n int) [][2]float64 {
	if n <= 0 {
		return nil
	}
	out := make([][2]float64, n)
	cols := 32
	for i := 0; i < n; i++ {
		col := i % cols
		row := i / cols
		out[i] = [2]float64{
			float64(col-cols/2) * 32,
			float64(row-n/(2*cols)) * 32,
		}
	}
	return out
}

// canvasBoardBenchBundle returns a synthesized 2D-mode RenderBundle that is
// ALREADY populated with the kind of fields Configure2DBundle is supposed to
// strip. Used by BenchmarkCanvasboardConfigure2dBundle to measure the cost
// of the gate on the bundle hot path.
func canvasBoardBenchBundle(n int) engine.RenderBundle {
	b := engine.RenderBundle{
		Camera: OrthoCamera2D(1.0, 0, 0, 1280, 720),
		Lights: []engine.RenderLight{
			{Kind: "directional", Intensity: 1, CastShadow: true},
		},
		Environment: engine.RenderEnvironment{
			AmbientIntensity: 0.5,
			Exposure:         1.0,
		},
		PostEffects: []engine.RenderPostEffect{
			{Kind: "bloom", Intensity: 0.3},
		},
		PostFXMaxPixels: 921600,
	}
	_ = canvasBoardBenchTransforms(n) // touch the helper so the bench grows with n
	return b
}
