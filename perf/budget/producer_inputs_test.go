package budget

import (
	"context"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"m31labs.dev/gosx/buildmanifest"
)

func TestProducerProtectsInteractionContract(t *testing.T) {
	for _, alias := range []string{"path", "hard-link", "external-hard-link"} {
		t.Run(alias, func(t *testing.T) {
			opts, _ := fixtureProducer(t)
			producerTestPublic(t, opts, "inputs/interaction.json", "other", []byte("replacement public body"))
			bound := []byte("hash-bound interaction sequence")
			inputRoot := opts.Inputs.RootDir()
			if alias != "external-hard-link" {
				raw, err := os.ReadFile(filepath.Join(inputRoot, "catalog.json"))
				if err != nil {
					t.Fatal(err)
				}
				inputRoot = opts.DistDir
				opts.Inputs.rootDir = inputRoot
				producerTestFile(t, inputRoot, "catalog.json", raw)
			}
			input := "contracts/interaction.json"
			if alias == "path" {
				input = "inputs/interaction.json"
			}
			producerTestFile(t, inputRoot, input, bound)
			producerTestCatalog(t, opts, func(catalog map[string]any) {
				catalog["interactionContract"] = Ref{File: input, SHA256: testMeasureHash(bound)}
			})
			if alias != "path" {
				if err := os.MkdirAll(filepath.Join(opts.DistDir, "inputs"), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Link(filepath.Join(inputRoot, input), filepath.Join(opts.DistDir, "inputs/interaction.json")); err != nil {
					t.Fatal(err)
				}
			}
			producerTestFile(t, opts.DistDir, fixtureManifestFile, []byte("previous manifest"))
			digest, err := ProduceFixture(context.Background(), opts)
			if digest != "" || err == nil {
				t.Errorf("interaction collision published: digest=%q error=%v", digest, err)
			}
			for _, file := range []string{filepath.Join(inputRoot, input), filepath.Join(opts.DistDir, "inputs/interaction.json")} {
				got, err := os.ReadFile(file)
				if err != nil || string(got) != string(bound) {
					t.Errorf("interaction input changed: %q %v", got, err)
				}
			}
			got, err := os.ReadFile(filepath.Join(opts.DistDir, fixtureManifestFile))
			if err != nil || string(got) != "previous manifest" {
				t.Error("collision changed manifest", err)
			}
		})
	}
}

func TestProducerInputPathSchemaGuard(t *testing.T) {
	// Use a copy so this probes schema evolution without mutating global input
	// validation. Even an absent optional path must acquire a protection role.
	raw, err := json.Marshal(inputDefinitions)
	if err != nil {
		t.Fatal(err)
	}
	var definitions map[string]any
	if err := json.Unmarshal(raw, &definitions); err != nil {
		t.Fatal(err)
	}
	properties := definitions["FixtureCatalog"].(map[string]any)["properties"].(map[string]any)
	properties["futureInput"] = map[string]any{"$ref": "#/$defs/Path"}
	for _, value := range []any{nil, map[string]any{"futureInput": "inputs/future.json"}} {
		if _, err := producerSchemaInputPaths(definitions, producerSchemaPathFields, "FixtureCatalog", value); err == nil {
			t.Fatal("new catalog path passed without a protection role")
		}
	}
	roles := maps.Clone(producerSchemaPathFields)
	roles["FixtureCatalog.futureInput"] = true
	files, err := producerSchemaInputPaths(definitions, roles, "FixtureCatalog", map[string]any{"futureInput": "inputs/future.json"})
	if err != nil || !reflect.DeepEqual(files, []string{"inputs/future.json"}) {
		t.Fatal("classified path was not protected", files, err)
	}

	type futureBuild struct {
		buildmanifest.Manifest
		FutureInput string
	}
	if err := producerCheckBuildPathFields(reflect.TypeFor[futureBuild](), producerBuildPathFields); err == nil {
		t.Fatal("new build path passed without a protection role")
	}
	buildRoles := maps.Clone(producerBuildPathFields)
	buildRoles["futureBuild.FutureInput"] = "dist"
	if err := producerCheckBuildPathFields(reflect.TypeFor[futureBuild](), buildRoles); err != nil {
		t.Fatal(err)
	}
}

func TestProducerInputSchemaStructFields(t *testing.T) {
	for definition, typ := range map[string]reflect.Type{
		"Budget": reflect.TypeFor[File](), "Profile": reflect.TypeFor[Profile](), "Coefficients": reflect.TypeFor[Coefficients](),
		"Toolchain": reflect.TypeFor[Toolchain](), "FixtureCatalog": reflect.TypeFor[producerCatalog](),
		"Ref": reflect.TypeFor[Ref](), "RouteFixture": reflect.TypeFor[FixtureRoute](), "AssetRule": reflect.TypeFor[producerAssetRule](),
	} {
		properties := inputDefinitions[definition].(map[string]any)["properties"].(map[string]any)
		fields := map[string]bool{}
		for i := 0; i < typ.NumField(); i++ {
			fields[strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]] = true
		}
		for field := range properties {
			if !fields[field] {
				t.Errorf("%s.%s has no decoded struct field", definition, field)
			}
		}
		for field := range fields {
			if properties[field] == nil {
				t.Errorf("%s.%s has no schema field", definition, field)
			}
		}
	}
}

func TestProducerDerivedInputPaths(t *testing.T) {
	opts, _ := fixtureProducer(t)
	opts.Build.SourceRoot = opts.Inputs.RootDir()
	opts.Build.Runtime.Bootstrap = buildmanifest.HashedAsset{File: "bootstrap.js"}
	opts.Build.Runtime.WASMVariants = map[string]buildmanifest.RuntimeVariantAsset{"variant": {HashedAsset: buildmanifest.HashedAsset{File: "variant.wasm"}}}
	opts.Build.Islands = []buildmanifest.IslandAsset{{SourceFile: "source/island.gsx", HashedAsset: buildmanifest.HashedAsset{File: "island.bin"}}}
	opts.Build.CSS = []buildmanifest.CSSAsset{{Source: "source/component.css", HashedAsset: buildmanifest.HashedAsset{File: "component.css"}}}
	opts.Build.Images = []buildmanifest.ImageAsset{{Source: "/source.png", Variants: []buildmanifest.ImageVariantAsset{{HashedAsset: buildmanifest.HashedAsset{File: "image.png"}}}}}
	opts.Build.SceneAssets = &buildmanifest.SceneAssetManifest{File: "reports/scene.json"}
	opts.Inputs.Toolchain.Fonts = []Ref{{File: "inputs/font.woff2"}}
	raw, err := os.ReadFile(filepath.Join(opts.Inputs.RootDir(), opts.Inputs.File.Fixtures.File))
	if err != nil {
		t.Fatal(err)
	}
	var catalog map[string]any
	if err := json.Unmarshal(raw, &catalog); err != nil {
		t.Fatal(err)
	}
	catalog["interactionContract"] = Ref{File: "inputs/interaction.json"}
	raw, err = json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	files, err := producerBoundInputPaths(opts, raw)
	if err != nil {
		t.Fatal(err)
	}
	inputFiles := []string{opts.Inputs.File.Profile.File, opts.Inputs.File.Coefficients.File, opts.Inputs.File.Toolchain.File,
		opts.Inputs.File.Fixtures.File, "inputs/font.woff2", "inputs/interaction.json", "source/island.gsx", "source/component.css",
		catalog["routes"].([]any)[0].(map[string]any)["sourcePath"].(string)}
	want := []string{}
	for _, name := range inputFiles {
		want = append(want, filepath.Join(opts.Inputs.RootDir(), name))
	}
	distFiles := []string{"assets/runtime/bootstrap.js", "assets/runtime/variant.wasm", "assets/islands/island.bin",
		"assets/css/component.css", "public/source.png", "assets/images/image.png", "reports/scene.json"}
	for _, use := range opts.Build.PerfAssetUses.Assets {
		name, err := fixtureFilePath(use.URL, use.Kind)
		if err != nil {
			t.Fatal(err)
		}
		distFiles = append(distFiles, name)
	}
	for _, name := range distFiles {
		want = append(want, filepath.Join(opts.DistDir, name))
	}
	slices.Sort(files)
	slices.Sort(want)
	if !reflect.DeepEqual(files, want) {
		t.Fatalf("derived files differ: got %v want %v", files, want)
	}
	// Every derived input, even one outside dist, must be protected by inode
	// identity before the first write. Exercise them all through hard links.
	for _, input := range files {
		producerTestFile(t, filepath.Dir(input), filepath.Base(input), []byte("bound input"))
	}
	root, err := os.OpenRoot(opts.DistDir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	for i, input := range files {
		t.Run(filepath.Base(input), func(t *testing.T) {
			target := "snapshot.bin"
			if err := os.Link(input, filepath.Join(opts.DistDir, target)); err != nil {
				t.Fatal(err)
			}
			defer os.Remove(filepath.Join(opts.DistDir, target))
			_, err := preflightProducerPaths(root, opts, nil, []producerPublicFile{{source: "public/unused", target: target, assetIndex: i}}, files)
			if err == nil {
				t.Fatal("derived input identity was not reserved", input)
			}
		})
	}
	t.Logf("derived project inputs=%v; distribution inputs=%v", inputFiles, distFiles)
}
