package scene

import (
	"bytes"
	"encoding/json"
	"sort"
)

// CommandKind mirrors the Scene3D client command protocol. The numeric values
// are intentionally stable because the browser runtime consumes them directly.
type CommandKind int

const (
	CommandCreateObject CommandKind = iota
	CommandRemoveObject
	CommandSetTransform
	CommandSetMaterial
	CommandSetLight
	CommandSetCamera
	CommandSetParticles
	CommandSetPostEffects
	CommandSetInstancedMeshes
	CommandSetMaterials
	CommandSetModels
	CommandSetInstancedGLBMeshes
	CommandSetAnimations
	CommandSetEnvironment
	CommandSetPostUniforms
)

// Command is a server-authored Scene3D mutation. Send a JSON array of Commands
// to a mounted Scene3D surface's applyCommands bridge to update the scene
// without replacing the whole page or rehydrating an island.
type Command struct {
	Kind     CommandKind `json:"kind"`
	ObjectID string      `json:"objectId,omitempty"`
	Data     any         `json:"data,omitempty"`
}

// CommandPayload is the create payload shape accepted by the Scene3D runtime.
type CommandPayload struct {
	Kind     string `json:"kind,omitempty"`
	Geometry string `json:"geometry,omitempty"`
	Props    any    `json:"props,omitempty"`
}

// PostUniformPatch is one named CustomPost uniform patch. The browser runtime
// shallow-merges Uniforms into every installed post effect with the same name.
type PostUniformPatch struct {
	Name     string         `json:"name"`
	Uniforms map[string]any `json:"uniforms"`
}

// DiffCommands builds a conservative command list that turns previous into
// next for records the current client command bridge can mutate: objects,
// labels, sprites, HTML overlays, and lights. Changed records are replaced
// with remove+create instead of partial patches so zero-value resets and
// omitted JSON fields remain correct.
func DiffCommands(previous, next SceneIR) []Command {
	var commands []Command
	diffSceneRecords(&commands, previous.Objects, next.Objects, func(record ObjectIR) string {
		return record.ID
	}, func(record ObjectIR) Command {
		return CreateObjectCommand(record)
	})
	diffSceneRecords(&commands, previous.Labels, next.Labels, func(record LabelIR) string {
		return record.ID
	}, func(record LabelIR) Command {
		return CreateLabelCommand(record)
	})
	diffSceneRecords(&commands, previous.Sprites, next.Sprites, func(record SpriteIR) string {
		return record.ID
	}, func(record SpriteIR) Command {
		return CreateSpriteCommand(record)
	})
	diffSceneRecords(&commands, previous.HTML, next.HTML, func(record HTMLIR) string {
		return record.ID
	}, func(record HTMLIR) Command {
		return CreateHTMLCommand(record)
	})
	diffSceneRecords(&commands, previous.Lights, next.Lights, func(record LightIR) string {
		return record.ID
	}, func(record LightIR) Command {
		return CreateLightCommand(record)
	})
	if !sceneRecordJSONEqual(previous.Environment, next.Environment) {
		commands = append(commands, SetEnvironmentCommand(next.Environment))
	}
	if !sceneRecordJSONEqual(previous.Models, next.Models) {
		commands = append(commands, SetModelsCommand(next.Models))
	}
	if !sceneRecordJSONEqual(previous.Points, next.Points) || !sceneRecordJSONEqual(previous.ComputeParticles, next.ComputeParticles) || !sceneRecordJSONEqual(previous.WaterSystems, next.WaterSystems) {
		commands = append(commands, SetParticlesCommand(next.Points, next.ComputeParticles, next.WaterSystems))
	}
	if !sceneRecordJSONEqual(previous.InstancedMeshes, next.InstancedMeshes) {
		commands = append(commands, SetInstancedMeshesCommand(next.InstancedMeshes))
	}
	if !sceneRecordJSONEqual(previous.InstancedGLBMeshes, next.InstancedGLBMeshes) {
		commands = append(commands, SetInstancedGLBMeshesCommand(next.InstancedGLBMeshes))
	}
	if !sceneRecordJSONEqual(previous.Animations, next.Animations) {
		commands = append(commands, SetAnimationsCommand(next.Animations))
	}
	if !sceneRecordJSONEqual(previous.PostEffects, next.PostEffects) || previous.PostFXMaxPixels != next.PostFXMaxPixels {
		commands = append(commands, SetPostEffectsCommand(next.PostEffects, next.PostFXMaxPixels))
	}
	return commands
}

// DiffPropsCommands lowers two typed Scene3D props values and diffs the
// resulting SceneIR payloads.
func DiffPropsCommands(previous, next Props) []Command {
	return DiffCommands(previous.SceneIR(), next.SceneIR())
}

// DiffIRCommands builds commands for canonical Scene3D IR fields that do not
// exist on the legacy SceneIR compatibility payload.
func DiffIRCommands(previous, next IR) []Command {
	var commands []Command
	if !sceneRecordJSONEqual(previous.Camera, next.Camera) {
		commands = append(commands, SetCameraCommand(next.Camera))
	}
	if !sceneRecordJSONEqual(previous.Environment, next.Environment) {
		commands = append(commands, SetEnvironmentCommand(next.Environment))
	}
	if !sceneRecordJSONEqual(previous.Materials, next.Materials) {
		commands = append(commands, SetMaterialsCommand(next.Materials))
	}
	return commands
}

// CreateObjectCommand builds a create command for mesh-like Scene3D objects.
func CreateObjectCommand(record ObjectIR) Command {
	return Command{
		Kind:     CommandCreateObject,
		ObjectID: record.ID,
		Data: CommandPayload{
			Geometry: record.Kind,
			Props:    record,
		},
	}
}

// CreateLabelCommand builds a create command for a projected Scene3D label.
func CreateLabelCommand(record LabelIR) Command {
	return Command{
		Kind:     CommandCreateObject,
		ObjectID: record.ID,
		Data: CommandPayload{
			Kind:  "label",
			Props: record,
		},
	}
}

// CreateSpriteCommand builds a create command for a projected Scene3D sprite.
func CreateSpriteCommand(record SpriteIR) Command {
	return Command{
		Kind:     CommandCreateObject,
		ObjectID: record.ID,
		Data: CommandPayload{
			Kind:  "sprite",
			Props: record,
		},
	}
}

// CreateHTMLCommand builds a create command for a projected Scene3D HTML
// overlay or texture-backed HTML surface fallback record.
func CreateHTMLCommand(record HTMLIR) Command {
	return Command{
		Kind:     CommandCreateObject,
		ObjectID: record.ID,
		Data: CommandPayload{
			Kind:  "html",
			Props: record,
		},
	}
}

// CreateLightCommand builds a create command for a Scene3D light.
func CreateLightCommand(record LightIR) Command {
	return Command{
		Kind:     CommandCreateObject,
		ObjectID: record.ID,
		Data: CommandPayload{
			Kind:  "light",
			Props: record,
		},
	}
}

// SetParticlesCommand replaces point layers and compute particle systems as a
// unit. Dense particle buffers are diffed by value on the server and swapped as
// whole normalized runtime records on the client.
func SetParticlesCommand(points []PointsIR, compute []ComputeParticlesIR, water []WaterSystemIR) Command {
	return Command{
		Kind: CommandSetParticles,
		Data: map[string]any{
			"points":           points,
			"computeParticles": compute,
			"waterSystems":     water,
		},
	}
}

// SetInstancedMeshesCommand replaces the instanced primitive batches.
func SetInstancedMeshesCommand(meshes []InstancedMeshIR) Command {
	return Command{
		Kind: CommandSetInstancedMeshes,
		Data: map[string]any{
			"instancedMeshes": meshes,
		},
	}
}

// SetModelsCommand replaces GLB/glTF model instances as a collection. Model
// hydration is asynchronous on the browser side, so the runtime swaps the
// resolved model-owned objects/points/overlays after assets are loaded.
func SetModelsCommand(models []ModelIR) Command {
	return Command{
		Kind: CommandSetModels,
		Data: map[string]any{
			"models": models,
		},
	}
}

// SetInstancedGLBMeshesCommand replaces GLB-backed instanced model batches.
func SetInstancedGLBMeshesCommand(meshes []InstancedGLBMeshIR) Command {
	return Command{
		Kind: CommandSetInstancedGLBMeshes,
		Data: map[string]any{
			"instancedGLBMeshes": meshes,
		},
	}
}

// SetAnimationsCommand replaces top-level procedural/asset animation clips.
func SetAnimationsCommand(animations []AnimationClipIR) Command {
	return Command{
		Kind: CommandSetAnimations,
		Data: map[string]any{
			"animations": animations,
		},
	}
}

// SetCameraCommand replaces the active camera state.
func SetCameraCommand(camera any) Command {
	return Command{
		Kind: CommandSetCamera,
		Data: camera,
	}
}

// SetEnvironmentCommand replaces scene-wide lighting, atmosphere, and exposure.
func SetEnvironmentCommand(environment any) Command {
	return Command{
		Kind: CommandSetEnvironment,
		Data: map[string]any{
			"environment": environment,
		},
	}
}

// SetMaterialsCommand replaces the named/canonical material table used by
// nodes that reference materials by name or materialIndex.
func SetMaterialsCommand(materials []IRMaterial) Command {
	return Command{
		Kind: CommandSetMaterials,
		Data: map[string]any{
			"materials": materials,
		},
	}
}

// SetPostEffectsCommand replaces the ordered post-FX chain and memory cap.
// Post-FX order is semantic, so the diff protocol treats the chain as one
// collection rather than trying to patch individual effects in place.
func SetPostEffectsCommand(effects []PostEffectIR, maxPixels int) Command {
	return Command{
		Kind: CommandSetPostEffects,
		Data: map[string]any{
			"postEffects":     effects,
			"postFXMaxPixels": maxPixels,
		},
	}
}

// SetPostUniformsCommand patches named CustomPost uniforms without replacing
// the post-FX chain or invalidating compiled shader pipeline identity.
func SetPostUniformsCommand(effects []PostUniformPatch) Command {
	return Command{
		Kind: CommandSetPostUniforms,
		Data: map[string]any{
			"effects": effects,
		},
	}
}

// RemoveObjectCommand removes any renderable record with the given ID from the
// client runtime maps.
func RemoveObjectCommand(id string) Command {
	return Command{Kind: CommandRemoveObject, ObjectID: id}
}

// MarshalCommands returns the compact JSON payload a hub/server route should
// send to the browser.
func MarshalCommands(commands []Command) ([]byte, error) {
	if commands == nil {
		commands = []Command{}
	}
	return json.Marshal(commands)
}

func diffSceneRecords[T any](commands *[]Command, previous, next []T, id func(T) string, create func(T) Command) {
	prevByID := make(map[string]T, len(previous))
	nextByID := make(map[string]T, len(next))
	for _, record := range previous {
		recordID := id(record)
		if recordID != "" {
			prevByID[recordID] = record
		}
	}
	for _, record := range next {
		recordID := id(record)
		if recordID != "" {
			nextByID[recordID] = record
		}
	}

	var removed []string
	for recordID := range prevByID {
		if _, ok := nextByID[recordID]; !ok {
			removed = append(removed, recordID)
		}
	}
	sort.Strings(removed)
	for _, recordID := range removed {
		*commands = append(*commands, RemoveObjectCommand(recordID))
	}

	for _, record := range next {
		recordID := id(record)
		if recordID == "" {
			continue
		}
		previousRecord, existed := prevByID[recordID]
		if existed && sceneRecordJSONEqual(previousRecord, record) {
			continue
		}
		if existed {
			*commands = append(*commands, RemoveObjectCommand(recordID))
		}
		*commands = append(*commands, create(record))
	}
}

func sceneRecordJSONEqual[T any](a, b T) bool {
	aj, err := json.Marshal(a)
	if err != nil {
		return false
	}
	bj, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return bytes.Equal(aj, bj)
}
