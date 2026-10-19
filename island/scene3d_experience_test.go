package island

import (
	"encoding/json"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/engine"
)

func TestScene3DExperienceAdvertisesOnlyRequestedAuthorities(t *testing.T) {
	for _, tc := range []struct {
		name         string
		props        map[string]any
		experience   bool
		zoom, vessel bool
	}{
		{"plain", map[string]any{}, false, false, false},
		{"null", map[string]any{"dayCycle": nil}, false, false, false},
		{"cycle", map[string]any{"dayCycle": map[string]any{"durationSeconds": 1080}}, true, false, false},
		{"optics", map[string]any{"optics": map[string]any{"magnification": 3}}, true, true, false},
		{"descriptor", map[string]any{"experienceRef": "/api/world"}, true, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := NewRenderer("main")
			raw, err := json.Marshal(tc.props)
			if err != nil {
				t.Fatal(err)
			}
			r.RenderEngine(engine.Config{Name: "GoSXScene3D", Kind: engine.KindSurface, Props: raw}, gosx.Text(""))
			markup := gosx.RenderHTML(r.BootstrapScript())
			for kind, want := range map[string]bool{"experience": tc.experience, "zoom": tc.zoom, "vessel": tc.vessel, "ocean-query": tc.vessel} {
				if strings.Contains(markup, "data-gosx-scene3d-"+kind+"-url=") != want {
					t.Fatalf("%s advertisement differs from configured authority", kind)
				}
			}
			if strings.Contains(markup, `src="/gosx/bootstrap-feature-scene3d-experience.js"`) {
				t.Fatal("experience chunk must remain lazy")
			}
		})
	}
}
