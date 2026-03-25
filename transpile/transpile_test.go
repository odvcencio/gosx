package transpile

import (
	"errors"
	"strings"
	"testing"

	"github.com/odvcencio/gosx"
)

func TestTranspileParseErrorIncludesLocationAndSnippet(t *testing.T) {
	source := []byte(`package main

func Broken() Node {
	return <div>{</div>
}
`)

	_, err := Transpile(source, Options{SourceFile: "broken.gsx"})
	if err == nil {
		t.Fatal("expected parse error")
	}

	var parseErr *gosx.ParseError
	if !errors.As(err, &parseErr) {
		t.Fatalf("expected ParseError, got %T: %v", err, err)
	}
	if parseErr.Line == 0 || parseErr.Column == 0 {
		t.Fatalf("expected line/column, got %d:%d", parseErr.Line, parseErr.Column)
	}
	if !strings.Contains(parseErr.Snippet, `return <div>{</div>`) {
		t.Fatalf("expected source snippet, got %q", parseErr.Snippet)
	}
}
