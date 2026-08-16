package server

import (
	"bytes"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx/buildmanifest"
)

// TestWarnStaleIslandsLogsMismatchOnce reproduces issue #166: a build.json
// entry recorded SourceHash against the island's .gsx source, the source
// then changed (island edited, only the Go binary rebuilt), and
// warnStaleIslands must log exactly one warning naming the island — even
// across repeated calls, since staleIslandsOnce guards the check.
func TestWarnStaleIslandsLogsMismatchOnce(t *testing.T) {
	root := t.TempDir()
	sourceRel := "app/counter.gsx"
	sourcePath := filepath.Join(root, filepath.FromSlash(sourceRel))
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	original := []byte("package app\n\n//gosx:island\nfunc Counter() Node { return <div>0</div> }\n")
	if err := os.WriteFile(sourcePath, original, 0644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	manifest := buildmanifest.Manifest{Islands: []buildmanifest.IslandAsset{
		{
			Name:        "Counter",
			Format:      "bin",
			HashedAsset: buildmanifest.HashedAsset{File: "Counter.aaaaaaaa.gxi", Hash: "aaaaaaaa", Size: 1},
			SourceFile:  sourceRel,
			SourceHash:  buildmanifest.ContentHash(original),
		},
	}}
	writeManifest(t, root, manifest)

	// Mutate the source AFTER the manifest recorded its hash, mirroring
	// editing an island and rebuilding only the Go binary.
	changed := []byte("package app\n\n//gosx:island\nfunc Counter() Node { return <div>1</div> }\n")
	if err := os.WriteFile(sourcePath, changed, 0644); err != nil {
		t.Fatalf("rewrite source: %v", err)
	}

	app := New()
	app.SetRuntimeRoot(root)

	buf := captureLogOutput(t)

	app.warnStaleIslands()
	app.warnStaleIslands() // second call must not log again

	want := "gosx islands: Counter program in dist is stale (source changed since gosx build); run gosx build"
	got := buf.String()
	if strings.Count(got, want) != 1 {
		t.Fatalf("warnStaleIslands log output = %q, want exactly one occurrence of %q", got, want)
	}
}

// TestWarnStaleIslandsSilentWhenSourceUnchanged asserts the happy path: a
// manifest whose SourceHash still matches the source on disk logs nothing.
func TestWarnStaleIslandsSilentWhenSourceUnchanged(t *testing.T) {
	root := t.TempDir()
	sourcePath := filepath.Join(root, "counter.gsx")
	original := []byte("package app\n\n//gosx:island\nfunc Counter() Node { return <div>0</div> }\n")
	if err := os.WriteFile(sourcePath, original, 0644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	manifest := buildmanifest.Manifest{Islands: []buildmanifest.IslandAsset{
		{
			Name:        "Counter",
			Format:      "bin",
			HashedAsset: buildmanifest.HashedAsset{File: "Counter.aaaaaaaa.gxi", Hash: "aaaaaaaa", Size: 1},
			SourceFile:  "counter.gsx",
			SourceHash:  buildmanifest.ContentHash(original),
		},
	}}
	writeManifest(t, root, manifest)

	app := New()
	app.SetRuntimeRoot(root)

	buf := captureLogOutput(t)
	app.warnStaleIslands()

	if buf.Len() != 0 {
		t.Fatalf("unchanged island logged unexpectedly: %q", buf.String())
	}
}

// TestWarnStaleIslandsSilentOnBackCompatManifest asserts a manifest written
// before issue #166 (no SourceFile/SourceHash on its island assets) never
// logs — there is nothing recorded to compare the current source against.
func TestWarnStaleIslandsSilentOnBackCompatManifest(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "counter.gsx"), []byte("package app\n"), 0644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	manifest := buildmanifest.Manifest{Islands: []buildmanifest.IslandAsset{
		{Name: "Counter", Format: "bin", HashedAsset: buildmanifest.HashedAsset{File: "Counter.aaaaaaaa.gxi", Hash: "aaaaaaaa", Size: 1}},
	}}
	writeManifest(t, root, manifest)

	app := New()
	app.SetRuntimeRoot(root)

	buf := captureLogOutput(t)
	app.warnStaleIslands()

	if buf.Len() != 0 {
		t.Fatalf("back-compat manifest logged unexpectedly: %q", buf.String())
	}
}

// TestWarnStaleIslandsNoOpWithoutManifest asserts the zero-manifest case
// (e.g. `go run` in dev before the first `gosx build`) never logs and never
// panics.
func TestWarnStaleIslandsNoOpWithoutManifest(t *testing.T) {
	app := New()
	app.SetRuntimeRoot(t.TempDir())

	buf := captureLogOutput(t)
	app.warnStaleIslands()

	if buf.Len() != 0 {
		t.Fatalf("no manifest present but warnStaleIslands logged: %q", buf.String())
	}
}

func writeManifest(t *testing.T, root string, manifest buildmanifest.Manifest) {
	t.Helper()
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "build.json"), data, 0644); err != nil {
		t.Fatalf("write build.json: %v", err)
	}
}

// captureLogOutput redirects the standard library log package to a buffer
// for the duration of the test and restores the previous output/flags on
// cleanup.
func captureLogOutput(t *testing.T) *bytes.Buffer {
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
