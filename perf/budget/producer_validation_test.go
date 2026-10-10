package budget

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"m31labs.dev/gosx/buildmanifest"
)

func producerDistributionSnapshot(t *testing.T, dist string) map[string]string {
	t.Helper()
	files := map[string]string{}
	if err := filepath.WalkDir(dist, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dist, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			files[rel+"/"] = ""
			return nil
		}
		body, err := os.ReadFile(path)
		if err == nil {
			files[rel] = string(body)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return files
}

func producerRequireNoWrites(t *testing.T, opts ProducerOptions, before map[string]string) {
	t.Helper()
	digest, err := ProduceFixture(context.Background(), opts)
	if err == nil || digest != "" {
		t.Errorf("invalid fixture published: digest=%q error=%v", digest, err)
	}
	if !reflect.DeepEqual(before, producerDistributionSnapshot(t, opts.DistDir)) {
		t.Error("rejection changed distribution files")
	}
}

func TestProducerRegisteredOwnershipMetadata(t *testing.T) {
	for _, owner := range []string{"app", "framework"} {
		for _, registered := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/registered=%v", owner, registered), func(t *testing.T) {
				opts, document := fixtureProducer(t)
				image := []byte("registered dependency")
				producerTestPublic(t, opts, "dependency.png", "image", image)
				id := "app/fixture/scheduled.js"
				if owner == "framework" {
					id = "framework/runtime/scheduled.js"
				}
				use := buildmanifest.PerfAssetUse{ID: id, URL: "/scheduled.js", SHA256: testMeasureHash([]byte("const scheduled=1;")),
					Owner: owner, Kind: "js", Phase: "startup", Condition: "interaction", Dependencies: []string{"app/fixture/public/dependency.png"}}
				want := use
				if registered {
					producerTestCatalog(t, opts, func(catalog map[string]any) {
						catalog["assetRules"] = append(catalog["assetRules"].([]any), map[string]any{"id": id, "owner": owner, "kind": use.Kind,
							"phase": use.Phase, "condition": use.Condition, "dependencies": use.Dependencies})
					})
					use.Phase, use.Condition, use.Dependencies = "dormant", "always", []string{}
				}
				producerTestFile(t, opts.DistDir, "scheduled.js", []byte("const scheduled=1;"))
				opts.Build.PerfAssetUses.Assets = append(opts.Build.PerfAssetUses.Assets, use)
				before := append([]buildmanifest.PerfAssetUse{}, opts.Build.PerfAssetUses.Assets...)
				producerTestServe(&opts, map[string][]byte{"/counter/": document, "/scheduled.js": []byte("const scheduled=1;"), "/dependency.png": image},
					map[string]string{"/counter/": "html", "/scheduled.js": "js", "/dependency.png": "image"})
				digest, err := ProduceFixture(context.Background(), opts)
				if err != nil {
					t.Fatal(err)
				}
				raw, err := os.ReadFile(filepath.Join(opts.DistDir, fixtureManifestFile))
				if err != nil {
					t.Fatal(err)
				}
				manifest, err := DecodeFixtureManifest(bytes.NewReader(raw))
				if err != nil {
					t.Fatal(err)
				}
				found := false
				for _, asset := range manifest.Assets {
					if asset.ID == id {
						found = true
						if !reflect.DeepEqual(asset, want) {
							t.Errorf("metadata differs: got %+v want %+v", asset, want)
						}
					}
				}
				if !found {
					t.Fatal("scheduled identity missing")
				}
				for _, chunks := range []bool{false, true} {
					report := producerTestCollect(t, opts, digest, chunks)
					found = false
					for _, asset := range report.Assets {
						if asset.ID == id {
							found = true
							if asset.Phase != "startup" || asset.Condition != want.Condition || !reflect.DeepEqual(asset.Dependencies, want.Dependencies) {
								t.Errorf("collected registered scheduling changed: %+v", asset)
							}
						}
					}
					if !found {
						t.Fatal("collected identity missing")
					}
				}
				if !reflect.DeepEqual(before, opts.Build.PerfAssetUses.Assets) {
					t.Fatal("caller inventory changed")
				}
			})
		}
	}
}

func TestProducerSharedURLCompatibility(t *testing.T) {
	for _, conflict := range []string{"none", "kind", "hash"} {
		t.Run(conflict, func(t *testing.T) {
			opts, _ := fixtureProducer(t)
			framework := opts.Build.PerfAssetUses.Assets[0]
			alias := framework
			alias.ID, alias.Owner = "app/fixture/alias.js", "app"
			if conflict == "kind" {
				alias.Kind = "other"
			}
			if conflict == "hash" {
				alias.SHA256 = testMeasureHash([]byte("different body"))
			}
			producerTestCatalog(t, opts, func(catalog map[string]any) {
				catalog["assetRules"] = append(catalog["assetRules"].([]any), map[string]any{"id": alias.ID, "owner": alias.Owner, "kind": alias.Kind,
					"phase": alias.Phase, "condition": alias.Condition, "dependencies": alias.Dependencies})
			})
			opts.Build.PerfAssetUses.Assets = append(opts.Build.PerfAssetUses.Assets, alias)
			if conflict != "none" {
				producerRequireNoWrites(t, opts, producerDistributionSnapshot(t, opts.DistDir))
				return
			}
			digest, err := ProduceFixture(context.Background(), opts)
			if err != nil {
				t.Fatal(err)
			}
			for _, chunks := range []bool{false, true} {
				report := producerTestCollect(t, opts, digest, chunks)
				if len(report.Assets) != 2 {
					t.Fatal("physical alias was not deduplicated")
				}
			}
		})
	}
}

func TestProducerCollectorErrorsBeforeWrites(t *testing.T) {
	for _, failure := range []string{"undeclared-fetch", "wrong-backend", "registered-owner", "registered-kind", "stale-sidecar", "capabilities"} {
		t.Run(failure, func(t *testing.T) {
			opts, document := fixtureProducer(t)
			producerTestPublic(t, opts, "snapshot.bin", "other", []byte("pending public snapshot"))
			producerTestFile(t, opts.DistDir, fixtureManifestFile, []byte("previous manifest"))
			switch failure {
			case "undeclared-fetch":
				document = bytes.Replace(document, []byte("</head>"), []byte(`<link rel="stylesheet" href="/missing.css"></head>`), 1)
			case "wrong-backend":
				opts.Build.PerfAssetUses.Assets[0].Phase = "startup"
				opts.Build.PerfAssetUses.Assets[0].Dependencies = []string{"app/fixture/public/snapshot.bin"}
				producerTestCatalog(t, opts, func(catalog map[string]any) {
					catalog["assetRules"].([]any)[1].(map[string]any)["condition"] = "webgpu"
				})
			case "registered-owner", "registered-kind":
				framework := opts.Build.PerfAssetUses.Assets[0]
				producerTestCatalog(t, opts, func(catalog map[string]any) {
					fields := producerJSONFields(t, framework)
					delete(fields, "sha256")
					delete(fields, "url")
					if failure == "registered-owner" {
						fields["owner"] = "app"
					} else {
						fields["kind"] = "other"
					}
					catalog["assetRules"] = append(catalog["assetRules"].([]any), fields)
				})
			case "stale-sidecar":
				producerTestFile(t, opts.DistDir, "public/snapshot.bin.gz", []byte("invalid gzip"))
			case "capabilities":
				document = bytes.Replace(document, []byte("</head>"), []byte(`<script>const active=1;</script></head>`), 1)
			}
			producerTestServe(&opts, map[string][]byte{"/counter/": document}, map[string]string{"/counter/": "html"})
			producerRequireNoWrites(t, opts, producerDistributionSnapshot(t, opts.DistDir))
		})
	}
}

func producerTestAssetRule(t *testing.T, use buildmanifest.PerfAssetUse) producerAssetRule {
	t.Helper()
	properties := inputDefinitions["AssetRule"].(map[string]any)["properties"].(map[string]any)
	fields := producerJSONFields(t, use)
	values := map[string]any{}
	for name := range properties {
		values[name] = fields[name]
	}
	producerSchemaObject(t, "AssetRule", values)
	raw, err := json.Marshal(values)
	var rule producerAssetRule
	if err != nil || json.Unmarshal(raw, &rule) != nil {
		t.Fatal("cannot decode source rule", err)
	}
	return rule
}

func TestProducerGeneratedSharedURLRoundTrip(t *testing.T) {
	properties := inputDefinitions["AssetRule"].(map[string]any)["properties"].(map[string]any)
	enum := func(field string) []any { return properties[field].(map[string]any)["enum"].([]any) }
	coverage := map[string]bool{}
	count := 2 * len(enum("owner")) * len(enum("phase")) * len(enum("condition"))
	for seed := 0; seed < count; seed++ {
		t.Run(fmt.Sprintf("seed-%03d", seed), func(t *testing.T) {
			opts, document := fixtureProducer(t)
			framework := opts.Build.PerfAssetUses.Assets[0]
			file, err := fixtureFilePath(framework.URL, framework.Kind)
			if err != nil {
				t.Fatal(err)
			}
			body, err := os.ReadFile(filepath.Join(opts.DistDir, file))
			if err != nil {
				t.Fatal(err)
			}
			image := []byte("alias dependency")
			producerTestPublic(t, opts, "alias.png", "image", image)
			combination := seed / (len(enum("phase")) * len(enum("condition")))
			owner := enum("owner")[combination%len(enum("owner"))].(string)
			registered := (combination/len(enum("owner")))%2 == 0
			conflict := []string{"none", "kind", "hash"}[seed%3]
			coverage[fmt.Sprintf("%s/%v/%s", owner, registered, conflict)] = true
			alias := framework
			alias.ID, alias.Owner = "app/fixture/alias.js", owner
			if owner == "framework" {
				alias.ID = "framework/runtime/zz-shared.js"
			}
			alias.Phase = enum("phase")[seed%len(enum("phase"))].(string)
			alias.Condition = enum("condition")[(seed/len(enum("phase")))%len(enum("condition"))].(string)
			alias.Dependencies = []string{"app/fixture/public/alias.png"}
			if conflict == "kind" {
				alias.Kind = "other"
			}
			if conflict == "hash" {
				alias.SHA256 = testMeasureHash([]byte("different body"))
			}
			want := alias
			producerTestCatalog(t, opts, func(catalog map[string]any) {
				if registered {
					fields := producerJSONFields(t, want)
					rule := map[string]any{}
					for name := range properties {
						rule[name] = fields[name]
					}
					catalog["assetRules"] = append(catalog["assetRules"].([]any), producerSchemaObject(t, "AssetRule", rule))
				}
			})
			if registered {
				alias.Phase, alias.Condition, alias.Dependencies = "dormant", "always", []string{}
			}
			opts.Build.PerfAssetUses.Assets = append(opts.Build.PerfAssetUses.Assets, alias)
			before := append([]buildmanifest.PerfAssetUse{}, opts.Build.PerfAssetUses.Assets...)
			producerTestServe(&opts, map[string][]byte{"/counter/": document, framework.URL: body, "/alias.png": image},
				map[string]string{"/counter/": "html", framework.URL: "js", "/alias.png": "image"})
			if conflict != "none" {
				producerRequireNoWrites(t, opts, producerDistributionSnapshot(t, opts.DistDir))
			} else {
				digest, err := ProduceFixture(context.Background(), opts)
				if err != nil {
					t.Fatal(err)
				}
				raw, err := os.ReadFile(filepath.Join(opts.DistDir, fixtureManifestFile))
				if err != nil {
					t.Fatal(err)
				}
				manifest, err := DecodeFixtureManifest(bytes.NewReader(raw))
				if err != nil {
					t.Fatal(err)
				}
				rule := producerTestAssetRule(t, want)
				found := false
				for _, use := range manifest.Assets {
					if use.ID == want.ID {
						found = true
						if err := producerCompareCatalogFields(rule, use); err != nil {
							t.Fatal("alias lost metadata", err)
						}
					}
				}
				if !found {
					t.Fatal("logical alias missing")
				}
				expectedUses := []buildmanifest.PerfAssetUse{framework, want,
					{ID: "app/fixture/html", URL: "/counter/", SHA256: testMeasureHash(document), Owner: "app", Kind: "html", Phase: "critical", Condition: "always", Dependencies: []string{}},
					{ID: "app/fixture/public/alias.png", URL: "/alias.png", SHA256: testMeasureHash(image), Owner: "app", Kind: "image", Phase: "dormant", Condition: "always", Dependencies: []string{}}}
				plan, err := ResolveReachability(ReachabilityOptions{Graph: &buildmanifest.PerfAssetUses{Version: 1, Assets: expectedUses},
					Bodies: map[string][]byte{framework.ID: body, want.ID: body, "app/fixture/html": document, "app/fixture/public/alias.png": image}, Route: manifest.Routes[0], Backend: "none"})
				if err != nil {
					t.Fatal(err)
				}
				phases := map[string]string{}
				for _, use := range plan.Assets {
					phases[use.ID] = use.Phase
				}
				byID := map[string]buildmanifest.PerfAssetUse{}
				for _, use := range expectedUses {
					byID[use.ID] = use
				}
				for _, chunks := range []bool{false, true} {
					report := producerTestCollect(t, opts, digest, chunks)
					if len(report.Assets) != 3 {
						t.Fatal("matching aliases did not share physical identity")
					}
					for _, asset := range report.Assets {
						source, ok := byID[asset.ID]
						if !ok {
							t.Fatal("unknown collected alias identity", asset.ID)
						}
						if source.URL == framework.URL && asset.Owner != "framework" {
							t.Fatal("shared body lost framework attribution")
						}
						source.Phase = phases[source.ID]
						if chunks {
							source.Phase = "startup"
						}
						if err := producerCompareCatalogFields(producerTestAssetRule(t, source), asset); err != nil {
							t.Fatal("collected alias changed source fields", err)
						}
						if source.SHA256 != asset.SHA256 {
							t.Fatal("collected alias changed body hash")
						}
					}
				}
			}
			if !reflect.DeepEqual(before, opts.Build.PerfAssetUses.Assets) {
				t.Fatal("alias publication mutated inventory")
			}
		})
	}
	for _, owner := range enum("owner") {
		for _, registered := range []bool{false, true} {
			for _, conflict := range []string{"none", "kind", "hash"} {
				if !coverage[fmt.Sprintf("%s/%v/%s", owner, registered, conflict)] {
					t.Errorf("missing alias combination %s/%v/%s", owner, registered, conflict)
				}
			}
		}
	}
	t.Logf("shared URL catalogs=%d; owner/registration/compatibility combinations=%d", count, len(coverage))
}
