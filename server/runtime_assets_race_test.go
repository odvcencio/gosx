package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"m31labs.dev/gosx/buildmanifest"
)

// TestRuntimeBuildManifestConcurrentAccessDoesNotRace proves that concurrent
// requests hitting runtimeBuildManifest for the first time on a fresh App do
// not race on the lazy allocation of App.runtimeMeta. Before the
// runtimeMetaInitMu fix, `go test -race` failed here in roughly 2 of 3 runs:
// each goroutine saw a nil cache, allocated its own *runtimeManifestCache,
// and locked a different mutex, so the mutex protected nothing.
func TestRuntimeBuildManifestConcurrentAccessDoesNotRace(t *testing.T) {
	root := t.TempDir()
	manifest := buildmanifest.Manifest{
		Runtime: buildmanifest.RuntimeAssets{
			Bootstrap: buildmanifest.HashedAsset{File: "bootstrap.aaaa.js", Hash: "aaaa", Size: 1},
		},
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "build.json"), data, 0644); err != nil {
		t.Fatalf("write build.json: %v", err)
	}

	app := New()

	const goroutines = 128
	var (
		start   sync.WaitGroup
		release = make(chan struct{})
		done    sync.WaitGroup
		results = make([]*buildmanifest.Manifest, goroutines)
		oks     = make([]bool, goroutines)
	)
	start.Add(goroutines)
	done.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer done.Done()
			start.Done()
			<-release
			m, ok := app.runtimeBuildManifest(root)
			results[idx] = m
			oks[idx] = ok
		}(i)
	}
	start.Wait()
	close(release)
	done.Wait()

	for i := 0; i < goroutines; i++ {
		if !oks[i] {
			t.Fatalf("goroutine %d: runtimeBuildManifest reported not ok", i)
		}
		if results[i] == nil {
			t.Fatalf("goroutine %d: runtimeBuildManifest returned nil manifest", i)
		}
		if results[i] != results[0] {
			t.Fatalf("goroutine %d: got a different cached manifest pointer than goroutine 0", i)
		}
	}
	if app.runtimeMeta == nil {
		t.Fatal("expected App.runtimeMeta to be initialized after concurrent access")
	}
}
