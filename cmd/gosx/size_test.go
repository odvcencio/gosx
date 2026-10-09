package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"m31labs.dev/gosx/buildmanifest"
	runtimehost "m31labs.dev/gosx/client/runtime/host"
	"m31labs.dev/gosx/internal/assetmeasure"
	"testing"
)

func TestSizeReportCountsColdStartRuntimeAssets(t *testing.T) {
	sizeTestCompressor(t)
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
	if report.TotalBytes != int64(len("wasm")+len("islands")+len("exec")+len("standard-exec")+len("runtime")+len("feature-islands")+len("feature-engines")+len("scene")+len(runtimehost.NavigationRuntime)) {
		t.Fatalf("unexpected total bytes: %#v", report)
	}
	if len(report.Assets) != 9 {
		t.Fatalf("expected nine assets, got %#v", report.Assets)
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

// Test binaries omit dependency build metadata. Test the reporting boundary
// with canonical compressors; assetmeasure separately verifies build identity.
func sizeTestCompressor(t *testing.T) {
	t.Helper()
	previous := measureSizeAsset
	t.Cleanup(func() { measureSizeAsset = previous })
	measureSizeAsset = func(data []byte, pin assetmeasure.CompressorPin) (assetmeasure.Sizes, error) {
		if pin != (assetmeasure.CompressorPin{GoVersion: "1.26.0", BrotliVersion: "v1.2.1", GzipLevel: 9, BrotliQuality: 11}) {
			return assetmeasure.Sizes{}, errors.New("wrong compressor pin")
		}
		digest := sha256.Sum256(data)
		return assetmeasure.Sizes{Raw: int64(len(data)), Gzip: gzipLength(data), Brotli: brotliLength(data), SHA256: hex.EncodeToString(digest[:])}, nil
	}
}

func TestSizeCanonicalBytesAndServedSidecars(t *testing.T) {
	sizeTestCompressor(t)
	raw := bytes.Repeat([]byte("function counter(){return 123456789;}\n"), 40)
	// Zopfli with five iterations: the release representation is smaller.
	sidecar, err := hex.DecodeString("1f8b08000000000002034b2bcd4b2ec9cccf5348ce2fcd2b492dd2d0ac2e4a2d292dca5330343236313533b7b0b4aee51aeaaa46558daa1a5535aa6a541500e656f91bf0050000")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	file := "app.hash.js"
	mustWriteFile(t, filepath.Join(dir, file), string(raw))
	if err := os.WriteFile(filepath.Join(dir, file+".gz"), sidecar, 0600); err != nil {
		t.Fatal(err)
	}
	entry, err := sizeReportEntry(dir, runtimeSizeAsset{name: "app.js", file: file})
	if err != nil {
		t.Fatal(err)
	}
	if entry.GzipBytes != 75 || entry.GzipWireBytes == nil || *entry.GzipWireBytes != 71 || entry.BrotliWireBytes != nil {
		t.Fatal("release bytes replaced canonical bytes", entry)
	}
	body, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(body, []byte("\"gzipWireBytes\":71")) || bytes.Contains(body, []byte("brotliWireBytes")) {
		t.Fatal("missing sidecar became zero")
	}
	mustWriteFile(t, filepath.Join(dir, file), strings.Repeat("x", len(raw)))
	if _, err := sizeReportEntry(dir, runtimeSizeAsset{file: file}); err == nil {
		t.Fatal("stale sidecar accepted")
	}
}

func TestSizeAllCapabilityVariantsAndRecovery(t *testing.T) {
	sizeTestCompressor(t)
	dir := t.TempDir()
	runtimeDir := filepath.Join(dir, "assets", "runtime")
	manifest := buildmanifest.Manifest{}
	manifest.Runtime.WASM.File = "runtime.hash.wasm"
	manifest.Runtime.WASMExec.File = "exec.hash.js"
	manifest.Runtime.BootstrapRuntime.File = "bootstrap.hash.js"
	manifest.Runtime.BootstrapFeatureScene3DPipelineRecovery.File = "recovery.hash.js"
	manifest.Runtime.WASMVariants = map[string]buildmanifest.RuntimeVariantAsset{}
	for _, id := range []string{"full", "core", "engine", "collab"} {
		name := id + ".hash.wasm"
		if id == "full" {
			name = manifest.Runtime.WASM.File
		}
		variant := buildmanifest.RuntimeVariantAsset{}
		variant.File = name
		manifest.Runtime.WASMVariants[id] = variant
		mustWriteFile(t, filepath.Join(runtimeDir, name), "wasm")
	}
	for _, name := range []string{"exec.hash.js", "bootstrap.hash.js", "recovery.hash.js"} {
		mustWriteFile(t, filepath.Join(runtimeDir, name), "body")
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(dir, "build.json"), string(data))
	report, err := buildSizeReport(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	files := map[string]bool{}
	var total int64
	for _, entry := range report.Assets {
		names[entry.Name] = true
		if !files[entry.File] {
			files[entry.File] = true
			total += entry.Bytes
		}
	}
	for _, name := range []string{"navigation.js", "runtime-variant/full", "runtime-variant/core", "runtime-variant/engine", "runtime-variant/collab", "bootstrap-feature-scene3d-pipeline-recovery.js"} {
		if !names[name] {
			t.Fatal("missing runtime asset", name)
		}
	}
	// Four distinct WASM files and three JS files, plus navigation; full is an alias.
	if report.TotalBytes != total || total != 28+int64(len(runtimehost.NavigationRuntime)) {
		t.Fatal("capability aliases counted twice", report.TotalBytes, total)
	}
	profiles := map[string]bool{}
	for _, profile := range report.Profiles {
		profiles[profile.Name] = true
	}
	for _, name := range []string{"navigation", "capability-collab", "capability-core", "capability-engine", "capability-full", "full-runtime"} {
		if !profiles[name] {
			t.Fatal("missing profile", name)
		}
	}
}

func TestSizeRejectsConfinedPathEscapes(t *testing.T) {
	sizeTestCompressor(t)
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "asset.js"), "body")
	for _, file := range []string{"../asset.js", "nested/asset.js", "/asset.js", "C:/asset.js", "."} {
		if _, err := sizeReportEntry(dir, runtimeSizeAsset{file: file}); err == nil {
			t.Fatal("unconfined runtime path accepted")
		}
	}
	outside := filepath.Join(t.TempDir(), "outside.js")
	mustWriteFile(t, outside, "body")
	for _, suffix := range []string{"", ".gz", ".br"} {
		file := "linked.js"
		if suffix != "" {
			file = "asset.js"
		}
		if err := os.Symlink(outside, filepath.Join(dir, file+suffix)); err != nil {
			t.Fatal(err)
		}
		if _, err := sizeReportEntry(dir, runtimeSizeAsset{file: file}); err == nil {
			t.Fatal("unconfined runtime symlink accepted")
		}
		if err := os.Remove(filepath.Join(dir, file+suffix)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSizeRejectsCanonicalMeasurementFailure(t *testing.T) {
	previous := measureSizeAsset
	t.Cleanup(func() { measureSizeAsset = previous })
	measureSizeAsset = func([]byte, assetmeasure.CompressorPin) (assetmeasure.Sizes, error) {
		return assetmeasure.Sizes{}, errors.New("unavailable build identity")
	}
	if _, err := sizeReportEntry("", runtimeSizeAsset{file: "asset.js", embedded: []byte("body")}); err == nil {
		t.Fatal("canonical failure ignored")
	}
}
