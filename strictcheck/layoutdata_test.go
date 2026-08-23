package strictcheck

import (
	"m31labs.dev/gosx/ir"
	"path/filepath"
	"strings"
	"testing"
)

func layoutPageLoader(entries string) string {
	return `package main

import "m31labs.dev/gosx/route"

func init() {
	route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			return map[string]any{` + entries + `}, nil
		},
	})
}
`
}

func layoutWarningCount(warnings []ir.Diagnostic, key string) int {
	count := 0
	for _, warning := range warnings {
		if strings.Contains(warning.Message, "layout data."+key) {
			count++
		}
	}
	return count
}

func TestLayoutDataContractAcceptsDescendantPageKey(t *testing.T) {
	dir := newTestModule(t)
	mustWrite(t, filepath.Join(dir, "main.go"), mainGoWithAddDir())
	mustWrite(t, filepath.Join(dir, "app", "layout.gsx"), `package main
func Layout() Node {
	return <main>{data.title}<Slot /></main>
}
`)
	mustWrite(t, filepath.Join(dir, "app", "page.gsx"), formPageFixture("<p>Home</p>"))
	mustWrite(t, filepath.Join(dir, "app", "page.server.go"), layoutPageLoader(`"title": "Home",`))

	warnings := checkTreeWarnings(t, dir)
	if layoutWarningCount(warnings, "title") != 0 {
		t.Fatalf("expected the descendant page loader to satisfy layout data, got: %+v", warnings)
	}
}

func TestLayoutDataContractMissingKeyNamesPageAndRoute(t *testing.T) {
	dir := newTestModule(t)
	mustWrite(t, filepath.Join(dir, "main.go"), mainGoWithAddDir())
	mustWrite(t, filepath.Join(dir, "app", "layout.gsx"), `package main
func Layout() Node {
	return <main>{data.title}<Slot /></main>
}
`)
	mustWrite(t, filepath.Join(dir, "app", "page.gsx"), formPageFixture("<p>Home</p>"))
	mustWrite(t, filepath.Join(dir, "app", "page.server.go"), layoutPageLoader(`"other": "Home",`))

	warnings := checkTreeWarnings(t, dir)
	if layoutWarningCount(warnings, "title") != 1 {
		t.Fatalf("expected one missing layout key warning, got: %+v", warnings)
	}
	if !hasWarningContaining(warnings, "page.gsx") || !hasWarningContaining(warnings, "route /") {
		t.Fatalf("expected the warning to identify the affected page and route, got: %+v", warnings)
	}
}

func TestLayoutDataContractChecksOnlyMissingDescendants(t *testing.T) {
	dir := newTestModule(t)
	mustWrite(t, filepath.Join(dir, "main.go"), mainGoWithAddDir())
	mustWrite(t, filepath.Join(dir, "app", "layout.gsx"), `package main
func Layout() Node {
	return <main>{data.title}<Slot /></main>
}
`)
	mustWrite(t, filepath.Join(dir, "app", "one", "page.gsx"), formPageFixture("<p>One</p>"))
	mustWrite(t, filepath.Join(dir, "app", "one", "page.server.go"), layoutPageLoader(`"title": "One",`))
	mustWrite(t, filepath.Join(dir, "app", "two", "page.gsx"), formPageFixture("<p>Two</p>"))
	mustWrite(t, filepath.Join(dir, "app", "two", "page.server.go"), layoutPageLoader(`"other": "Two",`))

	warnings := checkTreeWarnings(t, dir)
	if layoutWarningCount(warnings, "title") != 1 {
		t.Fatalf("expected one missing descendant warning, got: %+v", warnings)
	}
	if !hasWarningContaining(warnings, "two/page.gsx") || hasWarningContaining(warnings, "one/page.gsx") {
		t.Fatalf("expected only the missing descendant page to be named, got: %+v", warnings)
	}
}

func TestLayoutDataContractNestedScopesExcludeSiblings(t *testing.T) {
	dir := newTestModule(t)
	mustWrite(t, filepath.Join(dir, "main.go"), mainGoWithAddDir())
	mustWrite(t, filepath.Join(dir, "app", "layout.gsx"), `package main
func Layout() Node {
	return <main>{data.root}<Slot /></main>
}
`)
	mustWrite(t, filepath.Join(dir, "app", "nested", "layout.gsx"), `package main
func Layout() Node {
	return <section>{data.nested}<Slot /></section>
}
`)
	mustWrite(t, filepath.Join(dir, "app", "sibling", "page.gsx"), formPageFixture("<p>Sibling</p>"))
	mustWrite(t, filepath.Join(dir, "app", "sibling", "page.server.go"), layoutPageLoader(`"root": "Sibling",`))
	mustWrite(t, filepath.Join(dir, "app", "nested", "page.gsx"), formPageFixture("<p>Nested</p>"))
	mustWrite(t, filepath.Join(dir, "app", "nested", "page.server.go"), layoutPageLoader(`"root": "Nested", "nested": "Nested",`))

	warnings := checkTreeWarnings(t, dir)
	if len(warnings) != 0 {
		t.Fatalf("expected nested layouts to see only their actual descendant pages, got: %+v", warnings)
	}
}

func TestLayoutDataContractAbstainsOnUnknownDescendantLoaders(t *testing.T) {
	dir := newTestModule(t)
	mustWrite(t, filepath.Join(dir, "main.go"), mainGoWithAddDir())
	mustWrite(t, filepath.Join(dir, "app", "layout.gsx"), `package main
func Layout() Node {
	return <main>{data.title}<Slot /></main>
}
`)
	mustWrite(t, filepath.Join(dir, "app", "page.gsx"), formPageFixture("<p>Unknown</p>"))
	mustWrite(t, filepath.Join(dir, "app", "page.server.go"), `package main

import "m31labs.dev/gosx/route"

func loadPageData() map[string]any { return map[string]any{"title": "Unknown"} }

func init() {
	route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			return loadPageData(), nil
		},
	})
}
`)
	mustWrite(t, filepath.Join(dir, "app", "bindings", "page.gsx"), formPageFixture("<p>Bindings</p>"))
	mustWrite(t, filepath.Join(dir, "app", "bindings", "page.server.go"), `package main

import "m31labs.dev/gosx/route"

func init() {
	route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			return map[string]any{"title": "Bindings"}, nil
		},
		Bindings: func(ctx *route.RouteContext, page route.FilePage, data any) route.FileTemplateBindings {
			return route.FileTemplateBindings{}
		},
	})
}
`)

	warnings := checkTreeWarnings(t, dir)
	if layoutWarningCount(warnings, "title") != 0 {
		t.Fatalf("expected unknown or Bindings loader descendants to abstain, got: %+v", warnings)
	}
}

func TestLayoutDataContractNeverCreditsLayoutOwnLoader(t *testing.T) {
	dir := newTestModule(t)
	mustWrite(t, filepath.Join(dir, "main.go"), mainGoWithAddDir())
	mustWrite(t, filepath.Join(dir, "app", "layout.gsx"), `package main
func Layout() Node {
	return <main>{data.title}<Slot /></main>
}
`)
	mustWrite(t, filepath.Join(dir, "app", "layout.server.go"), layoutPageLoader(`"title": "Layout",`))
	mustWrite(t, filepath.Join(dir, "app", "page.gsx"), formPageFixture("<p>Page</p>"))
	mustWrite(t, filepath.Join(dir, "app", "page.server.go"), layoutPageLoader(`"other": "Page",`))

	warnings := checkTreeWarnings(t, dir)
	if layoutWarningCount(warnings, "title") != 1 {
		t.Fatalf("expected the page loader, not the layout loader, to determine data.title, got: %+v", warnings)
	}
}

func TestLayoutDataContractAggregatesDuplicateSelectors(t *testing.T) {
	dir := newTestModule(t)
	mustWrite(t, filepath.Join(dir, "main.go"), mainGoWithAddDir())
	mustWrite(t, filepath.Join(dir, "app", "layout.gsx"), `package main
func Layout() Node {
	return <main>{data.missing}{data.missing}<Slot /></main>
}
`)
	mustWrite(t, filepath.Join(dir, "app", "page.gsx"), formPageFixture("<p>Page</p>"))

	warnings := checkTreeWarnings(t, dir)
	if layoutWarningCount(warnings, "missing") != 1 {
		t.Fatalf("expected duplicate layout selectors to produce one warning, got: %+v", warnings)
	}
}
