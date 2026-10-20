package scene3d

import (
	"encoding/json"
	"testing"

	"m31labs.dev/gosx/crdt"
	crdtsync "m31labs.dev/gosx/crdt/sync"
	"m31labs.dev/gosx/scene"
)

func TestClientWritesRequireStringValuesAfterSyncDecoding(t *testing.T) {
	for _, field := range []string{fieldCreate, fieldTransform, fieldMaterial, fieldLight} {
		for _, kind := range []crdt.ValueKind{
			crdt.ValueKindBool, crdt.ValueKindNull, crdt.ValueKindInt,
			crdt.ValueKindUint, crdt.ValueKindFloat, crdt.ValueKindBytes,
		} {
			t.Run(field+"/"+string(kind), func(t *testing.T) {
				base, _ := newSeededPeer(t, scene.SceneIR{Objects: []scene.ObjectIR{{ID: "overlay", Kind: "box"}}})
				payload := `{"html":"<b>content</b>"}`
				if field == fieldCreate {
					payload = `{"kind":"html","props":{"id":"overlay","html":"<b>content</b>"}}`
				}
				source, wire, changes := sceneWriteOnWire(t, base, field, crdt.Value{Kind: kind, Str: payload})
				found := false
				for _, change := range changes {
					for _, op := range change.Ops {
						if op.Prop == base.objectKey("overlay", field) && op.Value.Kind == kind && op.Value.Str == payload {
							found = true
						}
					}
				}
				if !found {
					t.Fatal("sync decoding did not retain the malformed value")
				}
				for _, options := range []GateOptions{{}, {ClientHTMLGuard: AllowAll}} {
					if err := ChangeGate("room", base, AllowAll, nil, options)(nil, "room", changes); err == nil {
						t.Fatal("gate accepted a non-string scene payload")
					}
				}
				// Readers must also enforce types when receiving document history
				// independently of the hub's inbound authorizer.
				receiver := forkPeer(t, base)
				var watched []scene.Command
				receiver.Watch(func(commands []scene.Command) { watched = append(watched, commands...) })
				if err := receiver.Doc().ReceiveSyncMessage(crdtsync.NewState(), wire); err != nil {
					t.Fatal(err)
				}
				if len(watched) != 0 {
					t.Fatalf("non-string value emitted watcher commands: %v", watched)
				}
				if len(mustView(t, receiver).IR.HTML) != 0 {
					t.Fatal("non-string value materialized HTML")
				}
				commands, err := source.Commands()
				if err != nil {
					t.Fatal(err)
				}
				for _, command := range commands {
					if field == fieldCreate || command.Kind == kindForField[field] {
						t.Fatal("bootstrap emitted a non-string scene payload")
					}
				}
			})
		}
	}
}

func TestCreateKindsAreUnambiguousAcrossGateWatchAndView(t *testing.T) {
	for _, payload := range []string{
		`{"kind":"html","kind":null,"props":{"id":"overlay","html":"<b>content</b>"}}`,
		`{"kind":null,"kind":"html","props":{"id":"overlay","html":"<b>content</b>"}}`,
		`{"kind":null,"props":{"id":"overlay"}}`,
		`{"kind":"label","kind":"label","props":{"id":"overlay"}}`,
		`{"kind":"html","\u006bind":null,"props":{"id":"overlay"}}`,
		`{"Kind":"html","props":{"id":"overlay"}}`,
		`{"kind":"html","Kind":null,"props":{"id":"overlay"}}`,
	} {
		t.Run(payload, func(t *testing.T) {
			base, _ := newSeededPeer(t, scene.SceneIR{Objects: []scene.ObjectIR{{ID: "overlay", Kind: "box"}}})
			source, wire, changes := sceneWriteOnWire(t, base, fieldCreate, crdt.StringValue(payload))
			for _, options := range []GateOptions{{}, {ClientHTMLGuard: AllowAll}} {
				if err := ChangeGate("room", base, AllowAll, nil, options)(nil, "room", changes); err == nil {
					t.Fatal("gate accepted an ambiguous create kind")
				}
			}
			receiver := forkPeer(t, base)
			var watched []scene.Command
			receiver.Watch(func(commands []scene.Command) { watched = append(watched, commands...) })
			if err := receiver.Doc().ReceiveSyncMessage(crdtsync.NewState(), wire); err != nil {
				t.Fatal(err)
			}
			if len(watched) != 0 {
				t.Fatalf("ambiguous create emitted watcher commands: %v", watched)
			}
			if view, err := receiver.View(); err == nil || len(view.IR.HTML) != 0 {
				t.Fatalf("ambiguous create materialized: %v, HTML records=%d", err, len(view.IR.HTML))
			}
			if _, err := source.Commands(); err == nil {
				t.Fatal("bootstrap accepted an ambiguous create kind")
			}
			// An error must not contaminate the next envelope in a reused reader.
			var decoder createDecoder
			if err := decoder.decodeEnvelope(payload); err == nil {
				t.Fatal("canonical decoder accepted an ambiguous create kind")
			}
			if err := decoder.decodeEnvelope(`{"props":{"id":"box","kind":"box"}}`); err != nil || decoder.envelope.Kind != "" {
				t.Fatalf("reader did not recover for an ordinary object: %v", err)
			}
		})
	}
}

func TestClientMaterialUpdatesPreserveObjectPatchProtocol(t *testing.T) {
	for _, payload := range []string{
		`{"unlit":true}`, `{"unlit":false}`, `{"normalScale":0.5}`, `{"normalScale":0}`,
		`{"materialKind":"standard","emissiveColor":[1,0.5,0],"occlusionStrength":0.5,"normalUVScale":[2,3]}`,
		`{"roughness":"var(--roughness)","opacity":0.5,"alphaCutoff":null}`,
		`{"material":"standard"}`, `{"material":{"kind":"standard","unlit":true,"normalScale":0.5}}`,
	} {
		t.Run(payload, func(t *testing.T) {
			base, _ := newSeededPeer(t, scene.SceneIR{Objects: []scene.ObjectIR{{ID: "overlay", Kind: "box"}}})
			_, wire, changes := sceneWriteOnWire(t, base, fieldMaterial, crdt.StringValue(payload))
			if err := ChangeGate("room", base, AllowAll, nil)(nil, "room", changes); err != nil {
				t.Fatalf("supported material update refused: %v", err)
			}
			var watched []scene.Command
			base.Watch(func(commands []scene.Command) { watched = append(watched, commands...) })
			if err := base.Doc().ReceiveSyncMessage(crdtsync.NewState(), wire); err != nil {
				t.Fatal(err)
			}
			if len(watched) != 1 || watched[0].Kind != scene.CommandSetMaterial || watched[0].ObjectID != "overlay" {
				t.Fatalf("material watcher output: %v", watched)
			}
			if raw, ok := watched[0].Data.(json.RawMessage); !ok || string(raw) != payload {
				t.Fatal("material watcher changed the patch bytes")
			}
			if got := string(mustView(t, base).MaterialPatches["overlay"]); got != payload {
				t.Fatalf("materialized patch = %q, want %q", got, payload)
			}
		})
	}
	for _, payload := range []string{
		`{"unlit":"true"}`, `{"normalScale":"bad"}`, `{"roughness":true}`,
		`{"html":"content"}`, `{"props":{"html":"content"}}`, `{"material":{"html":"content"}}`,
		`{"material":{"material":{"unlit":true}}}`,
	} {
		if err := validateClientPatch(payload, clientPatchSchemas[fieldMaterial]); err == nil {
			t.Fatalf("accepted invalid material update: %s", payload)
		}
	}
}

func sceneWriteOnWire(t *testing.T, base *Doc, field string, value crdt.Value) (*Doc, []byte, []crdt.Change) {
	t.Helper()
	source := forkPeer(t, base)
	if err := source.Doc().Put(crdt.Root, source.objectKey("overlay", field), value); err != nil {
		t.Fatal(err)
	}
	if _, err := source.Doc().Commit("scene update"); err != nil {
		t.Fatal(err)
	}
	wire, ok := source.Doc().GenerateSyncMessage(crdtsync.NewState())
	if !ok {
		t.Fatal("no scene sync frame")
	}
	return source, wire, changesOf(t, source.Doc())
}
