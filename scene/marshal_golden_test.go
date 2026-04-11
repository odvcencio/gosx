package scene

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestPropsMarshalJSONGolden captures the exact bytes that Props.MarshalJSON
// currently emits for the shared bench fixture. It's a pinning test so any
// future refactor of the marshal path (e.g., skipping the legacyProps map
// tree in favor of direct MarshalJSON methods on the IR types) has a
// byte-for-byte reference to compare against.
//
// Run with `-update` (via -args) to regenerate the golden file after an
// intentional shape change.
func TestPropsMarshalJSONGolden(t *testing.T) {
	t.Helper()

	props := benchMixedScene()
	got, err := json.Marshal(props)
	if err != nil {
		t.Fatalf("json.Marshal(props) failed: %v", err)
	}

	// Canonicalize via a round-trip through map[string]any + json.Marshal
	// so the golden file is stable across map iteration orders. We store
	// the canonicalized form because Go maps don't guarantee key order in
	// json.Marshal — actually they do since 1.12 (keys are sorted), so a
	// second pass isn't strictly necessary — but it keeps the golden
	// resilient to any map-versus-struct path changes.
	var tree map[string]any
	if err := json.Unmarshal(got, &tree); err != nil {
		t.Fatalf("round-trip unmarshal failed: %v\nraw: %s", err, got)
	}
	canonical, err := json.Marshal(tree)
	if err != nil {
		t.Fatalf("round-trip remarshal failed: %v", err)
	}

	goldenPath := filepath.Join("testdata", "props_marshal_golden.json")
	if _, err := os.Stat(goldenPath); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(goldenPath, canonical, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("created golden: %s (%d bytes)", goldenPath, len(canonical))
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if string(canonical) != string(want) {
		// Write the actual output next to the golden for easy diff.
		actualPath := goldenPath + ".actual"
		_ = os.WriteFile(actualPath, canonical, 0o644)
		t.Fatalf("marshal output drift — diff %s against %s", goldenPath, actualPath)
	}
}
