package lsp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAnalyzeValidationDiagnosticUsesUTF16Position(t *testing.T) {
	source := []byte(`package main

type TeamMarkProps struct { Tone string }

component TeamMark(props: TeamMarkProps) {
	return <span>{props.Tone}</span>
}

func ScoreTeam(props any) Node {
	return <div>é𐐀<TeamMark {...props}></TeamMark></div>
}
`)
	diags := Analyze("page.gsx", source)
	if len(diags) != 1 {
		t.Fatalf("got %d diagnostics, want 1: %#v", len(diags), diags)
	}
	if got := diags[0].Range.Start; got.Line != 9 || got.Character != 16 {
		t.Fatalf("diagnostic start = %d:%d, want UTF-16 9:16", got.Line, got.Character)
	}
	if got := diags[0].Range.End; got.Line != 9 || got.Character != 48 {
		t.Fatalf("diagnostic end = %d:%d, want UTF-16 9:48", got.Line, got.Character)
	}
}

func TestAnalyzeMalformedUnicodeParseErrorUsesUTF16Range(t *testing.T) {
	source := []byte("package main\n\nfunc Broken() Node {\n\treturn <div>é𐐀")
	diags := Analyze("broken.gsx", source)
	if len(diags) != 1 {
		t.Fatalf("got %d diagnostics, want 1: %#v", len(diags), diags)
	}
	if !strings.Contains(diags[0].Message, "unexpected syntax") {
		t.Fatalf("diagnostic message = %q, want parse error", diags[0].Message)
	}
	if got := diags[0].Range.Start; got.Line != 3 || got.Character != 14 {
		t.Fatalf("parse diagnostic start = %d:%d, want UTF-16 3:14", got.Line, got.Character)
	}
	if got := diags[0].Range.End; got.Line != 3 || got.Character != 16 {
		t.Fatalf("parse diagnostic end = %d:%d, want UTF-16 3:16", got.Line, got.Character)
	}
	if _, err := FormatSource(source); err == nil {
		t.Fatal("FormatSource succeeded, want parse error")
	}
}

func TestDocumentSymbolsUseUTF16SelectionRange(t *testing.T) {
	source := []byte("package main\r\n\r\nfunc /*é𐐀*/ Card() Node {\r\n\treturn <section>Card</section>\r\n}\r\n")
	symbols := DocumentSymbols("page.gsx", source)
	if len(symbols) != 1 {
		t.Fatalf("symbols = %#v, want one Card symbol", symbols)
	}
	start := symbols[0].SelectionRange.Start
	if start.Line != 2 || start.Character != 13 {
		t.Fatalf("Card selection start = %d:%d, want UTF-16 2:13", start.Line, start.Character)
	}
	if end := symbols[0].SelectionRange.End; end.Line != 2 || end.Character != 17 {
		t.Fatalf("Card selection end = %d:%d, want UTF-16 2:17", end.Line, end.Character)
	}
}

func TestHoverAndDefinitionAcceptUTF16RequestPositions(t *testing.T) {
	source := []byte(`package main

func /*é𐐀*/ Card() Node {
	return <section>Card</section>
}

func Page() Node {
	return <main>é𐐀<Card /></main>
}
`)
	hover := HoverAt("page.gsx", source, Position{Line: 7, Character: 18})
	if hover == nil {
		t.Fatal("HoverAt returned nil for UTF-16 position inside Card reference")
	}
	if !strings.Contains(hover.Contents.Value, "func Card() Node") {
		t.Fatalf("hover = %#v, want Card signature", hover)
	}
	loc := DefinitionAt("file:///tmp/page.gsx", "/tmp/page.gsx", source, Position{Line: 7, Character: 18})
	if loc == nil {
		t.Fatal("DefinitionAt returned nil for UTF-16 position inside Card reference")
	}
	if got := loc.Range.Start; got.Line != 2 || got.Character != 13 {
		t.Fatalf("definition start = %d:%d, want UTF-16 2:13", got.Line, got.Character)
	}
	if got := loc.Range.End; got.Line != 2 || got.Character != 17 {
		t.Fatalf("definition end = %d:%d, want UTF-16 2:17", got.Line, got.Character)
	}
	if got := hover.Range.Start; got.Line != 7 || got.Character != 18 {
		t.Fatalf("hover range start = %d:%d, want UTF-16 7:18", got.Line, got.Character)
	}
	if got := hover.Range.End; got.Line != 7 || got.Character != 22 {
		t.Fatalf("hover range end = %d:%d, want UTF-16 7:22", got.Line, got.Character)
	}
}

func TestServerWireUsesUTF16Positions(t *testing.T) {
	source := `package main

func /*é𐐀*/ Card() Node {
	return <section>Card</section>
}

func Page() Node {
	return <main>é𐐀<Card /></main>
}
`
	input := bytes.NewBuffer(nil)
	output := bytes.NewBuffer(nil)
	uri := "file:///tmp/page.gsx"

	writeFramedJSON(input, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	writeFramedJSON(input, fmt.Sprintf(`{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":%s,"version":1,"text":%s}}}`, quoteJSON(uri), quoteJSON(source)))
	writeFramedJSON(input, fmt.Sprintf(`{"jsonrpc":"2.0","id":2,"method":"textDocument/documentSymbol","params":{"textDocument":{"uri":%s}}}`, quoteJSON(uri)))
	writeFramedJSON(input, fmt.Sprintf(`{"jsonrpc":"2.0","id":3,"method":"textDocument/formatting","params":{"textDocument":{"uri":%s}}}`, quoteJSON(uri)))
	writeFramedJSON(input, fmt.Sprintf(`{"jsonrpc":"2.0","id":4,"method":"textDocument/hover","params":{"textDocument":{"uri":%s},"position":{"line":7,"character":18}}}`, quoteJSON(uri)))
	writeFramedJSON(input, fmt.Sprintf(`{"jsonrpc":"2.0","id":5,"method":"textDocument/definition","params":{"textDocument":{"uri":%s},"position":{"line":7,"character":18}}}`, quoteJSON(uri)))
	writeFramedJSON(input, `{"jsonrpc":"2.0","id":6,"method":"shutdown","params":{}}`)

	if err := Serve(input, output); err != nil {
		t.Fatalf("serve: %v", err)
	}
	messages := decodeWireMessages(t, output.String())
	symbolResp := responseByID(t, messages, 2)
	if got := intAt(t, symbolResp, "result", 0, "selectionRange", "start", "character"); got != 13 {
		t.Fatalf("wire symbol selection character = %d, want UTF-16 13", got)
	}
	if got := intAt(t, symbolResp, "result", 0, "selectionRange", "end", "character"); got != 17 {
		t.Fatalf("wire symbol selection end character = %d, want UTF-16 17", got)
	}
	formatResp := responseByID(t, messages, 3)
	if got := intAt(t, formatResp, "result", 0, "range", "end", "line"); got != 9 {
		t.Fatalf("format edit end line = %d, want document EOF line 9", got)
	}
	if got := intAt(t, formatResp, "result", 0, "range", "end", "character"); got != 0 {
		t.Fatalf("format edit end character = %d, want 0", got)
	}
	hoverResp := responseByID(t, messages, 4)
	if got := stringAt(t, hoverResp, "result", "contents", "value"); !strings.Contains(got, "func Card() Node") {
		t.Fatalf("wire hover contents = %q, want Card signature", got)
	}
	if got := intAt(t, hoverResp, "result", "range", "start", "line"); got != 7 {
		t.Fatalf("wire hover start line = %d, want 7", got)
	}
	if got := intAt(t, hoverResp, "result", "range", "start", "character"); got != 18 {
		t.Fatalf("wire hover start character = %d, want UTF-16 18", got)
	}
	if got := intAt(t, hoverResp, "result", "range", "end", "line"); got != 7 {
		t.Fatalf("wire hover end line = %d, want 7", got)
	}
	if got := intAt(t, hoverResp, "result", "range", "end", "character"); got != 22 {
		t.Fatalf("wire hover end character = %d, want UTF-16 22", got)
	}
	defResp := responseByID(t, messages, 5)
	if got := stringAt(t, defResp, "result", "uri"); got != uri {
		t.Fatalf("wire definition URI = %q, want %q", got, uri)
	}
	if got := intAt(t, defResp, "result", "range", "start", "line"); got != 2 {
		t.Fatalf("wire definition start line = %d, want 2", got)
	}
	if got := intAt(t, defResp, "result", "range", "start", "character"); got != 13 {
		t.Fatalf("wire definition start character = %d, want UTF-16 13", got)
	}
	if got := intAt(t, defResp, "result", "range", "end", "line"); got != 2 {
		t.Fatalf("wire definition end line = %d, want 2", got)
	}
	if got := intAt(t, defResp, "result", "range", "end", "character"); got != 17 {
		t.Fatalf("wire definition end character = %d, want UTF-16 17", got)
	}
}

func TestServerPublishesUnicodeDiagnosticURIWithUTF16Range(t *testing.T) {
	source := `package main

type TeamMarkProps struct { Tone string }

component TeamMark(props: TeamMarkProps) {
	return <span>{props.Tone}</span>
}

func ScoreTeam(props any) Node {
	return <div>é𐐀<TeamMark {...props}></TeamMark></div>
}
`
	input := bytes.NewBuffer(nil)
	output := bytes.NewBuffer(nil)
	uri := "file:///tmp/unicode-diagnostic.gsx"
	writeFramedJSON(input, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	writeFramedJSON(input, fmt.Sprintf(`{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":%s,"version":1,"text":%s}}}`, quoteJSON(uri), quoteJSON(source)))
	writeFramedJSON(input, `{"jsonrpc":"2.0","id":2,"method":"shutdown","params":{}}`)
	if err := Serve(input, output); err != nil {
		t.Fatalf("serve: %v", err)
	}
	diags := lastPublishedDiagnosticsByURI(t, output.String(), uri)
	if len(diags) != 1 {
		t.Fatalf("published diagnostics = %#v, want one", diags)
	}
	if got := diags[0].Range.Start; got.Line != 9 || got.Character != 16 {
		t.Fatalf("published diagnostic start = %d:%d, want UTF-16 9:16", got.Line, got.Character)
	}
	if got := diags[0].Range.End; got.Line != 9 || got.Character != 48 {
		t.Fatalf("published diagnostic end = %d:%d, want UTF-16 9:48", got.Line, got.Character)
	}
}

func TestProtocolPositionForByteOffsetFloorsInsideUTF8Rune(t *testing.T) {
	source := "a𐐀z"
	for _, offset := range []int{2, 3, 4} {
		got := protocolPositionForByteOffset(source, offset)
		if got.Line != 0 || got.Character != 1 {
			t.Fatalf("offset %d = %d:%d, want floor to UTF-16 0:1", offset, got.Line, got.Character)
		}
	}
	if got := protocolPositionForByteOffset(source, 5); got.Line != 0 || got.Character != 3 {
		t.Fatalf("offset after non-BMP = %d:%d, want UTF-16 0:3", got.Line, got.Character)
	}
	if got := byteOffsetForProtocolPosition(source, Position{Line: 0, Character: 2}); got != 1 {
		t.Fatalf("UTF-16 position inside non-BMP returned byte %d, want rune start byte 1", got)
	}
}

func TestProtocolPositionHandlesTabsCRLFAndLineClamps(t *testing.T) {
	source := "é\t𐐀\r\nz"
	if got := byteOffsetForProtocolPosition(source, Position{Line: 0, Character: 2}); got != 3 {
		t.Fatalf("UTF-16 position after BMP+tab returned byte %d, want 3", got)
	}
	if got := byteOffsetForProtocolPosition(source, Position{Line: 0, Character: 99}); got != 7 {
		t.Fatalf("line-end clamp returned byte %d, want byte before CR 7", got)
	}
	if got := protocolPositionForByteOffset(source, 8); got.Line != 0 || got.Character != 4 {
		t.Fatalf("offset on CR = %d:%d, want clamp to line-end UTF-16 0:4", got.Line, got.Character)
	}
	if got := protocolPositionForByteOffset(source, len(source)); got.Line != 1 || got.Character != 1 {
		t.Fatalf("EOF position = %d:%d, want UTF-16 1:1", got.Line, got.Character)
	}
}

func TestProjectDiagnosticsPublishToOwningURIAndClearWithoutDroppingFastDiagnostics(t *testing.T) {
	dir := t.TempDir()
	pagePath := filepath.Join(dir, "page.gsx")
	otherPath := filepath.Join(dir, "other.gsx")
	thirdPath := filepath.Join(dir, "third.gsx")
	pageURI := pathToURI(pagePath)
	otherURI := pathToURI(otherPath)
	thirdURI := pathToURI(thirdPath)
	pageSource := "package app\n\nfunc Page() Node {\n\treturn <main>ok</main>\n}\n"
	otherWithWarning := "package app\n\nfunc Other() Node {\n\treturn <div>é𐐀<span data-gosx-lonk></span></div>\n}\n"
	otherRepaired := strings.Replace(otherWithWarning, "data-gosx-lonk", "data-gosx-link", 1)
	otherUnsavedMalformed := "package app\n\nfunc Other() Node {\n\treturn <div>{</div>\n}\n"
	thirdSource := "package app\n\nfunc Third() Node {\n\treturn <aside>ok</aside>\n}\n"
	if err := os.WriteFile(pagePath, []byte(pageSource), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(otherPath, []byte(otherWithWarning), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(thirdPath, []byte(thirdSource), 0o644); err != nil {
		t.Fatal(err)
	}
	output := bytes.NewBuffer(nil)
	s := &server{
		docs:                  map[string]string{pageURI: pageSource, thirdURI: thirdSource},
		projectDiagnosticURIs: make(map[string]map[string]struct{}),
		writer:                output,
	}
	if err := s.publishProjectDiagnostics(pageURI); err != nil {
		t.Fatalf("publish project diagnostics: %v", err)
	}
	first := lastPublishedDiagnosticsByURI(t, output.String(), otherURI)
	if len(first) != 1 {
		t.Fatalf("first sibling diagnostics = %#v, want one project warning", first)
	}
	if !strings.Contains(first[0].Message, "data-gosx-link") {
		t.Fatalf("first sibling message = %q, want navigation warning", first[0].Message)
	}
	if got := first[0].Range.Start; got.Line != 3 || got.Character != 16 {
		t.Fatalf("first sibling range start = %d:%d, want UTF-16 3:16", got.Line, got.Character)
	}
	if got := first[0].Range.End; got.Line != 3 || got.Character != 44 {
		t.Fatalf("first sibling range end = %d:%d, want UTF-16 3:44", got.Line, got.Character)
	}

	if err := os.WriteFile(otherPath, []byte(otherRepaired), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.publishProjectDiagnostics(thirdURI); err != nil {
		t.Fatalf("third-file save after sibling repair: %v", err)
	}
	second := lastPublishedDiagnosticsByURI(t, output.String(), otherURI)
	if len(second) != 0 {
		t.Fatalf("sibling diagnostics after save of another file = %#v, want stale project diagnostics cleared", second)
	}

	if err := os.WriteFile(otherPath, []byte(otherWithWarning), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.publishProjectDiagnostics(pageURI); err != nil {
		t.Fatalf("republish project warning: %v", err)
	}
	reintroduced := lastPublishedDiagnosticsByURI(t, output.String(), otherURI)
	if len(reintroduced) != 1 || !strings.Contains(reintroduced[0].Message, "data-gosx-link") {
		t.Fatalf("reintroduced sibling diagnostics = %#v, want one project warning", reintroduced)
	}

	s.docs[otherURI] = otherUnsavedMalformed
	if err := s.publishProjectDiagnostics(pageURI); err != nil {
		t.Fatalf("republish while sibling has unsaved buffer: %v", err)
	}
	third := lastPublishedDiagnosticsByURI(t, output.String(), otherURI)
	if len(third) != 1 {
		t.Fatalf("third sibling diagnostics = %#v, want one fast parse diagnostic only", third)
	}
	if !strings.Contains(third[0].Message, "unexpected syntax") {
		t.Fatalf("third sibling message = %q, want fast parse diagnostic", third[0].Message)
	}
	if strings.Contains(third[0].Message, "data-gosx-link") {
		t.Fatalf("third sibling diagnostics retained saved-source project warning in unsaved buffer: %#v", third)
	}
}

func TestProjectDiagnosticsScopesDoNotClearUnrelatedPackages(t *testing.T) {
	dir := t.TempDir()
	pkgA := filepath.Join(dir, "a")
	pkgB := filepath.Join(dir, "b")
	pageA := filepath.Join(pkgA, "page.gsx")
	otherA := filepath.Join(pkgA, "other.gsx")
	thirdA := filepath.Join(pkgA, "third.gsx")
	pageB := filepath.Join(pkgB, "page.gsx")
	otherB := filepath.Join(pkgB, "other.gsx")
	pageSource := "package app\n\nfunc Page() Node {\n\treturn <main>ok</main>\n}\n"
	thirdSource := "package app\n\nfunc Third() Node {\n\treturn <aside>ok</aside>\n}\n"
	warningSource := "package app\n\nfunc Other() Node {\n\treturn <div><span data-gosx-lonk></span></div>\n}\n"
	repairedSource := strings.Replace(warningSource, "data-gosx-lonk", "data-gosx-link", 1)
	for path, source := range map[string]string{
		pageA:  pageSource,
		otherA: warningSource,
		thirdA: thirdSource,
		pageB:  pageSource,
		otherB: warningSource,
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	pageAURI := pathToURI(pageA)
	thirdAURI := pathToURI(thirdA)
	pageBURI := pathToURI(pageB)
	otherAURI := pathToURI(otherA)
	otherBURI := pathToURI(otherB)
	output := bytes.NewBuffer(nil)
	s := &server{
		docs: map[string]string{
			pageAURI:  pageSource,
			thirdAURI: thirdSource,
			pageBURI:  pageSource,
		},
		projectDiagnosticURIs: make(map[string]map[string]struct{}),
		writer:                output,
	}
	if err := s.publishProjectDiagnostics(pageAURI); err != nil {
		t.Fatalf("publish package A: %v", err)
	}
	if err := s.publishProjectDiagnostics(pageBURI); err != nil {
		t.Fatalf("publish package B: %v", err)
	}
	if got := lastPublishedDiagnosticsByURI(t, output.String(), otherAURI); len(got) != 1 {
		t.Fatalf("package A sibling diagnostics = %#v, want one warning", got)
	}
	if got := lastPublishedDiagnosticsByURI(t, output.String(), otherBURI); len(got) != 1 {
		t.Fatalf("package B sibling diagnostics = %#v, want one warning", got)
	}

	if err := os.WriteFile(otherA, []byte(repairedSource), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.publishProjectDiagnostics(thirdAURI); err != nil {
		t.Fatalf("republish package A from different sibling: %v", err)
	}
	if got := lastPublishedDiagnosticsByURI(t, output.String(), otherAURI); len(got) != 0 {
		t.Fatalf("package A sibling diagnostics = %#v, want cleared", got)
	}
	if got := lastPublishedDiagnosticsByURI(t, output.String(), otherBURI); len(got) != 1 || !strings.Contains(got[0].Message, "data-gosx-link") {
		t.Fatalf("package B sibling diagnostics = %#v, want unrelated warning preserved", got)
	}
	if publishes := publishedDiagnosticsForURI(t, output.String(), otherBURI); len(publishes) != 1 {
		t.Fatalf("package B publish count = %d, want no rewrite/clear from package A save", len(publishes))
	}
}

func TestPathToURIRoundTripsEscapedPOSIXPathAndRelativeProjectOwner(t *testing.T) {
	path := "/tmp/gosx space/é/page.gsx"
	uri := pathToURI(path)
	if uri != "file:///tmp/gosx%20space/%C3%A9/page.gsx" {
		t.Fatalf("pathToURI(%q) = %q", path, uri)
	}
	if got := URIToPath(uri); got != path {
		t.Fatalf("URIToPath(%q) = %q, want %q", uri, got, path)
	}

	dir := t.TempDir()
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(originalWD); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	})
	pageSource := "package app\n\nfunc Page() Node {\n\treturn <main>ok</main>\n}\n"
	otherSource := "package app\n\nfunc Other() Node {\n\treturn <div>é𐐀<span data-gosx-lonk></span></div>\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "page.gsx"), []byte(pageSource), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "other.gsx"), []byte(otherSource), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	owned := analyzeProjectOwned("page.gsx")
	if len(owned) != 1 {
		t.Fatalf("owned diagnostics = %#v, want one owner", owned)
	}
	wantURI := pathToURI(filepath.Join(dir, "other.gsx"))
	if owned[0].URI != wantURI {
		t.Fatalf("owner URI = %q, want %q", owned[0].URI, wantURI)
	}
	if len(owned[0].Diagnostics) != 1 {
		t.Fatalf("owner diagnostics = %#v, want one warning", owned[0].Diagnostics)
	}
	if got := owned[0].Diagnostics[0].Range.Start; got.Line != 3 || got.Character != 16 {
		t.Fatalf("relative owner range start = %d:%d, want UTF-16 3:16", got.Line, got.Character)
	}
}

func decodeWireMessages(t *testing.T, wire string) []map[string]any {
	t.Helper()
	reader := bufio.NewReader(strings.NewReader(wire))
	var messages []map[string]any
	for {
		payload, err := readMessage(reader)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read framed message: %v", err)
		}
		var msg map[string]any
		if err := json.Unmarshal(payload, &msg); err != nil {
			t.Fatalf("decode %q: %v", payload, err)
		}
		messages = append(messages, msg)
	}
	return messages
}

func responseByID(t *testing.T, messages []map[string]any, id int) map[string]any {
	t.Helper()
	for _, msg := range messages {
		if got, ok := msg["id"].(float64); ok && int(got) == id {
			return msg
		}
	}
	t.Fatalf("missing response id %d in %#v", id, messages)
	return nil
}

func lastPublishedDiagnosticsByURI(t *testing.T, wire, uri string) []Diagnostic {
	t.Helper()
	messages := decodeWireMessages(t, wire)
	var found []Diagnostic
	for _, msg := range messages {
		if msg["method"] != "textDocument/publishDiagnostics" {
			continue
		}
		params, ok := msg["params"].(map[string]any)
		if !ok || params["uri"] != uri {
			continue
		}
		data, err := json.Marshal(params["diagnostics"])
		if err != nil {
			t.Fatal(err)
		}
		var diags []Diagnostic
		if err := json.Unmarshal(data, &diags); err != nil {
			t.Fatal(err)
		}
		found = diags
	}
	if found == nil {
		t.Fatalf("missing publishDiagnostics for %s in %#v", uri, messages)
	}
	return found
}

func publishedDiagnosticsForURI(t *testing.T, wire, uri string) [][]Diagnostic {
	t.Helper()
	messages := decodeWireMessages(t, wire)
	var out [][]Diagnostic
	for _, msg := range messages {
		if msg["method"] != "textDocument/publishDiagnostics" {
			continue
		}
		params, ok := msg["params"].(map[string]any)
		if !ok || params["uri"] != uri {
			continue
		}
		data, err := json.Marshal(params["diagnostics"])
		if err != nil {
			t.Fatal(err)
		}
		var diags []Diagnostic
		if err := json.Unmarshal(data, &diags); err != nil {
			t.Fatal(err)
		}
		out = append(out, diags)
	}
	return out
}

func intAt(t *testing.T, value any, path ...any) int {
	t.Helper()
	current := value
	for _, step := range path {
		switch key := step.(type) {
		case string:
			obj, ok := current.(map[string]any)
			if !ok {
				t.Fatalf("path %v reached non-object %#v", path, current)
			}
			current = obj[key]
		case int:
			arr, ok := current.([]any)
			if !ok || key >= len(arr) {
				t.Fatalf("path %v reached non-array/out-of-range %#v", path, current)
			}
			current = arr[key]
		default:
			t.Fatalf("unsupported path step %T", step)
		}
	}
	num, ok := current.(float64)
	if !ok {
		t.Fatalf("path %v reached non-number %#v", path, current)
	}
	return int(num)
}

func stringAt(t *testing.T, value any, path ...any) string {
	t.Helper()
	current := value
	for _, step := range path {
		switch key := step.(type) {
		case string:
			obj, ok := current.(map[string]any)
			if !ok {
				t.Fatalf("path %v reached non-object %#v", path, current)
			}
			current = obj[key]
		case int:
			arr, ok := current.([]any)
			if !ok || key >= len(arr) {
				t.Fatalf("path %v reached non-array/out-of-range %#v", path, current)
			}
			current = arr[key]
		default:
			t.Fatalf("unsupported path step %T", step)
		}
	}
	text, ok := current.(string)
	if !ok {
		t.Fatalf("path %v reached non-string %#v", path, current)
	}
	return text
}
