package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStageDeploymentBundleCopiesRuntimeDirsAndWritesArtifacts(t *testing.T) {
	projectDir := t.TempDir()
	distDir := filepath.Join(t.TempDir(), "dist")

	mustWriteFile(t, filepath.Join(projectDir, "app", "page.gsx"), "package app\n")
	mustWriteFile(t, filepath.Join(projectDir, "public", "styles.css"), "body {}\n")
	mustWriteFile(t, filepath.Join(projectDir, ".env.example"), "PORT=8080\n")
	serverBinaryPath := filepath.Join(distDir, "server", "app")
	mustWriteFile(t, serverBinaryPath, "binary")

	if err := stageDeploymentBundle(projectDir, distDir, true, serverBinaryPath); err != nil {
		t.Fatal(err)
	}

	for _, rel := range []string{
		"app/page.gsx",
		"public/styles.css",
		".env.example",
		"README.md",
		"run.sh",
	} {
		if _, err := os.Stat(filepath.Join(distDir, rel)); err != nil {
			t.Fatalf("expected %s in bundle: %v", rel, err)
		}
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

func TestStageManifestCompatibilityRuntimeCopiesSelectiveBootstrap(t *testing.T) {
	distDir := t.TempDir()
	outputDir := t.TempDir()

	files := map[string]string{
		filepath.Join(distDir, "assets", "runtime", "runtime.1111.wasm"):                 "wasm",
		filepath.Join(distDir, "assets", "runtime", "runtime-islands.aaaa.wasm"):         "wasm-islands",
		filepath.Join(distDir, "assets", "runtime", "wasm_exec.2222.js"):                 "wasm-exec",
		filepath.Join(distDir, "assets", "runtime", "bootstrap.3333.js"):                 "bootstrap",
		filepath.Join(distDir, "assets", "runtime", "bootstrap-lite.4444.js"):            "bootstrap-lite",
		filepath.Join(distDir, "assets", "runtime", "bootstrap-runtime.5555.js"):         "bootstrap-runtime",
		filepath.Join(distDir, "assets", "runtime", "bootstrap-feature-islands.6666.js"): "bootstrap-feature-islands",
		filepath.Join(distDir, "assets", "runtime", "bootstrap-feature-engines.7777.js"): "bootstrap-feature-engines",
		filepath.Join(distDir, "assets", "runtime", "bootstrap-feature-hubs.8888.js"):    "bootstrap-feature-hubs",
		filepath.Join(distDir, "assets", "runtime", "patch.9999.js"):                     "patch",
		filepath.Join(distDir, "assets", "runtime", "hls.min.aaaa.js"):                   "hls",
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
		},
	}

	if err := stageManifestCompatibilityRuntime(distDir, manifest, outputDir); err != nil {
		t.Fatal(err)
	}

	for _, rel := range []string{
		"gosx/runtime.wasm",
		"gosx/runtime-islands.wasm",
		"gosx/wasm_exec.js",
		"gosx/bootstrap.js",
		"gosx/bootstrap-lite.js",
		"gosx/bootstrap-runtime.js",
		"gosx/bootstrap-feature-islands.js",
		"gosx/bootstrap-feature-engines.js",
		"gosx/bootstrap-feature-hubs.js",
		"gosx/patch.js",
		"gosx/hls.min.js",
	} {
		if _, err := os.Stat(filepath.Join(outputDir, rel)); err != nil {
			t.Fatalf("expected staged compat runtime file %s: %v", rel, err)
		}
	}
}

func TestRunBuildProdWritesHybridStaticBundleForStarterApp(t *testing.T) {
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
		"dist/static/assets/runtime",
		"dist/static/gosx/runtime.wasm",
		"dist/static/gosx/bootstrap-lite.js",
		"dist/static/gosx/bootstrap-runtime.js",
		"dist/static/gosx/bootstrap-feature-islands.js",
		"dist/static/gosx/bootstrap-feature-engines.js",
		"dist/static/gosx/bootstrap-feature-hubs.js",
		"dist/static/gosx/hls.min.js",
	} {
		if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
			t.Fatalf("expected build artifact %s: %v", rel, err)
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
	"github.com/odvcencio/gosx"
	"github.com/odvcencio/gosx/route"
	"github.com/odvcencio/gosx/server"
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
