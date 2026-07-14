package main

import (
	"path/filepath"
	"testing"
)

func TestBuildSizeReportCountsColdStartRuntimeAssets(t *testing.T) {
	dir := t.TempDir()
	runtimeDir := filepath.Join(dir, "assets", "runtime")
	mustWriteFile(t, filepath.Join(runtimeDir, "runtime.hash.wasm"), "wasm")
	mustWriteFile(t, filepath.Join(runtimeDir, "runtime-islands.hash.wasm"), "islands")
	mustWriteFile(t, filepath.Join(runtimeDir, "wasm_exec.hash.js"), "exec")
	mustWriteFile(t, filepath.Join(runtimeDir, "standard-go-wasm_exec.hash.js"), "standard-exec")
	mustWriteFile(t, filepath.Join(runtimeDir, "bootstrap-runtime.hash.js"), "runtime")
	mustWriteFile(t, filepath.Join(runtimeDir, "bootstrap-feature-islands.hash.js"), "feature-islands")
	mustWriteFile(t, filepath.Join(runtimeDir, "bootstrap-feature-engines.hash.js"), "feature-engines")
	mustWriteFile(t, filepath.Join(runtimeDir, "bootstrap-feature-scene3d.hash.js"), "scene")
	mustWriteFile(t, filepath.Join(dir, "build.json"), `{
  "runtime": {
    "wasm": {"file": "runtime.hash.wasm"},
    "wasmIslands": {"file": "runtime-islands.hash.wasm"},
	    "wasmExec": {"file": "wasm_exec.hash.js"},
	    "standardGoWasmExec": {"file": "standard-go-wasm_exec.hash.js"},
    "bootstrapRuntime": {"file": "bootstrap-runtime.hash.js"},
	    "bootstrapFeatureIslands": {"file": "bootstrap-feature-islands.hash.js"},
	    "bootstrapFeatureEngines": {"file": "bootstrap-feature-engines.hash.js"},
    "bootstrapFeatureScene3d": {"file": "bootstrap-feature-scene3d.hash.js"}
  },
  "islands": [],
  "css": []
}`)

	report, err := buildSizeReport(dir)
	if err != nil {
		t.Fatal(err)
	}
	if report.ColdStartBytes != int64(len("wasm")+len("exec")+len("runtime")) {
		t.Fatalf("unexpected cold start bytes: %#v", report)
	}
	if report.TotalBytes != int64(len("wasm")+len("islands")+len("exec")+len("standard-exec")+len("runtime")+len("feature-islands")+len("feature-engines")+len("scene")) {
		t.Fatalf("unexpected total bytes: %#v", report)
	}
	if len(report.Assets) != 8 {
		t.Fatalf("expected eight assets, got %#v", report.Assets)
	}
	profiles := map[string]sizeProfile{}
	for _, profile := range report.Profiles {
		profiles[profile.Name] = profile
	}
	if profiles["full-runtime"].Bytes != int64(len("wasm")+len("exec")+len("runtime")) {
		t.Fatalf("unexpected full-runtime profile: %#v", profiles["full-runtime"])
	}
	if profiles["islands-runtime"].Bytes != int64(len("islands")+len("exec")+len("runtime")+len("feature-islands")) {
		t.Fatalf("unexpected islands-runtime profile: %#v", profiles["islands-runtime"])
	}
	if profiles["go-wasm-engine"].Bytes != int64(len("standard-exec")+len("runtime")+len("feature-engines")) {
		t.Fatalf("unexpected go-wasm-engine profile: %#v", profiles["go-wasm-engine"])
	}
}
