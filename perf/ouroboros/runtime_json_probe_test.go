package ouroboros

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRuntimeJSONStaticCorpusIsDeterministic(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "client/js/bootstrap-src/00-runtime.js", strings.Join([]string{
		"function hydrate(propsJSON) {",
		"  const parsed = JSON.parse(propsJSON);",
		"  const ignored = \"JSON.parse(propsJSON) window.__gosx_string()\";",
		"  // JSON.stringify(patchJSON); window.__gosx_comment();",
		"  window.__gosx_hydrate('one', JSON.stringify(parsed));",
		"  window.__gosx_hydrate('two'); window.__gosx_hydrate('three');",
		"}",
		"window.__gosx_runtime_ready = function(){};",
		"",
	}, "\n"))
	writeFile(t, root, "client/js/patch.js", "function applyPatch(patchJSON) { return JSON.parse(patchJSON); }\n")
	writeFile(t, root, "server/embed.go", "package server\nimport _ \"embed\"\n//go:embed navigation_runtime.js\nvar navigationRuntime string\n")
	writeFile(t, root, "server/navigation_runtime.js", "function submit(valueJSON) { return fetch('/x', {body: JSON.stringify(valueJSON)}); }\n")
	writeFile(t, root, "client/wasm/main.go", "package main\nfunc register() { setRuntimeFunc(\"__gosx_action\", nil) }\n")
	writeFile(t, root, "client/wasm/main_test.go", "package main\nfunc testOnly() { setRuntimeFunc(\"__gosx_test_only\", nil) }\n")
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.invalid")
	runGit(t, root, "config", "user.name", "test")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "base")

	opts := RuntimeJSONProbeOptions{
		RepoRoot:     root,
		ArtifactRoot: filepath.Join(t.TempDir(), "runtime-calls"),
		GeneratedAt:  time.Unix(0, 0).UTC(),
		Git:          true,
	}
	first, err := CollectRuntimeJSONStaticCorpus(context.Background(), opts)
	if err != nil {
		t.Fatalf("CollectRuntimeJSONStaticCorpus first: %v", err)
	}
	second, err := CollectRuntimeJSONStaticCorpus(context.Background(), opts)
	if err != nil {
		t.Fatalf("CollectRuntimeJSONStaticCorpus second: %v", err)
	}
	if first.Counts.SerializationSiteCount != second.Counts.SerializationSiteCount {
		t.Fatalf("site count drifted: %d then %d", first.Counts.SerializationSiteCount, second.Counts.SerializationSiteCount)
	}
	if first.Counts.ByOperation["json-parse"] != 2 {
		t.Fatalf("json-parse count = %d, want 2", first.Counts.ByOperation["json-parse"])
	}
	if first.Counts.ByOperation["json-stringify"] != 2 {
		t.Fatalf("json-stringify count = %d, want 2", first.Counts.ByOperation["json-stringify"])
	}
	if first.Counts.ByOperation["props-json"] != 2 || first.Counts.ByOperation["patch-json"] != 2 || first.Counts.ByOperation["value-json"] != 2 {
		t.Fatalf("wire JSON counts = %+v", first.Counts.ByOperation)
	}
	if first.Counts.SerializationSiteCount != 10 {
		t.Fatalf("serializationSiteCount = %d, want 10", first.Counts.SerializationSiteCount)
	}
	if first.Counts.GosxCallCount != 3 || first.Counts.GosxWriteCount != 2 {
		t.Fatalf("gosx operation counts = %+v", first.Counts.ByOperation)
	}
	if containsStaticSiteName(first, "__gosx_test_only") {
		t.Fatal("test-only bridge file entered static corpus")
	}
	if !first.Counts.FailClosed {
		t.Fatal("static corpus must fail closed until the reproducible query is accepted")
	}
	if first.ScannerVersion == "" || first.PhaseClassifierVersion == "" || first.SemanticHash == "" || first.CountsHash == "" || first.CurrentSourceIdentityHash == "" {
		t.Fatalf("static corpus lacks identity hashes: %+v", first)
	}
	if first.SemanticHash != second.SemanticHash || first.CountsHash != second.CountsHash || first.GlobalNames.Hash != second.GlobalNames.Hash {
		t.Fatalf("static corpus hashes drifted: first semantic=%s counts=%s globals=%s second semantic=%s counts=%s globals=%s",
			first.SemanticHash, first.CountsHash, first.GlobalNames.Hash,
			second.SemanticHash, second.CountsHash, second.GlobalNames.Hash)
	}
	if got := RuntimeJSONStaticGlobalNames(first); !sameStringSlice(got, first.GlobalNames.Names) {
		t.Fatalf("RuntimeJSONStaticGlobalNames = %v, want %v", got, first.GlobalNames.Names)
	}

	firstPath := filepath.Join(root, "build", "first.jsonl")
	secondPath := filepath.Join(root, "build", "second.jsonl")
	if err := WriteRuntimeJSONStaticCorpusJSONL(firstPath, first); err != nil {
		t.Fatalf("WriteRuntimeJSONStaticCorpusJSONL first: %v", err)
	}
	if err := WriteRuntimeJSONStaticCorpusJSONL(secondPath, second); err != nil {
		t.Fatalf("WriteRuntimeJSONStaticCorpusJSONL second: %v", err)
	}
	firstBody, err := os.ReadFile(firstPath)
	if err != nil {
		t.Fatalf("read first jsonl: %v", err)
	}
	secondBody, err := os.ReadFile(secondPath)
	if err != nil {
		t.Fatalf("read second jsonl: %v", err)
	}
	if string(firstBody) != string(secondBody) {
		t.Fatalf("static corpus JSONL is not deterministic\nfirst:\n%s\nsecond:\n%s", firstBody, secondBody)
	}
	if !strings.Contains(string(firstBody), `"kind":"site"`) {
		t.Fatalf("JSONL lacks site rows: %s", firstBody)
	}
}

func TestRuntimeJSONStaticCanonicalSourceHashIgnoresArtifactRootAndInventoryTimestamp(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "client/js/bootstrap-src/00-runtime.js", strings.Join([]string{
		"window.__gosx_runtime_ready = function(){};",
		"function hydrate(propsJSON) { return JSON.parse(propsJSON); }",
		"",
	}, "\n"))
	writeFile(t, root, "server/embed.go", "package server\nimport _ \"embed\"\n//go:embed navigation_runtime.js\nvar navigationRuntime string\n")
	writeFile(t, root, "server/navigation_runtime.js", "window.__gosx_navigation_runtime = function(valueJSON) { return JSON.stringify(valueJSON); };\n")
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.invalid")
	runGit(t, root, "config", "user.name", "test")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "base")

	baseTime := time.Unix(100, 0).UTC()
	first, err := CollectRuntimeJSONStaticCorpus(context.Background(), RuntimeJSONProbeOptions{
		RepoRoot:     root,
		ArtifactRoot: filepath.Join(t.TempDir(), "first"),
		GeneratedAt:  baseTime,
		Git:          true,
	})
	if err != nil {
		t.Fatalf("CollectRuntimeJSONStaticCorpus first: %v", err)
	}
	second, err := CollectRuntimeJSONStaticCorpus(context.Background(), RuntimeJSONProbeOptions{
		RepoRoot:     root,
		ArtifactRoot: filepath.Join(t.TempDir(), "second"),
		GeneratedAt:  baseTime,
		Git:          true,
	})
	if err != nil {
		t.Fatalf("CollectRuntimeJSONStaticCorpus second: %v", err)
	}
	third, err := CollectRuntimeJSONStaticCorpus(context.Background(), RuntimeJSONProbeOptions{
		RepoRoot:     root,
		ArtifactRoot: filepath.Join(t.TempDir(), "third"),
		GeneratedAt:  baseTime.Add(time.Hour),
		Git:          true,
	})
	if err != nil {
		t.Fatalf("CollectRuntimeJSONStaticCorpus third: %v", err)
	}
	for _, corpus := range []*RuntimeJSONStaticCorpus{second, third} {
		if first.CurrentSourceIdentityHash != corpus.CurrentSourceIdentityHash {
			t.Fatalf("source identity hash drifted: first=%s next=%s", first.CurrentSourceIdentityHash, corpus.CurrentSourceIdentityHash)
		}
		if first.SemanticHash != corpus.SemanticHash || first.CountsHash != corpus.CountsHash || first.GlobalNames.Hash != corpus.GlobalNames.Hash {
			t.Fatalf("semantic hashes drifted: first=%s/%s/%s next=%s/%s/%s",
				first.SemanticHash, first.CountsHash, first.GlobalNames.Hash,
				corpus.SemanticHash, corpus.CountsHash, corpus.GlobalNames.Hash)
		}
	}

	firstPath := filepath.Join(t.TempDir(), "first.jsonl")
	secondPath := filepath.Join(t.TempDir(), "second.jsonl")
	thirdPath := filepath.Join(t.TempDir(), "third.jsonl")
	if err := WriteRuntimeJSONStaticCorpusJSONL(firstPath, first); err != nil {
		t.Fatalf("WriteRuntimeJSONStaticCorpusJSONL first: %v", err)
	}
	if err := WriteRuntimeJSONStaticCorpusJSONL(secondPath, second); err != nil {
		t.Fatalf("WriteRuntimeJSONStaticCorpusJSONL second: %v", err)
	}
	if err := WriteRuntimeJSONStaticCorpusJSONL(thirdPath, third); err != nil {
		t.Fatalf("WriteRuntimeJSONStaticCorpusJSONL third: %v", err)
	}
	firstBody := readFileForTest(t, firstPath)
	secondBody := readFileForTest(t, secondPath)
	thirdBody := readFileForTest(t, thirdPath)
	if string(firstBody) != string(secondBody) {
		t.Fatalf("JSONL drifted across artifact roots\nfirst:\n%s\nsecond:\n%s", firstBody, secondBody)
	}
	if string(normalizeRuntimeJSONGeneratedAtForTest(firstBody)) != string(normalizeRuntimeJSONGeneratedAtForTest(thirdBody)) {
		t.Fatalf("JSONL drifted beyond generatedAt\nfirst:\n%s\nthird:\n%s", firstBody, thirdBody)
	}
}

func TestRuntimeJSONInventoryPathRejectsStrictAndStaleInputs(t *testing.T) {
	root, invPath := writeRuntimeJSONInventoryForProbeTest(t)
	ctx := context.Background()
	opts := RuntimeJSONProbeOptions{
		RepoRoot:      root,
		InventoryPath: invPath,
		ArtifactRoot:  filepath.Join(root, "build", "ouroboros", "test-runtime-json-strict"),
		GeneratedAt:   time.Unix(0, 0).UTC(),
		Git:           true,
	}
	_, source, err := loadRuntimeJSONInventory(ctx, opts)
	if err != nil {
		t.Fatalf("loadRuntimeJSONInventory valid: %v", err)
	}
	if !source.CurrentOverlayVerified || !source.StrictInventory || !source.ReconstructionProof || source.Reconstruction == nil || !source.Reconstruction.Verified {
		t.Fatalf("source proof incomplete: %+v", source)
	}

	t.Run("unknown field", func(t *testing.T) {
		body := readFileForTest(t, invPath)
		body = append(body[:len(body)-2], []byte(",\n  \"unknown\": true\n}\n")...)
		bad := filepath.Join(t.TempDir(), "inventory.json")
		writeFile(t, filepath.Dir(bad), filepath.Base(bad), string(body))
		_, _, err := loadRuntimeJSONInventory(ctx, runtimeJSONInventoryOptsForTest(root, bad))
		if err == nil || !strings.Contains(err.Error(), "unknown field") {
			t.Fatalf("unknown inventory field error = %v", err)
		}
	})
	t.Run("trailing JSON", func(t *testing.T) {
		bad := filepath.Join(t.TempDir(), "inventory.json")
		body := append(readFileForTest(t, invPath), []byte("\n{}")...)
		writeFile(t, filepath.Dir(bad), filepath.Base(bad), string(body))
		_, _, err := loadRuntimeJSONInventory(ctx, runtimeJSONInventoryOptsForTest(root, bad))
		if err == nil || !strings.Contains(err.Error(), "trailing JSON") {
			t.Fatalf("trailing inventory JSON error = %v", err)
		}
	})
	t.Run("pruned files totals", func(t *testing.T) {
		assertRuntimeJSONInventoryMutationRejected(t, root, invPath, "fresh-inventory mismatch", func(inv *Inventory) {
			if len(inv.Files.Included) == 0 {
				t.Fatal("test inventory has no included files")
			}
			inv.Files.Included = inv.Files.Included[1:]
			inv.Totals.IncludedFiles--
		})
	})
	t.Run("pruned surface names", func(t *testing.T) {
		assertRuntimeJSONInventoryMutationRejected(t, root, invPath, "fresh-inventory mismatch", func(inv *Inventory) {
			if len(inv.Surface.GosxNames) == 0 {
				t.Fatal("test inventory has no gosx names")
			}
			inv.Surface.GosxNames = inv.Surface.GosxNames[1:]
			inv.Surface.GosxNameCount--
		})
	})
	t.Run("compatibility audit drift", func(t *testing.T) {
		assertRuntimeJSONInventoryMutationRejected(t, root, invPath, "fresh-inventory mismatch", func(inv *Inventory) {
			inv.Surface.CompatibilityAudit.Notes = append(inv.Surface.CompatibilityAudit.Notes, "forged-note")
		})
	})
	t.Run("inconsistent broader totals", func(t *testing.T) {
		assertRuntimeJSONInventoryMutationRejected(t, root, invPath, "broaderBrowserNameCount mismatch", func(inv *Inventory) {
			inv.Surface.BroaderBrowserNameCount++
		})
	})
	t.Run("duplicate surface names", func(t *testing.T) {
		assertRuntimeJSONInventoryMutationRejected(t, root, invPath, "duplicate gosxNames record", func(inv *Inventory) {
			record := GosxName{Name: "__gosx_action", Owners: []string{"client/js/bootstrap-src/00-runtime.js"}, SourceFamilies: []string{"bootstrap"}, CompatibilityClass: "runtime"}
			inv.Surface.GosxNames = []GosxName{record, record}
			inv.Surface.GosxNameCount = 2
		})
	})
	t.Run("duplicate file path", func(t *testing.T) {
		assertRuntimeJSONInventoryMutationRejected(t, root, invPath, "duplicate inventory file path", func(inv *Inventory) {
			file := SourceFile{Path: "client/js/bootstrap-src/00-runtime.js", Language: "javascript", SourceKind: "runtime", Lines: 1, ParseOK: true}
			inv.Files.Included = []SourceFile{file}
			inv.Files.Sidecars = []SourceFile{file}
			inv.Totals.IncludedFiles = 1
			inv.Totals.SidecarFiles = 1
		})
	})
	t.Run("unsafe file path", func(t *testing.T) {
		assertRuntimeJSONInventoryMutationRejected(t, root, invPath, "unsafe path", func(inv *Inventory) {
			inv.Files.Included = []SourceFile{{Path: "../escape.js", Language: "javascript", SourceKind: "runtime", Lines: 1, ParseOK: true}}
			inv.Totals.IncludedFiles = 1
		})
	})
	t.Run("variant artifact source drift", func(t *testing.T) {
		assertRuntimeJSONInventoryMutationRejected(t, root, invPath, "mismatched source anchor", func(inv *Inventory) {
			for i := range inv.Manifest.Variants {
				if inv.Manifest.Variants[i].SizeArtifact != nil {
					inv.Manifest.Variants[i].SizeArtifact.OverlayHash = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
					return
				}
			}
		})
	})
	t.Run("tampered identity", func(t *testing.T) {
		var inv Inventory
		mustJSONUnmarshalForTest(t, readFileForTest(t, invPath), &inv)
		inv.OverlayHash = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		inv.Overlay.Hash = inv.OverlayHash
		inv.Manifest.OverlayHash = inv.OverlayHash
		anchorManifestArtifacts(&inv.Manifest, inv.BaseRevision, inv.OverlayHash)
		inv.Surface.CompatibilityAudit = minimalCompatibilityAudit(inv.BaseRevision, inv.OverlayHash)
		bad := filepath.Join(t.TempDir(), "inventory.json")
		if err := WriteJSONFile(bad, inv); err != nil {
			t.Fatalf("WriteJSONFile tampered: %v", err)
		}
		_, _, err := loadRuntimeJSONInventory(ctx, runtimeJSONInventoryOptsForTest(root, bad))
		if err == nil || !strings.Contains(err.Error(), "stale overlay") {
			t.Fatalf("tampered inventory identity error = %v", err)
		}
	})
	t.Run("replay mismatch", func(t *testing.T) {
		var inv Inventory
		mustJSONUnmarshalForTest(t, readFileForTest(t, invPath), &inv)
		if inv.Overlay.PatchPath == "" {
			t.Skip("current overlay has no tracked patch to tamper")
		}
		patchPath := filepath.Join(root, filepath.FromSlash(inv.Overlay.PatchPath))
		original := readFileForTest(t, patchPath)
		if err := os.WriteFile(patchPath, append(append([]byte{}, original...), []byte("\n# tamper\n")...), 0644); err != nil {
			t.Fatalf("tamper patch: %v", err)
		}
		t.Cleanup(func() { _ = os.WriteFile(patchPath, original, 0644) })
		_, _, err := loadRuntimeJSONInventory(ctx, runtimeJSONInventoryOptsForTest(root, invPath))
		if err == nil || !strings.Contains(err.Error(), "replay failed") {
			t.Fatalf("replay mismatch error = %v", err)
		}
	})
	t.Run("compat current preserves proof", func(t *testing.T) {
		opts := runtimeJSONInventoryOptsForTest(root, invPath)
		opts.scanMode = runtimeJSONInventoryScanCompatibilityCurrent
		_, source, err := loadRuntimeJSONInventory(ctx, opts)
		if err != nil {
			t.Fatalf("compat current loadRuntimeJSONInventory: %v", err)
		}
		if !source.CurrentOverlayVerified || !source.StrictInventory || !source.ReconstructionProof || source.Reconstruction == nil || !source.Reconstruction.Verified {
			t.Fatalf("compat current source proof incomplete: %+v", source)
		}
	})
	t.Run("anchor archive exception is proofless and private", func(t *testing.T) {
		opts := runtimeJSONInventoryOptsForTest(root, invPath)
		opts.scanMode = runtimeJSONInventoryScanCompatibilityAnchorArchive
		_, source, err := loadRuntimeJSONInventory(ctx, opts)
		if err != nil {
			t.Fatalf("anchor archive loadRuntimeJSONInventory: %v", err)
		}
		if !source.StrictInventory || source.CurrentOverlayVerified || source.ReconstructionProof || source.Reconstruction != nil {
			t.Fatalf("anchor archive proof flags = %+v, want strict-only source identity", source)
		}
	})
}

func TestRuntimeJSONProbeScriptAndDrainContract(t *testing.T) {
	script, err := RuntimeJSONProbeScript([]string{"__gosx_action", "__gosx_action", "__gosx_runtime_ready"})
	if err != nil {
		t.Fatalf("RuntimeJSONProbeScript: %v", err)
	}
	for _, want := range []string{
		"__gosxOuroborosProbe",
		"schemaVersion = 1",
		"probe.record(\"probe\", \"install\"",
		"JSON.parse",
		"JSON.stringify",
		"runtime-call",
		"json-call",
		"__gosx_action",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("script missing %q", want)
		}
	}
	if strings.Count(script, "__gosx_action") != 1 {
		t.Fatalf("known names were not de-duplicated in script")
	}
	if got := RuntimeJSONDrainExpression(true); !strings.Contains(got, ".drain()") {
		t.Fatalf("clear drain expression = %q", got)
	}
	if got := RuntimeJSONDrainExpression(false); !strings.Contains(got, ".snapshot()") {
		t.Fatalf("snapshot expression = %q", got)
	}
}

func TestClassifyRuntimeJSONDynamicSourceIsFailClosed(t *testing.T) {
	detail := map[string]any{
		"source": map[string]any{
			"urlHash": "abcd",
			"path":    "/assets/runtime/product.js",
			"line":    float64(12),
			"column":  json.Number("7"),
		},
	}
	source := RuntimeJSONDynamicSourceFromDetail(detail)
	if source.URLHash != "abcd" || source.Path != "/assets/runtime/product.js" || source.Line != 12 || source.Column != 7 {
		t.Fatalf("source = %+v", source)
	}
	if got := ClassifyRuntimeJSONDynamicSource(source, []string{"/assets/runtime/"}, nil); got != RuntimeJSONDynamicSourceProduct {
		t.Fatalf("product classification = %q", got)
	}
	if got := ClassifyRuntimeJSONDynamicSource(source, nil, []string{"/assets/"}); got != RuntimeJSONDynamicSourceHarness {
		t.Fatalf("harness classification = %q", got)
	}
	if got := ClassifyRuntimeJSONDynamicSource(source, []string{"/assets/"}, []string{"/assets/runtime/"}); got != RuntimeJSONDynamicSourceUnknown {
		t.Fatalf("ambiguous classification = %q", got)
	}
	if got := ClassifyRuntimeJSONDynamicSource(RuntimeJSONDynamicSource{}, []string{"/assets/"}, nil); got != RuntimeJSONDynamicSourceUnknown {
		t.Fatalf("empty source classification = %q", got)
	}
	if got := RuntimeJSONDynamicSourceFromDetail(map[string]any{}); got != (RuntimeJSONDynamicSource{}) {
		t.Fatalf("missing source = %+v", got)
	}
}

func readFileForTest(t *testing.T, path string) []byte {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return body
}

func normalizeRuntimeJSONGeneratedAtForTest(body []byte) []byte {
	var rows []string
	for _, line := range strings.Split(string(body), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			rows = append(rows, line)
			continue
		}
		if value, ok := row["value"].(map[string]any); ok {
			if _, ok := value["generatedAt"]; ok {
				value["generatedAt"] = "<generated-at>"
			}
		}
		canonical, err := json.Marshal(row)
		if err != nil {
			rows = append(rows, line)
			continue
		}
		rows = append(rows, string(canonical))
	}
	return []byte(strings.Join(rows, "\n"))
}

func TestRuntimeJSONStaticCorpusCoversAliasAndDefinePropertyExports(t *testing.T) {
	src := SourceFile{Path: "client/js/bootstrap-src/20a-scene-mount-backend.js", SourceKind: "scoreboard", Language: "javascript"}
	body := []byte(strings.Join([]string{
		"function ensureSceneDebugAPI(){",
		"  const root = window;",
		"  Object.defineProperty(root, \"__gosx_scene3d_debug\", { value: api, configurable: true });",
		"  root.__gosx_scene3d_debug_registry = registry;",
		"}",
		"function ensureSceneIR(){",
		"  var root = typeof globalThis !== \"undefined\" ? globalThis : (typeof window !== \"undefined\" ? window : null);",
		"  if (root) {",
		"    try {",
		"      Object.defineProperty(root, \"__gosx_validate_scene_ir_strict\", {",
		"        value: validateSceneIRStrict,",
		"        writable: false,",
		"        enumerable: false,",
		"        configurable: false",
		"      });",
		"    } catch (_err) {",
		"      root.__gosx_validate_scene_ir_strict = root.__gosx_validate_scene_ir_strict || validateSceneIRStrict;",
		"    }",
		"  }",
		"}",
		"",
	}, "\n"))
	globals := map[string]bool{}
	sites, err := runtimeJSONSitesForFile(src, body, globals)
	if err != nil {
		t.Fatalf("runtimeJSONSitesForFile: %v", err)
	}
	for _, want := range []struct {
		name string
		op   string
	}{
		{"__gosx_scene3d_debug", "gosx-write"},
		{"__gosx_scene3d_debug_registry", "gosx-write"},
		{"__gosx_validate_scene_ir_strict", "gosx-write"},
		{"__gosx_validate_scene_ir_strict", "gosx-read"},
	} {
		if !hasStaticGlobalSite(sites, want.name, want.op) {
			t.Fatalf("missing %s %s in sites: %+v", want.name, want.op, sites)
		}
	}
	for _, site := range sites {
		if site.GlobalName != "" && !globals[site.GlobalName] {
			t.Fatalf("global %s missing from globals map", site.GlobalName)
		}
		if site.Symbol == "" {
			t.Fatalf("empty owner for site %+v", site)
		}
	}
}

func TestRuntimeJSONStaticCorpusCoversSceneSubFeaturePromiseKey(t *testing.T) {
	src := SourceFile{Path: "client/js/bootstrap-src/20b-scene-mount-webgl-chunk.js", SourceKind: "scoreboard", Language: "javascript"}
	body := []byte(strings.Join([]string{
		"function ensureWebGLFeatureLoaded(){",
		"  return loadSceneSubFeature(\"webgl\", \"gosxScene3dWebglUrl\", \"/gosx/bootstrap-feature-scene3d-webgl.js\", \"__gosx_scene3d_webgl_feature_promise\", function(){",
		"    return window.__gosx_scene3d_webgl_api || null;",
		"  });",
		"}",
		"const ignored = \"__gosx_not_active\";",
		"const indirect = \"__gosx_indirect_key\";",
		"loadSceneSubFeature(\"x\", \"dataset\", \"/x.js\", indirect, function(){ return null; });",
		"loadSceneSubFeature(\"x\", \"dataset\", \"/x.js\", __gosx_not_literal, function(){ return null; });",
		"",
	}, "\n"))
	globals := map[string]bool{}
	sites, err := runtimeJSONSitesForFile(src, body, globals)
	if err != nil {
		t.Fatalf("runtimeJSONSitesForFile: %v", err)
	}
	if !hasStaticGlobalSite(sites, "__gosx_scene3d_webgl_feature_promise", "gosx-write") {
		t.Fatalf("missing scene sub-feature promise key in sites: %+v", sites)
	}
	if !globals["__gosx_scene3d_webgl_feature_promise"] {
		t.Fatalf("scene sub-feature promise key missing from globals: %+v", globals)
	}
	if globals["__gosx_not_active"] || containsStaticSiteName(&RuntimeJSONStaticCorpus{Sites: sites}, "__gosx_not_active") {
		t.Fatalf("inactive string literal was classified as an active global: sites=%+v globals=%+v", sites, globals)
	}
	if globals["__gosx_indirect_key"] || containsStaticSiteName(&RuntimeJSONStaticCorpus{Sites: sites}, "__gosx_indirect_key") {
		t.Fatalf("nonliteral scene sub-feature key was classified as an active global: sites=%+v globals=%+v", sites, globals)
	}
	if globals["__gosx_not_literal"] || containsStaticSiteName(&RuntimeJSONStaticCorpus{Sites: sites}, "__gosx_not_literal") {
		t.Fatalf("bare identifier scene sub-feature key was classified as an active global: sites=%+v globals=%+v", sites, globals)
	}
}

func TestRuntimeJSONStaticCorpusCoversProductionStrictSceneIRExport(t *testing.T) {
	path := filepath.Join("..", "..", "client", "js", "bootstrap-src", "15-scene-ir-schema-strict.js")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read production strict SceneIR schema: %v", err)
	}
	src := SourceFile{Path: "client/js/bootstrap-src/15-scene-ir-schema-strict.js", SourceKind: "scoreboard", Language: "javascript"}
	sites, err := runtimeJSONSitesForFile(src, body, map[string]bool{})
	if err != nil {
		t.Fatalf("runtimeJSONSitesForFile: %v", err)
	}
	if got := countStaticGlobalSite(sites, "__gosx_validate_scene_ir_strict", "gosx-write"); got < 2 {
		t.Fatalf("strict SceneIR export writes = %d, want defineProperty and fallback assignment; sites=%+v", got, sites)
	}
	if !hasStaticGlobalSite(sites, "__gosx_validate_scene_ir_strict", "gosx-read") {
		t.Fatalf("strict SceneIR fallback read missing; sites=%+v", sites)
	}
}

func TestRuntimeJSONStaticCorpusAliasDoesNotLeakAcrossScopes(t *testing.T) {
	src := SourceFile{Path: "client/js/bootstrap-src/scope-negative.js", SourceKind: "scoreboard", Language: "javascript"}
	body := []byte(strings.Join([]string{
		"function first(){",
		"  const root = window;",
		"  root.__gosx_local = function(){};",
		"}",
		"function second(){",
		"  root.__gosx_leaked = function(){};",
		"}",
		"if (true) {",
		"  const scoped = window;",
		"  scoped.__gosx_blocked = function(){};",
		"}",
		"scoped.__gosx_block_leaked = function(){};",
		"",
	}, "\n"))
	sites, err := runtimeJSONSitesForFile(src, body, map[string]bool{})
	if err != nil {
		t.Fatalf("runtimeJSONSitesForFile: %v", err)
	}
	if !hasStaticGlobalSite(sites, "__gosx_local", "gosx-write") || !hasStaticGlobalSite(sites, "__gosx_blocked", "gosx-write") {
		t.Fatalf("expected in-scope alias writes missing: %+v", sites)
	}
	for _, leaked := range []string{"__gosx_leaked", "__gosx_block_leaked"} {
		if hasStaticGlobalSite(sites, leaked, "gosx-write") {
			t.Fatalf("alias leaked across scope for %s: %+v", leaked, sites)
		}
	}
}

func TestRuntimeJSONStaticCorpusSerializationRowsHaveOwners(t *testing.T) {
	src := SourceFile{Path: "client/js/bootstrap-src/15-scene-ir-schema.js", SourceKind: "scoreboard", Language: "javascript"}
	body := []byte(`const kind = "mesh"; function validateSceneIR(node){ errors.push("kind " + JSON.stringify(kind)); return JSON.parse(node.propsJSON); }`)
	sites, err := runtimeJSONSitesForFile(src, body, map[string]bool{})
	if err != nil {
		t.Fatalf("runtimeJSONSitesForFile: %v", err)
	}
	for _, site := range sites {
		if strings.Contains(site.Operation, "json") && site.Symbol == "" {
			t.Fatalf("serialization row has empty owner: %+v", site)
		}
	}
	if !hasStaticOperation(sites, "json-stringify") || !hasStaticOperation(sites, "json-parse") || !hasStaticOperation(sites, "props-json") {
		t.Fatalf("missing expected scene IR serialization sites: %+v", sites)
	}
}

func TestRuntimeJSONStaticPhaseClassifierExamples(t *testing.T) {
	cases := []struct {
		name         string
		src          SourceFile
		body         string
		op           string
		status       string
		phase        string
		wantPhases   []string
		rule         string
		hotPossible  bool
		hotConfirmed bool
	}{
		{
			name:        "navigation runtime route parse",
			src:         SourceFile{Path: "server/navigation_runtime.js", SourceKind: "embedded", Language: "javascript"},
			body:        `function readRoute(node){ return JSON.parse(String(node.textContent || "{}")); }`,
			op:          "json-parse",
			status:      "exact",
			phase:       "route-load",
			wantPhases:  []string{"route-load"},
			rule:        "R10",
			hotPossible: false,
		},
		{
			name:        "tail hub event stringify",
			src:         SourceFile{Path: "client/js/bootstrap-src/30c-tail-hub-connections.js", SourceKind: "scoreboard", Language: "javascript"},
			body:        `function sendTailEvent(binding, value){ socket.send(JSON.stringify({ event: binding.event, data: value || {} })); }`,
			op:          "json-stringify",
			status:      "exact",
			phase:       "network",
			wantPhases:  []string{"network"},
			rule:        "R10",
			hotPossible: false,
		},
		{
			name:         "declarative patch",
			src:          SourceFile{Path: "client/js/bootstrap-src/06-declarative-actions.js", SourceKind: "scoreboard", Language: "javascript"},
			body:         `function applyPatch(patchJSON){ return JSON.parse(patchJSON); }`,
			op:           "patch-json",
			status:       "exact",
			phase:        "patch",
			wantPhases:   []string{"patch"},
			rule:         "R10",
			hotPossible:  true,
			hotConfirmed: true,
		},
		{
			name:        "text layout shared value",
			src:         SourceFile{Path: "client/js/bootstrap-src/00-textlayout.js", SourceKind: "scoreboard", Language: "javascript"},
			body:        `function parseSharedSignalJSON(valueJSON){ return JSON.parse(valueJSON); }`,
			op:          "value-json",
			status:      "ambiguous",
			wantPhases:  []string{"input", "reconciliation", "frame"},
			rule:        "R10",
			hotPossible: true,
		},
		{
			name:        "unknown serialization",
			src:         SourceFile{Path: "client/js/bootstrap-src/mystery.js", SourceKind: "scoreboard", Language: "javascript"},
			body:        `function mystery(value){ return JSON.stringify(value); }`,
			op:          "json-stringify",
			status:      "unknown",
			wantPhases:  runtimeJSONAllPhases,
			rule:        "R90",
			hotPossible: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sites, err := runtimeJSONSitesForFile(tc.src, []byte(tc.body), map[string]bool{})
			if err != nil {
				t.Fatalf("runtimeJSONSitesForFile: %v", err)
			}
			site, ok := firstStaticOperation(sites, tc.op)
			if !ok {
				t.Fatalf("missing %s in sites: %+v", tc.op, sites)
			}
			if site.PhaseStatus != tc.status || site.Phase != tc.phase || site.PhaseRule != tc.rule {
				t.Fatalf("phase classification = status %q phase %q rule %q, want %q %q %q: %+v", site.PhaseStatus, site.Phase, site.PhaseRule, tc.status, tc.phase, tc.rule, site)
			}
			if !sameStringSlice(site.PossiblePhases, tc.wantPhases) {
				t.Fatalf("possible phases = %v, want %v", site.PossiblePhases, tc.wantPhases)
			}
			if site.HotPathPossible != tc.hotPossible || site.HotPathConfirmed != tc.hotConfirmed {
				t.Fatalf("hot flags = possible %v confirmed %v, want %v %v", site.HotPathPossible, site.HotPathConfirmed, tc.hotPossible, tc.hotConfirmed)
			}
			if err := validateRuntimeJSONStaticSite(site); err != nil {
				t.Fatalf("validate site: %v", err)
			}
		})
	}
}

func TestValidateRuntimeJSONStaticCorpusRejectsInvalidRowsAndCounts(t *testing.T) {
	src := SourceFile{Path: "client/js/bootstrap-src/06-declarative-actions.js", SourceKind: "scoreboard", Language: "javascript"}
	sites, err := runtimeJSONSitesForFile(src, []byte(`function applyPatch(patchJSON){ return JSON.parse(patchJSON); }`), map[string]bool{})
	if err != nil {
		t.Fatalf("runtimeJSONSitesForFile: %v", err)
	}
	base := runtimeJSONCorpusForSitesForTest(t, sites)
	if err := ValidateRuntimeJSONStaticCorpus(base); err != nil {
		t.Fatalf("base corpus validation failed: %v", err)
	}
	tests := []struct {
		name   string
		mutate func(*RuntimeJSONStaticCorpus)
	}{
		{
			name: "empty possible phases",
			mutate: func(c *RuntimeJSONStaticCorpus) {
				c.Sites[0].PossiblePhases = nil
			},
		},
		{
			name: "exact phase mismatch",
			mutate: func(c *RuntimeJSONStaticCorpus) {
				c.Sites[0].PhaseStatus = "exact"
				c.Sites[0].PossiblePhases = []string{"patch"}
				c.Sites[0].Phase = "input"
			},
		},
		{
			name: "count mismatch",
			mutate: func(c *RuntimeJSONStaticCorpus) {
				c.Counts.SerializationSiteCount++
			},
		},
		{
			name: "classifier mismatch",
			mutate: func(c *RuntimeJSONStaticCorpus) {
				c.Query.PhaseClassifier = "stale"
			},
		},
		{
			name: "scanner mismatch",
			mutate: func(c *RuntimeJSONStaticCorpus) {
				c.ScannerVersion = "stale"
			},
		},
		{
			name: "counts hash mismatch",
			mutate: func(c *RuntimeJSONStaticCorpus) {
				c.CountsHash = "sha256:stale"
			},
		},
		{
			name: "semantic hash mismatch",
			mutate: func(c *RuntimeJSONStaticCorpus) {
				c.SemanticHash = "sha256:stale"
			},
		},
		{
			name: "global set mismatch",
			mutate: func(c *RuntimeJSONStaticCorpus) {
				c.GlobalNames.Names = append(c.GlobalNames.Names, "__gosx_extra")
			},
		},
		{
			name: "source identity hash mismatch",
			mutate: func(c *RuntimeJSONStaticCorpus) {
				c.CurrentSourceIdentityHash = "sha256:stale"
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			corpus := cloneRuntimeJSONCorpusForTest(t, base)
			tt.mutate(corpus)
			if err := ValidateRuntimeJSONStaticCorpus(corpus); err == nil {
				t.Fatal("ValidateRuntimeJSONStaticCorpus succeeded for invalid corpus")
			}
		})
	}
}

func containsStaticSiteName(corpus *RuntimeJSONStaticCorpus, text string) bool {
	for _, site := range corpus.Sites {
		if strings.Contains(site.Path, text) || strings.Contains(site.Operation, text) || strings.Contains(site.Symbol, text) {
			return true
		}
	}
	return false
}

func firstStaticOperation(sites []RuntimeJSONStaticSite, op string) (RuntimeJSONStaticSite, bool) {
	for _, site := range sites {
		if site.Operation == op {
			return site, true
		}
	}
	return RuntimeJSONStaticSite{}, false
}

func hasStaticGlobalSite(sites []RuntimeJSONStaticSite, globalName, op string) bool {
	for _, site := range sites {
		if site.GlobalName == globalName && site.Operation == op {
			return true
		}
	}
	return false
}

func countStaticGlobalSite(sites []RuntimeJSONStaticSite, globalName, op string) int {
	count := 0
	for _, site := range sites {
		if site.GlobalName == globalName && site.Operation == op {
			count++
		}
	}
	return count
}

func hasStaticOperation(sites []RuntimeJSONStaticSite, op string) bool {
	for _, site := range sites {
		if site.Operation == op {
			return true
		}
	}
	return false
}

func runtimeJSONCorpusForSitesForTest(t *testing.T, sites []RuntimeJSONStaticSite) *RuntimeJSONStaticCorpus {
	t.Helper()
	corpus := &RuntimeJSONStaticCorpus{
		SchemaVersion:          RuntimeJSONProbeSchemaVersion,
		Contract:               ContractO02,
		ScannerVersion:         runtimeJSONStaticScannerVersion,
		PhaseClassifierVersion: runtimeJSONPhaseClassifierVersion,
		Query: RuntimeJSONStaticQuery{
			PhaseClassifier: runtimeJSONPhaseClassifierVersion,
		},
		Sites: sites,
		Counts: RuntimeJSONStaticCounts{
			ByOperation:     map[string]int{},
			ByPhase:         map[string]int{},
			ByPossiblePhase: map[string]int{},
			ByPhaseStatus:   map[string]int{},
			BySourceFamily:  map[string]int{},
			TargetStatus:    "undefined/fail-closed",
			FailClosed:      true,
		},
	}
	globals := map[string]bool{}
	for _, site := range sites {
		corpus.Counts.ByOperation[site.Operation]++
		if site.Phase != "" {
			corpus.Counts.ByPhase[site.Phase]++
		}
		for _, phase := range site.PossiblePhases {
			corpus.Counts.ByPossiblePhase[phase]++
		}
		corpus.Counts.ByPhaseStatus[site.PhaseStatus]++
		corpus.Counts.BySourceFamily[site.SourceFamily]++
		if site.GlobalName != "" {
			globals[site.GlobalName] = true
		}
		if site.HotPathPossible {
			corpus.Counts.HotPathPossibleCount++
		}
		if site.HotPathConfirmed {
			corpus.Counts.HotPathConfirmedCount++
		}
		switch site.PhaseStatus {
		case "unknown":
			corpus.Counts.UnknownCount++
		case "ambiguous":
			corpus.Counts.AmbiguousCount++
		case "exact":
			corpus.Counts.ExactCount++
		}
		if isRuntimeJSONSerializationOperation(site.Operation) {
			corpus.Counts.SerializationSiteCount++
			if site.HotPathPossible {
				corpus.Counts.SerializationHotPathPossibleCount++
			}
			if site.HotPathConfirmed {
				corpus.Counts.SerializationHotPathConfirmedCount++
			}
		}
		switch site.Operation {
		case "json-parse":
			corpus.Counts.JSONParseCount++
		case "json-stringify":
			corpus.Counts.JSONStringifyCount++
		case "props-json":
			corpus.Counts.PropsJSONCount++
		case "patch-json":
			corpus.Counts.PatchJSONCount++
		case "value-json":
			corpus.Counts.ValueJSONCount++
		case "gosx-read":
			corpus.Counts.GosxReadCount++
		case "gosx-write":
			corpus.Counts.GosxWriteCount++
		case "gosx-call":
			corpus.Counts.GosxCallCount++
		}
	}
	corpus.Counts.UniqueGosxGlobals = len(globals)
	corpus.Source = SourceIdentity{BaseRevision: "test", OverlayHash: "sha256:test"}
	corpus.CurrentSourceIdentity = corpus.Source
	corpus.CurrentSourceIdentityHash = RuntimeJSONStaticSourceIdentityHash(corpus.Source)
	corpus.GlobalNames = RuntimeJSONStaticGlobalSetFromMap(globals)
	corpus.CountsHash = RuntimeJSONStaticCountsHash(corpus.Counts)
	corpus.SemanticHash = RuntimeJSONStaticCorpusSemanticHash(corpus)
	return corpus
}

func cloneRuntimeJSONCorpusForTest(t *testing.T, corpus *RuntimeJSONStaticCorpus) *RuntimeJSONStaticCorpus {
	t.Helper()
	body, err := json.Marshal(corpus)
	if err != nil {
		t.Fatalf("marshal corpus: %v", err)
	}
	var out RuntimeJSONStaticCorpus
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("unmarshal corpus: %v", err)
	}
	return &out
}

func TestRuntimeJSONProbeCoverageReportsMissingKindsAndPhases(t *testing.T) {
	missing := RuntimeJSONProbeCoverage([]ProbeEvent{
		{Kind: "json-call", Phase: "input"},
	}, []string{"R02"}, []string{"input", "dispatch"})
	got := strings.Join(missing, ",")
	if !strings.Contains(got, "kind:runtime-call") || !strings.Contains(got, "phase:dispatch") {
		t.Fatalf("missing coverage = %v", missing)
	}
}

func writeRuntimeJSONInventoryForProbeTest(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, ".gitignore", "build/\n")
	writeFile(t, root, "client/js/bootstrap-src/00-runtime.js", "window.__gosx_runtime_ready = function(){};\n")
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.invalid")
	runGit(t, root, "config", "user.name", "test")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "base")
	writeFile(t, root, "client/js/bootstrap-src/00-runtime.js", "window.__gosx_runtime_ready = function(){};\nwindow.__gosx_action = function(){};\n")
	artifactRoot := filepath.Join(root, "build", "ouroboros", "test-runtime-json-inventory")
	inv, err := Collect(context.Background(), CollectOptions{
		RepoRoot:     root,
		ArtifactRoot: artifactRoot,
		GeneratedAt:  time.Unix(0, 0).UTC(),
		Git:          true,
		Canopy:       false,
	})
	if err != nil {
		t.Fatalf("Collect inventory: %v", err)
	}
	if err := WriteOverlayArtifacts(context.Background(), root, artifactRoot, inv.Overlay); err != nil {
		t.Fatalf("WriteOverlayArtifacts: %v", err)
	}
	inv.Overlay.PatchPath = filepath.ToSlash(filepath.Join("build", "ouroboros", "test-runtime-json-inventory", "tracked-overlay.patch"))
	inv.Overlay.ArchivePath = filepath.ToSlash(filepath.Join("build", "ouroboros", "test-runtime-json-inventory", "untracked-sources"))
	path := filepath.Join(artifactRoot, "source-inventory.json")
	if err := WriteJSONFile(path, inv); err != nil {
		t.Fatalf("WriteJSONFile inventory: %v", err)
	}
	return root, path
}

func runtimeJSONInventoryOptsForTest(root, inventoryPath string) RuntimeJSONProbeOptions {
	return RuntimeJSONProbeOptions{
		RepoRoot:      root,
		InventoryPath: inventoryPath,
		ArtifactRoot:  filepath.Join(root, "build", "ouroboros", "test-runtime-json-strict"),
		GeneratedAt:   time.Unix(0, 0).UTC(),
		Git:           true,
	}
}

func assertRuntimeJSONInventoryMutationRejected(t *testing.T, root, invPath, want string, mutate func(*Inventory)) {
	t.Helper()
	var inv Inventory
	mustJSONUnmarshalForTest(t, readFileForTest(t, invPath), &inv)
	mutate(&inv)
	bad := filepath.Join(t.TempDir(), "inventory.json")
	if err := WriteJSONFile(bad, inv); err != nil {
		t.Fatalf("WriteJSONFile mutated inventory: %v", err)
	}
	_, _, err := loadRuntimeJSONInventory(context.Background(), runtimeJSONInventoryOptsForTest(root, bad))
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("mutated inventory error = %v, want %q", err, want)
	}
}

func mustJSONUnmarshalForTest(t *testing.T, body []byte, out any) {
	t.Helper()
	if err := json.Unmarshal(body, out); err != nil {
		t.Fatalf("json unmarshal: %v", err)
	}
}
