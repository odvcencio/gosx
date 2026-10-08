package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"m31labs.dev/gosx/buildmanifest"
	runtimehost "m31labs.dev/gosx/client/runtime/host"
	"m31labs.dev/gosx/internal/assetmeasure"
)

var perfBuildAppID = regexp.MustCompile(`^[a-z][a-z0-9-]{0,47}$`)
var perfBuildFile = regexp.MustCompile(`^[A-Za-z0-9_.-]+(/[A-Za-z0-9_.-]+)*$`)

func optimizeOptionalBuildWASM(path string, enabled bool) (bool, error) {
	if !enabled {
		return false, nil
	}
	return optimizeWASMWithWasmOpt(path)
}

func validatePerfBuildOptions(opts BuildOptions) error {
	if opts.PerfAppID != "" && (!perfBuildAppID.MatchString(opts.PerfAppID) || opts.Dev) {
		return perfBuildError("invalid-input", "/perfAppID")
	}
	return nil
}

func perfBuildError(code, pointer string) error {
	return &buildmanifest.PerfAssetError{Code: code, Pointer: pointer}
}

func perfBuildDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// stagePerfBuildAssets inventories whole emitted bodies. Build inventory is
// dormant until a page producer proves a use; it cannot certify reachability.
// App identity is explicit and never inferred from a directory or module name.
func stagePerfBuildAssets(dist string, manifest *BuildManifest, appID string) error {
	if !perfBuildAppID.MatchString(appID) || manifest == nil {
		return perfBuildError("invalid-input", "/perfAppID")
	}
	root, err := os.OpenRoot(dist)
	if err != nil {
		return perfBuildError("asset-unavailable", "/perfAssetUses")
	}
	defer root.Close()
	uses := &buildmanifest.PerfAssetUses{Version: 1, Assets: []buildmanifest.PerfAssetUse{}}
	seen := make(map[string]buildmanifest.PerfAssetUse)
	add := func(id, file, url, owner, kind, condition string, raw []byte) error {
		index := strconv.Itoa(len(uses.Assets))
		pointer := "/perfAssetUses/assets/" + index
		if raw == nil {
			raw, err = readPerfBuildBody(root, file)
			if err != nil {
				return perfBuildError("asset-unavailable", pointer)
			}
		}
		use := buildmanifest.PerfAssetUse{ID: id, SHA256: perfBuildDigest(raw), URL: url,
			Owner: owner, Kind: kind, Phase: "dormant", Condition: condition, Dependencies: []string{}}
		if file != "" && !strings.Contains(file, "."+buildmanifest.ContentHash(raw)+".") {
			return perfBuildError("invalid-input", pointer+"/sha256")
		}
		if previous, ok := seen[id]; ok {
			if previous.SHA256 != use.SHA256 || previous.URL != use.URL || previous.Kind != use.Kind || previous.Owner != use.Owner {
				return perfBuildError("invalid-input", pointer+"/id")
			}
			return nil
		}
		if kind == "wasm" {
			key := strings.TrimSuffix(strings.TrimPrefix(id, "framework/runtime/"), ".wasm")
			proof, ok := manifest.Runtime.WASMOptimization[key]
			if !ok || proof.Tool != "wasm-opt" || proof.Version != "108" || !proof.Applied ||
				proof.OutputSHA256 != use.SHA256 || !perfBuildHash.MatchString(proof.InputSHA256) {
				return perfBuildError("optimizer-unverified", "/runtime/wasmOptimization")
			}
		}
		for _, sidecar := range []struct{ suffix, encoding string }{{".gz", "gzip"}, {".br", "br"}} {
			if file == "" {
				break // Embedded navigation is verified against its served sidecars below.
			}
			encoded, err := readPerfBuildBody(root, file+sidecar.suffix)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil || assetmeasure.VerifySidecar(raw, encoded, sidecar.encoding) != nil {
				return perfBuildError("invalid-sidecar", pointer)
			}
		}
		seen[id] = use
		uses.Assets = append(uses.Assets, use)
		return nil
	}
	for _, asset := range runtimeSizeAssets(manifest) {
		if asset.file == "" {
			continue
		}
		name := asset.name
		switch name {
		case "runtime.wasm":
			name = "full.wasm"
		case "runtime-islands.wasm":
			name = "islands.wasm"
		default:
			if variant, ok := strings.CutPrefix(name, "runtime-variant/"); ok {
				name = variant + ".wasm"
			}
		}
		kind, condition := "js", "always"
		if strings.HasSuffix(name, ".wasm") {
			kind = "wasm"
		}
		switch name {
		case "bootstrap-feature-scene3d-webgpu.js":
			condition = "webgpu"
		case "bootstrap-feature-scene3d-webgl.js":
			condition = "webgl"
		case "bootstrap-feature-scene3d-pipeline-recovery.js":
			condition = "pipeline-recovery"
		case "hls.min.js":
			condition = "hls-required"
		}
		file, url := "assets/runtime/"+asset.file, buildmanifest.AssetURL("/gosx/assets", "runtime", asset.file)
		if asset.embedded != nil {
			file, url = "", runtimehost.NavigationRuntimePath
			for _, sidecar := range []struct {
				data     []byte
				encoding string
			}{{asset.gzipSidecar, "gzip"}, {asset.brotliSidecar, "br"}} {
				if assetmeasure.VerifySidecar(asset.embedded, sidecar.data, sidecar.encoding) != nil {
					return perfBuildError("invalid-sidecar", "/perfAssetUses")
				}
			}
			hash, ok := runtimehost.NavigationRuntimeAssetHash(url)
			if !ok || hash != perfBuildDigest(asset.embedded) {
				return perfBuildError("invalid-input", "/perfAssetUses")
			}
		}
		if err := add("framework/runtime/"+name, file, url, "framework", kind, condition, asset.embedded); err != nil {
			return err
		}
	}
	// Component names remain private producer inputs; public reports later
	// validate their logical IDs against a tracked catalog.
	for _, island := range manifest.Islands {
		if err := add("app/"+appID+"/islands/"+island.Name, "assets/islands/"+island.File,
			buildmanifest.AssetURL("/gosx/assets", "islands", island.File), "app", "program", "always", nil); err != nil {
			return err
		}
	}
	for _, css := range manifest.CSS {
		name := css.Source
		if name == "" {
			name = css.Component + ".css"
		}
		if err := add("app/"+appID+"/css/"+name, "assets/css/"+css.File,
			buildmanifest.AssetURL("/gosx/assets", "css", css.File), "app", "css", "always", nil); err != nil {
			return err
		}
	}
	sort.Slice(uses.Assets, func(i, j int) bool { return uses.Assets[i].ID < uses.Assets[j].ID })
	candidate := *manifest
	candidate.PerfAssetUses = uses
	if err := candidate.ValidatePerfAssetUses(); err != nil {
		return err
	}
	manifest.PerfAssetUses = uses
	return nil
}

const perfBuildBodyLimit = 64 << 20

func readPerfBuildBody(root *os.Root, path string) ([]byte, error) {
	if !perfBuildFile.MatchString(path) {
		return nil, errors.New("invalid asset path")
	}
	for _, part := range strings.Split(path, "/") {
		if part == "." || part == ".." {
			return nil, errors.New("invalid asset path")
		}
	}
	f, err := root.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > perfBuildBodyLimit {
		return nil, errors.New("invalid asset body")
	}
	body, err := io.ReadAll(io.LimitReader(f, perfBuildBodyLimit+1))
	if err != nil || len(body) > perfBuildBodyLimit {
		return nil, errors.New("invalid asset body")
	}
	return body, nil
}

var perfBuildHash = regexp.MustCompile(`^[0-9a-f]{64}$`)

func optimizePerfBuildWASM(path string) (*buildmanifest.WASMOptimization, error) {
	tool, err := exec.LookPath("wasm-opt")
	if err != nil {
		return nil, perfBuildError("optimizer-unverified", "/runtime/wasmOptimization")
	}
	return recordPerfWASMOptimization(path, func() ([]byte, error) {
		return exec.Command(tool, "--version").Output()
	}, func(path string) (bool, error) { return optimizeWASMUsing(path, tool) })
}

// The pinned tool must actually succeed. A missing tool, a failed optional
// optimizer, or a prebuilt artifact's size alone never proves optimization.
func recordPerfWASMOptimization(path string, version func() ([]byte, error), apply func(string) (bool, error)) (*buildmanifest.WASMOptimization, error) {
	failure := func() (*buildmanifest.WASMOptimization, error) {
		return nil, perfBuildError("optimizer-unverified", "/runtime/wasmOptimization")
	}
	v, err := version()
	if err != nil || strings.TrimSpace(string(v)) != "wasm-opt version 108 (version_108)" {
		return failure()
	}
	before, err := readPerfOptimizerBody(path)
	if err != nil || !validPerfWASMHeader(before) {
		return failure()
	}
	applied, err := apply(path)
	if err != nil || !applied {
		return failure()
	}
	after, err := readPerfOptimizerBody(path)
	if err != nil || !validPerfWASMHeader(after) {
		return failure()
	}
	return &buildmanifest.WASMOptimization{Tool: "wasm-opt", Version: "108", Applied: true,
		InputSHA256: perfBuildDigest(before), OutputSHA256: perfBuildDigest(after)}, nil
}

func validPerfWASMHeader(data []byte) bool {
	return len(data) >= 8 && bytes.Equal(data[:8], []byte{'\x00', 'a', 's', 'm', 1, 0, 0, 0})
}

func readPerfOptimizerBody(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > perfBuildBodyLimit {
		return nil, errors.New("invalid optimizer body")
	}
	raw, err := io.ReadAll(io.LimitReader(f, perfBuildBodyLimit+1))
	if err != nil || len(raw) > perfBuildBodyLimit {
		return nil, errors.New("invalid optimizer body")
	}
	return raw, nil
}
