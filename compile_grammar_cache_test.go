//go:build !tinygo

package gosx

import (
	"strings"
	"testing"
)

// A corrupt embedded grammar blob used to fail its load/smoke-parse
// silently and fall back to full runtime regeneration (~40s per
// loadGSXLanguageBlob's own comment), with the load error discarded.
// The blob is now required to load correctly by default; runtime
// regeneration is an explicit opt-in, and a corrupt blob otherwise fails
// loudly. These tests exercise loadGSXLanguageBlob directly so a corrupt
// blob's failure path never pays the ~40s regeneration cost.

func TestLoadGSXLanguageBlobCorruptFailsWithoutOptIn(t *testing.T) {
	t.Setenv(grammarRegenEnvVar, "")
	_, err := loadGSXLanguageBlob([]byte("not a real grammar blob"))
	if err == nil {
		t.Fatal("expected an error for a corrupt grammar blob")
	}
	if !strings.Contains(err.Error(), grammarRegenEnvVar) {
		t.Fatalf("error should name the opt-in env var %q so a reader knows how to allow regeneration, got: %v", grammarRegenEnvVar, err)
	}
}

func TestLoadGSXLanguageBlobCorruptRegeneratesWithOptIn(t *testing.T) {
	if testing.Short() {
		t.Skip("regenerating the grammar from source takes ~40s; skipped in short mode")
	}
	t.Setenv(grammarRegenEnvVar, "1")
	lang, err := loadGSXLanguageBlob([]byte("not a real grammar blob"))
	if err != nil {
		t.Fatalf("loadGSXLanguageBlob with the opt-in env var set: %v", err)
	}
	if lang == nil {
		t.Fatal("expected a regenerated language, got nil")
	}
}

// gosxGrammarCache.setBlob's already-initialized behavior — a second
// SetGrammarBlob call after the language is already cached used to be a
// silent no-op regardless of whether the second call's data actually
// matched what was loaded. These tests exercise a private cache instance
// (never the shared gosxLang singleton every other test in this package
// also relies on staying warm), so they can freely simulate both an
// initialized and an uninitialized cache.

func TestGosxGrammarCacheSetBlobIdempotentForIdenticalBytes(t *testing.T) {
	c := &gosxGrammarCache{}
	blobA := []byte("blob-a")

	// Whether the load itself succeeds is irrelevant to this test; only
	// the caching contract (does a second identical call error) is
	// under test, so no opt-in regen env var is set — the first call is
	// allowed to fail its own load.
	_ = c.setBlob(blobA, nil)

	if err := c.setBlob(blobA, nil); err != nil {
		t.Fatalf("a second setBlob call with byte-identical data should be idempotent, got: %v", err)
	}
}

func TestGosxGrammarCacheSetBlobAfterInitWithDifferentBytesErrors(t *testing.T) {
	c := &gosxGrammarCache{}
	blobA := []byte("blob-a")
	blobB := []byte("blob-b")

	_ = c.setBlob(blobA, nil)

	err := c.setBlob(blobB, nil)
	if err == nil {
		t.Fatal("expected an error setting a different grammar blob after the cache already initialized")
	}
	if !strings.Contains(err.Error(), "already initialized") {
		t.Fatalf("error should say the cache is already initialized, got: %v", err)
	}
}
