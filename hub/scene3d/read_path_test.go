package scene3d

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"testing"

	"m31labs.dev/gosx/crdt"
	"m31labs.dev/gosx/scene"
)

// TestKeyBufBuildsTheSameKeyAsObjectKey pins the buffered reader to the writer.
// A read that built a different key from the write would find nothing, and the
// scene would materialize empty with no error at all.
func TestKeyBufBuildsTheSameKeyAsObjectKey(t *testing.T) {
	_, bound := newBoundDoc(t)
	keys := keyBuf{d: bound}

	objectIDs := []string{
		"obj-1",
		"",
		"a/b/c",
		"nested/obj-2",
		strings.Repeat("long", 40),
		"unicode-é中",
		"trailing/",
	}
	fields := []string{fieldCreate, fieldTransform, fieldMaterial, fieldLight, fieldGone}
	for _, id := range objectIDs {
		for _, field := range fields {
			want := string(bound.objectKey(id, field))
			got := string(keys.objectKey(id, field))
			if got != want {
				t.Fatalf("keyBuf key for %q/%q = %q, want %q", id, field, got, want)
			}
		}
	}
}

// TestKeyBufReadsWithoutAllocating proves the buffer is reused. The key text was
// about 15% of the allocations of a whole View before this existed.
func TestKeyBufReadsWithoutAllocating(t *testing.T) {
	_, bound := newBoundDoc(t)
	if _, err := bound.Apply([]scene.Command{
		scene.CreateObjectCommand(scene.ObjectIR{ID: "obj-1", Kind: "box"}),
	}, "seed"); err != nil {
		t.Fatal(err)
	}

	keys := keyBuf{d: bound}
	// Warm the buffer once, so the measurement excludes its first growth.
	keys.value("obj-1", fieldCreate)

	allocs := testing.AllocsPerRun(200, func() {
		if _, ok := keys.value("obj-1", fieldCreate); !ok {
			t.Fatal("the create payload read as absent")
		}
		if _, ok := keys.value("obj-1", fieldTransform); ok {
			t.Fatal("an absent transform read as present")
		}
		if keys.removed("obj-1") {
			t.Fatal("a live object read as removed")
		}
	})
	if allocs != 0 {
		t.Fatalf("three buffered reads allocated %.1f times, want 0", allocs)
	}
}

// TestBufferedAndSingleKeyReadsAgree proves the one-key wrappers, which the
// cold paths still use, return what the buffered reader returns.
func TestBufferedAndSingleKeyReadsAgree(t *testing.T) {
	_, bound := newBoundDoc(t)
	if _, err := bound.Apply([]scene.Command{
		scene.CreateObjectCommand(scene.ObjectIR{ID: "live", Kind: "box"}),
		scene.CreateObjectCommand(scene.ObjectIR{ID: "dead", Kind: "box"}),
		scene.RemoveObjectCommand("dead"),
	}, "seed"); err != nil {
		t.Fatal(err)
	}

	keys := keyBuf{d: bound}
	for _, id := range []string{"live", "dead", "absent"} {
		for _, field := range []string{fieldCreate, fieldTransform, fieldGone} {
			wantValue, wantOK := bound.objectValue(id, field)
			gotValue, gotOK := keys.value(id, field)
			if gotValue != wantValue || gotOK != wantOK {
				t.Fatalf("keyBuf value for %q/%q = %q %v, single-key read = %q %v",
					id, field, gotValue, gotOK, wantValue, wantOK)
			}
		}
		if keys.removed(id) != bound.removed(id) {
			t.Fatalf("removed(%q) disagrees: buffered %v, single-key %v",
				id, keys.removed(id), bound.removed(id))
		}
	}
}

// TestCreateDecoderDoesNotCarryKindBetweenObjects is the reason the decoder
// resets every envelope field. encoding/json leaves a field untouched when the
// input omits it, so a mesh decoded after a light would arrive as a light and
// the scene would gain a light it never had.
func TestCreateDecoderDoesNotCarryKindBetweenObjects(t *testing.T) {
	var (
		decoder createDecoder
		view    = View{}
	)
	light := mustPayload(t, scene.CommandPayload{
		Kind:  "light",
		Props: scene.LightIR{ID: "sun", Kind: "directional", Intensity: 1},
	})
	mesh := mustPayload(t, scene.CommandPayload{
		Props: scene.ObjectIR{ID: "box", Kind: "box", Width: 2},
	})

	if err := decoder.addCreate(&view, "sun", light); err != nil {
		t.Fatal(err)
	}
	if err := decoder.addCreate(&view, "box", mesh); err != nil {
		t.Fatal(err)
	}
	if len(view.IR.Lights) != 1 {
		t.Fatalf("lights = %d, want 1", len(view.IR.Lights))
	}
	if len(view.IR.Objects) != 1 {
		t.Fatalf("objects = %d, want 1: the mesh inherited the light's kind", len(view.IR.Objects))
	}
	if view.IR.Objects[0].ID != "box" || view.IR.Objects[0].Width != 2 {
		t.Fatalf("mesh decoded as %+v", view.IR.Objects[0])
	}
}

// TestCreateDecoderDoesNotCarryFieldsBetweenObjects proves a record field is not
// carried either. The second object omits every field the first one set.
func TestCreateDecoderDoesNotCarryFieldsBetweenObjects(t *testing.T) {
	var (
		decoder createDecoder
		view    = View{}
	)
	full := mustPayload(t, scene.CommandPayload{
		Props: scene.ObjectIR{ID: "full", Kind: "box", Width: 9, Color: "#ff0000", CastShadow: true},
	})
	bare := mustPayload(t, scene.CommandPayload{Props: scene.ObjectIR{ID: "bare"}})

	if err := decoder.addCreate(&view, "full", full); err != nil {
		t.Fatal(err)
	}
	if err := decoder.addCreate(&view, "bare", bare); err != nil {
		t.Fatal(err)
	}
	second := view.IR.Objects[1]
	if second.ID != "bare" || second.Width != 0 || second.Color != "" || second.CastShadow {
		t.Fatalf("the bare object inherited fields: %+v", second)
	}
}

// TestCreateDecoderRefusesTrailingBytes proves a payload with bytes after the
// top-level value is refused, and refused against the object that carries it.
// A reused decoder keeps unread bytes for its next call, so without the length
// check the fault would be blamed on the NEXT object.
func TestCreateDecoderRefusesTrailingBytes(t *testing.T) {
	var (
		decoder createDecoder
		view    = View{}
	)
	good := mustPayload(t, scene.CommandPayload{Props: scene.ObjectIR{ID: "good", Kind: "box"}})
	bad := good + `{"kind":"light"}`

	err := decoder.addCreate(&view, "bad", bad)
	if err == nil {
		t.Fatal("a payload with trailing bytes was accepted")
	}
	if !strings.Contains(err.Error(), `"bad"`) {
		t.Fatalf("the error does not name the object that carries the fault: %v", err)
	}

	// The next object must still decode, so one bad payload cannot poison the
	// rest of the scene.
	if err := decoder.addCreate(&view, "good", good); err != nil {
		t.Fatalf("a later object failed after a bad one: %v", err)
	}
	if len(view.IR.Objects) != 1 || view.IR.Objects[0].ID != "good" {
		t.Fatalf("objects = %+v", view.IR.Objects)
	}
}

// TestCreateDecoderAcceptsSurroundingSpace proves the reused decoder keeps the
// tolerance json.Unmarshal has always had, and that a payload after one with
// trailing space still decodes.
func TestCreateDecoderAcceptsSurroundingSpace(t *testing.T) {
	var (
		decoder createDecoder
		view    = View{}
	)
	first := mustPayload(t, scene.CommandPayload{Props: scene.ObjectIR{ID: "first", Kind: "box"}})
	second := mustPayload(t, scene.CommandPayload{Props: scene.ObjectIR{ID: "second", Kind: "box"}})

	if err := decoder.addCreate(&view, "first", " \n\t"+first+" \n\t"); err != nil {
		t.Fatalf("a payload with surrounding space was refused: %v", err)
	}
	if err := decoder.addCreate(&view, "second", second); err != nil {
		t.Fatalf("the payload after one with trailing space was refused: %v", err)
	}
	if len(view.IR.Objects) != 2 ||
		view.IR.Objects[0].ID != "first" || view.IR.Objects[1].ID != "second" {
		t.Fatalf("objects = %+v", view.IR.Objects)
	}
}

// TestCreateDecoderReportsAMalformedPayload proves a broken payload is reported
// against its own object and does not stop the objects that follow.
func TestCreateDecoderReportsAMalformedPayload(t *testing.T) {
	cases := map[string]string{
		"truncated":     `{"props":{"id":"x"`,
		"not an object": `42`,
		"no record":     `{"kind":"light"}`,
		"empty":         ``,
	}
	good := mustPayload(t, scene.CommandPayload{Props: scene.ObjectIR{ID: "good", Kind: "box"}})

	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			var (
				decoder createDecoder
				view    = View{}
			)
			err := decoder.addCreate(&view, "broken", payload)
			if err == nil {
				t.Fatal("a malformed payload was accepted")
			}
			if !strings.Contains(err.Error(), `"broken"`) {
				t.Fatalf("the error does not name the object: %v", err)
			}
			if err := decoder.addCreate(&view, "good", good); err != nil {
				t.Fatalf("a later object failed after a broken one: %v", err)
			}
		})
	}
}

// TestCreateDecoderMatchesAPlainUnmarshal is the differential check between the
// reused decoder and the plain unmarshal it replaced. Both must reach the same
// record and the same verdict for every payload.
func TestCreateDecoderMatchesAPlainUnmarshal(t *testing.T) {
	payloads := []string{
		mustPayload(t, scene.CommandPayload{Props: scene.ObjectIR{ID: "a", Kind: "box", Width: 3}}),
		mustPayload(t, scene.CommandPayload{Kind: "label", Props: scene.LabelIR{ID: "l", Text: "hi"}}),
		mustPayload(t, scene.CommandPayload{Kind: "sprite", Props: scene.SpriteIR{ID: "s"}}),
		mustPayload(t, scene.CommandPayload{Kind: "html", Props: scene.HTMLIR{ID: "h"}}),
		mustPayload(t, scene.CommandPayload{Kind: "light", Props: scene.LightIR{ID: "sun"}}),
		`{"props":null}`,
		`{"props":{}}`,
		// An envelope with no record at all must stay a fault even though the
		// decoder before it left a record in the reused envelope.
		`{}`,
		`{"kind":"light"}`,
		` {"props":{"id":"spaced"}} `,
		`{"kind":"nope","props":{}}`,
		`{"props":{"id":"x"}}trailing`,
		`{"props":`,
		``,
		`null`,
	}

	var (
		decoder createDecoder
		got     = View{}
		want    = View{}
	)
	for i, payload := range payloads {
		id := fmt.Sprintf("obj-%d", i)
		gotErr := decoder.addCreate(&got, id, payload)
		wantErr := referenceAddCreate(&want, id, payload)
		if (gotErr == nil) != (wantErr == nil) {
			t.Fatalf("payload %d: decoder err = %v, plain unmarshal err = %v", i, gotErr, wantErr)
		}
	}
	gotJSON, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	wantJSON, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotJSON) != string(wantJSON) {
		t.Fatalf("the decoder and the plain unmarshal built different views\n got %s\nwant %s", gotJSON, wantJSON)
	}
}

// TestObjectIDsMatchTheStringKeyedRead is the differential check for the
// positional index read. The read used to format each index into a property
// name and parse it back. Both forms must return the same IDs in the same
// order, including the duplicates two partitioned peers can create.
func TestObjectIDsMatchTheStringKeyedRead(t *testing.T) {
	_, left := newBoundDoc(t)
	_, right := newBoundDoc(t)
	records := []scene.ObjectIR{
		{ID: "obj-b", Kind: "box"},
		{ID: "obj-a", Kind: "box"},
		{ID: "shared/deep/id", Kind: "box"},
	}
	for _, record := range records {
		if _, err := left.Apply([]scene.Command{scene.CreateObjectCommand(record)}, "seed"); err != nil {
			t.Fatal(err)
		}
		if _, err := right.Apply([]scene.Command{scene.CreateObjectCommand(record)}, "seed"); err != nil {
			t.Fatal(err)
		}
	}
	// Merging two peers that both appended the same IDs makes the index carry
	// each ID twice, which is the case the read must deduplicate.
	if err := left.Doc().Merge(right.Doc()); err != nil {
		t.Fatal(err)
	}

	got, err := left.ObjectIDs()
	if err != nil {
		t.Fatal(err)
	}
	want, err := referenceObjectIDs(left)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("ObjectIDs = %v, string-keyed read = %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("ObjectIDs = %v, string-keyed read = %v", got, want)
		}
	}
	if len(got) != len(records) {
		t.Fatalf("ObjectIDs = %v, want %d distinct ids", got, len(records))
	}
}

// -----------------------------------------------------------------------------
// helpers
// -----------------------------------------------------------------------------

func mustPayload(t *testing.T, payload scene.CommandPayload) string {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// referenceAddCreate is the plain two-unmarshal decode this package used before
// the reused decoder. It stays here as the oracle the fast path is checked
// against, so a change to the fast path cannot quietly change the outcome.
func referenceAddCreate(v *View, objectID, payload string) error {
	var envelope createEnvelope
	if err := json.Unmarshal([]byte(payload), &envelope); err != nil {
		return fmt.Errorf("scene3d: decode create payload for %q: %w", objectID, err)
	}
	switch envelope.Kind {
	case "":
		var record scene.ObjectIR
		if err := json.Unmarshal(envelope.Props, &record); err != nil {
			return fmt.Errorf("scene3d: decode object %q: %w", objectID, err)
		}
		v.IR.Objects = append(v.IR.Objects, record)
	case "label":
		var record scene.LabelIR
		if err := json.Unmarshal(envelope.Props, &record); err != nil {
			return fmt.Errorf("scene3d: decode label %q: %w", objectID, err)
		}
		v.IR.Labels = append(v.IR.Labels, record)
	case "sprite":
		var record scene.SpriteIR
		if err := json.Unmarshal(envelope.Props, &record); err != nil {
			return fmt.Errorf("scene3d: decode sprite %q: %w", objectID, err)
		}
		v.IR.Sprites = append(v.IR.Sprites, record)
	case "html":
		var record scene.HTMLIR
		if err := json.Unmarshal(envelope.Props, &record); err != nil {
			return fmt.Errorf("scene3d: decode html %q: %w", objectID, err)
		}
		v.IR.HTML = append(v.IR.HTML, record)
	case "light":
		var record scene.LightIR
		if err := json.Unmarshal(envelope.Props, &record); err != nil {
			return fmt.Errorf("scene3d: decode light %q: %w", objectID, err)
		}
		v.IR.Lights = append(v.IR.Lights, record)
	default:
		return fmt.Errorf("scene3d: object %q has unsupported create kind %q", objectID, envelope.Kind)
	}
	return nil
}

// referenceObjectIDs reads the object index the way this package read it before
// the positional lookup existed: format each index into a property name, then
// let the document parse it back. It is the oracle for the fast path.
func referenceObjectIDs(d *Doc) ([]string, error) {
	length, err := d.doc.ListLen(d.indexObj)
	if err != nil {
		return nil, nil
	}
	seen := make(map[string]struct{}, length)
	ids := make([]string, 0, length)
	for i := 0; i < length; i++ {
		value, _, err := d.doc.Get(d.indexObj, crdt.Prop(strconv.Itoa(i)))
		if err != nil {
			return nil, fmt.Errorf("scene3d: read object index at %d: %w", i, err)
		}
		id := value.Str
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, nil
}
