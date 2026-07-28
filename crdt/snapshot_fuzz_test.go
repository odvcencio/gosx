package crdt

import (
	"strings"
	"testing"
)

// FuzzLoadBinarySnapshotNeverPanics feeds mutated binary snapshots to Load. The
// seeds are real documents, so the fuzzer reaches the binary decoder instead of
// stopping at the chunk header.
func FuzzLoadBinarySnapshotNeverPanics(f *testing.F) {
	f.Add(mustSaveSeed(f, func(doc *Doc) {
		if err := doc.Put(Root, "title", StringValue("seed")); err != nil {
			f.Fatal(err)
		}
	}))
	f.Add(mustSaveSeed(f, func(doc *Doc) {
		textID, err := doc.MakeText(Root, "content")
		if err != nil {
			f.Fatal(err)
		}
		if _, _, err := doc.SpliceText(textID, 0, 0, strings.Repeat("xy", 40)); err != nil {
			f.Fatal(err)
		}
		if _, _, err := doc.SpliceText(textID, 10, 20, "cut"); err != nil {
			f.Fatal(err)
		}
	}))
	f.Add(mustSaveSeed(f, func(doc *Doc) {
		listID, err := doc.MakeList(Root, "items")
		if err != nil {
			f.Fatal(err)
		}
		for i := 0; i < 8; i++ {
			if err := doc.InsertAt(listID, uint64(i), IntValue(int64(i))); err != nil {
				f.Fatal(err)
			}
		}
		if err := doc.DeleteAt(listID, 3); err != nil {
			f.Fatal(err)
		}
		if err := doc.Increment(Root, "hits", 5); err != nil {
			f.Fatal(err)
		}
	}))

	f.Fuzz(func(t *testing.T, payload []byte) {
		if len(payload) > 65536 {
			return
		}
		doc, err := Load(payload)
		if err != nil || doc == nil {
			return
		}
		// A document that loads must answer every read without panicking.
		_, _ = doc.Save()
		for id := range doc.objects {
			_, _ = doc.TextToString(id)
			_, _ = doc.ListLen(id)
			_, _ = doc.ElementIDAt(id, 0)
			_, _, _ = doc.Get(id, "0")
		}
	})
}

func mustSaveSeed(f *testing.F, build func(*Doc)) []byte {
	f.Helper()
	doc := NewDoc()
	build(doc)
	if _, err := doc.Commit("seed"); err != nil {
		f.Fatal(err)
	}
	saved, err := doc.Save()
	if err != nil {
		f.Fatal(err)
	}
	return saved
}
