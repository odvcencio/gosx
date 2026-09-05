//go:build !tinygo

package gosx

import (
	"errors"
	"strings"
	"testing"
)

func TestApproximateParsePointUsesByteBoundaryForNonASCII(t *testing.T) {
	text := "package main\n\nfunc Broken() Node {\n\treturn <div>é𐐀"
	line, column, snippet, excerpt := approximateParsePoint(text, []byte(text))
	if line != 4 {
		t.Fatalf("line = %d, want 4", line)
	}
	// The fallback points at the final useful rune. Keep ParseError.Column in
	// source-byte units like gotreesitter StartPoint columns, but never inside a
	// UTF-8 sequence. The final 𐐀 starts at 1-based byte column 16.
	if column != 16 {
		t.Fatalf("column = %d, want 16 byte-boundary column", column)
	}
	if snippet != "\treturn <div>é𐐀" {
		t.Fatalf("snippet = %q", snippet)
	}
	if excerpt != "return <div>é𐐀" {
		t.Fatalf("excerpt = %q", excerpt)
	}
}

func TestCompileMalformedUnicodeParseErrorUsesByteColumn(t *testing.T) {
	source := []byte("package main\n\nfunc Broken() Node {\n\treturn <div>é𐐀")
	_, err := Compile(source)
	if err == nil {
		t.Fatal("Compile succeeded, want parse error")
	}
	var parseErr *ParseError
	if !errors.As(err, &parseErr) {
		t.Fatalf("Compile error = %T %[1]v, want *ParseError", err)
	}
	if parseErr.Line != 4 || parseErr.Column != 16 {
		t.Fatalf("parse position = %d:%d, want 4:16 byte column at final non-BMP rune", parseErr.Line, parseErr.Column)
	}
	if parseErr.Snippet != "\treturn <div>é𐐀" {
		t.Fatalf("snippet = %q", parseErr.Snippet)
	}
	text := parseErr.Error()
	if !strings.Contains(text, "4:16:") || !strings.Contains(text, "\treturn <div>é𐐀") {
		t.Fatalf("error text = %q, want byte-column location and Unicode snippet", text)
	}
}
