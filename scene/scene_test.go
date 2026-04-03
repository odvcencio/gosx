package scene

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/odvcencio/gosx/engine"
)

func TestPropsLegacyPropsLowerNestedGraph(t *testing.T) {
	props := Props{
		Width:      920,
		Height:     560,
		Background: "#08151f",
		Camera: PerspectiveCamera{
			Position: Vec3(0, 1.5, 6),
			FOV:      60,
			Near:     0.15,
			Far:      64,
		},
		Graph: NewGraph(
			Group{
				ID:       "cluster",
				Position: Vec3(1, 0, 0),
				Rotation: Rotate(0, 0, math.Pi/2),
				Children: []Node{
					Mesh{
						ID:       "hero",
						Geometry: BoxGeometry{Width: 2, Height: 1.2, Depth: 0.8},
						Material: FlatMaterial{Color: "#8de1ff"},
						Position: Vec3(2, 0, 0),
					},
					Label{
						ID:         "hero-label",
						Target:     "hero",
						Text:       "Docking-ready",
						Position:   Vec3(0, 1, 0),
						Priority:   4,
						Shift:      Vec3(0.12, 0.18, 0),
						DriftSpeed: 0.8,
						DriftPhase: 0.4,
						Occlude:    true,
					},
				},
			},
		),
	}

	legacy := props.LegacyProps()
	if got := legacy["width"]; got != 920 {
		t.Fatalf("expected width 920, got %#v", got)
	}
	if got := legacy["height"]; got != 560 {
		t.Fatalf("expected height 560, got %#v", got)
	}
	if got := legacy["background"]; got != "#08151f" {
		t.Fatalf("expected background, got %#v", got)
	}

	camera, ok := legacy["camera"].(map[string]any)
	if !ok {
		t.Fatalf("expected camera map, got %#v", legacy["camera"])
	}
	if got := camera["y"]; got != 1.5 {
		t.Fatalf("expected camera y 1.5, got %#v", got)
	}
	if got := camera["z"]; got != 6.0 {
		t.Fatalf("expected camera z 6, got %#v", got)
	}
	if got := camera["fov"]; got != 60.0 {
		t.Fatalf("expected camera fov 60, got %#v", got)
	}
	if got := camera["near"]; got != 0.15 {
		t.Fatalf("expected camera near 0.15, got %#v", got)
	}
	if got := camera["far"]; got != 64.0 {
		t.Fatalf("expected camera far 64, got %#v", got)
	}

	sceneValue, ok := legacy["scene"].(map[string]any)
	if !ok {
		t.Fatalf("expected scene map, got %#v", legacy["scene"])
	}
	objects, ok := sceneValue["objects"].([]map[string]any)
	if !ok || len(objects) != 1 {
		t.Fatalf("expected one lowered object, got %#v", sceneValue["objects"])
	}
	object := objects[0]
	if got := object["id"]; got != "hero" {
		t.Fatalf("expected hero id, got %#v", got)
	}
	if got := object["kind"]; got != "box" {
		t.Fatalf("expected box geometry, got %#v", got)
	}
	if got := object["color"]; got != "#8de1ff" {
		t.Fatalf("expected mesh color, got %#v", got)
	}
	if !approxMapFloat(object["x"], 1) || !approxMapFloat(object["y"], 2) {
		t.Fatalf("expected rotated world position (1,2), got (%#v,%#v)", object["x"], object["y"])
	}
	if !approxMapFloat(object["rotationZ"], math.Pi/2) {
		t.Fatalf("expected world rotation z pi/2, got %#v", object["rotationZ"])
	}

	labels, ok := sceneValue["labels"].([]map[string]any)
	if !ok || len(labels) != 1 {
		t.Fatalf("expected one lowered label, got %#v", sceneValue["labels"])
	}
	label := labels[0]
	if got := label["text"]; got != "Docking-ready" {
		t.Fatalf("expected label text, got %#v", got)
	}
	if !approxMapFloat(label["x"], 0) || !approxMapFloat(label["y"], 2) {
		t.Fatalf("expected target-relative label position (0,2), got (%#v,%#v)", label["x"], label["y"])
	}
	if got := label["occlude"]; got != true {
		t.Fatalf("expected occlude true, got %#v", got)
	}
	if !approxMapFloat(label["shiftX"], 0.12) || !approxMapFloat(label["shiftY"], 0.18) {
		t.Fatalf("expected label drift shift, got (%#v,%#v)", label["shiftX"], label["shiftY"])
	}
	if !approxMapFloat(label["driftSpeed"], 0.8) || !approxMapFloat(label["driftPhase"], 0.4) {
		t.Fatalf("expected label drift motion, got (%#v,%#v)", label["driftSpeed"], label["driftPhase"])
	}
}

func TestPropsSceneIRLowerNestedGraph(t *testing.T) {
	props := Props{
		Graph: NewGraph(
			Group{
				ID:       "cluster",
				Position: Vec3(1, 0, 0),
				Rotation: Rotate(0, 0, math.Pi/2),
				Children: []Node{
					Mesh{
						ID:       "hero",
						Geometry: BoxGeometry{Width: 2, Height: 1.2, Depth: 0.8},
						Material: FlatMaterial{Color: "#8de1ff"},
						Position: Vec3(2, 0, 0),
					},
					Label{
						ID:         "hero-label",
						Target:     "hero",
						Text:       "Docking-ready",
						Position:   Vec3(0, 1, 0),
						Priority:   4,
						Shift:      Vec3(0.12, 0.18, 0),
						DriftSpeed: 0.8,
						DriftPhase: 0.4,
						Occlude:    true,
					},
				},
			},
		),
	}

	ir := props.SceneIR()
	if len(ir.Objects) != 1 {
		t.Fatalf("expected one lowered object, got %#v", ir.Objects)
	}
	object := ir.Objects[0]
	if object.ID != "hero" {
		t.Fatalf("expected hero id, got %#v", object.ID)
	}
	if object.Kind != "box" {
		t.Fatalf("expected box geometry, got %#v", object.Kind)
	}
	if object.Color != "#8de1ff" {
		t.Fatalf("expected mesh color, got %#v", object.Color)
	}
	if math.Abs(object.X-1) > 1e-9 || math.Abs(object.Y-2) > 1e-9 {
		t.Fatalf("expected rotated world position (1,2), got (%#v,%#v)", object.X, object.Y)
	}
	if math.Abs(object.RotationZ-math.Pi/2) > 1e-9 {
		t.Fatalf("expected world rotation z pi/2, got %#v", object.RotationZ)
	}

	if len(ir.Labels) != 1 {
		t.Fatalf("expected one lowered label, got %#v", ir.Labels)
	}
	label := ir.Labels[0]
	if label.ID != "hero-label" {
		t.Fatalf("expected label id, got %#v", label.ID)
	}
	if label.Text != "Docking-ready" {
		t.Fatalf("expected label text, got %#v", label.Text)
	}
	if math.Abs(label.X-0) > 1e-9 || math.Abs(label.Y-2) > 1e-9 {
		t.Fatalf("expected target-relative label position (0,2), got (%#v,%#v)", label.X, label.Y)
	}
	if !label.Occlude {
		t.Fatalf("expected occlude true, got %#v", label.Occlude)
	}
	if math.Abs(label.ShiftX-0.12) > 1e-9 || math.Abs(label.ShiftY-0.18) > 1e-9 {
		t.Fatalf("expected label shift motion, got (%#v,%#v)", label.ShiftX, label.ShiftY)
	}
	if math.Abs(label.DriftSpeed-0.8) > 1e-9 || math.Abs(label.DriftPhase-0.4) > 1e-9 {
		t.Fatalf("expected label drift motion, got (%#v,%#v)", label.DriftSpeed, label.DriftPhase)
	}
}

func TestPropsSceneIRLowersRichMaterialFields(t *testing.T) {
	props := Props{
		Graph: NewGraph(
			Mesh{
				ID:       "glass-panel",
				Geometry: PlaneGeometry{Width: 2.4, Height: 1.6},
				Material: GlassMaterial{
					Color:      "#c7f0ff",
					Opacity:    Float(0),
					Emissive:   Float(0),
					BlendMode:  BlendAlpha,
					RenderPass: RenderAlpha,
					Wireframe:  Bool(false),
				},
				Position: Vec3(0, 0.4, 0.8),
			},
		),
	}

	ir := props.SceneIR()
	if len(ir.Objects) != 1 {
		t.Fatalf("expected one lowered object, got %#v", ir.Objects)
	}
	object := ir.Objects[0]
	if object.MaterialKind != "glass" {
		t.Fatalf("expected glass material kind, got %#v", object.MaterialKind)
	}
	if object.Opacity == nil || *object.Opacity != 0 {
		t.Fatalf("expected explicit zero opacity, got %#v", object.Opacity)
	}
	if object.Emissive == nil || *object.Emissive != 0 {
		t.Fatalf("expected explicit zero emissive, got %#v", object.Emissive)
	}
	if object.BlendMode != "alpha" {
		t.Fatalf("expected alpha blend mode, got %#v", object.BlendMode)
	}
	if object.RenderPass != "alpha" {
		t.Fatalf("expected alpha render pass, got %#v", object.RenderPass)
	}
	if object.Wireframe == nil || *object.Wireframe {
		t.Fatalf("expected explicit wireframe=false, got %#v", object.Wireframe)
	}

	legacy := props.LegacyProps()
	sceneValue, ok := legacy["scene"].(map[string]any)
	if !ok {
		t.Fatalf("expected scene map, got %#v", legacy["scene"])
	}
	objects, ok := sceneValue["objects"].([]map[string]any)
	if !ok || len(objects) != 1 {
		t.Fatalf("expected one lowered object, got %#v", sceneValue["objects"])
	}
	record := objects[0]
	if got := record["materialKind"]; got != "glass" {
		t.Fatalf("expected glass material kind in legacy props, got %#v", got)
	}
	if got := record["opacity"]; got != 0.0 {
		t.Fatalf("expected explicit zero opacity in legacy props, got %#v", got)
	}
	if got := record["emissive"]; got != 0.0 {
		t.Fatalf("expected explicit zero emissive in legacy props, got %#v", got)
	}
	if got := record["blendMode"]; got != "alpha" {
		t.Fatalf("expected alpha blend mode in legacy props, got %#v", got)
	}
	if got := record["renderPass"]; got != "alpha" {
		t.Fatalf("expected alpha render pass in legacy props, got %#v", got)
	}
	if got := record["wireframe"]; got != false {
		t.Fatalf("expected explicit wireframe=false in legacy props, got %#v", got)
	}
}

func TestPropsMarshalJSONOmitsEngineTransportFields(t *testing.T) {
	props := Props{
		ProgramRef:   "/api/runtime/scene-program",
		Capabilities: []string{"pointer", "keyboard"},
		Background:   "#08151f",
		Graph: NewGraph(
			Mesh{
				ID:       "hero",
				Geometry: CubeGeometry{Size: 1.6},
			},
		),
	}

	data, err := json.Marshal(props)
	if err != nil {
		t.Fatalf("marshal props: %v", err)
	}
	text := string(data)
	if contains(text, "programRef") {
		t.Fatalf("did not expect programRef in props json: %s", text)
	}
	if contains(text, "capabilities") {
		t.Fatalf("did not expect capabilities in props json: %s", text)
	}
	if !contains(text, `"background":"#08151f"`) {
		t.Fatalf("expected background in props json: %s", text)
	}
	if !contains(text, `"kind":"cube"`) {
		t.Fatalf("expected lowered scene object in props json: %s", text)
	}
}

func TestPropsEngineConfigUsesBuiltInSceneContract(t *testing.T) {
	props := Props{
		ProgramRef:   "/api/runtime/scene-program",
		Capabilities: []string{"pointer", "keyboard", "canvas"},
		Width:        720,
		Height:       420,
		Background:   "#08151f",
	}

	cfg := props.EngineConfig()
	if cfg.Name != DefaultEngineName {
		t.Fatalf("expected default scene engine name, got %q", cfg.Name)
	}
	if cfg.Kind != engine.KindSurface {
		t.Fatalf("expected scene engine surface kind, got %q", cfg.Kind)
	}
	if cfg.Runtime != engine.RuntimeShared {
		t.Fatalf("expected shared runtime when programRef is present, got %q", cfg.Runtime)
	}
	if cfg.WASMPath != "/api/runtime/scene-program" {
		t.Fatalf("expected programRef to become engine program path, got %q", cfg.WASMPath)
	}
	if got := cfg.MountAttrs["data-gosx-scene3d"]; got != true {
		t.Fatalf("expected Scene3D mount attr, got %#v", got)
	}

	wantCaps := []engine.Capability{
		engine.CapCanvas,
		engine.CapWebGL,
		engine.CapAnimation,
		engine.CapPointer,
		engine.CapKeyboard,
	}
	if len(cfg.Capabilities) != len(wantCaps) {
		t.Fatalf("expected %d capabilities, got %#v", len(wantCaps), cfg.Capabilities)
	}
	for i, capability := range wantCaps {
		if cfg.Capabilities[i] != capability {
			t.Fatalf("unexpected capability %d: got %q want %q", i, cfg.Capabilities[i], capability)
		}
	}
	if !contains(string(cfg.Props), `"width":720`) || !contains(string(cfg.Props), `"height":420`) {
		t.Fatalf("expected scene props in engine config, got %s", string(cfg.Props))
	}
	if contains(string(cfg.Props), "programRef") {
		t.Fatalf("did not expect programRef in engine props, got %s", string(cfg.Props))
	}
}

func approxMapFloat(value any, want float64) bool {
	got, ok := value.(float64)
	if !ok {
		return false
	}
	return math.Abs(got-want) < 1e-9
}

func contains(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}
