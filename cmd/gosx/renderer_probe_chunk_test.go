package main

import "testing"

func TestDeferAndProbeChunksAreProductionFeatures(t *testing.T) {
	for name, file := range map[string]string{
		"renderer-probe": "bootstrap-feature-renderer-probe.js",
		"scene3d-defer":  "bootstrap-feature-scene3d-defer.js",
	} {
		found := false
		for _, chunk := range runtimeFeatureChunks {
			if chunk.name != name {
				continue
			}
			found = true
			ok := false
			for _, f := range runtimeExcludableAssetRoles[chunk.role] {
				ok = ok || f == file
			}
			if !ok {
				t.Fatalf("role %q must cover %s: %v", chunk.role, file, runtimeExcludableAssetRoles[chunk.role])
			}
		}
		if !found {
			t.Fatalf("%s is missing from runtimeFeatureChunks, so production builds would not list or serve it", name)
		}
	}
}
