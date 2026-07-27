package scene

import (
	"encoding/json"
	"fmt"
	"sort"
	"testing"
)

// benchObjectIR builds one representative lowered mesh record. The field mix
// matches what a real authored mesh lowers to: geometry size, a material, a
// transform, and a few flags. A record that only carries an ID would make the
// comparison benchmark report a cost no real scene pays.
func benchObjectIR(index int) ObjectIR {
	opacity := 0.85
	emissive := 0.2
	pickable := true
	visible := true
	return ObjectIR{
		ID:            fmt.Sprintf("mesh-%05d", index),
		Kind:          "box",
		Width:         1.5,
		Height:        2.25,
		Depth:         0.75,
		Segments:      24,
		MaterialKind:  "standard",
		Color:         "#4488ccff",
		Texture:       "/assets/crate.png",
		Opacity:       &opacity,
		Emissive:      &emissive,
		BlendMode:     "normal",
		Pickable:      &pickable,
		Visible:       &visible,
		CastShadow:    true,
		ReceiveShadow: true,
		Roughness:     0.4,
		Metalness:     0.1,
		NormalMap:     "/assets/crate-normal.png",
		RoughnessMap:  "/assets/crate-rough.png",
		X:             float64(index%97) * 1.5,
		Y:             float64(index%13) * 0.5,
		Z:             float64(index%53) * -2,
		RotationX:     0.1,
		RotationY:     0.2,
		RotationZ:     0.3,
		ScaleX:        1.25,
		ScaleY:        1.25,
		ScaleZ:        1.25,
		QualityGroup:  "props",
	}
}

// benchDiffScenes returns two scenes of the given object count where the first
// movedCount objects have a new position. Everything else is identical, so the
// benchmark measures the cost the diff pays for records that did NOT change.
func benchDiffScenes(objects, movedCount int) (SceneIR, SceneIR) {
	previous := SceneIR{Schema: SceneIRSchema, Objects: make([]ObjectIR, objects)}
	next := SceneIR{Schema: SceneIRSchema, Objects: make([]ObjectIR, objects)}
	for i := range objects {
		previous.Objects[i] = benchObjectIR(i)
		record := benchObjectIR(i)
		if i < movedCount {
			record.X += 0.5
			record.Y += 0.25
			record.Z -= 0.125
		}
		next.Objects[i] = record
	}
	return previous, next
}

// BenchmarkReferenceDiffCommands5000Objects10PercentMoved measures the original
// algorithm, which marshaled both records for every record in the scene. It stays
// in the repository so the before and after numbers come from one run on one
// machine, instead of two runs a reader has to trust.
func BenchmarkReferenceDiffCommands5000Objects10PercentMoved(b *testing.B) {
	previous, next := benchDiffScenes(5000, 500)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		commands := referenceDiffCommands(previous, next)
		if len(commands) != 1000 {
			b.Fatalf("commands = %d, want 1000", len(commands))
		}
	}
}

func BenchmarkReferenceDiffCommands5000ObjectsUnchanged(b *testing.B) {
	previous, next := benchDiffScenes(5000, 0)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if commands := referenceDiffCommands(previous, next); len(commands) != 0 {
			b.Fatalf("commands = %d, want 0", len(commands))
		}
	}
}

func BenchmarkReferenceDiffCommands5000ObjectsAllMoved(b *testing.B) {
	previous, next := benchDiffScenes(5000, 5000)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		commands := referenceDiffCommands(previous, next)
		if len(commands) != 10000 {
			b.Fatalf("commands = %d, want 10000", len(commands))
		}
	}
}

func BenchmarkDiffCommands5000Objects10PercentMoved(b *testing.B) {
	previous, next := benchDiffScenes(5000, 500)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		commands := DiffCommands(previous, next)
		if len(commands) != 1000 {
			b.Fatalf("commands = %d, want 1000", len(commands))
		}
	}
}

func BenchmarkDiffCommands5000ObjectsUnchanged(b *testing.B) {
	previous, next := benchDiffScenes(5000, 0)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if commands := DiffCommands(previous, next); len(commands) != 0 {
			b.Fatalf("commands = %d, want 0", len(commands))
		}
	}
}

func BenchmarkDiffCommands5000ObjectsAllMoved(b *testing.B) {
	previous, next := benchDiffScenes(5000, 5000)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		commands := DiffCommands(previous, next)
		if len(commands) != 10000 {
			b.Fatalf("commands = %d, want 10000", len(commands))
		}
	}
}

func BenchmarkDiffScenePatchTransforms5000Objects10PercentMoved(b *testing.B) {
	previous, next := benchDiffScenes(5000, 500)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		diff := DiffScene(previous, next, DiffOptions{PatchTransforms: true})
		if len(diff.Commands) != 500 {
			b.Fatalf("commands = %d, want 500", len(diff.Commands))
		}
	}
}

// BenchmarkSceneRecordEqual measures one record comparison in isolation, for both
// answers. The equal case is the one a real edit pays for most records, so it is
// the case the fast path must win. The changed case pays the fast path and then
// the encoder, and the diff-level benchmarks show that trade is worth it.
func BenchmarkSceneRecordEqual(b *testing.B) {
	same := benchObjectIR(7)
	other := benchObjectIR(7)
	moved := benchObjectIR(7)
	moved.X += 0.5

	b.Run("equal/marshal", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if !referenceMarshalEqual(same, other) {
				b.Fatal("want equal")
			}
		}
	})
	b.Run("equal/current", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if !sceneRecordPointerJSONEqual(&same, &other) {
				b.Fatal("want equal")
			}
		}
	})
	b.Run("changed/marshal", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if referenceMarshalEqual(same, moved) {
				b.Fatal("want unequal")
			}
		}
	})
	b.Run("changed/current", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if sceneRecordPointerJSONEqual(&same, &moved) {
				b.Fatal("want unequal")
			}
		}
	})
}

// referenceMarshalEqual is the original sceneRecordJSONEqual implementation. It
// marshals both values and compares the bytes. Every new comparison must agree
// with it; see TestSceneRecordEqualAgreesWithMarshalComparison.
func referenceMarshalEqual[T any](a, b T) bool {
	aj, err := json.Marshal(a)
	if err != nil {
		return false
	}
	bj, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return string(aj) == string(bj)
}

// referenceDiffCommands is the original DiffCommands, kept verbatim except for
// the comparison it calls. It is the baseline for the benchmarks above, and
// TestDiffCommandsMatchesTheReferenceAlgorithm requires the live diff to produce
// the same commands, so the speedup cannot come from doing less work.
func referenceDiffCommands(previous, next SceneIR) []Command {
	var commands []Command
	referenceDiffSceneRecords(&commands, previous.Objects, next.Objects, func(record ObjectIR) string {
		return record.ID
	}, CreateObjectCommand)
	referenceDiffSceneRecords(&commands, previous.Labels, next.Labels, func(record LabelIR) string {
		return record.ID
	}, CreateLabelCommand)
	referenceDiffSceneRecords(&commands, previous.Sprites, next.Sprites, func(record SpriteIR) string {
		return record.ID
	}, CreateSpriteCommand)
	referenceDiffSceneRecords(&commands, previous.HTML, next.HTML, func(record HTMLIR) string {
		return record.ID
	}, CreateHTMLCommand)
	referenceDiffSceneRecords(&commands, previous.Lights, next.Lights, func(record LightIR) string {
		return record.ID
	}, CreateLightCommand)
	if !referenceMarshalEqual(previous.Environment, next.Environment) {
		commands = append(commands, SetEnvironmentCommand(next.Environment))
	}
	if !referenceMarshalEqual(previous.Models, next.Models) {
		commands = append(commands, SetModelsCommand(next.Models))
	}
	if !referenceMarshalEqual(previous.Points, next.Points) || !referenceMarshalEqual(previous.ComputeParticles, next.ComputeParticles) || !referenceMarshalEqual(previous.WaterSystems, next.WaterSystems) {
		commands = append(commands, SetParticlesCommand(next.Points, next.ComputeParticles, next.WaterSystems))
	}
	if !referenceMarshalEqual(previous.InstancedMeshes, next.InstancedMeshes) {
		commands = append(commands, SetInstancedMeshesCommand(next.InstancedMeshes))
	}
	if !referenceMarshalEqual(previous.InstancedGLBMeshes, next.InstancedGLBMeshes) {
		commands = append(commands, SetInstancedGLBMeshesCommand(next.InstancedGLBMeshes))
	}
	if !referenceMarshalEqual(previous.Animations, next.Animations) {
		commands = append(commands, SetAnimationsCommand(next.Animations))
	}
	if !referenceMarshalEqual(previous.PostEffects, next.PostEffects) || previous.PostFXMaxPixels != next.PostFXMaxPixels {
		commands = append(commands, SetPostEffectsCommand(next.PostEffects, next.PostFXMaxPixels))
	}
	return commands
}

func referenceDiffSceneRecords[T any](commands *[]Command, previous, next []T, id func(T) string, create func(T) Command) {
	prevByID := make(map[string]T, len(previous))
	nextByID := make(map[string]T, len(next))
	for _, record := range previous {
		if recordID := id(record); recordID != "" {
			prevByID[recordID] = record
		}
	}
	for _, record := range next {
		if recordID := id(record); recordID != "" {
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
		if existed && referenceMarshalEqual(previousRecord, record) {
			continue
		}
		if existed {
			*commands = append(*commands, RemoveObjectCommand(recordID))
		}
		*commands = append(*commands, create(record))
	}
}
