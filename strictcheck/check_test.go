package strictcheck

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func newTestModule(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	stub := filepath.Join(dir, "gosxstub")
	mustMkdir(t, stub)
	mustWrite(t, filepath.Join(stub, "go.mod"), "module m31labs.dev/gosx\n\ngo 1.26\n")
	mustWrite(t, filepath.Join(stub, "node.go"), `package gosx
type Node struct{}
type AttrValue struct{}
func El(string, ...any) Node { return Node{} }
func Text(string) Node { return Node{} }
func Expr(any) Node { return Node{} }
func RawHTML(string) Node { return Node{} }
func Fragment(...Node) Node { return Node{} }
func Attr(string, any) AttrValue { return AttrValue{} }
func Attrs(...AttrValue) any { return nil }
func Props(...AttrValue) any { return nil }
func Spread(any) AttrValue { return AttrValue{} }
`)
	mustWrite(t, filepath.Join(dir, "go.mod"), "module example.test/app\n\ngo 1.26\n\nrequire m31labs.dev/gosx v0.0.0\nreplace m31labs.dev/gosx => "+filepath.ToSlash(stub)+"\n")
	return dir
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, path, contents string) {
	t.Helper()
	mustMkdir(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func strictFixture(call string) string {
	return `package main

type LinkProps struct {
	Label string
	HTMLFor string
	URL string
}

component Link(props: *LinkProps) {
	return <a>{props.Label}</a>
}

component Page() {
	return ` + call + `
}
`
}

func TestCheckFileUsesRealGoTypesAndAllowsPackageMainWithoutMain(t *testing.T) {
	dir := newTestModule(t)
	path := filepath.Join(dir, "page.gsx")
	// A real companion with the first conventional overlay name must remain in
	// the build rather than being silently replaced by a projection.
	mustWrite(t, filepath.Join(dir, "zz_gosx_strictcheck_0.go"), "package main\nconst CompanionPresent = true\n")
	mustWrite(t, path, strictFixture(`<Link label="Docs" htmlFor="field" url="/docs" />`))
	if err := CheckFile(context.Background(), path); err != nil {
		t.Fatalf("CheckFile: %v", err)
	}
}

func TestCheckTreeChecksEachPackageClauseInOneDirectory(t *testing.T) {
	dir := newTestModule(t)
	mustWrite(t, filepath.Join(dir, "a.gsx"), `package legacy
func Legacy() Node {
	return <p>{data.missing}</p>
}
`)
	mustWrite(t, filepath.Join(dir, "z.gsx"), `package strictapp
type Props struct { Label string }
component Child(props: Props) {
	return <p>{props.Label}</p>
}
component Page() {
	return <Child label={42} />
}
`)
	for _, check := range []struct {
		name string
		fn   func() error
	}{
		{name: "tree", fn: func() error { return CheckTree(context.Background(), dir) }},
		{name: "package", fn: func() error { return CheckPackage(context.Background(), dir) }},
	} {
		t.Run(check.name, func(t *testing.T) {
			err := check.fn()
			if err == nil || !strings.Contains(err.Error(), "cannot use 42") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestCheckFileRejectsUnknownAndWrongTypedPropsAtOriginalLine(t *testing.T) {
	for _, test := range []struct {
		name string
		call string
		want string
	}{
		{name: "unknown", call: `<Link mystery="x" />`, want: "unknown field Mystery"},
		{name: "wrong type", call: `<Link label={123} />`, want: "cannot use 123"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := newTestModule(t)
			path := filepath.Join(dir, "page.gsx")
			mustWrite(t, path, strictFixture(test.call))
			err := CheckFile(context.Background(), path)
			if err == nil {
				t.Fatal("expected strict type error")
			}
			message := err.Error()
			if !strings.Contains(message, "page.gsx:14:") || !strings.Contains(message, test.want) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestCheckFileCompanionGoTypesRequireExactInitialism(t *testing.T) {
	for _, test := range []struct {
		name    string
		attr    string
		wantErr bool
	}{
		{name: "exact", attr: `URL="/docs"`},
		{name: "lower camel", attr: `url="/docs"`, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := newTestModule(t)
			mustWrite(t, filepath.Join(dir, "props.go"), "package main\ntype LinkProps struct { URL string }\n")
			path := filepath.Join(dir, "page.gsx")
			mustWrite(t, path, `package main
component Link(props: *LinkProps) {
	return <a>{props.URL}</a>
}
component Page() {
	return <Link `+test.attr+` />
}
`)
			err := CheckFile(context.Background(), path)
			if test.wantErr && (err == nil || !strings.Contains(err.Error(), "unknown field Url")) {
				t.Fatalf("error = %v", err)
			}
			if !test.wantErr && err != nil {
				t.Fatalf("CheckFile: %v", err)
			}
		})
	}
}

func TestCheckFileProjectsOutInvalidLegacyDSLButChecksStrictCalls(t *testing.T) {
	for _, test := range []struct {
		name    string
		call    string
		wantErr bool
	}{
		{name: "valid strict", call: `<Card label="ok" />`},
		{name: "invalid strict", call: `<Card label={42} />`, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := newTestModule(t)
			path := filepath.Join(dir, "page.gsx")
			mustWrite(t, path, `package main
import legacy "example.test/undefined/legacy"
type CardProps struct { Label string }
func Legacy() Node {
	return <p>{legacy.Route(data.missing, request.user, unknownHelper())}</p>
}
component Card(props: CardProps) {
	return <p>{props.Label}</p>
}
component Page() {
	return `+test.call+`
}
`)
			err := CheckFile(context.Background(), path)
			if test.wantErr && (err == nil || !strings.Contains(err.Error(), "cannot use 42")) {
				t.Fatalf("error = %v", err)
			}
			if !test.wantErr && err != nil {
				t.Fatalf("CheckFile: %v", err)
			}
		})
	}
}

func TestCheckFileIncludesPeerGSXTypesButRejectsCrossFileStrictCalls(t *testing.T) {
	dir := newTestModule(t)
	mustWrite(t, filepath.Join(dir, "types.gsx"), `package main
import "time"
type PageProps struct { When time.Time }
func Legacy() Node {
	return <p>{data.missing}</p>
}
`)
	page := filepath.Join(dir, "page.gsx")
	mustWrite(t, page, `package main
component Page(props: PageProps) {
	return <time>{props.When.String()}</time>
}
`)
	if err := CheckFile(context.Background(), page); err != nil {
		t.Fatalf("peer type check: %v", err)
	}

	mustWrite(t, filepath.Join(dir, "badge.gsx"), `package main
component Badge() {
	return <span />
}
`)
	mustWrite(t, page, `package main
component Page() {
	return <Badge />
}
`)
	err := CheckFile(context.Background(), page)
	if err == nil || !strings.Contains(err.Error(), "cross-file strict component call") {
		t.Fatalf("error = %v", err)
	}
}

func TestCheckTreeSkipsGeneratedHiddenAndNestedGitButChecksUnderscoreRoutes(t *testing.T) {
	invalid := strictFixture(`<Link label={123} />`)
	valid := strictFixture(`<Link label="ok" />`)

	t.Run("skip", func(t *testing.T) {
		dir := newTestModule(t)
		mustWrite(t, filepath.Join(dir, "page.gsx"), valid)
		for _, rel := range []string{
			"build/page.gsx", "vendor/page.gsx", "node_modules/page.gsx", "testdata/page.gsx",
			".cache/page.gsx", "nested/page.gsx",
		} {
			mustWrite(t, filepath.Join(dir, rel), invalid)
		}
		mustMkdir(t, filepath.Join(dir, "nested", ".git"))
		if err := CheckTree(context.Background(), dir); err != nil {
			t.Fatalf("CheckTree: %v", err)
		}
	})

	for _, route := range []string{"_slug", "__path"} {
		t.Run(route, func(t *testing.T) {
			dir := newTestModule(t)
			path := filepath.Join(dir, route, "page.gsx")
			mustWrite(t, path, invalid)
			err := CheckTree(context.Background(), dir)
			if err == nil || !strings.Contains(err.Error(), "page.gsx:14:") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestCheckFileWithOptionsAcceptsBuildEnvironment(t *testing.T) {
	dir := newTestModule(t)
	path := filepath.Join(dir, "page.gsx")
	mustWrite(t, path, strictFixture(`<Link label="ok" />`))
	err := CheckFileWithOptions(context.Background(), path, Options{
		Env:     os.Environ(),
		GOWORK:  "off",
		GOFLAGS: "-mod=mod -buildvcs=false",
	})
	if err != nil {
		t.Fatalf("CheckFileWithOptions: %v", err)
	}
}

func TestCommandEnvPreservesWorkspaceUnlessOverridden(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("environment key casing differs on Windows")
	}
	got := commandEnv(Options{Env: []string{"PATH=/bin", "GOWORK=/workspace/go.work", "GOFLAGS=-tags=dev"}})
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, "GOWORK=/workspace/go.work") || !strings.Contains(joined, "GOFLAGS=-tags=dev") {
		t.Fatalf("environment not preserved: %v", got)
	}
	got = commandEnv(Options{Env: got, GOWORK: "off", GOFLAGS: "-mod=readonly"})
	joined = strings.Join(got, "\n")
	if strings.Contains(joined, "GOWORK=/workspace/go.work") || !strings.Contains(joined, "GOWORK=off") || !strings.Contains(joined, "GOFLAGS=-mod=readonly") {
		t.Fatalf("environment not overridden: %v", got)
	}
}
