package gosx_test

import (
	"errors"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/format"
	"m31labs.dev/gosx/ir"
	"m31labs.dev/gosx/lsp"
	"m31labs.dev/gosx/transpile"
)

func TestMalformedMarkupIsRejectedByEverySourceConsumer(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "mismatched", body: `<div><span>x</div></span>`},
		{name: "crossed", body: `<a><b>value</a></b>`},
		{name: "missing", body: `<div><span>x</span>`},
		{name: "orphan", body: `</div>`},
		{name: "ordinary tag case remains significant", body: `<DIV>value</div>`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := []byte("package main\n\nfunc Page() Node {\n\treturn " + tt.body + "\n}\n")

			if _, err := gosx.Compile(source); err == nil {
				t.Fatal("Compile accepted malformed markup")
			} else {
				var located *gosx.ParseError
				if errors.As(err, &located) && (located.Line < 1 || located.Column < 1) {
					t.Fatalf("Compile returned an unlocated parse error: %v", err)
				}
			}
			tree, lang, err := gosx.Parse(source)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if _, err := ir.Lower(tree.RootNode(), source, lang); err == nil {
				t.Fatal("ir.Lower accepted malformed markup")
			}
			if _, err := format.Source(source); err == nil {
				t.Fatal("format.Source accepted malformed markup")
			}
			if _, err := transpile.Transpile(source, transpile.Options{SourceFile: tt.name + ".gsx"}); err == nil {
				t.Fatal("transpile.Transpile accepted malformed markup")
			}
			if diags := lsp.Analyze(tt.name+".gsx", source); len(diags) == 0 {
				t.Fatal("lsp.Analyze returned no diagnostic for malformed markup")
			}
		})
	}
}
