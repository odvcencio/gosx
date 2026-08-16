package ir

import "testing"

// TestDiagnosticStringCodeFieldIsAdditive proves the new Code field
// (gosx#186) only changes Diagnostic.String() output when a caller sets it.
// Every built-in gosx diagnostic leaves Code empty, so this pins the
// pre-existing "line:col: message (hint)" format for that case and adds a
// "line:col: CODE: message (hint)" case for a rule-coded diagnostic such as
// a third-party strictcheck.Lint would produce.
func TestDiagnosticStringCodeFieldIsAdditive(t *testing.T) {
	span := Span{StartLine: 5, StartCol: 1}

	empty := Diagnostic{Span: span, Message: "element has empty tag name"}
	if got, want := empty.String(), "5:1: element has empty tag name"; got != want {
		t.Fatalf("String() with empty Code = %q, want %q", got, want)
	}

	withHint := Diagnostic{Span: span, Message: "unsupported member \".length\"", Hint: "use len(...)"}
	if got, want := withHint.String(), `5:1: unsupported member ".length" (use len(...))`; got != want {
		t.Fatalf("String() with empty Code and hint = %q, want %q", got, want)
	}

	coded := Diagnostic{Span: span, Code: "EM001", Message: "element <script> is not allowed"}
	if got, want := coded.String(), "5:1: EM001: element <script> is not allowed"; got != want {
		t.Fatalf("String() with Code = %q, want %q", got, want)
	}

	codedWithHint := Diagnostic{Span: span, Code: "EM110", Message: "href scheme not allowed", Hint: "use https"}
	if got, want := codedWithHint.String(), "5:1: EM110: href scheme not allowed (use https)"; got != want {
		t.Fatalf("String() with Code and hint = %q, want %q", got, want)
	}
}

// TestDiagnosticStringIncludesFilePrefixWhenSpanFileIsSet pins gosx#186 B2:
// a diagnostic whose Span.File is set (as every strictcheck.Lint finding's
// is, by strictcheck itself if the lint leaves it empty) prints that file
// as a "path:" prefix before "line:col:". A diagnostic that leaves Span.File
// empty -- every built-in gosx and strictcheck diagnostic today -- is
// unaffected; see TestDiagnosticStringCodeFieldIsAdditive above.
func TestDiagnosticStringIncludesFilePrefixWhenSpanFileIsSet(t *testing.T) {
	noFile := Diagnostic{Span: Span{StartLine: 5, StartCol: 1}, Message: "element has empty tag name"}
	if got, want := noFile.String(), "5:1: element has empty tag name"; got != want {
		t.Fatalf("String() with empty Span.File = %q, want %q", got, want)
	}

	// The consumer contract this extension point exists for (a gsxmail-style
	// per-file email lint catalog; see strictcheck.Lint and gsx-email-spec.md
	// section 8): a coded finding attributed to its source file.
	consumer := Diagnostic{
		Span:    Span{File: "emails/invite.gsx", StartLine: 14, StartCol: 9},
		Code:    "EM0xx",
		Message: "message per gsx-email-spec §8",
	}
	if got, want := consumer.String(), "emails/invite.gsx:14:9: EM0xx: message per gsx-email-spec §8"; got != want {
		t.Fatalf("String() with Span.File set = %q, want %q", got, want)
	}
}
