package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sceneinspect "m31labs.dev/gosx/scene/inspect"
)

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

	if err := stageDeploymentBundle(projectDir, distDir, true, serverBinaryPath); err != nil {
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
