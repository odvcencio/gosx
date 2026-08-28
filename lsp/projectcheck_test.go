package lsp

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAnalyzeProjectFlagsUnregisteredFormAction proves AnalyzeProject
// reaches strictcheck's whole-project form-action check (gosx#249): a
// page reading the file back off disk, not the in-memory buffer, still
// finds the same unregistered actionPath(...) reference gosx check would.
func TestAnalyzeProjectFlagsUnregisteredFormAction(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "page.gsx")
	source := "package main\n\nfunc Page() Node {\n\treturn <form method=\"post\" action={actionPath(\"missing\")}></form>\n}\n"
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatalf("write page.gsx: %v", err)
	}

	diags := AnalyzeProject(path)
	if len(diags) == 0 {
		t.Fatal("expected at least one diagnostic")
	}
	found := false
	for _, diag := range diags {
		if diag.Severity == SeverityError && strings.Contains(diag.Message, `actionPath("missing")`) {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an unregistered-action error, got %+v", diags)
	}
}

// TestServerPublishesProjectDiagnosticsOnSave proves the didSave wiring
// end to end: initialize advertises save support, and a save notification
// for a page with an unregistered form action republishes a diagnostic a
// keystroke-only didChange never would (it never touches disk).
func TestServerPublishesProjectDiagnosticsOnSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "page.gsx")
	source := "package main\n\nfunc Page() Node {\n\treturn <form method=\"post\" action={actionPath(\"missing\")}></form>\n}\n"
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
	if !strings.Contains(wire, `actionPath(\"missing\")`) {
		t.Fatalf("expected the save-triggered diagnostic in wire output, got %s", wire)
	}
}
