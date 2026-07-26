package crdt

import (
	"bytes"
	"strings"
	"testing"
	"time"

	enc "m31labs.dev/gosx/crdt/encoding"
)

// TestSnapshotRoundTripCoversEveryValueKind locks the binary snapshot codec
// against every value the document can hold.
func TestSnapshotRoundTripCoversEveryValueKind(t *testing.T) {
	doc := NewDoc()
	stamp := time.Date(2026, 7, 26, 4, 5, 6, 123456789, time.UTC)

	values := map[Prop]Value{
		"null":      NullValue(),
		"boolTrue":  BoolValue(true),
		"boolFalse": BoolValue(false),
		"int":       IntValue(-987654321),
		"uint":      UintValue(18446744073709551615),
		"float":     FloatValue(-3.14159265358979),
		"string":    StringValue("hello, 世界 \x00 \"quoted\""),
		"bytes":     BytesValue([]byte{0, 1, 2, 254, 255}),
		"counter":   CounterValue(-42),
		"timestamp": TimestampValue(stamp),
		"vector": {
			Kind:         ValueKindVector,
			VectorPacked: []byte{9, 8, 7},
			VectorNorm:   1.25,
			VectorDim:    3,
			VectorBits:   2,
		},
	}
	for prop, value := range values {
		if err := doc.Put(Root, prop, value); err != nil {
			t.Fatalf("put %s: %v", prop, err)
		}
	}
	if err := doc.Increment(Root, "counter", 8); err != nil {
		t.Fatal(err)
	}
	if err := doc.Delete(Root, "boolFalse"); err != nil {
		t.Fatal(err)
	}

	child, err := doc.MakeMap(Root, "child")
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.Put(child, "nested", StringValue("deep")); err != nil {
		t.Fatal(err)
	}
	listID, err := doc.MakeList(Root, "items")
	if err != nil {
		t.Fatal(err)
	}
	for i, item := range []string{"a", "b", "c"} {
		if err := doc.InsertAt(listID, uint64(i), StringValue(item)); err != nil {
			t.Fatal(err)
		}
	}
	if err := doc.DeleteAt(listID, 1); err != nil {
		t.Fatal(err)
	}
	textID, err := doc.MakeText(Root, "content")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := doc.SpliceText(textID, 0, 0, "hello world"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := doc.SpliceText(textID, 5, 6, ", there"); err != nil {
		t.Fatal(err)
	}
	if _, err := doc.Commit("everything"); err != nil {
		t.Fatal(err)
	}

	saved, err := doc.Save()
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := Load(saved)
	if err != nil {
		t.Fatal(err)
	}

	assertSameDocState(t, doc, reloaded, listID, textID, child)

	// A second save must produce the identical bytes.
	again, err := reloaded.Save()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(saved, again) {
		t.Fatalf("save is not stable: %d bytes then %d bytes", len(saved), len(again))
	}
}

func assertSameDocState(t *testing.T, want, got *Doc, listID, textID, child ObjID) {
	t.Helper()
	if want.ActorID() != got.ActorID() {
		t.Fatalf("actor = %s, want %s", got.ActorID(), want.ActorID())
	}
	want.mu.RLock()
	wantSeq, wantMaxOp, wantChanges, wantDeps := want.seq, want.maxOp, len(want.changes), append([]ChangeHash(nil), want.deps...)
	want.mu.RUnlock()
	got.mu.RLock()
	gotSeq, gotMaxOp, gotChanges, gotDeps := got.seq, got.maxOp, len(got.changes), append([]ChangeHash(nil), got.deps...)
	got.mu.RUnlock()

	if wantSeq != gotSeq || wantMaxOp != gotMaxOp {
		t.Fatalf("seq/maxOp = %d/%d, want %d/%d", gotSeq, gotMaxOp, wantSeq, wantMaxOp)
	}
	if wantChanges != gotChanges {
		t.Fatalf("change count = %d, want %d", gotChanges, wantChanges)
	}
	if len(wantDeps) != len(gotDeps) {
		t.Fatalf("dep count = %d, want %d", len(gotDeps), len(wantDeps))
	}
	for i := range wantDeps {
		if wantDeps[i] != gotDeps[i] {
			t.Fatalf("dep %d = %s, want %s", i, gotDeps[i].String(), wantDeps[i].String())
		}
	}

	for _, prop := range []Prop{"null", "boolTrue", "int", "uint", "float", "string", "bytes", "counter", "timestamp", "vector"} {
		wantValue, _, err := want.Get(Root, prop)
		if err != nil {
			t.Fatalf("source get %s: %v", prop, err)
		}
		gotValue, _, err := got.Get(Root, prop)
		if err != nil {
			t.Fatalf("reloaded get %s: %v", prop, err)
		}
		if !sameValue(wantValue, gotValue) {
			t.Fatalf("prop %s = %#v, want %#v", prop, gotValue, wantValue)
		}
	}
	if _, _, err := got.Get(Root, "boolFalse"); err == nil {
		t.Fatal("deleted prop survived the round trip")
	}
	if value, _, err := got.Get(child, "nested"); err != nil || value.Str != "deep" {
		t.Fatalf("nested map value = %#v err=%v", value, err)
	}

	wantText, _ := want.TextToString(textID)
	gotText, _ := got.TextToString(textID)
	if wantText != gotText {
		t.Fatalf("text = %q, want %q", gotText, wantText)
	}
	wantList, _ := want.TextToString(listID)
	gotList, _ := got.TextToString(listID)
	if wantList != gotList {
		t.Fatalf("list = %q, want %q", gotList, wantList)
	}
}

func sameValue(a, b Value) bool {
	if a.Kind != b.Kind || a.Bool != b.Bool || a.Int != b.Int || a.Uint != b.Uint ||
		a.Float != b.Float || a.Str != b.Str || a.Counter != b.Counter || a.Obj != b.Obj {
		return false
	}
	if !bytes.Equal(a.Bytes, b.Bytes) || !bytes.Equal(a.VectorPacked, b.VectorPacked) {
		return false
	}
	if a.VectorNorm != b.VectorNorm || a.VectorDim != b.VectorDim || a.VectorBits != b.VectorBits {
		return false
	}
	if (a.Time == nil) != (b.Time == nil) {
		return false
	}
	if a.Time != nil && !a.Time.Equal(*b.Time) {
		return false
	}
	return true
}

// TestLoadAcceptsLegacyJSONSnapshot proves that documents written by the older
// JSON snapshot still load.
func TestLoadAcceptsLegacyJSONSnapshot(t *testing.T) {
	doc := NewDoc()
	textID, err := doc.MakeText(Root, "content")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := doc.SpliceText(textID, 0, 0, "legacy text"); err != nil {
		t.Fatal(err)
	}
	if err := doc.Put(Root, "title", StringValue("old format")); err != nil {
		t.Fatal(err)
	}
	if _, err := doc.Commit("seed"); err != nil {
		t.Fatal(err)
	}

	legacy, err := doc.saveLegacyJSON()
	if err != nil {
		t.Fatal(err)
	}
	body, err := enc.DecodeDocument(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if body[0] != '{' {
		t.Fatalf("legacy body starts with %q, want a JSON object", body[0])
	}

	reloaded, err := Load(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := reloaded.TextToString(textID); got != "legacy text" {
		t.Fatalf("legacy text = %q", got)
	}
	if value, _, err := reloaded.Get(Root, "title"); err != nil || value.Str != "old format" {
		t.Fatalf("legacy prop = %#v err=%v", value, err)
	}

	// The binary form must be far smaller for the same content.
	compact, err := reloaded.Save()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("legacy JSON = %d bytes, binary = %d bytes", len(legacy), len(compact))
	if len(compact) >= len(legacy) {
		t.Fatalf("binary snapshot is %d bytes, want fewer than the legacy %d", len(compact), len(legacy))
	}
}

// TestSnapshotSurvivesTombstoneRunsAndMerge covers the run-length tombstone
// column together with a merge from a second replica.
func TestSnapshotSurvivesTombstoneRunsAndMerge(t *testing.T) {
	doc := NewDoc()
	textID, err := doc.MakeText(Root, "content")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := doc.SpliceText(textID, 0, 0, strings.Repeat("abcdefghij", 40)); err != nil {
		t.Fatal(err)
	}
	if _, err := doc.Commit("seed"); err != nil {
		t.Fatal(err)
	}
	// Delete alternating blocks so the tombstone column holds many runs.
	for start := 300; start >= 0; start -= 20 {
		if _, _, err := doc.SpliceText(textID, uint64(start), 10, ""); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := doc.Commit("carve"); err != nil {
		t.Fatal(err)
	}
	want, _ := doc.TextToString(textID)

	saved, err := doc.Save()
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := Load(saved)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := reloaded.TextToString(textID); got != want {
		t.Fatalf("text after reload = %d runes, want %d", len([]rune(got)), len([]rune(want)))
	}

	// A fork that edits the reloaded document must still merge back.
	fork, err := reloaded.Fork()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := fork.SpliceText(textID, 0, 0, "PREFIX"); err != nil {
		t.Fatal(err)
	}
	if _, err := fork.Commit("fork edit"); err != nil {
		t.Fatal(err)
	}
	if err := reloaded.Merge(fork); err != nil {
		t.Fatal(err)
	}
	if got, _ := reloaded.TextToString(textID); got != "PREFIX"+want {
		t.Fatalf("merged text = %q, want %q", got, "PREFIX"+want)
	}
}
