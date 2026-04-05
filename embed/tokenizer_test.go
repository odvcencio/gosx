package embed

import (
	"path/filepath"
	"runtime"
	"testing"
)

func testdataDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "testdata")
}

func loadTestTokenizer(t *testing.T) *Tokenizer {
	t.Helper()
	dir := testdataDir()
	tok, err := LoadTokenizer(TokenizerFiles{
		VocabPath:         filepath.Join(dir, "vocab.json"),
		MergesPath:        filepath.Join(dir, "merges.txt"),
		SpecialTokensPath: filepath.Join(dir, "special_tokens.json"),
	})
	if err != nil {
		t.Fatalf("LoadTokenizer: %v", err)
	}
	return tok
}

func TestTokenizerHelloWorld(t *testing.T) {
	tok := loadTestTokenizer(t)
	ids := tok.Encode("hello world")

	// Expected: [CLS]=100, "hello"=12, "\u0120world"=17, [SEP]=101
	want := []int{100, 12, 17, 101}
	assertTokenIDs(t, "hello world", ids, want)
}

func TestTokenizerEmpty(t *testing.T) {
	tok := loadTestTokenizer(t)
	ids := tok.Encode("")
	want := []int{100, 101} // [CLS] [SEP] only
	assertTokenIDs(t, "<empty>", ids, want)
}

func TestTokenizerPunctuationSplit(t *testing.T) {
	tok := loadTestTokenizer(t)
	ids := tok.Encode("hello!")

	// "hello" merges to 12, "!" is punctuation split -> 18
	// Expected: [CLS]=100, "hello"=12, "!"=18, [SEP]=101
	want := []int{100, 12, 18, 101}
	assertTokenIDs(t, "hello!", ids, want)
}

func TestTokenizerUnknownToken(t *testing.T) {
	tok := loadTestTokenizer(t)
	ids := tok.Encode("z")

	// "z" is not in vocab, should map to [UNK]=103
	want := []int{100, 103, 101}
	assertTokenIDs(t, "z", ids, want)
}

func TestTokenizerRoundTrip(t *testing.T) {
	tok := loadTestTokenizer(t)

	// "hello world" round-trips cleanly since all tokens are in vocab
	ids := tok.Encode("hello world")
	decoded := tok.Decode(ids)
	if decoded != "hello world" {
		t.Errorf("round-trip: got %q, want %q", decoded, "hello world")
	}
}

func TestTokenizerMergeOrder(t *testing.T) {
	tok := loadTestTokenizer(t)

	// "abc" should merge a+b -> ab, then ab+c -> abc (ID 25)
	ids := tok.Encode("abc")
	want := []int{100, 25, 101}
	assertTokenIDs(t, "abc", ids, want)
}

func TestTokenizerVocabSize(t *testing.T) {
	tok := loadTestTokenizer(t)
	if tok.VocabSize() == 0 {
		t.Fatal("VocabSize() returned 0")
	}
}

// Tests using the in-memory NewTokenizer constructor.

func TestTokenizerEncodeDecode(t *testing.T) {
	vocab := map[string]int{
		"h": 0, "e": 1, "l": 2, "o": 3, " ": 4, "w": 5,
		"r": 6, "d": 7, "he": 8, "ll": 9, "lo": 10,
		"wo": 11, "rl": 12, "hel": 13, "hello": 14, "world": 15,
	}
	merges := []string{"h e", "l l", "l o", "w o", "r l", "he l", "hel lo", "wo rl", "worl d"}
	tok := NewTokenizer(vocab, merges)
	ids := tok.Encode("hello world")
	text := tok.Decode(ids)
	// Should round-trip
	if text != "hello world" {
		t.Errorf("round-trip got %q want %q", text, "hello world")
	}
}

func TestTokenizerVocabSizeInMemory(t *testing.T) {
	vocab := map[string]int{"a": 0, "b": 1, "c": 2}
	tok := NewTokenizer(vocab, nil)
	if tok.VocabSize() != 3 {
		t.Errorf("VocabSize() = %d want 3", tok.VocabSize())
	}
}

func TestTokenizerUnknownTokenInMemory(t *testing.T) {
	vocab := map[string]int{"a": 0, "b": 1}
	tok := NewTokenizer(vocab, nil)
	// Characters not in vocab should not crash
	ids := tok.Encode("abc")
	if len(ids) == 0 {
		t.Error("expected non-empty token IDs for input with partial vocab coverage")
	}
}

func assertTokenIDs(t *testing.T, label string, got, want []int) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: got %d tokens %v, want %d tokens %v", label, len(got), got, len(want), want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("%s: token[%d] = %d, want %d (full: got %v, want %v)", label, i, got[i], want[i], got, want)
		}
	}
}
