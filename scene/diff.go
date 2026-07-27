package scene

import (
	"bytes"
	"encoding/json"
	"math"
	"reflect"
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
	// commandKindCount is one past the last declared kind. It lets a test walk
	// every kind and prove each one is either emitted by a Diff function or
	// recorded as constructor-only. Keep it last.
	commandKindCount
)

// commandKindOrigin says which caller produces one CommandKind.
type commandKindOrigin int

const (
	// originDiffScene: DiffScene, and so DiffCommands, emits the kind.
	originDiffScene commandKindOrigin = iota
	// originDiffSceneOption: DiffScene emits the kind only when a DiffOptions
	// field turns it on. DiffCommands never emits it.
	originDiffSceneOption
	// originDiffIR: DiffIRCommands emits the kind.
	originDiffIR
	// originConstructorOnly: no Diff function emits the kind. The exported
	// constructor is the whole contract, for a caller that builds commands by
	// hand and knows the payload shape.
	originConstructorOnly
)

// commandKindOrigins records the origin of every declared CommandKind, with the
// reason for a kind no Diff function emits.
// TestCommandKindOriginsMatchDiffOutput proves the table matches the code: it
// walks every kind, fails when a kind has no entry, and compares the emitted
// set against the table instead of trusting the comment.
var commandKindOrigins = map[CommandKind]struct {
	origin commandKindOrigin
	reason string
}{
	CommandCreateObject: {originDiffScene, ""},
	CommandRemoveObject: {originDiffScene, ""},
	CommandSetTransform: {originDiffSceneOption, "DiffOptions.PatchTransforms turns it on. " +
		"DiffCommands keeps emitting remove plus create for a moved object, because a consumer " +
		"folds that pair into a move and a silent switch to a patch would change its input shape."},
	CommandSetMaterial: {originConstructorOnly, "A material edit changes many record fields at once, " +
		"including fields the runtime resolves against the geometry, so a partial patch cannot express " +
		"a zero-value reset. DiffScene replaces the record instead."},
	CommandSetLight: {originConstructorOnly, "Same reason as CommandSetMaterial. A light record is small, " +
		"so replacing it costs about what a patch would cost."},
	CommandSetCamera:             {originDiffIR, ""},
	CommandSetParticles:          {originDiffScene, ""},
	CommandSetPostEffects:        {originDiffScene, ""},
	CommandSetInstancedMeshes:    {originDiffScene, ""},
	CommandSetMaterials:          {originDiffIR, ""},
	CommandSetModels:             {originDiffScene, ""},
	CommandSetInstancedGLBMeshes: {originDiffScene, ""},
	CommandSetAnimations:         {originDiffScene, ""},
	CommandSetEnvironment:        {originDiffScene, ""},
	CommandSetPostUniforms: {originConstructorOnly, "A uniform patch keeps compiled shader pipeline " +
		"identity, which a diff of two whole post chains cannot prove. Only the caller knows that it " +
		"changed nothing but uniform values, so only the caller may claim it."},
}

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

// DiffOptions selects optional diff behavior. The zero value reproduces
// DiffCommands exactly, command for command.
type DiffOptions struct {
	// PatchTransforms emits one CommandSetTransform for an object whose record
	// changed only in position, rotation, or scale. It replaces the remove plus
	// create pair for that object, and it ships nine floats instead of the whole
	// record, including geometry. Turn it on for a drag or a physics step, where
	// most frames move an object and change nothing else.
	//
	// Leave it off when a consumer folds a remove plus create pair back into a
	// move. Such a consumer reads the pair, not the patch, and the fold exists
	// only because DiffCommands has no patch path. A consumer that understands
	// CommandSetTransform can drop the fold and turn this on.
	//
	// The option covers objects only. A label, a sprite, an HTML overlay, and a
	// light keep the pair, because their records carry no scale or rotation and
	// a position-only patch would save little.
	PatchTransforms bool
}

// Diff is the full result of DiffScene: the commands that carry the change, plus
// the fields that no command can carry.
type Diff struct {
	// Commands turns previous into next for every field a command kind covers.
	Commands []Command
	// RemountFields names the SceneIR fields that changed and that the client
	// command protocol cannot express. The list is sorted and stable.
	//
	// A non-empty list means the commands alone do NOT reproduce next. Resend
	// the whole scene, or remount the surface, and do not treat the commands as
	// a complete update. Before this list existed, a change to one of these
	// fields produced zero commands and no diagnostic, so a collaborator's edit
	// to the quality ladder or the motion program vanished. See
	// sceneIRFieldPolicies for the field-by-field reason.
	RemountFields []string
}

// DiffCommands builds a conservative command list that turns previous into
// next for records the current client command bridge can mutate: objects,
// labels, sprites, HTML overlays, and lights. Changed records are replaced
// with remove+create instead of partial patches so zero-value resets and
// omitted JSON fields remain correct.
//
// DiffCommands reports nothing about the SceneIR fields no command kind carries.
// Call DiffScene instead when a lost change matters; its Diff.RemountFields
// names them.
func DiffCommands(previous, next SceneIR) []Command {
	return DiffScene(previous, next, DiffOptions{}).Commands
}

// DiffScene builds the commands that turn previous into next, and reports the
// changed fields the command protocol cannot carry.
func DiffScene(previous, next SceneIR, options DiffOptions) Diff {
	var commands []Command
	var objectPatch func(previous, next *ObjectIR) (Command, bool)
	if options.PatchTransforms {
		objectPatch = objectTransformPatch
	}
	diffSceneRecords(&commands, previous.Objects, next.Objects, func(record *ObjectIR) string {
		return record.ID
	}, func(record *ObjectIR) Command {
		return CreateObjectCommand(*record)
	}, objectPatch)
	diffSceneRecords(&commands, previous.Labels, next.Labels, func(record *LabelIR) string {
		return record.ID
	}, func(record *LabelIR) Command {
		return CreateLabelCommand(*record)
	}, nil)
	diffSceneRecords(&commands, previous.Sprites, next.Sprites, func(record *SpriteIR) string {
		return record.ID
	}, func(record *SpriteIR) Command {
		return CreateSpriteCommand(*record)
	}, nil)
	diffSceneRecords(&commands, previous.HTML, next.HTML, func(record *HTMLIR) string {
		return record.ID
	}, func(record *HTMLIR) Command {
		return CreateHTMLCommand(*record)
	}, nil)
	diffSceneRecords(&commands, previous.Lights, next.Lights, func(record *LightIR) string {
		return record.ID
	}, func(record *LightIR) Command {
		return CreateLightCommand(*record)
	}, nil)
	if !sceneRecordJSONEqual(previous.Environment, next.Environment) {
		commands = append(commands, SetSceneEnvironmentCommand(next.Environment))
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
	return Diff{Commands: commands, RemountFields: sceneIRRemountFields(&previous, &next)}
}

// sceneIRDiffPolicy says how DiffScene treats one SceneIR field.
type sceneIRDiffPolicy int

const (
	// sceneIRDiffed: a command kind carries the field, and DiffScene emits it.
	sceneIRDiffed sceneIRDiffPolicy = iota
	// sceneIRRemount: no command kind carries the field. DiffScene names it in
	// Diff.RemountFields when it changes, so a caller resends the whole scene
	// instead of losing the change.
	sceneIRRemount
	// sceneIRDerived: the field is computed, never authored. DiffScene ignores it,
	// because the authored field it comes from is diffed or reported already.
	sceneIRDerived
)

// sceneIRFieldPolicy is one field's entry in sceneIRFieldPolicies.
type sceneIRFieldPolicy struct {
	policy sceneIRDiffPolicy
	// reason explains a policy other than sceneIRDiffed.
	reason string
	// changed reports whether the field differs. Set it only for sceneIRRemount.
	changed func(previous, next *SceneIR) bool
}

// sceneIRFieldPolicies classifies every SceneIR field for the diff protocol.
//
// The table exists because a missing entry used to be invisible. Nine fields
// produced no command and no diagnostic when they changed, so a collaborator who
// changed the quality ladder or the motion program lost the edit with no way to
// find out. Now every field is named, and
// TestSceneIRFieldPoliciesCoverEveryField walks SceneIR by reflection and fails
// when a new field joins without a decision.
//
// The reason is the same for most non-diffed fields: the client command protocol
// has no kind that carries the field, and the numeric kinds are a wire contract
// the browser runtime switches on. Adding one needs a matching runtime change, so
// the honest answer today is to report the change and let the caller remount.
var sceneIRFieldPolicies = map[string]sceneIRFieldPolicy{
	"Objects":            {policy: sceneIRDiffed},
	"Models":             {policy: sceneIRDiffed},
	"Points":             {policy: sceneIRDiffed},
	"InstancedMeshes":    {policy: sceneIRDiffed},
	"InstancedGLBMeshes": {policy: sceneIRDiffed},
	"ComputeParticles":   {policy: sceneIRDiffed},
	"WaterSystems":       {policy: sceneIRDiffed},
	"Animations":         {policy: sceneIRDiffed},
	"Labels":             {policy: sceneIRDiffed},
	"Sprites":            {policy: sceneIRDiffed},
	"HTML":               {policy: sceneIRDiffed},
	"Lights":             {policy: sceneIRDiffed},
	"Environment":        {policy: sceneIRDiffed},
	"PostEffects":        {policy: sceneIRDiffed},
	"PostFXMaxPixels":    {policy: sceneIRDiffed},

	"Schema": {
		policy: sceneIRRemount,
		reason: "The schema names the payload contract itself. A different schema means the " +
			"records may not mean what this build reads, so no command sequence is safe.",
		changed: func(previous, next *SceneIR) bool { return previous.Schema != next.Schema },
	},
	"ShadowMaxPixels": {
		policy: sceneIRRemount,
		reason: "A shadow atlas budget. The runtime sizes the atlas once at mount and no command " +
			"kind resizes it.",
		changed: func(previous, next *SceneIR) bool { return previous.ShadowMaxPixels != next.ShadowMaxPixels },
	},
	"QualityLadder": {
		policy: sceneIRRemount,
		reason: "The client quality governor reads the ladder at mount. No command kind replaces it.",
		changed: func(previous, next *SceneIR) bool {
			return !sceneRecordJSONEqual(previous.QualityLadder, next.QualityLadder)
		},
	},
	"QualityStartRung": {
		policy:  sceneIRRemount,
		reason:  "The governor's starting rung, read at mount with the ladder.",
		changed: func(previous, next *SceneIR) bool { return previous.QualityStartRung != next.QualityStartRung },
	},
	"PointQualityGroups": {
		policy: sceneIRRemount,
		reason: "Maps a runtime-extracted points layer to a ladder group. The governor reads it at " +
			"mount, and no command kind replaces it.",
		changed: func(previous, next *SceneIR) bool {
			return !sceneRecordJSONEqual(previous.PointQualityGroups, next.PointQualityGroups)
		},
	},
	"BackendCaps": {
		policy: sceneIRRemount,
		reason: "The honesty-gate verdict: which backends can render this scene faithfully. The " +
			"server computes it from the scene and ships it once, and the client picks a backend from " +
			"it at mount. A live surface cannot re-gate itself, so a changed verdict means the mounted " +
			"backend may no longer be honest for the scene it now shows. Two scenes lowered by the same " +
			"build agree here, so this reports nothing on a normal edit.",
		changed: func(previous, next *SceneIR) bool {
			return !sceneRecordJSONEqual(previous.BackendCaps, next.BackendCaps)
		},
	},
	"ShaderLib": {
		policy: sceneIRRemount,
		reason: "A content-addressed side table for shader sources hoisted out of records. " +
			"UnmarshalJSON inflates every ref back into its record and clears the table, so a decoded " +
			"scene never reaches here with entries. A hand-built scene can, and then the records hold " +
			"refs whose meaning lives only in this table: comparing records alone would call two " +
			"different shaders equal.",
		changed: func(previous, next *SceneIR) bool {
			return !sceneRecordJSONEqual(previous.ShaderLib, next.ShaderLib)
		},
	},
	"MotionProgram": {
		policy: sceneIRRemount,
		reason: "The encoded transform motion timeline. No command kind carries it. A spin edit also " +
			"changes the owning object record, which does produce a command, but the timeline the " +
			"runtime plays arrives only with the scene.",
		changed: func(previous, next *SceneIR) bool {
			return !bytes.Equal(previous.MotionProgram, next.MotionProgram)
		},
	},
	"MaterialMotionProgram": {
		policy: sceneIRRemount,
		reason: "The encoded material-uniform motion timeline. No command kind carries it, and no " +
			"record field mirrors it, so a change here is invisible in every other field.",
		changed: func(previous, next *SceneIR) bool {
			return !bytes.Equal(previous.MaterialMotionProgram, next.MaterialMotionProgram)
		},
	},

	"SpinTracks": {
		policy: sceneIRDerived,
		reason: "An in-memory facade over motion core, tagged json:\"-\". Graph.SceneIR encodes it " +
			"into MotionProgram, which is reported, so diffing both would report one change twice.",
	},
	"MaterialTracks": {
		policy: sceneIRDerived,
		reason: "An in-memory facade, tagged json:\"-\". MaterialMotionProgram carries its encoded " +
			"form and is reported.",
	},
}

// sceneIRRemountFieldNames lists every sceneIRRemount field in sorted order, so
// Diff.RemountFields is stable across calls and across builds. Map iteration
// order is random, and a caller that compares two diffs needs a stable list.
var sceneIRRemountFieldNames = sortedRemountFieldNames()

func sortedRemountFieldNames() []string {
	names := make([]string, 0, len(sceneIRFieldPolicies))
	for name, policy := range sceneIRFieldPolicies {
		if policy.policy == sceneIRRemount {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// sceneIRRemountFields returns the changed fields no command kind can carry.
func sceneIRRemountFields(previous, next *SceneIR) []string {
	var changed []string
	for _, name := range sceneIRRemountFieldNames {
		if sceneIRFieldPolicies[name].changed(previous, next) {
			changed = append(changed, name)
		}
	}
	return changed
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
		commands = append(commands, SetIREnvironmentCommand(next.Environment))
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

// Environment payload shapes. CommandSetEnvironment carries two different Go
// types: EnvironmentIR from the SceneIR compatibility payload, and IREnvironment
// from the canonical IR. The two share most keys and differ in the rest, and a
// receiver holding raw JSON cannot tell which one it has. The "shape" key names
// it, so a receiver decodes the right struct instead of guessing.
//
// A discriminator key, not a second command kind. Three reasons:
//   - The numeric CommandKind values are a wire contract the browser runtime
//     switches on directly. A new kind needs a matching runtime change.
//   - Both shapes drive the same runtime state. applySceneEnvironmentCommand
//     normalizes either one into state.environment, so two kinds would write one
//     slot and the second would silently win.
//   - The key is additive. The runtime reads data.environment and ignores every
//     other key, so an older runtime keeps working.
const (
	// EnvironmentShapeSceneIR marks a scene.EnvironmentIR payload.
	EnvironmentShapeSceneIR = "sceneIR"
	// EnvironmentShapeCanonicalIR marks a scene.IREnvironment payload.
	EnvironmentShapeCanonicalIR = "canonicalIR"
)

// SetSceneEnvironmentCommand replaces scene-wide lighting, atmosphere, and
// exposure from the SceneIR compatibility payload.
func SetSceneEnvironmentCommand(environment EnvironmentIR) Command {
	return environmentCommand(environment, EnvironmentShapeSceneIR)
}

// SetIREnvironmentCommand replaces scene-wide lighting, atmosphere, and exposure
// from the canonical IR.
func SetIREnvironmentCommand(environment IREnvironment) Command {
	return environmentCommand(environment, EnvironmentShapeCanonicalIR)
}

// SetEnvironmentCommand replaces scene-wide lighting, atmosphere, and exposure.
//
// It stamps the shape key when it recognizes the payload type. It stamps
// nothing for any other type, because it cannot name a shape it does not know;
// such a payload stays as ambiguous as it was before. Prefer
// SetSceneEnvironmentCommand or SetIREnvironmentCommand, which always stamp.
func SetEnvironmentCommand(environment any) Command {
	switch typed := environment.(type) {
	case EnvironmentIR:
		return SetSceneEnvironmentCommand(typed)
	case *EnvironmentIR:
		if typed != nil {
			return SetSceneEnvironmentCommand(*typed)
		}
	case IREnvironment:
		return SetIREnvironmentCommand(typed)
	case *IREnvironment:
		if typed != nil {
			return SetIREnvironmentCommand(*typed)
		}
	}
	return environmentCommand(environment, "")
}

func environmentCommand(environment any, shape string) Command {
	data := map[string]any{"environment": environment}
	if shape != "" {
		data["shape"] = shape
	}
	return Command{Kind: CommandSetEnvironment, Data: data}
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

// TransformPatch is a whole-transform patch for one object: position, rotation
// in radians, and scale. The runtime shallow-merges it over the live object, so
// no field carries omitempty. An omitted key would keep the old value, and a
// reset to zero has to replicate.
//
// Scale is resolved, not raw. A zero scale component in ObjectIR means "unset",
// and both the browser runtime and the native renderer read it as 1. A patch
// merges over an object whose scale the runtime already resolved, so it must
// send the resolved 1. Sending 0 would collapse the object to a point.
type TransformPatch struct {
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	Z         float64 `json:"z"`
	RotationX float64 `json:"rotationX"`
	RotationY float64 `json:"rotationY"`
	RotationZ float64 `json:"rotationZ"`
	ScaleX    float64 `json:"scaleX"`
	ScaleY    float64 `json:"scaleY"`
	ScaleZ    float64 `json:"scaleZ"`
}

// SetTransformCommand patches one object's position, rotation, and scale without
// replacing the record. Dragging an object through remove plus create ships the
// whole record every frame, including geometry; this ships nine floats.
//
// DiffScene emits it when DiffOptions.PatchTransforms is on. Any caller may
// build it by hand for an object the runtime already holds.
func SetTransformCommand(id string, patch TransformPatch) Command {
	return Command{Kind: CommandSetTransform, ObjectID: id, Data: patch}
}

// objectTransformPatch returns a transform patch when next changed only in
// position, rotation, or scale. It proves that claim rather than assuming it: it
// copies previous, overwrites the nine transform fields with next's, and
// requires the copy to equal next. Any other changed field, including a nested
// slice or a map, leaves the copy unequal and the caller falls back to remove
// plus create. So a patch can never drop a second edit that arrived in the same
// frame.
func objectTransformPatch(previous, next *ObjectIR) (Command, bool) {
	probe := *previous
	probe.X, probe.Y, probe.Z = next.X, next.Y, next.Z
	probe.RotationX, probe.RotationY, probe.RotationZ = next.RotationX, next.RotationY, next.RotationZ
	probe.ScaleX, probe.ScaleY, probe.ScaleZ = next.ScaleX, next.ScaleY, next.ScaleZ
	if !sceneRecordPointerJSONEqual(&probe, next) {
		return Command{}, false
	}
	return SetTransformCommand(next.ID, TransformPatch{
		X:         next.X,
		Y:         next.Y,
		Z:         next.Z,
		RotationX: next.RotationX,
		RotationY: next.RotationY,
		RotationZ: next.RotationZ,
		ScaleX:    resolveIRScale(next.ScaleX),
		ScaleY:    resolveIRScale(next.ScaleY),
		ScaleZ:    resolveIRScale(next.ScaleZ),
	}), true
}

// resolveIRScale maps one ObjectIR scale component to an explicit factor. See
// TransformPatch for why a patch may not send the raw zero.
func resolveIRScale(value float64) float64 {
	if value == 0 {
		return 1
	}
	return value
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

// diffSceneRecords compares two record collections by ID.
//
// Every callback takes a pointer. A lowered record is a large struct — ObjectIR
// alone carries about a hundred fields — so passing one by value copied it into
// the ID map, into the comparison, and into the interface the comparison boxed.
// Pointers remove all three copies and leave the emitted commands unchanged.
//
// patch is optional. When it returns a command, that command replaces the remove
// plus create pair for one record. Pass nil to keep the pair.
func diffSceneRecords[T any](commands *[]Command, previous, next []T, id func(*T) string, create func(*T) Command, patch func(previous, next *T) (Command, bool)) {
	prevByID := make(map[string]*T, len(previous))
	nextIDs := make(map[string]struct{}, len(next))
	for index := range previous {
		if recordID := id(&previous[index]); recordID != "" {
			prevByID[recordID] = &previous[index]
		}
	}
	for index := range next {
		if recordID := id(&next[index]); recordID != "" {
			nextIDs[recordID] = struct{}{}
		}
	}

	var removed []string
	for recordID := range prevByID {
		if _, ok := nextIDs[recordID]; !ok {
			removed = append(removed, recordID)
		}
	}
	sort.Strings(removed)
	for _, recordID := range removed {
		*commands = append(*commands, RemoveObjectCommand(recordID))
	}

	for index := range next {
		record := &next[index]
		recordID := id(record)
		if recordID == "" {
			continue
		}
		previousRecord, existed := prevByID[recordID]
		if existed && sceneRecordPointerJSONEqual(previousRecord, record) {
			continue
		}
		if existed {
			if patch != nil {
				if command, ok := patch(previousRecord, record); ok {
					*commands = append(*commands, command)
					continue
				}
			}
			*commands = append(*commands, RemoveObjectCommand(recordID))
		}
		*commands = append(*commands, create(record))
	}
}

// sceneRecordJSONEqual reports whether two values encode to identical JSON.
func sceneRecordJSONEqual[T any](a, b T) bool {
	return sceneRecordPointerJSONEqual(&a, &b)
}

// sceneRecordPointerJSONEqual answers the same question as sceneRecordJSONEqual
// without copying either value.
//
// It tries provenJSONEqual first, and marshals only when that fast path cannot
// prove equality. The fast path is one-way on purpose: true means the two values
// must encode to identical JSON, and false means "not proven", never "different".
// So the answer is always the answer the marshal comparison gives, and the
// marshal comparison is still the only code that decides a record changed.
//
// Why this shape: DiffCommands used to marshal both records for every record in
// the scene, so an edit that moved 10 percent of a scene paid the full encode
// cost for 100 percent of it. That made the diff cost ten times the whole CRDT
// write path it feeds. A real edit leaves most records untouched, and an
// untouched record is exactly the case provenJSONEqual settles without encoding.
func sceneRecordPointerJSONEqual[T any](a, b *T) bool {
	if provenJSONEqual(reflect.ValueOf(a).Elem(), reflect.ValueOf(b).Elem()) {
		return true
	}
	return sceneRecordMarshalEqual(*a, *b)
}

// sceneRecordMarshalEqual is the reference comparison: encode both values and
// compare the bytes. A value that cannot encode, such as a record holding NaN,
// compares unequal to everything, including itself.
func sceneRecordMarshalEqual(a, b any) bool {
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

// provenJSONEqual reports whether two values of the same type are certain to
// encode to identical JSON. It returns false whenever it cannot prove that, so a
// false answer means "ask the encoder", not "the values differ". Every rule below
// exists to keep that guarantee, because a wrong true is a lost edit:
//
//   - A float must match bit for bit. Negative zero and positive zero compare
//     equal under ==, and encode as "-0" and "0", so bit equality is the test. A
//     NaN or an infinity is never proven equal, because the encoder rejects both
//     and sceneRecordMarshalEqual then reports unequal.
//   - A nil slice and an empty slice must not compare equal. They encode as null
//     and [] unless the field carries omitempty, so only the encoder knows.
//   - An interface must hold the same dynamic type on both sides. Two different
//     types can hold equal fields and still write different keys.
//   - An unexported field is compared too. The encoder ignores it, so comparing
//     it can only make this function stricter, never wrong. The walk never calls
//     Interface(), which is what an unexported field would reject.
//   - A kind this walk does not model, a channel or a function for example,
//     returns false.
//
// The walk assumes one thing: a MarshalJSON method is a pure function of the
// value it receives. Every marshaler in this package builds its output from its
// own fields, so two structurally identical values produce identical bytes.
func provenJSONEqual(a, b reflect.Value) bool {
	if a.Kind() != b.Kind() || a.Type() != b.Type() {
		return false
	}
	switch a.Kind() {
	case reflect.Bool:
		return a.Bool() == b.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return a.Int() == b.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return a.Uint() == b.Uint()
	case reflect.Float32, reflect.Float64:
		left, right := a.Float(), b.Float()
		if math.IsNaN(left) || math.IsNaN(right) || math.IsInf(left, 0) || math.IsInf(right, 0) {
			return false
		}
		return math.Float64bits(left) == math.Float64bits(right)
	case reflect.String:
		return a.String() == b.String()
	case reflect.Struct:
		for index := range a.NumField() {
			if !provenJSONEqual(a.Field(index), b.Field(index)) {
				return false
			}
		}
		return true
	case reflect.Slice:
		if a.IsNil() != b.IsNil() {
			return false
		}
		if a.Len() != b.Len() {
			return false
		}
		if a.Len() == 0 {
			return true
		}
		if equal, decided := provenNumericSliceEqual(a, b); decided {
			return equal
		}
		for index := range a.Len() {
			if !provenJSONEqual(a.Index(index), b.Index(index)) {
				return false
			}
		}
		return true
	case reflect.Array:
		for index := range a.Len() {
			if !provenJSONEqual(a.Index(index), b.Index(index)) {
				return false
			}
		}
		return true
	case reflect.Map:
		if a.IsNil() != b.IsNil() {
			return false
		}
		if a.Len() != b.Len() {
			return false
		}
		entries := a.MapRange()
		for entries.Next() {
			other := b.MapIndex(entries.Key())
			if !other.IsValid() {
				return false
			}
			if !provenJSONEqual(entries.Value(), other) {
				return false
			}
		}
		return true
	case reflect.Pointer:
		if a.IsNil() || b.IsNil() {
			return a.IsNil() && b.IsNil()
		}
		return provenJSONEqual(a.Elem(), b.Elem())
	case reflect.Interface:
		if a.IsNil() || b.IsNil() {
			return a.IsNil() && b.IsNil()
		}
		if a.Elem().Type() != b.Elem().Type() {
			return false
		}
		return provenJSONEqual(a.Elem(), b.Elem())
	default:
		return false
	}
}

// provenNumericSliceEqual compares the dense numeric slices a lowered scene
// carries in bulk: point positions, instance transforms, and packed quantized
// bytes. One typed loop replaces one reflect call per element, which matters
// when a single points layer holds a hundred thousand floats.
//
// The second result says whether this function decided. It declines for any
// other element type, and for a slice it cannot read as a plain Go value, and
// the caller then walks the elements.
func provenNumericSliceEqual(a, b reflect.Value) (equal, decided bool) {
	// Check the element kind before reading the slice as a Go value. Reading it
	// boxes the slice header into an interface, which allocates, and most slice
	// fields on a lowered record hold structs this function would decline anyway.
	switch a.Type().Elem().Kind() {
	case reflect.Uint8, reflect.Float32, reflect.Float64:
	default:
		return false, false
	}
	if !a.CanInterface() || !b.CanInterface() {
		return false, false
	}
	switch left := a.Interface().(type) {
	case []byte:
		return bytes.Equal(left, b.Interface().([]byte)), true
	case []float64:
		right := b.Interface().([]float64)
		for index, value := range left {
			if !provenFloatEqual(value, right[index]) {
				return false, true
			}
		}
		return true, true
	case []float32:
		right := b.Interface().([]float32)
		for index, value := range left {
			if !provenFloatEqual(float64(value), float64(right[index])) {
				return false, true
			}
		}
		return true, true
	default:
		return false, false
	}
}

// provenFloatEqual reports bit equality for two finite floats. See
// provenJSONEqual for why a NaN, an infinity, and a signed zero each need care.
func provenFloatEqual(left, right float64) bool {
	if math.IsNaN(left) || math.IsNaN(right) || math.IsInf(left, 0) || math.IsInf(right, 0) {
		return false
	}
	return math.Float64bits(left) == math.Float64bits(right)
}
