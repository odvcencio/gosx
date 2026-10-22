package main

import "testing"

func TestRendererProbeChunkIsAProductionFeature(t *testing.T) {
	found := false
	for _, chunk := range runtimeFeatureChunks {
		if chunk.name == "renderer-probe" {
			found = true
			if files := runtimeExcludableAssetRoles[chunk.role]; len(files) != 1 || files[0] != "bootstrap-feature-renderer-probe.js" {
				t.Fatalf("role %q must cover exactly the probe chunk: %v", chunk.role, files)
			}
		}
	}
	if !found {
		t.Fatal("renderer-probe is missing from runtimeFeatureChunks, so production builds would not list or serve it")
	}
}
