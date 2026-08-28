package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// inline_assets_test.go covers inlineAssets: the build-artifact class this
// tool prepares for a Go source to go:embed directly, rather than for a
// fetched <script src> bundle. gosx#221 added it so
// client/runtime/host/navigation.ts (app.EnableNavigation's inline
// per-page navigation runtime) ships minified instead of raw, without
// joining the fetched bundle graph in outputs (see the inlineAssets doc
// comment in main.go for why navigation.ts stays out of that graph).
//
// These tests build synthetic inline assets, the same way build_test.go and
// closure_test.go build synthetic outputs chunks. Separate tests below cover
// the real, committed client/runtime/host/navigation-runtime.min.js.

// writeHostSource writes one source file below host-src/ inside the fixture
// directory and returns the relative path an inline asset entry uses. It is
// the inlineAssets analog of fixture.writeSource, which is hard-coded to
// bootstrap-src/ for the outputs bundle graph.
func (f *fixture) writeHostSource(name, body string) string {
	f.t.Helper()
	rel := "host-src/" + name
	dir := f.path("host-src")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		f.t.Fatalf("create host-src: %v", err)
	}
	if err := os.WriteFile(f.path("host-src", name), []byte(body), 0o644); err != nil {
		f.t.Fatalf("write %s: %v", rel, err)
	}
	return rel
}

// inlineAssetFixture writes one synthetic inline asset: an adapter file that
// declares a top-level var, and a consumer file that reads it and installs a
// global. This mirrors the real shape (compatibility.ts declares gosxHost;
// navigation.ts reads it) closely enough to exercise the concatenate/
// minify/closure path the real asset goes through.
func inlineAssetFixture(t *testing.T) (*fixture, output) {
	t.Helper()
	f := newFixture(t)
	adapter := f.writeHostSource("adapter.ts", "var fixtureHost = window.__gosx_fixture_host || (window.__gosx_fixture_host = {});\n")
	consumer := f.writeHostSource("consumer.ts", "(function () {\n  fixtureHost.installed = true;\n  window.__gosx_fixture_consumer_ran = true;\n})();\n")
	entry := chunk("host-src/consumer-runtime.min.js", adapter, consumer)
	return f, entry
}

// TestInlineAssetBuildWritesOnlyTheMinifiedFile proves the build produces the
// minified artifact and nothing else: no .map, no .gz, no .br, and it does
// not touch chunks.json. An inline asset is never fetched over the network,
// so none of the fetched-bundle sidecars apply.
func TestInlineAssetBuildWritesOnlyTheMinifiedFile(t *testing.T) {
	f, entry := inlineAssetFixture(t)
	useInlineAssets(t, entry)

	if err := runTool(t, "-dir", f.dir); err != nil {
		t.Fatalf("build: %v", err)
	}

	code := f.read(entry.name)
	if len(code) == 0 {
		t.Fatal("build wrote an empty inline asset")
	}
	if !bytes.Contains(code, []byte("__gosx_fixture_consumer_ran")) {
		t.Error("inline asset lost the consumer source")
	}
	if !strings.HasSuffix(string(code), "\n") {
		t.Error("inline asset does not end with a newline")
	}

	for _, suffix := range []string{".map", ".gz", ".br"} {
		if _, err := os.Stat(f.path(entry.name + suffix)); err == nil {
			t.Errorf("build wrote %s%s; an inline asset must ship no fetched-bundle sidecar", entry.name, suffix)
		}
	}
	// run() always writes chunks.json, even for an outputs-empty run (an
	// empty chunk list is still a valid, honest manifest of the fetched
	// bundle graph). What an inline asset must never do is appear inside
	// it: chunks.json is the fetched-bundle-to-symbol map JS tests read,
	// and an inline asset is never fetched.
	if manifest, err := os.ReadFile(f.path(chunksManifestRel)); err == nil {
		if strings.Contains(string(manifest), entry.name) {
			t.Errorf("chunks.json names the inline asset %s; an inline asset must not join the fetched chunk manifest", entry.name)
		}
	}
}

// TestInlineAssetBuildIsReproducible mirrors TestBuildIsReproducible: the
// minified artifact is committed, so a non-deterministic build would make
// every --check run report false staleness.
func TestInlineAssetBuildIsReproducible(t *testing.T) {
	f, entry := inlineAssetFixture(t)
	useInlineAssets(t, entry)

	if err := runTool(t, "-dir", f.dir); err != nil {
		t.Fatalf("first build: %v", err)
	}
	first := f.read(entry.name)

	if err := runTool(t, "-dir", f.dir); err != nil {
		t.Fatalf("second build: %v", err)
	}
	second := f.read(entry.name)

	if !bytes.Equal(first, second) {
		t.Errorf("inline asset changed between two builds of the same sources (%d bytes then %d)", len(first), len(second))
	}
}

// TestInlineAssetCheckCatchesStaleness is the staleness gate gosx#221 asks
// for: --check must fail when a source changes and the committed artifact
// was not rebuilt, and it must clear once the rebuild runs.
func TestInlineAssetCheckCatchesStaleness(t *testing.T) {
	f, entry := inlineAssetFixture(t)
	useInlineAssets(t, entry)

	if err := runTool(t, "-dir", f.dir); err != nil {
		t.Fatalf("build: %v", err)
	}
	if err := runTool(t, "-dir", f.dir, "--check"); err != nil {
		t.Fatalf("--check reports stale right after a build: %v", err)
	}

	// A trailing comment alone would not move the gate: the minifier strips
	// comments, so the minified bytes would not change. Append a real
	// statement instead, the same way a genuine navigation.ts edit would.
	f.appendSource("host-src/consumer.ts", "window.__gosx_fixture_added = true;\n")

	err := runTool(t, "-dir", f.dir, "--check")
	if err == nil {
		t.Fatal("--check passed after an inline asset source changed")
	}
	if !strings.Contains(err.Error(), entry.name) {
		t.Errorf("--check does not name the stale inline asset %s; got: %v", entry.name, err)
	}

	if err := runTool(t, "-dir", f.dir); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if err := runTool(t, "-dir", f.dir, "--check"); err != nil {
		t.Fatalf("--check still reports stale after the rebuild: %v", err)
	}
}

// TestInlineAssetCheckWritesNothing mirrors TestCheckWritesNothing: the gate
// must be read-only even when it fails.
func TestInlineAssetCheckWritesNothing(t *testing.T) {
	f, entry := inlineAssetFixture(t)
	useInlineAssets(t, entry)

	if err := runTool(t, "-dir", f.dir); err != nil {
		t.Fatalf("build: %v", err)
	}
	before := f.read(entry.name)

	f.appendSource("host-src/consumer.ts", "window.__gosx_fixture_added = true;\n")
	if err := runTool(t, "-dir", f.dir, "--check"); err == nil {
		t.Fatal("--check passed after a source change")
	}
	after := f.read(entry.name)
	if !bytes.Equal(before, after) {
		t.Error("--check rewrote the inline asset; the gate must not write")
	}
}

// TestInlineAssetClosureCatchesAFreeIdentifier proves verifyChunkClosure
// covers inlineAssets too: an inline asset is still one script the browser
// evaluates on its own, and a symbol nothing in it declares is exactly the
// latent-crash class the closure check exists to catch (see closure.go).
func TestInlineAssetClosureCatchesAFreeIdentifier(t *testing.T) {
	f := newFixture(t)
	broken := f.writeHostSource("broken.ts", "(function () {\n  window.__gosx_fixture_broken = fixtureUndeclaredHelper();\n})();\n")
	entry := chunk("host-src/broken-runtime.min.js", broken)
	useInlineAssets(t, entry)

	err := verifyChunkClosure(f.dir)
	if err == nil {
		t.Fatal("closure check passed for an inline asset reading an undeclared identifier")
	}
	if !strings.Contains(err.Error(), "fixtureUndeclaredHelper") {
		t.Errorf("closure error does not mention the free identifier; got: %v", err)
	}
}

// TestShippedNavigationRuntimeMatchesItsSources pins the committed
// client/runtime/host/navigation-runtime.min.js: it must equal a fresh build
// from compatibility.ts and navigation.ts, byte for byte. --check covers
// this in CI; this test states it so `go test` alone catches drift too.
func TestShippedNavigationRuntimeMatchesItsSources(t *testing.T) {
	dir := shippedClientJS(t)
	var entry output
	for _, candidate := range inlineAssets {
		if strings.HasSuffix(candidate.name, "navigation-runtime.min.js") {
			entry = candidate
			break
		}
	}
	if entry.name == "" {
		t.Fatal("inlineAssets does not carry a navigation-runtime.min.js entry")
	}

	committedPath := filepath.Join(dir, entry.name)
	committed, err := os.ReadFile(committedPath)
	if err != nil {
		t.Fatalf("read committed %s: %v", entry.name, err)
	}
	fresh, err := buildInlineAsset(dir, entry, "esbuild")
	if err != nil {
		t.Fatalf("rebuild %s: %v", entry.name, err)
	}
	if string(committed) != fresh {
		t.Errorf("committed %s is stale (%d bytes committed, %d bytes fresh); run `go run ./cmd/buildbootstrap`",
			entry.name, len(committed), len(fresh))
	}
}
