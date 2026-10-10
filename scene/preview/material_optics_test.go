package preview

import (
	"m31labs.dev/gosx/scene"
	"strings"
	"testing"
)

func TestNewBrowserMaterialOpticsHaveHonestNativeDiagnostic(t *testing.T) {
	ir := scene.NewGraph(scene.Mesh{ID: "volume", Geometry: scene.BoxGeometry{}, Material: scene.StandardMaterial{ThicknessMap: "/thickness.png", SpecularAA: &scene.GeometricSpecularAA{Variance: .15, Threshold: .2}}}).SceneIR()
	d, ok := materialCoverageDiagnostic(ir)
	if !ok || !strings.Contains(d.Message, "thicknessMap(1)") || !strings.Contains(d.Message, "specularAA(1)") {
		t.Fatalf("missing native limitation: %+v", d)
	}
	d, ok = materialCoverageDiagnostic(scene.NewGraph(scene.Mesh{Geometry: scene.BoxGeometry{}, Material: scene.StandardMaterial{}}).SceneIR())
	if ok {
		t.Fatalf("unrequested feature changed diagnostics: %+v", d)
	}
}
