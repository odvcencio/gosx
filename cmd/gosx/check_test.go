package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunCheckAcceptsModernGSXShapes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "page.gsx")
	writeTempFile(t, dir, "page.gsx", `package main

func Page(item Item, ok bool) Node {
	return <article>
		<div class="card">
			<Link href={item.EditHref} class="btn btn-sm">Edit</Link>
			<Link href={item.ViewHref} class="btn btn-sm">View</Link>
		</div>
		<If when={ok}>
			<div class="empty">foo=bar</div>
		</If>
		<p>alpha & beta</p>
		<Demo.ThemeSwitcher></Demo.ThemeSwitcher>
		<Avatar userId={item.AuthorID} />
	</article>
}
`)

	var stderr bytes.Buffer
	if err := runCheck(path, &stderr); err != nil {
		t.Fatalf("runCheck failed: %v", err)
	}

	output := stderr.String()
	if !strings.Contains(output, "ok: 1 components") {
		t.Fatalf("unexpected check output: %q", output)
	}
	if !strings.Contains(output, "Page(Item)") {
		t.Fatalf("expected component signature in output: %q", output)
	}
}

// TestRunCheckWarnsOnUntypedLegacyComponent covers step one of retiring
// legacy component syntax: an untyped legacy component (`func Name(props
// any) Node`) must produce a warning naming the component and the strict
// replacement, but must not fail the check — 7 such declarations exist in
// this repo today and must keep checking, compiling, and rendering exactly
// as they do now.
func TestRunCheckWarnsOnUntypedLegacyComponent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "page.gsx")
	writeTempFile(t, dir, "page.gsx", `package main

func FeatureCard(props any) Node {
	return <div class="card">{props}</div>
}

func Page() Node {
	return <FeatureCard>hi</FeatureCard>
}
`)

	var stderr bytes.Buffer
	if err := runCheck(path, &stderr); err != nil {
		t.Fatalf("runCheck failed: %v", err)
	}

	output := stderr.String()
	if !strings.Contains(output, "warning:") {
		t.Fatalf("expected a warning line, got: %q", output)
	}
	if !strings.Contains(output, "FeatureCard") {
		t.Fatalf("expected the warning to name FeatureCard, got: %q", output)
	}
	if !strings.Contains(output, "component FeatureCard(props: FeatureCardProps)") {
		t.Fatalf("expected the warning to point at the strict replacement, got: %q", output)
	}
	if !strings.Contains(output, "v1.0") {
		t.Fatalf("expected the warning to say the form is removed before v1.0, got: %q", output)
	}
	if !strings.Contains(output, "ok: 2 components") {
		t.Fatalf("expected check to still pass, got: %q", output)
	}
}

func TestRunCheckReportsReadError(t *testing.T) {
	var stderr bytes.Buffer
	err := runCheck(filepath.Join(t.TempDir(), "missing.gsx"), &stderr)
	if err == nil {
		t.Fatal("expected missing file error")
	}
	if !strings.Contains(err.Error(), "read") {
		t.Fatalf("unexpected error: %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected no stderr output on failure, got %q", stderr.String())
	}
}

func TestRunCheckAndRenderRejectStrictGoTypeErrors(t *testing.T) {
	dir := newInvalidStrictStarter(t, "check-render-strict-gate")
	path := filepath.Join(dir, "app", "page.gsx")
	for _, check := range []struct {
		name string
		fn   func() error
	}{
		{name: "check", fn: func() error { return runCheck(path, &bytes.Buffer{}) }},
		{name: "render", fn: func() error { return runRender(path, "", &bytes.Buffer{}) }},
	} {
		t.Run(check.name, func(t *testing.T) {
			err := check.fn()
			if err == nil || !strings.Contains(err.Error(), "cannot use 42") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestRunCheckAndRenderRejectStrictSemanticDivergence(t *testing.T) {
	dir := newInvalidStrictStarter(t, "check-render-strict-semantics")
	path := filepath.Join(dir, "app", "page.gsx")
	mustWriteFile(t, path, `package app
type Props struct { A int; B int }
component Page(props: Props) {
	return <main>{props.A / props.B}</main>
}
`)
	for _, check := range []struct {
		name string
		fn   func() error
	}{
		{name: "check", fn: func() error { return runCheck(path, &bytes.Buffer{}) }},
		{name: "render", fn: func() error { return runRender(path, "", &bytes.Buffer{}) }},
	} {
		t.Run(check.name, func(t *testing.T) {
			err := check.fn()
			if err == nil || !strings.Contains(err.Error(), `binary operator "/" is not supported`) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

// TestRunCheckAndRenderRejectPropsBearingStrictLayoutEntry proves the
// narrowed gate (gosx#248) still refuses a props-bearing strict entry where
// the render path genuinely cannot bind it: a layout. No code path calls a
// layout's own module's Load hook, so a layout's EntryProps is always nil.
// A props-bearing strict Page entry, by contrast, now passes — see
// TestRunCheckAndRenderAcceptPropsBearingStrictPageEntry.
func TestRunCheckAndRenderRejectPropsBearingStrictLayoutEntry(t *testing.T) {
	dir := newInvalidStrictStarter(t, "check-render-root-props-gate")
	path := filepath.Join(dir, "app", "layout.gsx")
	mustWriteFile(t, path, `package app
type LayoutProps struct { Title string }
component Layout(props: LayoutProps) {
	return <main>{props.Title}</main>
}
`)
	for _, check := range []struct {
		name string
		fn   func() error
	}{
		{name: "check", fn: func() error { return runCheck(path, &bytes.Buffer{}) }},
		{name: "render", fn: func() error { return runRender(path, "", &bytes.Buffer{}) }},
	} {
		t.Run(check.name, func(t *testing.T) {
			err := check.fn()
			if err == nil || !strings.Contains(err.Error(), "layout has no Load hook wired to its own root props") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

// TestRunCheckAcceptsPropsBearingStrictPageEntry proves gosx#248's
// narrowing: `gosx check` now accepts a strict Page entry that declares
// props, because renderFilePage binds it from this file's own Load hook at
// request time (see route/filesystem.go).
//
// `gosx render` still refuses the same file, but for a different, correct
// reason: it renders through route.RenderProgramComponent with an empty
// ProgramRenderEnv, a standalone preview path with no Load hook and no HTTP
// request to draw ctx.Data from — so EntryProps is genuinely nil here, the
// same "no root props binding" refusal ProgramRenderEnv.Props documents for
// any caller that renders a props-declaring strict entry without supplying
// Props (see TestRenderProgramComponentRejectsStrictRootProps).
func TestRunCheckAcceptsPropsBearingStrictPageEntry(t *testing.T) {
	dir := newInvalidStrictStarter(t, "check-render-root-props-accept")
	path := filepath.Join(dir, "app", "page.gsx")
	mustWriteFile(t, path, `package app
type PageProps struct { Title string }
component Page(props: PageProps) {
	return <main>{props.Title}</main>
}
`)
	if err := runCheck(path, &bytes.Buffer{}); err != nil {
		t.Fatalf("runCheck error = %v, want a props-bearing Page entry to pass", err)
	}
	err := runRender(path, "", &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "no root props binding") {
		t.Fatalf("runRender error = %v, want the no-Props-supplied refusal", err)
	}
}

func TestRunCheckAndRenderRejectStrictClientDirectiveComponents(t *testing.T) {
	for _, tc := range []struct {
		name      string
		directive string
		want      string
	}{
		{name: "island", directive: "//gosx:island", want: "strict island declarations are not supported"},
		{name: "engine", directive: "//gosx:engine surface", want: "strict engine declarations are not supported"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := newInvalidStrictStarter(t, "check-render-strict-"+tc.name)
			path := filepath.Join(dir, "app", "page.gsx")
			mustWriteFile(t, path, "package app\n"+tc.directive+"\ncomponent Page() {\nreturn <canvas />\n}\n")
			for _, check := range []struct {
				name string
				fn   func() error
			}{
				{name: "check", fn: func() error { return runCheck(path, &bytes.Buffer{}) }},
				{name: "render", fn: func() error { return runRender(path, "", &bytes.Buffer{}) }},
			} {
				t.Run(check.name, func(t *testing.T) {
					err := check.fn()
					if err == nil || !strings.Contains(err.Error(), tc.want) {
						t.Fatalf("error = %v", err)
					}
				})
			}
		})
	}
}

func TestRunCheckRejectsLegacyCallerIntoStrictCalleeBeforePropTyping(t *testing.T) {
	dir := newInvalidStrictStarter(t, "check-cross-style-gate")
	path := filepath.Join(dir, "app", "page.gsx")
	for _, call := range []string{`<Badge label={42} />`, `<Badge mystery="x" />`} {
		mustWriteFile(t, path, `package app
type BadgeProps struct { Label string }
component Badge(props: BadgeProps) {
	return <strong>{props.Label}</strong>
}
func Page() Node {
	return `+call+`
}
`)
		err := runCheck(path, &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), "legacy component cannot call strict component Badge") {
			t.Fatalf("call %s: error = %v", call, err)
		}
	}
}

// TestRunCheckRejectsLengthMemberInCond covers gosx#164: `gosx check` used to
// report ok for a legacy `<If cond={data.picks.length == 0}>`, and the page
// then silently rendered neither branch (a slice has no .length; the
// reflective renderer resolves it to nil, and nil never equals 0). check must
// now fail closed and name the offending expression.
func TestRunCheckRejectsLengthMemberInCond(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "page.gsx")
	writeTempFile(t, dir, "page.gsx", `package main

func Page(data any) Node {
	return <div>
		<If cond={data.picks.length == 0}>
			<b>empty</b>
		</If>
	</div>
}
`)

	err := runCheck(path, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected runCheck to reject .length in a cond expression")
	}
	if !strings.Contains(err.Error(), ".length") {
		t.Fatalf("expected error naming .length, got: %v", err)
	}
	if !strings.Contains(err.Error(), "data.picks.length == 0") {
		t.Fatalf("expected error naming the offending expression, got: %v", err)
	}
}

// TestRunCheckAcceptsValidCondWorkaround proves the documented workaround —
// passing a precomputed boolean from a DataLoader instead of reading .length
// in the template — still passes check.
func TestRunCheckAcceptsValidCondWorkaround(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "page.gsx")
	writeTempFile(t, dir, "page.gsx", `package main

func Page(data any) Node {
	return <div>
		<If cond={data.picksEmpty}>
			<b>empty</b>
		</If>
	</div>
}
`)

	if err := runCheck(path, &bytes.Buffer{}); err != nil {
		t.Fatalf("runCheck failed on a valid cond: %v", err)
	}
}

func TestRunCheckAcceptsDocsAppPages(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "examples", "gosx-docs", "app"))
	if err != nil {
		t.Fatal(err)
	}

	var checked int
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || filepath.Ext(path) != ".gsx" {
			return nil
		}

		var stderr bytes.Buffer
		if err := runCheck(path, &stderr); err != nil {
			t.Fatalf("runCheck(%s) failed: %v", path, err)
		}
		if !strings.Contains(stderr.String(), "ok: ") {
			t.Fatalf("unexpected check output for %s: %q", path, stderr.String())
		}
		checked++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked == 0 {
		t.Fatal("expected to check docs app GSX files")
	}
}
