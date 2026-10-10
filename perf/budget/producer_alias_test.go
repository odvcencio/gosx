package budget

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// The seed fixes construction order; every combination is exercised so a new
// identity rule cannot hide behind a randomly unselected alias shape.
func TestProducerAliasTreesPreserveInputsAndCollect(t *testing.T) {
	type tree struct {
		input, link, relation string
		sidecar               bool
	}
	var corpus []tree
	for _, input := range []string{"runtime", "runtime-gzip", "runtime-brotli", "public", "public-gzip", "static", "static-gzip", "build-manifest"} {
		for _, link := range []string{"relative", "absolute", "chained", "directory", "hard"} {
			for _, relation := range []string{"source", "destination", "input"} {
				for _, sidecar := range []bool{false, true} {
					corpus = append(corpus, tree{input, link, relation, sidecar})
				}
			}
		}
	}
	rand.New(rand.NewSource(57402)).Shuffle(len(corpus), func(i, j int) { corpus[i], corpus[j] = corpus[j], corpus[i] })
	accepted := 0
	for i, shape := range corpus {
		t.Run(fmt.Sprintf("%03d/%s/%s/%s/sidecar=%v", i, shape.input, shape.link, shape.relation, shape.sidecar), func(t *testing.T) {
			opts, document := fixtureProducer(t)
			runtime := "assets/runtime/" + filepath.Base(opts.Build.PerfAssetUses.Assets[0].URL)
			body, err := os.ReadFile(filepath.Join(opts.DistDir, runtime))
			if err != nil {
				t.Fatal(err)
			}
			producerTestEncodings(t, opts.DistDir, runtime, body)
			producerTestPublic(t, opts, "snapshot.bin", "other", []byte("public source"))
			producerTestEncodings(t, opts.DistDir, "public/snapshot.bin", []byte("public source"))
			producerTestFile(t, opts.DistDir, "static/counter/index.html", document)
			producerTestEncodings(t, opts.DistDir, "static/counter/index.html", document)
			producerTestFile(t, opts.DistDir, "build.json", []byte("production manifest"))
			sources := map[string]string{
				"runtime": runtime, "runtime-gzip": runtime + ".gz", "runtime-brotli": runtime + ".br",
				"public": "public/snapshot.bin", "public-gzip": "public/snapshot.bin.gz",
				"static": "static/counter/index.html", "static-gzip": "static/counter/index.html.gz", "build-manifest": "build.json",
			}
			source := filepath.Join(opts.DistDir, filepath.FromSlash(sources[shape.input]))
			target := filepath.Join(opts.DistDir, "copies", "replacement.bin")
			if shape.relation == "input" {
				target = filepath.Join(opts.DistDir, "public", "extra.bin")
				if shape.sidecar {
					target += ".gz"
				}
			} else {
				producerTestPublic(t, opts, "copies/replacement.bin", "other", []byte("replacement body"))
				if shape.sidecar {
					target += ".gz"
				}
			}
			if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
				t.Fatal(err)
			}
			if shape.relation == "source" {
				original, err := os.ReadFile(source)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(target, original, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(source); err != nil {
					t.Fatal(err)
				}
				producerTestLink(t, shape.link, source, target)
			} else {
				producerTestLink(t, shape.link, target, source)
			}
			// Observe through every reserved spelling, including source-side aliases.
			before := map[string][]byte{}
			for _, name := range []string{runtime, runtime + ".gz", runtime + ".br", "public/snapshot.bin", "public/snapshot.bin.gz", "public/snapshot.bin.br", "static/counter/index.html", "static/counter/index.html.gz", "static/counter/index.html.br", "build.json"} {
				file := filepath.Join(opts.DistDir, filepath.FromSlash(name))
				data, err := os.ReadFile(file)
				if err != nil {
					t.Fatal(err)
				}
				before[file] = data
			}
			digest, produceErr := ProduceFixture(context.Background(), opts)
			for file, original := range before {
				data, err := os.ReadFile(file)
				if err != nil || !bytes.Equal(data, original) {
					t.Error("reserved input changed", err)
				}
			}
			if produceErr != nil {
				if digest != "" {
					t.Error("failed producer returned a digest")
				}
				return
			}
			accepted++
			for _, chunks := range []bool{false, true} {
				_, err = testCollect(context.Background(), CollectOptions{Inputs: opts.Inputs, SHA: opts.SourceSHA, Client: opts.Client, ChunksOnly: chunks,
					Bindings: []CollectionBinding{{App: opts.App, DistDir: opts.DistDir, BaseURL: opts.BaseURL, ArtifactSHA256: digest}}})
				if err != nil {
					t.Errorf("successful fixture could not be collected (chunks=%v): %v", chunks, err)
				}
			}
		})
	}
	if accepted == 0 {
		t.Error("corpus did not exercise a successful fixture")
	}
	t.Logf("seed=57402 trees=%d successful=%d", len(corpus), accepted)
}

func TestProducerRechecksIdentityAfterPreflight(t *testing.T) {
	opts, _ := fixtureProducer(t)
	source := filepath.Join(opts.DistDir, "assets/runtime", filepath.Base(opts.Build.PerfAssetUses.Assets[0].URL))
	target := filepath.Join(opts.DistDir, "counter/index.html")
	transport := opts.Client.Transport
	opts.Client = &http.Client{Transport: testRoundTrip(func(req *http.Request) (*http.Response, error) {
		// Route fetching occurs after preflight and before the snapshot write.
		if err := os.Remove(target); err != nil {
			return nil, err
		}
		if err := os.Link(source, target); err != nil {
			return nil, err
		}
		return transport.RoundTrip(req)
	})}
	producerTestAliasRejection(t, opts, "assets/runtime/"+filepath.Base(source))
}

func TestProducerProtectsExternalInputIdentity(t *testing.T) {
	opts, _ := fixtureProducer(t)
	source := filepath.Join(opts.Inputs.RootDir(), opts.Inputs.File.Profile.File)
	producerTestFile(t, opts.Inputs.RootDir(), opts.Inputs.File.Profile.File, []byte("bound input"))
	target := "profile-alias.bin"
	if err := os.Link(source, filepath.Join(opts.DistDir, target)); err != nil {
		t.Skip("hard links unavailable")
	}
	producerTestPublic(t, opts, target, "other", []byte("replacement"))
	producerTestAliasRejection(t, opts, target)
}

func producerTestLink(t *testing.T, kind, alias, target string) {
	t.Helper()
	var err error
	switch kind {
	case "hard":
		err = os.Link(target, alias)
	case "absolute":
		err = os.Symlink(target, alias)
	case "relative":
		var relative string
		relative, err = filepath.Rel(filepath.Dir(alias), target)
		if err == nil {
			err = os.Symlink(relative, alias)
		}
	case "chained":
		middle := alias + ".link"
		err = os.Symlink(target, middle)
		if err == nil {
			err = os.Symlink(filepath.Base(middle), alias)
		}
	case "directory":
		middle := alias + ".dir"
		err = os.Symlink(filepath.Dir(target), middle)
		if err == nil {
			err = os.Symlink(filepath.Join(filepath.Base(middle), filepath.Base(target)), alias)
		}
	default:
		t.Fatal("unknown link shape")
	}
	if err != nil {
		t.Skip("link creation unavailable")
	}
}
