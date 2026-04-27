package main

import (
	"path/filepath"
	"testing"
)

func TestBuildSizeReportCountsColdStartRuntimeAssets(t *testing.T) {
	dir := t.TempDir()
	runtimeDir := filepath.Join(dir, "assets", "runtime")
	mustWriteFile(t, filepath.Join(runtimeDir, "runtime.hash.wasm"), "wasm")
	mustWriteFile(t, filepath.Join(runtimeDir, "wasm_exec.hash.js"), "exec")
	mustWriteFile(t, filepath.Join(runtimeDir, "bootstrap-runtime.hash.js"), "runtime")
	mustWriteFile(t, filepath.Join(runtimeDir, "bootstrap-feature-scene3d.hash.js"), "scene")
	mustWriteFile(t, filepath.Join(dir, "build.json"), `{
  "runtime": {
    "wasm": {"file": "runtime.hash.wasm"},
    "wasmExec": {"file": "wasm_exec.hash.js"},
    "bootstrapRuntime": {"file": "bootstrap-runtime.hash.js"},
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
	if report.TotalBytes != int64(len("wasm")+len("exec")+len("runtime")+len("scene")) {
		t.Fatalf("unexpected total bytes: %#v", report)
	}
	if len(report.Assets) != 4 {
		t.Fatalf("expected four assets, got %#v", report.Assets)
	}
}
