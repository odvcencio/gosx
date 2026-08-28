package scene3d

import (
	"bytes"
	"errors"
	"reflect"
	"testing"

	"m31labs.dev/gosx/crdt"
	"m31labs.dev/gosx/scene"
	"m31labs.dev/gosx/scene/capability"
)

func TestDiffSceneRejectsRemountOnlyBeforeMutation(t *testing.T) {
	doc, bound := newBoundDoc(t)
	var watched int
	bound.Watch(func(commands []scene.Command) { watched += len(commands) })

	before, err := doc.Save()
	if err != nil {
		t.Fatal(err)
	}
	commands, hash, err := bound.DiffScene(scene.SceneIR{}, scene.SceneIR{
		Schema: "gosx.scene3d.ir.v2",
	}, "remount-only")
	if err == nil {
		t.Fatal("remount-only diff returned no error")
	}
	if len(commands) != 0 {
		t.Fatalf("remount-only diff returned %d commands, want none", len(commands))
	}
	if hash != (crdt.ChangeHash{}) {
		t.Fatalf("remount-only hash = %s, want zero", hash)
	}
	assertRemountError(t, err, []string{"Schema"})

	after, err := doc.Save()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("remount-only diff changed the document snapshot")
	}
	if watched != 0 {
		t.Fatalf("remount-only diff notified watchers %d times", watched)
	}
	got, err := bound.Commands()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("remount-only diff left %d document commands", len(got))
	}
}

func TestDiffSceneRejectsMixedDiffAtomically(t *testing.T) {
	doc, bound := newBoundDoc(t)
	base := seedScene()
	if _, _, err := bound.DiffScene(scene.SceneIR{}, base, "seed"); err != nil {
		t.Fatal(err)
	}

	var watched int
	bound.Watch(func(commands []scene.Command) { watched += len(commands) })
	watched = 0
	before, err := doc.Save()
	if err != nil {
		t.Fatal(err)
	}

	next := moveObject(base, "cube", 7)
	next.Schema = "gosx.scene3d.ir.v2"
	commands, hash, err := bound.DiffScene(base, next, "mixed")
	if err == nil {
		t.Fatal("mixed diff returned no error")
	}
	if len(commands) != 0 {
		t.Fatalf("mixed diff returned %d commands, want none on rejection", len(commands))
	}
	if hash != (crdt.ChangeHash{}) {
		t.Fatalf("mixed diff hash = %s, want zero", hash)
	}
	assertRemountError(t, err, []string{"Schema"})

	after, err := doc.Save()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("mixed diff changed the document snapshot")
	}
	if watched != 0 {
		t.Fatalf("mixed diff notified watchers %d times", watched)
	}
	view, err := bound.View()
	if err != nil {
		t.Fatal(err)
	}
	if got := objectByID(t, view, "cube").X; got != 0 {
		t.Fatalf("mixed diff moved cube to %v, want seed value 0", got)
	}
}

func TestDiffSceneRemountFieldsAreStable(t *testing.T) {
	next := scene.SceneIR{
		Schema:                "gosx.scene3d.ir.v2",
		ShadowMaxPixels:       4096,
		QualityLadder:         []scene.QualityRungIR{{Name: "low"}},
		QualityStartRung:      2,
		PointQualityGroups:    map[string]string{"dust": "points"},
		BackendCaps:           &capability.BackendCaps{Capable: []capability.Backend{capability.BackendWebGL}},
		ShaderLib:             map[string]string{"sl:0123456789abcdef": "fn main() {}"},
		MotionProgram:         []byte{1, 2, 3},
		MaterialMotionProgram: []byte{4, 5, 6},
	}
	want := []string{
		"BackendCaps",
		"MaterialMotionProgram",
		"MotionProgram",
		"PointQualityGroups",
		"QualityLadder",
		"QualityStartRung",
		"Schema",
		"ShaderLib",
		"ShadowMaxPixels",
	}

	_, bound := newBoundDoc(t)
	for i := 0; i < 2; i++ {
		_, _, err := bound.DiffScene(scene.SceneIR{}, next, "stable")
		if err == nil {
			t.Fatal("remount diff returned no error")
		}
		assertRemountError(t, err, want)
	}
}

func TestApplyPatchAndEmptyDiffRemainUnchanged(t *testing.T) {
	_, bound := newBoundDoc(t)
	var seen []scene.Command
	bound.Watch(func(commands []scene.Command) { seen = append(seen, commands...) })

	create := scene.CreateObjectCommand(scene.ObjectIR{ID: "cube", Kind: "box"})
	if _, err := bound.Apply([]scene.Command{create}, "seed"); err != nil {
		t.Fatal(err)
	}
	seen = nil
	patch := scene.Command{
		Kind:     scene.CommandSetTransform,
		ObjectID: "cube",
		Data:     map[string]any{"x": 4.0, "y": 1.0, "z": 0.0},
	}
	hash, err := bound.Apply([]scene.Command{patch}, "patch")
	if err != nil {
		t.Fatal(err)
	}
	if hash == (crdt.ChangeHash{}) {
		t.Fatal("ordinary patch returned a zero hash")
	}
	assertCommandsEqual(t, "ordinary patch", []scene.Command{patch}, seen)

	seen = nil
	_, hash, err = bound.DiffScene(scene.SceneIR{}, scene.SceneIR{}, "empty")
	if err != nil {
		t.Fatal(err)
	}
	if hash != (crdt.ChangeHash{}) {
		t.Fatalf("empty diff hash = %s, want zero", hash)
	}
	if len(seen) != 0 {
		t.Fatalf("empty diff notified watchers with %d commands", len(seen))
	}
}

func assertRemountError(t *testing.T, err error, want []string) {
	t.Helper()
	var remountErr *RemountRequiredError
	if !errors.As(err, &remountErr) {
		t.Fatalf("error %T = %v, want *RemountRequiredError", err, err)
	}
	if !errors.Is(err, ErrRemountRequired) {
		t.Fatalf("error %v does not wrap ErrRemountRequired", err)
	}
	if !reflect.DeepEqual(remountErr.Fields, want) {
		t.Fatalf("remount fields = %v, want %v", remountErr.Fields, want)
	}
}
