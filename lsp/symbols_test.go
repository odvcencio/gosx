package lsp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

const lspComponentSource = `package main

func Card() Node {
	return <section>Card</section>
}

func Page() Node {
	return <main><Card /></main>
}
`

func TestDocumentSymbolsReturnsComponents(t *testing.T) {
	symbols := DocumentSymbols("page.gsx", []byte(lspComponentSource))
	if len(symbols) != 2 {
		t.Fatalf("expected 2 symbols, got %d", len(symbols))
	}
	if symbols[0].Name != "Card" || symbols[1].Name != "Page" {
		t.Fatalf("unexpected symbols: %#v", symbols)
	}
	if symbols[0].Detail != "func Card() Node" || symbols[0].Kind != symbolKindFunction {
		t.Fatalf("unexpected Card symbol: %#v", symbols[0])
	}
	if symbols[1].SelectionRange.Start.Line <= symbols[0].SelectionRange.Start.Line {
		t.Fatalf("expected Page selection after Card: %#v", symbols)
	}
}

func TestHoverAtComponentDefinition(t *testing.T) {
	pos := testPositionIn(lspComponentSource, "Page() Node", 1)
	hover := HoverAt("page.gsx", []byte(lspComponentSource), pos)
	if hover == nil {
		t.Fatal("expected hover")
	}
	if hover.Contents.Kind != "markdown" {
		t.Fatalf("expected markdown hover, got %#v", hover.Contents)
	}
	if !strings.Contains(hover.Contents.Value, "func Page() Node") {
		t.Fatalf("unexpected hover content %q", hover.Contents.Value)
	}
}

func TestDefinitionAtLocalComponentReference(t *testing.T) {
	symbols := DocumentSymbols("page.gsx", []byte(lspComponentSource))
	if len(symbols) == 0 {
		t.Fatal("expected symbols")
	}

	pos := testPositionIn(lspComponentSource, "<Card", 2)
	loc := DefinitionAt("file:///tmp/page.gsx", "/tmp/page.gsx", []byte(lspComponentSource), pos)
	if loc == nil {
		t.Fatal("expected definition location")
	}
	if loc.URI != "file:///tmp/page.gsx" {
		t.Fatalf("unexpected definition URI %q", loc.URI)
	}
	if loc.Range != symbols[0].SelectionRange {
		t.Fatalf("expected Card selection range %#v, got %#v", symbols[0].SelectionRange, loc.Range)
	}
}

func TestServerAdvertisesEditorCapabilities(t *testing.T) {
	input := bytes.NewBuffer(nil)
	output := bytes.NewBuffer(nil)

	writeFramedJSON(input, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	writeFramedJSON(input, `{"jsonrpc":"2.0","id":2,"method":"shutdown","params":{}}`)

	if err := Serve(input, output); err != nil {
		t.Fatalf("serve: %v", err)
	}

	wire := output.String()
	for _, field := range []string{
		`"documentSymbolProvider":true`,
		`"hoverProvider":true`,
		`"definitionProvider":true`,
	} {
		if !strings.Contains(wire, field) {
			t.Fatalf("expected %s in initialize response, got %s", field, wire)
		}
	}
}

func TestServerHandlesSymbolsHoverAndDefinition(t *testing.T) {
	input := bytes.NewBuffer(nil)
	output := bytes.NewBuffer(nil)
	uri := "file:///tmp/page.gsx"
	hoverPos := testPositionIn(lspComponentSource, "Page() Node", 1)
	defPos := testPositionIn(lspComponentSource, "<Card", 2)

	writeFramedJSON(input, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	writeFramedJSON(input, fmt.Sprintf(`{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":%s,"version":1,"text":%s}}}`, quoteJSON(uri), quoteJSON(lspComponentSource)))
	writeFramedJSON(input, fmt.Sprintf(`{"jsonrpc":"2.0","id":2,"method":"textDocument/documentSymbol","params":{"textDocument":{"uri":%s}}}`, quoteJSON(uri)))
	writeFramedJSON(input, fmt.Sprintf(`{"jsonrpc":"2.0","id":3,"method":"textDocument/hover","params":{"textDocument":{"uri":%s},"position":{"line":%d,"character":%d}}}`, quoteJSON(uri), hoverPos.Line, hoverPos.Character))
	writeFramedJSON(input, fmt.Sprintf(`{"jsonrpc":"2.0","id":4,"method":"textDocument/definition","params":{"textDocument":{"uri":%s},"position":{"line":%d,"character":%d}}}`, quoteJSON(uri), defPos.Line, defPos.Character))
	writeFramedJSON(input, `{"jsonrpc":"2.0","id":5,"method":"shutdown","params":{}}`)

	if err := Serve(input, output); err != nil {
		t.Fatalf("serve: %v", err)
	}

	wire := output.String()
	for _, want := range []string{
		`"name":"Card"`,
		`"name":"Page"`,
		`"kind":"markdown"`,
		`func Page() Node`,
		`"uri":"file:///tmp/page.gsx"`,
	} {
		if !strings.Contains(wire, want) {
			t.Fatalf("expected %q in wire output, got %s", want, wire)
		}
	}
}

func testPositionIn(source, needle string, delta int) Position {
	offset := strings.Index(source, needle)
	if offset < 0 {
		panic("missing test needle: " + needle)
	}
	return positionForOffset(source, offset+delta)
}

func quoteJSON(value string) string {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(data)
}
