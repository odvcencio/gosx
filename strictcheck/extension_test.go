package strictcheck

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx/ir"
)

// This file exercises gosx#186: the strictcheck.Lint extension point. It
// deliberately does not touch check_test.go so it merges cleanly alongside
// the strictcheck test suite landing on #183.

// --- Two example lints, standing in for a consumer catalog (gsxmail's EM
// rules; see gsx-email-spec.md section 8) -----------------------------------

// exampleNoScriptLint rejects <script> elements, mirroring gsxmail's EM001:
// mail clients do not run JavaScript. It proves a per-file lint can reject
// an element kind that vanilla gosx allows.
func exampleNoScriptLint() Lint {
	return Lint{
		Name: "gsxmail-no-script",
		Check: func(file LintFile, report func(ir.Diagnostic)) {
			for i := range file.Program.Nodes {
				node := &file.Program.Nodes[i]
				if node.Kind == ir.NodeElement && node.Tag == "script" {
					report(ir.Diagnostic{
						Span:    node.Span,
						Code:    "EM001",
						Message: "element <script> is not allowed in an email template; mail clients do not run JavaScript",
					})
				}
			}
		},
	}
}

// exampleHrefSchemeLint flags an attribute value by pattern, mirroring
// gsxmail's EM110: only https/http/mailto href schemes are allowed. It
// proves a per-file lint can inspect a specific attribute's static value.
func exampleHrefSchemeLint() Lint {
	return Lint{
		Name: "gsxmail-href-scheme",
		Check: func(file LintFile, report func(ir.Diagnostic)) {
			for i := range file.Program.Nodes {
				node := &file.Program.Nodes[i]
				if node.Kind != ir.NodeElement || node.Tag != "a" {
					continue
				}
				for _, attr := range node.Attrs {
					if attr.Kind != ir.AttrStatic || attr.Name != "href" {
						continue
					}
					if strings.HasPrefix(attr.Value, "javascript:") {
						report(ir.Diagnostic{
							Span:    node.Span,
							Code:    "EM110",
							Message: fmt.Sprintf("href scheme %q is not allowed; use https, http, or mailto", "javascript:"),
						})
					}
				}
			}
		},
	}
}

// TestExtraLintsExampleLintsCatchScriptAndJavascriptHrefEndToEnd runs both
// example lints through the real check path (CheckFileWithOptions) over a
// real .gsx fixture, on a legacy-syntax component (gsxmail templates are
// legacy-spelled; see gsx-email-spec.md section 6.1). It also pins exact
// source position: the file has no leading indentation on the offending
// lines, so StartLine/StartCol are unambiguous.
func TestExtraLintsExampleLintsCatchScriptAndJavascriptHrefEndToEnd(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "invite.gsx")
	mustWrite(t, path, `package emails

func Invite() Node {
	return <div>
<script>bad()</script>
<a href="javascript:evil()">click</a>
	</div>
}
`)
	err := CheckFileWithOptions(context.Background(), path, Options{
		ExtraLints: []Lint{exampleNoScriptLint(), exampleHrefSchemeLint()},
	})
	if err == nil {
		t.Fatal("expected diagnostics from the extra lints")
	}
	message := err.Error()
	for _, want := range []string{
		`5:1: EM001: element <script> is not allowed in an email template`,
		`6:1: EM110: href scheme "javascript:" is not allowed; use https, http, or mailto`,
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("error = %q; want substring %q", message, want)
		}
	}
}

// TestExtraLintsCodeFieldSurfacesDistinctlyFromEmptyCode proves that a
// rule-coded diagnostic and an uncoded diagnostic (the shape every built-in
// strictcheck/ir diagnostic uses today) both flow through the same channel
// without the code leaking across findings.
func TestExtraLintsCodeFieldSurfacesDistinctlyFromEmptyCode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "page.gsx")
	mustWrite(t, path, `package app

func Page() Node {
	return <div id="mark">x</div>
}
`)
	coded := Lint{Name: "coded", Check: func(file LintFile, report func(ir.Diagnostic)) {
		report(ir.Diagnostic{Span: file.Program.Nodes[0].Span, Code: "EM777", Message: "coded finding"})
	}}
	uncoded := Lint{Name: "uncoded", Check: func(file LintFile, report func(ir.Diagnostic)) {
		report(ir.Diagnostic{Span: file.Program.Nodes[0].Span, Message: "uncoded finding"})
	}}

	err := CheckFileWithOptions(context.Background(), path, Options{ExtraLints: []Lint{coded, uncoded}})
	if err == nil {
		t.Fatal("expected diagnostics")
	}
	message := err.Error()
	if !strings.Contains(message, "EM777: coded finding") {
		t.Fatalf("coded diagnostic missing its code prefix: %q", message)
	}
	for _, line := range strings.Split(message, "\n") {
		if strings.Contains(line, "uncoded finding") && strings.Contains(line, "EM777") {
			t.Fatalf("code leaked from one diagnostic onto another: %q", line)
		}
	}
}

// TestExtraLintsPanicIsContainedAndCheckContinues proves gosx#186's fail-
// closed-but-contained requirement: a panicking lint neither crashes the
// check run nor stops it from covering the remaining files, and it does not
// stop a second, well-behaved lint from running on every file either. The
// fixture package has two files so the assertions can tell "contained once"
// apart from "contained forever after the first panic".
func TestExtraLintsPanicIsContainedAndCheckContinues(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.gsx"), `package app

func A() Node {
	return <div id="mark">a</div>
}
`)
	mustWrite(t, filepath.Join(dir, "b.gsx"), `package app

func B() Node {
	return <div id="mark">b</div>
}
`)

	panicky := Lint{
		Name: "panicky",
		Check: func(file LintFile, report func(ir.Diagnostic)) {
			panic("boom in " + filepath.Base(file.Path))
		},
	}
	flagsMark := Lint{
		Name: "flags-mark",
		Check: func(file LintFile, report func(ir.Diagnostic)) {
			for i := range file.Program.Nodes {
				node := &file.Program.Nodes[i]
				if node.Kind != ir.NodeElement {
					continue
				}
				for _, attr := range node.Attrs {
					if attr.Kind == ir.AttrStatic && attr.Name == "id" && attr.Value == "mark" {
						report(ir.Diagnostic{Span: node.Span, Code: "MARK", Message: "found mark in " + filepath.Base(file.Path)})
					}
				}
			}
		},
	}

	err := CheckTreeWithOptions(context.Background(), dir, Options{ExtraLints: []Lint{panicky, flagsMark}})
	if err == nil {
		t.Fatal("expected diagnostics (the panics and the mark findings)")
	}
	message := err.Error()

	if got := strings.Count(message, `lint "panicky" panicked`); got != 2 {
		t.Fatalf("panic diagnostic count = %d, want 2 (one per file); message:\n%s", got, message)
	}
	for _, want := range []string{"boom in a.gsx", "boom in b.gsx", "MARK: found mark in a.gsx", "MARK: found mark in b.gsx"} {
		if !strings.Contains(message, want) {
			t.Fatalf("error missing %q; message:\n%s", want, message)
		}
	}
}

// TestExtraLintsNilAndAbsentOptionsAreByteIdenticalToNoExtension is the
// gosx#186 compatibility golden: registering no extra lints (the field
// absent, explicitly nil, or an empty non-nil slice) must not change a
// single byte of strictcheck's returned error compared to CheckFile, on
// both a passing fixture and a failing one, so a build with the extension
// point compiled in behaves identically to today's strictcheck for every
// existing caller.
func TestExtraLintsNilAndAbsentOptionsAreByteIdenticalToNoExtension(t *testing.T) {
	variants := func() map[string]Options {
		return map[string]Options{
			"field absent": {},
			"nil slice":    {ExtraLints: nil},
			"empty slice":  {ExtraLints: []Lint{}},
		}
	}

	t.Run("passing fixture", func(t *testing.T) {
		dir := newTestModule(t)
		path := filepath.Join(dir, "page.gsx")
		mustWrite(t, path, strictFixture(`<Link label="Docs" htmlFor="field" url="/docs" />`))

		baseline := CheckFile(context.Background(), path)
		if baseline != nil {
			t.Fatalf("CheckFile baseline: %v", baseline)
		}
		for name, opts := range variants() {
			t.Run(name, func(t *testing.T) {
				if err := CheckFileWithOptions(context.Background(), path, opts); err != nil {
					t.Fatalf("CheckFileWithOptions(%s): %v", name, err)
				}
			})
		}
	})

	t.Run("failing fixture", func(t *testing.T) {
		dir := newTestModule(t)
		path := filepath.Join(dir, "page.gsx")
		mustWrite(t, path, strictFixture(`<Link label={123} />`))

		baseline := CheckFile(context.Background(), path)
		if baseline == nil {
			t.Fatal("expected a baseline strict type-check error")
		}
		for name, opts := range variants() {
			t.Run(name, func(t *testing.T) {
				err := CheckFileWithOptions(context.Background(), path, opts)
				if err == nil {
					t.Fatal("expected an error")
				}
				if err.Error() != baseline.Error() {
					t.Fatalf("error text diverged for %s\n got:  %s\nwant: %s", name, err.Error(), baseline.Error())
				}
			})
		}
	})
}
