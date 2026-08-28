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

func TestParsePreviewModeQueryRequiresOrigin(t *testing.T) {
	if _, _, ok := parsePreviewModeQuery("?gosx-preview=1"); ok {
		t.Fatal("preview without an explicit origin must stay disabled")
	}
	query := inspectPreviewModeQuery("?gosx-preview=1")
	if query.diagnostic != "gosx-preview-origin is required" {
		t.Fatalf("expected missing-origin diagnostic, got %q", query.diagnostic)
	}
}

func TestParsePreviewModeQueryPinnedOrigin(t *testing.T) {
	prefix, origin, ok := parsePreviewModeQuery("?gosx-preview=1&gosx-preview-origin=https%3A%2F%2Feditor.example")
	if !ok {
		t.Fatal("preview activated")
	}
	if origin != "https://editor.example" {
		t.Fatalf("expected decoded origin, got %q", origin)
	}
	if prefix != "$preview." {
		t.Fatalf("expected default prefix $preview., got %q", prefix)
	}
}

func TestParsePreviewModeQueryCustomPrefix(t *testing.T) {
	prefix, _, ok := parsePreviewModeQuery("?gosx-preview=1&gosx-preview-origin=https%3A%2F%2Feditor.example&gosx-preview-prefix=%24custom.")
	if !ok {
		t.Fatal("preview activated")
	}
	if prefix != "$custom." {
		t.Fatalf("expected decoded prefix $custom., got %q", prefix)
	}
}

func TestParsePreviewModeQueryAcceptsTrue(t *testing.T) {
	if _, _, ok := parsePreviewModeQuery("?gosx-preview=true&gosx-preview-origin=https%3A%2F%2Feditor.example"); !ok {
		t.Fatal("gosx-preview=true should also activate the relay")
	}
}

func TestParsePreviewModeQueryRejectsWildcardOrigin(t *testing.T) {
	query := inspectPreviewModeQuery("?gosx-preview=1&gosx-preview-origin=*")
	if query.enabled {
		t.Fatal("URL-driven preview must reject wildcard origin")
	}
	if query.diagnostic != "gosx-preview-origin must not use the wildcard origin" {
		t.Fatalf("expected wildcard diagnostic, got %q", query.diagnostic)
	}
}

func TestParsePreviewModeQueryRejectsZero(t *testing.T) {
	query := inspectPreviewModeQuery("?gosx-preview=0")
	if query.enabled {
		t.Fatal("gosx-preview=0 should NOT activate the relay")
	}
	if query.diagnostic != "" {
		t.Fatalf("inactive preview should not warn, got %q", query.diagnostic)
	}
}

func TestParsePreviewModeQueryRejectsMalformedOrigin(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  string
	}{
		{
			name:  "bad percent escape",
			query: "?gosx-preview=1&gosx-preview-origin=https%3A%2F%2Feditor.example%ZZ",
			want:  "gosx-preview-origin is malformed",
		},
		{
			name:  "relative URL",
			query: "?gosx-preview=1&gosx-preview-origin=editor.example",
			want:  "gosx-preview-origin must be an absolute http(s) origin without a path",
		},
		{
			name:  "origin with path",
			query: "?gosx-preview=1&gosx-preview-origin=https%3A%2F%2Feditor.example%2Fpreview",
			want:  "gosx-preview-origin must be an absolute http(s) origin without a path",
		},
		{
			name:  "non-web scheme",
			query: "?gosx-preview=1&gosx-preview-origin=javascript%3Aalert%281%29",
			want:  "gosx-preview-origin must be an absolute http(s) origin without a path",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := inspectPreviewModeQuery(tt.query)
			if got.enabled {
				t.Fatal("invalid origin activated preview relay")
			}
			if got.diagnostic != tt.want {
				t.Fatalf("diagnostic = %q, want %q", got.diagnostic, tt.want)
			}
		})
	}
}

func TestParsePreviewModeQueryAcceptsLocalhostPort(t *testing.T) {
	prefix, origin, ok := parsePreviewModeQuery("?gosx-preview=1&gosx-preview-origin=http%3A%2F%2Flocalhost%3A4173")
	if !ok {
		t.Fatal("valid localhost origin did not activate preview relay")
	}
	if prefix != "$preview." || origin != "http://localhost:4173" {
		t.Fatalf("got prefix=%q origin=%q", prefix, origin)
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
