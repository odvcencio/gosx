package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Authored bootstrap sources are TypeScript. The build compiles them with
// esbuild and emits browser bundles whose names end in .js; the emitted
// artifact extension must never leak back into the chunk table as an authored
// input. This locks in the Scene3D WebGPU migration, which replaced the last
// authored JavaScript source (client/js/bootstrap-src/16a-scene-webgpu.js)
// with client/runtime/scene3d/webgpu.ts: any future edit that re-points a
// sourceFile at a .js path fails here -- both against the in-source chunk
// table (before chunks.json is regenerated) and against the committed
// manifest (after it is). Runtime URLs and emitted filenames keep their .js
// suffix by contract; only authored source-of-truth entries are constrained.
func TestAuthoredChunkSourcesAreTypeScript(t *testing.T) {
	for _, entry := range outputs {
		if !strings.HasSuffix(entry.name, ".js") {
			t.Errorf("chunk %q does not end in .js; emitted bundle names are part of the runtime URL contract", entry.name)
		}
		for _, src := range entry.sources {
			if !strings.HasSuffix(src.rel, ".ts") {
				t.Errorf("chunk %s names authored source %q: authored sources must be TypeScript (.ts), never JavaScript (.js)", entry.name, src.rel)
			}
		}
	}

	dir := shippedClientJS(t)
	committed, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(chunksManifestRel)))
	if err != nil {
		t.Fatalf("read committed chunks.json: %v", err)
	}
	var manifest struct {
		Chunks []struct {
			Name    string   `json:"name"`
			Sources []string `json:"sources"`
		} `json:"chunks"`
	}
	if err := json.Unmarshal(committed, &manifest); err != nil {
		t.Fatalf("parse committed chunks.json: %v", err)
	}
	if len(manifest.Chunks) == 0 {
		t.Fatal("committed chunks.json lists no chunks")
	}
	for _, ch := range manifest.Chunks {
		if !strings.HasSuffix(ch.Name, ".js") {
			t.Errorf("committed chunks.json chunk %q does not end in .js", ch.Name)
		}
		for _, src := range ch.Sources {
			if !strings.HasSuffix(src, ".ts") {
				t.Errorf("committed chunks.json lists authored source %q in chunk %s: authored sources must be TypeScript (.ts)", src, ch.Name)
			}
		}
	}
}
