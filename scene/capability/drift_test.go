package capability

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

// pendingManifestCorrections names every cell where Matrix is right, renderer
// source agrees with Matrix, and the shipped manifest still carries the old
// wrong value.
//
// An entry exists only when the Matrix correction and the manifest edit could
// not land in the same commit. The manifests live under client/js, which the
// author of this audit does not own. Keeping the guard live for every OTHER cell
// beats disabling it, and an explicit entry states the debt in code instead of
// in a report nobody reads again.
//
// Each entry pins the STALE value. When the manifest is fixed, the guard fails
// and tells the reader to delete the entry. So the exception cannot outlive the
// bug it covers.
//
// The list is EMPTY, and that is the intended resting state. The line-dashed
// entry retired on 2026-07-26 when the WebGL2 manifest went false, exactly as
// this mechanism is meant to work: the guard failed, named the entry, and the
// entry went away. Keep the map and its test rather than deleting them, so the
// next correction has a place to live for the length of one commit instead of
// silencing the whole guard.
var pendingManifestCorrections = map[Backend]map[Feature]bool{}

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
		if stale, pending := pendingManifestCorrections[BackendWebGL][f]; pending {
			if webglWant != stale {
				failures = append(failures, fmt.Sprintf(
					"feature %q: the WebGL manifest no longer reads %v, so the correction landed — "+
						"delete the pendingManifestCorrections entry in drift_test.go",
					key, stale,
				))
			}
		} else if backends[BackendWebGL] != webglWant {
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
		if stale, pending := pendingManifestCorrections[BackendWebGPU][f]; pending {
			if webgpuWant != stale {
				failures = append(failures, fmt.Sprintf(
					"feature %q: the WebGPU manifest no longer reads %v, so the correction landed — "+
						"delete the pendingManifestCorrections entry in drift_test.go",
					key, stale,
				))
			}
		} else if backends[BackendWebGPU] != webgpuWant {
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

// TestPendingManifestCorrectionsAreRealDebt keeps the exception list from
// becoming a place to hide agreement.
//
// A pending entry suppresses the drift check for one cell. That is only
// defensible when Matrix and the manifest genuinely disagree. An entry whose
// stale value already equals the Matrix cell suppresses a live guard and buys
// nothing, so reject it.
func TestPendingManifestCorrectionsAreRealDebt(t *testing.T) {
	for backend, features := range pendingManifestCorrections {
		for feature, stale := range features {
			row, ok := Matrix[feature]
			if !ok {
				t.Errorf("pendingManifestCorrections names %q, which has no Matrix row", feature)
				continue
			}
			if row[backend] == stale {
				t.Errorf("pendingManifestCorrections[%s][%s] records stale=%v, but Matrix already says %v; "+
					"the entry suppresses a working guard and must be deleted", backend, feature, stale, row[backend])
			}
		}
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
