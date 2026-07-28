package scene3d

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"m31labs.dev/gosx/crdt"
	crdtsync "m31labs.dev/gosx/crdt/sync"
	"m31labs.dev/gosx/scene"
)

// seedScene builds a small scene that every convergence test starts from.
func seedScene() scene.SceneIR {
	return scene.SceneIR{
		Objects: []scene.ObjectIR{
			{ID: "cube", Kind: "box", Width: 1, Height: 1, Depth: 1, Color: "#888888"},
			{ID: "ball", Kind: "sphere", Radius: 0.5, Color: "#3366ff"},
		},
		Lights: []scene.LightIR{{ID: "sun", Kind: "directional", Intensity: 1}},
	}
}

// moveObject returns a copy of source with one object placed at x.
func moveObject(source scene.SceneIR, id string, x float64) scene.SceneIR {
	next := source
	next.Objects = append([]scene.ObjectIR(nil), source.Objects...)
	for i := range next.Objects {
		if next.Objects[i].ID == id {
			next.Objects[i].X = x
		}
	}
	return next
}

// dropObject returns a copy of source without one object.
func dropObject(source scene.SceneIR, id string) scene.SceneIR {
	next := source
	next.Objects = nil
	for _, record := range source.Objects {
		if record.ID == id {
			continue
		}
		next.Objects = append(next.Objects, record)
	}
	return next
}

// TestPartitionedPeersConvergeOnTheSameScene is property 1. Two peers edit while
// partitioned, then exchange sync messages, and both must materialize the same
// scene. The comparison is over the materialized View, not the document bytes,
// because two converged documents can hold the same state with a different
// change order.
func TestPartitionedPeersConvergeOnTheSameScene(t *testing.T) {
	base := seedScene()
	server, serverScene := newSeededPeer(t, base)
	left := forkPeer(t, server)
	right := forkPeer(t, server)

	leftNext := moveObject(base, "cube", 5)
	if _, _, err := left.DiffScene(base, leftNext, "left moves the cube"); err != nil {
		t.Fatal(err)
	}
	rightNext := moveObject(base, "ball", -3)
	if _, _, err := right.DiffScene(base, rightNext, "right moves the ball"); err != nil {
		t.Fatal(err)
	}
	_ = serverScene

	syncPair(t, left, right)

	leftView := canonicalView(t, left)
	rightView := canonicalView(t, right)
	if string(leftView) != string(rightView) {
		t.Fatalf("peers diverged\n left %s\nright %s", leftView, rightView)
	}

	// Assert the scene is not empty. Two peers that both threw the scene away
	// also agree, and a comparison alone would pass while proving nothing.
	view, err := left.View()
	if err != nil {
		t.Fatal(err)
	}
	if view.Empty() {
		t.Fatal("converged on an EMPTY scene: the comparison above proves nothing")
	}
	if len(view.IR.Objects) != 2 {
		t.Fatalf("want 2 objects after convergence, got %d", len(view.IR.Objects))
	}
	if got := objectByID(t, view, "cube").X; got != 5 {
		t.Fatalf("cube x = %v, want 5 from the left partition", got)
	}
	if got := objectByID(t, view, "ball").X; got != -3 {
		t.Fatalf("ball x = %v, want -3 from the right partition", got)
	}
}

// TestConcurrentEditsToDifferentObjectsBothSurvive is property 3. It is the case
// a whole-scene snapshot loses, and losing it silently is the failure this
// package exists to prevent. The two edits land on two different document keys,
// so neither can overwrite the other.
func TestConcurrentEditsToDifferentObjectsBothSurvive(t *testing.T) {
	base := seedScene()
	server, _ := newSeededPeer(t, base)
	left := forkPeer(t, server)
	right := forkPeer(t, server)

	if _, _, err := left.DiffScene(base, moveObject(base, "cube", 9), "left"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := right.DiffScene(base, moveObject(base, "ball", 7), "right"); err != nil {
		t.Fatal(err)
	}
	mergeBoth(t, left, right)

	for name, peer := range map[string]*Doc{"left": left, "right": right} {
		view, err := peer.View()
		if err != nil {
			t.Fatal(err)
		}
		if got := objectByID(t, view, "cube").X; got != 9 {
			t.Fatalf("%s: cube x = %v, want 9 — the left edit was lost", name, got)
		}
		if got := objectByID(t, view, "ball").X; got != 7 {
			t.Fatalf("%s: ball x = %v, want 7 — the right edit was lost", name, got)
		}
	}
}

// TestConcurrentEditsToTheSameObjectPickTheGreaterOpID is property 2, first half.
// Two peers move the SAME object at once. The winner is the write with the
// greater crdt.OpID counter. Both peers compare the same two OpID values, so
// both reach the same answer.
//
// The test forces the counters apart: the right peer creates one extra object
// first, which raises its operation counter above the left peer's. The rule
// then predicts the right peer's move.
func TestConcurrentEditsToTheSameObjectPickTheGreaterOpID(t *testing.T) {
	base := seedScene()
	server, _ := newSeededPeer(t, base)
	left := forkPeer(t, server)
	right := forkPeer(t, server)

	if _, _, err := left.DiffScene(base, moveObject(base, "cube", 1), "left move"); err != nil {
		t.Fatal(err)
	}

	// Two extra operations on the right peer lift every later right-peer
	// counter above the left peer's single move.
	withExtra := base
	withExtra.Objects = append(append([]scene.ObjectIR(nil), base.Objects...),
		scene.ObjectIR{ID: "cone", Kind: "cone", Radius: 1})
	if _, _, err := right.DiffScene(base, withExtra, "right adds a cone"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := right.DiffScene(withExtra, moveObject(withExtra, "cube", 2), "right move"); err != nil {
		t.Fatal(err)
	}

	mergeBoth(t, left, right)

	leftView := canonicalView(t, left)
	rightView := canonicalView(t, right)
	if string(leftView) != string(rightView) {
		t.Fatalf("peers diverged on a same-object conflict\n left %s\nright %s", leftView, rightView)
	}
	view, err := left.View()
	if err != nil {
		t.Fatal(err)
	}
	if got := objectByID(t, view, "cube").X; got != 2 {
		t.Fatalf("cube x = %v, want 2: the greater OpID must win", got)
	}
	if len(view.IR.Objects) != 3 {
		t.Fatalf("want 3 objects, got %d: the cone must survive the conflict", len(view.IR.Objects))
	}
}

// TestSameCounterConflictBreaksTiesOnActorID is property 2, second half. When
// two writes carry the same operation counter, the lexicographically greater
// actor ID wins. The test derives the expected winner from the rule at run time,
// because crdt.NewDoc draws a random actor ID.
//
// The scenario runs many times on purpose. A single run has an even chance of
// matching the prediction by luck if the two counters were NOT equal, so one run
// could pass while proving nothing about the tie-break. Twelve runs reduce that
// chance to about one in four thousand.
func TestSameCounterConflictBreaksTiesOnActorID(t *testing.T) {
	for attempt := 0; attempt < 12; attempt++ {
		t.Run(fmt.Sprintf("attempt%02d", attempt), sameCounterConflictAttempt)
	}
}

func sameCounterConflictAttempt(t *testing.T) {
	base := seedScene()
	server, _ := newSeededPeer(t, base)
	left := forkPeer(t, server)
	right := forkPeer(t, server)

	// Both peers start from the same merged history, so both allocate the same
	// next operation counter. Each applies exactly ONE operation: the object is
	// already in the index, so no index insert is added.
	if _, err := left.Apply([]scene.Command{{
		Kind: scene.CommandSetTransform, ObjectID: "cube", Data: map[string]any{"x": 11.0},
	}}, "left"); err != nil {
		t.Fatal(err)
	}
	if _, err := right.Apply([]scene.Command{{
		Kind: scene.CommandSetTransform, ObjectID: "cube", Data: map[string]any{"x": 22.0},
	}}, "right"); err != nil {
		t.Fatal(err)
	}

	leftActor := left.Doc().ActorID().String()
	rightActor := right.Doc().ActorID().String()
	if leftActor == rightActor {
		t.Fatal("the two peers share an actor ID, so the tie-break cannot be observed")
	}
	wantX := 11.0
	if rightActor > leftActor {
		wantX = 22.0
	}

	mergeBoth(t, left, right)
	if string(canonicalView(t, left)) != string(canonicalView(t, right)) {
		t.Fatal("peers diverged on an equal-counter conflict")
	}
	view, err := left.View()
	if err != nil {
		t.Fatal(err)
	}
	raw, ok := view.Transforms["cube"]
	if !ok {
		t.Fatal("the transform patch vanished from both peers")
	}
	var patch struct{ X float64 }
	if err := json.Unmarshal(raw, &patch); err != nil {
		t.Fatal(err)
	}
	if patch.X != wantX {
		t.Fatalf("transform x = %v, want %v (left actor %s, right actor %s)", patch.X, wantX, leftActor, rightActor)
	}
}

// TestRemoveWinsOverConcurrentPatch is property 4. One peer removes an object
// while the other patches its transform. The removal wins, because the removal
// flag and the transform patch are different document keys and the flag stays
// raised. The patch is kept under the flag, so a later re-create restores it.
func TestRemoveWinsOverConcurrentPatch(t *testing.T) {
	base := seedScene()
	server, _ := newSeededPeer(t, base)
	remover := forkPeer(t, server)
	editor := forkPeer(t, server)

	if _, _, err := remover.DiffScene(base, dropObject(base, "cube"), "remove the cube"); err != nil {
		t.Fatal(err)
	}
	if _, err := editor.Apply([]scene.Command{{
		Kind: scene.CommandSetTransform, ObjectID: "cube", Data: map[string]any{"x": 4.0},
	}}, "move the cube"); err != nil {
		t.Fatal(err)
	}

	mergeBoth(t, remover, editor)
	if string(canonicalView(t, remover)) != string(canonicalView(t, editor)) {
		t.Fatal("peers diverged on remove against edit")
	}
	view, err := remover.View()
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range view.IR.Objects {
		if record.ID == "cube" {
			t.Fatal("the removed cube came back: remove must win over a concurrent patch")
		}
	}
	if len(view.IR.Objects) != 1 {
		t.Fatalf("want 1 surviving object, got %d", len(view.IR.Objects))
	}
	if len(view.Removed) != 1 || view.Removed[0] != "cube" {
		t.Fatalf("Removed = %v, want [cube]: the removal must be visible, not merely absent", view.Removed)
	}
	// The concurrent patch is retained under the removal flag. Losing it would
	// make a later re-create drop the edit.
	if _, ok := view.Transforms["cube"]; ok {
		t.Fatal("a removed object must not expose its transform patch in the view")
	}
	if raw, ok := remover.objectValue("cube", fieldTransform); !ok || !strings.Contains(raw, "4") {
		t.Fatalf("the concurrent transform was dropped from the document: %q", raw)
	}
}

// TestRemoveThenRecreateRestoresTheObject proves the removal flag is not
// permanent. A diff that adds the object back clears the flag, which a naive
// monotone tombstone could not do.
func TestRemoveThenRecreateRestoresTheObject(t *testing.T) {
	base := seedScene()
	peer, _ := newSeededPeer(t, base)

	without := dropObject(base, "cube")
	if _, _, err := peer.DiffScene(base, without, "remove"); err != nil {
		t.Fatal(err)
	}
	view, err := peer.View()
	if err != nil {
		t.Fatal(err)
	}
	if len(view.IR.Objects) != 1 {
		t.Fatalf("after the removal want 1 object, got %d", len(view.IR.Objects))
	}

	back := moveObject(base, "cube", 3)
	if _, _, err := peer.DiffScene(without, back, "add it back"); err != nil {
		t.Fatal(err)
	}
	view, err = peer.View()
	if err != nil {
		t.Fatal(err)
	}
	if len(view.IR.Objects) != 2 {
		t.Fatalf("after the re-create want 2 objects, got %d", len(view.IR.Objects))
	}
	if got := objectByID(t, view, "cube").X; got != 3 {
		t.Fatalf("re-created cube x = %v, want 3", got)
	}
	if len(view.Removed) != 0 {
		t.Fatalf("Removed = %v, want empty after the re-create", view.Removed)
	}
}

// TestReplacementDoesNotRaiseTheRemovalFlag proves the stream fold works. The
// diff API emits a removal followed by a create for every CHANGED record. If the
// encoder treated that removal as a real removal, a concurrent peer would see
// the object disappear on every edit.
func TestReplacementDoesNotRaiseTheRemovalFlag(t *testing.T) {
	base := seedScene()
	peer, _ := newSeededPeer(t, base)

	commands, _, err := peer.DiffScene(base, moveObject(base, "cube", 8), "move")
	if err != nil {
		t.Fatal(err)
	}
	// Confirm the diff really did emit the remove-then-create pair. Without
	// this check the test could pass because the diff changed shape.
	sawRemove := false
	for _, command := range commands {
		if command.Kind == scene.CommandRemoveObject && command.ObjectID == "cube" {
			sawRemove = true
		}
	}
	if !sawRemove {
		t.Fatal("scene.DiffCommands no longer emits a removal for a changed record; retune this test")
	}
	if peer.removed("cube") {
		t.Fatal("a replacement raised the removal flag: a concurrent peer would lose the object")
	}
}

// TestWatchDeliversTheReplicatedStream proves the recovery path a receiving peer
// uses. The peer applies nothing locally; every command it sees comes from the
// merged change.
func TestWatchDeliversTheReplicatedStream(t *testing.T) {
	base := seedScene()
	server, _ := newSeededPeer(t, base)
	viewer := forkPeer(t, server)

	var seen []scene.Command
	viewer.Watch(func(commands []scene.Command) { seen = append(seen, commands...) })

	if _, _, err := server.DiffScene(base, moveObject(base, "cube", 6), "server move"); err != nil {
		t.Fatal(err)
	}
	if err := viewer.Doc().Merge(server.Doc()); err != nil {
		t.Fatal(err)
	}
	if len(seen) == 0 {
		t.Fatal("the watcher saw no command for a replicated change")
	}
	sawCreate := false
	for _, command := range seen {
		if command.Kind == scene.CommandCreateObject && command.ObjectID == "cube" {
			sawCreate = true
			raw, err := json.Marshal(command.Data)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(raw), `"x":6`) {
				t.Fatalf("the replicated create lost the new position: %s", raw)
			}
		}
	}
	if !sawCreate {
		t.Fatalf("the watcher never saw the cube create: %d commands", len(seen))
	}
	// The runtime merges a create into the record it already holds, so the
	// stream must clear the record first.
	if seen[0].Kind != scene.CommandRemoveObject || seen[0].ObjectID != "cube" {
		t.Fatalf("the create is not preceded by a removal: first command = %+v", seen[0])
	}
}

// TestWatchReportsRemovalThatLosesToNothing proves a replicated removal reaches
// the watcher, so a peer's surface drops the object instead of keeping a stale
// mesh on screen.
func TestWatchReportsReplicatedRemoval(t *testing.T) {
	base := seedScene()
	server, _ := newSeededPeer(t, base)
	viewer := forkPeer(t, server)

	var seen []scene.Command
	viewer.Watch(func(commands []scene.Command) { seen = append(seen, commands...) })

	if _, _, err := server.DiffScene(base, dropObject(base, "cube"), "server removes"); err != nil {
		t.Fatal(err)
	}
	if err := viewer.Doc().Merge(server.Doc()); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, command := range seen {
		if command.Kind == scene.CommandRemoveObject && command.ObjectID == "cube" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the watcher never saw the removal: %+v", seen)
	}
}

// TestApplyCommandStreamMatchesFullRediff is the differential oracle. It applies
// an incremental diff at every step and compares the document against a
// document built by one diff from empty. The two must agree at every step.
//
// This follows FuzzIslandReuseMatchesFullEval in client/vm: an incremental path
// is only trustworthy when a full recomputation agrees with it.
func TestApplyCommandStreamMatchesFullRediff(t *testing.T) {
	steps := sceneSteps()
	incremental := crdt.NewDoc()
	incrementalBound, err := Bind(incremental, "scene")
	if err != nil {
		t.Fatal(err)
	}

	previous := scene.SceneIR{}
	for index, step := range steps {
		if _, _, err := incrementalBound.DiffScene(previous, step, fmt.Sprintf("step %d", index)); err != nil {
			t.Fatalf("step %d: %v", index, err)
		}
		previous = step

		fresh, err := Bind(crdt.NewDoc(), "scene")
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := fresh.DiffScene(scene.SceneIR{}, step, "full"); err != nil {
			t.Fatalf("full diff at step %d: %v", index, err)
		}
		// Compare the visible scene. A document built by one diff from empty
		// never learned that an object once existed, so its removal history
		// legitimately differs from the incremental document's.
		want := canonicalVisible(t, fresh)
		got := canonicalVisible(t, incrementalBound)
		if string(want) != string(got) {
			t.Fatalf("step %d: the incremental stream drifted from a full re-diff\n want %s\n  got %s", index, want, got)
		}
	}
	view, err := incrementalBound.View()
	if err != nil {
		t.Fatal(err)
	}
	if view.Empty() {
		t.Fatal("the oracle compared two EMPTY scenes")
	}
}

// sceneSteps returns a sequence of scene states that exercises creation, edits,
// removal, re-creation, and every whole-collection slot.
func sceneSteps() []scene.SceneIR {
	first := scene.SceneIR{
		Objects: []scene.ObjectIR{{ID: "cube", Kind: "box", Width: 1}},
	}
	second := first
	second.Objects = []scene.ObjectIR{
		{ID: "cube", Kind: "box", Width: 1, X: 2},
		{ID: "ball", Kind: "sphere", Radius: 1},
	}
	second.Lights = []scene.LightIR{{ID: "sun", Kind: "directional", Intensity: 1}}

	third := second
	third.Objects = []scene.ObjectIR{{ID: "ball", Kind: "sphere", Radius: 1, Y: 4}}
	third.Labels = []scene.LabelIR{{ID: "tag", Text: "ball"}}

	fourth := third
	fourth.Objects = append([]scene.ObjectIR(nil), third.Objects...)
	fourth.Objects = append(fourth.Objects, scene.ObjectIR{ID: "cube", Kind: "box", Width: 3, Z: -1})
	fourth.Environment = scene.EnvironmentIR{AmbientColor: "#202020", Exposure: 1.2}
	fourth.InstancedMeshes = []scene.InstancedMeshIR{{ID: "grass", Kind: "box", Count: 64}}

	fifth := fourth
	fifth.PostEffects = []scene.PostEffectIR{scene.TonemapIR{Mode: "aces", Exposure: 1}}
	fifth.PostFXMaxPixels = 1 << 20
	fifth.Models = []scene.ModelIR{{ObjectIR: scene.ObjectIR{ID: "robot"}, Src: "/robot.glb"}}
	fifth.Points = []scene.PointsIR{{ID: "dust", Count: 32, Size: 0.1}}
	fifth.Animations = []scene.AnimationClipIR{{Name: "idle", Duration: 1}}
	fifth.InstancedGLBMeshes = []scene.InstancedGLBMeshIR{{ID: "tree", Src: "/tree.glb", Instances: []scene.MeshInstanceIR{{X: 1}}}}
	fifth.Sprites = []scene.SpriteIR{{ID: "spark", Src: "/spark.png"}}
	fifth.HTML = []scene.HTMLIR{{ID: "panel", HTML: "<i>x</i>"}}

	sixth := fifth
	sixth.Labels = nil
	sixth.Objects = []scene.ObjectIR{{ID: "ball", Kind: "sphere", Radius: 2, Y: 4}}
	return []scene.SceneIR{first, second, third, fourth, fifth, sixth}
}

// -----------------------------------------------------------------------------
// helpers
// -----------------------------------------------------------------------------

// newSeededPeer builds a document that already holds the seed scene.
func newSeededPeer(t *testing.T, seed scene.SceneIR) (*Doc, scene.SceneIR) {
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

// forkPeer makes a peer that shares the source history but owns a distinct
// actor ID.
//
// crdt.Doc.Fork preserves the actor ID, so two forks would allocate the SAME
// operation identities and the document could not converge. A fresh document
// merged with the source keeps its own random actor, which is what a real second
// client has.
func forkPeer(t *testing.T, source *Doc) *Doc {
	t.Helper()
	bound, err := Bind(crdt.NewDoc(), source.Namespace())
	if err != nil {
		t.Fatal(err)
	}
	if err := bound.Doc().Merge(source.Doc()); err != nil {
		t.Fatal(err)
	}
	if bound.Doc().ActorID() == source.Doc().ActorID() {
		t.Fatal("the fork shares an actor ID with the source")
	}
	return bound
}

// mergeBoth exchanges history in both directions with crdt.Doc.Merge.
func mergeBoth(t *testing.T, left, right *Doc) {
	t.Helper()
	if err := left.Doc().Merge(right.Doc()); err != nil {
		t.Fatal(err)
	}
	if err := right.Doc().Merge(left.Doc()); err != nil {
		t.Fatal(err)
	}
}

// syncPair exchanges history with the sync-message protocol the hub uses, so a
// convergence test covers the real replication path and not only Merge.
func syncPair(t *testing.T, left, right *Doc) {
	t.Helper()
	leftState := crdtsync.NewState()
	rightState := crdtsync.NewState()
	for round := 0; round < 8; round++ {
		progress := false
		if msg, ok := left.Doc().GenerateSyncMessage(leftState); ok {
			progress = true
			if err := right.Doc().ReceiveSyncMessage(rightState, msg); err != nil {
				t.Fatalf("right receive: %v", err)
			}
		}
		if msg, ok := right.Doc().GenerateSyncMessage(rightState); ok {
			progress = true
			if err := left.Doc().ReceiveSyncMessage(leftState, msg); err != nil {
				t.Fatalf("left receive: %v", err)
			}
		}
		if !progress {
			return
		}
	}
	t.Fatal("the sync exchange did not settle")
}

// TestConcurrentCreationOfTheSameObjectIsNotDuplicated proves the object index
// tolerates two partitioned peers that create the SAME object. Both append the
// ID, so the merged index carries it twice, and the read path must deduplicate.
// Without that the object would appear twice in the materialized scene.
func TestConcurrentCreationOfTheSameObjectIsNotDuplicated(t *testing.T) {
	base := seedScene()
	server, _ := newSeededPeer(t, base)
	left := forkPeer(t, server)
	right := forkPeer(t, server)

	withCone := base
	withCone.Objects = append(append([]scene.ObjectIR(nil), base.Objects...),
		scene.ObjectIR{ID: "cone", Kind: "cone", Radius: 1})
	if _, _, err := left.DiffScene(base, withCone, "left adds the cone"); err != nil {
		t.Fatal(err)
	}
	rightCone := base
	rightCone.Objects = append(append([]scene.ObjectIR(nil), base.Objects...),
		scene.ObjectIR{ID: "cone", Kind: "cone", Radius: 2})
	if _, _, err := right.DiffScene(base, rightCone, "right adds the cone"); err != nil {
		t.Fatal(err)
	}

	mergeBoth(t, left, right)
	if string(canonicalView(t, left)) != string(canonicalView(t, right)) {
		t.Fatal("peers diverged after both created the same object")
	}

	ids, err := left.ObjectIDs()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]int{}
	for _, id := range ids {
		seen[id]++
	}
	if seen["cone"] != 1 {
		t.Fatalf("object index reports %d entries for cone, want 1", seen["cone"])
	}
	view, err := left.View()
	if err != nil {
		t.Fatal(err)
	}
	cones := 0
	for _, record := range view.IR.Objects {
		if record.ID == "cone" {
			cones++
		}
	}
	if cones != 1 {
		t.Fatalf("the materialized scene holds %d cones, want 1", cones)
	}
	if len(view.IR.Objects) != 3 {
		t.Fatalf("object count = %d, want 3", len(view.IR.Objects))
	}
}

// TestWatchIgnoresAnotherNamespace proves key parsing is namespaced. Two scenes
// share one document; a watcher on one must see nothing from the other.
//
// The object index alone does not prove this. The index is per namespace, so a
// leak through key parsing shows up only on the patch path that Watch uses.
func TestWatchIgnoresAnotherNamespace(t *testing.T) {
	doc := crdt.NewDoc()
	left, err := Bind(doc, "left")
	if err != nil {
		t.Fatal(err)
	}
	right, err := Bind(doc, "right")
	if err != nil {
		t.Fatal(err)
	}

	var rightSaw []scene.Command
	right.Watch(func(commands []scene.Command) { rightSaw = append(rightSaw, commands...) })

	if _, err := left.Apply([]scene.Command{
		scene.CreateObjectCommand(scene.ObjectIR{ID: "cube", Kind: "box"}),
		scene.SetCameraCommand(scene.IRCamera{Z: 5}),
	}, "left only"); err != nil {
		t.Fatal(err)
	}
	if len(rightSaw) != 0 {
		t.Fatalf("the right namespace saw %d commands from the left namespace: %+v", len(rightSaw), rightSaw)
	}

	// The same write inside the right namespace DOES reach the watcher, so the
	// check above is not passing merely because Watch is broken.
	if _, err := right.Apply([]scene.Command{
		scene.CreateObjectCommand(scene.ObjectIR{ID: "sphere", Kind: "sphere"}),
	}, "right only"); err != nil {
		t.Fatal(err)
	}
	if len(rightSaw) == 0 {
		t.Fatal("the watcher saw nothing for a write in its own namespace")
	}
}

// TestBootstrapStreamRebuildsTheSameScene closes the loop a joining peer walks.
// Commands() produces a stream, the stream is applied to a fresh document, and
// the fresh document must describe the same scene.
//
// This is a second, independent round trip. TestCommandRoundTripIsLossless
// compares command bytes; this compares the scene those commands build.
func TestBootstrapStreamRebuildsTheSameScene(t *testing.T) {
	source, err := Bind(crdt.NewDoc(), "scene")
	if err != nil {
		t.Fatal(err)
	}
	previous := scene.SceneIR{}
	for index, step := range sceneSteps() {
		if _, _, err := source.DiffScene(previous, step, fmt.Sprintf("step %d", index)); err != nil {
			t.Fatal(err)
		}
		previous = step
	}
	// Add the two collection kinds sceneSteps does not reach, so the bootstrap
	// stream carries every slot.
	if _, err := source.Apply([]scene.Command{
		scene.SetCameraCommand(scene.IRCamera{Kind: "perspective", Z: 9, FOV: 60}),
		scene.SetMaterialsCommand([]scene.IRMaterial{{Name: "brass", Metalness: 0.8}}),
		scene.SetPostUniformsCommand([]scene.PostUniformPatch{{Name: "glitch", Uniforms: map[string]any{"amount": 0.5}}}),
		{Kind: scene.CommandSetTransform, ObjectID: "ball", Data: map[string]any{"x": 1.5}},
		{Kind: scene.CommandSetMaterial, ObjectID: "ball", Data: map[string]any{"color": "#ff0000"}},
		{Kind: scene.CommandSetLight, ObjectID: "sun", Data: map[string]any{"intensity": 3.0}},
	}, "extra slots"); err != nil {
		t.Fatal(err)
	}

	bootstrap, err := source.Commands()
	if err != nil {
		t.Fatal(err)
	}
	if len(bootstrap) == 0 {
		t.Fatal("the bootstrap stream is empty")
	}

	joiner, err := Bind(crdt.NewDoc(), "scene")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := joiner.Apply(bootstrap, "bootstrap"); err != nil {
		t.Fatal(err)
	}

	want := canonicalVisible(t, source)
	got := canonicalVisible(t, joiner)
	if string(want) != string(got) {
		t.Fatalf("the bootstrap stream did not rebuild the scene\n want %s\n  got %s", want, got)
	}
	view, err := joiner.View()
	if err != nil {
		t.Fatal(err)
	}
	if view.Empty() {
		t.Fatal("the joiner rebuilt an EMPTY scene")
	}
	// Every slot must be present, or the comparison above could pass on a
	// stream that carries only objects.
	for _, slot := range []string{
		slotCamera, slotEnvironment, slotMaterials, slotModels, slotParticles,
		slotInstancedMeshes, slotInstancedGLBMeshes, slotAnimations,
		slotPostEffects, slotPostUniforms,
	} {
		if _, ok := view.Slots[slot]; !ok {
			t.Fatalf("the rebuilt scene is missing slot %q", slot)
		}
	}
	for _, patches := range []map[string]json.RawMessage{
		view.Transforms, view.MaterialPatches, view.LightPatches,
	} {
		if len(patches) == 0 {
			t.Fatal("the rebuilt scene lost a per-object patch")
		}
	}
}

// TestViewEmptyDetectsAnEmptyScene locks the guard every convergence test leans
// on. A guard that always reported "not empty" would make those tests vacuous.
func TestViewEmptyDetectsAnEmptyScene(t *testing.T) {
	bound, err := Bind(crdt.NewDoc(), "scene")
	if err != nil {
		t.Fatal(err)
	}
	view, err := bound.View()
	if err != nil {
		t.Fatal(err)
	}
	if !view.Empty() {
		t.Fatal("a document with no scene reports a non-empty view")
	}
	if _, err := bound.Apply([]scene.Command{
		scene.CreateObjectCommand(scene.ObjectIR{ID: "cube", Kind: "box"}),
	}, "one object"); err != nil {
		t.Fatal(err)
	}
	view, err = bound.View()
	if err != nil {
		t.Fatal(err)
	}
	if view.Empty() {
		t.Fatal("a document with one object reports an empty view")
	}
}
