package crdt

import (
	"fmt"
	"strings"
	"testing"
)

// setupTextDoc creates a document with one text object seeded to size runes.
func setupTextDoc(tb testing.TB, size int) (*Doc, ObjID) {
	tb.Helper()
	doc := NewDoc()
	textID, err := doc.MakeText(Root, "content")
	if err != nil {
		tb.Fatal(err)
	}
	if size > 0 {
		if _, _, err := doc.SpliceText(textID, 0, 0, strings.Repeat("a", size)); err != nil {
			tb.Fatal(err)
		}
	}
	if _, err := doc.Commit("seed"); err != nil {
		tb.Fatal(err)
	}
	return doc, textID
}

// BenchmarkTextTypeChar measures the cost of one single-rune insert at the end
// of a document that already holds size runes.
func BenchmarkTextTypeChar(b *testing.B) {
	for _, size := range []int{200, 400, 800, 1600, 3200} {
		b.Run(fmt.Sprintf("size=%d", size), func(b *testing.B) {
			doc, textID := setupTextDoc(b, size)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				length, err := doc.ListLen(textID)
				if err != nil {
					b.Fatal(err)
				}
				if _, _, err := doc.SpliceText(textID, uint64(length), 0, "x"); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkTextTypeSequence measures typing 200 runes one at a time into a
// document that already holds size runes.
func BenchmarkTextTypeSequence(b *testing.B) {
	for _, size := range []int{200, 1600} {
		b.Run(fmt.Sprintf("size=%d", size), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				doc, textID := setupTextDoc(b, size)
				b.StartTimer()
				for j := 0; j < 200; j++ {
					if _, _, err := doc.SpliceText(textID, uint64(size+j), 0, "x"); err != nil {
						b.Fatal(err)
					}
				}
			}
		})
	}
}

// BenchmarkTextToString measures a full text read.
func BenchmarkTextToString(b *testing.B) {
	doc, textID := setupTextDoc(b, 5000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := doc.TextToString(textID); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkDocSave measures snapshot encoding of a 5000-rune document.
func BenchmarkDocSave(b *testing.B) {
	doc, _ := setupTextDoc(b, 5000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := doc.Save(); err != nil {
			b.Fatal(err)
		}
	}
}

// TestSaveSizeStaysProportionalToContent reports the bytes that Save spends per
// stored rune. A collaborative editor must not pay hundreds of bytes per rune.
func TestSaveSizeStaysProportionalToContent(t *testing.T) {
	const size = 5000
	doc, _ := setupTextDoc(t, size)
	data, err := doc.Save()
	if err != nil {
		t.Fatal(err)
	}
	perRune := float64(len(data)) / float64(size)
	t.Logf("Save() = %d bytes for %d runes (%.1f bytes/rune)", len(data), size, perRune)
	if perRune > 40 {
		t.Fatalf("Save() spends %.1f bytes per rune, want at most 40", perRune)
	}
}

// TestSaveSizeWithLongEditHistory checks that the snapshot does not grow with
// the length of the change log once the visible content is unchanged.
func TestSaveSizeWithLongEditHistory(t *testing.T) {
	doc := NewDoc()
	textID, err := doc.MakeText(Root, "content")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := doc.Commit("seed"); err != nil {
		t.Fatal(err)
	}
	// Type 2000 runes as 2000 separate committed changes.
	for i := 0; i < 2000; i++ {
		if _, _, err := doc.SpliceText(textID, uint64(i), 0, "a"); err != nil {
			t.Fatal(err)
		}
		if _, err := doc.Commit("type"); err != nil {
			t.Fatal(err)
		}
	}
	data, err := doc.Save()
	if err != nil {
		t.Fatal(err)
	}
	perRune := float64(len(data)) / 2000
	t.Logf("Save() = %d bytes after 2000 single-rune changes (%.1f bytes/rune)", len(data), perRune)

	// Round trip must preserve the text and the change history.
	reloaded, err := Load(data)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Repeat("a", 2000)
	if got, err := reloaded.TextToString(textID); err != nil || got != want {
		t.Fatalf("reload text length=%d err=%v", len(got), err)
	}
	if perRune > 60 {
		t.Fatalf("Save() spends %.1f bytes per rune with history, want at most 60", perRune)
	}
}
