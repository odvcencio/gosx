package crdt

import (
	"strconv"
	"testing"
	"time"
)

// lookupDoc builds a document that carries one of every shape a lookup meets:
// a live map key, a deleted map key, a nested map, a list, and a text.
func lookupDoc(t *testing.T) (*Doc, ObjID, ObjID) {
	t.Helper()
	doc := NewDoc()
	if err := doc.Put(Root, "title", StringValue("hello")); err != nil {
		t.Fatal(err)
	}
	if err := doc.Put(Root, "count", IntValue(7)); err != nil {
		t.Fatal(err)
	}
	if err := doc.Put(Root, "flag", BoolValue(true)); err != nil {
		t.Fatal(err)
	}
	if err := doc.Put(Root, "blob", BytesValue([]byte{1, 2, 3})); err != nil {
		t.Fatal(err)
	}
	if err := doc.Put(Root, "gone", StringValue("bye")); err != nil {
		t.Fatal(err)
	}
	if err := doc.Delete(Root, "gone"); err != nil {
		t.Fatal(err)
	}
	child, err := doc.MakeMap(Root, "child")
	if err != nil {
		t.Fatal(err)
	}
	list, err := doc.MakeList(Root, "list")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		if err := doc.InsertAt(list, uint64(i), StringValue("item"+strconv.Itoa(i))); err != nil {
			t.Fatal(err)
		}
	}
	text, err := doc.MakeText(Root, "text")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := doc.SpliceText(text, 0, 0, "abc"); err != nil {
		t.Fatal(err)
	}
	if _, err := doc.Commit("seed"); err != nil {
		t.Fatal(err)
	}
	return doc, child, list
}

// TestLookupAgreesWithGet proves the fast presence test and the error-forming
// read reach the same answer. Two readers of one document that disagree would
// be the worst outcome of splitting the read path, so pin them together.
func TestLookupAgreesWithGet(t *testing.T) {
	doc, child, list := lookupDoc(t)

	cases := []struct {
		name string
		obj  ObjID
		prop Prop
	}{
		{"live string", Root, "title"},
		{"live int", Root, "count"},
		{"live bool", Root, "flag"},
		{"live bytes", Root, "blob"},
		{"deleted key", Root, "gone"},
		{"absent key", Root, "nothing"},
		{"nested map handle", Root, "child"},
		{"empty child key", child, "anything"},
		{"unknown object", ObjID("no-such-object"), "title"},
		{"list index 0", list, "0"},
		{"list index 3", list, "3"},
		{"list index past end", list, "4"},
		{"list index negative", list, "-1"},
		{"list prop not an index", list, "title"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wantValue, wantObj, err := doc.Get(tc.obj, tc.prop)
			gotValue, gotObj, ok := doc.Lookup(tc.obj, tc.prop)
			if ok != (err == nil) {
				t.Fatalf("Lookup ok = %v, Get err = %v", ok, err)
			}
			if gotObj != wantObj {
				t.Fatalf("Lookup obj = %q, Get obj = %q", gotObj, wantObj)
			}
			if gotValue.Kind != wantValue.Kind || gotValue.Str != wantValue.Str ||
				gotValue.Int != wantValue.Int || gotValue.Bool != wantValue.Bool ||
				string(gotValue.Bytes) != string(wantValue.Bytes) {
				t.Fatalf("Lookup value = %+v, Get value = %+v", gotValue, wantValue)
			}
		})
	}
}

// TestLookupKeyAgreesWithLookup proves the byte-key form of the map read
// matches the string form, including the keys it must refuse.
func TestLookupKeyAgreesWithLookup(t *testing.T) {
	doc, child, list := lookupDoc(t)

	cases := []struct {
		obj ObjID
		key string
	}{
		{Root, "title"},
		{Root, "gone"},
		{Root, "absent"},
		{Root, ""},
		{child, "title"},
		{ObjID("no-such-object"), "title"},
	}
	for _, tc := range cases {
		wantValue, wantObj, wantOK := doc.Lookup(tc.obj, Prop(tc.key))
		gotValue, gotObj, gotOK := doc.LookupKey(tc.obj, []byte(tc.key))
		if gotOK != wantOK || gotObj != wantObj || gotValue.Str != wantValue.Str {
			t.Fatalf("LookupKey(%q, %q) = %+v %q %v, Lookup = %+v %q %v",
				tc.obj, tc.key, gotValue, gotObj, gotOK, wantValue, wantObj, wantOK)
		}
	}

	// A list has positions, not named keys, so a byte key means nothing there.
	if _, _, ok := doc.LookupKey(list, []byte("0")); ok {
		t.Fatal("LookupKey read a list, which has no named keys")
	}
}

// TestLookupIndexAgreesWithLookup proves the positional read matches the read
// that formats the index into a property name.
func TestLookupIndexAgreesWithLookup(t *testing.T) {
	doc, child, list := lookupDoc(t)

	for index := -2; index < 6; index++ {
		wantValue, wantObj, wantOK := doc.Lookup(list, Prop(strconv.Itoa(index)))
		gotValue, gotObj, gotOK := doc.LookupIndex(list, index)
		if gotOK != wantOK || gotObj != wantObj || gotValue.Str != wantValue.Str {
			t.Fatalf("LookupIndex(%d) = %+v %q %v, Lookup = %+v %q %v",
				index, gotValue, gotObj, gotOK, wantValue, wantObj, wantOK)
		}
	}

	// A map has named keys, not positions.
	if _, _, ok := doc.LookupIndex(Root, 0); ok {
		t.Fatal("LookupIndex read a map, which has no positions")
	}
	if _, _, ok := doc.LookupIndex(child, 0); ok {
		t.Fatal("LookupIndex read an empty map")
	}
	if _, _, ok := doc.LookupIndex(ObjID("no-such-object"), 0); ok {
		t.Fatal("LookupIndex read an unknown object")
	}
}

// TestLookupIndexReadsTextRunes proves the positional read reaches a text
// object, which stores runes as list elements.
func TestLookupIndexReadsTextRunes(t *testing.T) {
	doc := NewDoc()
	text, err := doc.MakeText(Root, "body")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := doc.SpliceText(text, 0, 0, "hey"); err != nil {
		t.Fatal(err)
	}
	for index, want := range []string{"h", "e", "y"} {
		value, _, ok := doc.LookupIndex(text, index)
		if !ok || value.Str != want {
			t.Fatalf("LookupIndex(%d) = %q %v, want %q", index, value.Str, ok, want)
		}
	}
	if _, _, ok := doc.LookupIndex(text, 3); ok {
		t.Fatal("LookupIndex read past the end of the text")
	}
}

// TestLookupClonesTheStoredValue proves a caller cannot reach into the document
// through the value it reads. Get has always cloned; the lookups must too, or a
// caller could edit a document without an operation and break convergence.
func TestLookupClonesTheStoredValue(t *testing.T) {
	doc := NewDoc()
	if err := doc.Put(Root, "blob", BytesValue([]byte{1, 2, 3})); err != nil {
		t.Fatal(err)
	}
	stamp := time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC)
	if err := doc.Put(Root, "stamp", TimestampValue(stamp)); err != nil {
		t.Fatal(err)
	}

	blob, _, ok := doc.Lookup(Root, "blob")
	if !ok {
		t.Fatal("blob is absent")
	}
	blob.Bytes[0] = 99

	keyed, _, ok := doc.LookupKey(Root, []byte("blob"))
	if !ok {
		t.Fatal("blob is absent through the byte key")
	}
	if keyed.Bytes[0] != 1 {
		t.Fatalf("the document saw a caller's write: blob[0] = %d, want 1", keyed.Bytes[0])
	}

	moment, _, ok := doc.Lookup(Root, "stamp")
	if !ok {
		t.Fatal("stamp is absent")
	}
	*moment.Time = moment.Time.Add(time.Hour)
	again, _, ok := doc.Lookup(Root, "stamp")
	if !ok {
		t.Fatal("stamp is absent on the second read")
	}
	if !again.Time.Equal(stamp) {
		t.Fatalf("the document saw a caller's write: stamp = %v, want %v", again.Time, stamp)
	}
}

// TestLookupAllocatesNothingOnAMiss is the measurement this API exists for. An
// absent property is the normal answer for a presence test, and Get built a
// message for it that every such caller threw away.
func TestLookupAllocatesNothingOnAMiss(t *testing.T) {
	doc := NewDoc()
	if err := doc.Put(Root, "title", StringValue("hello")); err != nil {
		t.Fatal(err)
	}

	miss := testing.AllocsPerRun(200, func() {
		if _, _, ok := doc.Lookup(Root, "absent"); ok {
			t.Fatal("absent property read as present")
		}
	})
	if miss != 0 {
		t.Fatalf("Lookup on a miss allocated %.1f times, want 0", miss)
	}

	hit := testing.AllocsPerRun(200, func() {
		if _, _, ok := doc.Lookup(Root, "title"); !ok {
			t.Fatal("present property read as absent")
		}
	})
	if hit != 0 {
		t.Fatalf("Lookup on a hit allocated %.1f times, want 0", hit)
	}
}

// TestLookupKeyDoesNotCopyTheKey proves the byte key never becomes a string. A
// reader that walks many keys built from one prefix depends on that.
func TestLookupKeyDoesNotCopyTheKey(t *testing.T) {
	doc := NewDoc()
	if err := doc.Put(Root, "scene/o/obj-00001/c", StringValue("payload")); err != nil {
		t.Fatal(err)
	}

	key := make([]byte, 0, 64)
	allocs := testing.AllocsPerRun(200, func() {
		key = append(key[:0], "scene/o/obj-00001/c"...)
		if _, _, ok := doc.LookupKey(Root, key); !ok {
			t.Fatal("key read as absent")
		}
		key = append(key[:0], "scene/o/obj-00001/x"...)
		if _, _, ok := doc.LookupKey(Root, key); ok {
			t.Fatal("absent key read as present")
		}
	})
	if allocs != 0 {
		t.Fatalf("two LookupKey reads allocated %.1f times, want 0", allocs)
	}
}

// TestLookupIndexAllocatesNothing proves a positional read costs no index text.
func TestLookupIndexAllocatesNothing(t *testing.T) {
	doc := NewDoc()
	list, err := doc.MakeList(Root, "list")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 200; i++ {
		if err := doc.InsertAt(list, uint64(i), StringValue("item")); err != nil {
			t.Fatal(err)
		}
	}
	allocs := testing.AllocsPerRun(200, func() {
		if _, _, ok := doc.LookupIndex(list, 137); !ok {
			t.Fatal("element 137 read as absent")
		}
	})
	if allocs != 0 {
		t.Fatalf("LookupIndex allocated %.1f times, want 0", allocs)
	}
}
