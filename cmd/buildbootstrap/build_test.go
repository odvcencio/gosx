package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/gzip"
)

// build_test.go covers the build itself: what --check calls stale, what the
// compressed sidecars must hold, and whether one run of the build is
// reproducible.
//
// This tool writes every shipped client bundle. A defect here corrupts
// bootstrap.js, bootstrap-lite.js, bootstrap-runtime.js, every
// bootstrap-feature-*.js, and each .br, .gz and .map sibling. `make test-js`
// only ran the tool in --check mode, which detects staleness but not wrongness.

// buildFixture writes two synthetic chunks and returns the fixture. One chunk
// is large and repetitive, so both compressors beat it and the sidecar tests
// have sidecars to inspect.
func buildFixture(t *testing.T) (*fixture, string, string) {
	t.Helper()
	f := newFixture(t)
	head := f.writeSource("10-head.js", "(function () {\n  \"use strict\";\n")
	body := f.writeSource("20-body.js", compressibleSource("fixtureHelper", 200))
	tail := f.writeSource("30-tail.js", "  window.__gosx_fixture = fixtureHelper0(1, 2);\n})();\n")
	small := f.writeSource("40-small.js", "(function () {\n  window.__gosx_small = 1;\n})();\n")

	large := chunk("fixture-large.js", head, body, tail)
	tiny := chunk("fixture-small.js", small)
	useOutputs(t, large, tiny)
	return f, large.name, tiny.name
}

func gunzip(t *testing.T, raw []byte) []byte {
	t.Helper()
	r, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("open gzip stream: %v", err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read gzip stream: %v", err)
	}
	return out
}

func unbrotli(t *testing.T, raw []byte) []byte {
	t.Helper()
	out, err := io.ReadAll(brotli.NewReader(bytes.NewReader(raw)))
	if err != nil {
		t.Fatalf("read brotli stream: %v", err)
	}
	return out
}

// TestBuildWritesBundleMapAndSidecars states what one build produces.
func TestBuildWritesBundleMapAndSidecars(t *testing.T) {
	f, large, small := buildFixture(t)

	if err := runTool(t, "-dir", f.dir); err != nil {
		t.Fatalf("build: %v", err)
	}

	code := f.read(large)
	if len(code) == 0 {
		t.Fatal("build wrote an empty bundle")
	}
	if !bytes.HasSuffix(code, []byte("\n")) {
		t.Error("bundle does not end with a newline; the retired Node pipeline always ended with one")
	}
	if !strings.Contains(string(code), "__gosx_fixture") {
		t.Error("bundle lost the tail source")
	}
	if bytes.Contains(code, []byte("one line of prose that repeats")) {
		t.Error("bundle kept a comment; the build must drop comment-only lines")
	}
	if _, err := os.Stat(f.path(large + ".map")); err != nil {
		t.Errorf("build wrote no source map: %v", err)
	}
	if _, err := os.Stat(f.path(chunksManifestRel)); err != nil {
		t.Errorf("build wrote no chunk manifest: %v", err)
	}

	// The large chunk compresses, so both sidecars must exist and must
	// decompress back to the bundle byte for byte.
	gz := f.read(large + ".gz")
	if got := gunzip(t, gz); !bytes.Equal(got, code) {
		t.Errorf("%s.gz does not decompress to %s (%d bytes vs %d)", large, large, len(got), len(code))
	}
	br := f.read(large + ".br")
	if got := unbrotli(t, br); !bytes.Equal(got, code) {
		t.Errorf("%s.br does not decompress to %s (%d bytes vs %d)", large, large, len(got), len(code))
	}
	if len(gz) >= len(code) || len(br) >= len(code) {
		t.Errorf("sidecars are not smaller than the bundle (raw %d, gz %d, br %d)", len(code), len(gz), len(br))
	}

	// The small chunk is a few dozen bytes. The rule is one sided: a sidecar
	// that exists must be smaller than the bundle and must decompress to it. A
	// gzip stream carries a fixed 18-byte header and trailer, so gzip can never
	// win on a payload this short and the build must write no .gz at all.
	// Brotli carries a static dictionary of common web text, so brotli can win
	// here, and the build then keeps that sidecar.
	tiny := f.read(small)
	if _, err := os.Stat(f.path(small + ".gz")); err == nil {
		t.Errorf("build wrote %s.gz for a %d-byte bundle; gzip cannot beat its own header at that size", small, len(tiny))
	}
	if raw, err := os.ReadFile(f.path(small + ".br")); err == nil {
		if len(raw) >= len(tiny) {
			t.Errorf("build kept %s.br at %d bytes for a %d-byte bundle; a sidecar must be smaller than its source",
				small, len(raw), len(tiny))
		}
		if got := unbrotli(t, raw); !bytes.Equal(got, tiny) {
			t.Errorf("%s.br does not decompress to %s", small, small)
		}
	}
}

// TestBuildRemovesASidecarThatNoLongerHelps proves the build cleans up. A
// sidecar left behind from an earlier, larger bundle would be served as the
// encoded reply and would not match the bundle.
func TestBuildRemovesASidecarThatNoLongerHelps(t *testing.T) {
	f, _, small := buildFixture(t)
	stale := f.path(small + ".gz")
	if err := os.WriteFile(stale, []byte("stale sidecar from an earlier bundle"), 0o644); err != nil {
		t.Fatalf("plant stale sidecar: %v", err)
	}
	if err := runTool(t, "-dir", f.dir); err != nil {
		t.Fatalf("build: %v", err)
	}
	if _, err := os.Stat(stale); err == nil {
		t.Errorf("build kept %s.gz; a sidecar that does not beat the raw bundle must be removed", small)
	}
}

// TestBuildIsReproducible proves two runs over one source tree agree byte for
// byte. The bundles are committed, so a build that changed bytes without a
// source change would make every --check run report a false staleness.
func TestBuildIsReproducible(t *testing.T) {
	f, large, _ := buildFixture(t)
	if err := runTool(t, "-dir", f.dir); err != nil {
		t.Fatalf("first build: %v", err)
	}
	first := map[string][]byte{}
	for _, rel := range []string{large, large + ".map", large + ".gz", large + ".br", chunksManifestRel} {
		first[rel] = f.read(rel)
	}
	if err := runTool(t, "-dir", f.dir); err != nil {
		t.Fatalf("second build: %v", err)
	}
	for rel, want := range first {
		if got := f.read(rel); !bytes.Equal(got, want) {
			t.Errorf("%s changed between two builds of the same sources (%d bytes then %d)", rel, len(want), len(got))
		}
	}
}

// TestCheckIsCleanAfterABuild is the baseline for every staleness case below.
func TestCheckIsCleanAfterABuild(t *testing.T) {
	f, _, _ := buildFixture(t)
	if err := runTool(t, "-dir", f.dir); err != nil {
		t.Fatalf("build: %v", err)
	}
	if err := runTool(t, "-dir", f.dir, "--check"); err != nil {
		t.Fatalf("--check reports stale right after a build: %v", err)
	}
}

// TestCheckReportsStaleWhenASourceChanges is the case the gate exists for. It
// must name the bundle that changed, and it must not name the bundle that did
// not, so a reader knows which chunk to look at.
func TestCheckReportsStaleWhenASourceChanges(t *testing.T) {
	f, large, small := buildFixture(t)
	if err := runTool(t, "-dir", f.dir); err != nil {
		t.Fatalf("build: %v", err)
	}

	f.appendSource("bootstrap-src/20-body.js", "  function addedAfterTheBuild() { return 7; }\n")

	err := runTool(t, "-dir", f.dir, "--check")
	if err == nil {
		t.Fatal("--check passed after a source file changed")
	}
	if !strings.Contains(err.Error(), large) {
		t.Errorf("--check does not name the stale bundle %s; got: %v", large, err)
	}
	if strings.Contains(err.Error(), small) {
		t.Errorf("--check names %s, which no source change touched; got: %v", small, err)
	}

	// A rebuild must clear it. This is the second half of the contract: the gate
	// tells a reader to run the build, so the build has to satisfy the gate.
	if err := runTool(t, "-dir", f.dir); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if err := runTool(t, "-dir", f.dir, "--check"); err != nil {
		t.Fatalf("--check still reports stale after the rebuild: %v", err)
	}
}

// TestCheckReportsStaleForEachArtifactClass walks the four artifacts one bundle
// owns. Each one is served to a browser, so a stale copy of any of them is a
// shipped defect.
func TestCheckReportsStaleForEachArtifactClass(t *testing.T) {
	cases := []struct {
		name    string
		damage  func(f *fixture, large string)
		wantHit string
	}{
		{
			name:    "bundle code",
			damage:  func(f *fixture, large string) { f.overwrite(large, "// truncated\n") },
			wantHit: "fixture-large.js",
		},
		{
			name:    "source map",
			damage:  func(f *fixture, large string) { f.overwrite(large+".map", "{}") },
			wantHit: "fixture-large.js",
		},
		{
			name:    "gzip sidecar",
			damage:  func(f *fixture, large string) { f.overwrite(large+".gz", "not a gzip stream") },
			wantHit: "fixture-large.js",
		},
		{
			name:    "brotli sidecar",
			damage:  func(f *fixture, large string) { f.remove(large + ".br") },
			wantHit: "fixture-large.js",
		},
		{
			name:    "chunk manifest",
			damage:  func(f *fixture, large string) { f.remove(chunksManifestRel) },
			wantHit: chunksManifestRel,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, large, _ := buildFixture(t)
			if err := runTool(t, "-dir", f.dir); err != nil {
				t.Fatalf("build: %v", err)
			}
			tc.damage(f, large)
			err := runTool(t, "-dir", f.dir, "--check")
			if err == nil {
				t.Fatalf("--check passed with a damaged %s", tc.name)
			}
			if !strings.Contains(err.Error(), tc.wantHit) {
				t.Errorf("--check does not name %s; got: %v", tc.wantHit, err)
			}
		})
	}
}

// TestCheckWritesNothing proves the gate is read only. CI runs it on a checkout
// and then trusts `git status`, so a --check run that touched a bundle would
// report a false diff, or hide a real one.
func TestCheckWritesNothing(t *testing.T) {
	f, large, _ := buildFixture(t)
	if err := runTool(t, "-dir", f.dir); err != nil {
		t.Fatalf("build: %v", err)
	}
	before := map[string][]byte{}
	for _, rel := range []string{large, large + ".map", large + ".gz", large + ".br", chunksManifestRel} {
		before[rel] = f.read(rel)
	}
	f.appendSource("bootstrap-src/20-body.js", "  function addedAfterTheBuild() { return 7; }\n")
	if err := runTool(t, "-dir", f.dir, "--check"); err == nil {
		t.Fatal("--check passed after a source change")
	}
	for rel, want := range before {
		if got := f.read(rel); !bytes.Equal(got, want) {
			t.Errorf("--check rewrote %s; the gate must not write", rel)
		}
	}
}

// TestChunkManifestListsEveryChunkAndSource pins the machine-readable manifest.
// The JS size gates and the acorn cross-check read chunks.json, so a manifest
// that drifts from the table sends those tools at the wrong file list.
func TestChunkManifestListsEveryChunkAndSource(t *testing.T) {
	f, large, small := buildFixture(t)
	if err := runTool(t, "-dir", f.dir); err != nil {
		t.Fatalf("build: %v", err)
	}
	manifest := string(f.read(chunksManifestRel))
	for _, want := range []string{large, small, "bootstrap-src/20-body.js", "bootstrap-src/40-small.js"} {
		if !strings.Contains(manifest, want) {
			t.Errorf("chunks.json does not list %q", want)
		}
	}
	if manifest != chunksManifestJSON() {
		t.Error("the written chunks.json differs from chunksManifestJSON()")
	}
}

// TestShippedManifestMatchesTheChunkTable checks the committed chunks.json
// against the table in the source. --check covers this too. The test states it
// so `go test` alone catches a hand edit of the generated file.
func TestShippedManifestMatchesTheChunkTable(t *testing.T) {
	dir := shippedClientJS(t)
	committed, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(chunksManifestRel)))
	if err != nil {
		t.Fatalf("read committed chunks.json: %v", err)
	}
	if string(committed) != chunksManifestJSON() {
		if !deferredHostCutoverArtifacts[chunksManifestRel] {
			t.Error("committed client/js/bootstrap-src/chunks.json is stale; run `go run ./cmd/buildbootstrap`")
		}
	} else if deferredHostCutoverArtifacts[chunksManifestRel] {
		t.Error("chunks.json is current but still marked as a deferred Stack 03 artifact")
	}
}

// TestShippedSourceFilesAllExist proves every path in the chunk table resolves.
// A renamed source file must fail here, not produce a shorter bundle.
func TestShippedSourceFilesAllExist(t *testing.T) {
	dir := shippedClientJS(t)
	for _, entry := range outputs {
		for _, src := range entry.sources {
			if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(src.rel))); err != nil {
				t.Errorf("chunk %s names %s, which does not exist: %v", entry.name, src.rel, err)
			}
		}
	}
}

// TestShippedSidecarsDecompressToTheirBundle checks the committed .gz and .br
// files against the committed .js file. --check compares against freshly
// compressed bytes, which also fails when a compressor version changes. This
// test asks the question a browser asks: does the encoded reply decode to the
// bundle?
func TestShippedSidecarsDecompressToTheirBundle(t *testing.T) {
	dir := shippedClientJS(t)
	for _, entry := range outputs {
		t.Run(entry.name, func(t *testing.T) {
			if deferredHostCutoverArtifacts[entry.name] &&
				(entry.name == "patch.js" || entry.name == "relay.js" || entry.name == "stripe-bridge.js") {
				t.Skip("standalone host sidecars are deliberately deferred to Stack 07")
			}
			bundlePath := filepath.Join(dir, entry.name)
			code, err := os.ReadFile(bundlePath)
			if err != nil {
				t.Fatalf("read committed bundle: %v", err)
			}
			// compressedSidecars reports which sidecars this payload should have.
			// A payload that compression does not beat must have none.
			expected, err := compressedSidecars(bundlePath, string(code))
			if err != nil {
				t.Fatalf("compress: %v", err)
			}
			for _, sc := range expected {
				_, statErr := os.Stat(sc.path)
				if sc.bytes == nil {
					if statErr == nil {
						t.Errorf("%s exists although compression does not beat the raw bundle", sc.path)
					}
					continue
				}
				raw, err := os.ReadFile(sc.path)
				if err != nil {
					t.Errorf("missing sidecar %s: %v", sc.path, err)
					continue
				}
				var got []byte
				if strings.HasSuffix(sc.path, ".gz") {
					got = gunzip(t, raw)
				} else {
					got = unbrotli(t, raw)
				}
				if !bytes.Equal(got, code) {
					t.Errorf("%s decompresses to %d bytes, but the bundle holds %d; the sidecar does not match its source",
						sc.path, len(got), len(code))
				}
			}
		})
	}
}
