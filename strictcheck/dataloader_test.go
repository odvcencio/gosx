package strictcheck

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx/ir"
)

// checkFileWarnings runs CheckFileWithOptions against path and returns
// every warning-severity diagnostic collected, failing the test if the
// check itself returns an error (every fixture in this file is expected
// to be a clean, error-free page.gsx).
func checkFileWarnings(t *testing.T, path string) []ir.Diagnostic {
	t.Helper()
	var warnings []ir.Diagnostic
	if err := CheckFileWithOptions(context.Background(), path, Options{Warnings: &warnings}); err != nil {
		t.Fatalf("CheckFileWithOptions: %v", err)
	}
	return warnings
}

func hasWarningContaining(warnings []ir.Diagnostic, substr string) bool {
	for _, w := range warnings {
		if strings.Contains(w.Message, substr) {
			return true
		}
	}
	return false
}

// --- Rejects (warns) -----------------------------------------------------

// TestDataLoaderKeysWarnsOnMissingKey is check 4's own proof: a page reads
// data.open_seats, but Load's one, fully literal return only ever produces
// "seats" -- a typo that renders silently empty forever, gosx#249's
// largest defect class.
func TestDataLoaderKeysWarnsOnMissingKey(t *testing.T) {
	dir := newTestModule(t)
	mustWrite(t, filepath.Join(dir, "page.gsx"), formPageFixture(`<p>{data.open_seats}</p>`))
	mustWrite(t, filepath.Join(dir, "page.server.go"), `package main

import "m31labs.dev/gosx/route"

func init() {
	route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			return map[string]any{
				"seats": 12,
			}, nil
		},
	})
}
`)
	warnings := checkFileWarnings(t, filepath.Join(dir, "page.gsx"))
	if !hasWarningContaining(warnings, "data.open_seats") {
		t.Fatalf("expected a warning naming data.open_seats, got: %+v", warnings)
	}
}

// TestDataLoaderKeysAcceptsPresentKey proves the same fixture shape
// produces no warning once the key actually matches.
func TestDataLoaderKeysAcceptsPresentKey(t *testing.T) {
	dir := newTestModule(t)
	mustWrite(t, filepath.Join(dir, "page.gsx"), formPageFixture(`<p>{data.open_seats}</p>`))
	mustWrite(t, filepath.Join(dir, "page.server.go"), `package main

import "m31labs.dev/gosx/route"

func init() {
	route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			return map[string]any{
				"open_seats": 12,
			}, nil
		},
	})
}
`)
	warnings := checkFileWarnings(t, filepath.Join(dir, "page.gsx"))
	if hasWarningContaining(warnings, "data.open_seats") {
		t.Fatalf("expected no warning for a present key, got: %+v", warnings)
	}
}

// TestDataLoaderKeysWarnsWhenNoLoadAtAll proves the confidently-empty case:
// no Load hook exists anywhere, so "data" is always nil, and a data.X read
// is certain to be a bug, not merely a guess.
func TestDataLoaderKeysWarnsWhenNoLoadAtAll(t *testing.T) {
	dir := newTestModule(t)
	mustWrite(t, filepath.Join(dir, "page.gsx"), formPageFixture(`<p>{data.anything}</p>`))
	warnings := checkFileWarnings(t, filepath.Join(dir, "page.gsx"))
	if !hasWarningContaining(warnings, "data.anything") {
		t.Fatalf("expected a warning for data.anything with no Load hook, got: %+v", warnings)
	}
}

// TestDataLoaderKeysAbstainsOnNonLiteralReturn proves the honesty rule
// (gosx#249): a Load hook that returns anything this scan cannot read as a
// complete literal map (here, a call to a helper) must not be flagged --
// the true key set might include "open_seats" and this scan simply cannot
// see it.
func TestDataLoaderKeysAbstainsOnNonLiteralReturn(t *testing.T) {
	dir := newTestModule(t)
	mustWrite(t, filepath.Join(dir, "page.gsx"), formPageFixture(`<p>{data.open_seats}</p>`))
	mustWrite(t, filepath.Join(dir, "page.server.go"), `package main

import "m31labs.dev/gosx/route"

func init() {
	route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			return fetchSeatData(), nil
		},
	})
}

func fetchSeatData() map[string]any {
	return map[string]any{"open_seats": 12}
}
`)
	warnings := checkFileWarnings(t, filepath.Join(dir, "page.gsx"))
	if hasWarningContaining(warnings, "data.open_seats") {
		t.Fatalf("expected a non-literal Load return to abstain, got: %+v", warnings)
	}
}

// TestDataLoaderKeysAbstainsWhenBindingsMightOverrideData proves the
// FileModule.Bindings safety rule: withBindings does a whole-value
// overwrite of "data" (route/fileeval.go), so a page that also sets
// Bindings cannot be vouched for by Load's key set alone.
func TestDataLoaderKeysAbstainsWhenBindingsMightOverrideData(t *testing.T) {
	dir := newTestModule(t)
	mustWrite(t, filepath.Join(dir, "page.gsx"), formPageFixture(`<p>{data.open_seats}</p>`))
	mustWrite(t, filepath.Join(dir, "page.server.go"), `package main

import "m31labs.dev/gosx/route"

func init() {
	route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			return map[string]any{"seats": 12}, nil
		},
		Bindings: func(ctx *route.RouteContext, page route.FilePage, data any) route.FileTemplateBindings {
			return route.FileTemplateBindings{}
		},
	})
}
`)
	warnings := checkFileWarnings(t, filepath.Join(dir, "page.gsx"))
	if hasWarningContaining(warnings, "data.open_seats") {
		t.Fatalf("expected a page with Bindings set to abstain, got: %+v", warnings)
	}
}

// TestDataLoaderKeysAcceptsUnionOfBothReturnBranches proves keys are
// unioned across every literal return branch, not just the first.
func TestDataLoaderKeysAcceptsUnionOfBothReturnBranches(t *testing.T) {
	dir := newTestModule(t)
	mustWrite(t, filepath.Join(dir, "page.gsx"), formPageFixture(`<p>{data.b}</p>`))
	mustWrite(t, filepath.Join(dir, "page.server.go"), `package main

import "m31labs.dev/gosx/route"

func init() {
	route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			if page.Pattern == "/x" {
				return map[string]any{"a": 1}, nil
			}
			return map[string]any{"b": 2}, nil
		},
	})
}
`)
	warnings := checkFileWarnings(t, filepath.Join(dir, "page.gsx"))
	if hasWarningContaining(warnings, "data.b") {
		t.Fatalf("expected data.b to be accepted via the second branch, got: %+v", warnings)
	}
}

// TestDataLoaderKeysIgnoresStrictComponents proves the reflective "data"
// binding rule is scoped to legacy syntax only (route/fileeval.go binds
// "data" only in the reflective file renderer); a strict component's own
// typed props are out of scope for this check entirely, so a same-named
// "data.whatever" read inside one strict-syntax fixture would be a
// misleading test here -- this test instead just proves the legacy-only
// scope by confirming a strict-syntax file with no Load produces no
// data.X finding for an unrelated legacy field read elsewhere.
func TestDataLoaderKeysIgnoresStrictComponents(t *testing.T) {
	dir := newTestModule(t)
	// "widget.gsx", not "page.gsx"/"index.gsx"/"layout.gsx": this proves
	// the reflective "data" binding's own scope (a strict component has
	// typed props, not this binding, regardless of file name), without
	// also tripping validateStrictRenderEntries' unrelated file-routed
	// zero-props rule, which only applies to a page/layout/index entry.
	mustWrite(t, filepath.Join(dir, "widget.gsx"), `package main

type WidgetProps struct {
	Open int
}

component Widget(props: *WidgetProps) {
	return <p>{props.Open}</p>
}
`)
	warnings := checkFileWarnings(t, filepath.Join(dir, "widget.gsx"))
	if len(warnings) != 0 {
		t.Fatalf("expected no data.X warnings for a strict component, got: %+v", warnings)
	}
}
