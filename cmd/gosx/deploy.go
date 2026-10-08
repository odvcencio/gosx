package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"sort"
	"strings"

	"github.com/andybalholm/brotli"
	"m31labs.dev/gosx/buildmanifest"
	"m31labs.dev/gosx/internal/bundlepolicy"
)

func deployUsage(w io.Writer) {
	fmt.Fprint(w, `gosx deploy check - Validate a built server bundle before deployment

Usage:
  gosx deploy check [--json] <dist>

Checks bundle policy, asset sizes/checksums, server launch files, and exported
pages without starting the app, reading runtime secrets, or contacting a host.
This is an offline artifact check; configure secrets and verify health on the
destination separately. Build with gosx build --prod first.
Exit codes: 0 checks passed, 1 checks failed, 2 invalid command arguments.
With --json, check failures and argument errors both produce JSON reports.
`)
}

func cmdDeploy() {
	if err := runDeploy(os.Args[2:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "deploy check:", err)
		os.Exit(deploymentExitCode(err))
	}
}

type deploymentUsageError struct{ error }

func deploymentExitCode(err error) int {
	if err == nil {
		return 0
	}
	var usage *deploymentUsageError
	if errors.As(err, &usage) {
		return 2
	}
	return 1
}

func runDeploy(args []string, out io.Writer) (err error) {
	jsonOut := slices.Contains(args, "--json")
	report := deploymentCheckReport{Version: 1}
	defer func() {
		if !jsonOut {
			return
		}
		report.OK = err == nil
		if err != nil {
			report.Errors = []string{err.Error()}
		}
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")
		err = errors.Join(err, encoder.Encode(report))
	}()
	if len(args) > 0 && (args[0] == "--help" || args[0] == "-h" || args[0] == "help") {
		jsonOut = false
		deployUsage(out)
		return nil
	}
	if len(args) == 0 || args[0] != "check" {
		return &deploymentUsageError{errors.New("expected: gosx deploy check [--json] <dist>")}
	}
	target := ""
	for _, arg := range args[1:] {
		switch arg {
		case "--help", "-h":
			jsonOut = false
			deployUsage(out)
			return nil
		case "--json":
			jsonOut = true
		default:
			if strings.HasPrefix(arg, "-") || target != "" {
				return &deploymentUsageError{fmt.Errorf("unexpected argument %q", arg)}
			}
			target = arg
		}
	}
	if target == "" {
		return &deploymentUsageError{errors.New("missing bundle directory; run gosx build --prod first")}
	}
	report, err = checkDeploymentBundle(target)
	if !jsonOut && err == nil {
		fmt.Fprintf(out, "Bundle checks passed: %d assets (%d bytes), %d static routes.\nLaunch the checked bundle with its run.sh script.\nConfigure runtime secrets and verify destination health before routing traffic.\n", report.Assets, report.AssetBytes, report.StaticRoutes)
	}
	return err
}

type deploymentCheckReport struct {
	Version      int      `json:"version"`
	Directory    string   `json:"directory"`
	OK           bool     `json:"ok"`
	Assets       int      `json:"assets"`
	AssetBytes   int64    `json:"assetBytes"`
	StaticRoutes int      `json:"staticRoutes"`
	Errors       []string `json:"errors,omitempty"`
}

// checkDeploymentBundle only reads artifacts. It never follows SourceRoot or
// executes the server: build hosts need neither production credentials nor DBs.
func checkDeploymentBundle(dir string) (report deploymentCheckReport, resultErr error) {
	report.Version = 1
	defer func() {
		report.OK = resultErr == nil
		if resultErr != nil {
			report.Errors = []string{resultErr.Error()}
		}
	}()
	abs, err := filepath.Abs(dir)
	if err != nil {
		return report, err
	}
	report.Directory = abs
	root, err := os.OpenRoot(abs)
	if err != nil {
		return report, err
	}
	defer root.Close()
	data, err := readDeploymentMetadata(root, "bundle-policy.json")
	if err != nil {
		return report, err
	}
	policy, err := bundlepolicy.DecodePolicyFile(data)
	if err != nil {
		return report, fmt.Errorf("bundle-policy.json: %w", err)
	}
	if diagnostics := bundlepolicy.AuditArtifact(abs, bundlepolicy.Config{Allow: policy.Allow, AllowPublic: policy.AllowPublic, Exclude: policy.Exclude}); !diagnostics.Empty() {
		return report, errors.New(diagnostics.Error())
	}
	data, err = readDeploymentMetadata(root, "build.json")
	if err != nil {
		return report, err
	}
	var manifest buildmanifest.Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return report, fmt.Errorf("build.json: %w", err)
	}
	if err := manifest.ValidateIslandAssets(); err != nil {
		return report, fmt.Errorf("build.json: %w", err)
	}
	assets := deploymentAssets(&manifest)
	if len(assets) == 0 {
		return report, errors.New("build.json contains no runtime assets; rebuild with gosx build --prod")
	}
	for _, required := range []struct {
		name  string
		asset buildmanifest.HashedAsset
	}{
		{"wasm", manifest.Runtime.WASM},
		{"wasmExec", manifest.Runtime.WASMExec},
		{"bootstrap", manifest.Runtime.Bootstrap},
		{"patch", manifest.Runtime.Patch},
	} {
		if required.asset.File == "" {
			return report, fmt.Errorf("build.json: required runtime asset %s is missing", required.name)
		}
	}
	server := "server/app"
	if _, err := root.Lstat(server); errors.Is(err, fs.ErrNotExist) {
		server = "server/app.exe"
	}
	for _, file := range []string{server, "run.sh"} {
		info, err := deploymentFileInfo(root, file)
		if err != nil {
			if file == "server/app.exe" && errors.Is(err, fs.ErrNotExist) {
				return report, fmt.Errorf("server/app and server/app.exe are missing; run gosx build --prod: %w", fs.ErrNotExist)
			}
			return report, err
		}
		if info.Size() == 0 || (runtime.GOOS != "windows" && file != "server/app.exe" && info.Mode().Perm()&0111 == 0) {
			return report, fmt.Errorf("%s: expected a nonempty executable launch file", file)
		}
	}
	if manifest.SceneAssets != nil {
		if _, err := readDeploymentMetadata(root, manifest.SceneAssets.File); err != nil {
			return report, err
		}
	}
	seen := make(map[string]buildmanifest.HashedAsset)
	for _, asset := range assets {
		if prior, ok := seen[asset.path]; ok {
			if prior != asset.HashedAsset {
				return report, fmt.Errorf("%s: conflicting asset records", asset.path)
			}
			continue
		}
		seen[asset.path] = asset.HashedAsset
		if err := verifyDeploymentAsset(root, asset); err != nil {
			return report, err
		}
		report.Assets++
		report.AssetBytes += asset.Size
	}
	data, err = readDeploymentMetadata(root, "export.json")
	if errors.Is(err, fs.ErrNotExist) {
		return report, nil // Prerendering can be disabled for dynamic applications.
	}
	if err != nil {
		return report, err
	}
	var exported exportManifest
	if err := json.Unmarshal(data, &exported); err != nil {
		return report, fmt.Errorf("export.json: %w", err)
	}
	for _, route := range exported.Routes {
		if !safeDeploymentPath(route.File) {
			return report, fmt.Errorf("export.json: invalid static route file %q", route.File)
		}
		if _, err := deploymentFileInfo(root, "static/"+route.File); err != nil {
			return report, err
		}
		report.StaticRoutes++
	}
	return report, nil
}

func safeDeploymentPath(name string) bool {
	return fs.ValidPath(name) && name != "." && !strings.ContainsAny(name, "\\:")
}

func deploymentFileInfo(root *os.Root, name string) (fs.FileInfo, error) {
	if !safeDeploymentPath(name) {
		return nil, fmt.Errorf("invalid bundle path %q", name)
	}
	info, err := root.Lstat(name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%s is missing; run gosx build --prod: %w", name, fs.ErrNotExist)
	}
	if err != nil {
		return nil, fmt.Errorf("inspect %s: %w", name, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: expected a regular file", name)
	}
	return info, nil
}

func readDeploymentMetadata(root *os.Root, name string) ([]byte, error) {
	if _, err := deploymentFileInfo(root, name); err != nil {
		return nil, err
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	const limit = 16 << 20
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if len(data) > limit {
		return nil, fmt.Errorf("%s: metadata exceeds 16 MiB", name)
	}
	return data, err
}

type deploymentAsset struct {
	path string
	buildmanifest.HashedAsset
}

func deploymentAssets(manifest *buildmanifest.Manifest) []deploymentAsset {
	var assets []deploymentAsset
	add := func(prefix string, asset buildmanifest.HashedAsset) {
		if asset.File != "" {
			// Do not clean untrusted manifest paths before validating them.
			assets = append(assets, deploymentAsset{prefix + "/" + asset.File, asset})
		}
	}
	// Walk the typed runtime structure so optional/future chunks and WASM
	// variants cannot silently escape validation when the manifest grows.
	walkDeploymentRuntimeAssets(reflect.ValueOf(manifest.Runtime), func(asset buildmanifest.HashedAsset) { add("assets/runtime", asset) })
	for _, asset := range manifest.Islands {
		add("assets/islands", asset.HashedAsset)
	}
	for _, asset := range manifest.CSS {
		add("assets/css", asset.HashedAsset)
	}
	for _, source := range manifest.Images {
		for _, asset := range source.Variants {
			add("assets/images", asset.HashedAsset)
		}
	}
	sort.Slice(assets, func(i, j int) bool { return assets[i].path < assets[j].path })
	return assets
}

// Traverse containers as well as structs so future slice-valued runtime fields
// receive the same checksum, size and path validation as individual assets.
func walkDeploymentRuntimeAssets(v reflect.Value, add func(buildmanifest.HashedAsset)) {
	if !v.IsValid() || !v.CanInterface() {
		return
	}
	if asset, ok := v.Interface().(buildmanifest.HashedAsset); ok {
		add(asset)
		return
	}
	switch v.Kind() {
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			walkDeploymentRuntimeAssets(v.Field(i), add)
		}
	case reflect.Map:
		for _, key := range v.MapKeys() {
			walkDeploymentRuntimeAssets(v.MapIndex(key), add)
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			walkDeploymentRuntimeAssets(v.Index(i), add)
		}
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			walkDeploymentRuntimeAssets(v.Elem(), add)
		}
	}
}

func verifyDeploymentAsset(root *os.Root, asset deploymentAsset) error {
	if !safeDeploymentPath(asset.File) || path.Base(asset.File) != asset.File {
		return fmt.Errorf("invalid asset filename %q", asset.File)
	}
	info, err := deploymentFileInfo(root, asset.path)
	if err != nil {
		return err
	}
	if info.Size() != asset.Size {
		return fmt.Errorf("%s: size mismatch (got %d, manifest %d)", asset.path, info.Size(), asset.Size)
	}
	f, err := root.Open(asset.path)
	if err != nil {
		return err
	}
	defer f.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return err
	}
	sum := hash.Sum(nil)
	if asset.Hash != hex.EncodeToString(sum[:8]) {
		return fmt.Errorf("%s: content checksum mismatch", asset.path)
	}
	if asset.Integrity != "" && asset.Integrity != "sha256-"+base64.StdEncoding.EncodeToString(sum) {
		return fmt.Errorf("%s: script integrity mismatch", asset.path)
	}
	for _, suffix := range []string{".gz", ".br"} {
		if err := verifyDeploymentSidecar(root, asset, suffix, sum); err != nil {
			return err
		}
	}
	return nil
}

func verifyDeploymentSidecar(root *os.Root, asset deploymentAsset, suffix string, sum []byte) error {
	name := asset.path + suffix
	if _, err := deploymentFileInfo(root, name); errors.Is(err, fs.ErrNotExist) {
		return nil // Compression is optional when it does not reduce size.
	} else if err != nil {
		return err
	}
	f, err := root.Open(name)
	if err != nil {
		return err
	}
	defer f.Close()
	var reader io.Reader = brotli.NewReader(f)
	if suffix == ".gz" {
		gz, err := gzip.NewReader(f)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		defer gz.Close()
		reader = gz
	}
	hash := sha256.New()
	n, err := io.Copy(hash, io.LimitReader(reader, asset.Size+1))
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	if n != asset.Size || !bytes.Equal(hash.Sum(nil), sum) {
		return fmt.Errorf("%s: compressed content differs from the manifest asset", name)
	}
	return nil
}
