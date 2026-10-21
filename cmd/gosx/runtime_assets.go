package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"m31labs.dev/gosx/buildmanifest"
)

type runtimeJSAssetSpec struct {
	name string
	path string
	dest *HashedAsset
	role string
}

// runtimeJSAssetSpecs is the shared inventory for production and dev staging.
func runtimeJSAssetSpecs(gosxRoot string, assets *RuntimeAssets) []runtimeJSAssetSpec {
	return []runtimeJSAssetSpec{
		{"bootstrap", filepath.Join(gosxRoot, "client", "js", "bootstrap.js"), &assets.Bootstrap, ""},
		{"bootstrap-lite", filepath.Join(gosxRoot, "client", "js", "bootstrap-lite.js"), &assets.BootstrapLite, ""},
		{"bootstrap-runtime", filepath.Join(gosxRoot, "client", "js", "bootstrap-runtime.js"), &assets.BootstrapRuntime, ""},
		{"bootstrap-feature-islands", filepath.Join(gosxRoot, "client", "js", "bootstrap-feature-islands.js"), &assets.BootstrapFeatureIslands, "islands"},
		{"bootstrap-feature-engines", filepath.Join(gosxRoot, "client", "js", "bootstrap-feature-engines.js"), &assets.BootstrapFeatureEngines, "engines"},
		{"bootstrap-feature-hubs", filepath.Join(gosxRoot, "client", "js", "bootstrap-feature-hubs.js"), &assets.BootstrapFeatureHubs, "hubs"},
		{"bootstrap-feature-controllers", filepath.Join(gosxRoot, "client", "js", "bootstrap-feature-controllers.js"), &assets.BootstrapFeatureControllers, "controllers"},
		{"bootstrap-controller-input", filepath.Join(gosxRoot, "client", "js", "bootstrap-controller-input.js"), &assets.BootstrapControllerInput, "controllers"},
		{"bootstrap-feature-textlayout", filepath.Join(gosxRoot, "client", "js", "bootstrap-feature-textlayout.js"), &assets.BootstrapFeatureTextlayout, "textlayout"},
		{"bootstrap-feature-scene3d", filepath.Join(gosxRoot, "client", "js", "bootstrap-feature-scene3d.js"), &assets.BootstrapFeatureScene3D, "scene3d"},
		{"bootstrap-feature-scene3d-command", filepath.Join(gosxRoot, "client", "js", "bootstrap-feature-scene3d-command.js"), &assets.BootstrapFeatureScene3DCommand, "scene3d"},
		{"bootstrap-feature-scene3d-experience", filepath.Join(gosxRoot, "client", "js", "bootstrap-feature-scene3d-experience.js"), &assets.BootstrapFeatureScene3DExperience, "scene3d"},
		{"bootstrap-feature-scene3d-hydrate", filepath.Join(gosxRoot, "client", "js", "bootstrap-feature-scene3d-hydrate.js"), &assets.BootstrapFeatureScene3DHydrate, "scene3d"},
		{"bootstrap-feature-scene3d-webgpu", filepath.Join(gosxRoot, "client", "js", "bootstrap-feature-scene3d-webgpu.js"), &assets.BootstrapFeatureScene3DWebGPU, "scene3d"},
		{"bootstrap-feature-scene3d-webgl", filepath.Join(gosxRoot, "client", "js", "bootstrap-feature-scene3d-webgl.js"), &assets.BootstrapFeatureScene3DWebGL, "scene3d"},
		{"bootstrap-feature-scene3d-gltf", filepath.Join(gosxRoot, "client", "js", "bootstrap-feature-scene3d-gltf.js"), &assets.BootstrapFeatureScene3DGLTF, "scene3d"},
		{"bootstrap-feature-scene3d-animation", filepath.Join(gosxRoot, "client", "js", "bootstrap-feature-scene3d-animation.js"), &assets.BootstrapFeatureScene3DAnimation, "scene3d"},
		{"bootstrap-feature-scene3d-compute", filepath.Join(gosxRoot, "client", "js", "bootstrap-feature-scene3d-compute.js"), &assets.BootstrapFeatureScene3DCompute, "scene3d"},
		{"bootstrap-feature-scene3d-decompress", filepath.Join(gosxRoot, "client", "js", "bootstrap-feature-scene3d-decompress.js"), &assets.BootstrapFeatureScene3DDecompress, "scene3d"},
		{"bootstrap-feature-scene3d-walk", filepath.Join(gosxRoot, "client", "js", "bootstrap-feature-scene3d-walk.js"), &assets.BootstrapFeatureScene3DWalk, "scene3d"},
		{"bootstrap-feature-scene3d-model-preparation", filepath.Join(gosxRoot, "client", "js", "bootstrap-feature-scene3d-model-preparation.js"), &assets.BootstrapFeatureScene3DModelPreparation, "scene3d"},
		{"bootstrap-feature-scene3d-particle-burst", filepath.Join(gosxRoot, "client", "js", "bootstrap-feature-scene3d-particle-burst.js"), &assets.BootstrapFeatureScene3DParticleBurst, "scene3d"},
		{"bootstrap-feature-scene3d-zoom", filepath.Join(gosxRoot, "client", "js", "bootstrap-feature-scene3d-zoom.js"), &assets.BootstrapFeatureScene3DZoom, "scene3d"},
		{"bootstrap-feature-scene3d-timeline", filepath.Join(gosxRoot, "client", "js", "bootstrap-feature-scene3d-timeline.js"), &assets.BootstrapFeatureScene3DTimeline, "scene3d"},
		{"bootstrap-feature-scene3d-vessel", filepath.Join(gosxRoot, "client", "js", "bootstrap-feature-scene3d-vessel.js"), &assets.BootstrapFeatureScene3DVessel, "scene3d"},
		{"bootstrap-feature-scene3d-ocean-query", filepath.Join(gosxRoot, "client", "js", "bootstrap-feature-scene3d-ocean-query.js"), &assets.BootstrapFeatureScene3DOceanQuery, "scene3d"},
		{"bootstrap-feature-scene3d-instance-stream", filepath.Join(gosxRoot, "client", "js", "bootstrap-feature-scene3d-instance-stream.js"), &assets.BootstrapFeatureScene3DInstanceStream, "scene3d"},
		{"patch", filepath.Join(gosxRoot, "client", "js", "patch.js"), &assets.Patch, ""},
		{"hls.min", filepath.Join(gosxRoot, "client", "js", "vendor", "hls.min.js"), &assets.VideoHLS, "video"},
		{"stripe-bridge", filepath.Join(gosxRoot, "client", "js", "stripe-bridge.js"), &assets.StripeBridge, "payments"},
		{"relay", filepath.Join(gosxRoot, "client", "js", "relay.js"), &assets.Relay, "relay"},
	}
}

func devRuntimeAsset(data []byte) HashedAsset {
	sum := sha256.Sum256(data)
	return HashedAsset{Hash: hex.EncodeToString(sum[:]), Size: int64(len(data)), Integrity: "sha256-" + base64.StdEncoding.EncodeToString(sum[:])}
}

func stageDevRuntimeJavaScript(gosxRoot, buildDir string, assets *RuntimeAssets) error {
	for _, js := range runtimeJSAssetSpecs(gosxRoot, assets) {
		data, err := os.ReadFile(js.path)
		if err != nil {
			return fmt.Errorf("read %s: %w", js.name, err)
		}
		data = runtimeJSAssetData(js.name, data)
		if err := os.WriteFile(filepath.Join(buildDir, js.name+".js"), data, 0644); err != nil {
			return fmt.Errorf("stage %s: %w", js.name, err)
		}
		*js.dest = devRuntimeAsset(data)
	}
	assets.Features = make(map[string]HashedAsset, len(runtimeFeatureChunks))
	for _, chunk := range runtimeFeatureChunks {
		name := "bootstrap-feature-" + chunk.name
		data, err := os.ReadFile(filepath.Join(gosxRoot, "client", "js", name+".js"))
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		data = runtimeJSAssetData(name, data)
		if err := os.WriteFile(filepath.Join(buildDir, name+".js"), data, 0644); err != nil {
			return fmt.Errorf("stage %s: %w", name, err)
		}
		assets.Features[chunk.name] = devRuntimeAsset(data)
	}
	return nil
}

// Publish digests only after every staged asset and island program is complete.
// Empty filenames describe stable compatibility URLs rather than deployment assets.
func writeDevRuntimeManifest(buildDir string, assets RuntimeAssets) error {
	for _, entry := range []struct {
		name   string
		target *HashedAsset
	}{
		{"gosx-runtime.wasm", &assets.WASM},
		{"wasm_exec.js", &assets.WASMExec},
		{"standard-go-wasm_exec.js", &assets.StandardGoWASMExec},
	} {
		data, err := os.ReadFile(filepath.Join(buildDir, entry.name))
		if err != nil {
			return fmt.Errorf("read staged %s: %w", entry.name, err)
		}
		*entry.target = devRuntimeAsset(data)
	}
	data, err := json.MarshalIndent(BuildManifest{Runtime: assets}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode dev runtime manifest: %w", err)
	}
	file, err := os.CreateTemp(buildDir, ".gosx-dev-runtime-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if err := file.Chmod(0644); err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), filepath.Join(buildDir, buildmanifest.DevRuntimeManifestFile))
}
