package budget

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"m31labs.dev/gosx/buildmanifest"
)

func fixtureProducer(t *testing.T) (ProducerOptions, []byte) {
	t.Helper()
	measured, manifest, document, _ := testRouteMeasurement(t)
	inputs, err := LoadInputs("testdata/budget.v2.json", LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	catalog := map[string]any{"schema": "gosx.perf-fixture-catalog/v1", "version": 1, "routes": manifest.Routes, "assetRules": []any{
		map[string]any{"id": manifest.Assets[0].ID, "owner": "app", "kind": "html", "phase": "critical", "condition": "always", "dependencies": []string{}},
	}, "interactionContract": inputs.File.Profile}
	raw, _ := json.Marshal(catalog)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "catalog.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	inputs.rootDir = root
	inputs.File.Fixtures = Ref{File: "catalog.json", SHA256: producerHash(raw)}
	build := &buildmanifest.Manifest{PerfAssetUses: &buildmanifest.PerfAssetUses{Version: 1, Assets: append([]buildmanifest.PerfAssetUse{}, manifest.Assets[1:]...)}}
	if err := os.Remove(filepath.Join(measured.DistDir, "perf-fixtures.v1.json")); err != nil {
		t.Fatal(err)
	}
	return ProducerOptions{Inputs: inputs, Build: build, App: "fixture", DistDir: measured.DistDir, BaseURL: measured.BaseURL, SourceSHA: manifest.SourceSHA, Client: measured.Client}, document
}

func TestProducerPublicSidecarsMatchWholeBodies(t *testing.T) {
	for _, stale := range []bool{false, true} {
		t.Run(map[bool]string{false: "valid", true: "stale"}[stale], func(t *testing.T) {
			opts, _ := fixtureProducer(t)
			catalogPath := filepath.Join(opts.Inputs.rootDir, "catalog.json")
			raw, _ := os.ReadFile(catalogPath)
			var catalog map[string]any
			json.Unmarshal(raw, &catalog)
			rule := map[string]any{"id": "app/fixture/public/styles.css", "owner": "app", "kind": "css", "phase": "dormant", "condition": "always", "dependencies": []string{}}
			catalog["assetRules"] = append(catalog["assetRules"].([]any), rule)
			raw, _ = json.Marshal(catalog)
			os.WriteFile(catalogPath, raw, 0600)
			opts.Inputs.File.Fixtures.SHA256 = producerHash(raw)
			os.MkdirAll(filepath.Join(opts.DistDir, "public"), 0700)
			body := []byte("body{color:green}")
			os.WriteFile(filepath.Join(opts.DistDir, "public/styles.css"), body, 0600)
			var encoded bytes.Buffer
			writer := gzip.NewWriter(&encoded)
			if stale {
				body = []byte("body{color:red}")
			}
			writer.Write(body)
			writer.Close()
			os.WriteFile(filepath.Join(opts.DistDir, "public/styles.css.gz"), encoded.Bytes(), 0600)
			digest, err := ProduceFixture(context.Background(), opts)
			if stale {
				if err == nil || digest != "" {
					t.Fatal("stale release representation accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			saved, err := os.ReadFile(filepath.Join(opts.DistDir, "styles.css.gz"))
			if err != nil || !bytes.Equal(saved, encoded.Bytes()) {
				t.Fatal("verified release sidecar was discarded", err)
			}
		})
	}
}

func TestProducerPreservesVerifiedPrerenderSidecars(t *testing.T) {
	for _, stale := range []bool{false, true} {
		t.Run(map[bool]string{false: "valid", true: "stale"}[stale], func(t *testing.T) {
			opts, body := fixtureProducer(t)
			static := filepath.Join(opts.DistDir, "static/counter")
			if err := os.MkdirAll(static, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(static, "index.html"), body, 0600); err != nil {
				t.Fatal(err)
			}
			var encoded bytes.Buffer
			writer := gzip.NewWriter(&encoded)
			if stale {
				body = []byte("stale document")
			}
			writer.Write(body)
			writer.Close()
			os.WriteFile(filepath.Join(static, "index.html.gz"), encoded.Bytes(), 0600)
			digest, err := ProduceFixture(context.Background(), opts)
			if stale {
				if err == nil || digest != "" {
					t.Fatal("stale prerender encoding accepted")
				}
				return
			}
			saved, readErr := os.ReadFile(filepath.Join(opts.DistDir, "counter/index.html.gz"))
			if err != nil || readErr != nil || !bytes.Equal(saved, encoded.Bytes()) {
				t.Fatal("prerender encoding missing", err, readErr)
			}
		})
	}
}

func TestProducerSnapshotsVerifiedBodiesAndIndependentProof(t *testing.T) {
	opts, document := fixtureProducer(t)
	before, _ := json.Marshal(opts.Build)
	digest, err := ProduceFixture(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(opts.DistDir, "perf-fixtures.v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := DecodeFixtureManifest(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if digest != manifest.FixturesSHA256 || digest == manifest.CatalogSHA256 || manifest.SourceSHA != opts.SourceSHA || manifest.CatalogSHA256 != opts.Inputs.File.Fixtures.SHA256 || len(manifest.Routes) != 1 || len(manifest.Assets) != 2 {
		t.Fatal("producer proof or coverage incorrect")
	}
	after, _ := json.Marshal(opts.Build)
	if !bytes.Equal(before, after) {
		t.Fatal("producer mutated build graph")
	}
	saved, err := os.ReadFile(filepath.Join(opts.DistDir, "counter/index.html"))
	if err != nil || !bytes.Equal(saved, document) {
		t.Fatal("snapshot differs from served document")
	}
	artifact := digest
	result, err := measureApp(context.Background(), MeasureOptions{App: opts.App, DistDir: opts.DistDir, BaseURL: opts.BaseURL, Client: opts.Client, Public: PublicInfo{SHA: opts.SourceSHA, FixtureSHA256: opts.Inputs.File.Fixtures.SHA256, ArtifactSHA256: &artifact}}, testBodyNormalizer)
	if err != nil || result.Coverage.RoutesMeasured != 1 || result.Coverage.AssetsMeasured != 2 {
		t.Fatal("producer contract unusable by collector", err)
	}
	second, err := ProduceFixture(context.Background(), opts)
	if err != nil || second != digest {
		t.Fatal("identical production bodies changed proof", err)
	}
}

func TestProducerRejectsMissingBindingsAndStaleBodies(t *testing.T) {
	changes := map[string]func(*ProducerOptions){
		"inputs":  func(o *ProducerOptions) { o.Inputs = nil },
		"build":   func(o *ProducerOptions) { o.Build = nil },
		"graph":   func(o *ProducerOptions) { o.Build.PerfAssetUses = nil },
		"source":  func(o *ProducerOptions) { o.SourceSHA = "private-value" },
		"app":     func(o *ProducerOptions) { o.App = "unregistered" },
		"dist":    func(o *ProducerOptions) { o.DistDir = "missing-directory" },
		"base":    func(o *ProducerOptions) { o.BaseURL = "https://private-value.invalid/?secret=1" },
		"catalog": func(o *ProducerOptions) { o.Inputs.File.Fixtures.SHA256 = strings.Repeat("f", 64) },
		"body":    func(o *ProducerOptions) { o.Build.PerfAssetUses.Assets[0].SHA256 = strings.Repeat("f", 64) },
		"duplicates": func(o *ProducerOptions) {
			o.Build.PerfAssetUses.Assets = append(o.Build.PerfAssetUses.Assets, o.Build.PerfAssetUses.Assets[0])
		},
		"foreign-app": func(o *ProducerOptions) {
			o.Build.PerfAssetUses.Assets[0].Owner = "app"
			o.Build.PerfAssetUses.Assets[0].ID = "app/other/program"
		},
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			opts, _ := fixtureProducer(t)
			change(&opts)
			digest, err := ProduceFixture(context.Background(), opts)
			var typed *InputError
			if digest != "" || !errors.As(err, &typed) || strings.Contains(err.Error(), "private-value") {
				t.Fatal("invalid producer input accepted or echoed", err)
			}
			if _, err := os.Stat(filepath.Join(opts.DistDir, "perf-fixtures.v1.json")); !os.IsNotExist(err) {
				t.Fatal("rejected producer published a contract")
			}
		})
	}
	opts, _ := fixtureProducer(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, ctx := range []context.Context{nil, ctx} {
		if _, err := ProduceFixture(ctx, opts); err == nil {
			t.Fatal("invalid context accepted")
		}
	}
}

func TestProducerRejectsUnstableHTTPAndCapabilityContract(t *testing.T) {
	for _, kind := range []string{"status", "encoding", "redirect", "private-error", "capabilities", "truncated"} {
		t.Run(kind, func(t *testing.T) {
			opts, document := fixtureProducer(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				switch kind {
				case "status":
					w.WriteHeader(404)
				case "encoding":
					w.Header().Set("Content-Encoding", "gzip")
					w.Write(document)
				case "redirect":
					w.Header().Set("Location", "https://private-value.invalid/")
					w.WriteHeader(302)
				case "private-error":
					w.WriteHeader(500)
					w.Write([]byte("private-value"))
				case "capabilities":
					w.Write([]byte(`<main data-gosx-bootstrap="lite">Changed</main><script src="/_gosx/navigation.js"></script>`))
				case "truncated":
					w.Header().Set("Content-Length", "999999")
					w.Write(document)
				}
			}))
			defer server.Close()
			opts.BaseURL = server.URL
			opts.Client = server.Client()
			digest, err := ProduceFixture(context.Background(), opts)
			if digest != "" || err == nil || strings.Contains(err.Error(), "private-value") {
				t.Fatal("invalid HTTP evidence accepted or echoed", err)
			}
		})
	}
}

func TestProducerPublicBodiesAndOutputConfinement(t *testing.T) {
	opts, _ := fixtureProducer(t)
	catalogPath := filepath.Join(opts.Inputs.rootDir, "catalog.json")
	raw, _ := os.ReadFile(catalogPath)
	var catalog map[string]any
	json.Unmarshal(raw, &catalog)
	rule := map[string]any{"id": "app/fixture/public/styles.css", "owner": "app", "kind": "css", "phase": "dormant", "condition": "always", "dependencies": []string{}}
	catalog["assetRules"] = append(catalog["assetRules"].([]any), rule)
	raw, _ = json.Marshal(catalog)
	os.WriteFile(catalogPath, raw, 0600)
	opts.Inputs.File.Fixtures.SHA256 = producerHash(raw)
	if err := os.MkdirAll(filepath.Join(opts.DistDir, "public"), 0700); err != nil {
		t.Fatal(err)
	}
	css := []byte("body{color:green}")
	if err := os.WriteFile(filepath.Join(opts.DistDir, "public/styles.css"), css, 0600); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(opts.DistDir, "styles.css.gz")
	os.WriteFile(stale, []byte("stale"), 0600)
	digest, err := ProduceFixture(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	saved, _ := os.ReadFile(filepath.Join(opts.DistDir, "styles.css"))
	if !bytes.Equal(saved, css) {
		t.Fatal("public fixture snapshot missing")
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("stale sidecar survived snapshot")
	}
	raw, _ = os.ReadFile(filepath.Join(opts.DistDir, "perf-fixtures.v1.json"))
	manifest, err := DecodeFixtureManifest(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, a := range manifest.Assets {
		found = found || a.ID == rule["id"] && a.Kind == "css" && a.SHA256 == producerHash(css)
	}
	if !found || digest != manifest.FixturesSHA256 {
		t.Fatal("public asset provenance missing")
	}
	root, err := os.OpenRoot(opts.DistDir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	for _, name := range []string{"../escape", "/escape", `C:\private\escape`} {
		if err := writeProducerFile(root, name, []byte("private-value")); err == nil {
			t.Fatal("output escaped root")
		}
	}
	native, _ := json.Marshal(opts)
	if !reflect.DeepEqual(native, []byte("{}")) {
		t.Fatal("private producer bindings became serializable")
	}
}

func TestProducerRequiresCompletedWASMOptimizerProof(t *testing.T) {
	for _, cause := range []string{"valid", "missing", "applied", "tool", "version", "input", "output"} {
		t.Run(cause, func(t *testing.T) {
			opts, _ := fixtureProducer(t)
			body := []byte("\x00asm\x01\x00\x00\x00")
			sha := producerHash(body)
			name := "core." + sha[:16] + ".wasm"
			file := filepath.Join(opts.DistDir, "assets/runtime", name)
			if err := os.WriteFile(file, body, 0600); err != nil {
				t.Fatal(err)
			}
			use := buildmanifest.PerfAssetUse{ID: "framework/runtime/core.wasm", SHA256: sha, URL: "/gosx/assets/runtime/" + name, Owner: "framework", Kind: "wasm", Phase: "dormant", Condition: "always", Dependencies: []string{}}
			opts.Build.PerfAssetUses.Assets = append(opts.Build.PerfAssetUses.Assets, use)
			proof := buildmanifest.WASMOptimization{Tool: "wasm-opt", Version: "108", Applied: true, InputSHA256: strings.Repeat("1", 64), OutputSHA256: sha}
			switch cause {
			case "applied":
				proof.Applied = false
			case "tool":
				proof.Tool = "other"
			case "version":
				proof.Version = "other"
			case "input":
				proof.InputSHA256 = "missing"
			case "output":
				proof.OutputSHA256 = strings.Repeat("f", 64)
			}
			if cause != "missing" {
				opts.Build.Runtime.WASMOptimization = map[string]buildmanifest.WASMOptimization{"core": proof}
			}
			digest, err := ProduceFixture(context.Background(), opts)
			if cause == "valid" {
				if err != nil || digest == "" {
					t.Fatal("valid optimizer proof rejected", err)
				}
			} else if err == nil || digest != "" {
				t.Fatal("unverified optimizer accepted")
			}
		})
	}
}
