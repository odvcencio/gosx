package budget

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"m31labs.dev/gosx/buildmanifest"
)

func TestProducerProtectsLoadedBudget(t *testing.T) {
	for _, alias := range []string{"path", "hard-link", "external-hard-link", "symlink-argument"} {
		t.Run(alias, func(t *testing.T) {
			opts, _ := fixtureProducer(t)
			producerTestPublic(t, opts, "budget.json", "other", []byte("replacement public body"))
			raw, err := os.ReadFile(filepath.Join(opts.Inputs.RootDir(), opts.Inputs.File.Fixtures.File))
			if err != nil {
				t.Fatal(err)
			}
			var registered map[string]any
			if err := json.Unmarshal(raw, &registered); err != nil {
				t.Fatal(err)
			}
			path := configFixture(t, func(_ *File, _ *Profile, _ *Coefficients, _ *Toolchain, catalog map[string]any) {
				catalog["routes"], catalog["assetRules"] = registered["routes"], registered["assetRules"]
				catalog["routes"].([]any)[0].(map[string]any)["sourcePath"] = "source.gsx"
			})
			root := filepath.Dir(path)
			if alias != "external-hard-link" {
				entries, err := os.ReadDir(root)
				if err != nil {
					t.Fatal(err)
				}
				for _, entry := range entries {
					body, err := os.ReadFile(filepath.Join(root, entry.Name()))
					if err != nil {
						t.Fatal(err)
					}
					name := entry.Name()
					if name == "budget.json" && alias != "path" {
						name = "loaded-budget.json"
					}
					producerTestFile(t, opts.DistDir, name, body)
				}
				root = opts.DistDir
				path = filepath.Join(root, "budget.json")
				if alias != "path" {
					path = filepath.Join(root, "loaded-budget.json")
				}
			}
			original, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			loadPath := path
			if alias == "symlink-argument" {
				loadPath = filepath.Join(root, "budget-alias.json")
				if err := os.Symlink(path, loadPath); err != nil {
					t.Fatal(err)
				}
			}
			opts.Inputs, err = LoadInputs(loadPath, LoadOptions{RootDir: root})
			if err != nil {
				t.Fatal(err)
			}
			if alias != "path" {
				if err := os.Link(path, filepath.Join(opts.DistDir, "budget.json")); err != nil {
					t.Fatal(err)
				}
			}
			producerTestFile(t, opts.DistDir, fixtureManifestFile, []byte("previous manifest"))
			digest, err := ProduceFixture(context.Background(), opts)
			if err == nil || digest != "" {
				t.Errorf("loaded budget overwritten: digest=%q error=%v", digest, err)
			}
			for _, file := range []string{path, filepath.Join(opts.DistDir, "budget.json")} {
				got, err := os.ReadFile(file)
				if err != nil || !bytes.Equal(got, original) {
					t.Errorf("budget bytes changed: %v", err)
				}
			}
			if opts.Inputs.BudgetSHA256 != testMeasureHash(original) {
				t.Fatal("loaded hash differs from original budget")
			}
			manifest, err := os.ReadFile(filepath.Join(opts.DistDir, fixtureManifestFile))
			if err != nil || string(manifest) != "previous manifest" {
				t.Error("collision changed the previous manifest", err)
			}
		})
	}
}

func TestProducerRegisteredStartupPhase(t *testing.T) {
	opts, document := fixtureProducer(t)
	body := []byte("const registeredStartup=1;")
	use := buildmanifest.PerfAssetUse{ID: "app/fixture/startup.js", URL: "/startup.js", SHA256: testMeasureHash(body),
		Owner: "app", Kind: "js", Phase: "dormant", Condition: "always", Dependencies: []string{}}
	opts.Build.PerfAssetUses.Assets = append(opts.Build.PerfAssetUses.Assets, use)
	before := append([]buildmanifest.PerfAssetUse{}, opts.Build.PerfAssetUses.Assets...)
	producerTestFile(t, opts.DistDir, "startup.js", body)
	producerTestCatalog(t, opts, func(catalog map[string]any) {
		catalog["assetRules"] = append(catalog["assetRules"].([]any), map[string]any{
			"id": use.ID, "owner": use.Owner, "kind": use.Kind, "phase": "startup", "condition": use.Condition, "dependencies": []string{},
		})
	})
	bodiesByURL := map[string][]byte{"/counter/": document}
	kinds := map[string]string{"/counter/": "html"}
	for _, asset := range opts.Build.PerfAssetUses.Assets {
		file, err := fixtureFilePath(asset.URL, asset.Kind)
		if err != nil {
			t.Fatal(err)
		}
		body, err := os.ReadFile(filepath.Join(opts.DistDir, file))
		if err != nil {
			t.Fatal(err)
		}
		bodiesByURL[asset.URL], kinds[asset.URL] = body, asset.Kind
	}
	producerTestServe(&opts, bodiesByURL, kinds)
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
	bodies := map[string][]byte{}
	root, err := os.OpenRoot(opts.DistDir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	for _, asset := range manifest.Assets {
		body, _, err := readFixtureBody(root, asset.URL, asset.Kind)
		if err != nil {
			t.Fatal(err)
		}
		bodies[asset.ID] = body
		if asset.ID == use.ID && asset.Phase != "startup" {
			t.Errorf("registered startup emitted as %s", asset.Phase)
		}
	}
	plan, err := ResolveReachability(ReachabilityOptions{Graph: &buildmanifest.PerfAssetUses{Version: 1, Assets: manifest.Assets},
		Bodies: bodies, Route: manifest.Routes[0], Backend: "none"})
	if err != nil || plan.Reachability != "known" {
		t.Fatal("unambiguous graph was not known", plan, err)
	}
	for _, asset := range plan.Assets {
		if asset.ID == use.ID && asset.Phase != "startup" {
			t.Errorf("registered startup planned as %s", asset.Phase)
		}
	}
	for _, chunks := range []bool{false, true} {
		report := producerTestCollect(t, opts, digest, chunks)
		for _, asset := range report.Assets {
			if asset.ID == use.ID && asset.Phase != "startup" {
				t.Errorf("registered startup collected as %s", asset.Phase)
			}
		}
	}
	if !reflect.DeepEqual(before, opts.Build.PerfAssetUses.Assets) {
		t.Fatal("producer changed caller inventory")
	}
}

func producerCheckLoaderArguments(loader, arguments reflect.Type) error {
	if loader.NumIn() != arguments.NumField() {
		return fmt.Errorf("loader arguments must all be bound")
	}
	for i := 0; i < loader.NumIn(); i++ {
		if loader.In(i) != arguments.Field(i).Type {
			return fmt.Errorf("loader argument %d is not bound", i)
		}
	}
	return nil
}

func TestProducerLoaderArgumentProtection(t *testing.T) {
	if err := producerCheckLoaderArguments(reflect.TypeOf(LoadInputs), reflect.TypeFor[loadInputArguments]()); err != nil {
		t.Fatal(err)
	}
	// A new path parameter must change the binding before this guard passes.
	futureLoader := reflect.TypeOf(func(string, LoadOptions, string) {})
	if err := producerCheckLoaderArguments(futureLoader, reflect.TypeFor[loadInputArguments]()); err == nil {
		t.Fatal("new loader path argument escaped the binding")
	}
	type futureOptions struct {
		LoadOptions
		FutureInput string
	}
	type futureArguments struct {
		Path    string
		Options futureOptions
		Extra   string
	}
	if err := producerCheckLoaderArguments(reflect.TypeOf(func(string, futureOptions, string) {}), reflect.TypeFor[futureArguments]()); err != nil {
		t.Fatal(err)
	}
	opts, _ := fixtureProducer(t)
	inputRoot := opts.Inputs.RootDir()
	names := []string{"budget-input.json", "profile-input.json", "extra-input.json"}
	for _, name := range names {
		producerTestFile(t, inputRoot, name, []byte("protected input"))
	}
	arguments := futureArguments{filepath.Join(inputRoot, names[0]),
		futureOptions{LoadOptions{RootDir: inputRoot}, filepath.Join(inputRoot, names[1])}, filepath.Join(inputRoot, names[2])}
	files, err := loadArgumentPaths(arguments)
	want := []string{}
	for _, name := range names {
		want = append(want, filepath.Join(inputRoot, name))
	}
	if err != nil || !reflect.DeepEqual(files, want) {
		t.Fatal("new file option or argument was not captured", files, err)
	}
	opts.Inputs.inputFiles = files
	raw, err := os.ReadFile(filepath.Join(inputRoot, opts.Inputs.File.Fixtures.File))
	if err != nil {
		t.Fatal(err)
	}
	bound, err := producerBoundInputPaths(opts, raw)
	if err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(opts.DistDir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			target := "argument-snapshot.bin"
			if err := os.Link(file, filepath.Join(opts.DistDir, target)); err != nil {
				t.Fatal(err)
			}
			defer os.Remove(filepath.Join(opts.DistDir, target))
			public := []producerPublicFile{{source: "public/unused", target: target}}
			if _, err := preflightProducerPaths(root, opts, nil, public, bound); err == nil {
				t.Fatal("new input path or file identity was not reserved")
			}
			// The control demonstrates that the collision is rejected because of
			// this argument binding, rather than another path reservation.
			if _, err := preflightProducerPaths(root, opts, nil, public, bound[len(files):]); err != nil {
				t.Fatal("unbound argument control unexpectedly reserved", err)
			}
		})
	}
}
