package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

func TestWebGPUPointSpritePerPointVaryingsStayFlat(t *testing.T) {
	clientJS := shippedClientJS(t)
	sourcePath := filepath.Clean(filepath.Join(clientJS, "..", "runtime", "scene3d", "webgpu.ts"))
	bundlePath := filepath.Join(clientJS, "bootstrap-feature-scene3d-webgpu.js")
	monolithPath := filepath.Join(clientJS, "bootstrap.js")

	for _, path := range []string{sourcePath, bundlePath, monolithPath} {
		t.Run(filepath.Base(path), func(t *testing.T) {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read WebGPU runtime source: %v", err)
			}
			src := string(raw)
			for _, want := range []string{
				"@location(0) @interpolate(flat) color: vec3f",
				"@location(1) @interpolate(flat) fogFactor: f32",
				"@location(2) @interpolate(flat) alpha: f32",
				"@location(4) @interpolate(flat) pointSize: f32",
			} {
				if !strings.Contains(src, want) {
					t.Errorf("%s missing %q", path, want)
				}
			}
			for _, forbidden := range []string{
				"@location(0) color: vec3f",
				"@location(1) fogFactor: f32",
				"@location(2) alpha: f32",
				"@location(4) pointSize: f32",
				"@location(3) @interpolate(flat) pointCoord: vec2f",
			} {
				if strings.Contains(src, forbidden) {
					t.Errorf("%s contains forbidden point-sprite interpolation form %q", path, forbidden)
				}
			}
		})
	}
}
