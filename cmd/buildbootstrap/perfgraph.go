package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"m31labs.dev/gosx/buildmanifest"
	runtimehost "m31labs.dev/gosx/client/runtime/host"
	"m31labs.dev/gosx/internal/assetmeasure"
)

// perfGraph inventories complete outputs from the existing bundle table.
// chunks.json remains source provenance; minification/compression prevent
// additive byte attribution to individual source files.
func perfGraph(dir string) (*buildmanifest.PerfAssetUses, error) {
	dir = filepath.Clean(dir)
	root, err := os.OpenRoot(filepath.Dir(dir))
	if err != nil {
		return nil, perfGraphError("asset-unavailable")
	}
	defer root.Close()
	graph := &buildmanifest.PerfAssetUses{Version: 1, Assets: []buildmanifest.PerfAssetUse{}}
	read := func(path string) ([]byte, error) {
		f, err := root.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() > 64<<20 {
			return nil, perfGraphError("invalid-input")
		}
		body, err := io.ReadAll(io.LimitReader(f, (64<<20)+1))
		if err != nil || len(body) > 64<<20 {
			return nil, perfGraphError("invalid-input")
		}
		return body, nil
	}
	add := func(name, file, url string) error {
		raw, err := read(file)
		if err != nil {
			return perfGraphError("asset-unavailable")
		}
		for _, sidecar := range []struct{ suffix, encoding string }{{".gz", "gzip"}, {".br", "br"}} {
			encoded, err := read(file + sidecar.suffix)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil || assetmeasure.VerifySidecar(raw, encoded, sidecar.encoding) != nil {
				return perfGraphError("invalid-sidecar")
			}
		}
		hash := sha256.Sum256(raw)
		condition := "always"
		deps := []string{}
		switch name {
		case "bootstrap-feature-scene3d-webgpu.js":
			condition, deps = "webgpu", []string{"framework/runtime/bootstrap-feature-scene3d.js"}
		case "bootstrap-feature-scene3d-webgl.js":
			condition, deps = "webgl", []string{"framework/runtime/bootstrap-feature-scene3d.js"}
		case "bootstrap-feature-scene3d-pipeline-recovery.js":
			condition = "pipeline-recovery"
		case "hls.min.js":
			condition = "hls-required"
		}
		graph.Assets = append(graph.Assets, buildmanifest.PerfAssetUse{ID: "framework/runtime/" + name, SHA256: hex.EncodeToString(hash[:]), URL: url,
			Owner: "framework", Kind: "js", Phase: "dormant", Condition: condition, Dependencies: deps})
		return nil
	}
	for _, output := range outputs {
		if err := add(output.name, filepath.Base(dir)+"/"+output.name, "/gosx/"+output.name); err != nil {
			return nil, err
		}
	}
	for _, output := range inlineAssets {
		if filepath.Base(output.name) != "navigation-runtime.min.js" {
			return nil, perfGraphError("invalid-input")
		}
		file := filepath.ToSlash(filepath.Clean(filepath.Join(filepath.Base(dir), output.name)))
		raw, err := read(file)
		if err != nil {
			return nil, perfGraphError("asset-unavailable")
		}
		hash := sha256.Sum256(raw)
		url := "/gosx/assets/runtime/navigation." + hex.EncodeToString(hash[:]) + ".js"
		if expected, ok := runtimehost.NavigationRuntimeAssetHash(url); !ok || expected != hex.EncodeToString(hash[:]) {
			return nil, perfGraphError("invalid-input")
		}
		if err := add("navigation.js", file, url); err != nil {
			return nil, err
		}
	}
	if err := (&buildmanifest.Manifest{PerfAssetUses: graph}).ValidatePerfAssetUses(); err != nil {
		return nil, err
	}
	return graph, nil
}

func perfGraphError(code string) error {
	return &buildmanifest.PerfAssetError{Code: code, Pointer: "/perfAssetUses"}
}

func writePerfGraph(dir, path string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	graph, err := perfGraph(dir)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(graph, "", "  ")
	if err != nil {
		return perfGraphError("invalid-input")
	}
	if err := os.WriteFile(path, append(data, '\n'), 0644); err != nil {
		return perfGraphError("output-unavailable")
	}
	return nil
}
