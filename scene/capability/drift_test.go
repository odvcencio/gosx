package capability

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

// TestDriftGuard loads the renderer capability manifests and asserts that
// every cell in Matrix agrees with what the manifests declare. The test fails
// if any renderer gains or loses a feature without a matching Matrix update.
func TestDriftGuard(t *testing.T) {
	webglPath := "../../client/js/bootstrap-src/16-scene-webgl.capabilities.json"
	webgpuPath := "../../client/js/bootstrap-src/16a-scene-webgpu.capabilities.json"

	webglManifest, err := loadManifest(webglPath)
	if err != nil {
		t.Fatalf("failed to load WebGL manifest at %s: %v", webglPath, err)
	}
	webgpuManifest, err := loadManifest(webgpuPath)
	if err != nil {
		t.Fatalf("failed to load WebGPU manifest at %s: %v", webgpuPath, err)
	}

	var failures []string
	for f, backends := range Matrix {
		key := string(f)

		webglWant, inWebGL := webglManifest[key]
		if !inWebGL {
			failures = append(failures, fmt.Sprintf("feature %q missing from WebGL manifest", key))
			continue
		}
		if backends[BackendWebGL] != webglWant {
			failures = append(failures, fmt.Sprintf(
				"feature %q: Matrix[webgl]=%v but manifest says %v — flip the Matrix cell or update the manifest",
				key, backends[BackendWebGL], webglWant,
			))
		}

		webgpuWant, inWebGPU := webgpuManifest[key]
		if !inWebGPU {
			failures = append(failures, fmt.Sprintf("feature %q missing from WebGPU manifest", key))
			continue
		}
		if backends[BackendWebGPU] != webgpuWant {
			failures = append(failures, fmt.Sprintf(
				"feature %q: Matrix[webgpu]=%v but manifest says %v — flip the Matrix cell or update the manifest",
				key, backends[BackendWebGPU], webgpuWant,
			))
		}
	}

	if len(failures) > 0 {
		for _, f := range failures {
			t.Error(f)
		}
		t.Fatalf("drift guard: %d mismatch(es) — update Matrix or the renderer manifests to match", len(failures))
	}
}

func loadManifest(path string) (map[string]bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m map[string]bool
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return m, nil
}
