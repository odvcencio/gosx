package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx/buildmanifest"
)

// ── build.runtime.exclude config validation ────────────────────────────────

func TestRuntimeAssetRolesAllValidateAndRoundTrip(t *testing.T) {
	roles := runtimeAssetRoles()
	want := []string{"controllers", "engines", "hubs", "islands", "payments", "relay", "scene3d", "textlayout", "video"}
	if len(roles) != len(want) {
		t.Fatalf("runtimeAssetRoles() = %v, want %v", roles, want)
	}
	for i, role := range want {
		if roles[i] != role {
			t.Fatalf("runtimeAssetRoles()[%d] = %q, want %q (full: %v)", i, roles[i], role, roles)
		}
	}
	if err := validateRuntimeExcludeRoles(roles); err != nil {
		t.Fatalf("validateRuntimeExcludeRoles(%v) = %v, want nil", roles, err)
	}
}

func TestValidateRuntimeExcludeRolesRejectsUnknownRole(t *testing.T) {
	err := validateRuntimeExcludeRoles([]string{"scene3d", "bogus-feature"})
	if err == nil {
		t.Fatal("expected an error for an unknown runtime asset role")
	}
	if !strings.Contains(err.Error(), `unknown runtime asset role "bogus-feature"`) {
		t.Fatalf("error = %v, want an unknown-role message naming the offending value", err)
	}
}

func TestValidateRuntimeExcludeRolesRejectsBlankAndDuplicateEntries(t *testing.T) {
	if err := validateRuntimeExcludeRoles([]string{"  "}); err == nil {
		t.Fatal("expected an error for a blank build.runtime.exclude entry")
	}
	if err := validateRuntimeExcludeRoles([]string{"scene3d", "scene3d"}); err == nil {
		t.Fatal("expected an error for a duplicate build.runtime.exclude entry")
	}
}

func TestLoadProjectConfigRejectsUnknownRuntimeExcludeRole(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "gosx.config.json"), `{
  "build": {
    "runtime": {
      "exclude": ["islands", "bogus-feature"]
    }
  }
}`)
	_, err := loadProjectConfig(dir)
	if err == nil || !strings.Contains(err.Error(), "unknown runtime asset role") {
		t.Fatalf("loadProjectConfig error = %v, want unknown runtime asset role", err)
	}
}

// TestLoadProjectConfigRuntimeDefaultsMatchTodaysBehavior pins GC-3's
// framework-lane compatibility contract: a project with no gosx.config.json,
// and one with an empty build.runtime block, both keep sourceMaps on and
// exclude nothing — so cmd/gosx/build.go's payload loop runs exactly as it
// did before build.runtime existed.
func TestLoadProjectConfigRuntimeDefaultsMatchTodaysBehavior(t *testing.T) {
	dir := t.TempDir()
	cfg, err := loadProjectConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Build.Runtime.sourceMapsEnabled() {
		t.Fatal("sourceMapsEnabled() = false with no gosx.config.json, want true (today's default)")
	}
	for _, role := range runtimeAssetRoles() {
		if cfg.Build.Runtime.excludesRole(role) {
			t.Fatalf("excludesRole(%q) = true with no gosx.config.json, want false", role)
		}
	}
	if cfg.Build.Runtime.excludesRole("") {
		t.Fatal(`excludesRole("") = true, want false: the core loader chain is never excludable`)
	}

	mustWriteFile(t, filepath.Join(dir, "gosx.config.json"), `{"build": {"runtime": {}}}`)
	cfg, err = loadProjectConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Build.Runtime.sourceMapsEnabled() {
		t.Fatal(`sourceMapsEnabled() = false with "runtime": {}, want true`)
	}
}

func TestLoadProjectConfigRuntimeSourceMapsExplicitFalseAndExclude(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "gosx.config.json"), `{
  "build": {
    "runtime": {
      "sourceMaps": false,
      "exclude": ["scene3d", "video", "payments", "relay", "textlayout"]
    }
  }
}`)
	cfg, err := loadProjectConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Build.Runtime.sourceMapsEnabled() {
		t.Fatal(`sourceMapsEnabled() = true, want false after "sourceMaps": false`)
	}
	for _, role := range []string{"scene3d", "video", "payments", "relay", "textlayout"} {
		if !cfg.Build.Runtime.excludesRole(role) {
			t.Fatalf("excludesRole(%q) = false, want true", role)
		}
	}
	for _, role := range []string{"islands", "engines", "hubs", "controllers"} {
		if cfg.Build.Runtime.excludesRole(role) {
			t.Fatalf("excludesRole(%q) = true, want false (not listed in exclude)", role)
		}
	}
}

// ── cmd/gosx build integration: exclude + sourceMaps ────────────────────────

// TestRunBuildRuntimeExcludeDropsAssetsFilesAndManifestKeys is GC-3's
// framework-lane acceptance test for build.runtime.exclude and
// build.runtime.sourceMaps: excluding scene3d/video/payments/relay/
// textlayout — the exact set docs/MODULES.md's server-rendered-app example
// names — removes those files from dist/assets/runtime, removes their keys
// from dist/build.json, and (with sourceMaps: false) removes every .map
// sidecar; the untouched core loader chain and the other feature chunks
// still build.
func TestRunBuildRuntimeExcludeDropsAssetsFilesAndManifestKeys(t *testing.T) {
	if raceDetectorEnabled {
		t.Skip("shells out to a go build subprocess; race instrumentation adds no value and blows the -race timeout")
	}
	dir := filepath.Join(t.TempDir(), "runtime-exclude-app")
	if err := RunInit(dir, "example.com/runtime-exclude-app", ""); err != nil {
		t.Fatal(err)
	}
	addLocalGoSXReplace(t, dir)
	tidyModule(t, dir)
	mustWriteFile(t, filepath.Join(dir, "gosx.config.json"), `{
  "build": {
    "runtime": {
      "sourceMaps": false,
      "exclude": ["scene3d", "video", "payments", "relay", "textlayout"]
    }
  }
}`)

	if err := RunBuild(dir, true); err != nil {
		t.Fatal(err)
	}

	runtimeDir := filepath.Join(dir, "dist", "assets", "runtime")
	entries, err := os.ReadDir(runtimeDir)
	if err != nil {
		t.Fatalf("read %s: %v", runtimeDir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}

	for _, forbidden := range []string{"scene3d", "hls.min", "stripe-bridge", "relay.", "textlayout"} {
		for _, name := range names {
			if strings.Contains(name, forbidden) {
				t.Fatalf("excluded asset fragment %q present in dist/assets/runtime: %v", forbidden, names)
			}
		}
	}
	for _, name := range names {
		if strings.HasSuffix(name, ".map") {
			t.Fatalf("sourceMaps: false but found a .map sidecar in dist/assets/runtime: %s (all: %v)", name, names)
		}
	}
	for _, want := range []string{"bootstrap.", "bootstrap-lite.", "bootstrap-runtime.", "bootstrap-feature-islands.", "bootstrap-feature-engines.", "bootstrap-feature-hubs.", "bootstrap-feature-controllers.", "patch."} {
		found := false
		for _, name := range names {
			if strings.HasPrefix(name, want) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected non-excluded asset with prefix %q, got dist/assets/runtime listing %v", want, names)
		}
	}

	manifestPath := filepath.Join(dir, "dist", "build.json")
	manifestJSON := readFile(t, manifestPath)
	for _, forbiddenKey := range []string{
		`"bootstrapFeatureScene3d"`, `"bootstrapFeatureScene3dCommand"`, `"bootstrapFeatureScene3dWebgpu"`,
		`"bootstrapFeatureScene3dWebgl"`, `"bootstrapFeatureScene3dGltf"`, `"bootstrapFeatureScene3dAnimation"`,
		`"bootstrapFeatureScene3dCompute"`, `"bootstrapFeatureScene3dDecompress"`,
		`"videoHLS"`, `"stripeBridge"`, `"relay"`, `"bootstrapFeatureTextlayout"`,
	} {
		if strings.Contains(manifestJSON, forbiddenKey) {
			t.Fatalf("dist/build.json still carries excluded key %s:\n%s", forbiddenKey, manifestJSON)
		}
	}
	for _, wantKey := range []string{`"bootstrapFeatureIslands"`, `"bootstrapFeatureEngines"`, `"bootstrapFeatureHubs"`, `"bootstrapFeatureControllers"`, `"patch"`} {
		if !strings.Contains(manifestJSON, wantKey) {
			t.Fatalf("dist/build.json is missing non-excluded key %s:\n%s", wantKey, manifestJSON)
		}
	}

	manifest, err := buildmanifest.Load(manifestPath)
	if err != nil {
		t.Fatalf("load %s: %v", manifestPath, err)
	}
	if manifest.Runtime.BootstrapFeatureScene3D.File != "" || manifest.Runtime.VideoHLS.File != "" ||
		manifest.Runtime.StripeBridge.File != "" || manifest.Runtime.Relay.File != "" ||
		manifest.Runtime.BootstrapFeatureTextlayout.File != "" {
		t.Fatalf("decoded manifest still names an excluded runtime asset: %#v", manifest.Runtime)
	}
	if manifest.Runtime.BootstrapFeatureIslands.File == "" || manifest.Runtime.Bootstrap.File == "" {
		t.Fatalf("decoded manifest is missing a non-excluded runtime asset: %#v", manifest.Runtime)
	}
}

// TestRunBuildDefaultRuntimeConfigMatchesLegacyOutputShape proves GC-3's
// no-config compatibility contract at the integration level: a project with
// no gosx.config.json builds every runtime JS payload and its .map sidecar,
// exactly as `gosx build` did before build.runtime existed.
func TestRunBuildDefaultRuntimeConfigMatchesLegacyOutputShape(t *testing.T) {
	if raceDetectorEnabled {
		t.Skip("shells out to a go build subprocess; race instrumentation adds no value and blows the -race timeout")
	}
	dir := filepath.Join(t.TempDir(), "runtime-default-app")
	if err := RunInit(dir, "example.com/runtime-default-app", ""); err != nil {
		t.Fatal(err)
	}
	addLocalGoSXReplace(t, dir)
	tidyModule(t, dir)
	// Deliberately no gosx.config.json.

	if err := RunBuild(dir, true); err != nil {
		t.Fatal(err)
	}

	runtimeDir := filepath.Join(dir, "dist", "assets", "runtime")
	entries, err := os.ReadDir(runtimeDir)
	if err != nil {
		t.Fatalf("read %s: %v", runtimeDir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}

	wantPrefixes := []string{
		"bootstrap.", "bootstrap-lite.", "bootstrap-runtime.",
		"bootstrap-feature-islands.", "bootstrap-feature-engines.", "bootstrap-feature-hubs.",
		"bootstrap-feature-controllers.", "bootstrap-feature-textlayout.",
		"bootstrap-feature-scene3d.", "bootstrap-feature-scene3d-command.",
		"bootstrap-feature-scene3d-webgpu.", "bootstrap-feature-scene3d-webgl.",
		"bootstrap-feature-scene3d-gltf.", "bootstrap-feature-scene3d-animation.",
		"bootstrap-feature-scene3d-compute.", "bootstrap-feature-scene3d-decompress.",
		"patch.", "hls.min.", "stripe-bridge.", "relay.",
	}
	for _, prefix := range wantPrefixes {
		foundJS, foundMap := false, false
		for _, name := range names {
			if !strings.HasPrefix(name, prefix) {
				continue
			}
			foundJS = true
			if strings.HasSuffix(name, ".js.map") {
				foundMap = true
			}
		}
		if !foundJS {
			t.Fatalf("default build (no gosx.config.json) is missing runtime asset %q, listing: %v", prefix, names)
		}
		// hls.min ships with no source file to derive a .map from
		// upstream (see client/js/vendor), so it never had one to keep.
		if prefix != "hls.min." && !foundMap {
			t.Fatalf("default build (no gosx.config.json) dropped the .map sidecar for %q, listing: %v", prefix, names)
		}
	}
}
