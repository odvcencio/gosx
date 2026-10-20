package main

import (
	"bytes"
	"log"
	"strings"
	"testing"

	"m31labs.dev/gosx/server"
)

// TestWarnStaleIslandsEndToEndAfterRealBuildPipeline runs GoSX's real
// island-discovery and manifest-writing pipeline (islandManifestStage —
// RunBuildWithOptions' own island+manifest stage, minus the wasm runtime
// compile) against a temp project, mutates the island's .gsx source at the
// project root after the build (mirroring issue #166: editing an island and
// rebuilding only the Go binary, never `gosx build` again), points a
// server.App at the project root exactly as issue #166's repro does (server
// root == project root, with dist/build.json nested underneath — not a
// synthetic layout where build.json and sources sit in the same directory),
// and asserts the staleness warning fires through the server's public
// Build() entry point.
func TestWarnStaleIslandsEndToEndAfterRealBuildPipeline(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "counter.gsx", `package main

//gosx:island
func Counter() Node {
	count := signal.New(0)
	increment := func() { count.Set(count.Get() + 1) }
	return <div><span>{count.Get()}</span><button onClick={increment}>+</button></div>
}
`)

	if _, _, err := islandManifestStage(dir, false); err != nil {
		t.Fatalf("islandManifestStage: %v", err)
	}

	// Mirror issue #166: edit the island and rebuild only the Go binary —
	// never run `gosx build` again — so dist/build.json's SourceHash still
	// reflects the original source.
	writeTempFile(t, dir, "counter.gsx", `package main

//gosx:island
func Counter() Node {
	count := signal.New(0)
	increment := func() { count.Set(count.Get() + 2) }
	return <div><span>{count.Get()}</span><button onClick={increment}>+</button></div>
}
`)

	app := server.New()
	app.SetRuntimeRoot(dir) // project root, not dist — issue #166's layout

	buf := captureBuildLogOutput(t)
	app.Build()

	want := "gosx islands: Counter's source file changed since gosx build (counter.gsx); run gosx build"
	if got := buf.String(); strings.Count(got, want) != 1 {
		t.Fatalf("warnStaleIslands log output = %q, want exactly one occurrence of %q", got, want)
	}
}

// TestWarnStaleIslandsEndToEndSilentWhenUnchanged is the inverse of
// TestWarnStaleIslandsEndToEndAfterRealBuildPipeline: the same real build
// pipeline and project-root/dist/build.json layout, but the island source
// never changed after the build, so nothing should log.
func TestWarnStaleIslandsEndToEndSilentWhenUnchanged(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "counter.gsx", `package main

//gosx:island
func Counter() Node {
	count := signal.New(0)
	increment := func() { count.Set(count.Get() + 1) }
	return <div><span>{count.Get()}</span><button onClick={increment}>+</button></div>
}
`)

	if _, _, err := islandManifestStage(dir, false); err != nil {
		t.Fatalf("islandManifestStage: %v", err)
	}

	app := server.New()
	app.SetRuntimeRoot(dir)

	buf := captureBuildLogOutput(t)
	app.Build()

	if buf.Len() != 0 {
		t.Fatalf("unchanged island logged unexpectedly: %q", buf.String())
	}
}

// captureBuildLogOutput redirects the standard library log package to a
// buffer for the duration of the test and restores the previous output and
// flags on cleanup. warnStaleIslands (invoked indirectly through the
// exported server.App.Build(), since it is unexported to package server)
// reports through the standard log package.
func captureBuildLogOutput(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prevOutput := log.Writer()
	prevFlags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(prevOutput)
		log.SetFlags(prevFlags)
	})
	return &buf
}
