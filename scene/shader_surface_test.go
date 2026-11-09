package scene

import (
	"math"
	"testing"
)

func TestShaderSurfaceRejectsInvalidShapeAndBudgets(t *testing.T) {
	for _, surface := range []ShaderSurface{{Shape: "unknown"}, {MaxPixels: -1}, {MaxFrameRate: math.NaN()}, {MaxDevicePixelRatio: math.Inf(1)}} {
		if _, err := surface.EngineConfig(); err == nil {
			t.Fatalf("accepted invalid surface %+v", surface)
		}
	}
}
