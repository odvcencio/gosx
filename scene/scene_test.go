package scene

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"m31labs.dev/gosx/engine"
)

func TestSceneIRSchemaIsOptionalButPreserved(t *testing.T) {
	payload, err := json.Marshal(SceneIR{
		Schema:  SceneIRSchema,
		Objects: []ObjectIR{{ID: "box", Kind: "box"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), `"schema":"gosx.scene3d.ir.v1"`) {
		t.Fatalf("schema was not preserved in SceneIR JSON: %s", payload)
	}

	legacy := SceneIR{Schema: SceneIRSchema}.legacyProps()
	if legacy["schema"] != SceneIRSchema {
		t.Fatalf("legacy schema = %#v", legacy["schema"])
	}
}

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

func TestPropsSceneIRLowersLiveTransitions(t *testing.T) {
	props := Props{
		Environment: Environment{
			FogColor:   "#050008",
			FogDensity: 0.00042,
			Live:       Live("mood:atmosphere", " mood:atmosphere ", "mood:fog"),
			Transition: Transition{
				In: TransitionTiming{Duration: 4 * time.Second, Easing: EaseInOut},
			},
			InState: &EnvironmentProps{
				FogDensity: Float(0),
			},
			OutState: &EnvironmentProps{
				FogDensity: Float(0),
			},
		},
		Graph: NewGraph(
			Points{
				ID:      "stars",
				Count:   1760,
				Color:   "#f8f7ff",
				Opacity: 0.83,
				Live:    Live("mood:atmosphere"),
				Transition: Transition{
					In:     TransitionTiming{Duration: 2 * time.Second, Easing: EaseOut},
					Update: TransitionTiming{Duration: 300 * time.Millisecond, Easing: Linear},
				},
				InState: &PointsProps{
					Opacity: Float(0),
				},
				OutState: &PointsProps{
					Opacity: Float(0),
				},
			},
			ComputeParticles{
				ID:    "galaxy-dust",
				Count: 182,
				Live:  Live("mood:atmosphere", "mood:dust"),
				Emitter: ParticleEmitter{
					Kind:   "spiral",
					Radius: 230,
				},
				Material: ParticleMaterial{
					Color:   "#7c83fd",
					Opacity: 0.24,
				},
				Transition: Transition{
					In:  TransitionTiming{Duration: 3 * time.Second, Easing: EaseOut},
					Out: TransitionTiming{Duration: 1 * time.Second, Easing: EaseIn},
				},
				InState: &ComputeParticlesProps{
					Count:    Int(0),
					Material: &ParticleMaterial{Opacity: 0},
				},
				OutState: &ComputeParticlesProps{
					Count:    Int(0),
					Material: &ParticleMaterial{Opacity: 0},
				},
			},
		),
	}

	ir := props.SceneIR()
	if len(ir.Points) != 1 {
		t.Fatalf("expected one points record, got %#v", ir.Points)
	}
	if len(ir.ComputeParticles) != 1 {
		t.Fatalf("expected one compute particle record, got %#v", ir.ComputeParticles)
	}

	points := ir.Points[0]
	if points.Transition.In.Duration != 2000 {
		t.Fatalf("expected 2000ms points enter duration, got %#v", points.Transition.In.Duration)
	}
	if points.Transition.Update.Duration != 300 {
		t.Fatalf("expected 300ms points update duration, got %#v", points.Transition.Update.Duration)
	}
	if points.Transition.In.Easing != "ease-out" {
		t.Fatalf("expected ease-out points transition, got %#v", points.Transition.In.Easing)
	}
	if len(points.Live) != 1 || points.Live[0] != "mood:atmosphere" {
		t.Fatalf("expected normalized points live events, got %#v", points.Live)
	}
	if got := mapFloat64(points.InState["opacity"]); got != 0 {
		t.Fatalf("expected explicit zero opacity inState, got %#v", points.InState)
	}

	particles := ir.ComputeParticles[0]
	if particles.Transition.Out.Duration != 1000 {
		t.Fatalf("expected 1000ms particles exit duration, got %#v", particles.Transition.Out.Duration)
	}
	if len(particles.Live) != 2 || particles.Live[0] != "mood:atmosphere" || particles.Live[1] != "mood:dust" {
		t.Fatalf("expected normalized particle live events, got %#v", particles.Live)
	}
	if got := mapInt(particles.InState["count"]); got != 0 {
		t.Fatalf("expected explicit zero particle count inState, got %#v", particles.InState)
	}
	material, ok := particles.InState["material"].(map[string]any)
	if !ok || mapFloat64(material["opacity"]) != 0 {
		t.Fatalf("expected particle material opacity override, got %#v", particles.InState["material"])
	}

	if ir.Environment.Transition.In.Duration != 4000 {
		t.Fatalf("expected 4000ms environment enter duration, got %#v", ir.Environment.Transition.In.Duration)
	}
	if len(ir.Environment.Live) != 2 || ir.Environment.Live[0] != "mood:atmosphere" || ir.Environment.Live[1] != "mood:fog" {
		t.Fatalf("expected normalized environment live events, got %#v", ir.Environment.Live)
	}
}

func TestPropsLegacyPropsSerializesLiveTransitions(t *testing.T) {
	props := Props{
		Environment: Environment{
			FogColor: "#050008",
			Live:     Live("mood:atmosphere"),
			Transition: Transition{
				In: TransitionTiming{Duration: 4 * time.Second, Easing: EaseInOut},
			},
		},
		Graph: NewGraph(
			Points{
				ID:      "stars",
				Count:   1760,
				Color:   "#f8f7ff",
				Opacity: 0.83,
				Live:    Live("mood:atmosphere"),
				Transition: Transition{
					In:     TransitionTiming{Duration: 2 * time.Second, Easing: EaseOut},
					Update: TransitionTiming{Duration: 300 * time.Millisecond, Easing: Linear},
				},
				InState: &PointsProps{
					Opacity: Float(0),
				},
			},
		),
	}

	legacy := props.LegacyProps()
	sceneValue, ok := legacy["scene"].(map[string]any)
	if !ok {
		t.Fatalf("expected scene map, got %#v", legacy["scene"])
	}
	points, ok := sceneValue["points"].([]map[string]any)
	if !ok || len(points) != 1 {
		t.Fatalf("expected one points record, got %#v", sceneValue["points"])
	}
	record := points[0]
	transition, ok := record["transition"].(map[string]any)
	if !ok {
		t.Fatalf("expected transition map, got %#v", record["transition"])
	}
	in, ok := transition["in"].(map[string]any)
	if !ok || in["duration"] != int64(2000) || in["easing"] != "ease-out" {
		t.Fatalf("expected points in transition, got %#v", transition["in"])
	}
	update, ok := transition["update"].(map[string]any)
	if !ok || update["duration"] != int64(300) || update["easing"] != "linear" {
		t.Fatalf("expected points update transition, got %#v", transition["update"])
	}
	inState, ok := record["inState"].(map[string]any)
	if !ok || inState["opacity"] != 0.0 {
		t.Fatalf("expected points inState opacity 0, got %#v", record["inState"])
	}
	live, ok := record["live"].([]string)
	if !ok || len(live) != 1 || live[0] != "mood:atmosphere" {
		t.Fatalf("expected points live events, got %#v", record["live"])
	}

	environment, ok := sceneValue["environment"].(map[string]any)
	if !ok {
		t.Fatalf("expected environment map, got %#v", sceneValue["environment"])
	}
	if _, ok := environment["transition"].(map[string]any); !ok {
		t.Fatalf("expected environment transition map, got %#v", environment["transition"])
	}
	envLive, ok := environment["live"].([]string)
	if !ok || len(envLive) != 1 || envLive[0] != "mood:atmosphere" {
		t.Fatalf("expected environment live events, got %#v", environment["live"])
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

func TestPropsSceneIRLowersTexturedPlaneMaterials(t *testing.T) {
	props := Props{
		Graph: NewGraph(
			Mesh{
				ID:       "info-card",
				Geometry: PlaneGeometry{Width: 1.55, Height: 1.02},
				Material: FlatMaterial{
					Color:     "#f7fbff",
					Texture:   "/paper-card.png",
					Wireframe: Bool(false),
				},
				Position: Vec3(0.3, 0.55, 0.2),
			},
		),
	}

	ir := props.SceneIR()
	if len(ir.Objects) != 1 {
		t.Fatalf("expected one lowered object, got %#v", ir.Objects)
	}
	object := ir.Objects[0]
	if object.MaterialKind != "flat" {
		t.Fatalf("expected flat material kind, got %#v", object.MaterialKind)
	}
	if object.Texture != "/paper-card.png" {
		t.Fatalf("expected lowered texture, got %#v", object.Texture)
	}
	if object.Wireframe == nil || *object.Wireframe {
		t.Fatalf("expected explicit wireframe=false for textured plane, got %#v", object.Wireframe)
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
	if got := record["texture"]; got != "/paper-card.png" {
		t.Fatalf("expected texture in legacy props, got %#v", got)
	}
	if got := record["wireframe"]; got != false {
		t.Fatalf("expected explicit wireframe=false in legacy props, got %#v", got)
	}
}

func TestPropsSceneIRLowersTargetedSprites(t *testing.T) {
	props := Props{
		Graph: NewGraph(
			Mesh{
				ID:       "hero",
				Geometry: BoxGeometry{Width: 1.4, Height: 1.1, Depth: 0.9},
				Material: FlatMaterial{Color: "#8de1ff"},
				Position: Vec3(1.2, 0.4, -0.2),
			},
			Sprite{
				Target:     "hero",
				Src:        "/paper-card.png",
				Position:   Vec3(0, 1.05, 0.2),
				Width:      1.55,
				Height:     1.02,
				Scale:      1.1,
				Opacity:    0.94,
				Priority:   3,
				AnchorX:    0.5,
				AnchorY:    0.5,
				Occlude:    true,
				Fit:        "cover",
				Shift:      Vec3(0.08, 0.06, 0),
				DriftSpeed: 0.62,
				DriftPhase: 0.45,
			},
		),
	}

	ir := props.SceneIR()
	if len(ir.Sprites) != 1 {
		t.Fatalf("expected one lowered sprite, got %#v", ir.Sprites)
	}
	sprite := ir.Sprites[0]
	if sprite.Src != "/paper-card.png" {
		t.Fatalf("expected sprite src, got %#v", sprite.Src)
	}
	if math.Abs(sprite.X-1.2) > 1e-9 || math.Abs(sprite.Y-1.45) > 1e-9 {
		t.Fatalf("expected targeted sprite position near hero, got (%#v,%#v)", sprite.X, sprite.Y)
	}
	if sprite.Fit != "cover" || !sprite.Occlude {
		t.Fatalf("expected sprite fit/occlude, got %#v", sprite)
	}

	legacy := props.LegacyProps()
	sceneValue, ok := legacy["scene"].(map[string]any)
	if !ok {
		t.Fatalf("expected scene map, got %#v", legacy["scene"])
	}
	sprites, ok := sceneValue["sprites"].([]map[string]any)
	if !ok || len(sprites) != 1 {
		t.Fatalf("expected one lowered sprite record, got %#v", sceneValue["sprites"])
	}
	record := sprites[0]
	if got := record["src"]; got != "/paper-card.png" {
		t.Fatalf("expected sprite src in legacy props, got %#v", got)
	}
	if got := record["fit"]; got != "cover" {
		t.Fatalf("expected sprite fit in legacy props, got %#v", got)
	}
}

func TestPropsLegacyPropsIncludeEventSignalNamespace(t *testing.T) {
	props := Props{
		Width:                640,
		Height:               360,
		PickSignalNamespace:  "$scene.demo.pick",
		EventSignalNamespace: "$scene.demo.event",
	}

	legacy := props.LegacyProps()
	if got := legacy["pickSignalNamespace"]; got != "$scene.demo.pick" {
		t.Fatalf("expected pick signal namespace, got %#v", got)
	}
	if got := legacy["eventSignalNamespace"]; got != "$scene.demo.event" {
		t.Fatalf("expected event signal namespace, got %#v", got)
	}
}

func TestPropsLegacyPropsIncludeGizmoInputSignal(t *testing.T) {
	props := Props{
		Width:            640,
		Height:           360,
		GizmoInputSignal: "$scene.demo.gizmo",
	}

	legacy := props.LegacyProps()
	if got := legacy["gizmoInputSignal"]; got != "$scene.demo.gizmo" {
		t.Fatalf("expected gizmo input signal, got %#v", got)
	}

	// Fast SSR path (spreadPropsFast / MarshalJSON) must agree with LegacyProps.
	raw := props.RawPropsJSON()
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal RawPropsJSON: %v", err)
	}
	if got := decoded["gizmoInputSignal"]; got != "$scene.demo.gizmo" {
		t.Fatalf("expected gizmoInputSignal in fast-path props, got %#v", got)
	}
}

func TestPropsSceneIRLowersLightsAndEnvironment(t *testing.T) {
	props := Props{
		Environment: Environment{
			AmbientColor:     "#f4fbff",
			AmbientIntensity: 0.22,
			SkyColor:         "#b9deff",
			SkyIntensity:     0.18,
			GroundColor:      "#102030",
			GroundIntensity:  0.06,
			Exposure:         1.1,
		},
		Graph: NewGraph(
			Group{
				Position: Vec3(0, 2, 0),
				Rotation: Rotate(0, 0, math.Pi/2),
				Children: []Node{
					DirectionalLight{
						ID:        "sun",
						Color:     "#fff1d6",
						Intensity: 1.4,
						Direction: Vec3(1, 0, 0),
					},
					PointLight{
						ID:        "beacon",
						Color:     "#8de1ff",
						Intensity: 1.2,
						Position:  Vec3(1, 0, 0),
						Range:     7.5,
						Decay:     1.6,
					},
				},
			},
			AmbientLight{
				ID:        "fill",
				Color:     "#f3fbff",
				Intensity: 0.24,
			},
		),
	}

	ir := props.SceneIR()
	if len(ir.Lights) != 3 {
		t.Fatalf("expected three lowered lights, got %#v", ir.Lights)
	}
	if ir.Environment.AmbientColor != "#f4fbff" || ir.Environment.Exposure != 1.1 {
		t.Fatalf("expected lowered environment, got %#v", ir.Environment)
	}

	if ir.Lights[0].Kind != "directional" || ir.Lights[0].ID != "sun" {
		t.Fatalf("expected directional light first, got %#v", ir.Lights[0])
	}
	if math.Abs(ir.Lights[0].DirectionX) > 1e-9 || math.Abs(ir.Lights[0].DirectionY-1) > 1e-9 {
		t.Fatalf("expected rotated light direction (0,1,0), got %#v", ir.Lights[0])
	}

	if ir.Lights[1].Kind != "point" || ir.Lights[1].ID != "beacon" {
		t.Fatalf("expected point light second, got %#v", ir.Lights[1])
	}
	if math.Abs(ir.Lights[1].X-0) > 1e-9 || math.Abs(ir.Lights[1].Y-3) > 1e-9 {
		t.Fatalf("expected transformed point-light position (0,3,0), got %#v", ir.Lights[1])
	}
	if ir.Lights[1].Range != 7.5 || ir.Lights[1].Decay != 1.6 {
		t.Fatalf("expected point-light falloff fields, got %#v", ir.Lights[1])
	}

	if ir.Lights[2].Kind != "ambient" || ir.Lights[2].ID != "fill" {
		t.Fatalf("expected ambient light third, got %#v", ir.Lights[2])
	}

	legacy := props.LegacyProps()
	sceneValue, ok := legacy["scene"].(map[string]any)
	if !ok {
		t.Fatalf("expected scene map, got %#v", legacy["scene"])
	}
	lights, ok := sceneValue["lights"].([]map[string]any)
	if !ok || len(lights) != 3 {
		t.Fatalf("expected three lowered lights in legacy props, got %#v", sceneValue["lights"])
	}
	environment, ok := sceneValue["environment"].(map[string]any)
	if !ok {
		t.Fatalf("expected environment map, got %#v", sceneValue["environment"])
	}
	if got := environment["ambientIntensity"]; got != 0.22 {
		t.Fatalf("expected ambient intensity in legacy props, got %#v", got)
	}
	if got := environment["exposure"]; got != 1.1 {
		t.Fatalf("expected exposure in legacy props, got %#v", got)
	}
}

func TestPropsSceneIRLowersLODDecalsAndProbeLights(t *testing.T) {
	props := Props{
		PostFX: PostFX{Effects: []PostEffect{
			DOF{FocusDistance: 7, Aperture: 0.05, MaxBlur: 6},
		}},
		Graph: NewGraph(
			LODGroup{
				ID: "ship-lod",
				Levels: []LODLevel{
					{Distance: 12, Node: Mesh{ID: "ship-low", Geometry: BoxGeometry{Width: 1, Height: 1, Depth: 1}}},
					{Distance: 0, Node: Mesh{ID: "ship-high", Geometry: SphereGeometry{Radius: 0.5}}},
				},
			},
			Decal{ID: "badge", Src: "/textures/badge.png", Width: 0.8, Height: 0.35, Opacity: 0.85},
			RectAreaLight{ID: "softbox", Color: "#dbeafe", Intensity: 0.8, Width: 2.5, Height: 1.5, Position: Vec3(0, 2, 2)},
			LightProbe{ID: "probe", Color: "#dbeafe", Intensity: 0.25, Coefficients: []Vector3{{X: 1}}},
		),
	}

	ir := props.SceneIR()
	if len(ir.Objects) != 3 {
		t.Fatalf("expected two LOD objects plus decal, got %#v", ir.Objects)
	}
	if ir.Objects[0].ID != "ship-high" || ir.Objects[0].LODGroup != "ship-lod" || ir.Objects[0].LODMinDistance != 0 || ir.Objects[0].LODMaxDistance != 12 {
		t.Fatalf("expected first LOD level metadata, got %#v", ir.Objects[0])
	}
	if ir.Objects[1].ID != "ship-low" || ir.Objects[1].LODGroup != "ship-lod" || ir.Objects[1].LODLevel != 1 || ir.Objects[1].LODMinDistance != 12 || ir.Objects[1].LODMaxDistance != 0 {
		t.Fatalf("expected second LOD level metadata, got %#v", ir.Objects[1])
	}
	decal := ir.Objects[2]
	if decal.ID != "badge" || decal.Kind != "plane" || decal.Texture != "/textures/badge.png" || decal.DepthWrite == nil || *decal.DepthWrite {
		t.Fatalf("expected alpha plane decal, got %#v", decal)
	}
	if len(ir.Lights) != 2 || ir.Lights[0].Kind != "rect-area" || ir.Lights[0].Width != 2.5 || ir.Lights[1].Kind != "light-probe" {
		t.Fatalf("expected rect area and probe lights, got %#v", ir.Lights)
	}
	if len(ir.PostEffects) != 1 {
		t.Fatalf("expected one post effect, got %#v", ir.PostEffects)
	}
	if dof, ok := ir.PostEffects[0].(DOFIR); !ok || dof.FocusDistance != 7 || math.Abs(dof.Aperture-0.05) > 1e-6 || dof.MaxBlur != 6 {
		t.Fatalf("expected DOF post effect, got %#v", ir.PostEffects[0])
	}
}

func TestPropsLegacyPropsLowerCameraRotationAndControls(t *testing.T) {
	props := Props{
		Controls:               ControlFirstPerson,
		ControlTarget:          Vec3(1.5, 0.25, 0.8),
		ControlRotateMode:      "pixel-degrees",
		ControlRotateDirection: "grab",
		ControlRotateSpeed:     1.4,
		ControlZoomSpeed:       0.85,
		ControlLookSpeed:       1.2,
		ControlMoveSpeed:       6.5,
		ControlMinDistance:     2,
		ControlMaxDistance:     10,
		ControlPitchLimit:      1.5707788735,
		PointerLock:            Bool(true),
		MSAASamples:            4,
		Camera: PerspectiveCamera{
			Position: Vec3(0.2, 0.6, 6),
			Rotation: Rotate(0.18, -0.32, 0.05),
			FOV:      62,
			Near:     0.1,
			Far:      96,
		},
	}

	legacy := props.LegacyProps()
	if got := legacy["controls"]; got != ControlFirstPerson {
		t.Fatalf("expected controls first-person, got %#v", got)
	}
	if got := legacy["pointerLock"]; got != true {
		t.Fatalf("expected pointerLock true, got %#v", got)
	}
	controlTarget, ok := legacy["controlTarget"].(map[string]any)
	if !ok {
		t.Fatalf("expected control target map, got %#v", legacy["controlTarget"])
	}
	if got := controlTarget["x"]; got != 1.5 {
		t.Fatalf("expected control target x 1.5, got %#v", got)
	}
	if got := legacy["controlRotateMode"]; got != "pixel-degrees" {
		t.Fatalf("expected rotate mode pixel-degrees, got %#v", got)
	}
	if got := legacy["controlRotateDirection"]; got != "grab" {
		t.Fatalf("expected rotate direction grab, got %#v", got)
	}
	if got := legacy["controlRotateSpeed"]; got != 1.4 {
		t.Fatalf("expected rotate speed 1.4, got %#v", got)
	}
	if got := legacy["controlZoomSpeed"]; got != 0.85 {
		t.Fatalf("expected zoom speed 0.85, got %#v", got)
	}
	if got := legacy["controlLookSpeed"]; got != 1.2 {
		t.Fatalf("expected look speed 1.2, got %#v", got)
	}
	if got := legacy["controlMoveSpeed"]; got != 6.5 {
		t.Fatalf("expected move speed 6.5, got %#v", got)
	}
	if got := legacy["controlMinDistance"]; got != 2.0 {
		t.Fatalf("expected min distance 2, got %#v", got)
	}
	if got := legacy["controlMaxDistance"]; got != 10.0 {
		t.Fatalf("expected max distance 10, got %#v", got)
	}
	if got := legacy["controlPitchLimit"]; got != 1.5707788735 {
		t.Fatalf("expected pitch limit 1.5707788735, got %#v", got)
	}
	if got := legacy["msaaSamples"]; got != 4 {
		t.Fatalf("expected msaa samples 4, got %#v", got)
	}
	if !slicesContainCapability(props.EngineConfig().Capabilities, engine.CapPointerLock) {
		t.Fatalf("expected pointer lock capability, got %#v", props.EngineConfig().Capabilities)
	}

	camera, ok := legacy["camera"].(map[string]any)
	if !ok {
		t.Fatalf("expected camera map, got %#v", legacy["camera"])
	}
	if got := camera["rotationX"]; got != 0.18 {
		t.Fatalf("expected camera rotationX 0.18, got %#v", got)
	}
	if got := camera["rotationY"]; got != -0.32 {
		t.Fatalf("expected camera rotationY -0.32, got %#v", got)
	}
	if got := camera["rotationZ"]; got != 0.05 {
		t.Fatalf("expected camera rotationZ 0.05, got %#v", got)
	}
}

func TestPropsMarshalJSONOmitsEngineTransportFields(t *testing.T) {
	props := Props{
		ProgramRef:           "/api/runtime/scene-program",
		Capabilities:         []string{"pointer", "keyboard"},
		RequiredCapabilities: RequireWebGPU(engine.CapWebGPUTimestampQuery),
		Background:           "#08151f",
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
	if contains(text, "requiredCapabilities") {
		t.Fatalf("did not expect requiredCapabilities in props json: %s", text)
	}
	if !contains(text, `"background":"#08151f"`) {
		t.Fatalf("expected background in props json: %s", text)
	}
	if !contains(text, `"kind":"cube"`) {
		t.Fatalf("expected lowered scene object in props json: %s", text)
	}
}

func TestPropsMarshalJSONCarriesWebGPUPresentationHints(t *testing.T) {
	props := Props{
		PreferWebGPU:          Bool(true),
		WebGPUAlphaMode:       "opaque",
		WebGPUColorSpace:      "display-p3",
		WebGPUToneMapping:     "extended",
		WebGPUPowerPreference: "high-performance",
	}

	data, err := json.Marshal(props)
	if err != nil {
		t.Fatalf("marshal props: %v", err)
	}
	text := string(data)
	for _, snippet := range []string{
		`"preferWebGPU":true`,
		`"webgpuAlphaMode":"opaque"`,
		`"webgpuColorSpace":"display-p3"`,
		`"webgpuToneMapping":"extended"`,
		`"webgpuPowerPreference":"high-performance"`,
	} {
		if !contains(text, snippet) {
			t.Fatalf("expected %q in props json: %s", snippet, text)
		}
	}
}

func TestPropsLegacyPropsCarriesInspectorFlag(t *testing.T) {
	props := Props{
		Stats:     Bool(true),
		Inspector: Bool(true),
	}

	legacy := props.LegacyProps()
	if got := legacy["stats"]; got != true {
		t.Fatalf("expected stats flag in legacy props, got %#v", got)
	}
	if got := legacy["inspector"]; got != true {
		t.Fatalf("expected inspector flag in legacy props, got %#v", got)
	}

	data, err := json.Marshal(props)
	if err != nil {
		t.Fatalf("marshal props: %v", err)
	}
	if !contains(string(data), `"inspector":true`) {
		t.Fatalf("expected inspector flag in props json: %s", string(data))
	}
}

func TestPropsSceneIRLowersModelInstancesAndLineGeometry(t *testing.T) {
	props := Props{
		PickSignalNamespace: "$scene.pick",
		Graph: NewGraph(
			Model{
				ID:       "runner",
				Src:      "/models/runner.gosx3d.json",
				Position: Vec3(1.2, 0.4, -0.8),
				Rotation: Rotate(0.1, 0.2, -0.3),
				Scale:    Vec3(1.6, 0.8, 1.2),
				Bounds:   0.5,
				Fit:      "contain",
				FitAlign: "center-min-y",
				Material: GlowMaterial{
					Color:      "#ffd48f",
					Opacity:    Float(0.78),
					Emissive:   Float(0.24),
					BlendMode:  BlendAdditive,
					RenderPass: RenderAdditive,
					Wireframe:  Bool(false),
				},
				CastShadow:    true,
				ReceiveShadow: true,
				Pickable:      Bool(true),
				Static:        Bool(true),
			},
			Mesh{
				ID: "wireframe",
				Geometry: LinesGeometry{
					Points: []Vector3{
						Vec3(0, 0, 0),
						Vec3(1, 0, 0),
						Vec3(1, 1, 0),
						Vec3(0, 1, 0),
					},
					Segments: [][2]int{{0, 1}, {1, 2}, {2, 3}, {3, 0}},
				},
				Material: FlatMaterial{Color: "#8de1ff"},
				Position: Vec3(-1, 0.5, 0),
				Pickable: Bool(false),
			},
		),
	}

	ir := props.SceneIR()
	if len(ir.Models) != 1 {
		t.Fatalf("expected one model instance, got %#v", ir.Models)
	}
	model := ir.Models[0]
	if model.ID != "runner" {
		t.Fatalf("expected runner model id, got %#v", model.ID)
	}
	if model.Src != "/models/runner.gosx3d.json" {
		t.Fatalf("expected model src, got %#v", model.Src)
	}
	if math.Abs(model.X-1.2) > 1e-9 || math.Abs(model.Y-0.4) > 1e-9 || math.Abs(model.Z+0.8) > 1e-9 {
		t.Fatalf("expected model position, got (%#v,%#v,%#v)", model.X, model.Y, model.Z)
	}
	if math.Abs(model.ScaleX-1.6) > 1e-9 || math.Abs(model.ScaleY-0.8) > 1e-9 || math.Abs(model.ScaleZ-1.2) > 1e-9 {
		t.Fatalf("expected model scale, got (%#v,%#v,%#v)", model.ScaleX, model.ScaleY, model.ScaleZ)
	}
	if math.Abs(model.Bounds-0.5) > 1e-9 || model.Fit != "contain" || model.FitAlign != "center-min-y" {
		t.Fatalf("expected model fit metadata, got bounds=%#v fit=%#v fitAlign=%#v", model.Bounds, model.Fit, model.FitAlign)
	}
	if model.MaterialKind != "glow" {
		t.Fatalf("expected glow material override, got %#v", model.MaterialKind)
	}
	if !model.CastShadow || !model.ReceiveShadow {
		t.Fatalf("expected model shadow flags to lower, got cast=%v receive=%v", model.CastShadow, model.ReceiveShadow)
	}
	if model.Pickable == nil || !*model.Pickable {
		t.Fatalf("expected explicit pickable model override, got %#v", model.Pickable)
	}
	if model.Static == nil || !*model.Static {
		t.Fatalf("expected explicit static override, got %#v", model.Static)
	}
	if len(ir.Objects) != 1 {
		t.Fatalf("expected one inline object, got %#v", ir.Objects)
	}
	object := ir.Objects[0]
	if object.Kind != "lines" {
		t.Fatalf("expected lines geometry kind, got %#v", object.Kind)
	}
	if len(object.Points) != 4 {
		t.Fatalf("expected four line points, got %#v", object.Points)
	}
	if len(object.LineSegments) != 4 {
		t.Fatalf("expected four line segments, got %#v", object.LineSegments)
	}
	if object.Pickable == nil || *object.Pickable {
		t.Fatalf("expected explicit non-pickable lines object, got %#v", object.Pickable)
	}

	legacy := props.LegacyProps()
	if got := legacy["pickSignalNamespace"]; got != "$scene.pick" {
		t.Fatalf("expected pick signal namespace in legacy props, got %#v", got)
	}
	sceneValue, ok := legacy["scene"].(map[string]any)
	if !ok {
		t.Fatalf("expected scene map, got %#v", legacy["scene"])
	}
	models, ok := sceneValue["models"].([]map[string]any)
	if !ok || len(models) != 1 {
		t.Fatalf("expected one model record, got %#v", sceneValue["models"])
	}
	if got := models[0]["src"]; got != "/models/runner.gosx3d.json" {
		t.Fatalf("expected model src in legacy props, got %#v", got)
	}
	if got := models[0]["pickable"]; got != true {
		t.Fatalf("expected model pickable in legacy props, got %#v", got)
	}
	if got := models[0]["bounds"]; got != 0.5 {
		t.Fatalf("expected model bounds in legacy props, got %#v", got)
	}
	if got := models[0]["fit"]; got != "contain" {
		t.Fatalf("expected model fit in legacy props, got %#v", got)
	}
	if got := models[0]["fitAlign"]; got != "center-min-y" {
		t.Fatalf("expected model fit alignment in legacy props, got %#v", got)
	}
	objects, ok := sceneValue["objects"].([]map[string]any)
	if !ok || len(objects) != 1 {
		t.Fatalf("expected one object record, got %#v", sceneValue["objects"])
	}
	if got := objects[0]["kind"]; got != "lines" {
		t.Fatalf("expected lines kind in legacy props, got %#v", got)
	}
	if _, ok := objects[0]["points"]; !ok {
		t.Fatalf("expected line points in legacy props, got %#v", objects[0])
	}
	if _, ok := objects[0]["lineSegments"]; !ok {
		t.Fatalf("expected line segments in legacy props, got %#v", objects[0])
	}
	if got := objects[0]["pickable"]; got != false {
		t.Fatalf("expected non-pickable lines object in legacy props, got %#v", got)
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
		engine.CapWebGPU,
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

func TestPropsEngineCapabilitiesIncludeWebGPUByDefault(t *testing.T) {
	cfg := (Props{}).EngineConfig()
	if !slicesContainCapability(cfg.Capabilities, engine.CapWebGPU) {
		t.Fatalf("expected webgpu capability by default, got %#v", cfg.Capabilities)
	}
}

func TestPropsEngineCapabilitiesIncludeWebGPUForComputeParticles(t *testing.T) {
	props := Props{
		Graph: NewGraph(
			Group{
				Children: []Node{
					ComputeParticles{ID: "sparks", Count: 128},
				},
			},
		),
	}

	cfg := props.EngineConfig()
	if err := engine.ValidateCapabilities(cfg.Capabilities); err != nil {
		t.Fatalf("expected generated capabilities to validate: %v", err)
	}
	if !slicesContainCapability(cfg.Capabilities, engine.CapWebGPU) {
		t.Fatalf("expected webgpu capability for compute particles, got %#v", cfg.Capabilities)
	}
}

func TestPropsEngineCapabilitiesIncludeWebGPUForWaterSystem(t *testing.T) {
	props := Props{
		Graph: NewGraph(
			WaterSystem{ID: "pool-water"},
		),
	}

	cfg := props.EngineConfig()
	if err := engine.ValidateCapabilities(cfg.Capabilities); err != nil {
		t.Fatalf("expected generated capabilities to validate: %v", err)
	}
	if !slicesContainCapability(cfg.Capabilities, engine.CapWebGPU) {
		t.Fatalf("expected webgpu capability for water system, got %#v", cfg.Capabilities)
	}
}

func TestPropsEngineConfigCarriesRequiredWebGLContract(t *testing.T) {
	props := Props{
		RequireWebGL:       Bool(true),
		UnsupportedMessage: "Use a current browser with hardware acceleration enabled.",
	}

	cfg := props.EngineConfig()
	if got := cfg.RequiredCapabilities; len(got) != 2 || got[0] != engine.CapCanvas || got[1] != engine.CapWebGL {
		t.Fatalf("unexpected required capabilities: %#v", got)
	}
	raw := string(cfg.Props)
	if !contains(raw, `"requireWebGL":true`) || !contains(raw, `"unsupportedMessage":"Use a current browser with hardware acceleration enabled."`) {
		t.Fatalf("expected required WebGL props in engine payload, got %s", raw)
	}
}

func TestPropsEngineConfigCarriesRequiredWebGPUContract(t *testing.T) {
	props := Props{
		RequiredCapabilities: RequireWebGPU(
			engine.CapWebGPUTimestampQuery,
			engine.WebGPULimit("maxTextureDimension2D", 4096),
			engine.WebGPUAdapterLimit("maxBufferSize", 1048576),
		),
	}

	cfg := props.EngineConfig()
	want := []engine.Capability{
		engine.CapWebGPU,
		engine.CapWebGPUTimestampQuery,
		"webgpu:limit:maxTextureDimension2D>=4096",
		"webgpu:adapter-limit:maxBufferSize>=1048576",
	}
	if len(cfg.RequiredCapabilities) != len(want) {
		t.Fatalf("expected %d required capabilities, got %#v", len(want), cfg.RequiredCapabilities)
	}
	for i := range want {
		if cfg.RequiredCapabilities[i] != want[i] {
			t.Fatalf("unexpected required capability %d: got %q want %q", i, cfg.RequiredCapabilities[i], want[i])
		}
	}
	if err := engine.ValidateCapabilities(cfg.RequiredCapabilities); err != nil {
		t.Fatalf("expected required WebGPU Scene3D config to validate: %v", err)
	}
	if contains(string(cfg.Props), "requiredCapabilities") {
		t.Fatalf("did not expect required capabilities in scene props json: %s", string(cfg.Props))
	}
}

func TestPropsSceneIRLowersStandardMaterial(t *testing.T) {
	props := Props{
		Graph: NewGraph(
			Mesh{
				ID:       "pbr-sphere",
				Geometry: SphereGeometry{Radius: 1.2, Segments: 32},
				Material: StandardMaterial{
					Color:        "#c0a070",
					Roughness:    0.45,
					Metalness:    0.9,
					Clearcoat:    0.35,
					Sheen:        0.2,
					Transmission: 0.12,
					Iridescence:  0.18,
					Anisotropy:   -0.25,
					NormalMap:    "/maps/normal.png",
					RoughnessMap: "/maps/roughness.png",
					MetalnessMap: "/maps/metalness.png",
					EmissiveMap:  "/maps/emissive.png",
					Emissive:     0.15,
					Opacity:      Float(0.88),
					BlendMode:    BlendAlpha,
					Wireframe:    Bool(false),
				},
				Position: Vec3(1, 2, 3),
			},
		),
	}

	ir := props.SceneIR()
	if len(ir.Objects) != 1 {
		t.Fatalf("expected one lowered object, got %d", len(ir.Objects))
	}
	obj := ir.Objects[0]
	if obj.ID != "pbr-sphere" {
		t.Fatalf("expected pbr-sphere id, got %q", obj.ID)
	}
	if obj.Kind != "sphere" {
		t.Fatalf("expected sphere geometry kind, got %q", obj.Kind)
	}
	if obj.MaterialKind != "standard" {
		t.Fatalf("expected standard material kind, got %q", obj.MaterialKind)
	}
	if obj.Roughness != 0.45 {
		t.Fatalf("expected roughness 0.45, got %v", obj.Roughness)
	}
	if obj.Metalness != 0.9 {
		t.Fatalf("expected metalness 0.9, got %v", obj.Metalness)
	}
	if obj.Clearcoat != 0.35 || obj.Sheen != 0.2 || obj.Transmission != 0.12 || obj.Iridescence != 0.18 || obj.Anisotropy != -0.25 {
		t.Fatalf("expected physical extension fields, got %#v", obj)
	}
	if obj.NormalMap != "/maps/normal.png" {
		t.Fatalf("expected normalMap, got %q", obj.NormalMap)
	}
	if obj.RoughnessMap != "/maps/roughness.png" {
		t.Fatalf("expected roughnessMap, got %q", obj.RoughnessMap)
	}
	if obj.MetalnessMap != "/maps/metalness.png" {
		t.Fatalf("expected metalnessMap, got %q", obj.MetalnessMap)
	}
	if obj.EmissiveMap != "/maps/emissive.png" {
		t.Fatalf("expected emissiveMap, got %q", obj.EmissiveMap)
	}
	if obj.Color != "#c0a070" {
		t.Fatalf("expected color, got %q", obj.Color)
	}
	if obj.Opacity == nil || *obj.Opacity != 0.88 {
		t.Fatalf("expected opacity 0.88, got %v", obj.Opacity)
	}
	if obj.BlendMode != "alpha" {
		t.Fatalf("expected alpha blend mode, got %q", obj.BlendMode)
	}
	if obj.Wireframe == nil || *obj.Wireframe {
		t.Fatalf("expected wireframe false, got %v", obj.Wireframe)
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
	if got := record["materialKind"]; got != "standard" {
		t.Fatalf("expected standard material kind in legacy props, got %#v", got)
	}
	if got := record["roughness"]; got != 0.45 {
		t.Fatalf("expected roughness in legacy props, got %#v", got)
	}
	if got := record["metalness"]; got != 0.9 {
		t.Fatalf("expected metalness in legacy props, got %#v", got)
	}
	if got := record["clearcoat"]; got != 0.35 {
		t.Fatalf("expected clearcoat in legacy props, got %#v", got)
	}
	if got := record["sheen"]; got != 0.2 {
		t.Fatalf("expected sheen in legacy props, got %#v", got)
	}
	if got := record["transmission"]; got != 0.12 {
		t.Fatalf("expected transmission in legacy props, got %#v", got)
	}
	if got := record["iridescence"]; got != 0.18 {
		t.Fatalf("expected iridescence in legacy props, got %#v", got)
	}
	if got := record["anisotropy"]; got != -0.25 {
		t.Fatalf("expected anisotropy in legacy props, got %#v", got)
	}
	if got := record["normalMap"]; got != "/maps/normal.png" {
		t.Fatalf("expected normalMap in legacy props, got %#v", got)
	}
}

func TestPropsSceneIRLowersCylinderAndTorusGeometry(t *testing.T) {
	props := Props{
		Graph: NewGraph(
			Mesh{
				ID: "cyl",
				Geometry: CylinderGeometry{
					RadiusTop:    0.5,
					RadiusBottom: 1.0,
					Height:       2.0,
					Segments:     16,
				},
				Material: FlatMaterial{Color: "#ff0000"},
				Position: Vec3(0, 0, 0),
			},
			Mesh{
				ID: "tor",
				Geometry: TorusGeometry{
					Radius:          1.5,
					Tube:            0.4,
					RadialSegments:  16,
					TubularSegments: 48,
				},
				Material: FlatMaterial{Color: "#00ff00"},
				Position: Vec3(3, 0, 0),
			},
		),
	}

	ir := props.SceneIR()
	if len(ir.Objects) != 2 {
		t.Fatalf("expected two lowered objects, got %d", len(ir.Objects))
	}

	cyl := ir.Objects[0]
	if cyl.Kind != "cylinder" {
		t.Fatalf("expected cylinder kind, got %q", cyl.Kind)
	}
	if cyl.RadiusTop != 0.5 {
		t.Fatalf("expected radiusTop 0.5, got %v", cyl.RadiusTop)
	}
	if cyl.RadiusBottom != 1.0 {
		t.Fatalf("expected radiusBottom 1.0, got %v", cyl.RadiusBottom)
	}
	if cyl.Height != 2.0 {
		t.Fatalf("expected height 2.0, got %v", cyl.Height)
	}
	if cyl.Segments != 16 {
		t.Fatalf("expected segments 16, got %v", cyl.Segments)
	}

	tor := ir.Objects[1]
	if tor.Kind != "torus" {
		t.Fatalf("expected torus kind, got %q", tor.Kind)
	}
	if tor.Radius != 1.5 {
		t.Fatalf("expected radius 1.5, got %v", tor.Radius)
	}
	if tor.Tube != 0.4 {
		t.Fatalf("expected tube 0.4, got %v", tor.Tube)
	}
	if tor.RadialSegments != 16 {
		t.Fatalf("expected radialSegments 16, got %v", tor.RadialSegments)
	}
	if tor.TubularSegments != 48 {
		t.Fatalf("expected tubularSegments 48, got %v", tor.TubularSegments)
	}

	legacy := props.LegacyProps()
	sceneValue, ok := legacy["scene"].(map[string]any)
	if !ok {
		t.Fatalf("expected scene map, got %#v", legacy["scene"])
	}
	objects, ok := sceneValue["objects"].([]map[string]any)
	if !ok || len(objects) != 2 {
		t.Fatalf("expected two objects in legacy props, got %#v", sceneValue["objects"])
	}
	if got := objects[0]["kind"]; got != "cylinder" {
		t.Fatalf("expected cylinder kind in legacy props, got %#v", got)
	}
	if got := objects[0]["radiusTop"]; got != 0.5 {
		t.Fatalf("expected radiusTop in legacy props, got %#v", got)
	}
	if got := objects[1]["kind"]; got != "torus" {
		t.Fatalf("expected torus kind in legacy props, got %#v", got)
	}
	if got := objects[1]["tube"]; got != 0.4 {
		t.Fatalf("expected tube in legacy props, got %#v", got)
	}
}

func TestPropsSceneIRLowersShadowFields(t *testing.T) {
	props := Props{
		Graph: NewGraph(
			DirectionalLight{
				ID:             "sun",
				Color:          "#ffffff",
				Intensity:      1.5,
				Direction:      Vec3(-1, -1, 0),
				CastShadow:     true,
				ShadowBias:     0.001,
				ShadowSize:     2048,
				ShadowCascades: 3,
				ShadowSoftness: 0.04,
			},
			Mesh{
				ID:            "floor",
				Geometry:      PlaneGeometry{Width: 10, Height: 10},
				Material:      MatteMaterial{Color: "#808080"},
				CastShadow:    true,
				ReceiveShadow: true,
			},
		),
	}

	ir := props.SceneIR()
	if len(ir.Lights) != 1 {
		t.Fatalf("expected one light, got %d", len(ir.Lights))
	}
	light := ir.Lights[0]
	if !light.CastShadow {
		t.Fatalf("expected light castShadow true, got false")
	}
	if light.ShadowBias != 0.001 {
		t.Fatalf("expected shadowBias 0.001, got %v", light.ShadowBias)
	}
	if light.ShadowSize != 2048 {
		t.Fatalf("expected shadowSize 2048, got %v", light.ShadowSize)
	}
	if light.ShadowCascades != 3 {
		t.Fatalf("expected shadowCascades 3, got %v", light.ShadowCascades)
	}
	if light.ShadowSoftness != 0.04 {
		t.Fatalf("expected shadowSoftness 0.04, got %v", light.ShadowSoftness)
	}
	canonical := props.CanonicalIR()
	if len(canonical.Lights) != 1 {
		t.Fatalf("expected one canonical light, got %d", len(canonical.Lights))
	}
	if canonical.Lights[0].ShadowCascades != 3 || canonical.Lights[0].ShadowSoftness != 0.04 {
		t.Fatalf("expected shadow polish in canonical IR, got %#v", canonical.Lights[0])
	}

	if len(ir.Objects) != 1 {
		t.Fatalf("expected one object, got %d", len(ir.Objects))
	}
	obj := ir.Objects[0]
	if !obj.CastShadow {
		t.Fatalf("expected object castShadow true, got false")
	}
	if !obj.ReceiveShadow {
		t.Fatalf("expected object receiveShadow true, got false")
	}

	legacy := props.LegacyProps()
	sceneValue, ok := legacy["scene"].(map[string]any)
	if !ok {
		t.Fatalf("expected scene map, got %#v", legacy["scene"])
	}
	lights, ok := sceneValue["lights"].([]map[string]any)
	if !ok || len(lights) != 1 {
		t.Fatalf("expected one light in legacy props, got %#v", sceneValue["lights"])
	}
	if got := lights[0]["castShadow"]; got != true {
		t.Fatalf("expected castShadow true in legacy light, got %#v", got)
	}
	if got := lights[0]["shadowBias"]; got != 0.001 {
		t.Fatalf("expected shadowBias in legacy light, got %#v", got)
	}
	if got := lights[0]["shadowSize"]; got != 2048 {
		t.Fatalf("expected shadowSize in legacy light, got %#v", got)
	}
	if got := lights[0]["shadowCascades"]; got != 3 {
		t.Fatalf("expected shadowCascades in legacy light, got %#v", got)
	}
	if got := lights[0]["shadowSoftness"]; got != 0.04 {
		t.Fatalf("expected shadowSoftness in legacy light, got %#v", got)
	}

	data, err := json.Marshal(props)
	if err != nil {
		t.Fatalf("marshal props: %v", err)
	}
	var wire map[string]any
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatalf("unmarshal props: %v", err)
	}
	sceneWire, ok := wire["scene"].(map[string]any)
	if !ok {
		t.Fatalf("expected scene object in JSON, got %#v", wire["scene"])
	}
	wireLights, ok := sceneWire["lights"].([]any)
	if !ok || len(wireLights) != 1 {
		t.Fatalf("expected one JSON light, got %#v", sceneWire["lights"])
	}
	wireLight, ok := wireLights[0].(map[string]any)
	if !ok {
		t.Fatalf("expected JSON light object, got %#v", wireLights[0])
	}
	if got := wireLight["shadowCascades"]; got != float64(3) {
		t.Fatalf("expected shadowCascades in JSON, got %#v", got)
	}
	if got := wireLight["shadowSoftness"]; got != 0.04 {
		t.Fatalf("expected shadowSoftness in JSON, got %#v", got)
	}
	objects, ok := sceneValue["objects"].([]map[string]any)
	if !ok || len(objects) != 1 {
		t.Fatalf("expected one object in legacy props, got %#v", sceneValue["objects"])
	}
	if got := objects[0]["castShadow"]; got != true {
		t.Fatalf("expected castShadow true in legacy object, got %#v", got)
	}
	if got := objects[0]["receiveShadow"]; got != true {
		t.Fatalf("expected receiveShadow true in legacy object, got %#v", got)
	}
}

func TestPropsSceneIRLowersEnvironmentMapFields(t *testing.T) {
	props := Props{
		Background: "#08151f",
		Environment: Environment{
			AmbientColor:   "#f4fbff",
			EnvironmentMap: " /hdri/studio.hdr ",
			EnvIntensity:   1.25,
			EnvRotation:    0.5,
			Exposure:       1.1,
		},
	}

	ir := props.SceneIR()
	if ir.Environment.EnvMap != "/hdri/studio.hdr" {
		t.Fatalf("expected envMap to be trimmed into SceneIR, got %#v", ir.Environment.EnvMap)
	}
	if ir.Environment.EnvIntensity != 1.25 {
		t.Fatalf("expected envIntensity 1.25 in SceneIR, got %v", ir.Environment.EnvIntensity)
	}
	if ir.Environment.EnvRotation != 0.5 {
		t.Fatalf("expected envRotation 0.5 in SceneIR, got %v", ir.Environment.EnvRotation)
	}

	canonical := props.CanonicalIR()
	if canonical.Environment.EnvMap != "/hdri/studio.hdr" {
		t.Fatalf("expected envMap in canonical IR, got %#v", canonical.Environment.EnvMap)
	}
	if canonical.Environment.EnvIntensity != 1.25 || canonical.Environment.EnvRotation != 0.5 {
		t.Fatalf("expected IBL controls in canonical IR, got %#v", canonical.Environment)
	}

	legacy := props.LegacyProps()
	sceneValue, ok := legacy["scene"].(map[string]any)
	if !ok {
		t.Fatalf("expected scene map, got %#v", legacy["scene"])
	}
	environment, ok := sceneValue["environment"].(map[string]any)
	if !ok {
		t.Fatalf("expected environment map, got %#v", sceneValue["environment"])
	}
	if got := environment["envMap"]; got != "/hdri/studio.hdr" {
		t.Fatalf("expected envMap in legacy props, got %#v", got)
	}
	if got := environment["envIntensity"]; got != 1.25 {
		t.Fatalf("expected envIntensity in legacy props, got %#v", got)
	}
	if got := environment["envRotation"]; got != 0.5 {
		t.Fatalf("expected envRotation in legacy props, got %#v", got)
	}

	data, err := json.Marshal(props)
	if err != nil {
		t.Fatalf("marshal props: %v", err)
	}
	var wire map[string]any
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatalf("unmarshal props: %v", err)
	}
	sceneWire, ok := wire["scene"].(map[string]any)
	if !ok {
		t.Fatalf("expected scene object in JSON, got %#v", wire["scene"])
	}
	envWire, ok := sceneWire["environment"].(map[string]any)
	if !ok {
		t.Fatalf("expected environment object in JSON, got %#v", sceneWire["environment"])
	}
	if got := envWire["envMap"]; got != "/hdri/studio.hdr" {
		t.Fatalf("expected envMap in JSON, got %#v", got)
	}
	if got := envWire["envIntensity"]; got != 1.25 {
		t.Fatalf("expected envIntensity in JSON, got %#v", got)
	}
	if got := envWire["envRotation"]; got != 0.5 {
		t.Fatalf("expected envRotation in JSON, got %#v", got)
	}
}

func TestShadowPolishClamps(t *testing.T) {
	props := Props{
		Graph: NewGraph(
			DirectionalLight{
				ID:             "negative-cascades",
				ShadowCascades: -2,
				ShadowSoftness: -0.5,
			},
			DirectionalLight{
				ID:             "too-many-cascades",
				ShadowCascades: 8,
				ShadowSoftness: math.Inf(1),
			},
			SpotLight{
				ID:             "soft-spot",
				ShadowSoftness: -1,
			},
		),
	}

	ir := props.SceneIR()
	if len(ir.Lights) != 3 {
		t.Fatalf("expected three lights, got %d", len(ir.Lights))
	}
	if ir.Lights[0].ShadowCascades != 1 {
		t.Fatalf("expected negative cascades to clamp to 1, got %v", ir.Lights[0].ShadowCascades)
	}
	if ir.Lights[0].ShadowSoftness != 0 {
		t.Fatalf("expected negative softness to clamp to 0, got %v", ir.Lights[0].ShadowSoftness)
	}
	if ir.Lights[1].ShadowCascades != 4 {
		t.Fatalf("expected high cascades to clamp to 4, got %v", ir.Lights[1].ShadowCascades)
	}
	if ir.Lights[1].ShadowSoftness != 0 {
		t.Fatalf("expected non-finite softness to clamp to 0, got %v", ir.Lights[1].ShadowSoftness)
	}
	if ir.Lights[2].ShadowSoftness != 0 {
		t.Fatalf("expected spot softness to clamp to 0, got %v", ir.Lights[2].ShadowSoftness)
	}

	legacy := ir.legacyProps()
	lights, ok := legacy["lights"].([]map[string]any)
	if !ok || len(lights) != 3 {
		t.Fatalf("expected three legacy lights, got %#v", legacy["lights"])
	}
	if got := lights[0]["shadowCascades"]; got != 1 {
		t.Fatalf("expected clamped shadowCascades in legacy props, got %#v", got)
	}
	if _, present := lights[0]["shadowSoftness"]; present {
		t.Fatalf("expected clamped zero shadowSoftness to be omitted, got %#v", lights[0])
	}
	if got := lights[1]["shadowCascades"]; got != 4 {
		t.Fatalf("expected clamped high shadowCascades in legacy props, got %#v", got)
	}
}

func TestPropsSceneIRLowersModelAnimationFields(t *testing.T) {
	props := Props{
		Graph: NewGraph(
			Model{
				ID:                 "dancer",
				Src:                "/models/dancer.gosx3d.json",
				Position:           Vec3(0, 0, 0),
				Scale:              Vec3(1, 1, 1),
				Animation:          "idle",
				AnimationSeq:       "idle-boot",
				AnimationSpeed:     Float(1.25),
				AnimationWeight:    Float(0.8),
				AnimationFadeInMS:  Int(120),
				AnimationFadeOutMS: Int(80),
				Loop:               Bool(true),
			},
		),
	}

	ir := props.SceneIR()
	if len(ir.Models) != 1 {
		t.Fatalf("expected one model, got %d", len(ir.Models))
	}
	model := ir.Models[0]
	if model.Animation != "idle" {
		t.Fatalf("expected animation 'idle', got %q", model.Animation)
	}
	if model.Loop == nil || !*model.Loop {
		t.Fatalf("expected loop true, got %v", model.Loop)
	}
	if model.AnimationSeq != "idle-boot" {
		t.Fatalf("expected animation sequence 'idle-boot', got %q", model.AnimationSeq)
	}
	if model.AnimationSpeed == nil || *model.AnimationSpeed != 1.25 {
		t.Fatalf("expected animation speed 1.25, got %v", model.AnimationSpeed)
	}
	if model.AnimationWeight == nil || *model.AnimationWeight != 0.8 {
		t.Fatalf("expected animation weight 0.8, got %v", model.AnimationWeight)
	}
	if model.AnimationFadeInMS == nil || *model.AnimationFadeInMS != 120 {
		t.Fatalf("expected animation fade-in 120ms, got %v", model.AnimationFadeInMS)
	}
	if model.AnimationFadeOutMS == nil || *model.AnimationFadeOutMS != 80 {
		t.Fatalf("expected animation fade-out 80ms, got %v", model.AnimationFadeOutMS)
	}

	legacy := props.LegacyProps()
	sceneValue, ok := legacy["scene"].(map[string]any)
	if !ok {
		t.Fatalf("expected scene map, got %#v", legacy["scene"])
	}
	models, ok := sceneValue["models"].([]map[string]any)
	if !ok || len(models) != 1 {
		t.Fatalf("expected one model in legacy props, got %#v", sceneValue["models"])
	}
	if got := models[0]["animation"]; got != "idle" {
		t.Fatalf("expected animation in legacy model, got %#v", got)
	}
	if got := models[0]["loop"]; got != true {
		t.Fatalf("expected loop true in legacy model, got %#v", got)
	}
	if got := models[0]["animationSeq"]; got != "idle-boot" {
		t.Fatalf("expected animation sequence in legacy model, got %#v", got)
	}
	if got := models[0]["animationSpeed"]; got != 1.25 {
		t.Fatalf("expected animation speed in legacy model, got %#v", got)
	}
	if got := models[0]["animationWeight"]; got != 0.8 {
		t.Fatalf("expected animation weight in legacy model, got %#v", got)
	}
	if got := models[0]["animationFadeInMS"]; got != 120 {
		t.Fatalf("expected animation fade-in in legacy model, got %#v", got)
	}
	if got := models[0]["animationFadeOutMS"]; got != 80 {
		t.Fatalf("expected animation fade-out in legacy model, got %#v", got)
	}
}

func TestPropsSceneIRSanitizesModelAnimationControls(t *testing.T) {
	props := Props{
		Graph: NewGraph(
			Model{
				ID:                 "dancer",
				Src:                "/models/dancer.gosx3d.json",
				Animation:          "idle",
				AnimationSpeed:     Float(-2),
				AnimationWeight:    Float(math.Inf(1)),
				AnimationFadeInMS:  Int(-120),
				AnimationFadeOutMS: Int(-80),
			},
		),
	}

	ir := props.SceneIR()
	if len(ir.Models) != 1 {
		t.Fatalf("expected one model, got %d", len(ir.Models))
	}
	model := ir.Models[0]
	if model.AnimationSpeed == nil || *model.AnimationSpeed != 0 {
		t.Fatalf("expected negative animation speed to clamp to 0, got %v", model.AnimationSpeed)
	}
	if model.AnimationWeight != nil {
		t.Fatalf("expected non-finite animation weight to be omitted, got %v", model.AnimationWeight)
	}
	if model.AnimationFadeInMS == nil || *model.AnimationFadeInMS != 0 {
		t.Fatalf("expected negative animation fade-in to clamp to 0, got %v", model.AnimationFadeInMS)
	}
	if model.AnimationFadeOutMS == nil || *model.AnimationFadeOutMS != 0 {
		t.Fatalf("expected negative animation fade-out to clamp to 0, got %v", model.AnimationFadeOutMS)
	}
	if _, err := json.Marshal(props); err != nil {
		t.Fatalf("expected sanitized animation controls to marshal: %v", err)
	}
}

func TestPropsSceneIRLowersPointsNode(t *testing.T) {
	props := Props{
		Graph: NewGraph(
			Points{
				ID:           "stars",
				Count:        3,
				Positions:    []Vector3{Vec3(1, 2, 3), Vec3(4, 5, 6), Vec3(7, 8, 9)},
				Sizes:        []float64{2.0, 3.0, 4.0},
				Colors:       []string{"#ff0000", "#00ff00", "#0000ff"},
				Color:        "#ffffff",
				Style:        PointStyleFocus,
				Size:         5.0,
				MinPixelSize: 1.75,
				MaxPixelSize: 9.5,
				Opacity:      0.8,
				BlendMode:    BlendAdditive,
				DepthWrite:   false,
				Attenuation:  true,
				Position:     Vec3(10, 20, 30),
				Spin:         Rotate(0.1, 0.2, 0.3),
			},
		),
	}

	ir := props.SceneIR()
	if len(ir.Points) != 1 {
		t.Fatalf("expected one lowered points entry, got %d", len(ir.Points))
	}
	pts := ir.Points[0]
	if pts.ID != "stars" {
		t.Fatalf("expected points id 'stars', got %q", pts.ID)
	}
	if pts.Count != 3 {
		t.Fatalf("expected count 3, got %d", pts.Count)
	}
	// Positions should be flattened to [x,y,z,x,y,z,x,y,z].
	if len(pts.Positions) != 9 {
		t.Fatalf("expected 9 flat position values, got %d", len(pts.Positions))
	}
	if pts.Positions[0] != 1 || pts.Positions[1] != 2 || pts.Positions[2] != 3 {
		t.Fatalf("expected first position (1,2,3), got (%v,%v,%v)", pts.Positions[0], pts.Positions[1], pts.Positions[2])
	}
	if len(pts.Sizes) != 3 {
		t.Fatalf("expected 3 per-particle sizes, got %d", len(pts.Sizes))
	}
	if len(pts.Colors) != 3 {
		t.Fatalf("expected 3 per-particle colors, got %d", len(pts.Colors))
	}
	if pts.Color != "#ffffff" {
		t.Fatalf("expected uniform color, got %q", pts.Color)
	}
	if pts.Style != "focus" {
		t.Fatalf("expected style focus, got %q", pts.Style)
	}
	if pts.Size != 5.0 {
		t.Fatalf("expected uniform size 5.0, got %v", pts.Size)
	}
	if pts.MinPixelSize != 1.75 {
		t.Fatalf("expected min pixel size 1.75, got %v", pts.MinPixelSize)
	}
	if pts.MaxPixelSize != 9.5 {
		t.Fatalf("expected max pixel size 9.5, got %v", pts.MaxPixelSize)
	}
	if pts.Opacity != 0.8 {
		t.Fatalf("expected opacity 0.8, got %v", pts.Opacity)
	}
	if pts.BlendMode != "additive" {
		t.Fatalf("expected additive blend mode, got %q", pts.BlendMode)
	}
	if pts.DepthWrite == nil || *pts.DepthWrite {
		t.Fatalf("expected depthWrite false, got %v", pts.DepthWrite)
	}
	if !pts.Attenuation {
		t.Fatalf("expected attenuation true, got false")
	}
	if math.Abs(pts.X-10) > 1e-9 || math.Abs(pts.Y-20) > 1e-9 || math.Abs(pts.Z-30) > 1e-9 {
		t.Fatalf("expected world position (10,20,30), got (%v,%v,%v)", pts.X, pts.Y, pts.Z)
	}
	if pts.SpinX != 0.1 || pts.SpinY != 0.2 || pts.SpinZ != 0.3 {
		t.Fatalf("expected spin (0.1,0.2,0.3), got (%v,%v,%v)", pts.SpinX, pts.SpinY, pts.SpinZ)
	}

	// Verify legacy props round-trip.
	legacy := props.LegacyProps()
	sceneValue, ok := legacy["scene"].(map[string]any)
	if !ok {
		t.Fatalf("expected scene map, got %#v", legacy["scene"])
	}
	pointsList, ok := sceneValue["points"].([]map[string]any)
	if !ok || len(pointsList) != 1 {
		t.Fatalf("expected one points record in legacy props, got %#v", sceneValue["points"])
	}
	if got := pointsList[0]["id"]; got != "stars" {
		t.Fatalf("expected points id in legacy props, got %#v", got)
	}
	if got := pointsList[0]["attenuation"]; got != true {
		t.Fatalf("expected attenuation true in legacy props, got %#v", got)
	}
	if got := pointsList[0]["depthWrite"]; got != false {
		t.Fatalf("expected depthWrite false in legacy props, got %#v", got)
	}
	if got := pointsList[0]["style"]; got != "focus" {
		t.Fatalf("expected style focus in legacy props, got %#v", got)
	}
	if got := pointsList[0]["maxPixelSize"]; got != 9.5 {
		t.Fatalf("expected maxPixelSize 9.5 in legacy props, got %#v", got)
	}
	if got := pointsList[0]["minPixelSize"]; got != 1.75 {
		t.Fatalf("expected minPixelSize 1.75 in legacy props, got %#v", got)
	}
}

func TestPropsSceneIRLowersFogFields(t *testing.T) {
	props := Props{
		Environment: Environment{
			AmbientColor:     "#f4fbff",
			AmbientIntensity: 0.22,
			FogColor:         "#aabbcc",
			FogDensity:       0.035,
		},
		Graph: NewGraph(
			Mesh{
				ID:       "cube",
				Geometry: CubeGeometry{Size: 1},
				Material: FlatMaterial{Color: "#ffffff"},
			},
		),
	}

	ir := props.SceneIR()
	if ir.Environment.FogColor != "#aabbcc" {
		t.Fatalf("expected fog color '#aabbcc', got %q", ir.Environment.FogColor)
	}
	if ir.Environment.FogDensity != 0.035 {
		t.Fatalf("expected fog density 0.035, got %v", ir.Environment.FogDensity)
	}

	legacy := props.LegacyProps()
	sceneValue, ok := legacy["scene"].(map[string]any)
	if !ok {
		t.Fatalf("expected scene map, got %#v", legacy["scene"])
	}
	environment, ok := sceneValue["environment"].(map[string]any)
	if !ok {
		t.Fatalf("expected environment map, got %#v", sceneValue["environment"])
	}
	if got := environment["fogColor"]; got != "#aabbcc" {
		t.Fatalf("expected fogColor in legacy props, got %#v", got)
	}
	if got := environment["fogDensity"]; got != 0.035 {
		t.Fatalf("expected fogDensity in legacy props, got %#v", got)
	}
}

func TestPropsSceneIRLowersDepthWriteOnMesh(t *testing.T) {
	props := Props{
		Graph: NewGraph(
			Mesh{
				ID:         "no-depth",
				Geometry:   CubeGeometry{Size: 1},
				Material:   FlatMaterial{Color: "#ffffff"},
				DepthWrite: Bool(false),
			},
			Mesh{
				ID:       "default-depth",
				Geometry: CubeGeometry{Size: 1},
				Material: FlatMaterial{Color: "#ffffff"},
			},
		),
	}

	ir := props.SceneIR()
	if len(ir.Objects) != 2 {
		t.Fatalf("expected two objects, got %d", len(ir.Objects))
	}

	noDepth := ir.Objects[0]
	if noDepth.DepthWrite == nil {
		t.Fatalf("expected explicit depthWrite on first object, got nil")
	}
	if *noDepth.DepthWrite {
		t.Fatalf("expected depthWrite false on first object, got true")
	}

	defaultDepth := ir.Objects[1]
	if defaultDepth.DepthWrite != nil {
		t.Fatalf("expected nil depthWrite on second object (default), got %v", *defaultDepth.DepthWrite)
	}

	legacy := props.LegacyProps()
	sceneValue, ok := legacy["scene"].(map[string]any)
	if !ok {
		t.Fatalf("expected scene map, got %#v", legacy["scene"])
	}
	objects, ok := sceneValue["objects"].([]map[string]any)
	if !ok || len(objects) != 2 {
		t.Fatalf("expected two objects in legacy props, got %#v", sceneValue["objects"])
	}
	if got := objects[0]["depthWrite"]; got != false {
		t.Fatalf("expected depthWrite false in legacy props, got %#v", got)
	}
	if _, exists := objects[1]["depthWrite"]; exists {
		t.Fatalf("did not expect depthWrite key on default object, got %#v", objects[1]["depthWrite"])
	}
}

func TestPropsSceneIRLowersVisibleOnMeshAndModel(t *testing.T) {
	props := Props{
		Graph: NewGraph(
			Mesh{
				ID:       "hidden-mesh",
				Geometry: CubeGeometry{Size: 1},
				Material: FlatMaterial{Color: "#ffffff"},
				Visible:  Bool(false),
			},
			Model{
				ID:      "hidden-model",
				Src:     "duck.gltf",
				Visible: Bool(false),
			},
		),
	}

	ir := props.SceneIR()
	if len(ir.Objects) != 1 || ir.Objects[0].Visible == nil || *ir.Objects[0].Visible {
		t.Fatalf("expected hidden mesh visible=false in SceneIR, got %#v", ir.Objects)
	}
	if len(ir.Models) != 1 || ir.Models[0].Visible == nil || *ir.Models[0].Visible {
		t.Fatalf("expected hidden model visible=false in SceneIR, got %#v", ir.Models)
	}

	legacy := props.LegacyProps()
	sceneValue, ok := legacy["scene"].(map[string]any)
	if !ok {
		t.Fatalf("expected scene map, got %#v", legacy["scene"])
	}
	objects, ok := sceneValue["objects"].([]map[string]any)
	if !ok || len(objects) != 1 || objects[0]["visible"] != false {
		t.Fatalf("expected visible=false in legacy object props, got %#v", sceneValue["objects"])
	}
	models, ok := sceneValue["models"].([]map[string]any)
	if !ok || len(models) != 1 || models[0]["visible"] != false {
		t.Fatalf("expected visible=false in legacy model props, got %#v", sceneValue["models"])
	}

	canonical := props.CanonicalIR()
	if len(canonical.Nodes) != 2 {
		t.Fatalf("expected 2 canonical nodes, got %d", len(canonical.Nodes))
	}
	for _, node := range canonical.Nodes {
		if node.Mesh == nil || node.Mesh.Visible == nil || *node.Mesh.Visible {
			t.Fatalf("expected canonical node %q visible=false, got %#v", node.ID, node.Mesh)
		}
	}
}

func TestPropsSceneIRLowersHTMLOverlays(t *testing.T) {
	props := Props{
		Graph: NewGraph(
			Mesh{
				ID:       "hero",
				Geometry: BoxGeometry{Width: 1.4, Height: 1.1, Depth: 0.9},
				Material: FlatMaterial{Color: "#8de1ff"},
				Position: Vec3(1.2, 0.4, -0.2),
			},
			HTML{
				ID:            "inspect-card",
				Target:        "hero",
				Markup:        `<button>Inspect</button>`,
				ClassName:     "scene-card",
				Position:      Vec3(0, 1.05, 0.2),
				Width:         220,
				Height:        88,
				Priority:      5,
				AnchorX:       0.5,
				AnchorY:       1,
				Occlude:       true,
				PointerEvents: "auto",
			},
			HTMLSurface{
				ID:             "panel",
				Target:         "hero",
				Markup:         `<div>Panel</div>`,
				Fallback:       `<div>Panel fallback</div>`,
				FallbackReason: "accessible-dom-fallback",
				Width:          512,
				Height:         320,
			},
		),
	}

	ir := props.SceneIR()
	if len(ir.HTML) != 2 {
		t.Fatalf("expected two lowered html entries, got %#v", ir.HTML)
	}
	card := ir.HTML[0]
	if card.ID != "inspect-card" || card.HTML != `<button>Inspect</button>` {
		t.Fatalf("expected html overlay content, got %#v", card)
	}
	if card.Target != "hero" {
		t.Fatalf("expected html target preservation, got %#v", card.Target)
	}
	if math.Abs(card.X-1.2) > 1e-9 || math.Abs(card.Y-1.45) > 1e-9 {
		t.Fatalf("expected targeted html position near hero, got (%#v,%#v)", card.X, card.Y)
	}
	if card.PointerEvents != "auto" || !card.Occlude {
		t.Fatalf("expected html interaction fields, got %#v", card)
	}
	if ir.HTML[1].Mode != string(HTMLTexture) {
		t.Fatalf("expected html surface texture mode marker, got %#v", ir.HTML[1].Mode)
	}
	if ir.HTML[1].Target != "hero" || ir.HTML[1].Fallback != `<div>Panel fallback</div>` || ir.HTML[1].FallbackReason != "accessible-dom-fallback" {
		t.Fatalf("expected html surface target and fallback metadata, got %#v", ir.HTML[1])
	}
	if ir.HTML[1].TextureWidth != 512 || ir.HTML[1].TextureHeight != 320 {
		t.Fatalf("expected html surface texture dimensions, got %#v", ir.HTML[1])
	}
	if math.Abs(ir.HTML[1].Width-1.8) > 1e-9 || math.Abs(ir.HTML[1].Height-1.125) > 1e-9 {
		t.Fatalf("expected html surface fallback world size, got (%#v,%#v)", ir.HTML[1].Width, ir.HTML[1].Height)
	}

	legacy := props.LegacyProps()
	sceneValue, ok := legacy["scene"].(map[string]any)
	if !ok {
		t.Fatalf("expected scene map, got %#v", legacy["scene"])
	}
	html, ok := sceneValue["html"].([]map[string]any)
	if !ok || len(html) != 2 {
		t.Fatalf("expected two lowered html records, got %#v", sceneValue["html"])
	}
	if got := html[0]["pointerEvents"]; got != "auto" {
		t.Fatalf("expected pointerEvents in legacy props, got %#v", got)
	}
	if got := html[0]["target"]; got != "hero" {
		t.Fatalf("expected target in legacy props, got %#v", got)
	}
	if got := html[1]["mode"]; got != string(HTMLTexture) {
		t.Fatalf("expected texture mode in legacy props, got %#v", got)
	}
	if got := html[1]["fallback"]; got != `<div>Panel fallback</div>` {
		t.Fatalf("expected html surface fallback metadata in legacy props, got %#v", got)
	}
	if got := html[1]["textureWidth"]; got != 512 {
		t.Fatalf("expected html surface texture width in legacy props, got %#v", got)
	}

	canonical := props.CanonicalIR()
	var htmlNodes int
	for _, node := range canonical.Nodes {
		if node.Kind != "html" || node.HTML == nil {
			continue
		}
		htmlNodes++
		if node.ID == "inspect-card" && node.HTML.Target != "hero" {
			t.Fatalf("expected canonical html target preservation, got %#v", node.HTML)
		}
		if node.ID == "panel" && (node.HTML.Target != "hero" || node.HTML.Fallback != `<div>Panel fallback</div>` || node.HTML.TextureWidth != 512) {
			t.Fatalf("expected canonical html surface fallback metadata, got %#v", node.HTML)
		}
	}
	if htmlNodes != 2 {
		t.Fatalf("expected two canonical html nodes, got %#v", canonical.Nodes)
	}
}

func TestPropsSceneIRLowersHTMLPortalMode(t *testing.T) {
	props := Props{
		Graph: NewGraph(
			HTML{
				ID:            "portal",
				Mode:          HTMLPortal,
				Markup:        `<form><button>Apply</button></form>`,
				PointerEvents: "auto",
			},
		),
	}

	ir := props.SceneIR()
	if len(ir.HTML) != 1 {
		t.Fatalf("expected one html portal, got %#v", ir.HTML)
	}
	if ir.HTML[0].Mode != string(HTMLPortal) {
		t.Fatalf("expected portal mode, got %#v", ir.HTML[0].Mode)
	}
	if ir.HTML[0].PointerEvents != "auto" {
		t.Fatalf("expected pointer-events to survive portal lowering, got %#v", ir.HTML[0].PointerEvents)
	}

	legacy := props.LegacyProps()
	sceneValue, ok := legacy["scene"].(map[string]any)
	if !ok {
		t.Fatalf("expected scene map, got %#v", legacy["scene"])
	}
	html, ok := sceneValue["html"].([]map[string]any)
	if !ok || len(html) != 1 {
		t.Fatalf("expected one legacy portal record, got %#v", sceneValue["html"])
	}
	if got := html[0]["mode"]; got != string(HTMLPortal) {
		t.Fatalf("expected legacy portal mode, got %#v", got)
	}
}

func TestPropsSceneIRLowersInstancedMesh(t *testing.T) {
	props := Props{
		Graph: NewGraph(
			InstancedMesh{
				ID:       "trees",
				Count:    3,
				Geometry: BoxGeometry{Width: 1, Height: 2, Depth: 0.5},
				Material: FlatMaterial{Color: "#33aa55"},
				Positions: []Vector3{
					Vec3(0, 0, 0),
					Vec3(5, 0, 0),
					Vec3(0, 0, 5),
				},
				Rotations: []Euler{
					Rotate(0, 0, 0),
					Rotate(0, math.Pi/4, 0),
					Rotate(0, 0, 0),
				},
				Scales: []Vector3{
					Vec3(1, 1, 1),
					Vec3(2, 2, 2),
					Vec3(1, 1.5, 1),
				},
				Colors:        []string{"#ff0000", "#00ff00", "#0000ff"},
				Attributes:    map[string][]float64{"heat": []float64{0.1, 0.5, 1}},
				CastShadow:    true,
				ReceiveShadow: true,
			},
		),
	}

	ir := props.SceneIR()
	if len(ir.InstancedMeshes) != 1 {
		t.Fatalf("expected one instanced mesh, got %d", len(ir.InstancedMeshes))
	}
	im := ir.InstancedMeshes[0]
	if im.ID != "trees" {
		t.Fatalf("expected id 'trees', got %q", im.ID)
	}
	if im.Count != 3 {
		t.Fatalf("expected count 3, got %d", im.Count)
	}
	if im.Kind != "box" {
		t.Fatalf("expected box geometry kind, got %q", im.Kind)
	}
	if im.MaterialKind != "flat" {
		t.Fatalf("expected flat material kind, got %q", im.MaterialKind)
	}
	if im.Color != "#33aa55" {
		t.Fatalf("expected color #33aa55, got %q", im.Color)
	}
	if len(im.Colors) != 3 || im.Colors[1] != "#00ff00" {
		t.Fatalf("expected per-instance colors, got %#v", im.Colors)
	}
	if len(im.Attributes["heat"]) != 3 || im.Attributes["heat"][2] != 1 {
		t.Fatalf("expected per-instance heat attribute, got %#v", im.Attributes)
	}
	if !im.CastShadow {
		t.Fatalf("expected castShadow true")
	}
	if !im.ReceiveShadow {
		t.Fatalf("expected receiveShadow true")
	}
	// 3 instances * 16 floats per mat4 = 48 total.
	if len(im.Transforms) != 48 {
		t.Fatalf("expected 48 transform floats, got %d", len(im.Transforms))
	}
	// First instance: identity rotation, unit scale, position (0,0,0).
	// Column-major identity-like: [1,0,0,0, 0,1,0,0, 0,0,1,0, 0,0,0,1]
	if math.Abs(im.Transforms[0]-1) > 1e-9 {
		t.Fatalf("expected first transform m[0,0] = 1, got %v", im.Transforms[0])
	}
	if math.Abs(im.Transforms[12]-0) > 1e-9 {
		t.Fatalf("expected first transform tx = 0, got %v", im.Transforms[12])
	}
	if math.Abs(im.Transforms[15]-1) > 1e-9 {
		t.Fatalf("expected first transform m[3,3] = 1, got %v", im.Transforms[15])
	}
	// Second instance: position (5,0,0), scale (2,2,2), rotated pi/4 around Y.
	if math.Abs(im.Transforms[16+12]-5) > 1e-9 {
		t.Fatalf("expected second transform tx = 5, got %v", im.Transforms[16+12])
	}
	// Third instance: position (0,0,5), scale (1,1.5,1), no rotation.
	if math.Abs(im.Transforms[32+12]-0) > 1e-9 {
		t.Fatalf("expected third transform tx = 0, got %v", im.Transforms[32+12])
	}
	if math.Abs(im.Transforms[32+14]-5) > 1e-9 {
		t.Fatalf("expected third transform tz = 5, got %v", im.Transforms[32+14])
	}
	// Third instance scale Y = 1.5 should show up in m[1][1] (index 5).
	if math.Abs(im.Transforms[32+5]-1.5) > 1e-9 {
		t.Fatalf("expected third transform scaleY = 1.5, got %v", im.Transforms[32+5])
	}

	// Verify legacy props round-trip.
	legacy := props.LegacyProps()
	sceneValue, ok := legacy["scene"].(map[string]any)
	if !ok {
		t.Fatalf("expected scene map, got %#v", legacy["scene"])
	}
	instancedMeshes, ok := sceneValue["instancedMeshes"].([]map[string]any)
	if !ok || len(instancedMeshes) != 1 {
		t.Fatalf("expected one instanced mesh record, got %#v", sceneValue["instancedMeshes"])
	}
	if got := instancedMeshes[0]["id"]; got != "trees" {
		t.Fatalf("expected id in legacy props, got %#v", got)
	}
	if got := instancedMeshes[0]["kind"]; got != "box" {
		t.Fatalf("expected kind in legacy props, got %#v", got)
	}
	if got := instancedMeshes[0]["colors"]; got == nil {
		t.Fatalf("expected colors in legacy props, got %#v", instancedMeshes[0])
	}
	if got := instancedMeshes[0]["attributes"]; got == nil {
		t.Fatalf("expected attributes in legacy props, got %#v", instancedMeshes[0])
	}
}

func TestPropsSceneIRLowersInstancedMeshPrimitiveParameters(t *testing.T) {
	props := Props{
		Graph: NewGraph(
			InstancedMesh{
				ID:       "columns",
				Count:    1,
				Geometry: CylinderGeometry{RadiusTop: 0.4, RadiusBottom: 0.8, Height: 3, Segments: 24},
				Positions: []Vector3{
					Vec3(0, 0, 0),
				},
			},
			InstancedMesh{
				ID:       "rings",
				Count:    1,
				Geometry: TorusGeometry{Radius: 1.25, Tube: 0.18, RadialSegments: 40, TubularSegments: 12},
				Material: GlowMaterial{Color: "#f0b35b", Texture: "/atlas.png", Opacity: Float(0.42), Emissive: Float(0.34), BlendMode: BlendAdditive, RenderPass: RenderAdditive},
				Positions: []Vector3{
					Vec3(2, 0, 0),
				},
			},
		),
	}

	ir := props.SceneIR()
	if len(ir.InstancedMeshes) != 2 {
		t.Fatalf("expected two instanced meshes, got %d", len(ir.InstancedMeshes))
	}
	cyl := ir.InstancedMeshes[0]
	if cyl.Kind != "cylinder" {
		t.Fatalf("expected cylinder kind, got %q", cyl.Kind)
	}
	if cyl.RadiusTop != 0.4 || cyl.RadiusBottom != 0.8 || cyl.Height != 3 || cyl.Segments != 24 {
		t.Fatalf("cylinder parameters were not preserved: %#v", cyl)
	}
	tor := ir.InstancedMeshes[1]
	if tor.Kind != "torus" {
		t.Fatalf("expected torus kind, got %q", tor.Kind)
	}
	if tor.Radius != 1.25 || tor.Tube != 0.18 || tor.RadialSegments != 40 || tor.TubularSegments != 12 {
		t.Fatalf("torus parameters were not preserved: %#v", tor)
	}
	if tor.MaterialKind != "glow" || tor.Texture != "/atlas.png" || tor.Opacity == nil || *tor.Opacity != 0.42 || tor.Emissive == nil || *tor.Emissive != 0.34 || tor.BlendMode != "additive" || tor.RenderPass != "additive" {
		t.Fatalf("torus material parameters were not preserved: %#v", tor)
	}

	legacy := props.LegacyProps()
	sceneValue, ok := legacy["scene"].(map[string]any)
	if !ok {
		t.Fatalf("expected scene map, got %#v", legacy["scene"])
	}
	instancedMeshes, ok := sceneValue["instancedMeshes"].([]map[string]any)
	if !ok || len(instancedMeshes) != 2 {
		t.Fatalf("expected two instanced mesh records, got %#v", sceneValue["instancedMeshes"])
	}
	if got := instancedMeshes[0]["radiusTop"]; got != 0.4 {
		t.Fatalf("expected radiusTop in legacy props, got %#v", got)
	}
	if got := instancedMeshes[1]["tube"]; got != 0.18 {
		t.Fatalf("expected tube in legacy props, got %#v", got)
	}
	if got := instancedMeshes[1]["radialSegments"]; got != 40 {
		t.Fatalf("expected radialSegments in legacy props, got %#v", got)
	}
	if got := instancedMeshes[1]["opacity"]; got != 0.42 {
		t.Fatalf("expected opacity in legacy props, got %#v", got)
	}
	if got := instancedMeshes[1]["emissive"]; got != 0.34 {
		t.Fatalf("expected emissive in legacy props, got %#v", got)
	}
	if got := instancedMeshes[1]["blendMode"]; got != "additive" {
		t.Fatalf("expected additive blendMode in legacy props, got %#v", got)
	}
}

func TestPropsSceneIRLowersComputeParticles(t *testing.T) {
	props := Props{
		Graph: NewGraph(
			ComputeParticles{
				ID:    "galaxy",
				Count: 50000,
				Emitter: ParticleEmitter{
					Kind:     "spiral",
					Position: Vec3(0, 5, 0),
					Radius:   8,
					Rate:     2000,
					Lifetime: 4.5,
					Arms:     4,
					Wind:     0.3,
					Scatter:  0.15,
					Once:     true,
				},
				Forces: []ParticleForce{
					{
						Kind:      "orbit",
						Strength:  1.2,
						Direction: Vec3(0, 1, 0),
						Frequency: 0.8,
					},
					{
						Kind:     "gravity",
						Strength: -0.5,
					},
				},
				Material: ParticleMaterial{
					Color:       "#ffcc88",
					ColorEnd:    "#3366ff",
					Style:       PointStyleFocus,
					Size:        0.15,
					SizeEnd:     0.02,
					Opacity:     1.0,
					OpacityEnd:  0.0,
					BlendMode:   BlendAdditive,
					Attenuation: true,
				},
				Bounds: 20,
			},
		),
	}

	ir := props.SceneIR()
	if len(ir.ComputeParticles) != 1 {
		t.Fatalf("expected one compute particles entry, got %d", len(ir.ComputeParticles))
	}
	cp := ir.ComputeParticles[0]
	if cp.ID != "galaxy" {
		t.Fatalf("expected id 'galaxy', got %q", cp.ID)
	}
	if cp.Count != 50000 {
		t.Fatalf("expected count 50000, got %d", cp.Count)
	}
	if cp.Emitter.Kind != "spiral" {
		t.Fatalf("expected spiral emitter kind, got %q", cp.Emitter.Kind)
	}
	if math.Abs(cp.Emitter.Y-5) > 1e-9 {
		t.Fatalf("expected emitter Y = 5 (from position), got %v", cp.Emitter.Y)
	}
	if cp.Emitter.Radius != 8 {
		t.Fatalf("expected emitter radius 8, got %v", cp.Emitter.Radius)
	}
	if cp.Emitter.Rate != 2000 {
		t.Fatalf("expected emitter rate 2000, got %v", cp.Emitter.Rate)
	}
	if cp.Emitter.Lifetime != 4.5 {
		t.Fatalf("expected emitter lifetime 4.5, got %v", cp.Emitter.Lifetime)
	}
	if cp.Emitter.Arms != 4 {
		t.Fatalf("expected emitter arms 4, got %d", cp.Emitter.Arms)
	}
	if cp.Emitter.Wind != 0.3 {
		t.Fatalf("expected emitter wind 0.3, got %v", cp.Emitter.Wind)
	}
	if cp.Emitter.Scatter != 0.15 {
		t.Fatalf("expected emitter scatter 0.15, got %v", cp.Emitter.Scatter)
	}
	if !cp.Emitter.Once {
		t.Fatalf("expected one-shot emitter")
	}
	if cp.Material.Style != "focus" {
		t.Fatalf("expected material style focus, got %q", cp.Material.Style)
	}
	if len(cp.Forces) != 2 {
		t.Fatalf("expected two forces, got %d", len(cp.Forces))
	}
	if cp.Forces[0].Kind != "orbit" {
		t.Fatalf("expected orbit force kind, got %q", cp.Forces[0].Kind)
	}
	if cp.Forces[0].Strength != 1.2 {
		t.Fatalf("expected orbit force strength 1.2, got %v", cp.Forces[0].Strength)
	}
	if cp.Forces[0].Y != 1 {
		t.Fatalf("expected orbit force direction Y = 1, got %v", cp.Forces[0].Y)
	}
	if cp.Forces[0].Frequency != 0.8 {
		t.Fatalf("expected orbit force frequency 0.8, got %v", cp.Forces[0].Frequency)
	}
	if cp.Forces[1].Kind != "gravity" {
		t.Fatalf("expected gravity force kind, got %q", cp.Forces[1].Kind)
	}
	if cp.Forces[1].Strength != -0.5 {
		t.Fatalf("expected gravity force strength -0.5, got %v", cp.Forces[1].Strength)
	}
	if cp.Material.Color != "#ffcc88" {
		t.Fatalf("expected material color, got %q", cp.Material.Color)
	}
	if cp.Material.ColorEnd != "#3366ff" {
		t.Fatalf("expected material color end, got %q", cp.Material.ColorEnd)
	}
	if cp.Material.Size != 0.15 {
		t.Fatalf("expected material size, got %v", cp.Material.Size)
	}
	if cp.Material.SizeEnd != 0.02 {
		t.Fatalf("expected material size end, got %v", cp.Material.SizeEnd)
	}
	if cp.Material.Opacity != 1.0 {
		t.Fatalf("expected material opacity, got %v", cp.Material.Opacity)
	}
	if cp.Material.OpacityEnd != 0.0 {
		t.Fatalf("expected material opacity end 0, got %v", cp.Material.OpacityEnd)
	}
	if cp.Material.BlendMode != "additive" {
		t.Fatalf("expected additive blend mode, got %q", cp.Material.BlendMode)
	}
	if !cp.Material.Attenuation {
		t.Fatalf("expected attenuation true")
	}
	if cp.Bounds != 20 {
		t.Fatalf("expected bounds 20, got %v", cp.Bounds)
	}

	// Verify legacy props round-trip.
	legacy := props.LegacyProps()
	sceneValue, ok := legacy["scene"].(map[string]any)
	if !ok {
		t.Fatalf("expected scene map, got %#v", legacy["scene"])
	}
	particles, ok := sceneValue["computeParticles"].([]map[string]any)
	if !ok || len(particles) != 1 {
		t.Fatalf("expected one compute particles record, got %#v", sceneValue["computeParticles"])
	}
	if got := particles[0]["id"]; got != "galaxy" {
		t.Fatalf("expected id in legacy props, got %#v", got)
	}
	emitter, ok := particles[0]["emitter"].(map[string]any)
	if !ok {
		t.Fatalf("expected compute emitter record, got %#v", particles[0]["emitter"])
	}
	if got := emitter["once"]; got != true {
		t.Fatalf("expected compute emitter once in legacy props, got %#v", got)
	}
	material, ok := particles[0]["material"].(map[string]any)
	if !ok {
		t.Fatalf("expected compute material record, got %#v", particles[0]["material"])
	}
	if got := material["style"]; got != "focus" {
		t.Fatalf("expected compute material style focus in legacy props, got %#v", got)
	}
}

func approxMapFloat(value any, want float64) bool {
	got, ok := value.(float64)
	if !ok {
		return false
	}
	return math.Abs(got-want) < 1e-9
}

// TestComputeParticlesPayloadKernelFieldsRoundTrip verifies the full
// scene → IR → legacy-JSON round-trip for the three new kernel-override
// fields (ComputeWGSL, ComputeEntry, ComputeBackend).
func TestComputeParticlesPayloadKernelFieldsRoundTrip(t *testing.T) {
	const wgslSource = "@compute @workgroup_size(64) fn update() {}"
	props := Props{
		Graph: NewGraph(
			ComputeParticles{
				ID:    "elio-galaxy",
				Count: 1000,
				Emitter: ParticleEmitter{
					Kind:     "point",
					Lifetime: 2.0,
				},
				Material: ParticleMaterial{
					Color:   "#ff8800",
					Size:    0.2,
					Opacity: 1.0,
				},
				ComputeWGSL:    wgslSource,
				ComputeEntry:   "update",
				ComputeBackend: "elio",
			},
		),
	}

	// 1. Scene → IR
	ir := props.SceneIR()
	if len(ir.ComputeParticles) != 1 {
		t.Fatalf("expected 1 compute-particles IR entry, got %d", len(ir.ComputeParticles))
	}
	cp := ir.ComputeParticles[0]
	if cp.ComputeWGSL != wgslSource {
		t.Errorf("IR.ComputeWGSL: want %q, got %q", wgslSource, cp.ComputeWGSL)
	}
	if cp.ComputeEntry != "update" {
		t.Errorf("IR.ComputeEntry: want 'update', got %q", cp.ComputeEntry)
	}
	if cp.ComputeBackend != "elio" {
		t.Errorf("IR.ComputeBackend: want 'elio', got %q", cp.ComputeBackend)
	}

	// 2. IR → legacy JSON map (the path the JS browser receives)
	legacy := props.LegacyProps()
	sceneMap, ok := legacy["scene"].(map[string]any)
	if !ok {
		t.Fatalf("expected scene map in legacy props, got %T", legacy["scene"])
	}
	particles, ok := sceneMap["computeParticles"].([]map[string]any)
	if !ok || len(particles) != 1 {
		t.Fatalf("expected one compute-particles legacy record, got %#v", sceneMap["computeParticles"])
	}
	rec := particles[0]
	if got := rec["computeWGSL"]; got != wgslSource {
		t.Errorf("legacy computeWGSL: want %q, got %v", wgslSource, got)
	}
	if got := rec["computeEntry"]; got != "update" {
		t.Errorf("legacy computeEntry: want 'update', got %v", got)
	}
	if got := rec["computeBackend"]; got != "elio" {
		t.Errorf("legacy computeBackend: want 'elio', got %v", got)
	}
}

func TestPropsSceneIRLowersWaterSystem(t *testing.T) {
	const simulation = "@compute @workgroup_size(8, 8) fn simulate() {}"
	const seed = "@compute @workgroup_size(8, 8) fn seedDrops() {}"
	const drop = "@compute @workgroup_size(8, 8) fn addDrop() {}"
	const causticsFragment = "@fragment fn fragmentMain() -> @location(0) vec4f { return vec4f(1.0); }"
	const poolVertex = "@vertex fn vertexMain() -> @builtin(position) vec4f { return vec4f(0.0); }"
	const poolFragment = "@fragment fn fragmentMain() -> @location(0) vec4f { return vec4f(0.5); }"
	const surfaceFragment = "@fragment fn fragmentMain() -> @location(0) vec4f { return vec4f(0.0); }"
	const surfaceBelowFragment = "const WATER_SURFACE_VIEW_BELOW: bool = true; @fragment fn fragmentMain() -> @location(0) vec4f { return vec4f(0.0); }"
	const objectShadowFragment = "@fragment fn shadowMain() -> @location(0) vec4f { return vec4f(0.25); }"
	const objectMeshShadowVertex = "@vertex fn vertexMain() -> @builtin(position) vec4f { return vec4f(0.0); }"
	const objectMeshShadowFragment = "@fragment fn fragmentMain() -> @location(0) vec4f { return vec4f(1.0); }"
	props := Props{
		Graph: NewGraph(
			WaterSystem{
				ID:                          "pool-water",
				InteractionProfile:          "water-object-drop-orbit",
				InteractionTarget:           "water-main",
				InteractionObject:           "Sphere",
				Resolution:                  256,
				SurfaceResolution:           201,
				PoolShape:                   "Rounded Box",
				PoolWidth:                   7.2,
				PoolHeight:                  1.1,
				PoolLength:                  4.4,
				CornerRadius:                0.22,
				SeedDrops:                   20,
				DropRadius:                  0.035,
				DropStrength:                0.012,
				DropEventID:                 3,
				DropX:                       -0.25,
				DropZ:                       0.4,
				DropEventRadius:             0.03,
				DropEventStrength:           0.01,
				TileTexture:                 "/water/tiles.jpg",
				CubeMap:                     "/water/",
				ShallowColor:                "#7ad1eb",
				DeepColor:                   "#082e57",
				AboveWaterColor:             Vec3(0.25, 1, 1.25),
				CausticsResolution:          1024,
				ObjectTextureResolution:     512,
				ObjectTextureResolutionMode: "viewport",
				ObjectTexturePixelBudget:    3145728,
				ObjectShadowResolution:      1024,
				Caustics:                    true,
				Reflection:                  true,
				Refraction:                  true,
				Paused:                      true,
				LightDirection:              Vec3(2, 3, -1),
				ActiveObject:                "Sphere",
				ObjectKind:                  "sphere",
				ObjectX:                     -1.28,
				ObjectY:                     0.22,
				ObjectZ:                     0.1,
				ObjectPreviousSet:           true,
				ObjectPreviousX:             -1.28,
				ObjectPreviousY:             8,
				ObjectPreviousZ:             0.1,
				ObjectRadius:                0.44,
				ObjectDriftX:                0.16,
				ObjectBobAmplitude:          0.08,
				ObjectBobSpeed:              1.55,
				ObjectDisplacementScale:     1,
				ComputeSource:               "water/jeantimex-water.elio",
				MaterialSource:              "water/jeantimex-water.sel",
				ComputeSourceFiles:          map[string]string{"simulationWGSL": "shaders/jeantimex-water.elio/simulation.elio"},
				MaterialSourceFiles:         map[string]string{"surfaceFragmentWGSL": "shaders/jeantimex-water.sel/surface.fragment.sel"},
				ObjectDisplacementSpheres: []WaterDisplacementSphere{
					{Offset: Vec3(0, 0, 0), Radius: 0.15},
					{Offset: Vec3(0.1, 0.05, -0.02), Radius: 0.08},
				},
				ObjectDisplacementEvents: []WaterObjectDisplacementEvent{
					{
						ID:                7,
						ActiveObject:      "Sphere",
						ObjectKind:        "sphere",
						ObjectX:           -1.28,
						ObjectY:           10,
						ObjectZ:           0.1,
						ObjectPreviousSet: true,
						ObjectPreviousX:   -1.28,
						ObjectPreviousY:   0.22,
						ObjectPreviousZ:   0.1,
						ObjectRadius:      0.44,
						ObjectDisplacementSpheres: []WaterDisplacementSphere{
							{Offset: Vec3(0.02, 0, 0), Radius: 0.05},
						},
					},
				},
				SeedWGSL:                     seed,
				DropWGSL:                     drop,
				SimulationWGSL:               simulation,
				CausticsWGSL:                 causticsFragment,
				PoolVertexWGSL:               poolVertex,
				PoolFragmentWGSL:             poolFragment,
				SurfaceFragmentWGSL:          surfaceFragment,
				SurfaceBelowFragmentWGSL:     surfaceBelowFragment,
				ObjectShadowWGSL:             objectShadowFragment,
				ObjectMeshShadowVertexWGSL:   objectMeshShadowVertex,
				ObjectMeshShadowFragmentWGSL: objectMeshShadowFragment,
			},
		),
	}

	ir := props.SceneIR()
	if len(ir.WaterSystems) != 1 {
		t.Fatalf("expected one water system, got %#v", ir.WaterSystems)
	}
	water := ir.WaterSystems[0]
	if water.ID != "pool-water" || water.Resolution != 256 || water.PoolShape != "Rounded Box" {
		t.Fatalf("unexpected water system identity: %+v", water)
	}
	if water.ComputeBackend != "elio" || water.MaterialBackend != "selena" {
		t.Fatalf("expected Elio/Selena defaults, got compute=%q material=%q", water.ComputeBackend, water.MaterialBackend)
	}
	if water.WaveSpeed != 1 || water.Damping != 0.995 || water.NormalScale != 1 {
		t.Fatalf("expected upstream water physics defaults, got waveSpeed=%v damping=%v normalScale=%v", water.WaveSpeed, water.Damping, water.NormalScale)
	}
	if water.ComputeSource != "water/jeantimex-water.elio" || water.MaterialSource != "water/jeantimex-water.sel" {
		t.Fatalf("expected authored water source modules, got compute=%q material=%q", water.ComputeSource, water.MaterialSource)
	}
	if water.ComputeSourceFiles["simulationWGSL"] != "shaders/jeantimex-water.elio/simulation.elio" || water.MaterialSourceFiles["surfaceFragmentWGSL"] != "shaders/jeantimex-water.sel/surface.fragment.sel" {
		t.Fatalf("water source file manifests did not lower: compute=%#v material=%#v", water.ComputeSourceFiles, water.MaterialSourceFiles)
	}
	if water.InteractionProfile != "water-object-drop-orbit" || water.InteractionTarget != "water-main" || water.InteractionObject != "Sphere" {
		t.Fatalf("interaction profile fields did not lower: %+v", water)
	}
	if water.SimulationWGSL != simulation {
		t.Fatalf("simulation WGSL did not round-trip")
	}
	if water.SeedWGSL != seed || water.DropWGSL != drop {
		t.Fatalf("seed/drop WGSL did not round-trip")
	}
	if water.CausticsWGSL != causticsFragment {
		t.Fatalf("caustics WGSL did not round-trip")
	}
	if water.PoolVertexWGSL != poolVertex || water.PoolFragmentWGSL != poolFragment {
		t.Fatalf("pool WGSL did not round-trip")
	}
	if water.SurfaceFragmentWGSL != surfaceFragment || water.SurfaceBelowFragmentWGSL != surfaceBelowFragment {
		t.Fatalf("surface WGSL did not round-trip: above=%q below=%q", water.SurfaceFragmentWGSL, water.SurfaceBelowFragmentWGSL)
	}
	if water.ObjectShadowWGSL != objectShadowFragment || water.ObjectMeshShadowVertexWGSL != objectMeshShadowVertex || water.ObjectMeshShadowFragmentWGSL != objectMeshShadowFragment {
		t.Fatalf("object shadow WGSL did not round-trip")
	}
	if water.DropEventID != 3 || water.DropX != -0.25 || water.DropZ != 0.4 || water.DropEventRadius != 0.03 || water.DropEventStrength != 0.01 {
		t.Fatalf("drop event fields did not lower: %+v", water)
	}
	if water.CubeMap != "/water/" {
		t.Fatalf("cubemap field did not lower: %+v", water)
	}
	if water.Resolution != 256 || water.SurfaceResolution != 201 {
		t.Fatalf("water simulation/surface resolutions did not lower: %+v", water)
	}
	if water.ShallowColor != "#7ad1eb" || water.DeepColor != "#082e57" {
		t.Fatalf("water colors did not lower: shallow=%q deep=%q", water.ShallowColor, water.DeepColor)
	}
	if water.AboveWaterColorR != 0.25 || water.AboveWaterColorG != 1 || water.AboveWaterColorB != 1.25 {
		t.Fatalf("HDR water color did not lower: %+v", water)
	}
	if water.CausticsResolution != 1024 || water.ObjectTextureResolution != 512 || water.ObjectTextureResolutionMode != "viewport" || water.ObjectTexturePixelBudget != 3145728 || water.ObjectShadowResolution != 1024 {
		t.Fatalf("water texture target resolution fields did not lower: %+v", water)
	}
	if !water.Paused {
		t.Fatalf("paused flag did not lower: %+v", water)
	}
	if water.LightDirectionX != 2 || water.LightDirectionY != 3 || water.LightDirectionZ != -1 {
		t.Fatalf("light direction = (%v,%v,%v)", water.LightDirectionX, water.LightDirectionY, water.LightDirectionZ)
	}
	if water.ObjectKind != "sphere" || water.ObjectX != -1.28 || water.ObjectRadius != 0.44 || water.ObjectDisplacementScale != 1 {
		t.Fatalf("object displacement fields did not lower: %+v", water)
	}
	if !water.ObjectPreviousSet || water.ObjectPreviousX != -1.28 || water.ObjectPreviousY != 8 || water.ObjectPreviousZ != 0.1 {
		t.Fatalf("object previous displacement fields did not lower: %+v", water)
	}
	if len(water.ObjectDisplacementSpheres) != 2 {
		t.Fatalf("object displacement spheres length = %d, want 2", len(water.ObjectDisplacementSpheres))
	}
	if water.ObjectDisplacementSpheres[1].OffsetX != 0.1 || water.ObjectDisplacementSpheres[1].OffsetY != 0.05 || water.ObjectDisplacementSpheres[1].OffsetZ != -0.02 || water.ObjectDisplacementSpheres[1].Radius != 0.08 {
		t.Fatalf("object displacement sphere did not lower: %+v", water.ObjectDisplacementSpheres[1])
	}
	if len(water.ObjectDisplacementEvents) != 1 {
		t.Fatalf("object displacement events length = %d, want 1", len(water.ObjectDisplacementEvents))
	}
	event := water.ObjectDisplacementEvents[0]
	if event.ID != 7 || event.ActiveObject != "Sphere" || event.ObjectKind != "sphere" || event.ObjectY != 10 || event.ObjectPreviousY != 0.22 || !event.ObjectPreviousSet {
		t.Fatalf("object displacement event did not lower: %+v", event)
	}
	if len(event.ObjectDisplacementSpheres) != 1 || event.ObjectDisplacementSpheres[0].OffsetX != 0.02 || event.ObjectDisplacementSpheres[0].Radius != 0.05 {
		t.Fatalf("object displacement event spheres did not lower: %+v", event.ObjectDisplacementSpheres)
	}

	legacy := props.LegacyProps()
	sceneValue, ok := legacy["scene"].(map[string]any)
	if !ok {
		t.Fatalf("expected scene map, got %#v", legacy["scene"])
	}
	systems, ok := sceneValue["waterSystems"].([]map[string]any)
	if !ok || len(systems) != 1 {
		t.Fatalf("expected one waterSystems legacy record, got %#v", sceneValue["waterSystems"])
	}
	if got := systems[0]["computeBackend"]; got != "elio" {
		t.Fatalf("legacy computeBackend = %#v, want elio", got)
	}
	if got := systems[0]["materialBackend"]; got != "selena" {
		t.Fatalf("legacy materialBackend = %#v, want selena", got)
	}
	if got := systems[0]["computeSource"]; got != "water/jeantimex-water.elio" {
		t.Fatalf("legacy computeSource = %#v", got)
	}
	if got := systems[0]["materialSource"]; got != "water/jeantimex-water.sel" {
		t.Fatalf("legacy materialSource = %#v", got)
	}
	if got := systems[0]["computeSourceFiles"].(map[string]string)["simulationWGSL"]; got != "shaders/jeantimex-water.elio/simulation.elio" {
		t.Fatalf("legacy computeSourceFiles simulationWGSL = %#v", got)
	}
	if got := systems[0]["materialSourceFiles"].(map[string]string)["surfaceFragmentWGSL"]; got != "shaders/jeantimex-water.sel/surface.fragment.sel" {
		t.Fatalf("legacy materialSourceFiles surfaceFragmentWGSL = %#v", got)
	}
	if got := systems[0]["seedWGSL"]; got != seed {
		t.Fatalf("legacy seedWGSL = %#v", got)
	}
	if got := systems[0]["dropWGSL"]; got != drop {
		t.Fatalf("legacy dropWGSL = %#v", got)
	}
	if got := systems[0]["causticsWGSL"]; got != causticsFragment {
		t.Fatalf("legacy causticsWGSL = %#v", got)
	}
	if got := systems[0]["poolVertexWGSL"]; got != poolVertex {
		t.Fatalf("legacy poolVertexWGSL = %#v", got)
	}
	if got := systems[0]["poolFragmentWGSL"]; got != poolFragment {
		t.Fatalf("legacy poolFragmentWGSL = %#v", got)
	}
	if got := systems[0]["surfaceFragmentWGSL"]; got != surfaceFragment {
		t.Fatalf("legacy surfaceFragmentWGSL = %#v", got)
	}
	if got := systems[0]["surfaceBelowFragmentWGSL"]; got != surfaceBelowFragment {
		t.Fatalf("legacy surfaceBelowFragmentWGSL = %#v", got)
	}
	if got := systems[0]["objectShadowWGSL"]; got != objectShadowFragment {
		t.Fatalf("legacy objectShadowWGSL = %#v", got)
	}
	if got := systems[0]["objectMeshShadowVertexWGSL"]; got != objectMeshShadowVertex {
		t.Fatalf("legacy objectMeshShadowVertexWGSL = %#v", got)
	}
	if got := systems[0]["objectMeshShadowFragmentWGSL"]; got != objectMeshShadowFragment {
		t.Fatalf("legacy objectMeshShadowFragmentWGSL = %#v", got)
	}
	if got := systems[0]["interactionProfile"]; got != "water-object-drop-orbit" {
		t.Fatalf("legacy interactionProfile = %#v, want water-object-drop-orbit", got)
	}
	if got := systems[0]["interactionTarget"]; got != "water-main" {
		t.Fatalf("legacy interactionTarget = %#v, want water-main", got)
	}
	if got := systems[0]["interactionObject"]; got != "Sphere" {
		t.Fatalf("legacy interactionObject = %#v, want Sphere", got)
	}
	if got := systems[0]["paused"]; got != true {
		t.Fatalf("legacy paused = %#v, want true", got)
	}
	if got := systems[0]["objectKind"]; got != "sphere" {
		t.Fatalf("legacy objectKind = %#v, want sphere", got)
	}
	if got := systems[0]["objectRadius"]; got != 0.44 {
		t.Fatalf("legacy objectRadius = %#v, want 0.44", got)
	}
	if got := systems[0]["objectPreviousSet"]; got != true {
		t.Fatalf("legacy objectPreviousSet = %#v, want true", got)
	}
	if got := systems[0]["objectPreviousY"]; got != 8.0 {
		t.Fatalf("legacy objectPreviousY = %#v, want 8", got)
	}
	if got := systems[0]["dropEventID"]; got != 3 {
		t.Fatalf("legacy dropEventID = %#v, want 3", got)
	}
	if got := systems[0]["dropX"]; got != -0.25 {
		t.Fatalf("legacy dropX = %#v, want -0.25", got)
	}
	if got := systems[0]["cubeMap"]; got != "/water/" {
		t.Fatalf("legacy cubeMap = %#v, want /water/", got)
	}
	if got := systems[0]["shallowColor"]; got != "#7ad1eb" {
		t.Fatalf("legacy shallowColor = %#v, want #7ad1eb", got)
	}
	if got := systems[0]["deepColor"]; got != "#082e57" {
		t.Fatalf("legacy deepColor = %#v, want #082e57", got)
	}
	if got := systems[0]["aboveWaterColorB"]; got != 1.25 {
		t.Fatalf("legacy aboveWaterColorB = %#v, want 1.25", got)
	}
	if got := systems[0]["causticsResolution"]; got != 1024 {
		t.Fatalf("legacy causticsResolution = %#v, want 1024", got)
	}
	if got := systems[0]["surfaceResolution"]; got != 201 {
		t.Fatalf("legacy surfaceResolution = %#v, want 201", got)
	}
	if got := systems[0]["objectTextureResolution"]; got != 512 {
		t.Fatalf("legacy objectTextureResolution = %#v, want 512", got)
	}
	if got := systems[0]["objectTextureResolutionMode"]; got != "viewport" {
		t.Fatalf("legacy objectTextureResolutionMode = %#v, want viewport", got)
	}
	if got := systems[0]["objectShadowResolution"]; got != 1024 {
		t.Fatalf("legacy objectShadowResolution = %#v, want 1024", got)
	}
	legacySpheres, ok := systems[0]["objectDisplacementSpheres"].([]map[string]any)
	if !ok || len(legacySpheres) != 2 {
		t.Fatalf("legacy objectDisplacementSpheres = %#v", systems[0]["objectDisplacementSpheres"])
	}
	if got := legacySpheres[1]["radius"]; got != 0.08 {
		t.Fatalf("legacy displacement sphere radius = %#v, want 0.08", got)
	}
	legacyEvents, ok := systems[0]["objectDisplacementEvents"].([]map[string]any)
	if !ok || len(legacyEvents) != 1 {
		t.Fatalf("legacy objectDisplacementEvents = %#v", systems[0]["objectDisplacementEvents"])
	}
	if got := legacyEvents[0]["id"]; got != 7 {
		t.Fatalf("legacy event id = %#v, want 7", got)
	}
	if got := legacyEvents[0]["activeObject"]; got != "Sphere" {
		t.Fatalf("legacy event activeObject = %#v, want Sphere", got)
	}
	if got := legacyEvents[0]["objectY"]; got != 10.0 {
		t.Fatalf("legacy event objectY = %#v, want 10", got)
	}
	if got := legacyEvents[0]["objectPreviousY"]; got != 0.22 {
		t.Fatalf("legacy event objectPreviousY = %#v, want 0.22", got)
	}
	legacyEventSpheres, ok := legacyEvents[0]["objectDisplacementSpheres"].([]map[string]any)
	if !ok || len(legacyEventSpheres) != 1 {
		t.Fatalf("legacy event objectDisplacementSpheres = %#v", legacyEvents[0]["objectDisplacementSpheres"])
	}
	if got := legacyEventSpheres[0]["radius"]; got != 0.05 {
		t.Fatalf("legacy event displacement sphere radius = %#v, want 0.05", got)
	}
}

// TestPropsSceneIRLowersWaterSystemGLSL verifies the additive Selena GLSL/GLES +
// descriptor slots round-trip through WaterSystem -> WaterSystemIR -> JSON, and
// that they are omitted (zero-value path unchanged) when not set. The WGSL slots
// are intentionally not exercised here; they are covered above and unaffected.
func TestPropsSceneIRLowersWaterSystemGLSL(t *testing.T) {
	const surfaceVtxGLSL = "attribute float a_vertexIndex;\nvoid main() { gl_Position = vec4(0.0); }"
	const surfaceFragGLSL = "precision mediump float;\nvoid main() { gl_FragColor = vec4(0.0); }"
	const surfaceVtxGLES = "#version 300 es\nvoid main() { gl_Position = vec4(0.0); }"
	const surfaceFragGLES = "#version 300 es\nprecision mediump float;\nout vec4 o; void main() { o = vec4(0.0); }"
	const seedFragGLSL = "precision highp float;\nvoid main() { gl_FragColor = vec4(1.0); }"
	const compoundShadowVtxGLSL = "attribute vec2 a_position;\nvoid main() { gl_Position = vec4(a_position, 0.0, 1.0); }"
	const compoundShadowFragGLSL = "precision mediump float;\nvoid main() { gl_FragColor = vec4(0.0); }"
	const compoundShadowVtxGLES = "#version 300 es\nvoid main() { gl_Position = vec4(0.0); }"
	const compoundShadowFragGLES = "#version 300 es\nprecision mediump float;\nout vec4 o; void main() { o = vec4(0.0); }"
	descriptor := json.RawMessage(`{"kind":"mesh","textures":[{"name":"sky","dimension":"cube"}],"states":[{"name":"height"}]}`)

	props := Props{
		Graph: NewGraph(
			WaterSystem{
				ID:                         "pool-water-glsl",
				SurfaceVertexGLSL:          surfaceVtxGLSL,
				SurfaceFragmentGLSL:        surfaceFragGLSL,
				SurfaceVertexGLES:          surfaceVtxGLES,
				SurfaceFragmentGLES:        surfaceFragGLES,
				SeedFragmentGLSL:           seedFragGLSL,
				CompoundShadowVertexGLSL:   compoundShadowVtxGLSL,
				CompoundShadowFragmentGLSL: compoundShadowFragGLSL,
				CompoundShadowVertexGLES:   compoundShadowVtxGLES,
				CompoundShadowFragmentGLES: compoundShadowFragGLES,
				ShaderDescriptors:          map[string]json.RawMessage{"surface": descriptor},
			},
		),
	}

	ir := props.SceneIR()
	if len(ir.WaterSystems) != 1 {
		t.Fatalf("expected one water system, got %#v", ir.WaterSystems)
	}
	water := ir.WaterSystems[0]
	if water.SurfaceVertexGLSL != surfaceVtxGLSL || water.SurfaceFragmentGLSL != surfaceFragGLSL {
		t.Fatalf("surface GLSL did not round-trip: vtx=%q frag=%q", water.SurfaceVertexGLSL, water.SurfaceFragmentGLSL)
	}
	if water.SurfaceVertexGLES != surfaceVtxGLES || water.SurfaceFragmentGLES != surfaceFragGLES {
		t.Fatalf("surface GLES did not round-trip: vtx=%q frag=%q", water.SurfaceVertexGLES, water.SurfaceFragmentGLES)
	}
	if water.SeedFragmentGLSL != seedFragGLSL {
		t.Fatalf("seed fragment GLSL did not round-trip: %q", water.SeedFragmentGLSL)
	}
	if water.CompoundShadowVertexGLSL != compoundShadowVtxGLSL || water.CompoundShadowFragmentGLSL != compoundShadowFragGLSL {
		t.Fatalf("compoundShadow GLSL did not round-trip: vtx=%q frag=%q", water.CompoundShadowVertexGLSL, water.CompoundShadowFragmentGLSL)
	}
	if water.CompoundShadowVertexGLES != compoundShadowVtxGLES || water.CompoundShadowFragmentGLES != compoundShadowFragGLES {
		t.Fatalf("compoundShadow GLES did not round-trip: vtx=%q frag=%q", water.CompoundShadowVertexGLES, water.CompoundShadowFragmentGLES)
	}
	if string(water.ShaderDescriptors["surface"]) != string(descriptor) {
		t.Fatalf("surface descriptor did not round-trip: %s", water.ShaderDescriptors["surface"])
	}

	// JSON wire shape uses the camelCase tags the client reads (parallel to *WGSL).
	encoded, err := json.Marshal(water)
	if err != nil {
		t.Fatalf("marshal water IR: %v", err)
	}
	for _, key := range []string{
		`"surfaceVertexGLSL"`, `"surfaceFragmentGLSL"`,
		`"surfaceVertexGLES"`, `"surfaceFragmentGLES"`,
		`"seedFragmentGLSL"`, `"shaderDescriptors"`,
		`"compoundShadowVertexGLSL"`, `"compoundShadowFragmentGLSL"`,
		`"compoundShadowVertexGLES"`, `"compoundShadowFragmentGLES"`,
	} {
		if !strings.Contains(string(encoded), key) {
			t.Fatalf("marshaled water IR missing key %s: %s", key, encoded)
		}
	}

	// Zero-value path: empty GLSL slots and descriptors are omitted entirely.
	emptyIR := (Props{Graph: NewGraph(WaterSystem{ID: "empty-water"})}).SceneIR()
	emptyEncoded, err := json.Marshal(emptyIR.WaterSystems[0])
	if err != nil {
		t.Fatalf("marshal empty water IR: %v", err)
	}
	for _, key := range []string{"GLSL", "GLES", "shaderDescriptors"} {
		if strings.Contains(string(emptyEncoded), key) {
			t.Fatalf("empty water IR should omit %q, got %s", key, emptyEncoded)
		}
	}
}

// TestComputeParticlesKernelFieldsAbsentWhenEmpty ensures that when the new
// kernel fields are empty they are omitted from both the IR and legacy JSON
// (zero-value path unchanged).
func TestComputeParticlesKernelFieldsAbsentWhenEmpty(t *testing.T) {
	props := Props{
		Graph: NewGraph(
			ComputeParticles{
				ID:    "plain-galaxy",
				Count: 100,
				Emitter: ParticleEmitter{
					Kind:     "point",
					Lifetime: 1.0,
				},
				Material: ParticleMaterial{Size: 0.1, Opacity: 1.0},
				// ComputeWGSL, ComputeEntry, ComputeBackend intentionally absent.
			},
		),
	}

	ir := props.SceneIR()
	if len(ir.ComputeParticles) != 1 {
		t.Fatalf("expected 1 compute-particles IR entry, got %d", len(ir.ComputeParticles))
	}
	cp := ir.ComputeParticles[0]
	if cp.ComputeWGSL != "" {
		t.Errorf("expected empty ComputeWGSL, got %q", cp.ComputeWGSL)
	}
	if cp.ComputeEntry != "" {
		t.Errorf("expected empty ComputeEntry, got %q", cp.ComputeEntry)
	}
	if cp.ComputeBackend != "" {
		t.Errorf("expected empty ComputeBackend, got %q", cp.ComputeBackend)
	}

	// Legacy map: keys must be absent (omitempty semantics via setString).
	legacy := props.LegacyProps()
	sceneMap, ok := legacy["scene"].(map[string]any)
	if !ok {
		t.Fatalf("expected scene map")
	}
	particles, ok := sceneMap["computeParticles"].([]map[string]any)
	if !ok || len(particles) != 1 {
		t.Fatalf("expected one compute-particles legacy record, got %#v", sceneMap["computeParticles"])
	}
	rec := particles[0]
	if _, present := rec["computeWGSL"]; present {
		t.Errorf("computeWGSL key must be absent when empty")
	}
	if _, present := rec["computeEntry"]; present {
		t.Errorf("computeEntry key must be absent when empty")
	}
	if _, present := rec["computeBackend"]; present {
		t.Errorf("computeBackend key must be absent when empty")
	}
}

// TestInstancedMeshCullFieldsAbsentWhenEmpty is the Task 6 guard test:
// an InstancedMesh with NO cull fields must emit a payload with none of the
// cull keys present. Proves the additive plumbing is invisible when unused.
func TestInstancedMeshCullFieldsAbsentWhenEmpty(t *testing.T) {
	props := Props{
		Graph: NewGraph(
			InstancedMesh{
				ID:        "plain-instanced",
				Count:     1,
				Geometry:  BoxGeometry{Width: 1, Height: 1, Depth: 1},
				Material:  FlatMaterial{Color: "#ff0000"},
				Positions: []Vector3{Vec3(0, 0, 0)},
				Scales:    []Vector3{Vec3(1, 1, 1)},
				// CullKernelWGSL, CullKernelEntry, CullRadius, CullBackend intentionally absent.
			},
		),
	}

	ir := props.SceneIR()
	if len(ir.InstancedMeshes) != 1 {
		t.Fatalf("expected 1 InstancedMeshIR, got %d", len(ir.InstancedMeshes))
	}
	im := ir.InstancedMeshes[0]
	if im.CullKernelWGSL != "" {
		t.Errorf("CullKernelWGSL must be empty, got %q", im.CullKernelWGSL)
	}
	if im.CullKernelWGSLRef != "" {
		t.Errorf("CullKernelWGSLRef must be empty, got %q", im.CullKernelWGSLRef)
	}
	if im.CullKernelEntry != "" {
		t.Errorf("CullKernelEntry must be empty, got %q", im.CullKernelEntry)
	}
	if im.CullRadius != 0 {
		t.Errorf("CullRadius must be 0, got %v", im.CullRadius)
	}
	if im.CullBackend != "" {
		t.Errorf("CullBackend must be empty, got %q", im.CullBackend)
	}

	// Serialize via SceneIR.MarshalJSON and confirm no cull keys appear.
	data, err := json.Marshal(ir)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	meshes, ok := raw["instancedMeshes"].([]any)
	if !ok || len(meshes) != 1 {
		t.Fatalf("instancedMeshes: len=%d", len(meshes))
	}
	m := meshes[0].(map[string]any)
	for _, key := range []string{"cullKernelWGSL", "cullKernelWGSLRef", "cullKernelEntry", "cullRadius", "cullBackend"} {
		if _, has := m[key]; has {
			t.Errorf("payload must not contain key %q when cull fields are absent", key)
		}
	}
}

func contains(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}

func slicesContainCapability(values []engine.Capability, want engine.Capability) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestPropsSceneIRLowersSpotLight(t *testing.T) {
	props := Props{
		Graph: NewGraph(
			Group{
				Position: Vec3(0, 2, 0),
				Rotation: Rotate(0, 0, math.Pi/2),
				Children: []Node{
					SpotLight{
						ID:             "stage",
						Color:          "#ffe0a0",
						Intensity:      2.5,
						Position:       Vec3(1, 0, 0),
						Direction:      Vec3(0, -1, 0),
						Angle:          math.Pi / 6,
						Penumbra:       0.3,
						Range:          20,
						Decay:          2,
						CastShadow:     true,
						ShadowBias:     0.002,
						ShadowSize:     1024,
						ShadowSoftness: 0.08,
					},
				},
			},
		),
	}

	ir := props.SceneIR()
	if len(ir.Lights) != 1 {
		t.Fatalf("expected one lowered light, got %d: %#v", len(ir.Lights), ir.Lights)
	}
	spot := ir.Lights[0]
	if spot.Kind != "spot" || spot.ID != "stage" {
		t.Fatalf("expected spot light, got %#v", spot)
	}
	// Position (1,0,0) under group at (0,2,0) with pi/2 Z rotation => (0,3,0)
	if math.Abs(spot.X-0) > 1e-9 || math.Abs(spot.Y-3) > 1e-9 {
		t.Fatalf("expected transformed spot position (0,3,0), got (%v,%v,%v)", spot.X, spot.Y, spot.Z)
	}
	// Direction (0,-1,0) rotated by pi/2 about Z => (1,0,0)... actually (-1,0,0)
	// The group rotation is pi/2 about Z: (x,y) -> (-y,x)
	// (0,-1,0) -> (0-(-1), 0, 0) => (1, 0, 0)
	if math.Abs(spot.DirectionX-1) > 1e-9 || math.Abs(spot.DirectionY) > 1e-9 {
		t.Fatalf("expected rotated spot direction (1,0,0), got (%v,%v,%v)", spot.DirectionX, spot.DirectionY, spot.DirectionZ)
	}
	if spot.Angle != math.Pi/6 {
		t.Fatalf("expected angle pi/6, got %v", spot.Angle)
	}
	if spot.Penumbra != 0.3 {
		t.Fatalf("expected penumbra 0.3, got %v", spot.Penumbra)
	}
	if spot.Range != 20 || spot.Decay != 2 {
		t.Fatalf("expected range=20 decay=2, got %v %v", spot.Range, spot.Decay)
	}
	if !spot.CastShadow || spot.ShadowBias != 0.002 || spot.ShadowSize != 1024 || spot.ShadowSoftness != 0.08 {
		t.Fatalf("expected shadow fields, got %#v", spot)
	}

	// Verify legacy round-trip
	legacy := props.LegacyProps()
	sceneValue, ok := legacy["scene"].(map[string]any)
	if !ok {
		t.Fatalf("expected scene map, got %#v", legacy["scene"])
	}
	lights, ok := sceneValue["lights"].([]map[string]any)
	if !ok || len(lights) != 1 {
		t.Fatalf("expected one light in legacy props, got %#v", sceneValue["lights"])
	}
	if lights[0]["kind"] != "spot" {
		t.Fatalf("expected spot kind in legacy, got %#v", lights[0]["kind"])
	}
	if lights[0]["angle"] != math.Pi/6 {
		t.Fatalf("expected angle in legacy, got %#v", lights[0]["angle"])
	}
	if lights[0]["penumbra"] != 0.3 {
		t.Fatalf("expected penumbra in legacy, got %#v", lights[0]["penumbra"])
	}
	if lights[0]["shadowSoftness"] != 0.08 {
		t.Fatalf("expected shadowSoftness in legacy, got %#v", lights[0]["shadowSoftness"])
	}
}

func TestPropsSceneIRLowersHemisphereLight(t *testing.T) {
	props := Props{
		Graph: NewGraph(
			HemisphereLight{
				ID:          "sky",
				SkyColor:    "#87ceeb",
				GroundColor: "#362312",
				Intensity:   0.6,
			},
		),
	}

	ir := props.SceneIR()
	if len(ir.Lights) != 1 {
		t.Fatalf("expected one lowered light, got %d: %#v", len(ir.Lights), ir.Lights)
	}
	hemi := ir.Lights[0]
	if hemi.Kind != "hemisphere" || hemi.ID != "sky" {
		t.Fatalf("expected hemisphere light, got %#v", hemi)
	}
	if hemi.Color != "#87ceeb" {
		t.Fatalf("expected sky color in Color field, got %q", hemi.Color)
	}
	if hemi.GroundColor != "#362312" {
		t.Fatalf("expected ground color, got %q", hemi.GroundColor)
	}
	if hemi.Intensity != 0.6 {
		t.Fatalf("expected intensity 0.6, got %v", hemi.Intensity)
	}

	// Verify legacy round-trip
	legacy := props.LegacyProps()
	sceneValue, ok := legacy["scene"].(map[string]any)
	if !ok {
		t.Fatalf("expected scene map, got %#v", legacy["scene"])
	}
	lights, ok := sceneValue["lights"].([]map[string]any)
	if !ok || len(lights) != 1 {
		t.Fatalf("expected one light in legacy props, got %#v", sceneValue["lights"])
	}
	if lights[0]["kind"] != "hemisphere" {
		t.Fatalf("expected hemisphere kind in legacy, got %#v", lights[0]["kind"])
	}
	if lights[0]["groundColor"] != "#362312" {
		t.Fatalf("expected groundColor in legacy, got %#v", lights[0]["groundColor"])
	}
}

func TestComputeParticlesEmitterRotationReachesLegacyProps(t *testing.T) {
	props := Props{
		Graph: NewGraph(ComputeParticles{
			ID:    "test-spiral",
			Count: 100,
			Emitter: ParticleEmitter{
				Kind:     "spiral",
				Position: Vec3(100, 50, -600),
				Rotation: Euler{X: -0.5, Z: 0.3},
				Radius:   200,
				Rate:     10,
				Lifetime: 5,
				Arms:     2,
				Wind:     0.04,
				Scatter:  0.3,
			},
		}),
	}

	ir := props.Graph.SceneIR()
	if len(ir.ComputeParticles) != 1 {
		t.Fatalf("expected 1 compute particles entry, got %d", len(ir.ComputeParticles))
	}

	cp := ir.ComputeParticles[0]
	// Verify rotation survived the Euler→Quaternion→Euler roundtrip
	if math.Abs(cp.Emitter.RotationX-(-0.5)) > 0.001 {
		t.Errorf("emitter RotationX = %f, want -0.5", cp.Emitter.RotationX)
	}
	if math.Abs(cp.Emitter.RotationZ-0.3) > 0.001 {
		t.Errorf("emitter RotationZ = %f, want 0.3", cp.Emitter.RotationZ)
	}

	// Verify legacyProps serialization includes rotation
	legacy := cp.legacyProps()
	emitter, ok := legacy["emitter"].(map[string]any)
	if !ok {
		t.Fatal("emitter not found in legacy props")
	}

	rotX, _ := emitter["rotationX"].(float64)
	rotZ, _ := emitter["rotationZ"].(float64)
	if math.Abs(rotX-(-0.5)) > 0.001 {
		t.Errorf("legacyProps rotationX = %f, want -0.5", rotX)
	}
	if math.Abs(rotZ-0.3) > 0.001 {
		t.Errorf("legacyProps rotationZ = %f, want 0.3", rotZ)
	}

	// Verify JSON roundtrip
	jsonBytes, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	jsonStr := string(jsonBytes)
	if !strings.Contains(jsonStr, `"rotationX"`) {
		t.Errorf("JSON missing rotationX: %s", jsonStr[:200])
	}
	if !strings.Contains(jsonStr, `"rotationZ"`) {
		t.Errorf("JSON missing rotationZ: %s", jsonStr[:200])
	}
	t.Logf("Emitter rotation in JSON: rotX=%f rotZ=%f", rotX, rotZ)
}

func TestPositionStrideReachesLegacyProps(t *testing.T) {
	props := Props{
		Compression: &Compression{BitWidth: 6},
		Graph: NewGraph(Points{
			ID:    "test-points",
			Count: 10,
			Positions: []Vector3{
				Vec3(1, 0, 0), Vec3(2, 0, 0), Vec3(3, 0, 0), Vec3(4, 0, 0), Vec3(5, 0, 0),
				Vec3(6, 0, 0), Vec3(7, 0, 0), Vec3(8, 0, 0), Vec3(9, 0, 0), Vec3(10, 0, 0),
			},
		}),
	}

	ir := props.Graph.SceneIR()
	if len(ir.Points) != 1 {
		t.Fatalf("expected 1 points entry, got %d", len(ir.Points))
	}

	// Compress the IR
	compressSceneIR(&ir, 6, 3)

	pt := ir.Points[0]
	if pt.PositionStride != 3 {
		t.Errorf("PositionStride = %d, want 3", pt.PositionStride)
	}

	legacy := pt.legacyProps()
	stride, _ := legacy["positionStride"].(int)
	if stride != 3 {
		t.Errorf("legacyProps positionStride = %d, want 3", stride)
	}
	t.Logf("PositionStride in legacy props: %d", stride)
}

func TestShadowMaxPixelsConstants(t *testing.T) {
	if ShadowMaxPixels512 != 262144 {
		t.Errorf("ShadowMaxPixels512 = %d, want 262144", ShadowMaxPixels512)
	}
	if ShadowMaxPixels1024 != 1048576 {
		t.Errorf("ShadowMaxPixels1024 = %d, want 1048576", ShadowMaxPixels1024)
	}
	if ShadowMaxPixels2048 != 4194304 {
		t.Errorf("ShadowMaxPixels2048 = %d, want 4194304", ShadowMaxPixels2048)
	}
	if ShadowMaxPixels4096 != 16777216 {
		t.Errorf("ShadowMaxPixels4096 = %d, want 16777216", ShadowMaxPixels4096)
	}
	if ShadowMaxPixelsUnbounded != 1073741824 {
		t.Errorf("ShadowMaxPixelsUnbounded = %d, want 1073741824", ShadowMaxPixelsUnbounded)
	}
}

func TestHTMLTextureMaxPixelsConstants(t *testing.T) {
	if HTMLTextureMaxPixels512 != 262144 {
		t.Errorf("HTMLTextureMaxPixels512 = %d, want 262144", HTMLTextureMaxPixels512)
	}
	if HTMLTextureMaxPixels1024 != 1048576 {
		t.Errorf("HTMLTextureMaxPixels1024 = %d, want 1048576", HTMLTextureMaxPixels1024)
	}
	if HTMLTextureMaxPixels2048 != 4194304 {
		t.Errorf("HTMLTextureMaxPixels2048 = %d, want 4194304", HTMLTextureMaxPixels2048)
	}
	if HTMLTextureMaxPixelsUnbounded != 1073741824 {
		t.Errorf("HTMLTextureMaxPixelsUnbounded = %d, want 1073741824", HTMLTextureMaxPixelsUnbounded)
	}
}

func TestShadowsResolveMaxPixels(t *testing.T) {
	tests := []struct {
		name string
		in   int
		want int
	}{
		{"zero maps to 1024² default", 0, ShadowMaxPixels1024},
		{"negative maps to 1024² default", -1, ShadowMaxPixels1024},
		{"positive passes through", ShadowMaxPixels2048, ShadowMaxPixels2048},
		{"unbounded passes through", ShadowMaxPixelsUnbounded, ShadowMaxPixelsUnbounded},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Shadows{MaxPixels: tc.in}.resolveMaxPixels()
			if got != tc.want {
				t.Errorf("resolveMaxPixels(%d) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

func TestShadowMaxPixelsIRExplicit(t *testing.T) {
	props := Props{
		Shadows: Shadows{MaxPixels: ShadowMaxPixels2048},
		Graph: NewGraph(
			DirectionalLight{
				ID:         "sun",
				Color:      "#ffffff",
				Intensity:  1.0,
				CastShadow: true,
				ShadowSize: 4096,
			},
		),
	}
	ir := props.SceneIR()
	if ir.ShadowMaxPixels != ShadowMaxPixels2048 {
		t.Errorf("ShadowMaxPixels = %d, want %d", ir.ShadowMaxPixels, ShadowMaxPixels2048)
	}
	bag := ir.legacyProps()
	got, ok := bag["shadowMaxPixels"]
	if !ok {
		t.Fatalf("expected shadowMaxPixels in legacy props, got %v", bag)
	}
	if got != ShadowMaxPixels2048 {
		t.Errorf("legacy shadowMaxPixels = %v, want %d", got, ShadowMaxPixels2048)
	}
}

func TestShadowMaxPixelsIRDefault(t *testing.T) {
	props := Props{
		Graph: NewGraph(
			DirectionalLight{
				ID:         "sun",
				Color:      "#ffffff",
				Intensity:  1.0,
				CastShadow: true,
				ShadowSize: 1024,
			},
		),
	}
	ir := props.SceneIR()
	if ir.ShadowMaxPixels != ShadowMaxPixels1024 {
		t.Errorf("default ShadowMaxPixels = %d, want %d (1024²)", ir.ShadowMaxPixels, ShadowMaxPixels1024)
	}
}

func TestShadowMaxPixelsIRUnbounded(t *testing.T) {
	props := Props{
		Shadows: Shadows{MaxPixels: ShadowMaxPixelsUnbounded},
		Graph: NewGraph(
			DirectionalLight{
				ID:         "sun",
				Color:      "#ffffff",
				Intensity:  1.0,
				CastShadow: true,
				ShadowSize: 4096,
			},
		),
	}
	ir := props.SceneIR()
	if ir.ShadowMaxPixels != ShadowMaxPixelsUnbounded {
		t.Errorf("unbounded ShadowMaxPixels = %d, want %d", ir.ShadowMaxPixels, ShadowMaxPixelsUnbounded)
	}
}

func TestLinesGeometryWidthReachesObjectIR(t *testing.T) {
	// LinesGeometry.Width should flow through legacyGeometry into the
	// lowered ObjectIR.LineWidth field so renderers (canvas fallback today,
	// WebGL thick-line shader later) can honor the caller's requested width.
	props := Props{
		Graph: NewGraph(
			Mesh{
				ID: "lightning",
				Geometry: LinesGeometry{
					Points:   []Vector3{Vec3(0, 0, 0), Vec3(1, 1, 0), Vec3(2, 0, 0)},
					Segments: [][2]int{{0, 1}, {1, 2}},
					Width:    3.5,
				},
				Material: FlatMaterial{Color: "#8de1ff"},
			},
		),
	}
	ir := props.SceneIR()
	if len(ir.Objects) != 1 {
		t.Fatalf("expected 1 object, got %d", len(ir.Objects))
	}
	obj := ir.Objects[0]
	if obj.Kind != "lines" {
		t.Fatalf("expected lines kind, got %q", obj.Kind)
	}
	if math.Abs(obj.LineWidth-3.5) > 1e-9 {
		t.Errorf("expected obj.LineWidth = 3.5, got %v", obj.LineWidth)
	}
	// Legacy props round-trip: the lineWidth should be emitted so the JS
	// runtime can normalize it onto the scene object.
	legacy := ir.legacyProps()
	objects, ok := legacy["objects"].([]map[string]any)
	if !ok {
		t.Fatalf("expected legacy objects slice, got %T", legacy["objects"])
	}
	if len(objects) != 1 {
		t.Fatalf("expected 1 legacy object, got %d", len(objects))
	}
	gotWidth, present := objects[0]["lineWidth"]
	if !present {
		t.Fatalf("expected lineWidth key in legacy props, got %v", objects[0])
	}
	if diff := math.Abs(gotWidth.(float64) - 3.5); diff > 1e-9 {
		t.Errorf("legacy lineWidth = %v, want 3.5", gotWidth)
	}
}

func TestLinesGeometryWidthZeroOmitsProp(t *testing.T) {
	// The default (zero) width should not emit a lineWidth prop — the JS
	// runtime falls back to the canvas default (1.8px) when the key is absent.
	props := Props{
		Graph: NewGraph(
			Mesh{
				Geometry: LinesGeometry{
					Points:   []Vector3{Vec3(0, 0, 0), Vec3(1, 0, 0)},
					Segments: [][2]int{{0, 1}},
				},
				Material: FlatMaterial{Color: "#8de1ff"},
			},
		),
	}
	ir := props.SceneIR()
	if obj := ir.Objects[0]; obj.LineWidth != 0 {
		t.Errorf("expected zero LineWidth, got %v", obj.LineWidth)
	}
	legacy := ir.legacyProps()
	objects := legacy["objects"].([]map[string]any)
	if _, present := objects[0]["lineWidth"]; present {
		t.Errorf("expected lineWidth absent when Width is zero, got %v", objects[0])
	}
}

func TestOrthographicCameraReachesLegacyAndCanonicalIR(t *testing.T) {
	camera := OrthographicCamera{
		Position:     Vec3(1, 2, 8),
		Left:         -4,
		Right:        4,
		Top:          3,
		Bottom:       -3,
		Zoom:         1.5,
		Near:         0.2,
		Far:          90,
		TransitionMS: 250,
	}
	props := Props{OrthographicCamera: &camera}
	legacy := props.LegacyProps()
	gotCamera, ok := legacy["camera"].(map[string]any)
	if !ok {
		t.Fatalf("expected legacy camera map, got %#v", legacy["camera"])
	}
	if gotCamera["kind"] != "orthographic" {
		t.Fatalf("expected orthographic legacy camera, got %#v", gotCamera)
	}
	if gotCamera["left"] != -4.0 || gotCamera["right"] != 4.0 || gotCamera["zoom"] != 1.5 {
		t.Fatalf("expected orthographic bounds in legacy camera, got %#v", gotCamera)
	}
	ir := props.CanonicalIR()
	if ir.Camera.Kind != "orthographic" {
		t.Fatalf("expected orthographic canonical camera, got %#v", ir.Camera)
	}
	if ir.Camera.Left != -4 || ir.Camera.Right != 4 || ir.Camera.Top != 3 || ir.Camera.Bottom != -3 || ir.Camera.Zoom != 1.5 {
		t.Fatalf("expected orthographic canonical bounds, got %#v", ir.Camera)
	}
}

func TestScene3DHelperNodesLowerToLineObjects(t *testing.T) {
	props := Props{
		Graph: NewGraph(
			AxesHelper{ID: "axes", Size: 2},
			GridHelper{ID: "grid", Size: 4, Divisions: 2},
			BoxHelper{ID: "box", Width: 2, Height: 3, Depth: 4},
			BoundingBoxHelper{ID: "bounds", Min: Vec3(-1, -2, -3), Max: Vec3(1, 2, 3)},
			SkeletonHelper{ID: "skeleton", Joints: []Vector3{Vec3(0, 0, 0), Vec3(0, 1, 0)}, Bones: [][2]int{{0, 1}}},
			TransformControls{ID: "gizmo", Mode: "rotate", Size: 1},
		),
	}
	ir := props.SceneIR()
	if len(ir.Objects) < 9 {
		t.Fatalf("expected helpers to lower to line objects, got %d: %#v", len(ir.Objects), ir.Objects)
	}
	for _, object := range ir.Objects {
		if object.Kind != "lines" {
			t.Fatalf("expected helper object %q to be lines, got %q", object.ID, object.Kind)
		}
		if object.MaterialKind != "line-basic" {
			t.Fatalf("expected helper object %q line-basic material, got %q", object.ID, object.MaterialKind)
		}
	}
}

// TestScene3DTransformControlsRingAlwaysLoweredWithGizmoModeVisibility verifies
// the P6/gizmoInputSignal precondition: the rotate-mode ring helper must be
// lowered into the graph regardless of the *initial* TransformControls.Mode,
// carrying GizmoRing so the client can flip its visibility live off
// Props.GizmoInputSignal without a full scene re-render. Its baked Visible
// value should still match the initial mode for clients that never subscribe
// to the signal.
func TestScene3DTransformControlsRingAlwaysLoweredWithGizmoModeVisibility(t *testing.T) {
	findRing := func(ir SceneIR) (ObjectIR, bool) {
		for _, object := range ir.Objects {
			if strings.HasSuffix(object.ID, "-ring") {
				return object, true
			}
		}
		return ObjectIR{}, false
	}

	translateProps := Props{
		Graph: NewGraph(TransformControls{ID: "gizmo", Mode: "translate", Size: 1}),
	}
	ring, ok := findRing(translateProps.SceneIR())
	if !ok {
		t.Fatalf("expected a ring helper object to be lowered even in translate mode")
	}
	if !ring.GizmoRing {
		t.Fatalf("expected ring object to carry GizmoRing=true, got %#v", ring)
	}
	if ring.Visible == nil || *ring.Visible {
		t.Fatalf("expected ring to be baked hidden in translate mode, got %#v", ring.Visible)
	}

	scaleProps := Props{
		Graph: NewGraph(TransformControls{ID: "gizmo", Mode: "scale", Size: 1}),
	}
	ring, ok = findRing(scaleProps.SceneIR())
	if !ok {
		t.Fatalf("expected a ring helper object to be lowered even in scale mode")
	}
	if ring.Visible == nil || *ring.Visible {
		t.Fatalf("expected ring to be baked hidden in scale mode, got %#v", ring.Visible)
	}

	rotateProps := Props{
		Graph: NewGraph(TransformControls{ID: "gizmo", Mode: "rotate", Size: 1}),
	}
	ring, ok = findRing(rotateProps.SceneIR())
	if !ok {
		t.Fatalf("expected a ring helper object to be lowered in rotate mode")
	}
	if ring.Visible == nil || !*ring.Visible {
		t.Fatalf("expected ring to be baked visible in rotate mode, got %#v", ring.Visible)
	}
	if !ring.GizmoRing {
		t.Fatalf("expected ring object to carry GizmoRing=true, got %#v", ring)
	}
}

// TestScene3DTransformControlsAlwaysLowersAllThreeForms verifies the P7
// click-driven-reactivity precondition: every TransformControls form
// (translate axes triad, rotate ring, scale handle cubes) is lowered
// regardless of the initial Mode, each tagged GizmoHelper=true with the
// GizmoFormMode identifying which form it is — so a client-side selection +
// gizmo-mode signal sink can hide/reposition/switch-form the whole group
// live with no page navigation (see gosx's syncMountedSceneGizmoHelpers in
// 20-scene-mount.js and kiln's editor_viewport.go sceneHelperNodes).
func TestScene3DTransformControlsAlwaysLowersAllThreeForms(t *testing.T) {
	countByFormMode := func(ir SceneIR) map[string]int {
		counts := map[string]int{}
		for _, object := range ir.Objects {
			if !strings.HasPrefix(object.ID, "gizmo") {
				continue
			}
			if !object.GizmoHelper {
				t.Fatalf("expected TransformControls-lowered object %q to carry GizmoHelper=true, got %#v", object.ID, object)
			}
			if object.GizmoFormMode == "" {
				t.Fatalf("expected TransformControls-lowered object %q to carry a non-empty GizmoFormMode, got %#v", object.ID, object)
			}
			counts[object.GizmoFormMode]++
		}
		return counts
	}

	props := Props{
		Graph: NewGraph(TransformControls{ID: "gizmo", Mode: "translate", Size: 1}),
	}
	counts := countByFormMode(props.SceneIR())
	if counts["translate"] != 3 {
		t.Fatalf("expected 3 translate-form objects (axes triad), got %d: %#v", counts["translate"], counts)
	}
	if counts["rotate"] != 1 {
		t.Fatalf("expected 1 rotate-form object (ring), got %d: %#v", counts["rotate"], counts)
	}
	if counts["scale"] != 3 {
		t.Fatalf("expected 3 scale-form objects (handle cubes), got %d: %#v", counts["scale"], counts)
	}
}

// TestScene3DTransformControlsEmptyModeBakesEveryFormHidden verifies kiln's
// "no initial selection" click-driven case: an empty Mode (control.Target
// also empty) bakes every one of the three forms hidden for the first
// frame, so the client's syncMountedSceneGizmoHelpers sink starts from a
// fully-hidden group until a selection signal arrives.
func TestScene3DTransformControlsEmptyModeBakesEveryFormHidden(t *testing.T) {
	props := Props{
		Graph: NewGraph(TransformControls{ID: "gizmo", Size: 1}),
	}
	ir := props.SceneIR()
	found := 0
	for _, object := range ir.Objects {
		if !strings.HasPrefix(object.ID, "gizmo") {
			continue
		}
		found++
		if object.Visible == nil || *object.Visible {
			t.Fatalf("expected object %q to be baked hidden when Mode is empty, got %#v", object.ID, object.Visible)
		}
	}
	if found != 7 {
		t.Fatalf("expected all 7 TransformControls-lowered objects (3 translate + 1 rotate + 3 scale), got %d", found)
	}
}

func TestScene3DLineCustomAndOutlineMaterialsReachObjectIR(t *testing.T) {
	props := Props{
		Graph: NewGraph(
			Mesh{
				ID: "path",
				Geometry: LinesGeometry{
					Points:   []Vector3{Vec3(0, 0, 0), Vec3(1, 0, 0)},
					Segments: [][2]int{{0, 1}},
				},
				Material: LineDashedMaterial{
					MaterialStyle: MaterialStyle{Color: "#ffffff"},
					Width:         2,
					DashSize:      4,
					GapSize:       2,
				},
			},
			Mesh{
				ID:           "shader-box",
				Geometry:     BoxGeometry{Width: 1, Height: 1, Depth: 1},
				Selected:     true,
				OutlineColor: "#ffcc00",
				OutlineWidth: 4,
				Material: CustomMaterial{
					StandardMaterial: StandardMaterial{Color: "#8de1ff", Roughness: 0.4},
					FragmentGLSL:     "color.rgb = vec3(1.0, 0.0, 0.0);",
					Uniforms:         map[string]any{"u_gain": 0.75},
				},
			},
		),
	}
	ir := props.SceneIR()
	if len(ir.Objects) != 2 {
		t.Fatalf("expected two objects, got %#v", ir.Objects)
	}
	line := ir.Objects[0]
	if line.MaterialKind != "line-dashed" || line.LineDash == nil || !*line.LineDash {
		t.Fatalf("expected dashed line material, got %#v", line)
	}
	if line.LineWidth != 2 || line.DashSize != 4 || line.GapSize != 2 {
		t.Fatalf("expected dashed line sizing fields, got %#v", line)
	}
	custom := ir.Objects[1]
	if custom.MaterialKind != "custom" || custom.CustomFragment == "" {
		t.Fatalf("expected custom shader material, got %#v", custom)
	}
	if custom.CustomUniforms["u_gain"] != 0.75 {
		t.Fatalf("expected custom uniform to round trip, got %#v", custom.CustomUniforms)
	}
	if !custom.Selected || custom.OutlineColor != "#ffcc00" || custom.OutlineWidth != 4 {
		t.Fatalf("expected selected outline fields, got %#v", custom)
	}
}

func TestPerspectiveCameraTransitionMSLegacyProps(t *testing.T) {
	camera := PerspectiveCamera{
		Position:     Vec3(0, 6, 8),
		FOV:          45,
		Near:         0.1,
		Far:          40,
		TransitionMS: 600,
	}
	out := camera.legacyProps()
	if got, ok := out["transitionMS"]; !ok || got != 600.0 {
		t.Fatalf("expected transitionMS 600, got %#v", out["transitionMS"])
	}

	// Zero TransitionMS must be omitted.
	camera.TransitionMS = 0
	out2 := camera.legacyProps()
	if _, present := out2["transitionMS"]; present {
		t.Fatalf("expected transitionMS absent when zero, got %#v", out2)
	}
}

func TestPerspectiveCameraTransitionMSInSceneProps(t *testing.T) {
	props := Props{
		Camera: PerspectiveCamera{
			Position:     Vec3(0, 12, 8),
			FOV:          45,
			Near:         0.1,
			Far:          40,
			TransitionMS: 600,
		},
	}
	legacy := props.LegacyProps()
	camera, ok := legacy["camera"].(map[string]any)
	if !ok {
		t.Fatalf("expected camera map in legacy props, got %#v", legacy["camera"])
	}
	if got, present := camera["transitionMS"]; !present || got != 600.0 {
		t.Fatalf("expected camera.transitionMS 600, got %#v", camera["transitionMS"])
	}
}

func TestInstancedGLBMeshLowersToSceneIR(t *testing.T) {
	props := Props{
		Camera: PerspectiveCamera{Position: Vec3(0, 6, 8), FOV: 45, Near: 0.1, Far: 40},
		Graph: NewGraph(
			InstancedGLBMesh{
				ID:       "robot-batch",
				Src:      "/models/robot-scout.glb",
				Material: StandardMaterial{Color: "#ff6600", Roughness: 0.5},
				Instances: []MeshInstance{
					{ID: "robot-1", Position: Vec3(1, 0, 2), Scale: Vec3(1, 1, 1)},
					{ID: "robot-2", Position: Vec3(3, 0, 4), Scale: Vec3(1.2, 1.2, 1.2)},
				},
				Pickable: Bool(true),
				Static:   Bool(false),
			},
		),
	}

	ir := props.SceneIR()
	if len(ir.InstancedGLBMeshes) != 1 {
		t.Fatalf("expected 1 InstancedGLBMesh, got %d", len(ir.InstancedGLBMeshes))
	}
	batch := ir.InstancedGLBMeshes[0]
	if batch.ID != "robot-batch" {
		t.Fatalf("expected id robot-batch, got %q", batch.ID)
	}
	if batch.Src != "/models/robot-scout.glb" {
		t.Fatalf("expected src /models/robot-scout.glb, got %q", batch.Src)
	}
	if len(batch.Instances) != 2 {
		t.Fatalf("expected 2 instances, got %d", len(batch.Instances))
	}
	if batch.Instances[0].ID != "robot-1" {
		t.Fatalf("expected first instance id robot-1, got %q", batch.Instances[0].ID)
	}
	if batch.Instances[1].ID != "robot-2" {
		t.Fatalf("expected second instance id robot-2, got %q", batch.Instances[1].ID)
	}
	if batch.Pickable == nil || !*batch.Pickable {
		t.Fatalf("expected pickable=true, got %v", batch.Pickable)
	}
	if batch.Color != "#ff6600" {
		t.Fatalf("expected color #ff6600, got %q", batch.Color)
	}

	// Verify legacyProps wire shape.
	legacy := ir.legacyProps()
	batches, ok := legacy["instancedGLBMeshes"].([]map[string]any)
	if !ok || len(batches) != 1 {
		t.Fatalf("expected instancedGLBMeshes slice of length 1, got %#v", legacy["instancedGLBMeshes"])
	}
	batchMap := batches[0]
	if got := batchMap["id"]; got != "robot-batch" {
		t.Fatalf("expected batch id robot-batch, got %#v", got)
	}
	if got := batchMap["src"]; got != "/models/robot-scout.glb" {
		t.Fatalf("expected batch src, got %#v", got)
	}
	instances, ok := batchMap["instances"].([]map[string]any)
	if !ok || len(instances) != 2 {
		t.Fatalf("expected 2 instance maps, got %#v", batchMap["instances"])
	}
}

// TestInstancedMeshCullFieldsLowering verifies that cull fields on scene.InstancedMesh
// are carried through lowering to InstancedMeshIR, and that a mesh without cull fields
// lowers to an IR with all cull fields empty (additive contract).
func TestInstancedMeshCullFieldsLowering(t *testing.T) {
	positions := []Vector3{Vec3(0, 0, 0)}
	scales := []Vector3{Vec3(1, 1, 1)}

	props := Props{
		Graph: NewGraph(
			InstancedMesh{
				ID:              "meteor-ring",
				Count:           1,
				Geometry:        SphereGeometry{Radius: 0.1},
				Material:        FlatMaterial{Color: "#aabbcc"},
				Positions:       positions,
				Scales:          scales,
				CullKernelWGSL:  "@compute @workgroup_size(64) fn cull() {}",
				CullKernelEntry: "cullInstances",
				CullRadius:      100.0,
				CullBackend:     "elio",
			},
			InstancedMesh{
				ID:        "plain-mesh",
				Count:     1,
				Geometry:  BoxGeometry{Width: 1, Height: 1, Depth: 1},
				Material:  FlatMaterial{Color: "#ffffff"},
				Positions: positions,
				Scales:    scales,
			},
		),
	}

	ir := props.SceneIR()
	if len(ir.InstancedMeshes) != 2 {
		t.Fatalf("expected 2 InstancedMeshes, got %d", len(ir.InstancedMeshes))
	}

	// First mesh: cull fields must be carried through.
	im0 := ir.InstancedMeshes[0]
	if im0.CullKernelWGSL != "@compute @workgroup_size(64) fn cull() {}" {
		t.Errorf("CullKernelWGSL not lowered: %q", im0.CullKernelWGSL)
	}
	if im0.CullKernelEntry != "cullInstances" {
		t.Errorf("CullKernelEntry not lowered: %q", im0.CullKernelEntry)
	}
	if im0.CullRadius != 100.0 {
		t.Errorf("CullRadius not lowered: %v", im0.CullRadius)
	}
	if im0.CullBackend != "elio" {
		t.Errorf("CullBackend not lowered: %q", im0.CullBackend)
	}

	// Second mesh: all cull fields must be zero/empty.
	im1 := ir.InstancedMeshes[1]
	if im1.CullKernelWGSL != "" {
		t.Errorf("plain mesh CullKernelWGSL must be empty, got %q", im1.CullKernelWGSL)
	}
	if im1.CullKernelEntry != "" {
		t.Errorf("plain mesh CullKernelEntry must be empty, got %q", im1.CullKernelEntry)
	}
	if im1.CullRadius != 0 {
		t.Errorf("plain mesh CullRadius must be 0, got %v", im1.CullRadius)
	}
	if im1.CullBackend != "" {
		t.Errorf("plain mesh CullBackend must be empty, got %q", im1.CullBackend)
	}

	// Verify the payload (legacyProps path) emits cull fields for the first mesh.
	data, err := json.Marshal(ir)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	meshes, ok := raw["instancedMeshes"].([]any)
	if !ok || len(meshes) != 2 {
		t.Fatalf("instancedMeshes in payload: len=%d", len(meshes))
	}
	pm0 := meshes[0].(map[string]any)
	if pm0["cullKernelEntry"] != "cullInstances" {
		t.Errorf("payload mesh[0] cullKernelEntry: %v", pm0["cullKernelEntry"])
	}
	pm1 := meshes[1].(map[string]any)
	for _, key := range []string{"cullKernelWGSL", "cullKernelWGSLRef", "cullKernelEntry", "cullRadius", "cullBackend"} {
		if _, has := pm1[key]; has {
			t.Errorf("payload mesh[1] (plain) must not have %q", key)
		}
	}
}

// TestInstancedMeshSpreadProps verifies that InstancedMesh.SpreadProps returns
// a map carrying all cull fields and the instance count, mirroring the
// legacyProps path used by the full scene IR. This is the Task 1 guard.
func TestInstancedMeshSpreadProps(t *testing.T) {
	im := InstancedMesh{
		ID:              "meteor-ring",
		Count:           3,
		Geometry:        BoxGeometry{Width: 0.5, Height: 0.3, Depth: 0.4},
		Material:        FlatMaterial{Color: "#e0b0ff"},
		Positions:       []Vector3{Vec3(10, 0, 0), Vec3(-10, 0, 0), Vec3(0, 0, 10)},
		Scales:          []Vector3{Vec3(1, 1, 1), Vec3(1, 1, 1), Vec3(1, 1, 1)},
		CullKernelWGSL:  "@compute @workgroup_size(64) fn main() {}",
		CullKernelEntry: "main",
		CullRadius:      150.0,
		CullBackend:     "elio",
	}

	props := im.SpreadProps()
	if props == nil {
		t.Fatal("SpreadProps returned nil")
	}

	// Count.
	if got := props["count"]; got != 3 {
		t.Errorf("count = %v, want 3", got)
	}
	// Cull fields.
	if got, _ := props["cullKernelWGSL"].(string); got != "@compute @workgroup_size(64) fn main() {}" {
		t.Errorf("cullKernelWGSL = %q", got)
	}
	if got, _ := props["cullKernelEntry"].(string); got != "main" {
		t.Errorf("cullKernelEntry = %q", got)
	}
	if got, _ := props["cullRadius"].(float64); got != 150.0 {
		t.Errorf("cullRadius = %v, want 150", got)
	}
	if got, _ := props["cullBackend"].(string); got != "elio" {
		t.Errorf("cullBackend = %q, want elio", got)
	}
	// Transforms must be present (count*16 float64).
	transforms, ok := props["transforms"].([]float64)
	if !ok || len(transforms) != 3*16 {
		t.Errorf("transforms len = %d, want 48", len(transforms))
	}
}

// TestInstancedMeshSpreadPropsNoCullFields verifies that an InstancedMesh with
// no cull fields produces a map with no cull keys (additive baseline contract).
func TestInstancedMeshSpreadPropsNoCullFields(t *testing.T) {
	im := InstancedMesh{
		ID:        "plain",
		Count:     1,
		Geometry:  BoxGeometry{Width: 1, Height: 1, Depth: 1},
		Material:  FlatMaterial{Color: "#ffffff"},
		Positions: []Vector3{Vec3(0, 0, 0)},
		Scales:    []Vector3{Vec3(1, 1, 1)},
	}

	props := im.SpreadProps()
	if props == nil {
		t.Fatal("SpreadProps returned nil")
	}
	for _, key := range []string{"cullKernelWGSL", "cullKernelWGSLRef", "cullKernelEntry", "cullRadius", "cullBackend"} {
		if _, has := props[key]; has {
			t.Errorf("plain mesh must not have cull key %q in SpreadProps output", key)
		}
	}
}

func TestInstancedGLBMeshAutoID(t *testing.T) {
	props := Props{
		Camera: PerspectiveCamera{Position: Vec3(0, 6, 8), FOV: 45, Near: 0.1, Far: 40},
		Graph: NewGraph(
			InstancedGLBMesh{
				Src:       "/models/robot-scout.glb",
				Instances: []MeshInstance{{Position: Vec3(1, 0, 2), Scale: Vec3(1, 1, 1)}},
			},
			InstancedGLBMesh{
				Src:       "/models/robot-scout.glb",
				Instances: []MeshInstance{{Position: Vec3(3, 0, 4), Scale: Vec3(1, 1, 1)}},
			},
		),
	}
	ir := props.SceneIR()
	if len(ir.InstancedGLBMeshes) != 2 {
		t.Fatalf("expected 2 InstancedGLBMeshes, got %d", len(ir.InstancedGLBMeshes))
	}
	if ir.InstancedGLBMeshes[0].ID == ir.InstancedGLBMeshes[1].ID {
		t.Fatalf("expected distinct auto-IDs, got duplicate %q", ir.InstancedGLBMeshes[0].ID)
	}
}
