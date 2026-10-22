package scene

import (
	"encoding/json"
	"math/rand/v2"
	"reflect"
	"testing"
)

func TestSceneRecordIndexDuplicateAndEmptyIDsMatchReference(t *testing.T) {
	generator := &recordGenerator{rng: rand.New(rand.NewPCG(0x1d, 0x51ce))}
	// Repeated and empty identities exercise every record collection independently.
	ids := []string{"", "a", "a", "z", "z", "", "β", "bad\xff", "a", "β"}
	for iteration := range 1000 {
		previous, next := randomDiffScene(generator, ids), randomDiffScene(generator, ids)
		beforePrevious, err := json.Marshal(previous)
		if err != nil {
			t.Fatal(err)
		}
		beforeNext, err := json.Marshal(next)
		if err != nil {
			t.Fatal(err)
		}
		want, err := MarshalCommands(referenceDiffCommands(previous, next))
		if err != nil {
			t.Fatal(err)
		}
		got, err := MarshalCommands(DiffCommands(previous, next))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Fatalf("iteration%d: got%s want%s", iteration, got, want)
		}
		afterPrevious, _ := json.Marshal(previous)
		afterNext, _ := json.Marshal(next)
		if string(beforePrevious) != string(afterPrevious) || string(beforeNext) != string(afterNext) {
			t.Fatalf("iteration%d mutated input", iteration)
		}
	}
}

func TestSceneRecordIndexKeepsOrderAndPatchModes(t *testing.T) {
	old := ObjectIR{ID: "a", Kind: "box", X: 1}
	first := old
	first.X = 0
	previous := SceneIR{Objects: []ObjectIR{{ID: "z"}, first, {ID: "b"}, old, {ID: "", X: 99}}}
	moved := old
	moved.X = 2
	created := ObjectIR{ID: "new", Kind: "box"}
	// The first a must match the last previous a; later a records must still
	// compare with that same snapshot, rather than consuming its index entry.
	next := SceneIR{Objects: []ObjectIR{old, moved, first, created, {ID: "", X: 100}}}
	transform := func(x float64) TransformPatch { return TransformPatch{X: x, ScaleX: 1, ScaleY: 1, ScaleZ: 1} }
	for _, test := range []struct {
		name    string
		options DiffOptions
		want    []Command
	}{
		{
			"replace", DiffOptions{}, []Command{
				RemoveObjectCommand("b"), RemoveObjectCommand("z"),
				RemoveObjectCommand("a"), CreateObjectCommand(moved),
				RemoveObjectCommand("a"), CreateObjectCommand(first),
				CreateObjectCommand(created),
			},
		},
		{
			"transform", DiffOptions{PatchTransforms: true}, []Command{
				RemoveObjectCommand("b"), RemoveObjectCommand("z"),
				SetTransformCommand("a", transform(2)), SetTransformCommand("a", transform(0)),
				CreateObjectCommand(created),
			},
		},
		{
			"state", DiffOptions{PatchObjectState: true}, []Command{
				RemoveObjectCommand("b"), RemoveObjectCommand("z"),
				{Kind: CommandSetTransform, ObjectID: "a", Data: ObjectStatePatch{TransformPatch: transform(2), Visible: true}},
				{Kind: CommandSetTransform, ObjectID: "a", Data: ObjectStatePatch{TransformPatch: transform(0), Visible: true}},
				CreateObjectCommand(created),
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := DiffScene(previous, next, test.options)
			if !reflect.DeepEqual(got.Commands, test.want) || len(got.RemountFields) != 0 {
				t.Fatalf("got%#v want%#v", got, test.want)
			}
		})
	}
}
