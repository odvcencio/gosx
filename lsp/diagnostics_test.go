package lsp

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"m31labs.dev/gosx"
)

func TestAnalyzeParseDiagnostic(t *testing.T) {
	diags := Analyze("broken.gsx", []byte(`package main

func Broken() Node {
	return <div>{</div>
}
`))
	if len(diags) == 0 {
		t.Fatal("expected diagnostics")
	}
	if diags[0].Severity != SeverityError {
		t.Fatalf("expected error severity, got %d", diags[0].Severity)
	}
	if diags[0].Range.Start.Line < 0 || diags[0].Range.Start.Character < 0 {
		t.Fatal("expected non-negative parse position")
	}
	if !strings.Contains(diags[0].Message, "unexpected syntax") {
		t.Fatalf("unexpected parse message %q", diags[0].Message)
	}
}

func TestAnalyzeValidationDiagnostic(t *testing.T) {
	diags := Analyze("broken.gsx", []byte(`package main

func broken() Node {
	return <div>Hello</div>
}
`))
	if len(diags) == 0 {
		t.Fatal("expected validation diagnostics")
	}
	if !strings.Contains(diags[0].Message, "uppercase") {
		t.Fatalf("unexpected validation message %q", diags[0].Message)
	}
}

func TestAnalyzeReturnsAllValidationDiagnostics(t *testing.T) {
	diags := Analyze("broken.gsx", []byte(`package main

func broken() Node {
	return <div>Hello</div>
}

func alsoBroken() Node {
	return <span>World</span>
}
`))
	if len(diags) != 2 {
		t.Fatalf("expected 2 diagnostics, got %d", len(diags))
	}
	for _, diag := range diags {
		if !strings.Contains(diag.Message, "uppercase") {
			t.Fatalf("unexpected validation message %q", diag.Message)
		}
	}
}

// TestAnalyzeWarnsOnUntypedLegacyComponent covers step one of retiring
// legacy component syntax: an untyped legacy component (`func Name(props
// any) Node`) must reach the editor as a SeverityWarning diagnostic, not a
// SeverityError one — the file itself is otherwise valid, so Analyze must
// not report it as broken.
func TestAnalyzeWarnsOnUntypedLegacyComponent(t *testing.T) {
	diags := Analyze("page.gsx", []byte(`package main

func FeatureCard(props any) Node {
	return <div class="card">{props}</div>
}
`))
	if len(diags) != 1 {
		t.Fatalf("expected exactly one diagnostic, got %d: %+v", len(diags), diags)
	}
	diag := diags[0]
	if diag.Severity != SeverityWarning {
		t.Fatalf("expected warning severity, got %d", diag.Severity)
	}
	if !strings.Contains(diag.Message, "FeatureCard") {
		t.Fatalf("expected the diagnostic to name FeatureCard, got %q", diag.Message)
	}
	if !strings.Contains(diag.Message, "component FeatureCard(props: FeatureCardProps)") {
		t.Fatalf("expected the diagnostic to point at the strict replacement, got %q", diag.Message)
	}
}

func TestFormatSource(t *testing.T) {
	formatted, err := FormatSource([]byte(`package main

func Page() Node {
	return <div><span>Hi</span></div>
}
`))
	if err != nil {
		t.Fatalf("format source: %v", err)
	}
	if !strings.Contains(string(formatted), "<span>Hi</span>") {
		t.Fatalf("unexpected formatted output %q", string(formatted))
	}
}

func TestFormatSourceRejectsMalformedGSX(t *testing.T) {
	_, err := FormatSource([]byte(`package main

func Broken() Node {
	return <div>{</div>
}
`))
	if err == nil {
		t.Fatal("FormatSource succeeded on malformed GSX")
	}
	var parseErr *gosx.ParseError
	if !errors.As(err, &parseErr) {
		t.Fatalf("FormatSource error = %T %[1]v, want *gosx.ParseError", err)
	}
	if parseErr.Line != 4 || parseErr.Column != 20 {
		t.Fatalf("parse location = %d:%d, want 4:20 (%v)", parseErr.Line, parseErr.Column, err)
	}
	if !strings.Contains(parseErr.Message, "unexpected syntax") {
		t.Fatalf("parse message = %q, want unexpected syntax", parseErr.Message)
	}
}

func TestServerFormattingReturnsErrorForMalformedGSX(t *testing.T) {
	input := bytes.NewBuffer(nil)
	output := bytes.NewBuffer(nil)

	writeFramedJSON(input, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	writeFramedJSON(input, `{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":"file:///tmp/broken.gsx","version":1,"text":"package main\n\nfunc Broken() Node {\n\treturn <div>{</div>\n}\n"}}}`)
	writeFramedJSON(input, `{"jsonrpc":"2.0","id":2,"method":"textDocument/formatting","params":{"textDocument":{"uri":"file:///tmp/broken.gsx"}}}`)
	writeFramedJSON(input, `{"jsonrpc":"2.0","id":3,"method":"shutdown","params":{}}`)

	if err := Serve(input, output); err != nil {
		t.Fatalf("serve: %v", err)
	}

	wire := output.String()
	if !strings.Contains(wire, `"method":"textDocument/publishDiagnostics"`) {
		t.Fatalf("expected publishDiagnostics notification, got %s", wire)
	}
	if !strings.Contains(wire, `"id":2`) || !strings.Contains(wire, `"error"`) {
		t.Fatalf("expected formatting error response for id 2, got %s", wire)
	}
	if strings.Contains(wire, `"newText"`) {
		t.Fatalf("malformed formatting returned a text edit, got %s", wire)
	}
	if !strings.Contains(wire, "unexpected syntax") {
		t.Fatalf("expected parse diagnostic in wire output, got %s", wire)
	}
}

func TestFormatSourcePreservesHyphenatedAttributes(t *testing.T) {
	formatted, err := FormatSource([]byte(`package main

func Page() Node {
	return <a data-gosx-link aria-label="Docs">Hi</a>
}
`))
	if err != nil {
		t.Fatalf("format source: %v", err)
	}
	if !strings.Contains(string(formatted), "data-gosx-link") {
		t.Fatalf("expected hyphenated bool attr in formatted output %q", string(formatted))
	}
	if !strings.Contains(string(formatted), `aria-label="Docs"`) {
		t.Fatalf("expected hyphenated static attr in formatted output %q", string(formatted))
	}
}

func TestServerPublishesDiagnosticsAndFormatting(t *testing.T) {
	input := bytes.NewBuffer(nil)
	output := bytes.NewBuffer(nil)

	writeFramedJSON(input, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	writeFramedJSON(input, `{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":"file:///tmp/test.gsx","version":1,"text":"package main\n\nfunc broken() Node {\n\treturn <div>Hello</div>\n}\n"}}}`)
	writeFramedJSON(input, `{"jsonrpc":"2.0","id":2,"method":"textDocument/formatting","params":{"textDocument":{"uri":"file:///tmp/test.gsx"}}}`)
	writeFramedJSON(input, `{"jsonrpc":"2.0","id":3,"method":"shutdown","params":{}}`)

	if err := Serve(input, output); err != nil {
		t.Fatalf("serve: %v", err)
	}

	wire := output.String()
	if !strings.Contains(wire, `"method":"textDocument/publishDiagnostics"`) {
		t.Fatalf("expected publishDiagnostics notification, got %s", wire)
	}
	if !strings.Contains(wire, `"message":"component \"broken\" must start with an uppercase letter"`) {
		t.Fatalf("expected validation diagnostic in wire output, got %s", wire)
	}
	if !strings.Contains(wire, `"newText":"package main`) {
		t.Fatalf("expected formatting response, got %s", wire)
	}
}

func writeFramedJSON(buf *bytes.Buffer, payload string) {
	fmt.Fprintf(buf, "Content-Length: %d\r\n\r\n%s", len(payload), payload)
}
