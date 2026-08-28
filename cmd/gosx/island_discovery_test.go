package main

import (
	"path/filepath"
	"testing"

	"m31labs.dev/gosx/buildmanifest"
)

// TestCollectProjectIslandProgramsSourceHashDetectsSourceChange exercises the
// same discovery function RunBuildWithOptions calls to populate
// IslandAsset.SourceFile/SourceHash (issue #166). It builds a manifest from
// the real compiler pipeline, confirms the staleness check reports nothing
// for unchanged source, mutates the island's .gsx file, and confirms the
// staleness check now reports it — plus a back-compat manifest entry that
// predates the field, which must never be reported.
func TestCollectProjectIslandProgramsSourceHashDetectsSourceChange(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "counter.gsx", `package main

//gosx:island
func Counter() Node {
	count := signal.New(0)
	increment := func() { count.Set(count.Get() + 1) }
	return <div><span>{count.Get()}</span><button onClick={increment}>+</button></div>
}
`)

	programs, _, err := collectProjectIslandPrograms(dir)
	if err != nil {
		t.Fatalf("collectProjectIslandPrograms: %v", err)
	}
	if len(programs) != 1 || programs[0].Name != "Counter" {
		t.Fatalf("collectProjectIslandPrograms = %v, want one Counter island", programs)
	}
	prog := programs[0]
	if prog.SourceFile != filepath.Join(dir, "counter.gsx") {
		t.Fatalf("SourceFile = %q, want %q", prog.SourceFile, filepath.Join(dir, "counter.gsx"))
	}
	if prog.SourceHash == "" {
		t.Fatal("SourceHash is empty")
	}

	rel, err := filepath.Rel(dir, prog.SourceFile)
	if err != nil {
		t.Fatalf("relativize source path: %v", err)
	}
	manifest := &buildmanifest.Manifest{Islands: []buildmanifest.IslandAsset{
		{
			Name:        prog.Name,
			Format:      "bin",
			HashedAsset: buildmanifest.HashedAsset{File: "Counter.aaaaaaaa.gxi", Hash: "aaaaaaaa", Size: 1},
			SourceFile:  filepath.ToSlash(rel),
			SourceHash:  prog.SourceHash,
		},
		// A pre-#166 manifest entry: no SourceFile/SourceHash. Must never be
		// reported, regardless of what its dist program hash claims.
		{
			Name:        "LegacyIsland",
			Format:      "bin",
			HashedAsset: buildmanifest.HashedAsset{File: "LegacyIsland.bbbbbbbb.gxi", Hash: "bbbbbbbb", Size: 1},
		},
	}}

	if stale := manifest.StaleIslands(dir); len(stale) != 0 {
		t.Fatalf("unchanged island reported stale: %v", stale)
	}

	// Re-run the pipeline after the source changes, mirroring what happens
	// between `gosx build` and a later `go run`: only the .gsx source
	// mutates, the manifest on disk (and thus SourceHash) does not.
	writeTempFile(t, dir, "counter.gsx", `package main

//gosx:island
func Counter() Node {
	count := signal.New(0)
	increment := func() { count.Set(count.Get() + 2) }
	return <div><span>{count.Get()}</span><button onClick={increment}>+</button></div>
}
`)

	stale := manifest.StaleIslands(dir)
	if len(stale) != 1 || stale[0].Name != "Counter" {
		t.Fatalf("changed island stale report = %+v, want [Counter]", stale)
	}
}
