package lsp

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const savedModelSource = `package app
component Card(props: Props) {
 return <article>{props.Title}{props.Meta.Label}<Each of={props.Rows} as="row"><span>{row.Label}</span></Each></article>
}
component Page(props: Props) {
 return <Card {...props} />
}
`

const savedModelGo = "package app\ntype Meta struct { Label string }; type Row struct { Label string }; type Props struct { Title string; Meta Meta; Rows []Row }\n"

func TestServerSavedSchemaLifecycle(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.test/editor\n\ngo 1.26.0\n")
	write("model.go", savedModelGo)
	write("page.gsx", savedModelSource)
	uri := "file://" + filepath.ToSlash(filepath.Join(dir, "page.gsx"))
	goURI := "file://" + filepath.ToSlash(filepath.Join(dir, "model.go"))
	var output bytes.Buffer
	s := &server{docs: map[string]string{}, projectDiagnosticURIs: map[string]map[string]struct{}{}, writer: &output}
	send := func(method string, params any) {
		t.Helper()
		data, err := json.Marshal(params)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.handle(request{Method: method, Params: data}); err != nil {
			t.Fatal(err)
		}
	}
	open := func() {
		send("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "text": savedModelSource}})
	}
	change := func(source string) {
		send("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri}, "contentChanges": []map[string]string{{"text": source}}})
	}
	save := func(u string) { send("textDocument/didSave", map[string]any{"textDocument": map[string]any{"uri": u}}) }
	clean := func() {
		t.Helper()
		if d := s.fastDiagnostics(uri); len(d) != 0 {
			t.Fatalf("diagnostics: %+v", d)
		}
	}
	open()
	clean()
	idx, d := s.savedSourceIndex(uri, []byte(savedModelSource))
	if len(d) != 0 || len(documentSymbolsFromIndex(idx)) != 2 {
		t.Fatalf("index: %+v %+v", idx, d)
	}
	pos := testPositionIn(savedModelSource, "<Card", 2)
	if hoverFromIndex(idx, pos) == nil || definitionFromIndex(uri, idx, pos) == nil {
		t.Fatal("schema-aware navigation missing")
	}
	for _, method := range []string{"textDocument/documentSymbol", "textDocument/hover", "textDocument/definition"} {
		output.Reset()
		params, _ := json.Marshal(map[string]any{"textDocument": map[string]string{"uri": uri}, "position": pos})
		if err := s.handle(request{ID: json.RawMessage("1"), Method: method, Params: params}); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(output.String(), `"result":`) || strings.Contains(output.String(), `"result":null`) {
			t.Fatalf("%s: %s", method, output.String())
		}
	}
	if len(Analyze(filepath.Join(dir, "page.gsx"), []byte(savedModelSource))) == 0 || len(DocumentSymbols("page.gsx", []byte(savedModelSource))) != 0 || HoverAt("page.gsx", []byte(savedModelSource), pos) != nil || DefinitionAt(uri, "page.gsx", []byte(savedModelSource), pos) != nil {
		t.Fatal("public byte APIs unexpectedly resolved saved models")
	}

	// Disk becomes invalid: a compatible keystroke must still use the saved snapshot.
	write("model.go", "package app\ntype Props struct { Title chan int }\n")
	change(strings.Replace(savedModelSource, "<article>", "<article class=\"edited\">", 1))
	clean()
	change(strings.ReplaceAll(savedModelSource, "Props", "OtherProps"))
	if len(s.fastDiagnostics(uri)) == 0 {
		t.Fatal("changed roots reused schema")
	}
	change(strings.Replace(savedModelSource, "package app", "package other", 1))
	if len(s.fastDiagnostics(uri)) == 0 {
		t.Fatal("changed package reused schema")
	}
	change("package app\ncomponent Card(props: Props) {\n return <")
	if len(s.fastDiagnostics(uri)) == 0 {
		t.Fatal("invalid refs reused schema")
	}
	change(strings.Replace(savedModelSource, "component Card", "type Props struct { Title string }\ncomponent Card", 1))
	if len(s.fastDiagnostics(uri)) == 0 {
		t.Fatal("changed declaration boundary reused schema")
	}
	change(savedModelSource)
	clean()
	// A save in another directory must not refresh this now-invalid disk model.
	other := t.TempDir()
	if err := os.WriteFile(filepath.Join(other, "model.go"), []byte("package app"), 0600); err != nil {
		t.Fatal(err)
	}
	save("file://" + filepath.ToSlash(filepath.Join(other, "model.go")))
	clean()
	write("unrelated.go", "package unrelated\n")
	save("file://" + filepath.ToSlash(filepath.Join(dir, "unrelated.go")))
	clean()
	if err := os.Remove(filepath.Join(dir, "unrelated.go")); err != nil {
		t.Fatal(err)
	}
	// Neither Go opens nor Go changes enter the GSX parser/document registry.
	send("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": goURI, "text": "not GSX"}})
	send("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": goURI}, "contentChanges": []map[string]string{{"text": "also not GSX"}}})
	if _, ok := s.docs[goURI]; ok {
		t.Fatal("Go buffer stored as GSX")
	}
	save(goURI)
	if state := s.schemas[uri]; state.schemas != nil || state.err == nil {
		t.Fatalf("failure retained schema: %+v", state)
	}
	if d := s.fastDiagnostics(uri); !strings.Contains(fmtDiagnostics(d), "saved Go model schema") {
		t.Fatalf("missing resolver diagnostic: %+v", d)
	}
	write("model.go", savedModelGo)
	save(goURI)
	clean()
	write("model.go", "package app\ntype Props struct { Title chan int }\n")
	save(uri)
	if s.schemas[uri].err == nil {
		t.Fatal("GSX save did not refresh")
	}
	write("model.go", savedModelGo)
	save(uri)
	clean()
	send("textDocument/didClose", map[string]any{"textDocument": map[string]any{"uri": uri}})
	if len(s.schemas) != 0 {
		t.Fatal("close retained schema")
	}
	save(goURI)
	if len(s.schemas) != 0 || len(s.docs) != 0 {
		t.Fatal("Go save reopened closed document")
	}
}

func fmtDiagnostics(diags []Diagnostic) string {
	var b strings.Builder
	for _, d := range diags {
		b.WriteString(d.Message)
	}
	return b.String()
}
