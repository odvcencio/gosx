package scene

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

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

func TestPropsLegacyPropsLowerCameraRotationAndControls(t *testing.T) {
	props := Props{
		Controls:           "orbit",
		ControlTarget:      Vec3(1.5, 0.25, 0.8),
		ControlRotateSpeed: 1.4,
		ControlZoomSpeed:   0.85,
		Camera: PerspectiveCamera{
			Position: Vec3(0.2, 0.6, 6),
			Rotation: Rotate(0.18, -0.32, 0.05),
			FOV:      62,
			Near:     0.1,
			Far:      96,
		},
	}

	legacy := props.LegacyProps()
	if got := legacy["controls"]; got != "orbit" {
		t.Fatalf("expected controls orbit, got %#v", got)
	}
	controlTarget, ok := legacy["controlTarget"].(map[string]any)
	if !ok {
		t.Fatalf("expected control target map, got %#v", legacy["controlTarget"])
	}
	if got := controlTarget["x"]; got != 1.5 {
		t.Fatalf("expected control target x 1.5, got %#v", got)
	}
	if got := legacy["controlRotateSpeed"]; got != 1.4 {
		t.Fatalf("expected rotate speed 1.4, got %#v", got)
	}
	if got := legacy["controlZoomSpeed"]; got != 0.85 {
		t.Fatalf("expected zoom speed 0.85, got %#v", got)
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
				Material: GlowMaterial{
					Color:      "#ffd48f",
					Opacity:    Float(0.78),
					Emissive:   Float(0.24),
					BlendMode:  BlendAdditive,
					RenderPass: RenderAdditive,
					Wireframe:  Bool(false),
				},
				Pickable: Bool(true),
				Static:   Bool(true),
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
	if model.MaterialKind != "glow" {
		t.Fatalf("expected glow material override, got %#v", model.MaterialKind)
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
	if _, ok := objects[0]["segments"]; !ok {
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
				ID:         "sun",
				Color:      "#ffffff",
				Intensity:  1.5,
				Direction:  Vec3(-1, -1, 0),
				CastShadow: true,
				ShadowBias: 0.001,
				ShadowSize: 2048,
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

func TestPropsSceneIRLowersModelAnimationFields(t *testing.T) {
	props := Props{
		Graph: NewGraph(
			Model{
				ID:        "dancer",
				Src:       "/models/dancer.gosx3d.json",
				Position:  Vec3(0, 0, 0),
				Scale:     Vec3(1, 1, 1),
				Animation: "idle",
				Loop:      Bool(true),
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
}

func TestPropsSceneIRLowersPointsNode(t *testing.T) {
	props := Props{
		Graph: NewGraph(
			Points{
				ID:          "stars",
				Count:       3,
				Positions:   []Vector3{Vec3(1, 2, 3), Vec3(4, 5, 6), Vec3(7, 8, 9)},
				Sizes:       []float64{2.0, 3.0, 4.0},
				Colors:      []string{"#ff0000", "#00ff00", "#0000ff"},
				Color:       "#ffffff",
				Style:       PointStyleFocus,
				Size:        5.0,
				Opacity:     0.8,
				BlendMode:   BlendAdditive,
				DepthWrite:  false,
				Attenuation: true,
				Position:    Vec3(10, 20, 30),
				Spin:        Rotate(0.1, 0.2, 0.3),
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

func contains(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}

func TestPropsSceneIRLowersSpotLight(t *testing.T) {
	props := Props{
		Graph: NewGraph(
			Group{
				Position: Vec3(0, 2, 0),
				Rotation: Rotate(0, 0, math.Pi/2),
				Children: []Node{
					SpotLight{
						ID:         "stage",
						Color:      "#ffe0a0",
						Intensity:  2.5,
						Position:   Vec3(1, 0, 0),
						Direction:  Vec3(0, -1, 0),
						Angle:      math.Pi / 6,
						Penumbra:   0.3,
						Range:      20,
						Decay:      2,
						CastShadow: true,
						ShadowBias: 0.002,
						ShadowSize: 1024,
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
	if !spot.CastShadow || spot.ShadowBias != 0.002 || spot.ShadowSize != 1024 {
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
