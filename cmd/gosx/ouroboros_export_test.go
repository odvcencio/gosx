package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/buildmanifest"
	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/island"
	"m31labs.dev/gosx/perf/ouroboros"
	"m31labs.dev/gosx/server"
)

func TestLoadStrictOuroborosCorpusAcceptsFixtureManifest(t *testing.T) {
	root := repoRootForTest(t)
	routes, corpus, err := loadStrictOuroborosCorpus(
		filepath.Join(root, "examples", "ouroboros-corpus", "fixtures.v1.json"),
		filepath.Join(root, "examples", "ouroboros-corpus"),
		filepath.Join(root, "examples", "gosx-docs"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if corpus.CorpusID != "gosx-ouroboros-o0.2-v1" {
		t.Fatalf("unexpected corpus id %q", corpus.CorpusID)
	}
	if len(routes) != 12 || routes[10].ID != "R09B" || routes[11].ID != "R10" {
		t.Fatalf("unexpected canonical route sequence: %#v", routes)
	}
}

func TestLoadStrictOuroborosCorpusRejectsFixtureLocalR10(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fixtures.v1.json")
	mustWriteFile(t, path, canonicalCorpusJSONForTest(func(id string, body string) string {
		if id == "R10" {
			body = strings.ReplaceAll(body, `"fixtureApp":"examples/gosx-docs"`, `"fixtureApp":"examples/ouroboros-corpus"`)
			body = strings.ReplaceAll(body, `,"external":true`, "")
		}
		return body
	}))
	_, _, err := loadStrictOuroborosCorpus(path, filepath.Join(dir, "examples", "ouroboros-corpus"), filepath.Join(dir, "examples", "gosx-docs"))
	if err == nil || !strings.Contains(err.Error(), "R10 must be external") {
		t.Fatalf("expected fixture-local R10 rejection, got %v", err)
	}
}

func TestRunOuroborosExportCorpusRefusesExistingOutBeforeBuild(t *testing.T) {
	root := repoRootForTest(t)
	buildDir := filepath.Join(root, "build")
	if err := os.MkdirAll(buildDir, 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := os.MkdirTemp(buildDir, "existing-export-out-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(out) })
	err = runOuroborosExportCorpus(ouroborosExportOptions{
		RepoRoot:   root,
		OutDir:     out,
		CorpusPath: filepath.Join("examples", "ouroboros-corpus", "fixtures.v1.json"),
		FixtureApp: filepath.Join("examples", "ouroboros-corpus"),
		DocsApp:    filepath.Join("examples", "gosx-docs"),
	})
	if err == nil || !strings.Contains(err.Error(), "refusing to overwrite existing --out") {
		t.Fatalf("expected existing out refusal, got %v", err)
	}
}

func TestRunOuroborosExportCorpusRejectsOutOutsideRepo(t *testing.T) {
	err := runOuroborosExportCorpus(ouroborosExportOptions{
		RepoRoot:   repoRootForTest(t),
		OutDir:     filepath.Join(t.TempDir(), "canonical-export"),
		CorpusPath: filepath.Join("examples", "ouroboros-corpus", "fixtures.v1.json"),
		FixtureApp: filepath.Join("examples", "ouroboros-corpus"),
		DocsApp:    filepath.Join("examples", "gosx-docs"),
	})
	if err == nil || !strings.Contains(err.Error(), "inside repo root") {
		t.Fatalf("expected out containment rejection, got %v", err)
	}
}

func TestOuroborosInventoryOmitsCleanArchivePath(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "go.mod"), "module example.com/clean\n\ngo 1.23\n")
	mustWriteFile(t, filepath.Join(root, "main.go"), "package main\n\nfunc main() {}\n")
	mustWriteFile(t, filepath.Join(root, "client", "js", "bootstrap-src", "00-runtime.js"), "window.__gosx_runtime_ready = true;\n")
	runGitCommandForTest(t, root, "init")
	runGitCommandForTest(t, root, "config", "user.email", "test@example.invalid")
	runGitCommandForTest(t, root, "config", "user.name", "test")
	runGitCommandForTest(t, root, "add", ".")
	runGitCommandForTest(t, root, "commit", "-m", "base")

	out := filepath.Join(root, "build", "ouroboros", "source-inventory.json")
	cmdOuroborosInventory([]string{
		"--root", root,
		"--out", out,
		"--artifact-root", filepath.Join(root, "build", "canonical-browser"),
		"--no-canopy",
	})
	f, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	inv, err := ouroboros.DecodeInventoryStrict(f)
	_ = f.Close()
	if err != nil {
		t.Fatal(err)
	}
	if len(inv.Overlay.UntrackedSources) != 0 {
		t.Fatalf("clean inventory recorded untracked sources: %#v", inv.Overlay.UntrackedSources)
	}
	if inv.Overlay.ArchivePath != "" {
		t.Fatalf("clean inventory archivePath = %q, want empty", inv.Overlay.ArchivePath)
	}
}

func runGitCommandForTest(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func TestResolveContainedNewRepoOutputPathRejectsSymlinkParentEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "linked-out")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	_, err := resolveContainedNewRepoOutputPath(root, filepath.Join(link, "canonical-export"))
	if err == nil || !strings.Contains(err.Error(), "escapes repo root") {
		t.Fatalf("expected symlink-parent escape rejection, got %v", err)
	}
}

func TestCopyStrictOuroborosAssetsCopiesManifestBoundAssets(t *testing.T) {
	dist := t.TempDir()
	output := t.TempDir()
	data := "bootstrap runtime"
	hash := contentHash([]byte(data))
	file := "bootstrap." + hash + ".js"
	mustWriteFile(t, filepath.Join(dist, "assets", "runtime", file), data)
	mustWriteFile(t, filepath.Join(dist, "assets", "runtime", file+".gz"), "gzip")
	manifest := &buildmanifest.Manifest{
		Runtime: buildmanifest.RuntimeAssets{
			Bootstrap: buildmanifest.HashedAsset{File: file, Hash: hash, Size: int64(len(data))},
		},
	}
	app := &ouroborosExportBuiltApp{Name: "fixture", DistDir: dist, Manifest: manifest}
	refs := map[string]*ouroborosAssetBinding{}
	if err := addOuroborosAssetBinding(refs, "/gosx/assets/runtime/"+file, "fixture", app); err != nil {
		t.Fatal(err)
	}
	assets, filtered, err := copyStrictOuroborosAssets(output, refs)
	if err != nil {
		t.Fatal(err)
	}
	if len(assets) != 1 || assets[0].Ref != "/gosx/assets/runtime/"+file || assets[0].Bucket != "runtime" {
		t.Fatalf("unexpected copied assets: %#v", assets)
	}
	if filtered.Runtime.Bootstrap.File != file {
		t.Fatalf("filtered manifest missed bootstrap: %#v", filtered.Runtime.Bootstrap)
	}
	for _, rel := range []string{
		"gosx/assets/runtime/" + file,
		"gosx/assets/runtime/" + file + ".gz",
		"assets/runtime/" + file,
		"assets/runtime/" + file + ".gz",
	} {
		if _, err := os.Stat(filepath.Join(output, rel)); err != nil {
			t.Fatalf("expected copied %s: %v", rel, err)
		}
	}
}

func TestCopyStrictOuroborosAssetsCopiesTextlayoutAsset(t *testing.T) {
	dist := t.TempDir()
	output := t.TempDir()
	data := "textlayout runtime"
	hash := contentHash([]byte(data))
	file := "bootstrap-feature-textlayout." + hash + ".js"
	mustWriteFile(t, filepath.Join(dist, "assets", "runtime", file), data)
	manifest := &buildmanifest.Manifest{
		Runtime: buildmanifest.RuntimeAssets{
			BootstrapFeatureTextlayout: buildmanifest.HashedAsset{File: file, Hash: hash, Size: int64(len(data))},
		},
	}
	app := &ouroborosExportBuiltApp{Name: "fixture", DistDir: dist, Manifest: manifest}
	refs := map[string]*ouroborosAssetBinding{}
	ref := "/gosx/assets/runtime/" + file
	if err := addOuroborosAssetBinding(refs, ref, "fixture", app); err != nil {
		t.Fatal(err)
	}
	assets, filtered, err := copyStrictOuroborosAssets(output, refs)
	if err != nil {
		t.Fatal(err)
	}
	if len(assets) != 1 || assets[0].Ref != ref || assets[0].Bucket != "runtime" {
		t.Fatalf("unexpected copied textlayout assets: %#v", assets)
	}
	if filtered.Runtime.BootstrapFeatureTextlayout.File != file {
		t.Fatalf("filtered manifest missed textlayout: %#v", filtered.Runtime.BootstrapFeatureTextlayout)
	}
	if _, err := os.Stat(filepath.Join(output, "assets", "runtime", file)); err != nil {
		t.Fatalf("expected canonical textlayout asset: %v", err)
	}
	if _, err := os.Stat(filepath.Join(output, "gosx", "assets", "runtime", file)); err != nil {
		t.Fatalf("expected served textlayout asset: %v", err)
	}
}

func TestStrictOuroborosScene3DOverlayLabelsDiscoverTextlayoutAssetFromContract(t *testing.T) {
	dist := t.TempDir()
	output := t.TempDir()
	textlayoutData := "textlayout runtime"
	textlayoutHash := contentHash([]byte(textlayoutData))
	textlayoutFile := "bootstrap-feature-textlayout." + textlayoutHash + ".js"
	sceneData := "scene runtime"
	sceneHash := contentHash([]byte(sceneData))
	sceneFile := "bootstrap-feature-scene3d." + sceneHash + ".js"
	enginesData := "engines runtime"
	enginesHash := contentHash([]byte(enginesData))
	enginesFile := "bootstrap-feature-engines." + enginesHash + ".js"
	bootstrapRuntimeData := "bootstrap runtime"
	bootstrapRuntimeHash := contentHash([]byte(bootstrapRuntimeData))
	bootstrapRuntimeFile := "bootstrap-runtime." + bootstrapRuntimeHash + ".js"
	commandData := "scene command runtime"
	commandHash := contentHash([]byte(commandData))
	commandFile := "bootstrap-feature-scene3d-command." + commandHash + ".js"
	webgpuData := "scene webgpu runtime"
	webgpuHash := contentHash([]byte(webgpuData))
	webgpuFile := "bootstrap-feature-scene3d-webgpu." + webgpuHash + ".js"
	webglData := "scene webgl runtime"
	webglHash := contentHash([]byte(webglData))
	webglFile := "bootstrap-feature-scene3d-webgl." + webglHash + ".js"
	gltfData := "scene gltf runtime"
	gltfHash := contentHash([]byte(gltfData))
	gltfFile := "bootstrap-feature-scene3d-gltf." + gltfHash + ".js"
	animationData := "scene animation runtime"
	animationHash := contentHash([]byte(animationData))
	animationFile := "bootstrap-feature-scene3d-animation." + animationHash + ".js"
	mustWriteFile(t, filepath.Join(dist, "assets", "runtime", textlayoutFile), textlayoutData)
	mustWriteFile(t, filepath.Join(dist, "assets", "runtime", sceneFile), sceneData)
	mustWriteFile(t, filepath.Join(dist, "assets", "runtime", enginesFile), enginesData)
	mustWriteFile(t, filepath.Join(dist, "assets", "runtime", bootstrapRuntimeFile), bootstrapRuntimeData)
	mustWriteFile(t, filepath.Join(dist, "assets", "runtime", commandFile), commandData)
	mustWriteFile(t, filepath.Join(dist, "assets", "runtime", webgpuFile), webgpuData)
	mustWriteFile(t, filepath.Join(dist, "assets", "runtime", webglFile), webglData)
	mustWriteFile(t, filepath.Join(dist, "assets", "runtime", gltfFile), gltfData)
	mustWriteFile(t, filepath.Join(dist, "assets", "runtime", animationFile), animationData)
	manifest := &buildmanifest.Manifest{
		Runtime: buildmanifest.RuntimeAssets{
			BootstrapRuntime:                 buildmanifest.HashedAsset{File: bootstrapRuntimeFile, Hash: bootstrapRuntimeHash, Size: int64(len(bootstrapRuntimeData))},
			BootstrapFeatureEngines:          buildmanifest.HashedAsset{File: enginesFile, Hash: enginesHash, Size: int64(len(enginesData))},
			BootstrapFeatureTextlayout:       buildmanifest.HashedAsset{File: textlayoutFile, Hash: textlayoutHash, Size: int64(len(textlayoutData))},
			BootstrapFeatureScene3D:          buildmanifest.HashedAsset{File: sceneFile, Hash: sceneHash, Size: int64(len(sceneData))},
			BootstrapFeatureScene3DCommand:   buildmanifest.HashedAsset{File: commandFile, Hash: commandHash, Size: int64(len(commandData))},
			BootstrapFeatureScene3DWebGPU:    buildmanifest.HashedAsset{File: webgpuFile, Hash: webgpuHash, Size: int64(len(webgpuData))},
			BootstrapFeatureScene3DWebGL:     buildmanifest.HashedAsset{File: webglFile, Hash: webglHash, Size: int64(len(webglData))},
			BootstrapFeatureScene3DGLTF:      buildmanifest.HashedAsset{File: gltfFile, Hash: gltfHash, Size: int64(len(gltfData))},
			BootstrapFeatureScene3DAnimation: buildmanifest.HashedAsset{File: animationFile, Hash: animationHash, Size: int64(len(animationData))},
		},
	}
	if err := writeJSONFile(filepath.Join(dist, "build.json"), manifest); err != nil {
		t.Fatal(err)
	}
	island.ResetBuildManifestCache()
	t.Cleanup(island.ResetManifestRoot)
	app := &ouroborosExportBuiltApp{Name: "fixture", DistDir: dist, Manifest: manifest}
	textlayoutRef := "/gosx/assets/runtime/" + textlayoutFile

	webApp := server.New()
	webApp.SetRuntimeRoot(dist)
	webApp.Page("GET /scene/basic", func(ctx *server.Context) gosx.Node {
		return ctx.Engine(engine.Config{
			Name:  "GoSXScene3D",
			Kind:  engine.KindSurface,
			Props: json.RawMessage(`{"scene":{"labels":[{"id":"caption","text":"R08 overlay"}]},"width":640,"height":360}`),
		}, gosx.El("p", gosx.Text("Loading scene")))
	})
	req := httptest.NewRequest(http.MethodGet, "/scene/basic", nil)
	w := httptest.NewRecorder()
	webApp.Build().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("render R08 route: got HTTP %d", w.Code)
	}
	html := w.Body.String()

	if !strings.Contains(html, `"bootstrapFeatureTextLayoutPath":"`+textlayoutRef+`"`) {
		t.Fatalf("R08 HTML missed exact textlayout contract key: %s", html)
	}
	if strings.Contains(html, `<script src="`+textlayoutRef+`"`) ||
		strings.Contains(html, `<script defer src="`+textlayoutRef+`"`) ||
		strings.Contains(html, `rel="preload" href="`+textlayoutRef+`"`) {
		t.Fatalf("R08 HTML eagerly loads textlayout chunk: %s", html)
	}

	routeRefs := map[string]struct{}{}
	addExportRuntimeAssetRefs(routeRefs, html)
	if _, ok := routeRefs[textlayoutRef]; !ok {
		t.Fatalf("R08 discovery missed textlayout ref; got %#v", sortedExportRuntimeAssetRefs(routeRefs))
	}
	assetRefs := map[string]*ouroborosAssetBinding{}
	for ref := range routeRefs {
		if err := addOuroborosAssetBinding(assetRefs, ref, "fixture", app); err != nil {
			t.Fatal(err)
		}
	}

	assets, filtered, err := copyStrictOuroborosAssets(output, assetRefs)
	if err != nil {
		t.Fatal(err)
	}
	if filtered.Runtime.BootstrapFeatureTextlayout.File != textlayoutFile {
		t.Fatalf("filtered build manifest missed textlayout: %#v", filtered.Runtime.BootstrapFeatureTextlayout)
	}
	if err := writeJSONFile(filepath.Join(output, "build.json"), filtered); err != nil {
		t.Fatal(err)
	}
	provenance := ouroborosExportProvenance{
		SchemaVersion:   ouroborosExportSchemaVersion,
		ContractVersion: "O0.2",
		CorpusID:        "gosx-ouroboros-o0.2-v1",
		Routes: []ouroborosExportRoute{{
			ID:       "R08",
			Path:     "/scene/basic",
			File:     "scene/basic/index.html",
			Producer: "fixture",
		}},
		AssetRefs: assets,
	}
	if err := writeJSONFile(filepath.Join(output, "_ouroboros", "export-corpus.json"), provenance); err != nil {
		t.Fatal(err)
	}

	var persisted ouroborosExportProvenance
	mustDecodeJSONFileForTest(t, filepath.Join(output, "_ouroboros", "export-corpus.json"), &persisted)
	if !ouroborosAssetRefsInclude(persisted.AssetRefs, textlayoutRef) {
		t.Fatalf("export-corpus.json missed textlayout ref: %#v", persisted.AssetRefs)
	}
	var persistedBuild buildmanifest.Manifest
	mustDecodeJSONFileForTest(t, filepath.Join(output, "build.json"), &persistedBuild)
	if persistedBuild.Runtime.BootstrapFeatureTextlayout.File != textlayoutFile {
		t.Fatalf("build.json missed textlayout asset: %#v", persistedBuild.Runtime.BootstrapFeatureTextlayout)
	}
	for _, rel := range []string{
		filepath.Join("assets", "runtime", textlayoutFile),
		filepath.Join("gosx", "assets", "runtime", textlayoutFile),
	} {
		if _, err := os.Stat(filepath.Join(output, rel)); err != nil {
			t.Fatalf("expected copied textlayout output %s: %v", rel, err)
		}
	}
}

func TestStrictOuroborosR08AriaLabelDoesNotAdvertiseTextlayout(t *testing.T) {
	webApp := server.New()
	webApp.Page("GET /scene/basic", func(ctx *server.Context) gosx.Node {
		return ctx.Engine(engine.Config{
			Name:  "GoSXScene3D",
			Kind:  engine.KindSurface,
			Props: json.RawMessage(`{"label":"Ouroboros basic Scene3D","width":640,"height":360}`),
		}, gosx.El("p", gosx.Text("Loading scene")))
	})
	req := httptest.NewRequest(http.MethodGet, "/scene/basic", nil)
	w := httptest.NewRecorder()
	webApp.Build().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("render canonical R08 shape: got HTTP %d", w.Code)
	}
	body := w.Body.String()
	if strings.Contains(body, "bootstrapFeatureTextLayoutPath") {
		t.Fatalf("ARIA-only canonical R08 shape advertised textlayout: %s", body)
	}
	refs := map[string]struct{}{}
	addExportRuntimeAssetRefs(refs, body)
	if _, ok := refs["/gosx/bootstrap-feature-textlayout.js"]; ok {
		t.Fatalf("ARIA-only canonical R08 shape exported textlayout ref: %#v", sortedExportRuntimeAssetRefs(refs))
	}
}

func TestAddOuroborosAssetBindingRejectsMissingTextlayoutAsset(t *testing.T) {
	dist := t.TempDir()
	data := "textlayout runtime"
	hash := contentHash([]byte(data))
	file := "bootstrap-feature-textlayout." + hash + ".js"
	manifest := &buildmanifest.Manifest{
		Runtime: buildmanifest.RuntimeAssets{
			BootstrapFeatureTextlayout: buildmanifest.HashedAsset{File: file, Hash: hash, Size: int64(len(data))},
		},
	}
	app := &ouroborosExportBuiltApp{Name: "fixture", DistDir: dist, Manifest: manifest}
	err := addOuroborosAssetBinding(map[string]*ouroborosAssetBinding{}, "/gosx/assets/runtime/"+file, "fixture", app)
	if err == nil {
		t.Fatal("expected missing textlayout asset rejection")
	}
}

func ouroborosAssetRefsInclude(refs []ouroborosExportAssetRef, want string) bool {
	for _, ref := range refs {
		if ref.Ref == want {
			return true
		}
	}
	return false
}

func TestCopyStrictOuroborosAssetsRejectsSymlink(t *testing.T) {
	dist := t.TempDir()
	target := filepath.Join(dist, "assets", "runtime", "bootstrap.real.js")
	mustWriteFile(t, target, "real")
	hash := contentHash([]byte("real"))
	file := "bootstrap." + hash + ".js"
	link := filepath.Join(dist, "assets", "runtime", file)
	if err := os.Symlink("bootstrap.real.js", link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	manifest := &buildmanifest.Manifest{
		Runtime: buildmanifest.RuntimeAssets{
			Bootstrap: buildmanifest.HashedAsset{File: file, Hash: hash, Size: 4},
		},
	}
	app := &ouroborosExportBuiltApp{Name: "fixture", DistDir: dist, Manifest: manifest}
	refs := map[string]*ouroborosAssetBinding{}
	err := addOuroborosAssetBinding(refs, "/gosx/assets/runtime/"+file, "fixture", app)
	if err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("expected symlink rejection, got %v", err)
	}
}

func TestAddOuroborosAssetBindingRejectsHashTamper(t *testing.T) {
	dist := t.TempDir()
	data := "bootstrap runtime"
	hash := contentHash([]byte(data))
	file := "bootstrap." + hash + ".js"
	mustWriteFile(t, filepath.Join(dist, "assets", "runtime", file), data+" tampered")
	manifest := &buildmanifest.Manifest{
		Runtime: buildmanifest.RuntimeAssets{
			Bootstrap: buildmanifest.HashedAsset{File: file, Hash: hash, Size: int64(len(data + " tampered"))},
		},
	}
	app := &ouroborosExportBuiltApp{Name: "fixture", DistDir: dist, Manifest: manifest}
	err := addOuroborosAssetBinding(map[string]*ouroborosAssetBinding{}, "/gosx/assets/runtime/"+file, "fixture", app)
	if err == nil || !strings.Contains(err.Error(), "content hash") {
		t.Fatalf("expected content hash rejection, got %v", err)
	}
}

func TestAddOuroborosAssetBindingRejectsSameBasenameWrongBucket(t *testing.T) {
	dist := t.TempDir()
	data := "asset"
	hash := contentHash([]byte(data))
	file := "shared." + hash + ".js"
	mustWriteFile(t, filepath.Join(dist, "assets", "runtime", file), data)
	manifest := &buildmanifest.Manifest{
		Islands: []buildmanifest.IslandAsset{{
			Name:        "shared",
			Format:      "bin",
			HashedAsset: buildmanifest.HashedAsset{File: file, Hash: hash, Size: int64(len(data))},
		}},
	}
	app := &ouroborosExportBuiltApp{Name: "fixture", DistDir: dist, Manifest: manifest}
	err := addOuroborosAssetBinding(map[string]*ouroborosAssetBinding{}, "/gosx/assets/runtime/"+file, "fixture", app)
	if err == nil || !strings.Contains(err.Error(), "not bound") {
		t.Fatalf("expected bucket binding rejection, got %v", err)
	}
}

func TestAddOuroborosAssetBindingRejectsConflictingSharedRef(t *testing.T) {
	fixtureDist := t.TempDir()
	docsDist := t.TempDir()
	fixtureData := "fixture"
	docsData := "docs"
	fixtureHash := contentHash([]byte(fixtureData))
	file := "bootstrap." + fixtureHash + ".js"
	mustWriteFile(t, filepath.Join(fixtureDist, "assets", "runtime", file), fixtureData)
	mustWriteFile(t, filepath.Join(docsDist, "assets", "runtime", file), docsData)
	fixture := &ouroborosExportBuiltApp{Name: "fixture", DistDir: fixtureDist, Manifest: &buildmanifest.Manifest{Runtime: buildmanifest.RuntimeAssets{Bootstrap: buildmanifest.HashedAsset{File: file, Hash: fixtureHash, Size: int64(len(fixtureData))}}}}
	docs := &ouroborosExportBuiltApp{Name: "docs", DistDir: docsDist, Manifest: &buildmanifest.Manifest{Runtime: buildmanifest.RuntimeAssets{Bootstrap: buildmanifest.HashedAsset{File: file, Hash: contentHash([]byte(docsData)), Size: int64(len(docsData))}}}}
	refs := map[string]*ouroborosAssetBinding{}
	if err := addOuroborosAssetBinding(refs, "/gosx/assets/runtime/"+file, "fixture", fixture); err != nil {
		t.Fatal(err)
	}
	err := addOuroborosAssetBinding(refs, "/gosx/assets/runtime/"+file, "docs", docs)
	if err == nil || (!strings.Contains(err.Error(), "different content") && !strings.Contains(err.Error(), "manifest hash")) {
		t.Fatalf("expected shared-ref conflict, got %v", err)
	}
}

func TestResolveCanonicalAppPathRejectsEscapesAndAliases(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "examples", "ouroboros-corpus", "main.go"), "package main\n")
	mustWriteFile(t, filepath.Join(root, "outside", "main.go"), "package main\n")
	if _, err := resolveCanonicalAppPath(root, filepath.Join("examples", "ouroboros-corpus"), filepath.Join("examples", "ouroboros-corpus")); err != nil {
		t.Fatalf("expected canonical app path: %v", err)
	}
	if _, err := resolveCanonicalAppPath(root, filepath.Join("..", filepath.Base(root), "outside"), filepath.Join("examples", "ouroboros-corpus")); err == nil {
		t.Fatal("expected alias rejection")
	}
}

func TestRejectSymlinksAndValidateOuroborosProducerRootRequiresDistLayout(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{
		"dist/build.json",
		"dist/export.json",
		"dist/_ouroboros/export-corpus.json",
	} {
		mustWriteFile(t, filepath.Join(root, rel), "{}")
	}
	paths := map[string]string{"R00": "/static", "R01": "/lite", "R02": "/island/counter", "R03": "/islands/kitchen", "R04": "/action/form", "R05": "/canvas-board", "R06": "/hub/echo", "R07": "/video-sync", "R08": "/scene/basic", "R09A": "/navigation/a", "R09B": "/navigation/b", "R10": "/demos/water"}
	for _, id := range canonicalOuroborosExportIDs {
		mustWriteFile(t, filepath.Join(root, "dist", buildmanifest.ExportFilePath(paths[id])), "<html></html>")
		mustWriteFile(t, filepath.Join(root, "dist", "_ouroboros", "raw-html", id+".html"), "<html></html>")
	}
	if err := rejectSymlinksAndValidateOuroborosProducerRoot(root, nil, nil); err != nil {
		t.Fatalf("expected valid producer root: %v", err)
	}
	mustWriteFile(t, filepath.Join(root, "export.json"), "{}")
	if err := rejectSymlinksAndValidateOuroborosProducerRoot(root, nil, nil); err == nil || !strings.Contains(err.Error(), "unexpected file") {
		t.Fatalf("expected root-level export rejection, got %v", err)
	}
}

func TestRejectSymlinksAndValidateOuroborosProducerRootAllowsCanonicalAssets(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{
		"dist/build.json",
		"dist/export.json",
		"dist/_ouroboros/export-corpus.json",
	} {
		mustWriteFile(t, filepath.Join(root, rel), "{}")
	}
	paths := map[string]string{"R00": "/static", "R01": "/lite", "R02": "/island/counter", "R03": "/islands/kitchen", "R04": "/action/form", "R05": "/canvas-board", "R06": "/hub/echo", "R07": "/video-sync", "R08": "/scene/basic", "R09A": "/navigation/a", "R09B": "/navigation/b", "R10": "/demos/water"}
	for _, id := range canonicalOuroborosExportIDs {
		mustWriteFile(t, filepath.Join(root, "dist", buildmanifest.ExportFilePath(paths[id])), "<html></html>")
		mustWriteFile(t, filepath.Join(root, "dist", "_ouroboros", "raw-html", id+".html"), "<html></html>")
	}
	asset := ouroborosExportAssetRef{Ref: "/gosx/assets/runtime/bootstrap.11111111.js", Bucket: "runtime", File: "bootstrap.11111111.js"}
	mustWriteFile(t, filepath.Join(root, "dist", "assets", "runtime", "bootstrap.11111111.js"), "asset")
	mustWriteFile(t, filepath.Join(root, "dist", "gosx", "assets", "runtime", "bootstrap.11111111.js"), "asset")
	if err := rejectSymlinksAndValidateOuroborosProducerRoot(root, []ouroborosExportAssetRef{asset}, nil); err != nil {
		t.Fatalf("expected canonical asset layout: %v", err)
	}
}

func TestValidateGeneratedModulesPackageCurrentRejectsStale(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "go.mod"), "module example.com/app\n\ngo 1.23\n")
	mustWriteFile(t, filepath.Join(dir, "app", "page.server.go"), "package page\n")
	mustWriteFile(t, filepath.Join(dir, "modules", "modules.go"), "// Code generated by gosx. DO NOT EDIT.\npackage modules\n\n")
	err := validateGeneratedModulesPackageCurrent(dir)
	if err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("expected stale modules rejection, got %v", err)
	}
}

func TestSourceSnapshotRejectsMutationAndAddition(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "go.mod"), "module example.com/app\n\ngo 1.23\n")
	mustWriteFile(t, filepath.Join(dir, "go.sum"), "")
	mustWriteFile(t, filepath.Join(dir, "main.go"), "package main\n")
	mustWriteFile(t, filepath.Join(dir, "modules", "modules.go"), "// Code generated by gosx. DO NOT EDIT.\npackage modules\n")
	snapshot, err := snapshotOuroborosSourceTree(dir)
	if err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(dir, "new.go"), "package main\n")
	if err := snapshot.Validate(); err == nil || !strings.Contains(err.Error(), "was added") {
		t.Fatalf("expected addition rejection, got %v", err)
	}

	snapshot, err = snapshotOuroborosSourceTree(dir)
	if err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(dir, "go.mod"), "module example.com/app\n\ngo 1.24\n")
	if err := snapshot.Validate(); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("expected mutation rejection, got %v", err)
	}
}

func TestRunBuildWithCanonicalOptionsValidatesDependenciesReadonly(t *testing.T) {
	dir := t.TempDir()
	goMod := "module example.com/app\n\ngo 1.23\n\nrequire rsc.io/quote v1.5.2\n"
	goSum := ""
	mustWriteFile(t, filepath.Join(dir, "go.mod"), goMod)
	mustWriteFile(t, filepath.Join(dir, "go.sum"), goSum)
	mustWriteFile(t, filepath.Join(dir, "main.go"), "package main\n\nimport _ \"rsc.io/quote\"\n\nfunc main() {}\n")
	err := RunBuildWithOptions(dir, BuildOptions{
		SkipModuleSync:      true,
		ReadonlyModuleDeps:  true,
		SkipBuildHooks:      true,
		SkipStaticPrerender: true,
	})
	if err == nil || !strings.Contains(err.Error(), "readonly") {
		t.Fatalf("expected readonly dependency failure, got %v", err)
	}
	if got := mustReadFile(t, filepath.Join(dir, "go.mod")); got != goMod {
		t.Fatalf("go.mod changed:\n%s", got)
	}
	if got := mustReadFile(t, filepath.Join(dir, "go.sum")); got != goSum {
		t.Fatalf("go.sum changed:\n%s", got)
	}
}

func TestRunBuildWithCanonicalOptionsSkipsConfiguredHooks(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "go.mod"), "module example.com/app\n\ngo 1.23\n")
	mustWriteFile(t, filepath.Join(dir, "main.go"), "package main\n\nfunc main() {}\n")
	mustWriteFile(t, filepath.Join(dir, "gosx.config.json"), `{"build":{"hooks":{"pre":["printf ran > hook-ran"],"post":["printf ran > hook-post"]}}}`)
	err := RunBuildWithOptions(dir, BuildOptions{
		SkipModuleSync:      true,
		ReadonlyModuleDeps:  true,
		SkipBuildHooks:      true,
		SkipStaticPrerender: true,
		SceneBudgetPath:     filepath.Join(dir, "missing-scene-budget.json"),
	})
	if err == nil {
		t.Fatal("expected build to stop at missing scene budget")
	}
	for _, name := range []string{"hook-ran", "hook-post"} {
		if _, statErr := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(statErr) {
			t.Fatalf("canonical export build ran configured hook %s: %v", name, statErr)
		}
	}
}

func TestResolveGoSXModuleRootWithFlagsSetsReadonlyModuleEnv(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	record := filepath.Join(dir, "env.txt")
	goPath := filepath.Join(binDir, "go")
	mustWriteFile(t, goPath, "#!/bin/sh\nprintf 'GOFLAGS=%s\\nGOWORK=%s\\n' \"$GOFLAGS\" \"$GOWORK\" > \"$GOSX_TEST_ENV_OUT\"\nprintf '%s\\n' \"$PWD\"\n")
	if err := os.Chmod(goPath, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GOSX_TEST_ENV_OUT", record)

	root, err := resolveGoSXModuleRootWithFlags(dir, "-mod=readonly -buildvcs=false")
	if err != nil {
		t.Fatal(err)
	}
	if root != dir {
		t.Fatalf("root = %q, want %q", root, dir)
	}
	got := mustReadFile(t, record)
	if !strings.Contains(got, "GOFLAGS=-mod=readonly -buildvcs=false") || !strings.Contains(got, "GOWORK=off") {
		t.Fatalf("go list env was not canonical readonly:\n%s", got)
	}
}

func TestValidateOuroborosR10WaterHTML(t *testing.T) {
	valid := `<main class="water-demo" data-demo-slug="water"><p>Flagship GoSX Scene3D water</p></main>`
	if err := validateOuroborosR10WaterHTML(valid); err != nil {
		t.Fatalf("expected valid water HTML: %v", err)
	}
	if err := validateOuroborosR10WaterHTML(`<main data-fixture-local-copy></main>`); err == nil || !strings.Contains(err.Error(), "fixture-local") {
		t.Fatalf("expected fixture-local rejection, got %v", err)
	}
	if err := validateOuroborosR10WaterHTML(`<main class="water-demo"></main>`); err == nil || !strings.Contains(err.Error(), "missing external water marker") {
		t.Fatalf("expected marker rejection, got %v", err)
	}
}

func TestCollectOuroborosRouteResourcesFetchesNestedAndDynamic(t *testing.T) {
	files := map[string][]byte{
		"/media/ouroboros-placeholder.mp4": []byte("video"),
		"/water/style.css":                 []byte(`.x{background:url("./tiles.jpg")}`),
		"/water/tiles.jpg":                 []byte("tiles"),
		"/water/models/duck/Duck.gltf":     []byte(`{"buffers":[{"uri":"Duck.bin"}],"images":[{"uri":"DuckCM.png"}]}`),
		"/water/models/duck/Duck.bin":      []byte("bin"),
		"/water/models/duck/DuckCM.png":    []byte("png"),
		"/water/srcset-small.png":          []byte("small"),
		"/water/srcset-large.png":          []byte("large"),
		"/_ouroboros/islands/Counter.json": []byte(`{"program":"counter"}`),
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		switch filepath.Ext(r.URL.Path) {
		case ".mp4":
			w.Header().Set("Content-Type", "video/mp4")
		case ".css":
			w.Header().Set("Content-Type", "text/css")
		case ".gltf", ".json":
			w.Header().Set("Content-Type", "application/json")
		}
		_, _ = w.Write(body)
	}))
	defer server.Close()

	html := `<link href="/water/style.css"><style>/* source: /layout.css */ .x{background:url("/water/tiles.jpg")}</style><form action="/action/form"></form><img srcset="/water/srcset-small.png 1x, /water/srcset-large.png 2x"><video src="/media/ouroboros-placeholder.mp4"></video><script type="application/json">{"gosx-manifest":{"hubs":[{"path":"/_ouroboros/hub/echo"}]},"programRef":"/_ouroboros/islands/Counter.json","sync":"/_ouroboros/video-sync","src":"/water/models/duck/Duck.gltf"}</script>`
	refs := map[string]*ouroborosResourceBinding{}
	dynamics := map[string]*ouroborosDynamicBinding{}
	app := &ouroborosExportBuiltApp{Name: "fixture", BaseURL: server.URL}
	if err := collectOuroborosRouteResources(server.Client(), app, "fixture", "R07", "/video-sync", html, t.TempDir(), refs, dynamics); err != nil {
		t.Fatal(err)
	}
	for _, ref := range []string{"/media/ouroboros-placeholder.mp4", "/water/style.css", "/water/tiles.jpg", "/water/models/duck/Duck.gltf", "/water/models/duck/Duck.bin", "/water/models/duck/DuckCM.png", "/water/srcset-small.png", "/water/srcset-large.png", "/_ouroboros/islands/Counter.json"} {
		if refs[ref] == nil {
			t.Fatalf("missing resource %s; got %#v", ref, sortedOuroborosResourceRefs(refs))
		}
	}
	if !refs["/water/models/duck/Duck.bin"].Parents[canonicalOuroborosResourceID("/water/models/duck/Duck.gltf")] {
		t.Fatalf("nested glTF parent was not recorded as resource ID: %#v", refs["/water/models/duck/Duck.bin"].Parents)
	}
	if refs["/layout.css"] != nil {
		t.Fatalf("inert CSS source label was misclassified as transfer: %#v", refs["/layout.css"])
	}
	if dynamics["/_ouroboros/video-sync"] == nil {
		t.Fatalf("missing dynamic endpoint: %#v", sortedOuroborosDynamicRefs(dynamics))
	}
	if dynamics["/_ouroboros/video-sync"].Kind != "sync" {
		t.Fatalf("sync endpoint kind = %q", dynamics["/_ouroboros/video-sync"].Kind)
	}
	if dynamics["/action/form"] == nil || dynamics["/action/form"].Kind != "action" {
		t.Fatalf("action endpoint was not typed dynamic: %#v", sortedOuroborosDynamicRefs(dynamics))
	}
	if dynamics["/_ouroboros/hub/echo"] == nil || dynamics["/_ouroboros/hub/echo"].Kind != "hub" {
		t.Fatalf("hub endpoint was not typed dynamic: %#v", sortedOuroborosDynamicRefs(dynamics))
	}
	resources := sortedOuroborosResourceRefs(refs)
	manifest := buildOuroborosResourceManifest(ouroborosCorpusManifest{ContractVersion: "O0.2", CorpusID: "gosx-ouroboros-o0.2-v1"}, resources, sortedOuroborosDynamicRefs(dynamics))
	if len(manifest.Resources) != len(resources) || len(manifest.DynamicEndpoints) != 3 {
		t.Fatalf("unexpected resource manifest: %#v", manifest)
	}
}

func TestCollectOuroborosRouteResourcesResolvesBareRelativeHTMLResources(t *testing.T) {
	files := map[string][]byte{
		"/demos/app.js":    []byte("js"),
		"/demos/small.png": []byte("small"),
		"/demos/large.png": []byte("large"),
	}
	requested := map[string]bool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested[r.URL.Path] = true
		body, ok := files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(body)
	}))
	defer server.Close()

	refs := map[string]*ouroborosResourceBinding{}
	app := &ouroborosExportBuiltApp{Name: "fixture", BaseURL: server.URL}
	html := `<script src="app.js"></script><img srcset="small.png 1x, large.png 2x">`
	if err := collectOuroborosRouteResources(server.Client(), app, "fixture", "R10", "/demos/water", html, t.TempDir(), refs, map[string]*ouroborosDynamicBinding{}); err != nil {
		t.Fatal(err)
	}
	for _, ref := range []string{"/demos/app.js", "/demos/small.png", "/demos/large.png"} {
		if refs[ref] == nil || !requested[ref] {
			t.Fatalf("missing route-relative resource %s; refs=%#v requested=%#v", ref, sortedOuroborosResourceRefs(refs), requested)
		}
	}
}

func TestDiscoverOuroborosResourceCandidatesRejectsQueryFragmentAndPercentEncoding(t *testing.T) {
	for _, tc := range []struct {
		name string
		html string
	}{
		{name: "query", html: `<script src="/asset.js?v=1"></script>`},
		{name: "fragment", html: `<script src="/asset.js#frag"></script>`},
		{name: "percent", html: `<script src="/asset%2ejs"></script>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := discoverOuroborosResourceCandidates(tc.html, "/")
			if err == nil || (!strings.Contains(err.Error(), "query or fragment") && !strings.Contains(err.Error(), "percent-encoded")) {
				t.Fatalf("expected unsafe URL rejection, got %v", err)
			}
		})
	}
}

func TestDiscoverOuroborosResourceCandidatesIgnoresSameDocumentQueryAndFragmentLinks(t *testing.T) {
	candidates, err := discoverOuroborosResourceCandidates(`<a href="?quality=hero"></a><a href="#top"></a>`, "/demos")
	if err != nil {
		t.Fatalf("same-document links should be ignored: %v", err)
	}
	if len(candidates) != 0 {
		t.Fatalf("same-document links produced transfer candidates: %#v", candidates)
	}
	_, err = discoverOuroborosResourceCandidates(`<script src="/asset.js?v=1"></script>`, "/")
	if err == nil || !strings.Contains(err.Error(), "query or fragment") {
		t.Fatalf("expected real transfer query rejection, got %v", err)
	}
	_, err = discoverOuroborosResourceCandidates(`<form action="?mutate=1"></form>`, "/")
	if err == nil || !strings.Contains(err.Error(), "query or fragment") {
		t.Fatalf("expected query-only action rejection, got %v", err)
	}
}

func TestCollectOuroborosRouteResourcesPreservesDirectAndNestedParents(t *testing.T) {
	files := map[string][]byte{
		"/water/a.png": []byte("png"),
		"/water/z.gltf": []byte(`{
			"images":[{"uri":"a.png"}]
		}`),
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if filepath.Ext(r.URL.Path) == ".gltf" {
			w.Header().Set("Content-Type", "model/gltf+json")
		}
		_, _ = w.Write(body)
	}))
	defer server.Close()

	refs := map[string]*ouroborosResourceBinding{}
	app := &ouroborosExportBuiltApp{Name: "fixture", BaseURL: server.URL}
	html := `<img src="/water/a.png"><script type="application/json">{"src":"/water/z.gltf"}</script>`
	if err := collectOuroborosRouteResources(server.Client(), app, "fixture", "R10", "/demos/water", html, t.TempDir(), refs, map[string]*ouroborosDynamicBinding{}); err != nil {
		t.Fatal(err)
	}
	image := refs["/water/a.png"]
	if image == nil {
		t.Fatalf("missing direct image resource: %#v", sortedOuroborosResourceRefs(refs))
	}
	parentID := canonicalOuroborosResourceID("/water/z.gltf")
	if !image.Parents[parentID] {
		t.Fatalf("direct+nested duplicate lost parent %s: %#v", parentID, image.Parents)
	}
}

func TestCollectOuroborosRouteResourcesRejectsRedirectExternalAndConflict(t *testing.T) {
	t.Run("redirect", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/asset.js", http.StatusFound)
		}))
		defer server.Close()
		err := collectOuroborosRouteResources(server.Client(), &ouroborosExportBuiltApp{Name: "fixture", BaseURL: server.URL}, "fixture", "R01", "/lite", `<script src="/asset.js"></script>`, t.TempDir(), map[string]*ouroborosResourceBinding{}, map[string]*ouroborosDynamicBinding{})
		if err == nil || !strings.Contains(err.Error(), "redirected") {
			t.Fatalf("expected redirect rejection, got %v", err)
		}
	})
	t.Run("external", func(t *testing.T) {
		server := httptest.NewServer(http.NotFoundHandler())
		defer server.Close()
		err := collectOuroborosRouteResources(server.Client(), &ouroborosExportBuiltApp{Name: "fixture", BaseURL: server.URL}, "fixture", "R01", "/lite", `<img src="https://example.com/a.png">`, t.TempDir(), map[string]*ouroborosResourceBinding{}, map[string]*ouroborosDynamicBinding{})
		if err == nil || !strings.Contains(err.Error(), "external resource") {
			t.Fatalf("expected external rejection, got %v", err)
		}
	})
	t.Run("conflict", func(t *testing.T) {
		count := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			count++
			_, _ = fmt.Fprintf(w, "body-%d", count)
		}))
		defer server.Close()
		refs := map[string]*ouroborosResourceBinding{}
		app := &ouroborosExportBuiltApp{Name: "fixture", BaseURL: server.URL}
		if err := collectOuroborosRouteResources(server.Client(), app, "fixture", "R01", "/lite", `<script src="/asset.js"></script>`, t.TempDir(), refs, map[string]*ouroborosDynamicBinding{}); err != nil {
			t.Fatal(err)
		}
		err := collectOuroborosRouteResources(server.Client(), app, "fixture", "R02", "/island/counter", `<script src="/asset.js"></script>`, t.TempDir(), refs, map[string]*ouroborosDynamicBinding{})
		if err == nil || !strings.Contains(err.Error(), "conflicting duplicate content") {
			t.Fatalf("expected conflict rejection, got %v", err)
		}
	})
	t.Run("producer", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("same"))
		}))
		defer server.Close()
		refs := map[string]*ouroborosResourceBinding{}
		app := &ouroborosExportBuiltApp{Name: "fixture", BaseURL: server.URL}
		if err := collectOuroborosRouteResources(server.Client(), app, "fixture", "R01", "/lite", `<script src="/asset.js"></script>`, t.TempDir(), refs, map[string]*ouroborosDynamicBinding{}); err != nil {
			t.Fatal(err)
		}
		err := collectOuroborosRouteResources(server.Client(), app, "docs", "R10", "/demos/water", `<script src="/asset.js"></script>`, t.TempDir(), refs, map[string]*ouroborosDynamicBinding{})
		if err == nil || !strings.Contains(err.Error(), "produced by both") {
			t.Fatalf("expected producer conflict rejection, got %v", err)
		}
	})
}

func TestCollectOuroborosRouteResourcesRejectsDynamicEndpointConflicts(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	app := &ouroborosExportBuiltApp{Name: "fixture", BaseURL: server.URL}

	t.Run("producer", func(t *testing.T) {
		dynamics := map[string]*ouroborosDynamicBinding{}
		if err := collectOuroborosRouteResources(server.Client(), app, "fixture", "R01", "/lite", `<form action="/shared-dynamic"></form>`, t.TempDir(), map[string]*ouroborosResourceBinding{}, dynamics); err != nil {
			t.Fatal(err)
		}
		err := collectOuroborosRouteResources(server.Client(), app, "docs", "R10", "/demos/water", `<form action="/shared-dynamic"></form>`, t.TempDir(), map[string]*ouroborosResourceBinding{}, dynamics)
		if err == nil || !strings.Contains(err.Error(), "produced by both") {
			t.Fatalf("expected dynamic producer conflict, got %v", err)
		}
	})

	t.Run("kind", func(t *testing.T) {
		dynamics := map[string]*ouroborosDynamicBinding{}
		if err := collectOuroborosRouteResources(server.Client(), app, "fixture", "R01", "/lite", `<script type="application/json">{"sync":"/_ouroboros/shared"}</script>`, t.TempDir(), map[string]*ouroborosResourceBinding{}, dynamics); err != nil {
			t.Fatal(err)
		}
		err := collectOuroborosRouteResources(server.Client(), app, "fixture", "R02", "/island/counter", `<script type="application/json">{"hub":"/_ouroboros/shared"}</script>`, t.TempDir(), map[string]*ouroborosResourceBinding{}, dynamics)
		if err == nil || !strings.Contains(err.Error(), "conflicting kinds") {
			t.Fatalf("expected dynamic kind conflict, got %v", err)
		}
	})

	t.Run("external", func(t *testing.T) {
		err := collectOuroborosRouteResources(server.Client(), app, "fixture", "R01", "/lite", `<script type="application/json">{"sync":"https://example.com/sync"}</script>`, t.TempDir(), map[string]*ouroborosResourceBinding{}, map[string]*ouroborosDynamicBinding{})
		if err == nil || !strings.Contains(err.Error(), "external dynamic endpoint") {
			t.Fatalf("expected external dynamic rejection, got %v", err)
		}
	})
}

func TestCollectOuroborosRouteResourcesRejectsCountAndTotalByteBounds(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("x"))
	}))
	defer server.Close()
	app := &ouroborosExportBuiltApp{Name: "fixture", BaseURL: server.URL}
	t.Run("count", func(t *testing.T) {
		refs := map[string]*ouroborosResourceBinding{}
		for i := 0; i < ouroborosMaxResources; i++ {
			ref := fmt.Sprintf("/preseed/%03d.js", i)
			refs[ref] = &ouroborosResourceBinding{Ref: ref, Bytes: 1}
		}
		err := collectOuroborosRouteResources(server.Client(), app, "fixture", "R01", "/lite", `<script src="/asset.js"></script>`, t.TempDir(), refs, map[string]*ouroborosDynamicBinding{})
		if err == nil || !strings.Contains(err.Error(), "exceeded") {
			t.Fatalf("expected count bound rejection, got %v", err)
		}
	})
	t.Run("bytes", func(t *testing.T) {
		refs := map[string]*ouroborosResourceBinding{"/preseed.js": {Ref: "/preseed.js", Bytes: ouroborosMaxTotalResourceBytes}}
		err := collectOuroborosRouteResources(server.Client(), app, "fixture", "R01", "/lite", `<script src="/asset.js"></script>`, t.TempDir(), refs, map[string]*ouroborosDynamicBinding{})
		if err == nil || !strings.Contains(err.Error(), "total byte limit") {
			t.Fatalf("expected total byte rejection, got %v", err)
		}
	})
}

func TestPublishOuroborosExportRootRemovesTempRoot(t *testing.T) {
	parent := t.TempDir()
	tempRoot := filepath.Join(parent, ".canonical-export.tmp-123")
	publishRoot := filepath.Join(tempRoot, "publish")
	out := filepath.Join(parent, "canonical-export")
	mustWriteFile(t, filepath.Join(publishRoot, "dist", "build.json"), "{}")
	mustWriteFile(t, filepath.Join(tempRoot, "fixture-dist", "build.json"), "{}")
	if err := publishOuroborosExportRoot(tempRoot, publishRoot, out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, "dist", "build.json")); err != nil {
		t.Fatalf("published output missing: %v", err)
	}
	if _, err := os.Stat(tempRoot); !os.IsNotExist(err) {
		t.Fatalf("temp root survived successful publish: %v", err)
	}
}

func TestStopAndPublishOuroborosExportRootStopsBeforePublish(t *testing.T) {
	fixture := &ouroborosExportBuiltApp{Name: "fixture"}
	docs := &ouroborosExportBuiltApp{Name: "docs"}
	order := []string{}
	stopFn := func(app *ouroborosExportBuiltApp) error {
		order = append(order, "stop:"+app.Name)
		return nil
	}
	publishFn := func(_, _, _ string) error {
		order = append(order, "publish")
		return nil
	}
	if err := stopAndPublishOuroborosExportRoot(fixture, docs, "tmp", "tmp/publish", "out", stopFn, publishFn); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(order, ",")
	if got != "stop:fixture,stop:docs,publish" {
		t.Fatalf("unexpected stop/publish order: %s", got)
	}
}

func TestStopAndPublishOuroborosExportRootFailsClosedOnStopError(t *testing.T) {
	published := false
	err := stopAndPublishOuroborosExportRoot(&ouroborosExportBuiltApp{Name: "fixture"}, &ouroborosExportBuiltApp{Name: "docs"}, "tmp", "tmp/publish", "out",
		func(app *ouroborosExportBuiltApp) error {
			if app.Name == "docs" {
				return fmt.Errorf("stop timeout")
			}
			return nil
		},
		func(_, _, _ string) error {
			published = true
			return nil
		},
	)
	if err == nil || !strings.Contains(err.Error(), "stop timeout") {
		t.Fatalf("expected stop error, got %v", err)
	}
	if published {
		t.Fatal("publish ran after stop failure")
	}
}

func TestStopOuroborosExportAppIsIdempotent(t *testing.T) {
	if err := stopOuroborosExportApp(&ouroborosExportBuiltApp{Name: "already-stopped"}); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", "-c", "exit 0")
	cmd.SysProcAttr = childProcessAttributes()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	app := &ouroborosExportBuiltApp{Name: "short", Command: cmd}
	if err := stopOuroborosExportApp(app); err != nil {
		t.Fatal(err)
	}
	if app.Command != nil {
		t.Fatal("stop did not clear command")
	}
	if err := stopOuroborosExportApp(app); err != nil {
		t.Fatalf("double stop failed: %v", err)
	}
}

func TestStopOuroborosExportAppTimeoutThenRetry(t *testing.T) {
	cmd := exec.Command("sh", "-c", "sleep 10")
	cmd.SysProcAttr = childProcessAttributes()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	app := &ouroborosExportBuiltApp{Name: "retry", Command: cmd}
	noopSignal := func(int) error { return nil }
	err := stopOuroborosExportAppWithTimeout(app, 5*time.Millisecond, noopSignal, noopSignal)
	if err == nil || !strings.Contains(err.Error(), "did not exit") {
		t.Fatalf("expected timeout, got %v", err)
	}
	if app.Command == nil {
		t.Fatal("timeout cleared command before confirmed exit")
	}
	done := app.stopDone
	if done == nil {
		t.Fatal("timeout did not keep wait channel")
	}
	if err := stopOuroborosExportAppWithTimeout(app, time.Second, noopSignal, terminateProcessTree); err != nil {
		t.Fatalf("retry stop failed: %v", err)
	}
	if app.Command != nil {
		t.Fatal("retry did not clear command after confirmed exit")
	}
	if app.stopDone != done {
		t.Fatal("retry replaced wait channel")
	}
	if err := stopOuroborosExportApp(app); err != nil {
		t.Fatalf("post-retry double stop failed: %v", err)
	}
}

func TestJoinOuroborosStopErrorReportsErrorPathCleanupFailure(t *testing.T) {
	err := fmt.Errorf("fetch failed")
	joinOuroborosStopError(&err, &ouroborosExportBuiltApp{Name: "fixture"}, func(*ouroborosExportBuiltApp) error {
		return fmt.Errorf("stop failed")
	})
	if err == nil || !strings.Contains(err.Error(), "fetch failed") || !strings.Contains(err.Error(), "stop fixture app") {
		t.Fatalf("cleanup error was not joined: %v", err)
	}
}

func TestValidateOuroborosProducerRootRejectsRawHTMLTamper(t *testing.T) {
	root, assets, provenance := writeValidProducerRootForTest(t)
	if err := validateOuroborosProducerRoot(root, assets, provenance); err != nil {
		t.Fatalf("expected valid producer root: %v", err)
	}
	mustWriteFile(t, filepath.Join(root, "dist", "_ouroboros", "raw-html", "R10.html"), "<html>tampered</html>")
	if err := validateOuroborosProducerRoot(root, assets, provenance); err == nil || !strings.Contains(err.Error(), "raw HTML") {
		t.Fatalf("expected raw HTML tamper rejection, got %v", err)
	}
}

func TestValidateOuroborosProducerRootRejectsSameLengthProvenanceTamper(t *testing.T) {
	root, assets, provenance := writeValidProducerRootForTest(t)
	provenance.Routes[0].Producer = "docs"
	if err := writeJSONFile(filepath.Join(root, "dist", "_ouroboros", "export-corpus.json"), provenance); err != nil {
		t.Fatal(err)
	}
	original := provenance
	original.Routes[0].Producer = "fixture"
	if err := validateOuroborosProducerRoot(root, assets, original); err == nil || !strings.Contains(err.Error(), "provenance changed") {
		t.Fatalf("expected same-length provenance tamper rejection, got %v", err)
	}
}

func TestValidateOuroborosProducerRootRejectsDuplicateRouteFile(t *testing.T) {
	root, assets, provenance := writeValidProducerRootForTest(t)
	var manifest strictOuroborosExportManifest
	if err := decodeStrictJSONFile(filepath.Join(root, "dist", "export.json"), &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.Routes[1].File = manifest.Routes[0].File
	provenance.Routes[1].File = provenance.Routes[0].File
	if err := writeJSONFile(filepath.Join(root, "dist", "export.json"), manifest); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONFile(filepath.Join(root, "dist", "_ouroboros", "export-corpus.json"), provenance); err != nil {
		t.Fatal(err)
	}
	if err := validateOuroborosProducerRoot(root, assets, provenance); err == nil || (!strings.Contains(err.Error(), "duplicate route HTML file") && !strings.Contains(err.Error(), "want export file")) {
		t.Fatalf("expected duplicate route file rejection, got %v", err)
	}
}

func TestValidateOuroborosProducerRootRejectsMissingCanonicalDynamicEndpoint(t *testing.T) {
	root, assets, provenance := writeValidProducerRootForTest(t)
	provenance.DynamicRefs = nil
	if err := writeJSONFile(filepath.Join(root, "dist", "_ouroboros", "export-corpus.json"), provenance); err != nil {
		t.Fatal(err)
	}
	resourceManifest := buildOuroborosResourceManifest(ouroborosCorpusManifest{ContractVersion: "O0.2", CorpusID: provenance.CorpusID}, provenance.ResourceRefs, provenance.DynamicRefs)
	if err := writeJSONFile(filepath.Join(root, "dist", CanonicalResourceManifestRef), resourceManifest); err != nil {
		t.Fatal(err)
	}
	if err := validateOuroborosProducerRoot(root, assets, provenance); err == nil || !strings.Contains(err.Error(), "dynamic endpoints differ") {
		t.Fatalf("expected dynamic matrix rejection, got %v", err)
	}
}

func TestValidateOuroborosProducerRootRejectsExportUnusedFields(t *testing.T) {
	for _, tc := range []struct {
		name   string
		tamper func(map[string]any)
	}{
		{
			name: "pages",
			tamper: func(doc map[string]any) {
				doc["pages"] = []string{"/static"}
			},
		},
		{
			name: "tags",
			tamper: func(doc map[string]any) {
				routes := doc["routes"].([]any)
				routes[0].(map[string]any)["tags"] = []string{"tamper"}
			},
		},
		{
			name: "revalidateSeconds",
			tamper: func(doc map[string]any) {
				routes := doc["routes"].([]any)
				routes[0].(map[string]any)["revalidateSeconds"] = float64(30)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, assets, provenance := writeValidProducerRootForTest(t)
			path := filepath.Join(root, "dist", "export.json")
			var doc map[string]any
			mustDecodeJSONFileForTest(t, path, &doc)
			tc.tamper(doc)
			if err := writeJSONFile(path, doc); err != nil {
				t.Fatal(err)
			}
			if err := validateOuroborosProducerRoot(root, assets, provenance); err == nil || !strings.Contains(err.Error(), "unknown field") {
				t.Fatalf("expected unused export field rejection, got %v", err)
			}
		})
	}
}

func TestValidateOuroborosProducerRootRejectsBuildSceneAssets(t *testing.T) {
	root, assets, provenance := writeValidProducerRootForTest(t)
	path := filepath.Join(root, "dist", "build.json")
	var doc map[string]any
	mustDecodeJSONFileForTest(t, path, &doc)
	doc["sceneAssets"] = map[string]any{"file": "scene-assets.json"}
	if err := writeJSONFile(path, doc); err != nil {
		t.Fatal(err)
	}
	if err := validateOuroborosProducerRoot(root, assets, provenance); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("expected sceneAssets rejection, got %v", err)
	}
}

func writeValidProducerRootForTest(t *testing.T) (string, []ouroborosExportAssetRef, ouroborosExportProvenance) {
	t.Helper()
	root := t.TempDir()
	dist := filepath.Join(root, "dist")
	assetData := []byte("asset")
	assetHash := contentHash(assetData)
	assetFile := "bootstrap." + assetHash + ".js"
	assetSHA := sha256StringForTest(assetData)
	asset := ouroborosExportAssetRef{Ref: "/gosx/assets/runtime/" + assetFile, Bucket: "runtime", File: assetFile, Hash: assetHash, Size: int64(len(assetData)), SHA256: assetSHA}
	mustWriteFile(t, filepath.Join(dist, "assets", "runtime", assetFile), string(assetData))
	mustWriteFile(t, filepath.Join(dist, "gosx", "assets", "runtime", assetFile), string(assetData))
	manifest := &buildmanifest.Manifest{Runtime: buildmanifest.RuntimeAssets{Bootstrap: buildmanifest.HashedAsset{File: assetFile, Hash: assetHash, Size: int64(len(assetData))}}}
	if err := writeJSONFile(filepath.Join(dist, "build.json"), manifest); err != nil {
		t.Fatal(err)
	}
	paths := map[string]string{"R00": "/static", "R01": "/lite", "R02": "/island/counter", "R03": "/islands/kitchen", "R04": "/action/form", "R05": "/canvas-board", "R06": "/hub/echo", "R07": "/video-sync", "R08": "/scene/basic", "R09A": "/navigation/a", "R09B": "/navigation/b", "R10": "/demos/water"}
	exportRoutes := []strictOuroborosExportRoute{}
	provenanceRoutes := []ouroborosExportRoute{}
	for _, id := range canonicalOuroborosExportIDs {
		routePath := paths[id]
		file := filepath.ToSlash(buildmanifest.ExportFilePath(routePath))
		raw := "<html>raw " + id + "</html>"
		html := "<html>canonical " + id + "</html>"
		mustWriteFile(t, filepath.Join(dist, file), html)
		mustWriteFile(t, filepath.Join(dist, "_ouroboros", "raw-html", id+".html"), raw)
		exportRoutes = append(exportRoutes, strictOuroborosExportRoute{Path: routePath, File: file, SHA256: sha256StringForTest([]byte(html)), Bytes: int64(len(html))})
		producer := "fixture"
		if id == "R10" {
			producer = "docs"
		}
		provenanceRoutes = append(provenanceRoutes, ouroborosExportRoute{
			ID:        id,
			Path:      routePath,
			File:      file,
			Producer:  producer,
			SHA256:    sha256StringForTest([]byte(html)),
			Bytes:     int64(len(html)),
			RawSHA256: sha256StringForTest([]byte(raw)),
			RawBytes:  int64(len(raw)),
		})
	}
	exportManifest := strictOuroborosExportManifest{Routes: exportRoutes, AssetRefs: []string{asset.Ref}, ResourceManifest: CanonicalResourceManifestRef}
	if err := writeJSONFile(filepath.Join(dist, "export.json"), exportManifest); err != nil {
		t.Fatal(err)
	}
	resourceData := []byte("resource")
	resourceSHA := sha256StringForTest(resourceData)
	resource := ouroborosExportResourceRef{
		ID:          stableOuroborosResourceID("resource", "/media/example.mp4"),
		Ref:         "/media/example.mp4",
		File:        "media/example.mp4",
		Producer:    "fixture",
		Kind:        "mp4",
		Source:      "html",
		ContentType: "video/mp4",
		SHA256:      resourceSHA,
		Bytes:       int64(len(resourceData)),
		GzipBytes:   gzipLength(resourceData),
		BrotliBytes: brotliLength(resourceData),
		Routes:      []string{"R07"},
	}
	mustWriteFile(t, filepath.Join(dist, resource.File), string(resourceData))
	provenance := ouroborosExportProvenance{
		SchemaVersion:   ouroborosExportSchemaVersion,
		ContractVersion: "O0.2",
		CorpusID:        "gosx-ouroboros-o0.2-v1",
		Routes:          provenanceRoutes,
		AssetRefs:       []ouroborosExportAssetRef{asset},
		ResourceRefs:    []ouroborosExportResourceRef{resource},
		DynamicRefs:     expectedOuroborosDynamicRefs(),
	}
	resourceManifest := buildOuroborosResourceManifest(ouroborosCorpusManifest{ContractVersion: "O0.2", CorpusID: provenance.CorpusID}, provenance.ResourceRefs, provenance.DynamicRefs)
	if err := writeJSONFile(filepath.Join(dist, CanonicalResourceManifestRef), resourceManifest); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONFile(filepath.Join(dist, "_ouroboros", "export-corpus.json"), provenance); err != nil {
		t.Fatal(err)
	}
	return root, []ouroborosExportAssetRef{asset}, provenance
}

func sha256StringForTest(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func mustDecodeJSONFileForTest(t *testing.T, path string, value any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, value); err != nil {
		t.Fatal(err)
	}
}

func repoRootForTest(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func canonicalCorpusJSONForTest(mutator func(id string, body string) string) string {
	paths := map[string]string{"R00": "/static", "R01": "/lite", "R02": "/island/counter", "R03": "/islands/kitchen", "R04": "/action/form", "R05": "/canvas-board", "R06": "/hub/echo", "R07": "/video-sync", "R08": "/scene/basic", "R09A": "/navigation/a", "R09B": "/navigation/b", "R10": "/demos/water"}
	parts := []string{`{"schemaVersion":"gosx.ouroboros.fixtures.v1","contractVersion":"O0.2","corpusID":"gosx-ouroboros-o0.2-v1","fixtureApp":"examples/ouroboros-corpus","routes":[`}
	for i, id := range canonicalOuroborosExportIDs {
		if i > 0 {
			parts = append(parts, ",")
		}
		app := "examples/ouroboros-corpus"
		external := ""
		if id == "R10" {
			app = "examples/gosx-docs"
			external = `,"external":true`
		}
		body := `{"id":"` + id + `","route":"` + paths[id] + `","fixtureApp":"` + app + `"` + external + `}`
		if mutator != nil {
			body = mutator(id, body)
		}
		parts = append(parts, body)
	}
	parts = append(parts, `]}`)
	return strings.Join(parts, "")
}
