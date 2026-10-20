package budget

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andybalholm/brotli"
)

func producerTestFile(t *testing.T, dir, name string, body []byte) {
	t.Helper()
	file := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, body, 0600); err != nil {
		t.Fatal(err)
	}
}

func producerTestPublic(t *testing.T, opts ProducerOptions, name, kind string, body []byte) {
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
	catalog["assetRules"] = append(catalog["assetRules"].([]any), map[string]any{
		"id": "app/fixture/public/" + name, "owner": "app", "kind": kind,
		"phase": "dormant", "condition": "always", "dependencies": []string{},
	})
	raw, err = json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, raw, 0600); err != nil {
		t.Fatal(err)
	}
	opts.Inputs.File.Fixtures.SHA256 = producerHash(raw)
	producerTestFile(t, opts.DistDir, "public/"+name, body)
}

func producerTestEncodings(t *testing.T, dir, name string, body []byte) map[string][]byte {
	t.Helper()
	var gz, br bytes.Buffer
	gzipWriter := gzip.NewWriter(&gz)
	brotliWriter := brotli.NewWriter(&br)
	if _, err := gzipWriter.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := brotliWriter.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := brotliWriter.Close(); err != nil {
		t.Fatal(err)
	}
	producerTestFile(t, dir, name+".gz", gz.Bytes())
	producerTestFile(t, dir, name+".br", br.Bytes())
	return map[string][]byte{"gzip": gz.Bytes(), "br": br.Bytes()}
}

func TestProducerPreflightsAllDestinationsBeforeWriting(t *testing.T) {
	for _, name := range []string{
		"runtime", "runtime-gzip", "runtime-brotli", "runtime-temporary", "managed-runtime",
		"build.json", "build.json.gz", "gosx-grammar.blob", "run.sh", "README.md", "scene-assets.json",
		"server/app", "app/page.gsx", "content/article.md", "static/counter/index.html",
		"public/input.txt", "counter/index.html", "perf-fixtures.v1.json",
	} {
		t.Run(name, func(t *testing.T) {
			opts, _ := fixtureProducer(t)
			use := &opts.Build.PerfAssetUses.Assets[0]
			runtimePath := "assets/" + strings.SplitN(use.URL, "/gosx/assets/", 2)[1]
			runtimeBody, err := os.ReadFile(filepath.Join(opts.DistDir, runtimePath))
			if err != nil {
				t.Fatal(err)
			}
			producerTestEncodings(t, opts.DistDir, runtimePath, runtimeBody)
			target, publicName := name, name
			switch name {
			case "runtime", "runtime-gzip", "runtime-brotli", "runtime-temporary":
				// Exercise a verified path outside the standard runtime directory too.
				if name == "runtime" {
					runtimePath = "shared/runtime.js"
					use.URL = "/" + runtimePath
					producerTestFile(t, opts.DistDir, runtimePath, runtimeBody)
				}
				target = runtimePath
				if name == "runtime-gzip" {
					target += ".gz"
				} else if name == "runtime-brotli" {
					target += ".br"
				} else if name == "runtime-temporary" {
					target += ".new"
				}
				publicName = target
			case "managed-runtime":
				target = runtimePath
				publicName = strings.TrimPrefix(use.URL, "/")
			}
			if _, err := os.Stat(filepath.Join(opts.DistDir, target)); os.IsNotExist(err) {
				producerTestFile(t, opts.DistDir, target, []byte("production input"))
			}
			original, err := os.ReadFile(filepath.Join(opts.DistDir, target))
			if err != nil {
				t.Fatal(err)
			}
			producerTestPublic(t, opts, "first.css", "css", []byte("body{color:green}"))
			producerTestFile(t, opts.DistDir, "first.css", []byte("previous snapshot"))
			producerTestFile(t, opts.DistDir, "first.css.gz", []byte("previous encoding"))
			producerTestPublic(t, opts, publicName, "other", []byte("replacement"))
			digest, err := ProduceFixture(context.Background(), opts)
			var typed *InputError
			if digest != "" || !errors.As(err, &typed) || typed.Code != "wrong-fixture" || typed.Reference != "producer" {
				t.Errorf("collision did not prevent publication: %q, %v", digest, err)
			}
			for file, expected := range map[string][]byte{
				target: original, runtimePath: runtimeBody,
				"first.css": []byte("previous snapshot"), "first.css.gz": []byte("previous encoding"),
			} {
				saved, readErr := os.ReadFile(filepath.Join(opts.DistDir, file))
				if readErr != nil || !bytes.Equal(saved, expected) {
					t.Errorf("collision changed existing file %s: %v", file, readErr)
				}
			}
			if name != "perf-fixtures.v1.json" {
				if _, err := os.Stat(filepath.Join(opts.DistDir, "perf-fixtures.v1.json")); !os.IsNotExist(err) {
					t.Fatal("collision published a contract")
				}
			}
		})
	}
}

func TestProducerPublicHTMLRoundTrip(t *testing.T) {
	for _, name := range []string{"404.html", "errors/expired.html"} {
		t.Run(name, func(t *testing.T) {
			opts, body := fixtureProducer(t)
			producerTestPublic(t, opts, name, "html", body)
			encodings := producerTestEncodings(t, opts.DistDir, "public/"+name, body)
			digest, err := ProduceFixture(context.Background(), opts)
			if err != nil {
				t.Fatal(err)
			}
			root, err := os.OpenRoot(opts.DistDir)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			saved, representations, err := readFixtureBody(root, "/"+name, "html")
			if err != nil || !bytes.Equal(saved, body) {
				t.Fatalf("public HTML cannot be collected: %v", err)
			}
			for encoding, expected := range encodings {
				if !bytes.Equal(representations[encoding], expected) {
					t.Errorf("public HTML %s sidecar differs", encoding)
				}
			}
			report, err := testCollect(context.Background(), CollectOptions{Inputs: opts.Inputs, SHA: opts.SourceSHA, Client: opts.Client,
				Bindings: []CollectionBinding{{App: opts.App, DistDir: opts.DistDir, BaseURL: opts.BaseURL, ArtifactSHA256: digest}}})
			if err != nil || len(report.Assets) != 3 {
				t.Fatal("public HTML producer-to-collector round trip failed", err)
			}
			contract, err := readMeasureFile(root, "perf-fixtures.v1.json", maxInputBytes)
			if err != nil {
				t.Fatal(err)
			}
			manifest, err := DecodeFixtureManifest(bytes.NewReader(contract))
			if err != nil {
				t.Fatal(err)
			}
			for _, use := range manifest.Assets {
				if use.ID == "app/fixture/public/"+name && use.URL != "/"+name {
					t.Fatal("snapshot changed the serving URL")
				}
			}
		})
	}
}

func TestProducerRejectsOverlappingSnapshotPaths(t *testing.T) {
	for _, names := range [][]string{
		{"assets/custom/site.css", "gosx/assets/custom/site.css"},
		{"site.css", "site.css.gz"},
		{"site.css", "site.css.new"},
		{"Site.css", "site.css"},
		{"assets/custom", "gosx/assets/custom/model.bin"},
	} {
		t.Run(strings.Join(names, "+"), func(t *testing.T) {
			opts, _ := fixtureProducer(t)
			body := []byte("body{color:green}")
			producerTestPublic(t, opts, names[0], "other", body)
			if names[1] == "site.css.gz" {
				body = producerTestEncodings(t, opts.DistDir, "public/site.css", body)["gzip"]
			}
			producerTestPublic(t, opts, names[1], "other", body)
			digest, err := ProduceFixture(context.Background(), opts)
			var typed *InputError
			if digest != "" || !errors.As(err, &typed) || typed.Code != "wrong-fixture" {
				t.Fatal("overlapping snapshots reached the writer", err)
			}
			if _, err := os.Stat(filepath.Join(opts.DistDir, "perf-fixtures.v1.json")); !os.IsNotExist(err) {
				t.Fatal("overlapping snapshots published a contract")
			}
		})
	}
}

func TestProducerProtectsBoundInputsInDistribution(t *testing.T) {
	opts, _ := fixtureProducer(t)
	file := "inputs/catalog.json"
	raw, err := os.ReadFile(filepath.Join(opts.Inputs.RootDir(), opts.Inputs.File.Fixtures.File))
	if err != nil {
		t.Fatal(err)
	}
	producerTestFile(t, opts.DistDir, file, raw)
	opts.Inputs.rootDir = opts.DistDir
	opts.Inputs.File.Fixtures.File = file
	producerTestPublic(t, opts, file, "other", []byte("replacement"))
	original, err := os.ReadFile(filepath.Join(opts.DistDir, file))
	if err != nil {
		t.Fatal(err)
	}
	digest, err := ProduceFixture(context.Background(), opts)
	var typed *InputError
	if digest != "" || !errors.As(err, &typed) || typed.Code != "wrong-fixture" {
		t.Fatal("snapshot overwrote a bound production input", err)
	}
	saved, err := os.ReadFile(filepath.Join(opts.DistDir, file))
	if err != nil || !bytes.Equal(saved, original) {
		t.Fatal("bound input changed", err)
	}
}

func TestProducerRejectsSymlinkDestinationAlias(t *testing.T) {
	opts, _ := fixtureProducer(t)
	use := &opts.Build.PerfAssetUses.Assets[0]
	oldPath := "assets/" + strings.SplitN(use.URL, "/gosx/assets/", 2)[1]
	body, err := os.ReadFile(filepath.Join(opts.DistDir, oldPath))
	if err != nil {
		t.Fatal(err)
	}
	use.URL = "/shared/runtime.js"
	producerTestFile(t, opts.DistDir, "shared/runtime.js", body)
	if err := os.Symlink("shared", filepath.Join(opts.DistDir, "alias")); err != nil {
		t.Skip("symlinks unavailable")
	}
	producerTestPublic(t, opts, "first.css", "css", []byte("new snapshot"))
	producerTestFile(t, opts.DistDir, "first.css", []byte("old snapshot"))
	producerTestPublic(t, opts, "alias/runtime.js", "js", []byte("replacement"))
	digest, err := ProduceFixture(context.Background(), opts)
	var typed *InputError
	if digest != "" || !errors.As(err, &typed) || typed.Code != "wrong-fixture" {
		t.Fatal("symlink alias reached the writer", err)
	}
	for name, expected := range map[string][]byte{"shared/runtime.js": body, "first.css": []byte("old snapshot")} {
		saved, err := os.ReadFile(filepath.Join(opts.DistDir, name))
		if err != nil || !bytes.Equal(saved, expected) {
			t.Fatal("symlink alias changed an existing file", err)
		}
	}
}

func TestProducerRejectsSymlinkSourceAlias(t *testing.T) {
	opts, _ := fixtureProducer(t)
	use := opts.Build.PerfAssetUses.Assets[0]
	name := filepath.Base(use.URL)
	if err := os.Rename(filepath.Join(opts.DistDir, "assets/runtime"), filepath.Join(opts.DistDir, "shared")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../shared", filepath.Join(opts.DistDir, "assets/runtime")); err != nil {
		t.Skip("symlinks unavailable")
	}
	producerTestPublic(t, opts, "shared/"+name, "js", []byte("replacement"))
	producerTestAliasRejection(t, opts, "shared/"+name)
}

func TestProducerRejectsHardLinkSourceAlias(t *testing.T) {
	opts, _ := fixtureProducer(t)
	use := opts.Build.PerfAssetUses.Assets[0]
	source := "assets/runtime/" + filepath.Base(use.URL)
	target := "shared/runtime.js"
	if err := os.MkdirAll(filepath.Join(opts.DistDir, "shared"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(filepath.Join(opts.DistDir, source), filepath.Join(opts.DistDir, target)); err != nil {
		t.Skip("hard links unavailable")
	}
	producerTestPublic(t, opts, target, "js", []byte("replacement"))
	producerTestAliasRejection(t, opts, target)
}

func producerTestAliasRejection(t *testing.T, opts ProducerOptions, name string) {
	t.Helper()
	file := filepath.Join(opts.DistDir, filepath.FromSlash(name))
	original, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := ProduceFixture(context.Background(), opts)
	var typed *InputError
	if digest != "" || !errors.As(err, &typed) || typed.Code != "wrong-fixture" {
		t.Error("source alias reached the writer", err)
	}
	saved, err := os.ReadFile(file)
	if err != nil || !bytes.Equal(saved, original) {
		t.Error("source alias changed an existing file", err)
	}
}

func TestProducerFullFixtureSetRoundTrip(t *testing.T) {
	opts, document := fixtureProducer(t)
	runtime := opts.Build.PerfAssetUses.Assets[0]
	runtimePath := "assets/" + strings.SplitN(runtime.URL, "/gosx/assets/", 2)[1]
	runtimeBody, err := os.ReadFile(filepath.Join(opts.DistDir, runtimePath))
	if err != nil {
		t.Fatal(err)
	}
	producerTestFile(t, opts.DistDir, "static/counter/index.html", document)
	expected := map[string]map[string][]byte{
		runtime.ID:         producerTestEncodings(t, opts.DistDir, runtimePath, runtimeBody),
		"app/fixture/html": producerTestEncodings(t, opts.DistDir, "static/counter/index.html", document),
	}
	for _, public := range []struct{ name, kind, body string }{
		{"404.html", "html", string(document)}, {"errors/expired.html", "html", string(document)},
		{"styles/site.css", "css", "body{color:green}"}, {"scripts/widget.js", "js", "const active=true;"},
		{"models/mesh.glb", "model", "synthetic model"},
		{"gosx/assets/custom/credits.txt", "other", "fixture credits"},
	} {
		producerTestPublic(t, opts, public.name, public.kind, []byte(public.body))
		expected["app/fixture/public/"+public.name] = producerTestEncodings(t, opts.DistDir, "public/"+public.name, []byte(public.body))
	}
	digest, err := ProduceFixture(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(opts.DistDir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	raw, err := readMeasureFile(root, "perf-fixtures.v1.json", maxInputBytes)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := DecodeFixtureManifest(bytes.NewReader(raw))
	if err != nil || len(manifest.Assets) != len(expected) {
		t.Fatal("fixture contract did not cover every produced body", err)
	}
	for _, use := range manifest.Assets {
		body, representations, err := readFixtureBody(root, use.URL, use.Kind)
		if err != nil || producerHash(body) != use.SHA256 {
			t.Fatalf("produced body %s cannot be collected: %v", use.ID, err)
		}
		for encoding, encoded := range expected[use.ID] {
			if !bytes.Equal(representations[encoding], encoded) {
				t.Errorf("produced representation %s/%s differs", use.ID, encoding)
			}
		}
	}
	for _, chunks := range []bool{false, true} {
		report, err := testCollect(context.Background(), CollectOptions{Inputs: opts.Inputs, SHA: opts.SourceSHA, Client: opts.Client, ChunksOnly: chunks,
			Bindings: []CollectionBinding{{App: opts.App, DistDir: opts.DistDir, BaseURL: opts.BaseURL, ArtifactSHA256: digest}}})
		if err != nil || len(report.Assets) != len(expected) || report.Coverage.AssetsMeasured != int64(len(expected)) {
			t.Fatalf("full fixture set cannot be collected (chunks=%v): %v", chunks, err)
		}
	}
	if again, err := ProduceFixture(context.Background(), opts); err != nil || again != digest {
		t.Fatal("repeated full-set production changed the proof", err)
	}
}
