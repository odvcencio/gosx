package lsp

import (
	"strings"
	"testing"
)

// TestAnalyzeReportsLegacyPropsSpreadIntoStrict closes gosx#229's third
// surface. The rule lives in the IR validation pass and this package sources
// its payloads from ir.DiagnosticsError, so the editor reports the spread as
// the author types it, with no LSP-specific work. Before the rule existed
// the editor was silent on a statically provable failure.
func TestAnalyzeReportsLegacyPropsSpreadIntoStrict(t *testing.T) {
	diags := Analyze("page.gsx", []byte(`package app

type TeamMarkProps struct {
	Tone string
}

component TeamMark(props: TeamMarkProps) {
	return <span>{props.Tone}</span>
}

func ScoreTeam(props any) Node {
	return <div><TeamMark {...props}></TeamMark></div>
}
`))
	if len(diags) != 1 {
		t.Fatalf("got %d diagnostics, want 1: %#v", len(diags), diags)
	}
	diag := diags[0]
	if want := "legacy component ScoreTeam cannot spread props into strict component TeamMark"; !strings.Contains(diag.Message, want) {
		t.Fatalf("message = %q, want to contain %q", diag.Message, want)
	}
	if diag.Severity != SeverityError {
		t.Fatalf("severity = %d, want %d", diag.Severity, SeverityError)
	}
	// Zero-based LSP coordinates for the <TeamMark ...> call on line 12,
	// column 14 of the source above.
	if diag.Range.Start.Line != 11 || diag.Range.Start.Character != 13 {
		t.Fatalf("range start = %d:%d, want 11:13", diag.Range.Start.Line, diag.Range.Start.Character)
	}
}
