package docs

import (
	"reflect"
	"testing"

	"m31labs.dev/gosx/scene"
)

func TestDemoShowreelStaysWithinItsRenderBudget(t *testing.T) {
	props := DemoShowreelProgram()
	if got := len(props.Graph.Nodes); got != 10 {
		t.Fatalf("showreel node count = %d, want 10", got)
	}
	if len(props.Graph.Nodes) > showreelNodeBudget {
		t.Fatalf("showreel node count = %d, budget = %d", len(props.Graph.Nodes), showreelNodeBudget)
	}
	if props.MaxPixels != showreelMaxPixels {
		t.Errorf("showreel MaxPixels = %d, want %d", props.MaxPixels, showreelMaxPixels)
	}
	if props.PostFX.MaxPixels != scene.PostFXMaxPixels540p {
		t.Errorf("showreel postfx MaxPixels = %d, want %d", props.PostFX.MaxPixels, scene.PostFXMaxPixels540p)
	}
	if props.Shadows.MaxPixels != scene.ShadowMaxPixels512 {
		t.Errorf("showreel shadow MaxPixels = %d, want %d", props.Shadows.MaxPixels, scene.ShadowMaxPixels512)
	}
	if props.MaxFPS != 30 || props.MaxDevicePixelRatio != 1.5 {
		t.Errorf("showreel frame budget = %.0f fps at %.1f DPR, want 30 fps at 1.5 DPR", props.MaxFPS, props.MaxDevicePixelRatio)
	}
	if props.FillHeight == nil || !*props.FillHeight {
		t.Error("showreel must fill the bounded CSS stage at desktop and mobile sizes")
	}
	wantPostFX := []reflect.Type{
		reflect.TypeOf(scene.Bloom{}),
		reflect.TypeOf(scene.Tonemap{}),
		reflect.TypeOf(scene.Vignette{}),
	}
	gotPostFX := make([]reflect.Type, 0, len(props.PostFX.Effects))
	for _, effect := range props.PostFX.Effects {
		gotPostFX = append(gotPostFX, reflect.TypeOf(effect))
	}
	if !reflect.DeepEqual(gotPostFX, wantPostFX) {
		t.Errorf("showreel postfx order = %v, want Bloom → Tonemap → Vignette", gotPostFX)
	}
}

func TestDemoShowreelHasStableNodeIDs(t *testing.T) {
	want := []string{
		"showreel-key",
		"showreel-accent",
		"showreel-hemi",
		"showreel-plinth",
		"showreel-core",
		"showreel-orbit-a",
		"showreel-orbit-b",
		"showreel-satellite-box",
		"showreel-satellite-pyramid",
		"showreel-satellite-sphere",
	}
	got := make([]string, 0, len(DemoShowreelProgram().Graph.Nodes))
	for _, node := range DemoShowreelProgram().Graph.Nodes {
		got = append(got, showreelNodeID(node))
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("showreel node IDs = %#v, want %#v", got, want)
	}
}

func TestDemoShowreelIsDeterministicAssetFreeAndMotionSafe(t *testing.T) {
	props := DemoShowreelProgram()
	if props.AutoRotate == nil || *props.AutoRotate {
		t.Error("showreel must explicitly disable autonomous camera rotation")
	}
	if props.AriaLabel == "" || props.UnsupportedMessage == "" {
		t.Error("showreel must expose an accessible label and unsupported fallback")
	}
	for _, node := range props.Graph.Nodes {
		mesh, ok := node.(scene.Mesh)
		if !ok {
			continue
		}
		if mesh.Spin != (scene.Euler{}) || mesh.Drift != (scene.Vector3{}) {
			t.Errorf("mesh %q has autonomous motion", mesh.ID)
		}
		material, ok := mesh.Material.(scene.StandardMaterial)
		if !ok {
			t.Errorf("mesh %q uses non-standard material %T", mesh.ID, mesh.Material)
			continue
		}
		if material.Texture != "" || material.NormalMap != "" || material.RoughnessMap != "" || material.MetalnessMap != "" || material.EmissiveMap != "" {
			t.Errorf("mesh %q references an external texture", mesh.ID)
		}
	}
}

func showreelNodeID(node scene.Node) string {
	switch node := node.(type) {
	case scene.DirectionalLight:
		return node.ID
	case scene.PointLight:
		return node.ID
	case scene.HemisphereLight:
		return node.ID
	case scene.Mesh:
		return node.ID
	default:
		return ""
	}
}
