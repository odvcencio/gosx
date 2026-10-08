package budget

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func coefficientEntry(v map[string]any, set, entry int) map[string]any {
	return v["sets"].([]any)[set].(map[string]any)["entries"].([]any)[entry].(map[string]any)
}

func TestCoefficientsPreserveEvidence(t *testing.T) {
	c, err := LoadCoefficients("testdata/coefficients.v1.json", LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Sets) != 2 || c.Sets[0].ID == c.Sets[1].ID {
		t.Fatal("named sets were not retained")
	}
	// These priors reproduce illustrative rows; none is a measured cost.
	for _, entry := range c.Sets[0].Entries {
		if entry.Status != "provisional" || entry.Value == nil {
			t.Fatal("illustrative prior claimed measured evidence")
		}
	}
	// Three-size, ten-visit calibration is a pilot, rounded to us/raw KB.
	pilot := c.Sets[1].Entries[3]
	if pilot.Name != "wasmCompileMicrosPerRawKB" || pilot.Status != "pilot" || *pilot.Value != 2 || *pilot.CI95[1] != 3 {
		t.Fatalf("wrong pilot: %+v", pilot)
	}
	unknown := c.Sets[1].Entries[0]
	data, err := json.Marshal(unknown)
	if err != nil {
		t.Fatal(err)
	}
	if unknown.Value != nil || unknown.CI95[0] != nil || !strings.Contains(string(data), `"value":null`) || !strings.Contains(string(data), `"ci95":[null,null]`) {
		t.Fatalf("unknown became zero: %s", data)
	}
}

func TestCoefficientsRejectInvalidEvidence(t *testing.T) {
	for name, edit := range map[string]func(map[string]any){
		"duplicate-set":         func(v map[string]any) { v["sets"].([]any)[1].(map[string]any)["id"] = "illustrative-cold" },
		"duplicate-name":        func(v map[string]any) { coefficientEntry(v, 0, 1)["name"] = "serverMicros" },
		"unknown-name":          func(v map[string]any) { coefficientEntry(v, 0, 1)["name"] = "renderMillis" },
		"unknown-field":         func(v map[string]any) { coefficientEntry(v, 0, 1)["unit"] = "ms" },
		"bad-backend":           func(v map[string]any) { v["sets"].([]any)[0].(map[string]any)["backend"] = "adapter" },
		"bad-scenario":          func(v map[string]any) { v["sets"].([]any)[0].(map[string]any)["scenario"] = "cold" },
		"missing-name":          func(v map[string]any) { delete(coefficientEntry(v, 0, 1), "name") },
		"missing-value":         func(v map[string]any) { delete(coefficientEntry(v, 1, 1), "value") },
		"negative-cost":         func(v map[string]any) { coefficientEntry(v, 0, 1)["value"] = -1 },
		"fractional-cost":       func(v map[string]any) { coefficientEntry(v, 0, 1)["value"] = 0.5 },
		"unknown-as-zero":       func(v map[string]any) { coefficientEntry(v, 1, 0)["value"] = 0 },
		"unknown-with-support":  func(v map[string]any) { coefficientEntry(v, 1, 0)["nVisits"] = 1 },
		"unknown-with-interval": func(v map[string]any) { coefficientEntry(v, 1, 0)["ci95"] = []any{0, 1} },
		"half-interval":         func(v map[string]any) { coefficientEntry(v, 0, 0)["ci95"] = []any{0, nil} },
		"reversed-interval":     func(v map[string]any) { coefficientEntry(v, 0, 0)["ci95"] = []any{300000, 100000} },
		"outside-interval":      func(v map[string]any) { coefficientEntry(v, 0, 0)["ci95"] = []any{1, 2} },
		"short-interval":        func(v map[string]any) { coefficientEntry(v, 0, 0)["ci95"] = []any{0} },
		"bad-overlap":           func(v map[string]any) { coefficientEntry(v, 0, 4)["value"] = 1000001 },
		"bad-overlap-interval":  func(v map[string]any) { coefficientEntry(v, 0, 4)["ci95"] = []any{0, 1000001} },
		"null-prior":            func(v map[string]any) { coefficientEntry(v, 0, 0)["value"] = nil },
		"pilot-no-interval":     func(v map[string]any) { coefficientEntry(v, 1, 3)["ci95"] = []any{nil, nil} },
		"pilot-no-block":        func(v map[string]any) { coefficientEntry(v, 1, 3)["nBlocks"] = 0 },
		"too-many-blocks":       func(v map[string]any) { coefficientEntry(v, 1, 3)["nBlocks"] = 11 },
		"unsupported-measured":  func(v map[string]any) { coefficientEntry(v, 1, 3)["status"] = "measured" },
		"measured-no-holdout":   func(v map[string]any) { e := coefficientEntry(v, 1, 3); e["status"] = "measured"; e["nVisits"] = 30 },
		"unused-nonzero":        func(v map[string]any) { e := coefficientEntry(v, 0, 0); e["status"] = "unused"; e["method"] = "unused" },
		"bad-profile-hash":      func(v map[string]any) { v["profileSHA256"] = "not-a-sha" },
		"bad-time":              func(v map[string]any) { v["measuredAt"] = "2026-02-30T00:00:00Z" },
		"too-few-entries": func(v map[string]any) {
			s := v["sets"].([]any)[0].(map[string]any)
			s["entries"] = s["entries"].([]any)[:10]
		},
	} {
		t.Run(name, func(t *testing.T) {
			path := changedInput(t, "coefficients", edit)
			if _, err := LoadCoefficients(path, LoadOptions{RootDir: filepath.Dir(path)}); err == nil {
				t.Fatal("invalid coefficient evidence accepted")
			}
		})
	}
}

func TestCoefficientsMeasuredThresholdAndSeparateSets(t *testing.T) {
	for _, predictionError := range []int{200000, 200001} {
		path := changedInput(t, "coefficients", func(v map[string]any) {
			s := v["sets"].([]any)[1].(map[string]any)
			s["predictionErrorPPM"] = predictionError
			e := coefficientEntry(v, 1, 3)
			e["status"] = "measured"
			e["nVisits"] = 30
			e["nBlocks"] = 2
		})
		_, err := LoadCoefficients(path, LoadOptions{RootDir: filepath.Dir(path)})
		if (err == nil) != (predictionError == 200000) {
			t.Fatalf("wrong held-out boundary %d: %v", predictionError, err)
		}
	}
	path := changedInput(t, "coefficients", func(v map[string]any) {
		s := v["sets"].([]any)[1].(map[string]any)
		s["backend"] = "webgpu"
		s["scenario"] = "hard-warm"
		e := coefficientEntry(v, 0, 4)
		e["status"] = "unused"
		e["method"] = "unused"
	})
	c, err := LoadCoefficients(path, LoadOptions{RootDir: filepath.Dir(path)})
	if err != nil {
		t.Fatal(err)
	}
	if c.Sets[1].Backend != "webgpu" || c.Sets[1].Scenario != "hard-warm" || c.Sets[0].Entries[4].Status != "unused" {
		t.Fatal("set context or explicit unused cost changed")
	}
}
