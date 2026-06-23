package cert

import (
	"encoding/json"
	"testing"
)

func TestMatrixValidatesCertificationContract(t *testing.T) {
	entries := Matrix()
	if len(entries) == 0 {
		t.Fatal("matrix should not be empty")
	}
	if problems := Validate(entries); len(problems) > 0 {
		t.Fatalf("matrix validation problems: %+v", problems)
	}
}

func TestStrictGateAcceptsCurrentMinimumFloor(t *testing.T) {
	failures := StrictFailures(Matrix())
	if len(failures) > 0 {
		t.Fatalf("strict failures: %+v", failures)
	}
}

func TestMarshalJSONIncludesSummary(t *testing.T) {
	data, err := MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var report Report
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("decode report: %v\n%s", err, string(data))
	}
	if report.Schema != Schema {
		t.Fatalf("schema = %q, want %q", report.Schema, Schema)
	}
	if report.Summary.Features != len(report.Entries) {
		t.Fatalf("summary features = %d, entries = %d", report.Summary.Features, len(report.Entries))
	}
}

func TestStrictGateRejectsPrimitiveWebGPUGap(t *testing.T) {
	entries := Matrix()
	for i := range entries {
		if entries[i].Feature == "torus" {
			entries[i].Dimensions[WebGPU] = Partial
			break
		}
	}
	failures := StrictFailures(entries)
	if len(failures) == 0 {
		t.Fatal("expected strict failure")
	}
	found := false
	for _, failure := range failures {
		if failure.Feature == "torus" && failure.Dimension == WebGPU {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected torus WebGPU failure, got %+v", failures)
	}
}

func TestAllDimensionsContainsMotion(t *testing.T) {
	found := false
	for _, d := range AllDimensions {
		if d == Motion {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("AllDimensions must contain Motion dimension")
	}
}

func TestMotionStatusOnAnimationFeatures(t *testing.T) {
	// The features that carry animation data must carry a non-unsupported
	// Motion status now that the Phase 1 evaluator contract is proven.
	animationFeatures := []string{"skinned mesh", "GLB model", "GLB", "glTF"}
	entries := Matrix()
	byFeature := make(map[string]Entry, len(entries))
	for _, e := range entries {
		byFeature[e.Feature] = e
	}
	for _, name := range animationFeatures {
		entry, ok := byFeature[name]
		if !ok {
			t.Errorf("animation feature %q not found in matrix", name)
			continue
		}
		status := entry.Dimensions[Motion]
		if status == Unsupported || status == "" {
			t.Errorf("feature %q has Motion=%q; want partial or complete", name, status)
		}
	}
}

func TestStrictGateRejectsSkinnedMeshMotionUnsupported(t *testing.T) {
	entries := Matrix()
	for i := range entries {
		if entries[i].Feature == "skinned mesh" {
			entries[i].Dimensions[Motion] = Unsupported
			entries[i].Reasons[Motion] = "forced unsupported for test"
			entries[i].NextActions[Motion] = "n/a"
			break
		}
	}
	failures := StrictFailures(entries)
	if len(failures) == 0 {
		t.Fatal("expected strict failure when skinned mesh Motion is unsupported")
	}
	found := false
	for _, f := range failures {
		if f.Feature == "skinned mesh" && f.Dimension == Motion && f.Status == Unsupported {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected skinned mesh Motion=unsupported failure, got %+v", failures)
	}
}
