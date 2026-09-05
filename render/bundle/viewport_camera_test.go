package bundle

import (
	"math"
	"testing"

	"m31labs.dev/gosx/engine"
)

func TestViewportOrthographicBounds(t *testing.T) {
	for _, tc := range []struct {
		name string
		cam  engine.RenderCamera
		want [4]float32
	}{
		{"default", engine.RenderCamera{}, [4]float32{-6, 6, -3, 3}},
		{"partial-degenerate", engine.RenderCamera{Left: 1, Right: 1, Bottom: 4, Top: 8}, [4]float32{-6, 6, -3, 3}},
		{"midpoint-zoom", engine.RenderCamera{Left: 0, Right: 4, Bottom: -1, Top: 3, Zoom: 2}, [4]float32{1, 3, 0, 2}},
		{"reversed", engine.RenderCamera{Left: 4, Right: 0, Bottom: 3, Top: -1, Zoom: 2}, [4]float32{1, 3, 0, 2}},
		{"tiny-positive-zoom", engine.RenderCamera{Left: -1, Right: 1, Bottom: -1, Top: 1, Zoom: 1e-8}, [4]float32{-10000, 10000, -10000, 10000}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l, r, b, top := orthographicCameraBounds(tc.cam, 8, 4)
			if got := [4]float32{l, r, b, top}; got != tc.want {
				t.Fatalf("bounds=%v want=%v", got, tc.want)
			}
		})
	}
}

func TestViewportOrthographicProjectionDefaultsAndDepth(t *testing.T) {
	cam := engine.RenderCamera{Mode: "orthographic", Z: 5}
	m := computeMVP(cam, 8, 4)
	for _, p := range []struct {
		world [3]float32
		want  [3]float32
	}{
		{[3]float32{-6, -3, 4.9}, [3]float32{-1, -1, -1}},
		{[3]float32{6, 3, -95}, [3]float32{1, 1, 1}},
	} {
		got := transformPoint(m, p.world)
		for i := range got {
			if math.Abs(float64(got[i]-p.want[i])) > 1e-5 {
				t.Fatalf("projection=%v want=%v", got, p.want)
			}
		}
	}
}
