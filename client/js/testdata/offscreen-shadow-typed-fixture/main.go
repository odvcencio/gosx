// Command offscreen-shadow-typed-fixture emits a deterministic JSON fixture
// describing offscreen shadow-casting scene variants: static GLB models,
// posed and skinned models, and live animation subscriptions. It is pure
// data: it lowers scene.Props through SceneIR so the browser test consumes
// the exact wire shape the framework produces. No shadow matrices or shader
// math live here.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"m31labs.dev/gosx/scene"
)

const schema = "gosx.offscreen-shadow.fixture.v1"

type fixture struct {
	Schema string                   `json:"schema"`
	Camera cameraJSON               `json:"camera"`
	Scenes map[string]scene.SceneIR `json:"scenes"`
}

type cameraJSON struct {
	X   float64 `json:"x"`
	Y   float64 `json:"y"`
	Z   float64 `json:"z"`
	FOV float64 `json:"fov"`
}

var wireframe = false

// receiver is the white standard-material box the shadows land on. It never
// casts; only its receive flag matters for the offscreen capture.
func receiver() scene.Mesh {
	return scene.Mesh{
		ID: "receiver",
		Geometry: scene.BoxGeometry{
			Width:  3,
			Height: 2.2,
			Depth:  0.1,
		},
		Material: scene.StandardMaterial{
			Color:     "#ffffff",
			Roughness: 1,
			Metalness: 0,
			IOR:       scene.Float(1.5),
			Wireframe: &wireframe,
		},
		Position:      scene.Vec3(0, 0, -0.5),
		CastShadow:    false,
		ReceiveShadow: true,
	}
}

// ambient is the fixed fill light so shadowed regions are not pure black.
func ambient() scene.AmbientLight {
	return scene.AmbientLight{ID: "ambient", Color: "#404040", Intensity: 0.3}
}

// key is the single shadow-casting point light. Only the cast flag varies
// across scenes; everything else is fixed and explicit.
func key(cast bool) scene.PointLight {
	return scene.PointLight{
		ID:             "key",
		Color:          "#ffffff",
		Intensity:      80,
		Position:       scene.Vec3(-1, 1, 10),
		Range:          20,
		Decay:          2,
		CastShadow:     cast,
		ShadowBias:     0.0001,
		ShadowSize:     512,
		ShadowSoftness: 0,
	}
}

// model builds a framework-owned GLB instance. The root transform is always
// identity: zero position, unit scale, and no fit/bounds override, so the
// baked centers in the GLB fixtures land exactly where authored.
func model(id, src string, mutate ...func(*scene.Model)) scene.Model {
	wireframe := false
	m := scene.Model{
		ID:            id,
		Src:           src,
		Position:      scene.Vec3(0, 0, 0),
		Scale:         scene.Vector3{X: 1, Y: 1, Z: 1},
		Material:      scene.StandardMaterial{Color: "#c02020", Roughness: 1, Metalness: 0, Wireframe: &wireframe, IOR: scene.Float(1.5)},
		CastShadow:    true,
		ReceiveShadow: false,
	}
	for _, fn := range mutate {
		if fn != nil {
			fn(&m)
		}
	}
	return m
}

// poseAnim applies the named baked animation with looping enabled.
func poseAnim(name string) func(*scene.Model) {
	return func(m *scene.Model) {
		m.Animation = name
		loop := true
		m.Loop = &loop
	}
}

// live subscribes the model to the given live animation events.
func live(events []string) func(*scene.Model) {
	return func(m *scene.Model) { m.Live = events }
}

func build(props scene.Props) scene.SceneIR {
	return props.SceneIR()
}

func buildFixture() fixture {
	static := func(id, src string, mutate ...func(*scene.Model)) scene.SceneIR {
		g := scene.NewGraph(receiver(), ambient(), key(true), model(id, src, mutate...))
		return build(scene.Props{Graph: g})
	}

	scenes := map[string]scene.SceneIR{}

	// Baseline: no model at all, only receiver + lights.
	scenes["empty"] = build(scene.Props{Graph: scene.NewGraph(receiver(), ambient(), key(true))})

	// Static reference rest and pose.
	scenes["reference-rest"] = static("reference", "/models/static-rest.glb")
	scenes["reference-pose"] = static("reference", "/models/static-pose.glb")

	// Skinned rest and posed variants.
	scenes["skin-rest"] = static("skin-caster", "/models/skin-rest.glb")
	scenes["skin-pose"] = static("skin-caster", "/models/skin-pose.glb", poseAnim("pose"))

	// Posed skin that must not cast.
	scenes["skin-no-cast"] = static("skin-caster", "/models/skin-pose.glb", poseAnim("pose"), func(m *scene.Model) {
		m.CastShadow = false
	})

	// Posed skin with the key light's shadows disabled.
	scenes["skin-light-off"] = scene.SceneIR{}
	{
		k := key(false)
		g := scene.NewGraph(receiver(), ambient(), k, model("skin-caster", "/models/skin-pose.glb", poseAnim("pose")))
		scenes["skin-light-off"] = build(scene.Props{Graph: g})
	}

	// Live-updated posed skin driven by external animation events.
	scenes["skin-live"] = static("skin-caster", "/models/skin-pose.glb", live([]string{"pose-change"}))

	// Static live-subscribed caster plus a hidden guard target that must
	// retain its Live list so a static record exists for it.
	scenes["morph-live"] = scene.SceneIR{}
	{
		g := scene.NewGraph(
			receiver(),
			ambient(),
			key(true),
			model("morph-caster", "/models/static-rest.glb", func(m *scene.Model) {
				static := true
				m.Static = &static
				m.Live = []string{"pose-change"}
			}),
			model("morph-caster-guard", "/models/static-pose.glb", func(m *scene.Model) {
				static := true
				m.Static = &static
				m.Visible = scene.Bool(false)
				m.CastShadow = false
				m.Live = []string{"fixture-target"}
			}),
		)
		scenes["morph-live"] = build(scene.Props{Graph: g})
	}

	return fixture{
		Schema: schema,
		Camera: cameraJSON{X: 0, Y: 0, Z: 4, FOV: 50},
		Scenes: scenes,
	}
}

func main() {
	enc := json.NewEncoder(os.Stdout)
	if err := enc.Encode(buildFixture()); err != nil {
		fmt.Fprintf(os.Stderr, "offscreen-shadow-typed-fixture: %v\n", err)
		os.Exit(1)
	}
}
