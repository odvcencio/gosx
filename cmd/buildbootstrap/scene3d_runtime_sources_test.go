package main

import "testing"

func TestScene3DBundlesUseTypedRuntimeAuthorities(t *testing.T) {
	want := map[string]bool{
		"../runtime/scene3d/animation.ts":       false,
		"../runtime/scene3d/command-bridge.ts":  false,
		"../runtime/scene3d/command-runtime.ts": false,
		"../runtime/scene3d/compute.ts":         false,
		"../runtime/scene3d/dom-regions.ts":     false,
		"../runtime/scene3d/gltf.ts":            false,
		"../runtime/scene3d/hydrate-input.ts":   false,
		"../runtime/scene3d/mount-backend.ts":   false,
		"../runtime/scene3d/mount-controls.ts":  false,
		"../runtime/scene3d/mount-quality.ts":   false,
		"../runtime/scene3d/mount-telemetry.ts": false,
		"../runtime/scene3d/mount-viewport.ts":  false,
		"../runtime/scene3d/mount-webgl.ts":     false,
		"../runtime/scene3d/mount.ts":           false,
		"../runtime/scene3d/overlay-dom.ts":     false,
		"../runtime/scene3d/overlays.ts":        false,
		"../runtime/scene3d/webgl.ts":           false,
		"../runtime/scene3d/webgpu.ts":          false,
	}
	legacy := map[string]bool{
		"bootstrap-src/19a-scene-animation.js":         true,
		"bootstrap-src/09-scene3d-command-bridge.js":   true,
		"bootstrap-src/09a-scene3d-command-runtime.js": true,
		"bootstrap-src/16b-scene-compute.js":           true,
		"bootstrap-src/15d-scene-dom-regions.js":       true,
		"bootstrap-src/19-scene-gltf.js":               true,
		"bootstrap-src/20a-scene-mount-backend.js":     true,
		"bootstrap-src/20g-scene-mount-controls.js":    true,
		"bootstrap-src/20c-scene-mount-quality.js":     true,
		"bootstrap-src/20h-scene-mount-telemetry.js":   true,
		"bootstrap-src/20e-scene-mount-viewport.js":    true,
		"bootstrap-src/20b-scene-mount-webgl-chunk.js": true,
		"bootstrap-src/20-scene-mount.js":              true,
		"bootstrap-src/20f-scene-mount-overlay-dom.js": true,
		"bootstrap-src/20d-scene-mount-overlays.js":    true,
		"bootstrap-src/16-scene-webgl.js":              true,
		"bootstrap-src/16a-scene-webgpu.js":            true,
	}

	for _, out := range outputs {
		for _, src := range out.sources {
			if _, ok := want[src.rel]; ok {
				want[src.rel] = true
			}
			if legacy[src.rel] {
				t.Errorf("%s still references deleted Scene3D source %s", out.name, src.rel)
			}
		}
	}
	for source, found := range want {
		if !found {
			t.Errorf("typed Scene3D authority %s is absent from every bundle", source)
		}
	}
}
