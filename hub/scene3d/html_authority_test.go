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

func TestClientHTMLKindUsesCaseSensitiveBrowserKeys(t *testing.T) {
	server, err := Bind(crdt.NewDoc(), "scene")
	if err != nil {
		t.Fatal(err)
	}
	for _, payload := range []string{
		`{"kind":"html","Kind":"label","props":{"id":"overlay","html":"<b>new</b>"}}`,
		`{"Kind":"label","kind":"html","props":{"id":"overlay","html":"<b>new</b>"}}`,
		`{"kind":"html","KIND":"label","props":{"id":"overlay","html":"<b>new</b>"}}`,
		`{"kind":"label","kind":"html","props":{"id":"overlay","html":"<b>new</b>"}}`,
	} {
		doc := crdt.NewDoc()
		if err := doc.Put(crdt.Root, server.objectKey("overlay", fieldCreate), crdt.StringValue(payload)); err != nil {
			t.Fatal(err)
		}
		if _, err := doc.Commit("client overlay"); err != nil {
			t.Fatal(err)
		}
		if err := ChangeGate("room", server, AllowAll, nil)(nil, "room", changesOf(t, doc)); err == nil {
			t.Fatalf("accepted ambiguous or HTML create: %s", payload)
		}
	}
}

func TestClientPatchesCannotReplaceOverlayHTML(t *testing.T) {
	server, err := Bind(crdt.NewDoc(), "scene")
	if err != nil {
		t.Fatal(err)
	}
	overlay := scene.SceneIR{HTML: []scene.HTMLIR{{ID: "overlay", HTML: "<b>server</b>"}}}
	if _, _, err := server.DiffScene(scene.SceneIR{}, overlay, "server overlay"); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{fieldTransform, fieldMaterial, fieldLight} {
		t.Run(field, func(t *testing.T) {
			for _, payload := range []string{`{"html":"<b>client</b>"}`, `{"HTML":"<b>client</b>"}`, `{"\u0068tml":"<b>client</b>"}`, `{"__proto__":{"html":"client"}}`, `{"props":{"html":"client"}}`, `null`, `[]`} {
				client := forkPeer(t, server)
				if err := client.Doc().Put(crdt.Root, server.objectKey("overlay", field), crdt.StringValue(payload)); err != nil {
					t.Fatal(err)
				}
				if _, err := client.Doc().Commit("client patch"); err != nil {
					t.Fatal(err)
				}
				// Even an HTML author must replace HTML through the guarded create path.
				for _, htmlGuard := range []Guard{nil, func(*hub.Client, Target) bool { return false }, AllowAll} {
					if err := ChangeGate("room", server, AllowAll, nil, GateOptions{ClientHTMLGuard: htmlGuard})(nil, "room", changesOf(t, client.Doc())); err == nil {
						t.Fatalf("accepted patch: %s", payload)
					}
				}
			}
		})
	}
	if got := mustView(t, server).IR.HTML[0].HTML; got != "<b>server</b>" {
		t.Fatalf("server overlay changed: %q", got)
	}
}

func TestClientPatchesValidateDeclaredFieldsAndValues(t *testing.T) {
	for _, tc := range []struct {
		field, payload string
		valid          bool
	}{
		{fieldTransform, `{"x":2,"rotationY":0.5,"parentMatrix":null}`, true},
		{fieldMaterial, `{"color":"#00ff00","roughness":0.25}`, true},
		{fieldLight, `{"intensity":2}`, true},
		{fieldTransform, `{"x":"bad"}`, false},
		{fieldTransform, `{"X":2}`, false},
		{fieldTransform, `{"color":"red"}`, false},
		{fieldMaterial, `{"x":2}`, false},
		{fieldMaterial, `{"variants":{"full":{"html":"client"}}}`, false},
	} {
		if err := validateClientPatch(tc.payload, clientPatchSchemas[tc.field]); (err == nil) != tc.valid {
			t.Fatalf("field=%s payload=%s: %v", tc.field, tc.payload, err)
		}
	}
}
