package main

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"m31labs.dev/gosx"
	runtimewasm "m31labs.dev/gosx/client/runtime/wasm"
	"m31labs.dev/gosx/island"
	sceneinspect "m31labs.dev/gosx/scene/inspect"
)

func TestRuntimeVariantAssetCarriesContractIdentity(t *testing.T) {
	asset := runtimeVariantAsset(string(runtimewasm.VariantIslands), HashedAsset{
		File: "gosx-runtime-islands.abc.wasm",
		Hash: "abcdef0123456789",
		Size: 42,
	})
	if asset.ManifestHash == "" || asset.ManifestHash != runtimewasm.ManifestIdentity() {
		t.Fatalf("runtime variant manifest identity = %q", asset.ManifestHash)
	}
	if asset.FeatureMask != uint32(runtimewasm.FeatureCore|runtimewasm.FeatureIslands) {
		t.Fatalf("runtime variant feature mask = 0x%x", asset.FeatureMask)
	}
}

func TestPublishedRuntimeVariantAssetsAreExactlyTheFourProfiles(t *testing.T) {
	t.Setenv("GOSX_TINYGO_FULL_RUNTIME", "")
	assets := publishedRuntimeVariantAssets(
		HashedAsset{File: "core.wasm"},
		HashedAsset{File: "engine.wasm"},
		HashedAsset{File: "collab.wasm"},
		HashedAsset{File: "full.wasm"},
	)
	if len(assets) != 4 {
		t.Fatalf("published runtime assets = %v, want four profiles", assets)
	}
	if _, ok := assets[string(runtimewasm.VariantIslands)]; ok {
		t.Fatal("legacy islands alias was advertised as a capability profile")
	}
	for _, variant := range runtimewasm.PublishedVariants() {
		asset, ok := assets[string(variant)]
		if !ok {
			t.Fatalf("published runtime assets omitted %q", variant)
		}
		if asset.Variant != string(variant) || asset.FeatureMask != uint32(runtimewasm.FeatureMaskForVariant(variant)) {
			t.Fatalf("published %s contract = %+v", variant, asset)
		}
	}
}

func TestFullRuntimeCompatibilityModeOnlyAdvertisesFullProfile(t *testing.T) {
	t.Setenv("GOSX_TINYGO_FULL_RUNTIME", "1")
	assets := publishedRuntimeVariantAssets(
		HashedAsset{File: "core.wasm"},
		HashedAsset{File: "engine.wasm"},
		HashedAsset{File: "collab.wasm"},
		HashedAsset{File: "full.wasm"},
	)
	if len(assets) != 1 {
		t.Fatalf("full-runtime compatibility assets = %v, want one full profile", assets)
	}
	full, ok := assets[string(runtimewasm.VariantFull)]
	if !ok {
		t.Fatalf("full-runtime compatibility assets omitted full: %v", assets)
	}
	if full.File != "full.wasm" || full.Variant != string(runtimewasm.VariantFull) {
		t.Fatalf("full-runtime compatibility asset = %+v", full)
	}
}

func TestFullRuntimeCompatibilityManifestMakesIslandRouteSelectFull(t *testing.T) {
	t.Setenv("GOSX_TINYGO_FULL_RUNTIME", "1")
	full := HashedAsset{File: "gosx-runtime-full.wasm", Hash: "full", Size: 40}
	manifest := &BuildManifest{Runtime: RuntimeAssets{
		WASM:        full,
		WASMIslands: HashedAsset{File: "gosx-runtime-islands.wasm", Hash: "islands", Size: 5},
		WASMVariants: publishedRuntimeVariantAssets(
			HashedAsset{File: "gosx-runtime-core.wasm", Hash: "core", Size: 10},
			HashedAsset{File: "gosx-runtime-engine.wasm", Hash: "engine", Size: 25},
			HashedAsset{File: "gosx-runtime-collab.wasm", Hash: "collab", Size: 28},
			full,
		),
	}}
	renderer := island.NewRenderer("compatibility-route")
	if err := renderer.ApplyBuildManifest(manifest, "/gosx/assets"); err != nil {
		t.Fatal(err)
	}
	renderer.RenderIsland("Counter", nil, gosx.Text("counter"))
	if got := renderer.Summary().RuntimePath; got != "/gosx/assets/runtime/gosx-runtime-full.wasm" {
		t.Fatalf("full-runtime compatibility island selected %q, want full artifact", got)
	}
}

func TestRuntimeJSAssetDataStripsMissingHLSMapTrailer(t *testing.T) {
	raw := []byte("window.Hls = function() {};\n//# sourceMappingURL=hls.min.js.map\n")
	got := string(runtimeJSAssetData("hls.min", raw))
	if strings.Contains(got, "sourceMappingURL") {
		t.Fatalf("HLS runtime retained source map trailer: %q", got)
	}
	if got != "window.Hls = function() {};\n" {
		t.Fatalf("HLS runtime data = %q", got)
	}

	other := []byte("console.log('bootstrap');\n//# sourceMappingURL=bootstrap.js.map\n")
	if got := string(runtimeJSAssetData("bootstrap", other)); got != string(other) {
		t.Fatalf("non-HLS runtime asset was changed: %q", got)
	}
}

func TestGoServerBuildArgsUsesTrimpath(t *testing.T) {
	got := goServerBuildArgs("dist/server/app")
	want := []string{"build", "-trimpath", "-o", "dist/server/app", "."}
	if !slices.Equal(got, want) {
		t.Fatalf("server build args = %#v, want %#v", got, want)
	}
}

func TestWrapStandardGoWASMExecCapturesAndRestoresConstructor(t *testing.T) {
	wrapped := string(wrapStandardGoWASMExec([]byte(`globalThis.Go = function StandardGo() {};`)))
	for _, contract := range []string{
		`var previousGo = global.Go`,
		`standardGo = global.Go`,
		`if (hadGo) global.Go = previousGo; else delete global.Go`,
		`Object.defineProperty(global, "__gosx_standard_go_wasm_ctor", {value: standardGo, writable: false, configurable: true})`,
	} {
		if !strings.Contains(wrapped, contract) {
			t.Fatalf("standard-Go wrapper omitted %q:\n%s", contract, wrapped)
		}
	}
}

func TestStageDeploymentBundleCopiesRuntimeDirsAndWritesArtifacts(t *testing.T) {
	projectDir := t.TempDir()
	distDir := filepath.Join(t.TempDir(), "dist")

	mustWriteFile(t, filepath.Join(projectDir, "app", "page.gsx"), "package app\n")
	mustWriteFile(t, filepath.Join(projectDir, "content", "docs", "intro.md"), "# Introduction\n")
	mustWriteFile(t, filepath.Join(projectDir, "public", "styles.css"), "body {}\n")
	mustWriteFile(t, filepath.Join(projectDir, ".env.example"), "PORT=8080\n")
	mustWriteFile(t, filepath.Join(distDir, "content", "docs", "removed.md"), "# Removed\n")
	serverBinaryPath := filepath.Join(distDir, "server", "app")
	mustWriteFile(t, serverBinaryPath, "binary")

	manifest := &BuildManifest{}
	if err := stageDeploymentBundle(projectDir, distDir, manifest, true, serverBinaryPath); err != nil {
		t.Fatal(err)
	}

	for _, rel := range []string{
		"app/page.gsx",
		"content/docs/intro.md",
		"public/styles.css",
		".env.example",
		"README.md",
		"run.sh",
	} {
		if _, err := os.Stat(filepath.Join(distDir, rel)); err != nil {
			t.Fatalf("expected %s in bundle: %v", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(distDir, "content", "docs", "removed.md")); !os.IsNotExist(err) {
		t.Fatalf("stale content file survived deployment staging: %v", err)
	}

	runScript := readFile(t, filepath.Join(distDir, "run.sh"))
	for _, snippet := range []string{
		`export GOSX_APP_ROOT="${GOSX_APP_ROOT:-$DIR}"`,
		`exec "$DIR/server/app" "$@"`,
	} {
		if !strings.Contains(runScript, snippet) {
			t.Fatalf("expected %q in run.sh, got %q", snippet, runScript)
		}
	}

	readme := readFile(t, filepath.Join(distDir, "README.md"))
	if !strings.Contains(readme, "deployable GoSX bundle") {
		t.Fatalf("unexpected dist README: %q", readme)
	}
	if !strings.Contains(readme, "`content/` contains collection documents") {
		t.Fatalf("dist README omits staged content directory: %q", readme)
	}
}

func TestEvaluateProjectSceneBudget(t *testing.T) {
	projectDir := t.TempDir()
	scenePath := filepath.Join(projectDir, "app", "scene.json")
	budgetPath := filepath.Join(projectDir, "scene-budget.json")
	mustWriteFile(t, scenePath, `{"objects":[{"id":"cube","kind":"cube"}]}`)
	mustWriteFile(t, budgetPath, `{"scene3d":{"maxInitialGPUBytes":1,"maxDrawCalls":10}}`)

	results, found, err := evaluateProjectSceneBudget(projectDir, budgetPath, false)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("expected scene files to be found")
	}
	if !sceneinspect.BudgetFailed(results, false) {
		t.Fatalf("expected budget failure: %+v", results)
	}

	emptyDir := t.TempDir()
	results, found, err = evaluateProjectSceneBudget(emptyDir, budgetPath, false)
	if err != nil {
		t.Fatal(err)
	}
	if found || len(results) != 0 {
		t.Fatalf("expected no scene files in empty project, found=%v results=%+v", found, results)
	}
}

func TestWriteBuildReadmeWithoutServerBinary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "README.md")
	if err := writeBuildReadme(path, false); err != nil {
		t.Fatal(err)
	}
	readme := readFile(t, path)
	if strings.Contains(readme, "run.sh") {
		t.Fatalf("did not expect launch-script instructions in %q", readme)
	}
	if !strings.Contains(readme, "`assets/` contains immutable hashed runtime") {
		t.Fatalf("unexpected readme content %q", readme)
	}
	if !strings.Contains(readme, "`content/` contains collection documents") {
		t.Fatalf("readme omits optional content directory: %q", readme)
	}
}

func TestCSSAssetBaseNameUsesRelativePath(t *testing.T) {
	cases := map[string]string{
		"app/page.css":               "app_page",
		"app/docs/page.css":          "app_docs_page",
		"components/hero-banner.css": "components_hero_banner",
	}
	for input, want := range cases {
		if got := cssAssetBaseName(input); got != want {
			t.Fatalf("%s: expected %q, got %q", input, want, got)
		}
	}
}

func TestWriteHashedWritesCompressedSidecarsWhenSmaller(t *testing.T) {
	dir := t.TempDir()
	data := []byte(strings.Repeat("runtime island payload ", 64))

	asset, err := writeHashed(dir, "runtime", ".wasm", data)
	if err != nil {
		t.Fatal(err)
	}
	for _, ext := range []string{".gz", ".br"} {
		sidecar := filepath.Join(dir, asset.File+ext)
		info, err := os.Stat(sidecar)
		if err != nil {
			t.Fatalf("expected %s sidecar: %v", ext, err)
		}
		if info.Size() >= int64(len(data)) {
			t.Fatalf("expected %s sidecar smaller than raw data, raw=%d compressed=%d", ext, len(data), info.Size())
		}
	}
}

func TestWriteHashedWithoutCompressedSidecarsSkipsDevRuntimeSidecars(t *testing.T) {
	dir := t.TempDir()
	data := []byte(strings.Repeat("runtime island payload ", 64))

	prodAsset, err := writeHashed(dir, "gosx-runtime", ".wasm", data)
	if err != nil {
		t.Fatal(err)
	}
	for _, ext := range []string{".gz", ".br"} {
		if _, err := os.Stat(filepath.Join(dir, prodAsset.File+ext)); err != nil {
			t.Fatalf("expected initial %s sidecar: %v", ext, err)
		}
	}

	devAsset, err := writeHashedWithoutCompressedSidecars(dir, "gosx-runtime", ".wasm", data)
	if err != nil {
		t.Fatal(err)
	}
	if devAsset.File != prodAsset.File {
		t.Fatalf("expected same hashed runtime path, got %q want %q", devAsset.File, prodAsset.File)
	}
	for _, ext := range []string{".gz", ".br"} {
		if _, err := os.Stat(filepath.Join(dir, devAsset.File+ext)); !os.IsNotExist(err) {
			t.Fatalf("expected no dev runtime %s sidecar, stat err=%v", ext, err)
		}
	}
}

func TestDiscoverProjectGSXFilesSkipsHiddenScratchAndNestedWorktrees(t *testing.T) {
	projectDir := t.TempDir()
	mustWriteFile(t, filepath.Join(projectDir, "app", "page.gsx"), "package app\n")
	mustWriteFile(t, filepath.Join(projectDir, "components", "card.gsx"), "package components\n")
	mustWriteFile(t, filepath.Join(projectDir, ".tiller", "scratch", "codex", "work", "app", "bad.gsx"), "package scratch\n")
	mustWriteFile(t, filepath.Join(projectDir, ".cache", "app", "bad.gsx"), "package cache\n")
	mustWriteFile(t, filepath.Join(projectDir, "dist", "app", "bad.gsx"), "package dist\n")
	mustWriteFile(t, filepath.Join(projectDir, "node_modules", "pkg", "bad.gsx"), "package node\n")
	mustWriteFile(t, filepath.Join(projectDir, "nested-worktree", ".git"), "gitdir: ../.git/worktrees/nested\n")
	mustWriteFile(t, filepath.Join(projectDir, "nested-worktree", "app", "bad.gsx"), "package nested\n")

	files, err := discoverProjectGSXFiles(projectDir)
	if err != nil {
		t.Fatal(err)
	}
	relFiles := make([]string, 0, len(files))
	for _, file := range files {
		rel, err := filepath.Rel(projectDir, file)
		if err != nil {
			t.Fatal(err)
		}
		relFiles = append(relFiles, filepath.ToSlash(rel))
	}
	want := []string{"app/page.gsx", "components/card.gsx"}
	if !slices.Equal(relFiles, want) {
		t.Fatalf("discovered GSX files = %#v, want %#v", relFiles, want)
	}
}

func TestDiscoverProjectGSXFilesKeepsExplicitHiddenRoot(t *testing.T) {
	projectDir := t.TempDir()
	hiddenRoot := filepath.Join(projectDir, ".fixtures")
	mustWriteFile(t, filepath.Join(hiddenRoot, "app", "page.gsx"), "package app\n")

	files, err := discoverProjectGSXFiles(hiddenRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || filepath.ToSlash(files[0]) != filepath.ToSlash(filepath.Join(hiddenRoot, "app", "page.gsx")) {
		t.Fatalf("discovered GSX files = %#v", files)
	}
}

func TestStageOfflineAssetBundleWritesVersionedManifest(t *testing.T) {
	projectDir := t.TempDir()
	distDir := filepath.Join(t.TempDir(), "dist")
	mustWriteFile(t, filepath.Join(projectDir, "app", "page.gsx"), "package app\n")
	mustWriteFile(t, filepath.Join(projectDir, "public", "logo.svg"), "<svg />\n")
	mustWriteFile(t, filepath.Join(distDir, "assets", "runtime", "bootstrap.abc.js"), "runtime")
	mustWriteFile(t, filepath.Join(distDir, "build.json"), `{"runtime":{}}`)

	if err := stageOfflineAssetBundle(projectDir, distDir); err != nil {
		t.Fatal(err)
	}

	for _, rel := range []string{
		"assets/runtime/bootstrap.abc.js",
		"app/page.gsx",
		"public/logo.svg",
		"build.json",
		"offline-manifest.json",
	} {
		if _, err := os.Stat(filepath.Join(distDir, "offline", rel)); err != nil {
			t.Fatalf("expected offline artifact %s: %v", rel, err)
		}
	}

	var manifest offlineAssetManifest
	data := readFile(t, filepath.Join(distDir, "offline", "offline-manifest.json"))
	if err := json.Unmarshal([]byte(data), &manifest); err != nil {
		t.Fatalf("decode offline manifest: %v", err)
	}
	if manifest.SchemaVersion != 1 || manifest.CacheVersion == "" {
		t.Fatalf("unexpected offline manifest header: %#v", manifest)
	}
	policies := map[string]string{}
	for _, record := range manifest.Files {
		policies[record.Path] = record.CachePolicy
		if record.SHA256 == "" || record.Size <= 0 {
			t.Fatalf("record missing hash/size: %#v", record)
		}
	}
	if policies["assets/runtime/bootstrap.abc.js"] != "immutable" {
		t.Fatalf("runtime asset policy = %q", policies["assets/runtime/bootstrap.abc.js"])
	}
	if policies["build.json"] != "versioned" {
		t.Fatalf("build manifest policy = %q", policies["build.json"])
	}
	if policies["app/page.gsx"] != "first-launch" {
		t.Fatalf("app asset policy = %q", policies["app/page.gsx"])
	}
}

func TestProjectBuildHooksLoadAndRun(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "gosx.config.json"), `{
  "build": {
    "hooks": {
      "pre": ["printf pre > pre.txt"],
      "post": ["printf post > post.txt"]
    }
  }
}`)

	cfg, err := loadProjectConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Build.Hooks.Pre) != 1 || len(cfg.Build.Hooks.Post) != 1 {
		t.Fatalf("unexpected build hooks config %#v", cfg)
	}

	if err := runBuildHookCommands(dir, "pre-build", cfg.Build.Hooks.Pre); err != nil {
		t.Fatal(err)
	}
	if err := runBuildHookCommands(dir, "post-build", cfg.Build.Hooks.Post); err != nil {
		t.Fatal(err)
	}

	if got := readFile(t, filepath.Join(dir, "pre.txt")); got != "pre" {
		t.Fatalf("expected pre hook output, got %q", got)
	}
	if got := readFile(t, filepath.Join(dir, "post.txt")); got != "post" {
		t.Fatalf("expected post hook output, got %q", got)
	}
}

func TestStageManifestCompatibilityRuntimeCopiesOnlyReferencedAssets(t *testing.T) {
	distDir := t.TempDir()
	outputDir := t.TempDir()

	files := map[string]string{
		filepath.Join(distDir, "assets", "runtime", "runtime.1111.wasm"):                 "wasm",
		filepath.Join(distDir, "assets", "runtime", "runtime-islands.aaaa.wasm"):         "wasm-islands",
		filepath.Join(distDir, "assets", "runtime", "wasm_exec.2222.js"):                 "wasm-exec",
		filepath.Join(distDir, "assets", "runtime", "standard-go-wasm_exec.2a2a.js"):     "standard-go-wasm-exec",
		filepath.Join(distDir, "assets", "runtime", "bootstrap.3333.js"):                 "bootstrap",
		filepath.Join(distDir, "assets", "runtime", "bootstrap-lite.4444.js"):            "bootstrap-lite",
		filepath.Join(distDir, "assets", "runtime", "bootstrap-runtime.5555.js"):         "bootstrap-runtime",
		filepath.Join(distDir, "assets", "runtime", "bootstrap-runtime.5555.js.gz"):      "bootstrap-runtime-gzip",
		filepath.Join(distDir, "assets", "runtime", "bootstrap-runtime.5555.js.br"):      "bootstrap-runtime-br",
		filepath.Join(distDir, "assets", "runtime", "bootstrap-feature-islands.6666.js"): "bootstrap-feature-islands",
		filepath.Join(distDir, "assets", "runtime", "bootstrap-feature-engines.7777.js"): "bootstrap-feature-engines",
		filepath.Join(distDir, "assets", "runtime", "bootstrap-feature-hubs.8888.js"):    "bootstrap-feature-hubs",
		filepath.Join(distDir, "assets", "runtime", "patch.9999.js"):                     "patch",
		filepath.Join(distDir, "assets", "runtime", "hls.min.aaaa.js"):                   "hls",
		filepath.Join(distDir, "assets", "runtime", "relay.bbbb.js"):                     "relay",
		filepath.Join(distDir, "assets", "islands", "Counter.abcd.gxi"):                  "counter",
		filepath.Join(distDir, "assets", "css", "counter.dcba.css"):                      "counter-css",
	}
	for path, contents := range files {
		mustWriteFile(t, path, contents)
	}

	manifest := &BuildManifest{
		Runtime: RuntimeAssets{
			WASM:                    HashedAsset{File: "runtime.1111.wasm"},
			WASMIslands:             HashedAsset{File: "runtime-islands.aaaa.wasm"},
			WASMExec:                HashedAsset{File: "wasm_exec.2222.js"},
			StandardGoWASMExec:      HashedAsset{File: "standard-go-wasm_exec.2a2a.js"},
			Bootstrap:               HashedAsset{File: "bootstrap.3333.js"},
			BootstrapLite:           HashedAsset{File: "bootstrap-lite.4444.js"},
			BootstrapRuntime:        HashedAsset{File: "bootstrap-runtime.5555.js"},
			BootstrapFeatureIslands: HashedAsset{File: "bootstrap-feature-islands.6666.js"},
			BootstrapFeatureEngines: HashedAsset{File: "bootstrap-feature-engines.7777.js"},
			BootstrapFeatureHubs:    HashedAsset{File: "bootstrap-feature-hubs.8888.js"},
			Patch:                   HashedAsset{File: "patch.9999.js"},
			VideoHLS:                HashedAsset{File: "hls.min.aaaa.js"},
			Relay:                   HashedAsset{File: "relay.bbbb.js"},
		},
		Islands: []IslandAsset{{Name: "Counter", Format: "bin", HashedAsset: HashedAsset{File: "Counter.abcd.gxi"}}},
		CSS:     []CSSAsset{{Component: "Counter", Source: "counter.css", HashedAsset: HashedAsset{File: "counter.dcba.css"}}},
	}

	refs := []string{
		"/gosx/assets/runtime/bootstrap-runtime.5555.js",
		"/gosx/standard-go-wasm_exec.js",
		"/gosx/bootstrap-feature-engines.js",
		"/gosx/hls.min.js",
		"/gosx/relay.js",
		"/gosx/islands/Counter.gxi",
		"/gosx/css/counter.css",
	}
	if err := stageManifestCompatibilityRuntime(distDir, manifest, outputDir, refs); err != nil {
		t.Fatal(err)
	}

	for _, rel := range []string{
		"gosx/assets/runtime/bootstrap-runtime.5555.js",
		"gosx/assets/runtime/bootstrap-runtime.5555.js.gz",
		"gosx/assets/runtime/bootstrap-runtime.5555.js.br",
		"gosx/bootstrap-feature-engines.js",
		"gosx/standard-go-wasm_exec.js",
		"gosx/hls.min.js",
		"gosx/relay.js",
		"gosx/islands/Counter.gxi",
		"gosx/css/counter.css",
	} {
		if _, err := os.Stat(filepath.Join(outputDir, rel)); err != nil {
			t.Fatalf("expected staged compat runtime file %s: %v", rel, err)
		}
	}
	for _, rel := range []string{
		"gosx/runtime.wasm",
		"gosx/runtime-islands.wasm",
		"gosx/wasm_exec.js",
		"gosx/bootstrap.js",
		"gosx/bootstrap-lite.js",
		"gosx/bootstrap-runtime.js",
		"gosx/bootstrap-feature-islands.js",
		"gosx/bootstrap-feature-hubs.js",
		"gosx/patch.js",
	} {
		if _, err := os.Stat(filepath.Join(outputDir, rel)); !os.IsNotExist(err) {
			t.Fatalf("did not expect unreferenced runtime file %s: %v", rel, err)
		}
	}
}

func TestRunBuildProdWritesHybridStaticBundleForStarterApp(t *testing.T) {
	if raceDetectorEnabled {
		t.Skip("shells out to a TinyGo/go build subprocess; race instrumentation adds no value and blows the -race timeout")
	}
	dir := filepath.Join(t.TempDir(), "build-app")
	if err := RunInit(dir, "example.com/build-app", ""); err != nil {
		t.Fatal(err)
	}
	addLocalGoSXReplace(t, dir)
	tidyModule(t, dir)

	if err := RunBuild(dir, false); err != nil {
		t.Fatal(err)
	}

	for _, rel := range []string{
		"dist/build.json",
		"dist/export.json",
		"dist/edge/worker.js",
		"dist/platform/deployment.json",
		"dist/platform/vercel.json",
		"dist/server/app",
		"dist/static/index.html",
		"dist/static/stack/index.html",
	} {
		if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
			t.Fatalf("expected build artifact %s: %v", rel, err)
		}
	}
	for _, rel := range []string{
		"dist/static/assets/runtime",
		"dist/static/gosx",
	} {
		if _, err := os.Stat(filepath.Join(dir, rel)); !os.IsNotExist(err) {
			t.Fatalf("did not expect zero-runtime static build artifact %s: %v", rel, err)
		}
	}

	stackHTML := readFile(t, filepath.Join(dir, "dist", "static", "stack", "index.html"))
	if !strings.Contains(stackHTML, `href="../styles.css"`) {
		t.Fatalf("expected export-safe nested asset url in %q", stackHTML)
	}

	edgeWorker := readFile(t, filepath.Join(dir, "dist", "edge", "worker.js"))
	for _, snippet := range []string{
		`GOSX_STATIC_ROUTES`,
		`Missing GOSX_ORIGIN`,
		`stack/index.html`,
	} {
		if !strings.Contains(edgeWorker, snippet) {
			t.Fatalf("expected %q in edge worker bundle %q", snippet, edgeWorker)
		}
	}
}

// TestRunBuildRelocatedBundleRendersSiblingFragment proves the production
// deployment shape end to end. The server binary is built with -trimpath,
// the finished dist/ tree is moved to a fresh path, and the original source
// tree is removed before run.sh starts it from outside the bundle. run.sh's
// GOSX_APP_ROOT export must therefore point the trimpath-safe caller resolver
// at the staged app/ tree for LoadFileProgramHere to find page.gsx.
func TestRunBuildRelocatedBundleRendersSiblingFragment(t *testing.T) {
	if raceDetectorEnabled {
		t.Skip("shells out to the production build and server; race instrumentation adds no value and blows the timeout")
	}
	if _, err := exec.LookPath("tinygo"); err != nil {
		t.Skip("production relocation test requires TinyGo on PATH")
	}

	sourceDir := filepath.Join(t.TempDir(), "fragment-source")
	if err := os.MkdirAll(sourceDir, 0755); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(sourceDir, "go.mod"), `module example.com/fragment-relocation

go 1.25

require m31labs.dev/gosx v0.53.5
`)
	addLocalGoSXReplace(t, sourceDir)
	mustWriteFile(t, filepath.Join(sourceDir, "main.go"), `package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"example.com/fragment-relocation/app/wire"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

func main() {
	_, thisFile, _, _ := runtime.Caller(0)
	root := server.ResolveAppRoot(thisFile)
	router := route.NewRouter()
	router.Handle("/wire/signal", http.HandlerFunc(wire.ServeSignalFragment))
	if err := router.AddDir(filepath.Join(root, "app"), route.FileRoutesOptions{}); err != nil {
		log.Fatal(err)
	}
	handler, err := router.BuildChecked()
	if err != nil {
		log.Fatal(err)
	}
	listenAddr := "127.0.0.1:0"
	if port := os.Getenv("PORT"); port != "" {
		listenAddr = "127.0.0.1:" + strings.TrimPrefix(port, ":")
	}
	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("LISTENING %s\n", ln.Addr())
	log.Fatal(http.Serve(ln, handler))
}
`)
	mustWriteFile(t, filepath.Join(sourceDir, "app", "wire", "page.gsx"), `package wire

type SignalCardProps struct {
	Label string
	Value string
}

component SignalCard(props: SignalCardProps) {
	return <li class="signal-card">{props.Label}: {props.Value}</li>
}

component Page() {
	return <ul data-gosx-region data-gosx-region-url="/wire/signal">
		<SignalCard label="Passing Yards" value="317" />
	</ul>
}
`)
	mustWriteFile(t, filepath.Join(sourceDir, "app", "wire", "page.server.go"), `package wire

import (
	"net/http"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/route"
)

// FragmentProps deliberately has a different name from page.gsx's
// SignalCardProps. RenderProgramComponentNode proves the fields structurally
// at the render boundary; it does not require Go to recreate the .gsx type.
type FragmentProps struct {
	Label string
	Value string
}

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{}); err != nil {
		panic(err)
	}
}

func ServeSignalFragment(w http.ResponseWriter, _ *http.Request) {
	prog, err := route.LoadFileProgramHere("page.gsx")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	node, err := route.RenderProgramComponentNode(prog, "SignalCard", route.ProgramRenderEnv{
		Props: FragmentProps{Label: "Passing Yards", Value: "317"},
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(gosx.RenderHTML(node)))
}
`)
	tidyModule(t, sourceDir)

	if err := RunBuild(sourceDir, false); err != nil {
		t.Fatalf("RunBuild: %v", err)
	}

	relocated := filepath.Join(t.TempDir(), "staged-dist")
	if err := os.Rename(filepath.Join(sourceDir, "dist"), relocated); err != nil {
		t.Fatalf("move dist to staged path: %v", err)
	}
	if err := os.RemoveAll(sourceDir); err != nil {
		t.Fatalf("remove original source tree: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sourceDir, "app", "wire", "page.gsx")); !os.IsNotExist(err) {
		t.Fatalf("original page.gsx is still available after relocation: %v", err)
	}

	cmd := exec.Command(filepath.Join(relocated, "run.sh"))
	cmd.Dir = filepath.Dir(relocated)
	cmd.Stderr = os.Stderr
	cmd.Env = withoutEnv(os.Environ(), "GOSX_APP_ROOT")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start relocated bundle: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	addrCh := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			if addr, ok := strings.CutPrefix(scanner.Text(), "LISTENING "); ok {
				select {
				case addrCh <- addr:
				default:
				}
				return
			}
		}
	}()

	var addr string
	select {
	case addr = <-addrCh:
	case <-time.After(20 * time.Second):
		t.Fatal("relocated bundle never printed its listening address")
	}

	client := &http.Client{Timeout: 10 * time.Second}
	baseURL := "http://" + addr
	pageResp, err := client.Get(baseURL + "/wire")
	if err != nil {
		t.Fatalf("GET relocated page: %v", err)
	}
	pageBody, readErr := io.ReadAll(pageResp.Body)
	_ = pageResp.Body.Close()
	if readErr != nil {
		t.Fatalf("read relocated page: %v", readErr)
	}
	if pageResp.StatusCode != http.StatusOK {
		t.Fatalf("GET /wire = %d, want 200; body: %s", pageResp.StatusCode, pageBody)
	}
	if !strings.Contains(string(pageBody), `data-gosx-region-url="/wire/signal"`) {
		t.Fatalf("relocated page is missing region wiring: %s", pageBody)
	}

	fragmentResp, err := client.Get(baseURL + "/wire/signal")
	if err != nil {
		t.Fatalf("GET relocated fragment: %v", err)
	}
	fragmentBody, readErr := io.ReadAll(fragmentResp.Body)
	_ = fragmentResp.Body.Close()
	if readErr != nil {
		t.Fatalf("read relocated fragment: %v", readErr)
	}
	const wantFragment = `<li class="signal-card">Passing Yards: 317</li>`
	if fragmentResp.StatusCode != http.StatusOK || string(fragmentBody) != wantFragment {
		t.Fatalf("GET /wire/signal = %d body %q, want 200 and %q", fragmentResp.StatusCode, fragmentBody, wantFragment)
	}
}

func withoutEnv(env []string, name string) []string {
	prefix := name + "="
	out := make([]string, 0, len(env))
	for _, entry := range env {
		if !strings.HasPrefix(entry, prefix) {
			out = append(out, entry)
		}
	}
	return out
}

func TestRunBuildStrictGateRunsBeforeDistWrites(t *testing.T) {
	dir := newInvalidStrictStarter(t, "build-strict-gate")
	err := RunBuild(dir, false)
	if err == nil || !strings.Contains(err.Error(), "cannot use 42") {
		t.Fatalf("RunBuild error = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "dist")); !os.IsNotExist(statErr) {
		t.Fatalf("strict gate wrote dist before failing: %v", statErr)
	}
}

// TestRunBuildRejectsPropsBearingStrictLayoutEntryBeforeDistWrites proves
// the narrowed gate (gosx#248) still refuses a props-bearing strict layout
// before any dist write: no code path calls a layout's own module's Load
// hook, so a layout's EntryProps is always nil. A props-bearing strict Page
// entry, by contrast, now passes this gate — see route/fileprogram_test.go
// and examples/basic for the render-time proof.
func TestRunBuildRejectsPropsBearingStrictLayoutEntryBeforeDistWrites(t *testing.T) {
	dir := newInvalidStrictStarter(t, "build-root-props-gate")
	mustWriteFile(t, filepath.Join(dir, "app", "page.gsx"), `package app
component Page() {
	return <main>ok</main>
}
`)
	mustWriteFile(t, filepath.Join(dir, "app", "layout.gsx"), `package app
type LayoutProps struct { Title string }
component Layout(props: LayoutProps) {
	return <main>{props.Title}</main>
}
`)
	err := RunBuild(dir, false)
	if err == nil || !strings.Contains(err.Error(), "layout has no Load hook wired to its own root props") {
		t.Fatalf("RunBuild error = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "dist")); !os.IsNotExist(statErr) {
		t.Fatalf("root-props gate wrote dist before failing: %v", statErr)
	}
}

func TestRunBuildProdHandlesRelativeProjectDir(t *testing.T) {
	if raceDetectorEnabled {
		t.Skip("shells out to a TinyGo/go build subprocess; race instrumentation adds no value and blows the -race timeout")
	}
	root := t.TempDir()
	projectDir := filepath.Join(root, "build-app")
	if err := RunInit(projectDir, "example.com/build-app", ""); err != nil {
		t.Fatal(err)
	}
	addLocalGoSXReplace(t, projectDir)
	tidyModule(t, projectDir)

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if chdirErr := os.Chdir(wd); chdirErr != nil {
			t.Fatalf("restore cwd: %v", chdirErr)
		}
	}()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}

	if err := RunBuild("build-app", false); err != nil {
		t.Fatal(err)
	}

	for _, rel := range []string{
		"dist/server/app",
		"dist/static/index.html",
		"dist/export.json",
	} {
		if _, err := os.Stat(filepath.Join(projectDir, rel)); err != nil {
			t.Fatalf("expected build artifact %s: %v", rel, err)
		}
	}
}

func TestRunBuildProdPreservesFileModuleHooksInStaticExport(t *testing.T) {
	if raceDetectorEnabled {
		t.Skip("shells out to a TinyGo/go build subprocess; race instrumentation adds no value and blows the -race timeout")
	}
	dir := filepath.Join(t.TempDir(), "build-app")
	if err := RunInit(dir, "example.com/build-app", ""); err != nil {
		t.Fatal(err)
	}
	addLocalGoSXReplace(t, dir)

	mustWriteFile(t, filepath.Join(dir, "app", "verify", "page.gsx"), `package app

func Page() Node {
	return <main class="verify" data-name={data.name}>
		<Badge label={data.name} />
	</main>
}
`)
	mustWriteFile(t, filepath.Join(dir, "app", "verify", "page.server.go"), `package app

import (
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

func init() {
	route.MustRegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			return map[string]string{
				"name": "Build Verified",
			}, nil
		},
		Metadata: func(ctx *route.RouteContext, page route.FilePage, data any) (server.Metadata, error) {
			return server.Metadata{
				Title: server.Title{Default: "Verify Export"},
			}, nil
		},
		Bindings: func(ctx *route.RouteContext, page route.FilePage, data any) route.FileTemplateBindings {
			return route.FileTemplateBindings{
				Funcs: map[string]any{
					"Badge": func(props struct{ Label string }) gosx.Node {
						return gosx.El("span", gosx.Attrs(gosx.Attr("class", "badge")), gosx.Text(props.Label))
					},
				},
			}
		},
	})
}
`)
	tidyModule(t, dir)

	if err := RunBuild(dir, false); err != nil {
		t.Fatal(err)
	}

	verifyHTML := readFile(t, filepath.Join(dir, "dist", "static", "verify", "index.html"))
	for _, snippet := range []string{
		"<title>Verify Export</title>",
		`class="verify" data-name="Build Verified"`,
		`<span class="badge">Build Verified</span>`,
	} {
		if !strings.Contains(verifyHTML, snippet) {
			t.Fatalf("expected %q in exported verify page %q", snippet, verifyHTML)
		}
	}
}

func mustWriteFile(t *testing.T, path string, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0644); err != nil {
		t.Fatal(err)
	}
}

func newInvalidStrictStarter(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := RunInit(dir, "example.com/"+name, ""); err != nil {
		t.Fatal(err)
	}
	addLocalGoSXReplace(t, dir)
	tidyModule(t, dir)
	mustWriteFile(t, filepath.Join(dir, "app", "page.gsx"), `package app

type CardProps struct { Label string }

component Card(props: CardProps) {
	return <p>{props.Label}</p>
}

component Page() {
	return <Card label={42} />
}
`)
	return dir
}
