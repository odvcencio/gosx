package budget

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"m31labs.dev/gosx/buildmanifest"
)

func producerTestCatalog(t *testing.T, opts ProducerOptions, change func(map[string]any)) {
	t.Helper()
	file := filepath.Join(opts.Inputs.RootDir(), opts.Inputs.File.Fixtures.File)
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var catalog map[string]any
	if err := json.Unmarshal(raw, &catalog); err != nil {
		t.Fatal(err)
	}
	change(catalog)
	raw, err = json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateTyped("FixtureCatalog", catalog); err != nil {
		t.Fatal("generated catalog does not satisfy its schema", err)
	}
	if err := os.WriteFile(file, raw, 0600); err != nil {
		t.Fatal(err)
	}
	opts.Inputs.File.Fixtures.SHA256 = producerHash(raw)
}

// Serve the exact catalog bodies without coupling the generator to a fixture
// server's fixed routes. Sidecars remain release files, served as identity here.
func producerTestServe(opts *ProducerOptions, bodies map[string][]byte, kinds map[string]string) {
	opts.Client = &http.Client{Transport: testRoundTrip(func(req *http.Request) (*http.Response, error) {
		body, ok := bodies[req.URL.Path]
		status := http.StatusOK
		if !ok {
			status = http.StatusNotFound
		}
		mediaType := map[string]string{"html": "text/html", "js": "text/javascript", "css": "text/css", "wasm": "application/wasm",
			"program": "application/octet-stream", "font": "font/woff2", "image": "image/png", "model": "model/gltf-binary", "video": "video/mp4", "other": "application/octet-stream"}[kinds[req.URL.Path]]
		return &http.Response{StatusCode: status, ContentLength: int64(len(body)), Header: http.Header{"Content-Type": {mediaType}},
			Body: io.NopCloser(bytes.NewReader(body)), Request: req}, nil
	})}
}

func producerTestCollect(t *testing.T, opts ProducerOptions, digest string, chunks bool) *Report {
	t.Helper()
	report, err := testCollect(context.Background(), CollectOptions{Inputs: opts.Inputs, SHA: opts.SourceSHA, Client: opts.Client, ChunksOnly: chunks,
		Bindings: []CollectionBinding{{App: opts.App, DistDir: opts.DistDir, BaseURL: opts.BaseURL, ArtifactSHA256: digest}}})
	if err != nil {
		t.Fatalf("producer output rejected by collector (chunks=%v): %v", chunks, err)
	}
	return report
}

func TestProducerPublicDependencyRoundTrip(t *testing.T) {
	opts, document := fixtureProducer(t)
	css := []byte(`body{background:url("../images/bg.png")}`)
	image := []byte("registered image")
	producerTestPublic(t, opts, "styles/site.css", "css", css)
	producerTestPublic(t, opts, "images/bg.png", "image", image)
	producerTestCatalog(t, opts, func(catalog map[string]any) {
		catalog["assetRules"].([]any)[1].(map[string]any)["dependencies"] = []string{"app/fixture/public/images/bg.png"}
	})
	document = bytes.Replace(document, []byte("</head>"), []byte(`<link rel="stylesheet" href="/styles/site.css"></head>`), 1)
	producerTestServe(&opts, map[string][]byte{"/counter/": document, "/styles/site.css": css, "/images/bg.png": image},
		map[string]string{"/counter/": "html", "/styles/site.css": "css", "/images/bg.png": "image"})
	digest, err := ProduceFixture(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	for _, chunks := range []bool{false, true} {
		report := producerTestCollect(t, opts, digest, chunks)
		if len(report.Assets) != 4 {
			t.Fatal("collector lost registered bodies")
		}
		for _, asset := range report.Assets {
			if asset.ID == "app/fixture/public/styles/site.css" && !reflect.DeepEqual(asset.Dependencies, []string{"app/fixture/public/images/bg.png"}) {
				t.Fatal("collector lost the registered CSS-to-image edge")
			}
		}
	}
}

func TestProducerRejectsInvalidGraphBeforeWriting(t *testing.T) {
	for _, name := range []string{"missing", "foreign-app", "missing-public-file", "cycle", "duplicate-edge", "document-edge", "critical-root", "opposite-backend"} {
		t.Run(name, func(t *testing.T) {
			opts, _ := fixtureProducer(t)
			producerTestPublic(t, opts, "site.css", "css", []byte("body{color:green}"))
			producerTestPublic(t, opts, "image.png", "image", []byte("image"))
			producerTestCatalog(t, opts, func(catalog map[string]any) {
				rules := catalog["assetRules"].([]any)
				doc, css, image := rules[0].(map[string]any), rules[1].(map[string]any), rules[2].(map[string]any)
				dep := image["id"].(string)
				switch name {
				case "missing":
					dep = "app/fixture/absent"
				case "foreign-app":
					image["id"], dep = "app/other/public/image.png", "app/other/public/image.png"
				case "missing-public-file":
					if err := os.Remove(filepath.Join(opts.DistDir, "public/image.png")); err != nil {
						t.Fatal(err)
					}
				case "cycle":
					image["dependencies"] = []string{css["id"].(string)}
				case "duplicate-edge":
					css["dependencies"] = []string{dep, dep}
					return
				case "document-edge":
					doc["dependencies"] = []string{"app/other/html"}
				case "critical-root":
					catalog["routes"].([]any)[0].(map[string]any)["criticalAssetIDs"] = []string{doc["id"].(string), "app/other/missing"}
				case "opposite-backend":
					css["condition"], image["condition"] = "webgpu", "webgl"
				}
				css["dependencies"] = []string{dep}
			})
			outputs := []string{"site.css", "site.css.gz", "image.png", "counter/index.html", fixtureManifestFile}
			for _, file := range outputs {
				producerTestFile(t, opts.DistDir, file, []byte("previous "+file))
			}
			digest, err := ProduceFixture(context.Background(), opts)
			var typed *InputError
			if digest != "" || !errors.As(err, &typed) || typed.Code != "wrong-fixture" || typed.Reference != "producer" {
				t.Errorf("invalid graph was published: digest=%q error=%v", digest, err)
			}
			for _, file := range outputs {
				got, err := os.ReadFile(filepath.Join(opts.DistDir, file))
				if err != nil || string(got) != "previous "+file {
					t.Errorf("invalid graph changed %s: %v", file, err)
				}
			}
		})
	}
}

// Iterate the schema properties instead of projecting through producer structs.
// A new field (including an optional one) must acquire a generated value and an
// assertion before this test can pass; JSON decoding cannot silently drop it.
func producerSchemaObject(t *testing.T, definition string, values map[string]any) map[string]any {
	t.Helper()
	properties := inputDefinitions[definition].(map[string]any)["properties"].(map[string]any)
	for field := range properties {
		if _, ok := values[field]; !ok {
			t.Fatalf("%s.%s needs a generator and round-trip assertion", definition, field)
		}
	}
	if len(properties) != len(values) {
		t.Fatalf("generator fields no longer match %s", definition)
	}
	if err := validateTyped(definition, values); err != nil {
		t.Fatalf("invalid generated %s: %v", definition, err)
	}
	return values
}

func producerJSONFields(t *testing.T, value any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	return fields
}

func TestProducerGeneratedCatalogRoundTrip(t *testing.T) {
	properties := inputDefinitions["AssetRule"].(map[string]any)["properties"].(map[string]any)
	enum := func(field string) []any { return properties[field].(map[string]any)["enum"].([]any) }
	// Cover every schema kind, phase and condition, with seeded acyclic edges,
	// shuffled catalog/build order and independently hashed source bodies.
	for seed := 0; seed < len(enum("phase"))*len(enum("condition")); seed++ {
		t.Run(fmt.Sprintf("seed-%02d", seed), func(t *testing.T) {
			rng := rand.New(rand.NewSource(int64(seed)))
			opts, baseDocument := fixtureProducer(t)
			document := bytes.Replace(baseDocument, []byte("</head>"), []byte(`<link rel="stylesheet" href="/generated/css.body"></head>`), 1)
			bodies := map[string][]byte{"/counter/": document}
			kinds := map[string]string{}
			sidecars := map[string]map[string][]byte{}
			expected := map[string]map[string]any{}
			rules := []any{}
			add := func(use buildmanifest.PerfAssetUse, body []byte, source string) {
				use.SHA256 = testMeasureHash(body)
				fields := producerSchemaObject(t, "AssetUse", producerJSONFields(t, use))
				rule := map[string]any{"id": fields["id"], "owner": fields["owner"], "kind": fields["kind"],
					"phase": fields["phase"], "condition": fields["condition"], "dependencies": fields["dependencies"]}
				rules = append(rules, producerSchemaObject(t, "AssetRule", rule))
				// Expectations come from the complete catalog rule, so adding a
				// generator for a new field cannot bypass its output assertion.
				expected[use.ID] = producerJSONFields(t, rule)
				expected[use.ID]["sha256"], expected[use.ID]["url"] = fields["sha256"], fields["url"]
				bodies[use.URL] = body
				kinds[use.URL] = use.Kind
				producerTestFile(t, opts.DistDir, source, body)
				sidecars[use.ID] = producerTestEncodings(t, opts.DistDir, source, body)
			}
			runtime := opts.Build.PerfAssetUses.Assets[0]
			runtimeBody, err := os.ReadFile(filepath.Join(opts.DistDir, "assets/runtime", filepath.Base(runtime.URL)))
			if err != nil {
				t.Fatal(err)
			}
			add(runtime, runtimeBody, "assets/runtime/"+filepath.Base(runtime.URL))
			build := []buildmanifest.PerfAssetUse{runtime}
			previous := runtime.ID
			for i, count := 0, 1+rng.Intn(4); i < count; i++ {
				chunk := buildmanifest.PerfAssetUse{ID: fmt.Sprintf("app/fixture/chunk-%d.js", i), URL: fmt.Sprintf("/generated/chunk-%d.js", i), Owner: "app", Kind: "js", Phase: "dormant", Condition: "always", Dependencies: []string{previous}}
				body := []byte(fmt.Sprintf("const chunk%d=%d;", i, seed))
				chunk.SHA256 = testMeasureHash(body)
				add(chunk, body, "generated/"+filepath.Base(chunk.URL))
				build = append(build, chunk)
				previous = chunk.ID
			}
			for i, choice := range enum("kind") {
				kind := choice.(string)
				body := []byte(fmt.Sprintf("generated %s body %d", kind, seed))
				switch kind {
				case "html":
					body = baseDocument
				case "css":
					body = []byte(`body{background:url("image.body")}`)
				case "js":
					body = []byte(fmt.Sprintf("const generated=%d;", seed))
				case "wasm", "program", "font", "image", "model", "video", "other":
				default:
					t.Fatalf("schema kind %s needs a body generator", kind)
				}
				phase := enum("phase")[(seed+i)%len(enum("phase"))].(string)
				condition := enum("condition")[(seed/len(enum("phase"))+i)%len(enum("condition"))].(string)
				deps := []string{}
				if rng.Intn(2) == 0 {
					deps = append(deps, previous)
				}
				if kind == "css" || kind == "image" {
					condition = "always"
				}
				if kind == "css" {
					deps = append(deps, "app/fixture/public/generated/image.body")
				}
				use := buildmanifest.PerfAssetUse{ID: "app/fixture/public/generated/" + kind + ".body", URL: "/generated/" + kind + ".body", Owner: "app", Kind: kind, Phase: phase, Condition: condition, Dependencies: deps}
				add(use, body, "public/generated/"+kind+".body")
			}
			doc := buildmanifest.PerfAssetUse{ID: "app/fixture/html", URL: "/counter/", Owner: "app", Kind: "html", Phase: "critical", Condition: "always", Dependencies: []string{previous, "app/fixture/public/generated/css.body"}}
			add(doc, document, "static/counter/index.html")
			rng.Shuffle(len(build), func(i, j int) { build[i], build[j] = build[j], build[i] })
			opts.Build.PerfAssetUses.Assets = build
			rng.Shuffle(len(rules), func(i, j int) { rules[i], rules[j] = rules[j], rules[i] })
			var expectedRoutes any
			producerTestCatalog(t, opts, func(catalog map[string]any) {
				for _, route := range catalog["routes"].([]any) {
					producerSchemaObject(t, "RouteFixture", route.(map[string]any))
				}
				catalog["assetRules"] = rules
				expectedRoutes = catalog["routes"]
				producerSchemaObject(t, "FixtureCatalog", catalog)
			})
			producerTestServe(&opts, bodies, kinds)
			digest, err := ProduceFixture(context.Background(), opts)
			if err != nil {
				t.Fatal(err)
			}
			root, err := os.OpenRoot(opts.DistDir)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			raw, err := readMeasureFile(root, fixtureManifestFile, maxInputBytes)
			if err != nil {
				t.Fatal(err)
			}
			manifest, err := DecodeFixtureManifest(bytes.NewReader(raw))
			if err != nil || len(manifest.Assets) != len(expected) {
				t.Fatal("generated graph was not emitted intact", err)
			}
			if !reflect.DeepEqual(producerJSONFields(t, manifest)["routes"], expectedRoutes) {
				t.Fatal("generated routes changed during publication")
			}
			for _, asset := range manifest.Assets {
				if got := producerJSONFields(t, asset); !reflect.DeepEqual(got, expected[asset.ID]) {
					t.Errorf("emitted asset %s differs from catalog: got %v want %v", asset.ID, got, expected[asset.ID])
				}
				body, encodings, err := readFixtureBody(root, asset.URL, asset.Kind)
				if err != nil || !bytes.Equal(body, bodies[asset.URL]) || !reflect.DeepEqual(encodings, sidecars[asset.ID]) {
					t.Errorf("generated body/sidecars %s changed: %v", asset.ID, err)
				}
			}
			for _, chunks := range []bool{false, true} {
				report := producerTestCollect(t, opts, digest, chunks)
				if len(report.Assets) != len(expected) {
					t.Fatal("collector lost generated assets")
				}
				for _, asset := range report.Assets {
					if expected[asset.ID] == nil {
						t.Fatalf("collector returned an unregistered ID %s", asset.ID)
					}
					got := producerJSONFields(t, asset)
					for field, want := range expected[asset.ID] {
						// Collection computes observed phase and keeps native URLs private.
						if field != "url" && field != "phase" && !reflect.DeepEqual(got[field], want) {
							t.Errorf("collected %s.%s differs: got %v want %v", asset.ID, field, got[field], want)
						}
					}
				}
			}
		})
	}
}
