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
