package strictcheck

import (
	"context"
	"path/filepath"
	"testing"

	"m31labs.dev/gosx/ir"
)

func checkTreeWarnings(t *testing.T, dir string) []ir.Diagnostic {
	t.Helper()
	var warnings []ir.Diagnostic
	if err := CheckTreeWithOptions(context.Background(), dir, Options{Warnings: &warnings}); err != nil {
		t.Fatalf("CheckTreeWithOptions: %v", err)
	}
	return warnings
}

// mainGoWithAddDir writes the "_, thisFile, _, _ := runtime.Caller(0);
// root := filepath.Dir(thisFile); router.AddDir(filepath.Join(root,
// "app"), ...)" idiom every real AddDir call in this repository uses
// (examples/basic, examples/dashboard, examples/goetrope-watch,
// examples/gosx-docs, cmd/gosx/init.go's scaffold template).
func mainGoWithAddDir() string {
	return `package main

import (
	"path/filepath"
	"runtime"

	"m31labs.dev/gosx/route"
)

func main() {
	_, thisFile, _, _ := runtime.Caller(0)
	root := filepath.Dir(thisFile)
	router := route.NewRouter()
	if err := router.AddDir(filepath.Join(root, "app"), route.FileRoutesOptions{}); err != nil {
		panic(err)
	}
}
`
}

// TestRouteMountContractWarnsOnUnmountedPage reconstructs the gosx#249
// premise-table examples/dashboard defect: a page.gsx sitting outside the
// directory tree main.go's one router.AddDir call actually mounts is dead
// code a reader mistakes for live code.
func TestRouteMountContractWarnsOnUnmountedPage(t *testing.T) {
	dir := newTestModule(t)
	mustWrite(t, filepath.Join(dir, "main.go"), mainGoWithAddDir())
	mustWrite(t, filepath.Join(dir, "app", "page.gsx"), formPageFixture(`<p>Home</p>`))
	mustWrite(t, filepath.Join(dir, "orphan", "page.gsx"), formPageFixture(`<p>Orphaned</p>`))

	warnings := checkTreeWarnings(t, dir)
	if !hasWarningContaining(warnings, "not reached by any router.AddDir mount") {
		t.Fatalf("expected a warning for the unmounted orphan page, got: %+v", warnings)
	}
	for _, w := range warnings {
		if w.Span.File == filepath.Join(dir, "app", "page.gsx") {
			t.Fatalf("did not expect the mounted app/page.gsx to be flagged, got: %+v", warnings)
		}
	}
}

// TestRouteMountContractAcceptsFullyMountedTree proves a project where
// every page.gsx sits under the AddDir root produces no warning.
func TestRouteMountContractAcceptsFullyMountedTree(t *testing.T) {
	dir := newTestModule(t)
	mustWrite(t, filepath.Join(dir, "main.go"), mainGoWithAddDir())
	mustWrite(t, filepath.Join(dir, "app", "page.gsx"), formPageFixture(`<p>Home</p>`))
	mustWrite(t, filepath.Join(dir, "app", "settings", "page.gsx"), formPageFixture(`<p>Settings</p>`))

	warnings := checkTreeWarnings(t, dir)
	if hasWarningContaining(warnings, "not reached by any router.AddDir mount") {
		t.Fatalf("expected no unmounted-page warning, got: %+v", warnings)
	}
}

// TestRouteMountContractAbstainsWithNoAddDirCall proves a project with no
// AddDir call anywhere (a project that does not use file routing, or one
// this scan cannot see using it) produces no warning rather than assuming
// every page.gsx is orphaned.
func TestRouteMountContractAbstainsWithNoAddDirCall(t *testing.T) {
	dir := newTestModule(t)
	mustWrite(t, filepath.Join(dir, "app", "page.gsx"), formPageFixture(`<p>Home</p>`))

	warnings := checkTreeWarnings(t, dir)
	if hasWarningContaining(warnings, "not reached by any router.AddDir mount") {
		t.Fatalf("expected no warning with no AddDir call anywhere, got: %+v", warnings)
	}
}

// TestRouteMountContractAbstainsOnUnresolvableAddDirTarget proves the
// "only report with confidence" rule: an AddDir call whose target this
// scan cannot resolve (here, a directory named by a function parameter)
// could mount any directory in the project, including one holding a page
// this scan would otherwise call unmounted -- so the whole run abstains
// rather than risk a false positive.
func TestRouteMountContractAbstainsOnUnresolvableAddDirTarget(t *testing.T) {
	dir := newTestModule(t)
	mustWrite(t, filepath.Join(dir, "main.go"), `package main

import "m31labs.dev/gosx/route"

func mount(router *route.Router, dynamicDir string) {
	router.AddDir(dynamicDir, route.FileRoutesOptions{})
}
`)
	mustWrite(t, filepath.Join(dir, "orphan", "page.gsx"), formPageFixture(`<p>Orphaned</p>`))

	warnings := checkTreeWarnings(t, dir)
	if hasWarningContaining(warnings, "not reached by any router.AddDir mount") {
		t.Fatalf("expected an unresolvable AddDir target to abstain the whole run, got: %+v", warnings)
	}
}

// TestRouteMountContractAbstainsOnUnrenderedMainTemplate reconstructs the
// gosx#249 scaffold defect: cmd/gosx/templates/docs ships "main.gotmpl"
// at its root, never a real "main.go" -- `gosx init` renders the
// template into a real file only when scaffolding a new project. Before
// this test existed, this scan found no AddDir call anywhere under the
// scaffold and reported every one of its pages as unmounted.
func TestRouteMountContractAbstainsOnUnrenderedMainTemplate(t *testing.T) {
	dir := newTestModule(t)
	mustWrite(t, filepath.Join(dir, "main.gotmpl"), mainGoWithAddDir())
	mustWrite(t, filepath.Join(dir, "app", "docs", "page.gsx"), formPageFixture(`<p>Docs</p>`))

	warnings := checkTreeWarnings(t, dir)
	if hasWarningContaining(warnings, "not reached by any router.AddDir mount") {
		t.Fatalf("expected a main.gotmpl ancestor to abstain, got: %+v", warnings)
	}
}

// TestRouteMountContractStillWarnsBesideAnUnrelatedMainTemplate proves
// the abstention in TestRouteMountContractAbstainsOnUnrenderedMainTemplate
// is scoped to pages actually below the templated directory: a page in a
// resolvably-mounted tree elsewhere in the same project keeps being
// checked normally.
func TestRouteMountContractStillWarnsBesideAnUnrelatedMainTemplate(t *testing.T) {
	dir := newTestModule(t)
	mustWrite(t, filepath.Join(dir, "scaffold", "main.gotmpl"), mainGoWithAddDir())
	mustWrite(t, filepath.Join(dir, "scaffold", "app", "page.gsx"), formPageFixture(`<p>Scaffold</p>`))
	mustWrite(t, filepath.Join(dir, "main.go"), mainGoWithAddDir())
	mustWrite(t, filepath.Join(dir, "app", "page.gsx"), formPageFixture(`<p>Home</p>`))
	mustWrite(t, filepath.Join(dir, "orphan", "page.gsx"), formPageFixture(`<p>Orphaned</p>`))

	warnings := checkTreeWarnings(t, dir)
	if !hasWarningContaining(warnings, "not reached by any router.AddDir mount") {
		t.Fatalf("expected the real orphan page to still be flagged, got: %+v", warnings)
	}
	for _, w := range warnings {
		if w.Span.File == filepath.Join(dir, "scaffold", "app", "page.gsx") {
			t.Fatalf("did not expect the templated scaffold's own page to be flagged, got: %+v", warnings)
		}
	}
}
