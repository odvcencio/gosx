package main

import "testing"

// Tests for parsePreviewModeQuery — a non-js-wasm pure function so a
// regular `go test` invocation can exercise it without a wasm runtime.
// See cross_frame_parse.go for the function and ADR 0009 for context.

func TestParsePreviewModeQueryEmpty(t *testing.T) {
	if _, _, ok := parsePreviewModeQuery(""); ok {
		t.Fatal("empty query should NOT activate the relay")
	}
	if _, _, ok := parsePreviewModeQuery("?"); ok {
		t.Fatal("bare ? should NOT activate the relay")
	}
	if _, _, ok := parsePreviewModeQuery("?foo=bar"); ok {
		t.Fatal("unrelated query should NOT activate the relay")
	}
}

func TestParsePreviewModeQueryActivates(t *testing.T) {
	prefix, origin, ok := parsePreviewModeQuery("?gosx-preview=1")
	if !ok {
		t.Fatal("gosx-preview=1 should activate the relay")
	}
	if prefix != "$preview." {
		t.Fatalf("expected default prefix $preview., got %q", prefix)
	}
	if origin != "*" {
		t.Fatalf("expected default origin * (dev), got %q", origin)
	}
}

func TestParsePreviewModeQueryPinnedOrigin(t *testing.T) {
	_, origin, ok := parsePreviewModeQuery("?gosx-preview=1&gosx-preview-origin=https%3A%2F%2Feditor.example")
	if !ok {
		t.Fatal("preview activated")
	}
	if origin != "https://editor.example" {
		t.Fatalf("expected decoded origin, got %q", origin)
	}
}

func TestParsePreviewModeQueryCustomPrefix(t *testing.T) {
	prefix, _, ok := parsePreviewModeQuery("?gosx-preview=1&gosx-preview-prefix=%24custom.")
	if !ok {
		t.Fatal("preview activated")
	}
	if prefix != "$custom." {
		t.Fatalf("expected decoded prefix $custom., got %q", prefix)
	}
}

func TestParsePreviewModeQueryAcceptsTrue(t *testing.T) {
	if _, _, ok := parsePreviewModeQuery("?gosx-preview=true"); !ok {
		t.Fatal("gosx-preview=true should also activate the relay")
	}
}

func TestParsePreviewModeQueryRejectsZero(t *testing.T) {
	if _, _, ok := parsePreviewModeQuery("?gosx-preview=0"); ok {
		t.Fatal("gosx-preview=0 should NOT activate the relay")
	}
}

func TestDecodeQueryComponentPercent(t *testing.T) {
	out, err := decodeQueryComponent("hello%20world")
	if err != nil {
		t.Fatalf("decodeQueryComponent: %v", err)
	}
	if out != "hello world" {
		t.Fatalf("expected decoded value, got %q", out)
	}
}

func TestDecodeQueryComponentTruncatedRejected(t *testing.T) {
	if _, err := decodeQueryComponent("hello%2"); err == nil {
		t.Fatal("expected error for truncated escape")
	}
}

func TestDecodeQueryComponentInvalidHex(t *testing.T) {
	if _, err := decodeQueryComponent("hello%ZZ"); err == nil {
		t.Fatal("expected error for invalid hex digits")
	}
}
