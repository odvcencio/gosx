package scene

import (
	"reflect"
	"sort"
	"testing"

	"m31labs.dev/gosx/motion"
	"m31labs.dev/gosx/scene/capability"
)

// sceneIRFieldMutators changes exactly one SceneIR field, per field. The map
// covers every field of SceneIR, and TestSceneIRFieldPoliciesCoverEveryField
// fails when a field has no mutator, so a new field cannot join the payload
// without a decision about how the diff treats it.
var sceneIRFieldMutators = map[string]func(ir *SceneIR){
	"Schema":  func(ir *SceneIR) { ir.Schema = "gosx.scene3d.ir.v2" },
	"Objects": func(ir *SceneIR) { ir.Objects = append(ir.Objects, ObjectIR{ID: "added-object", Kind: "box"}) },
	"Models": func(ir *SceneIR) {
		ir.Models = append(ir.Models, ModelIR{ObjectIR: ObjectIR{ID: "added-model"}, Src: "/added.glb"})
	},
	"Points": func(ir *SceneIR) {
		ir.Points = append(ir.Points, PointsIR{ID: "added-points", Count: 1, Positions: []float64{0, 1, 2}})
	},
	"InstancedMeshes": func(ir *SceneIR) {
		ir.InstancedMeshes = append(ir.InstancedMeshes, InstancedMeshIR{ID: "added-batch", Kind: "box", Count: 1, Transforms: make([]float64, 16)})
	},
	"InstancedGLBMeshes": func(ir *SceneIR) {
		ir.InstancedGLBMeshes = append(ir.InstancedGLBMeshes, InstancedGLBMeshIR{ID: "added-glb", Src: "/added.glb", Instances: []MeshInstanceIR{{X: 1}}})
	},
	"ComputeParticles": func(ir *SceneIR) {
		ir.ComputeParticles = append(ir.ComputeParticles, ComputeParticlesIR{ID: "added-compute", Count: 8})
	},
	"WaterSystems": func(ir *SceneIR) {
		ir.WaterSystems = append(ir.WaterSystems, WaterSystemIR{ID: "added-water"})
	},
	"Animations": func(ir *SceneIR) {
		ir.Animations = append(ir.Animations, AnimationClipIR{Name: "added-clip", Duration: 2})
	},
	"Labels":  func(ir *SceneIR) { ir.Labels = append(ir.Labels, LabelIR{ID: "added-label", Text: "hello"}) },
	"Sprites": func(ir *SceneIR) { ir.Sprites = append(ir.Sprites, SpriteIR{ID: "added-sprite", Src: "/s.png"}) },
	"HTML":    func(ir *SceneIR) { ir.HTML = append(ir.HTML, HTMLIR{ID: "added-html"}) },
	"Lights":  func(ir *SceneIR) { ir.Lights = append(ir.Lights, LightIR{ID: "added-light", Kind: "point"}) },
	"Environment": func(ir *SceneIR) {
		ir.Environment.AmbientColor = "#123456"
		ir.Environment.AmbientIntensity = 0.42
	},
	"PostEffects":        func(ir *SceneIR) { ir.PostEffects = append(ir.PostEffects, BloomIR{Threshold: 0.55}) },
	"PostFXMaxPixels":    func(ir *SceneIR) { ir.PostFXMaxPixels = PostFXMaxPixels720p },
	"ShadowMaxPixels":    func(ir *SceneIR) { ir.ShadowMaxPixels = 4096 },
	"QualityLadder":      func(ir *SceneIR) { ir.QualityLadder = append(ir.QualityLadder, QualityRungIR{Name: "low"}) },
	"QualityStartRung":   func(ir *SceneIR) { ir.QualityStartRung = 2 },
	"PointQualityGroups": func(ir *SceneIR) { ir.PointQualityGroups = map[string]string{"dust": "props"} },
	"BackendCaps": func(ir *SceneIR) {
		ir.BackendCaps = &capability.BackendCaps{Capable: []capability.Backend{"webgl"}}
	},
	"ShaderLib":             func(ir *SceneIR) { ir.ShaderLib = map[string]string{"sl:0123456789abcdef": "fn main() {}"} },
	"SpinTracks":            func(ir *SceneIR) { ir.SpinTracks = append(ir.SpinTracks, motion.Track{Prop: "rotation"}) },
	"MotionProgram":         func(ir *SceneIR) { ir.MotionProgram = []byte{1, 2, 3} },
	"MaterialTracks":        func(ir *SceneIR) { ir.MaterialTracks = append(ir.MaterialTracks, motion.Track{Prop: "color"}) },
	"MaterialMotionProgram": func(ir *SceneIR) { ir.MaterialMotionProgram = []byte{4, 5, 6} },
}

// baseDiffScene is the scene every field mutation starts from. It carries one
// record of the collections a mutation appends to, so a mutation adds rather than
// creates, and the diff has to notice a change inside a populated collection.
func baseDiffScene() SceneIR {
	return SceneIR{
		Schema:  SceneIRSchema,
		Objects: []ObjectIR{{ID: "base-object", Kind: "sphere", Radius: 1}},
		Lights:  []LightIR{{ID: "base-light", Kind: "ambient"}},
	}
}

// TestSceneIRFieldPoliciesCoverEveryField walks SceneIR by reflection and fails
// when a field is neither diffed nor recorded as deliberately excluded.
//
// It exists because nine fields were silently undiffable: DiffCommands emitted
// nothing when only one of them changed, so a collaborator's edit to the quality
// ladder or the motion program disappeared with no diagnostic. A comment would
// not have caught the tenth field. This does.
func TestSceneIRFieldPoliciesCoverEveryField(t *testing.T) {
	sceneType := reflect.TypeFor[SceneIR]()
	fields := make(map[string]bool, sceneType.NumField())
	for index := range sceneType.NumField() {
		name := sceneType.Field(index).Name
		fields[name] = true

		policy, ok := sceneIRFieldPolicies[name]
		if !ok {
			t.Errorf("SceneIR field %s has no entry in sceneIRFieldPolicies: decide whether the diff emits it, "+
				"reports it in Diff.RemountFields, or ignores it as derived", name)
			continue
		}
		switch policy.policy {
		case sceneIRDiffed:
			if policy.changed != nil {
				t.Errorf("field %s is diffed and must not declare a changed function", name)
			}
		case sceneIRRemount:
			if policy.changed == nil {
				t.Errorf("field %s is reported and needs a changed function, or DiffScene cannot detect it", name)
			}
			if policy.reason == "" {
				t.Errorf("field %s is reported and needs a reason", name)
			}
		case sceneIRDerived:
			if policy.reason == "" {
				t.Errorf("field %s is ignored and needs a reason", name)
			}
			if policy.changed != nil {
				t.Errorf("field %s is ignored and must not declare a changed function", name)
			}
		}
		if _, ok := sceneIRFieldMutators[name]; !ok {
			t.Errorf("SceneIR field %s has no mutator, so its policy is never proven against the code", name)
		}
	}

	for name := range sceneIRFieldPolicies {
		if !fields[name] {
			t.Errorf("sceneIRFieldPolicies names %s, which is not a SceneIR field", name)
		}
	}
	for name := range sceneIRFieldMutators {
		if !fields[name] {
			t.Errorf("sceneIRFieldMutators names %s, which is not a SceneIR field", name)
		}
	}
}

// TestSceneIRFieldPolicyMatchesDiffBehavior changes one field at a time and
// checks the diff does what the policy claims. It reports every mismatch rather
// than stopping at the first, because the interesting failure is the set of
// fields that drifted, not the alphabetically first one.
func TestSceneIRFieldPolicyMatchesDiffBehavior(t *testing.T) {
	names := make([]string, 0, len(sceneIRFieldMutators))
	for name := range sceneIRFieldMutators {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			policy, ok := sceneIRFieldPolicies[name]
			if !ok {
				t.Skip("no policy; TestSceneIRFieldPoliciesCoverEveryField reports it")
			}
			previous := baseDiffScene()
			next := baseDiffScene()
			sceneIRFieldMutators[name](&next)

			// A diffed or reported field has to reach the wire, or the case proves
			// nothing. A derived field must NOT reach the wire: that is what makes
			// it safe to ignore. So mislabeling authored state as derived fails
			// here, instead of passing because nothing looked for it.
			changedOnWire := !sceneRecordJSONEqual(previous, next)
			if policy.policy == sceneIRDerived {
				if changedOnWire {
					t.Fatalf("field %s is declared derived, and changing it changes the payload; "+
						"authored state has to be diffed or reported", name)
				}
			} else if !changedOnWire {
				t.Fatalf("the mutator for %s left the payload unchanged on the wire", name)
			}

			diff := DiffScene(previous, next, DiffOptions{})
			switch policy.policy {
			case sceneIRDiffed:
				if len(diff.Commands) == 0 {
					t.Errorf("field %s is declared diffed and produced no command", name)
				}
				if len(diff.RemountFields) != 0 {
					t.Errorf("field %s is declared diffed and reported remount fields %v", name, diff.RemountFields)
				}
			case sceneIRRemount:
				if len(diff.Commands) != 0 {
					t.Errorf("field %s is declared unreachable by any command kind, yet produced %d commands: %#v",
						name, len(diff.Commands), diff.Commands)
				}
				if len(diff.RemountFields) != 1 || diff.RemountFields[0] != name {
					t.Errorf("field %s changed and Diff.RemountFields = %v, want exactly [%s]", name, diff.RemountFields, name)
				}
			case sceneIRDerived:
				if len(diff.Commands) != 0 {
					t.Errorf("field %s is declared derived and produced %d commands", name, len(diff.Commands))
				}
				if len(diff.RemountFields) != 0 {
					t.Errorf("field %s is declared derived and reported remount fields %v", name, diff.RemountFields)
				}
			}

			// The reverse direction has to report the same field. A change that
			// only registers in one direction is still a lost change for the peer
			// that receives the other one.
			reverse := DiffScene(next, previous, DiffOptions{})
			if policy.policy == sceneIRRemount {
				if len(reverse.RemountFields) != 1 || reverse.RemountFields[0] != name {
					t.Errorf("field %s reverse Diff.RemountFields = %v, want exactly [%s]", name, reverse.RemountFields, name)
				}
			}
		})
	}
}

// TestDiffSceneReportsEveryChangedRemountFieldTogether proves the report is a set
// and not the first hit. A caller that resends the scene needs to know every
// field it must resend, and a truncated list invites a partial fix.
func TestDiffSceneReportsEveryChangedRemountFieldTogether(t *testing.T) {
	previous := baseDiffScene()
	next := baseDiffScene()
	var want []string
	for name, policy := range sceneIRFieldPolicies {
		if policy.policy != sceneIRRemount {
			continue
		}
		sceneIRFieldMutators[name](&next)
		want = append(want, name)
	}
	sort.Strings(want)

	diff := DiffScene(previous, next, DiffOptions{})
	if !reflect.DeepEqual(diff.RemountFields, want) {
		t.Errorf("Diff.RemountFields =\n %v\nwant\n %v", diff.RemountFields, want)
	}
	if len(diff.Commands) != 0 {
		t.Errorf("no record changed, yet the diff produced %d commands: %#v", len(diff.Commands), diff.Commands)
	}
	// Sorted, so two peers comparing lists compare the same list.
	if !sort.StringsAreSorted(diff.RemountFields) {
		t.Errorf("Diff.RemountFields is not sorted: %v", diff.RemountFields)
	}
}

// TestDiffSceneReportsNothingForIdenticalScenes guards the other direction. A
// report that fired on every call would train a caller to ignore it.
func TestDiffSceneReportsNothingForIdenticalScenes(t *testing.T) {
	scene := baseDiffScene()
	scene.ShadowMaxPixels = 2048
	scene.QualityStartRung = 1
	scene.MotionProgram = []byte{9, 9}
	diff := DiffScene(scene, scene, DiffOptions{})
	if len(diff.RemountFields) != 0 {
		t.Errorf("identical scenes reported remount fields %v", diff.RemountFields)
	}
	if len(diff.Commands) != 0 {
		t.Errorf("identical scenes produced %d commands", len(diff.Commands))
	}
}

// TestDiffSceneReportsNothingForAMovedMeshOnTheTypedPath is the anti-noise case.
// Diff.RemountFields has to stay quiet on the common authoring path, or a caller
// learns to ignore it and the report buys nothing.
//
// Two lowered Props values carry a computed BackendCaps verdict, a schema, and a
// shadow budget. All three are reported fields, and all three have to agree
// between two scenes the same build lowered, so moving a mesh must produce
// commands and an empty report.
func TestDiffSceneReportsNothingForAMovedMeshOnTheTypedPath(t *testing.T) {
	previous := Props{Graph: NewGraph(Mesh{
		ID: "box", Geometry: BoxGeometry{Width: 1, Height: 1, Depth: 1}, Position: Vec3(1, 0, 0),
	})}
	next := Props{Graph: NewGraph(Mesh{
		ID: "box", Geometry: BoxGeometry{Width: 1, Height: 1, Depth: 1}, Position: Vec3(2, 0, 0),
	})}

	lowered := previous.SceneIR()
	loweredNext := next.SceneIR()
	if lowered.BackendCaps == nil {
		t.Fatal("the typed path stopped computing BackendCaps; this test no longer covers it")
	}
	// Props.SceneIR leaves Schema empty; the wire emitter stamps it. Stamp both
	// sides the way the emitter does, so the reported Schema field is covered too.
	lowered.Schema, loweredNext.Schema = SceneIRSchema, SceneIRSchema

	diff := DiffScene(lowered, loweredNext, DiffOptions{})
	if len(diff.RemountFields) != 0 {
		t.Errorf("moving one mesh reported %v; a report that fires on every edit teaches a caller to ignore it",
			diff.RemountFields)
	}
	if len(diff.Commands) != 2 {
		t.Errorf("commands = %#v, want a remove plus a create", diff.Commands)
	}

	patched := DiffScene(lowered, loweredNext, DiffOptions{PatchTransforms: true})
	if len(patched.Commands) != 1 || patched.Commands[0].Kind != CommandSetTransform {
		t.Errorf("commands with the patch option = %#v, want one transform patch", patched.Commands)
	}
	if len(patched.RemountFields) != 0 {
		t.Errorf("the patch path reported %v", patched.RemountFields)
	}
}

// TestCommandKindOriginsMatchDiffOutput walks every declared CommandKind and
// proves the origin table matches what the code emits. Three kinds are declared
// and unreachable from any Diff function, and the table has to say so in a way a
// test can check, or the next reader has to grep to find out.
func TestCommandKindOriginsMatchDiffOutput(t *testing.T) {
	for kind := CommandKind(0); kind < commandKindCount; kind++ {
		entry, ok := commandKindOrigins[kind]
		if !ok {
			t.Errorf("CommandKind %d has no entry in commandKindOrigins", kind)
			continue
		}
		if entry.origin != originDiffScene && entry.origin != originDiffIR && entry.reason == "" {
			t.Errorf("CommandKind %d is not emitted by a plain diff and needs a reason", kind)
		}
	}
	for kind := range commandKindOrigins {
		if kind < 0 || kind >= commandKindCount {
			t.Errorf("commandKindOrigins names CommandKind %d, which is not declared", kind)
		}
	}

	emittedByDefault := map[CommandKind]bool{}
	emittedWithOptions := map[CommandKind]bool{}
	for _, command := range diffKindScenarioCommands(t, DiffOptions{}) {
		emittedByDefault[command.Kind] = true
	}
	for _, command := range diffKindScenarioCommands(t, DiffOptions{PatchTransforms: true}) {
		emittedWithOptions[command.Kind] = true
	}

	for kind := CommandKind(0); kind < commandKindCount; kind++ {
		entry := commandKindOrigins[kind]
		switch entry.origin {
		case originDiffScene, originDiffIR:
			if !emittedByDefault[kind] {
				t.Errorf("CommandKind %d claims a diff emits it, and no scenario produced it", kind)
			}
		case originDiffSceneOption:
			if emittedByDefault[kind] {
				t.Errorf("CommandKind %d is opt-in, and the default diff emitted it", kind)
			}
			if !emittedWithOptions[kind] {
				t.Errorf("CommandKind %d claims an option emits it, and the option did not", kind)
			}
		case originConstructorOnly:
			if emittedByDefault[kind] || emittedWithOptions[kind] {
				t.Errorf("CommandKind %d is recorded as constructor-only, and a diff emitted it", kind)
			}
		}
	}
}

// diffKindScenarioCommands runs every diff entry point over scenes that change
// every diffable field, and returns the union of the commands.
func diffKindScenarioCommands(t *testing.T, options DiffOptions) []Command {
	t.Helper()
	previous := SceneIR{
		Schema:  SceneIRSchema,
		Objects: []ObjectIR{{ID: "kept", Kind: "box"}, {ID: "dropped", Kind: "box"}},
	}
	next := SceneIR{
		Schema:             SceneIRSchema,
		Objects:            []ObjectIR{{ID: "kept", Kind: "box", X: 3}},
		Models:             []ModelIR{{ObjectIR: ObjectIR{ID: "model"}, Src: "/m.glb"}},
		Points:             []PointsIR{{ID: "points", Count: 1, Positions: []float64{0, 0, 0}}},
		InstancedMeshes:    []InstancedMeshIR{{ID: "batch", Kind: "box", Count: 1, Transforms: make([]float64, 16)}},
		InstancedGLBMeshes: []InstancedGLBMeshIR{{ID: "glb", Src: "/g.glb", Instances: []MeshInstanceIR{{X: 1}}}},
		Animations:         []AnimationClipIR{{Name: "idle", Duration: 1}},
		Environment:        EnvironmentIR{AmbientColor: "#222222"},
		PostEffects:        []PostEffectIR{BloomIR{Threshold: 0.5}},
	}
	commands := DiffScene(previous, next, options).Commands
	commands = append(commands, DiffIRCommands(
		IR{Camera: IRCamera{Kind: "perspective"}},
		IR{
			Camera:      IRCamera{Kind: "orthographic"},
			Environment: IREnvironment{AmbientColor: "#333333"},
			Materials:   []IRMaterial{{Name: "steel"}},
		},
	)...)
	if len(commands) == 0 {
		t.Fatal("the scenario produced no command; it proves nothing")
	}
	return commands
}
