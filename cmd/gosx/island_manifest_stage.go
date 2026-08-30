package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// islandManifestStage runs the real per-island discovery, encode, and
// content-hash-write pipeline RunBuildWithOptions uses to populate
// build.json's Islands and SourceRoot fields, and writes the result to
// dir/dist/build.json.
//
// It is RunBuildWithOptions' island+manifest stage, factored out so a test
// can exercise the real manifest-writing, SourceFile-recording, and
// SourceRoot-recording code (issue #166) without paying for the wasm
// runtime compile RunBuildWithOptions also performs. See
// TestWarnStaleIslandsEndToEndAfterRealBuildPipeline.
func islandManifestStage(dir string, dev bool) (*BuildManifest, string, error) {
	canonicalDir, err := canonicalExistingDir(dir)
	if err != nil {
		return nil, "", fmt.Errorf("resolve project dir: %w", err)
	}
	dir = canonicalDir
	islandProgs, _, err := collectProjectIslandPrograms(dir)
	if err != nil {
		return nil, "", err
	}

	distDir := filepath.Join(dir, "dist")
	islandDir := filepath.Join(distDir, "assets", "islands")
	if err := os.MkdirAll(islandDir, 0755); err != nil {
		return nil, "", fmt.Errorf("create island output directory %s: %w", islandDir, err)
	}

	islandAssets, err := writeIslandManifestAssets(dir, islandDir, dev, islandProgs)
	if err != nil {
		return nil, "", err
	}

	manifest := &BuildManifest{SourceRoot: dir, Islands: islandAssets}
	manifestPath, err := writeBuildManifest(distDir, manifest)
	if err != nil {
		return nil, "", err
	}
	return manifest, manifestPath, nil
}

// writeBuildManifest marshals manifest and writes it to distDir/build.json —
// the same write RunBuildWithOptions performs for the full manifest.
func writeBuildManifest(distDir string, manifest *BuildManifest) (string, error) {
	if err := manifest.ValidateIslandAssets(); err != nil {
		return "", fmt.Errorf("validate manifest: %w", err)
	}
	manifestJSON, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal manifest: %w", err)
	}
	manifestPath := filepath.Join(distDir, "build.json")
	if err := os.WriteFile(manifestPath, manifestJSON, 0644); err != nil {
		return "", fmt.Errorf("write manifest: %w", err)
	}
	return manifestPath, nil
}
