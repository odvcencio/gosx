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
