package strictcheck

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

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
// gosx#186 compatibility golden, within this build: registering no extra
// lints (the field absent, explicitly nil, or an empty non-nil slice) must
// not change a single byte of CheckFileWithOptions's returned error
// compared to CheckFile's, on both a passing fixture and a failing one.
// This proves the extension point is inert when unused in the code that is
// actually running -- it is not, and cannot be, a claim that this build's
// output matches some other build's byte for byte.
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

// --- gosx#186 review findings: B1, M1, B3, M2, m2, n1, n2, n3 -------------

// TestCheckFileMutatingLintCannotSuppressBuiltinTypeError is the gosx#186 B1
// regression: the reviewer's probe showed a one-line lint (setting
// file.Program.Components = nil) turning a real strict type error into a
// pass, because runExtraLints used to run before the built-in stages that
// read file.Program. checkPackage now runs every built-in stage to
// completion before any lint sees the package (see checkPackage's doc in
// check.go), so a lint that mutates or empties file.Program cannot affect a
// built-in read that already happened.
func TestCheckFileMutatingLintCannotSuppressBuiltinTypeError(t *testing.T) {
	t.Run("well-typed fixture: a mutating lint does not introduce a failure", func(t *testing.T) {
		dir := newTestModule(t)
		path := filepath.Join(dir, "page.gsx")
		mustWrite(t, path, strictFixture(`<Link label="Docs" htmlFor="field" url="/docs" />`))

		if baseline := CheckFile(context.Background(), path); baseline != nil {
			t.Fatalf("baseline: %v", baseline)
		}

		mutator := Lint{Name: "mutator", Check: func(f LintFile, report func(ir.Diagnostic)) {
			for i := range f.Program.Nodes {
				n := &f.Program.Nodes[i]
				for j := range n.Attrs {
					if n.Attrs[j].Name == "label" {
						n.Attrs[j].Kind = ir.AttrExpr
						n.Attrs[j].Value = "123"
					}
				}
			}
		}}
		if err := CheckFileWithOptions(context.Background(), path, Options{ExtraLints: []Lint{mutator}}); err != nil {
			t.Fatalf("CheckFileWithOptions with a mutating lint: %v", err)
		}
	})

	t.Run("type-error fixture: a deleter lint cannot suppress the built-in error", func(t *testing.T) {
		dir := newTestModule(t)
		path := filepath.Join(dir, "page.gsx")
		mustWrite(t, path, strictFixture(`<Link label={123} />`))

		baseline := CheckFile(context.Background(), path)
		if baseline == nil || !strings.Contains(baseline.Error(), "cannot use 123") {
			t.Fatalf("baseline = %v, want a \"cannot use 123\" type error", baseline)
		}

		deleter := Lint{Name: "deleter", Check: func(f LintFile, report func(ir.Diagnostic)) {
			f.Program.Components = nil
		}}
		err := CheckFileWithOptions(context.Background(), path, Options{ExtraLints: []Lint{deleter}})
		if err == nil || !strings.Contains(err.Error(), "cannot use 123") {
			t.Fatalf("broken fixture + deleter lint = %v, want the built-in type error to survive", err)
		}
	})
}

func alwaysLint(name string) Lint {
	return Lint{Name: name, Check: func(f LintFile, report func(ir.Diagnostic)) {
		report(ir.Diagnostic{Code: "EM999", Message: "lint ran on " + filepath.Base(f.Path)})
	}}
}

// TestCheckFileJoinsLintFindingsWhenRenderEntryValidationFails proves the
// first of M1's early-return paths (checkPackage's validateStrictRenderEntries
// failure) still runs and joins extra lint findings instead of returning
// before any lint sees the package.
func TestCheckFileJoinsLintFindingsWhenRenderEntryValidationFails(t *testing.T) {
	dir := newTestModule(t)
	path := filepath.Join(dir, "page.gsx")
	mustWrite(t, path, `package main

type PageProps struct { Title string }

component Page(props: *PageProps) {
	return <div>{props.Title}</div>
}
`)
	err := CheckFileWithOptions(context.Background(), path, Options{ExtraLints: []Lint{alwaysLint("f1")}})
	if err == nil {
		t.Fatal("expected an error")
	}
	message := err.Error()
	if !strings.Contains(message, "file routes do not bind root props") {
		t.Fatalf("missing the render-entry built-in error: %s", message)
	}
	if !strings.Contains(message, "EM999") {
		t.Fatalf("missing the lint finding: %s", message)
	}
}

// TestCheckFileJoinsLintFindingsWhenImportResolutionFails proves the second
// of M1's early-return paths (checkPackage's resolveImportNames failure)
// still joins extra lint findings.
func TestCheckFileJoinsLintFindingsWhenImportResolutionFails(t *testing.T) {
	dir := newTestModule(t)
	path := filepath.Join(dir, "page.gsx")
	mustWrite(t, path, `package main

import "time"

type LinkProps struct {
	Label string
}

component Link(props: *LinkProps) {
	return <a>{props.Label}</a>
}

component Page() {
	return <Link label="ok" />
}
`)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := CheckFileWithOptions(ctx, path, Options{ExtraLints: []Lint{alwaysLint("f2")}})
	if err == nil {
		t.Fatal("expected an error")
	}
	message := err.Error()
	if !strings.Contains(message, "context canceled") {
		t.Fatalf("missing the import-resolution built-in error: %s", message)
	}
	if !strings.Contains(message, "EM999") {
		t.Fatalf("missing the lint finding: %s", message)
	}
}

// TestCheckFileJoinsLintFindingsWhenTranspileBoundaryFails proves the third
// of M1's early-return paths (checkPackage's TranspilePackageWithImportNames
// failure, via ValidateStrictPackageBoundaries) still joins extra lint
// findings. The caller must itself be a *legacy* component: a strict caller
// referencing an unknown-locally tag is already rejected earlier, at
// gosx.Compile/ir.Lower time for that one file (see
// TestCompileRejectsCrossFileStrictCallBeforePackageProjection in
// transpile/package_test.go) -- LoadPackage would fail and checkPackage
// would never run at all, which is M3's rule, not M1's. A legacy caller
// passes that per-file Lower check and only trips the package-wide boundary
// check in TranspilePackageWithImportNames (see
// TestTranspilePackageStrictOwnerCannotBeHiddenByLegacyPeer in the same
// file, which this mirrors).
func TestCheckFileJoinsLintFindingsWhenTranspileBoundaryFails(t *testing.T) {
	dir := newTestModule(t)
	mustWrite(t, filepath.Join(dir, "a_badge.gsx"), `package main
component Badge() {
	return <strong>strict</strong>
}
`)
	page := filepath.Join(dir, "page.gsx")
	mustWrite(t, page, `package main
func Page() Node {
	return <Badge />
}
`)
	err := CheckFileWithOptions(context.Background(), page, Options{ExtraLints: []Lint{alwaysLint("f3")}})
	if err == nil {
		t.Fatal("expected an error")
	}
	message := err.Error()
	if !strings.Contains(message, "cross-file strict component call") {
		t.Fatalf("missing the transpile-boundary built-in error: %s", message)
	}
	if !strings.Contains(message, "EM999") {
		t.Fatalf("missing the lint finding: %s", message)
	}
}

// TestCheckTreeReportsLintFindingsFromEveryOffendingPackageNotJustTheFirst is
// the gosx#186 B3 regression: the reviewer's probe put the same script-
// flagging lint over three packages and only the first package's findings
// came back, because checkSourcePackages stopped at the first package
// error. It now accumulates across every package.
func TestCheckTreeReportsLintFindingsFromEveryOffendingPackageNotJustTheFirst(t *testing.T) {
	root := t.TempDir()
	mark := Lint{Name: "mark", Check: func(f LintFile, report func(ir.Diagnostic)) {
		for i := range f.Program.Nodes {
			n := &f.Program.Nodes[i]
			if n.Kind == ir.NodeElement && n.Tag == "script" {
				report(ir.Diagnostic{Code: "EM001", Message: "no script in " + filepath.Base(filepath.Dir(f.Path)) + "/" + filepath.Base(f.Path)})
			}
		}
	}}
	for _, pkg := range []string{"aa", "bb", "cc"} {
		mustWrite(t, filepath.Join(root, pkg, "page.gsx"), "package "+pkg+"\n\nfunc Page() Node {\n\treturn <div>\n<script>x()</script>\n</div>\n}\n")
	}

	err := CheckTreeWithOptions(context.Background(), root, Options{ExtraLints: []Lint{mark}})
	if err == nil {
		t.Fatal("expected diagnostics from every package")
	}
	message := err.Error()
	for _, pkg := range []string{"aa", "bb", "cc"} {
		want := "no script in " + pkg + "/page.gsx"
		if !strings.Contains(message, want) {
			t.Fatalf("missing %q -- only some packages' findings survived? message:\n%s", want, message)
		}
	}
}

// TestExtraLintsConcurrentReportCallsDuringCheckAreRaceFree proves gosx#186
// M2: the reviewer's probe raced report under `go test -race` when several
// goroutines a lint spawned called it concurrently while Check was still
// running. report is now mutex-guarded; run this test with -race.
func TestExtraLintsConcurrentReportCallsDuringCheckAreRaceFree(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "page.gsx")
	mustWrite(t, path, "package app\n\nfunc Page() Node {\n\treturn <div id=\"m\">x</div>\n}\n")

	const fanOut = 8
	var wg sync.WaitGroup
	concurrent := Lint{Name: "concurrent", Check: func(f LintFile, report func(ir.Diagnostic)) {
		for i := 0; i < fanOut; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				report(ir.Diagnostic{Code: "CONC", Message: fmt.Sprintf("finding %d", i)})
			}(i)
		}
		wg.Wait() // every report call lands during Check, as the doc requires.
	}}

	err := CheckFileWithOptions(context.Background(), path, Options{ExtraLints: []Lint{concurrent}})
	if err == nil {
		t.Fatal("expected diagnostics")
	}
	if got := strings.Count(err.Error(), "CONC: finding"); got != fanOut {
		t.Fatalf("got %d CONC findings, want %d; message:\n%s", got, fanOut, err.Error())
	}
}

// TestExtraLintsReportCallAfterCheckReturnsIsDroppedNotPanicked proves the
// gosx#186 M2 no-retention rule documented on Lint.Check: a report call
// arriving from a goroutine Check left running, after Check itself already
// returned, does not panic, does not race (run this test with -race), and
// does not appear in the check result -- it is dropped.
func TestExtraLintsReportCallAfterCheckReturnsIsDroppedNotPanicked(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "page.gsx")
	mustWrite(t, path, "package app\n\nfunc Page() Node {\n\treturn <div id=\"m\">x</div>\n}\n")

	lateCalled := make(chan struct{})
	late := Lint{Name: "late", Check: func(f LintFile, report func(ir.Diagnostic)) {
		go func() {
			time.Sleep(20 * time.Millisecond) // run well after Check returns
			report(ir.Diagnostic{Code: "LATE", Message: "reported after Check returned"})
			close(lateCalled)
		}()
	}}
	good := Lint{Name: "after", Check: func(f LintFile, report func(ir.Diagnostic)) {
		report(ir.Diagnostic{Code: "OK", Message: "ran after"})
	}}

	err := CheckFileWithOptions(context.Background(), path, Options{ExtraLints: []Lint{late, good}})
	<-lateCalled // wait for the late call to actually happen before asserting

	if err == nil || !strings.Contains(err.Error(), "OK: ran after") {
		t.Fatalf("expected the well-behaved lint's finding, got: %v", err)
	}
	if strings.Contains(err.Error(), "LATE") {
		t.Fatalf("a report call after Check returns should have been dropped, got: %v", err)
	}
}

// TestExtraLintsNilCheckIsRejectedWithAClearMessageNotAFakePanic proves
// gosx#186 m2: a Lint with a nil Check is now rejected outright with a
// message that names the real problem, rather than being invoked (which
// would nil-pointer-panic) and reported as a misleading "panicked"
// diagnostic.
func TestExtraLintsNilCheckIsRejectedWithAClearMessageNotAFakePanic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "page.gsx")
	mustWrite(t, path, "package app\n\nfunc Page() Node {\n\treturn <div id=\"m\">x</div>\n}\n")

	err := CheckFileWithOptions(context.Background(), path, Options{
		ExtraLints: []Lint{{Name: "no-check-func"}},
	})
	if err == nil {
		t.Fatal("expected a diagnostic for the nil Check")
	}
	message := err.Error()
	if !strings.Contains(message, `lint "no-check-func" has no Check function and was skipped`) {
		t.Fatalf("message = %q, want the nil-Check explanation", message)
	}
	if strings.Contains(message, "panicked") {
		t.Fatalf("message should not claim a panic for a configuration mistake: %q", message)
	}
}

// TestExtraLintsUnnamedLintFallsBackToItsIndexInContainmentMessages proves
// gosx#186 n3: a Lint with an empty Name still identifies itself in a
// containment-style message, by its position in Options.ExtraLints, rather
// than printing an empty pair of quotes.
func TestExtraLintsUnnamedLintFallsBackToItsIndexInContainmentMessages(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "page.gsx")
	mustWrite(t, path, "package app\n\nfunc Page() Node {\n\treturn <div id=\"m\">x</div>\n}\n")

	named := Lint{Name: "named", Check: func(f LintFile, report func(ir.Diagnostic)) {}}
	unnamed := Lint{Check: func(f LintFile, report func(ir.Diagnostic)) { panic("x") }}

	err := CheckFileWithOptions(context.Background(), path, Options{ExtraLints: []Lint{named, unnamed}})
	if err == nil {
		t.Fatal("expected the panic-containment diagnostic")
	}
	message := err.Error()
	if !strings.Contains(message, "lint #1 panicked") {
		t.Fatalf("message = %q, want the unnamed lint labeled by its index (#1)", message)
	}
	if strings.Contains(message, `lint "" panicked`) {
		t.Fatalf("message should not print an empty name: %q", message)
	}
}

// TestExtraLintsReportEscapesNewlinesInCodeAndMessage proves gosx#186 n1: a
// newline in a lint-supplied Code or Message cannot inject what looks like
// an extra diagnostic line into the joined error text.
func TestExtraLintsReportEscapesNewlinesInCodeAndMessage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "page.gsx")
	mustWrite(t, path, "package app\n\nfunc Page() Node {\n\treturn <div id=\"m\">x</div>\n}\n")

	injector := Lint{Name: "injector", Check: func(f LintFile, report func(ir.Diagnostic)) {
		report(ir.Diagnostic{Code: "EM1\n02", Message: "line one\nline two: not a real diagnostic"})
	}}
	err := CheckFileWithOptions(context.Background(), path, Options{ExtraLints: []Lint{injector}})
	if err == nil {
		t.Fatal("expected a diagnostic")
	}
	message := err.Error()
	for _, line := range strings.Split(message, "\n") {
		if strings.Contains(line, "not a real diagnostic") && !strings.Contains(line, "line one") {
			t.Fatalf("newline was not escaped; it split one diagnostic across lines: %q", line)
		}
	}
	if !strings.Contains(message, `EM1\n02`) || !strings.Contains(message, `line one\nline two`) {
		t.Fatalf("expected the escaped literal \\n, got: %s", message)
	}
}

// TestExtraLintsReportFillsFileOnlyWhenSpanFileIsEmpty proves gosx#186 n2:
// report fills Span.File with the file it was invoked for only when the
// lint left it empty; a lint that deliberately sets a different Span.File
// is trusted, since a Lint.Check is trusted in-process code.
func TestExtraLintsReportFillsFileOnlyWhenSpanFileIsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "page.gsx")
	mustWrite(t, path, "package app\n\nfunc Page() Node {\n\treturn <div id=\"m\">x</div>\n}\n")

	wrongFile := Lint{Name: "wrongfile", Check: func(f LintFile, report func(ir.Diagnostic)) {
		report(ir.Diagnostic{Span: ir.Span{File: "/etc/passwd", StartLine: 1, StartCol: 1}, Code: "X", Message: "points at another file"})
	}}
	err := CheckFileWithOptions(context.Background(), path, Options{ExtraLints: []Lint{wrongFile}})
	if err == nil || !strings.Contains(err.Error(), "/etc/passwd:1:1: X: points at another file") {
		t.Fatalf("expected the lint's own Span.File to survive untouched, got: %v", err)
	}
}
