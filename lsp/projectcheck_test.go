package lsp

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAnalyzeProjectAcceptsExplicitManagedActionURL proves project analysis
// does not infer a deleted page-local action registry. The global managed
// endpoint is an ordinary explicit form target; registration belongs to the
// owning route.Router.
func TestAnalyzeProjectAcceptsExplicitManagedActionURL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "page.gsx")
	source := "package main\n\nfunc Page() Node {\n\treturn <form method=\"post\" action=\"/gosx/action/save\"></form>\n}\n"
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatalf("write page.gsx: %v", err)
	}

	diags := AnalyzeProject(path)
	if len(diags) != 0 {
		t.Fatalf("explicit managed action URL should not produce a registry diagnostic, got %+v", diags)
	}
}

// TestServerPublishesProjectDiagnosticsOnSave proves the didSave wiring stays
// quiet for an explicit managed endpoint while initialize still advertises
// save support.
func TestServerPublishesProjectDiagnosticsOnSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "page.gsx")
	source := "package main\n\nfunc Page() Node {\n\treturn <form method=\"post\" action=\"/gosx/action/save\"></form>\n}\n"
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatalf("write page.gsx: %v", err)
	}
	uri := "file://" + filepath.ToSlash(path)

	input := bytes.NewBuffer(nil)
	output := bytes.NewBuffer(nil)

	writeFramedJSON(input, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	writeFramedJSON(input, fmt.Sprintf(
		`{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":%q,"version":1,"text":%q}}}`,
		uri, source))
	writeFramedJSON(input, fmt.Sprintf(
		`{"jsonrpc":"2.0","method":"textDocument/didSave","params":{"textDocument":{"uri":%q}}}`, uri))
	writeFramedJSON(input, `{"jsonrpc":"2.0","id":3,"method":"shutdown","params":{}}`)

	if err := Serve(input, output); err != nil {
		t.Fatalf("serve: %v", err)
	}

	wire := output.String()
	if !strings.Contains(wire, `"save":{"includeText":false}`) {
		t.Fatalf("expected initialize to advertise save support, got %s", wire)
	}
	if strings.Contains(wire, "action"+"Path") {
		t.Fatalf("deleted page-local action helper appeared in wire output: %s", wire)
	}
}
