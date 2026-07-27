package scene3d

import (
	"testing"

	"m31labs.dev/gosx/crdt"
	"m31labs.dev/gosx/scene"
)

// FuzzIncrementalStreamMatchesFullRediff is the differential oracle as a fuzz
// target. It drives a scene through a random edit script two ways:
//
//   - Incrementally: diff each state against the previous one and apply the
//     commands into one long-lived document.
//   - From scratch: diff the final state against an empty scene and apply the
//     commands into a fresh document.
//
// Both documents must describe the same scene. Any command the incremental path
// fails to record, or records wrongly, shows up as a mismatch.
//
// It follows FuzzIslandReuseMatchesFullEval in client/vm: an incremental path
// earns trust only when a full recomputation agrees with it.
func FuzzIncrementalStreamMatchesFullRediff(f *testing.F) {
	f.Add([]byte{0x01, 0x02, 0x03})
	f.Add([]byte{0x11, 0x22, 0x33, 0x44, 0x55})
	f.Add([]byte{0xff, 0x00, 0xaa, 0x55, 0x10, 0x20, 0x30})
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, script []byte) {
		if len(script) > 64 {
			script = script[:64]
		}
		incremental, err := Bind(crdt.NewDoc(), "scene")
		if err != nil {
			t.Fatal(err)
		}

		previous := scene.SceneIR{}
		state := newFuzzScene()
		for _, step := range script {
			state.step(step)
			next := state.ir()
			if _, _, err := incremental.DiffScene(previous, next, "step"); err != nil {
				t.Fatalf("incremental diff: %v", err)
			}
			previous = next
		}

		fresh, err := Bind(crdt.NewDoc(), "scene")
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := fresh.DiffScene(scene.SceneIR{}, previous, "full"); err != nil {
			t.Fatalf("full diff: %v", err)
		}

		wantView, err := fresh.View()
		if err != nil {
			t.Fatalf("fresh view: %v", err)
		}
		gotView, err := incremental.View()
		if err != nil {
			t.Fatalf("incremental view: %v", err)
		}
		want, err := wantView.Visible().Canonical()
		if err != nil {
			t.Fatal(err)
		}
		got, err := gotView.Visible().Canonical()
		if err != nil {
			t.Fatal(err)
		}
		if string(want) != string(got) {
			t.Fatalf("the incremental stream drifted from a full re-diff\n want %s\n  got %s", want, got)
		}

		// The bootstrap stream a joining peer receives must also match.
		wantCommands, err := fresh.Commands()
		if err != nil {
			t.Fatal(err)
		}
		gotCommands, err := incremental.Commands()
		if err != nil {
			t.Fatal(err)
		}
		wantJSON, err := scene.MarshalCommands(wantCommands)
		if err != nil {
			t.Fatal(err)
		}
		gotJSON, err := scene.MarshalCommands(gotCommands)
		if err != nil {
			t.Fatal(err)
		}
		if string(wantJSON) != string(gotJSON) {
			t.Fatalf("the bootstrap streams differ\n want %s\n  got %s", wantJSON, gotJSON)
		}
	})
}

// FuzzTwoPeersConvergeOnTheSameScene drives two peers through two random edit
// scripts while partitioned, merges them in both directions, and requires the
// same scene on both sides.
func FuzzTwoPeersConvergeOnTheSameScene(f *testing.F) {
	f.Add([]byte{0x01, 0x02}, []byte{0x03, 0x04})
	f.Add([]byte{0x10, 0x11, 0x12}, []byte{0x10, 0x11, 0x13})
	f.Add([]byte{0xaa}, []byte{0xaa})

	f.Fuzz(func(t *testing.T, leftScript, rightScript []byte) {
		if len(leftScript) > 32 {
			leftScript = leftScript[:32]
		}
		if len(rightScript) > 32 {
			rightScript = rightScript[:32]
		}
		seed := seedScene()
		server, _ := bindSeeded(t, seed)
		left := mergeFork(t, server)
		right := mergeFork(t, server)

		runScript(t, left, seed, leftScript)
		runScript(t, right, seed, rightScript)

		if err := left.Doc().Merge(right.Doc()); err != nil {
			t.Fatalf("left merge: %v", err)
		}
		if err := right.Doc().Merge(left.Doc()); err != nil {
			t.Fatalf("right merge: %v", err)
		}

		leftView, err := left.View()
		if err != nil {
			t.Fatalf("left view: %v", err)
		}
		rightView, err := right.View()
		if err != nil {
			t.Fatalf("right view: %v", err)
		}
		leftJSON, err := leftView.Canonical()
		if err != nil {
			t.Fatal(err)
		}
		rightJSON, err := rightView.Canonical()
		if err != nil {
			t.Fatal(err)
		}
		if string(leftJSON) != string(rightJSON) {
			t.Fatalf("peers diverged\n left %s\nright %s", leftJSON, rightJSON)
		}
		// The seed light is never removed by any script step, so an empty scene
		// means the merge destroyed state instead of converging.
		if leftView.Empty() {
			t.Fatal("both peers converged on an EMPTY scene")
		}
	})
}

// fuzzScene is a tiny scene model an edit script drives. It covers creation,
// movement, removal, re-creation, and two whole-collection slots.
type fuzzScene struct {
	objects map[string]scene.ObjectIR
	labels  map[string]scene.LabelIR
	env     scene.EnvironmentIR
	meshes  []scene.InstancedMeshIR
}

func newFuzzScene() *fuzzScene {
	return &fuzzScene{
		objects: map[string]scene.ObjectIR{},
		labels:  map[string]scene.LabelIR{},
	}
}

// fuzzObjectIDs is a fixed pool, so two independent scripts collide on the same
// object often enough to exercise the conflict rules.
var fuzzObjectIDs = []string{"a", "b", "c", "d"}

func (f *fuzzScene) step(code byte) {
	id := fuzzObjectIDs[int(code>>2)%len(fuzzObjectIDs)]
	switch code & 0x03 {
	case 0:
		f.objects[id] = scene.ObjectIR{
			ID:    id,
			Kind:  "box",
			Width: 1 + float64(code%3),
			Color: "#112233",
		}
	case 1:
		record, ok := f.objects[id]
		if !ok {
			f.labels[id+"-tag"] = scene.LabelIR{ID: id + "-tag", Text: id}
			return
		}
		record.X += float64(code%5) - 2
		f.objects[id] = record
	case 2:
		delete(f.objects, id)
		delete(f.labels, id+"-tag")
	default:
		f.env.Exposure = 1 + float64(code%4)/4
		f.env.AmbientColor = "#0a0a0a"
		f.meshes = []scene.InstancedMeshIR{{ID: "batch", Kind: "box", Count: int(code) + 1}}
	}
}

func (f *fuzzScene) ir() scene.SceneIR {
	out := scene.SceneIR{Environment: f.env, InstancedMeshes: f.meshes}
	for _, id := range sortedObjectKeys(f.objects) {
		out.Objects = append(out.Objects, f.objects[id])
	}
	for _, id := range sortedLabelKeys(f.labels) {
		out.Labels = append(out.Labels, f.labels[id])
	}
	return out
}

func sortedObjectKeys(records map[string]scene.ObjectIR) []string {
	keys := make([]string, 0, len(records))
	for key := range records {
		keys = append(keys, key)
	}
	sortStrings(keys)
	return keys
}

func sortedLabelKeys(records map[string]scene.LabelIR) []string {
	keys := make([]string, 0, len(records))
	for key := range records {
		keys = append(keys, key)
	}
	sortStrings(keys)
	return keys
}

// sortStrings is an insertion sort. The key count is tiny, and pulling in the
// sort package for four keys is not worth the import in a test model.
func sortStrings(keys []string) {
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
}

// runScript replays an edit script on one peer, starting from the seed scene.
func runScript(t *testing.T, peer *Doc, seed scene.SceneIR, script []byte) {
	t.Helper()
	state := newFuzzScene()
	for _, record := range seed.Objects {
		state.objects[record.ID] = record
	}
	previous := seed
	for _, code := range script {
		state.step(code)
		next := state.ir()
		next.Lights = seed.Lights
		if _, _, err := peer.DiffScene(previous, next, "step"); err != nil {
			t.Fatalf("diff: %v", err)
		}
		previous = next
	}
}

func bindSeeded(t *testing.T, seed scene.SceneIR) (*Doc, scene.SceneIR) {
	t.Helper()
	bound, err := Bind(crdt.NewDoc(), "scene")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := bound.DiffScene(scene.SceneIR{}, seed, "seed"); err != nil {
		t.Fatal(err)
	}
	return bound, seed
}

func mergeFork(t *testing.T, source *Doc) *Doc {
	t.Helper()
	bound, err := Bind(crdt.NewDoc(), source.Namespace())
	if err != nil {
		t.Fatal(err)
	}
	if err := bound.Doc().Merge(source.Doc()); err != nil {
		t.Fatal(err)
	}
	return bound
}
