package budget

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"m31labs.dev/gosx/buildmanifest"
)

func TestProducerImportedIslandSource(t *testing.T) {
	for _, layout := range []string{"local", "imported-relative", "imported-absolute", "input-root-fallback"} {
		t.Run(layout, func(t *testing.T) {
			opts, document := fixtureProducer(t)
			root := filepath.Join(t.TempDir(), "app")
			opts.Build.SourceRoot = root
			name := "source/island.gsx"
			file := filepath.Join(root, name)
			switch layout {
			case "imported-relative", "imported-absolute":
				file = filepath.Join(filepath.Dir(root), "leaf/island.gsx")
				name = "../leaf/island.gsx"
				if layout == "imported-absolute" {
					name = file
				}
			case "input-root-fallback":
				opts.Build.SourceRoot = ""
				file = filepath.Join(opts.Inputs.RootDir(), name)
			}
			if err := os.MkdirAll(root, 0700); err != nil {
				t.Fatal(err)
			}
			source := []byte("package leaf\n//gosx:island\ncomponent Island() { <p>Imported island</p> }")
			producerTestFile(t, filepath.Dir(file), filepath.Base(file), source)
			body := []byte("compiled island")
			assetFile := "island." + testMeasureHash(body)[:16] + ".gxi"
			producerTestFile(t, opts.DistDir, "assets/islands/"+assetFile, body)
			opts.Build.Islands = []buildmanifest.IslandAsset{{Name: "Island", Format: "bin", SourceFile: name, SourceHash: testMeasureHash(source),
				HashedAsset: buildmanifest.HashedAsset{File: assetFile, Hash: testMeasureHash(body), Size: int64(len(body))}}}
			use := buildmanifest.PerfAssetUse{ID: "app/fixture/island.gxi", URL: "/gosx/assets/islands/" + assetFile,
				SHA256: testMeasureHash(body), Owner: "app", Kind: "program", Phase: "dormant", Condition: "always", Dependencies: []string{}}
			opts.Build.PerfAssetUses.Assets = append(opts.Build.PerfAssetUses.Assets, use)
			producerTestCatalog(t, opts, func(catalog map[string]any) {
				catalog["assetRules"] = append(catalog["assetRules"].([]any), producerJSONFields(t, producerTestAssetRule(t, use)))
			})
			producerTestServe(&opts, map[string][]byte{"/counter/": document, use.URL: body}, map[string]string{"/counter/": "html", use.URL: "program"})
			before, err := json.Marshal(opts.Build)
			if err != nil {
				t.Fatal(err)
			}
			digest, err := ProduceFixture(context.Background(), opts)
			if err != nil || digest == "" {
				t.Fatalf("supported source %q rejected: digest=%q error=%v", name, digest, err)
			}
			for _, chunks := range []bool{false, true} {
				report := producerTestCollect(t, opts, digest, chunks)
				found := false
				for _, asset := range report.Assets {
					if asset.ID == use.ID {
						found = true
						if asset.SHA256 != use.SHA256 || asset.Kind != use.Kind || asset.Owner != use.Owner {
							t.Fatal("collected imported island differs from build")
						}
					}
				}
				if !found {
					t.Fatal("collector lost imported island")
				}
			}
			after, err := json.Marshal(opts.Build)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("producer changed caller-owned source metadata", err)
			}
			saved, err := os.ReadFile(file)
			if err != nil || !bytes.Equal(source, saved) {
				t.Fatal("producer changed imported source", err)
			}
		})
	}
}

func TestProducerProtectsImportedSource(t *testing.T) {
	for _, alias := range []string{"path", "external-hard-link"} {
		t.Run(alias, func(t *testing.T) {
			opts, _ := fixtureProducer(t)
			opts.Build.SourceRoot = filepath.Join(t.TempDir(), "app")
			if alias == "path" {
				opts.Build.SourceRoot = filepath.Join(opts.DistDir, "app")
			}
			source := filepath.Join(filepath.Dir(opts.Build.SourceRoot), "leaf/island.gsx")
			body := []byte("protected imported source")
			producerTestFile(t, filepath.Dir(source), filepath.Base(source), body)
			opts.Build.Islands = []buildmanifest.IslandAsset{{SourceFile: "../leaf/island.gsx"}}
			producerTestPublic(t, opts, "leaf/island.gsx", "other", []byte("replacement public snapshot"))
			if alias == "external-hard-link" {
				target := filepath.Join(opts.DistDir, "leaf/island.gsx")
				if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Link(source, target); err != nil {
					t.Fatal(err)
				}
			}
			producerRequireNoWrites(t, opts, producerDistributionSnapshot(t, opts.DistDir))
			saved, err := os.ReadFile(source)
			if err != nil || !bytes.Equal(body, saved) {
				t.Fatal("collision changed imported source", err)
			}
		})
	}
}
