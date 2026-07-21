package scene

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDiffCommandsCreateReplaceAndRemoveSceneRecords(t *testing.T) {
	previous := SceneIR{
		Objects: []ObjectIR{
			{ID: "cube", Kind: "box", Width: 1, X: 3, Color: "#f00"},
			{ID: "gone", Kind: "sphere", Radius: 1},
		},
		Labels: []LabelIR{
			{ID: "title", Text: "Old", X: 1},
		},
		Lights: []LightIR{
			{ID: "sun", Kind: "directional", Intensity: 0.5},
		},
	}
	next := SceneIR{
		Objects: []ObjectIR{
			{ID: "cube", Kind: "box", Width: 1, X: 0, Color: "#f00"},
			{ID: "new", Kind: "sphere", Radius: 2},
		},
		Sprites: []SpriteIR{
			{ID: "marker", Src: "/marker.png", X: 2},
		},
		Lights: []LightIR{
			{ID: "sun", Kind: "directional", Intensity: 1.25},
		},
	}

	commands := DiffCommands(previous, next)
	wantKinds := []CommandKind{
		CommandRemoveObject, // gone
		CommandRemoveObject, // cube changed
		CommandCreateObject, // cube replacement
		CommandCreateObject, // new
		CommandRemoveObject, // title
		CommandCreateObject, // marker
		CommandRemoveObject, // sun changed
		CommandCreateObject, // sun replacement
	}
	if len(commands) != len(wantKinds) {
		t.Fatalf("commands = %d, want %d: %#v", len(commands), len(wantKinds), commands)
	}
	for i, want := range wantKinds {
		if commands[i].Kind != want {
			t.Fatalf("command %d kind = %d, want %d: %#v", i, commands[i].Kind, want, commands[i])
		}
	}
	if commands[0].ObjectID != "gone" || commands[1].ObjectID != "cube" || commands[2].ObjectID != "cube" {
		t.Fatalf("unexpected object command order: %#v", commands[:3])
	}
	payload := commandPayloadMap(t, commands[2])
	props := payloadMap(t, payload, "props")
	if _, ok := props["x"]; ok {
		t.Fatalf("replacement payload should rely on remove+create for zero-value reset, props=%#v", props)
	}
	if got := payload["geometry"]; got != "box" {
		t.Fatalf("object geometry = %#v, want box", got)
	}
	lightPayload := commandPayloadMap(t, commands[len(commands)-1])
	if got := lightPayload["kind"]; got != "light" {
		t.Fatalf("light payload kind = %#v", got)
	}
}

func TestDiffPropsCommandsLowerTypedScenes(t *testing.T) {
	previous := Props{
		Graph: NewGraph(Mesh{
			ID:       "box",
			Geometry: BoxGeometry{Width: 1, Height: 1, Depth: 1},
			Position: Vec3(1, 0, 0),
		}),
	}
	next := Props{
		Graph: NewGraph(Mesh{
			ID:       "box",
			Geometry: BoxGeometry{Width: 1, Height: 1, Depth: 1},
			Position: Vec3(2, 0, 0),
		}),
	}

	commands := DiffPropsCommands(previous, next)
	if len(commands) != 2 {
		t.Fatalf("commands = %#v, want remove+create", commands)
	}
	if commands[0].Kind != CommandRemoveObject || commands[0].ObjectID != "box" {
		t.Fatalf("remove command = %#v", commands[0])
	}
	data, err := MarshalCommands(commands)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, snippet := range []string{`"kind":1`, `"objectId":"box"`, `"geometry":"box"`} {
		if !strings.Contains(text, snippet) {
			t.Fatalf("expected %s in %s", snippet, text)
		}
	}
}

func TestDiffCommandsCoversHTMLRecords(t *testing.T) {
	previous := SceneIR{
		HTML: []HTMLIR{
			{ID: "panel", Target: "hero", Mode: string(HTMLDOM), HTML: "<button>Old</button>", X: 1},
			{ID: "stale", HTML: "<p>Gone</p>"},
		},
	}
	next := SceneIR{
		HTML: []HTMLIR{
			{ID: "panel", Target: "hero", Mode: string(HTMLTexture), HTML: "<button>New</button>", TextureWidth: 512, TextureHeight: 320},
			{ID: "fresh", HTML: "<p>Fresh</p>", PointerEvents: "auto"},
		},
	}

	commands := DiffCommands(previous, next)
	if len(commands) != 4 {
		t.Fatalf("commands = %d, want 4: %#v", len(commands), commands)
	}
	if commands[0].Kind != CommandRemoveObject || commands[0].ObjectID != "stale" {
		t.Fatalf("stale html remove = %#v", commands[0])
	}
	if commands[1].Kind != CommandRemoveObject || commands[1].ObjectID != "panel" {
		t.Fatalf("panel replacement remove = %#v", commands[1])
	}
	for _, idx := range []int{2, 3} {
		if commands[idx].Kind != CommandCreateObject {
			t.Fatalf("html create command %d = %#v", idx, commands[idx])
		}
		payload := commandPayloadMap(t, commands[idx])
		if got := payload["kind"]; got != "html" {
			t.Fatalf("html payload kind = %#v, want html", got)
		}
	}
	props := payloadMap(t, commandPayloadMap(t, commands[2]), "props")
	if got := props["mode"]; got != string(HTMLTexture) {
		t.Fatalf("html mode = %#v, want texture", got)
	}
	if got := props["textureWidth"]; got != float64(512) {
		t.Fatalf("html textureWidth = %#v, want 512", got)
	}
}

func TestDiffCommandsReplacesPostEffects(t *testing.T) {
	previous := SceneIR{
		PostEffects:     []PostEffectIR{BloomIR{Threshold: 0.8, Strength: 0.2}},
		PostFXMaxPixels: 4096,
	}
	next := SceneIR{
		PostEffects:     []PostEffectIR{DOFIR{FocusDistance: 6, Aperture: 0.08, MaxBlur: 5}},
		PostFXMaxPixels: 16384,
	}

	commands := DiffCommands(previous, next)
	if len(commands) != 1 {
		t.Fatalf("commands = %#v, want one post-fx command", commands)
	}
	if commands[0].Kind != CommandSetPostEffects {
		t.Fatalf("command kind = %d, want %d", commands[0].Kind, CommandSetPostEffects)
	}
	payload := commandPayloadMap(t, commands[0])
	if got := payload["postFXMaxPixels"]; got != float64(16384) {
		t.Fatalf("postFXMaxPixels = %#v, want 16384", got)
	}
	effects, ok := payload["postEffects"].([]any)
	if !ok || len(effects) != 1 {
		t.Fatalf("postEffects payload = %#v", payload["postEffects"])
	}
	effect, ok := effects[0].(map[string]any)
	if !ok || effect["kind"] != "dof" || effect["focusDistance"] != float64(6) {
		t.Fatalf("post effect payload = %#v", effects[0])
	}
}

func TestSetPostUniformsCommandBuildsStableTypedPayload(t *testing.T) {
	command := SetPostUniformsCommand([]PostUniformPatch{
		{
			Name: "flare-shield",
			Uniforms: map[string]any{
				"shieldStrength": 0.58,
				"iris":           true,
			},
		},
	})
	if command.Kind != CommandSetPostUniforms {
		t.Fatalf("command kind = %d, want %d", command.Kind, CommandSetPostUniforms)
	}
	if command.Kind != 14 {
		t.Fatalf("CommandSetPostUniforms value = %d, want stable wire value 14", command.Kind)
	}
	payload := commandPayloadMap(t, command)
	effects, ok := payload["effects"].([]any)
	if !ok || len(effects) != 1 {
		t.Fatalf("effects payload = %#v", payload["effects"])
	}
	effect, ok := effects[0].(map[string]any)
	if !ok || effect["name"] != "flare-shield" {
		t.Fatalf("effect payload = %#v", effects[0])
	}
	uniforms, ok := effect["uniforms"].(map[string]any)
	if !ok || uniforms["shieldStrength"] != 0.58 || uniforms["iris"] != true {
		t.Fatalf("uniform payload = %#v", effect["uniforms"])
	}
}

func TestDiffCommandsReplacesEnvironment(t *testing.T) {
	previous := SceneIR{
		Environment: EnvironmentIR{AmbientColor: "#ffffff", AmbientIntensity: 0.1},
	}
	next := SceneIR{
		Environment: EnvironmentIR{AmbientColor: "#f5fbff", AmbientIntensity: 0.35, Exposure: 1.2, ToneMapping: "aces"},
	}

	commands := DiffCommands(previous, next)
	if len(commands) != 1 || commands[0].Kind != CommandSetEnvironment {
		t.Fatalf("commands = %#v, want one environment command", commands)
	}
	payload := commandPayloadMap(t, commands[0])
	environment := payloadMap(t, payload, "environment")
	if environment["ambientIntensity"] != 0.35 || environment["toneMapping"] != "aces" {
		t.Fatalf("environment payload = %#v", environment)
	}
}

func TestDiffCommandsReplacesParticleAndInstancedCollections(t *testing.T) {
	previous := SceneIR{
		Points: []PointsIR{
			{ID: "old-points", Count: 1, Positions: []float64{0, 0, 0}},
		},
		InstancedMeshes: []InstancedMeshIR{
			{ID: "old-batch", Kind: "box", Count: 1, Transforms: make([]float64, 16)},
		},
	}
	next := SceneIR{
		Points: []PointsIR{
			{ID: "stars", Count: 2, Positions: []float64{0, 0, 0, 1, 1, 1}, MinPixelSize: 2},
		},
		ComputeParticles: []ComputeParticlesIR{
			{ID: "field", Count: 4, Emitter: ParticleEmitterIR{Kind: "sphere", Radius: 2}},
		},
		WaterSystems: []WaterSystemIR{
			{ID: "pool-water", Resolution: 256, ComputeBackend: "elio", MaterialBackend: "selena"},
		},
		InstancedMeshes: []InstancedMeshIR{
			{ID: "debris", Kind: "torus", Count: 2, Radius: 1.2, Tube: 0.2, Transforms: make([]float64, 32)},
		},
	}

	commands := DiffCommands(previous, next)
	if len(commands) != 2 {
		t.Fatalf("commands = %#v, want particle+instanced replacements", commands)
	}
	if commands[0].Kind != CommandSetParticles {
		t.Fatalf("particles command = %#v", commands[0])
	}
	particlesPayload := commandPayloadMap(t, commands[0])
	if points, ok := particlesPayload["points"].([]any); !ok || len(points) != 1 {
		t.Fatalf("points payload = %#v", particlesPayload["points"])
	}
	if compute, ok := particlesPayload["computeParticles"].([]any); !ok || len(compute) != 1 {
		t.Fatalf("compute payload = %#v", particlesPayload["computeParticles"])
	}
	if water, ok := particlesPayload["waterSystems"].([]any); !ok || len(water) != 1 {
		t.Fatalf("water payload = %#v", particlesPayload["waterSystems"])
	}
	if commands[1].Kind != CommandSetInstancedMeshes {
		t.Fatalf("instanced command = %#v", commands[1])
	}
	instancedPayload := commandPayloadMap(t, commands[1])
	meshes, ok := instancedPayload["instancedMeshes"].([]any)
	if !ok || len(meshes) != 1 {
		t.Fatalf("instanced payload = %#v", instancedPayload["instancedMeshes"])
	}
	mesh := meshes[0].(map[string]any)
	if mesh["kind"] != "torus" || mesh["radius"] != 1.2 {
		t.Fatalf("instanced mesh payload = %#v", mesh)
	}
}

func TestDiffCommandsReplacesModelGLBAndAnimationCollections(t *testing.T) {
	speed := 1.25
	previous := SceneIR{
		Models: []ModelIR{{
			ObjectIR: ObjectIR{ID: "old-model", Kind: "model"},
			Src:      "/old.glb",
		}},
	}
	next := SceneIR{
		Models: []ModelIR{{
			ObjectIR:       ObjectIR{ID: "hero", Kind: "model", Color: "#77c6ff"},
			Src:            "/hero.glb",
			Animation:      "open",
			AnimationSpeed: &speed,
		}},
		InstancedGLBMeshes: []InstancedGLBMeshIR{{
			ID:           "parts",
			Src:          "/part.glb",
			MaterialKind: "standard",
			Roughness:    0.32,
			Instances: []MeshInstanceIR{
				{ID: "a", X: 1, ScaleX: 1, ScaleY: 1, ScaleZ: 1},
				{ID: "b", X: 2, ScaleX: 1, ScaleY: 1, ScaleZ: 1},
			},
		}},
		Animations: []AnimationClipIR{{
			Name:     "pulse",
			Duration: 1,
			Channels: []AnimationChannelIR{{
				TargetNode: 0,
				Property:   "rotationY",
				Times:      []float64{0, 1},
				Values:     []float64{0, 3.14},
			}},
		}},
	}

	commands := DiffCommands(previous, next)
	wantKinds := []CommandKind{
		CommandSetModels,
		CommandSetInstancedGLBMeshes,
		CommandSetAnimations,
	}
	if len(commands) != len(wantKinds) {
		t.Fatalf("commands = %#v, want model/glb/animation replacements", commands)
	}
	for i, want := range wantKinds {
		if commands[i].Kind != want {
			t.Fatalf("command %d kind = %d, want %d: %#v", i, commands[i].Kind, want, commands[i])
		}
	}
	modelPayload := commandPayloadMap(t, commands[0])
	models, ok := modelPayload["models"].([]any)
	if !ok || len(models) != 1 {
		t.Fatalf("models payload = %#v", modelPayload["models"])
	}
	model := models[0].(map[string]any)
	if model["src"] != "/hero.glb" || model["animation"] != "open" || model["animationSpeed"] != speed {
		t.Fatalf("model payload = %#v", model)
	}
	glbPayload := commandPayloadMap(t, commands[1])
	batches, ok := glbPayload["instancedGLBMeshes"].([]any)
	if !ok || len(batches) != 1 {
		t.Fatalf("instanced GLB payload = %#v", glbPayload["instancedGLBMeshes"])
	}
	batch := batches[0].(map[string]any)
	if batch["src"] != "/part.glb" || batch["roughness"] != 0.32 {
		t.Fatalf("instanced GLB batch = %#v", batch)
	}
	instances, ok := batch["instances"].([]any)
	if !ok || len(instances) != 2 {
		t.Fatalf("instanced GLB instances = %#v", batch["instances"])
	}
	animationPayload := commandPayloadMap(t, commands[2])
	animations, ok := animationPayload["animations"].([]any)
	if !ok || len(animations) != 1 {
		t.Fatalf("animations payload = %#v", animationPayload["animations"])
	}
	clip := animations[0].(map[string]any)
	if clip["name"] != "pulse" || clip["duration"] != float64(1) {
		t.Fatalf("animation clip = %#v", clip)
	}
}

func TestDiffIRCommandsReplacesMaterials(t *testing.T) {
	previous := IR{
		Materials: []IRMaterial{{Name: "hero", Kind: "standard", Color: "#ffffff"}},
	}
	next := IR{
		Materials: []IRMaterial{{
			Name:  "hero",
			Kind:  "custom",
			Color: "#94a3b8",
			Variants: map[string]IRMaterialVariant{
				"full": {Color: "#f5c76b"},
			},
		}},
	}

	commands := DiffIRCommands(previous, next)
	if len(commands) != 1 || commands[0].Kind != CommandSetMaterials {
		t.Fatalf("commands = %#v, want one material replacement", commands)
	}
	payload := commandPayloadMap(t, commands[0])
	materials, ok := payload["materials"].([]any)
	if !ok || len(materials) != 1 {
		t.Fatalf("materials payload = %#v", payload["materials"])
	}
	material := materials[0].(map[string]any)
	if material["kind"] != "custom" || material["color"] != "#94a3b8" {
		t.Fatalf("material payload = %#v", material)
	}
}

func TestDiffIRCommandsReplacesCameraEnvironmentAndMaterials(t *testing.T) {
	previous := IR{
		Version:     IRVersion,
		Camera:      IRCamera{Z: 6, Near: 0.05, Far: 128},
		Environment: IREnvironment{AmbientColor: "#ffffff", AmbientIntensity: 0.1},
		Materials:   []IRMaterial{{Name: "hero", Kind: "standard", Color: "#ffffff"}},
	}
	next := IR{
		Version:     IRVersion,
		Camera:      IRCamera{Z: 8, Near: 0.05, Far: 256},
		Environment: IREnvironment{AmbientColor: "#f5fbff", AmbientIntensity: 0.35, Exposure: 1.1},
		Materials:   []IRMaterial{{Name: "hero", Kind: "standard", Color: "#77c6ff"}},
	}

	commands := DiffIRCommands(previous, next)
	wantKinds := []CommandKind{CommandSetCamera, CommandSetEnvironment, CommandSetMaterials}
	if len(commands) != len(wantKinds) {
		t.Fatalf("commands = %#v, want camera/environment/material replacements", commands)
	}
	for i, want := range wantKinds {
		if commands[i].Kind != want {
			t.Fatalf("command %d kind = %d, want %d: %#v", i, commands[i].Kind, want, commands[i])
		}
	}
	cameraPayload := commandPayloadMap(t, commands[0])
	if cameraPayload["z"] != float64(8) || cameraPayload["far"] != float64(256) {
		t.Fatalf("camera payload = %#v", cameraPayload)
	}
	environmentPayload := payloadMap(t, commandPayloadMap(t, commands[1]), "environment")
	if environmentPayload["exposure"] != 1.1 {
		t.Fatalf("environment payload = %#v", environmentPayload)
	}
}

func TestMarshalCommandsNilIsEmptyArray(t *testing.T) {
	data, err := MarshalCommands(nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "[]" {
		t.Fatalf("nil commands marshal = %s", data)
	}
}

func commandPayloadMap(t *testing.T, command Command) map[string]any {
	t.Helper()
	data, err := json.Marshal(command.Data)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func payloadMap(t *testing.T, payload map[string]any, key string) map[string]any {
	t.Helper()
	data, err := json.Marshal(payload[key])
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}
