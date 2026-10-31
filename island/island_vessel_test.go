package island

import (
	"strings"
	"testing"
)

func TestScene3DVesselURLIsPropGated(t *testing.T) {
	for _, tc := range []struct {
		name  string
		props map[string]any
		want  bool
	}{
		{"plain", map[string]any{}, false}, {"null", map[string]any{"vessel": nil}, false},
		{"unnamed", map[string]any{"vessel": map[string]any{}}, false},
		{"named", map[string]any{"vessel": map[string]any{"nodeId": "clipper"}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			markup := scene3DChunkGateRenderer(t, tc.props)
			for _, kind := range []string{"vessel", "ocean-query"} {
				attr := `data-gosx-scene3d-` + kind + `-url="/gosx/assets/runtime/bootstrap-feature-scene3d-` + kind + `.js"`
				if strings.Contains(markup, attr) != tc.want {
					t.Fatalf("%s URL gate: %s", kind, markup)
				}
				if strings.Contains(markup, `src="/gosx/assets/runtime/bootstrap-feature-scene3d-`+kind+`.js"`) {
					t.Fatal("sailing chunk is eager")
				}
			}
		})
	}
}

func TestScene3DOceanFFTURLIsSpectrumGated(t *testing.T) {
	ocean := func(o map[string]any) map[string]any {
		return map[string]any{"environment": map[string]any{"ocean": o}}
	}
	for _, tc := range []struct {
		name  string
		props map[string]any
		want  bool
	}{
		{"plain", map[string]any{}, false},
		{"ocean without spectrum", ocean(map[string]any{"level": 0}), false},
		{"null spectrum", ocean(map[string]any{"spectrum": nil}), false},
		{"empty spectrum is all defaults", ocean(map[string]any{"spectrum": map[string]any{}}), true},
		{"spectrum", ocean(map[string]any{"spectrum": map[string]any{"windSpeed": 9}}), true},
		{"spectrum in scene IR", map[string]any{"scene": ocean(map[string]any{"spectrum": map[string]any{"seed": 0}})}, true},
		{"vessel alone", map[string]any{"vessel": map[string]any{"nodeId": "clipper"}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			markup := scene3DChunkGateRenderer(t, tc.props)
			attr := `data-gosx-scene3d-ocean-fft-url="/gosx/assets/runtime/bootstrap-feature-scene3d-ocean-fft.js"`
			if strings.Contains(markup, attr) != tc.want {
				t.Fatalf("ocean-fft URL gate (want %v): %s", tc.want, markup)
			}
			if strings.Contains(markup, `src="/gosx/assets/runtime/bootstrap-feature-scene3d-ocean-fft.js"`) {
				t.Fatal("spectral ocean chunk is eager")
			}
		})
	}
}
