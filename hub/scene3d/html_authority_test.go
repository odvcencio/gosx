package scene3d

import (
	"m31labs.dev/gosx/crdt"
	"m31labs.dev/gosx/hub"
	"m31labs.dev/gosx/scene"
	"testing"
)

func TestClientHTMLRequiresAdditionalGuard(t *testing.T) {
	server, err := Bind(crdt.NewDoc(), "scene")
	if err != nil {
		t.Fatal(err)
	}
	client, err := Bind(crdt.NewDoc(), "scene")
	if err != nil {
		t.Fatal(err)
	}
	overlay := scene.SceneIR{HTML: []scene.HTMLIR{{ID: "overlay", HTML: "<b>content</b>"}}}
	if _, _, err := client.DiffScene(scene.SceneIR{}, overlay, "create overlay"); err != nil {
		t.Fatal(err)
	}
	changes := changesOf(t, client.Doc())
	if err := ChangeGate("room", server, nil, nil)(nil, "room", changes); err == nil {
		t.Fatal("default gate accepted client HTML")
	}
	if err := ChangeGate("room", server, AllowAll, nil)(nil, "room", changes); err == nil {
		t.Fatal("AllowAll accepted client HTML without opt-in")
	}
	if err := ChangeGate("room", server, AllowAll, nil, GateOptions{ClientHTMLGuard: func(*hub.Client, Target) bool { return false }})(nil, "room", changes); err == nil {
		t.Fatal("HTML guard refusal ignored")
	}
	called := false
	htmlGuard := func(_ *hub.Client, target Target) bool {
		called = true
		return target.ObjectID == "overlay" && target.Field == fieldCreate
	}
	if err := ChangeGate("room", server, AllowAll, nil, GateOptions{ClientHTMLGuard: htmlGuard})(nil, "room", changes); err != nil || !called {
		t.Fatalf("explicit HTML guard: %v called=%v", err, called)
	}
	if err := ChangeGate("room", server, func(*hub.Client, Target) bool { return false }, nil, GateOptions{ClientHTMLGuard: htmlGuard})(nil, "room", changes); err == nil {
		t.Fatal("HTML opt-in bypassed normal object guard")
	}
	// Server-authored overlays still materialize without an inbound gate.
	if _, _, err := server.DiffScene(scene.SceneIR{}, overlay, "server overlay"); err != nil {
		t.Fatal(err)
	}
	if view := mustView(t, server); len(view.IR.HTML) != 1 {
		t.Fatal("server overlay lost")
	}
}

func TestClientHTMLUpdateAndEncodedKindAreRejected(t *testing.T) {
	server, err := Bind(crdt.NewDoc(), "scene")
	if err != nil {
		t.Fatal(err)
	}
	for _, payload := range []string{`{"kind":"html","props":{"id":"overlay","html":"content"}}`, `{"\u006bind":"html","props":{"id":"overlay","html":"content"}}`, `{"kind":"label","Kind":"html","props":{"id":"overlay","html":"content"}}`} {
		doc := crdt.NewDoc()
		if err := doc.Put(crdt.Root, server.objectKey("overlay", fieldCreate), crdt.StringValue(payload)); err != nil {
			t.Fatal(err)
		}
		if _, err := doc.Commit("update overlay"); err != nil {
			t.Fatal(err)
		}
		if err := ChangeGate("room", server, AllowAll, nil)(nil, "room", changesOf(t, doc)); err == nil {
			t.Fatal("HTML update passed default gate")
		}
	}
}
