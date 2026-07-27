package main

import (
	"os"
	"strings"
	"testing"
)

// closure_test.go covers the check that proves each chunk is closed: every
// identifier a chunk reads must be declared inside that chunk, guarded by
// typeof, or supplied by the browser.
//
// The defect class these tests hold shut has shipped twice. A marker based split
// put one function into two chunks, and a free variable scan found
// 16z-scene-webgpu-probe.js reading loadManifest behind a typeof guard. Dropping
// the file that declared loadManifest turned a crash into a silently wrong
// WebGPU adapter request. Neither a size gate nor a staleness check can see
// either fault, so the closure check is the only guard, and it needs its own
// tests.

const definerSource = `(function () {
  "use strict";
  function sharedHelper(value) {
    return value + 1;
  }
  window.__gosx_test_shared = sharedHelper;
`

const readerSource = `  window.__gosx_test_result = sharedHelper(41);
})();
`

// TestChunkFreeIdentifiersFindsSymbolReadButNeverDeclared is the core case: a
// chunk that ships the reader without the declaration must report the symbol.
func TestChunkFreeIdentifiersFindsSymbolReadButNeverDeclared(t *testing.T) {
	f := newFixture(t)
	definer := f.writeSource("10-definer.js", definerSource)
	reader := f.writeSource("20-reader.js", readerSource)

	// The split that keeps both files is closed.
	free, err := chunkFreeIdentifiers(f.dir, chunk("closed.js", definer, reader))
	if err != nil {
		t.Fatalf("closed chunk: %v", err)
	}
	if len(free) != 0 {
		t.Fatalf("closed chunk reports free identifiers %v, want none", free)
	}

	// The split that leaves the declaration behind is not. This is the exact
	// shape of a chunk that throws ReferenceError on its first real call.
	broken := chunk("broken.js", f.writeSource("20-reader-alone.js", "(function () {\n"+readerSource))
	free, err = chunkFreeIdentifiers(f.dir, broken)
	if err != nil {
		t.Fatalf("broken chunk: %v", err)
	}
	if len(free) != 1 || free[0] != "sharedHelper" {
		t.Fatalf("broken chunk reports %v, want exactly [sharedHelper]", free)
	}
}

// TestVerifyChunkClosureFailsAndNamesTheChunk checks the reported message. The
// message must name the chunk and the symbol, because the reader has to know
// which of 14 chunks lost which declaration.
func TestVerifyChunkClosureFailsAndNamesTheChunk(t *testing.T) {
	f := newFixture(t)
	reader := f.writeSource("20-reader-alone.js", "(function () {\n"+readerSource)
	useOutputs(t, chunk("bootstrap-feature-broken.js", reader))

	err := verifyChunkClosure(f.dir)
	if err == nil {
		t.Fatal("verifyChunkClosure accepted a chunk that reads an undeclared symbol")
	}
	for _, want := range []string{"bootstrap-feature-broken.js", "sharedHelper"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("closure error does not mention %q; got: %v", want, err)
		}
	}
}

// TestBuildRefusesAChunkThatIsNotClosed proves the check fails the BUILD. A
// chunk that reads a symbol nothing declares must never reach disk, because a
// written bundle is a shipped bundle and the fault then shows up as a browser
// ReferenceError instead of a red build.
func TestBuildRefusesAChunkThatIsNotClosed(t *testing.T) {
	f := newFixture(t)
	reader := f.writeSource("20-reader-alone.js", "(function () {\n"+readerSource)
	useOutputs(t, chunk("bootstrap-feature-broken.js", reader))

	if err := runTool(t, "-dir", f.dir); err == nil {
		t.Fatal("build accepted a chunk that reads an undeclared symbol")
	}
	for _, artifact := range []string{
		"bootstrap-feature-broken.js",
		"bootstrap-feature-broken.js.map",
		"bootstrap-src/chunks.json",
	} {
		if _, err := os.Stat(f.path(artifact)); err == nil {
			t.Errorf("build wrote %s although the closure check failed; the check must run before any write", artifact)
		}
	}
}

// TestChunkClosureTreatsTypeofAsOptional pins the documented exception. Code
// that writes `typeof X === "function"` before it reads X states that X is
// optional, and typeof never throws for an undeclared name.
func TestChunkClosureTreatsTypeofAsOptional(t *testing.T) {
	f := newFixture(t)
	guarded := f.writeSource("30-guarded.js", `(function () {
  if (typeof optionalHelper === "function") {
    optionalHelper();
  }
})();
`)
	free, err := chunkFreeIdentifiers(f.dir, chunk("guarded.js", guarded))
	if err != nil {
		t.Fatalf("guarded chunk: %v", err)
	}
	if len(free) != 0 {
		t.Fatalf("guarded chunk reports %v, want none: a typeof probe is a deliberate optional symbol", free)
	}
}

// TestChunkClosureAcceptsBrowserGlobalsAndIgnoresComments states the two other
// classes of report that are noise. A name that appears only in a comment or a
// string is not a read at all, and the parser must not confuse either for code.
func TestChunkClosureAcceptsBrowserGlobalsAndIgnoresComments(t *testing.T) {
	f := newFixture(t)
	rel := f.writeSource("40-globals.js", `(function () {
  // mentionedOnlyInAComment is not a read
  var text = "mentionedOnlyInAString";
  var canvas = document.createElement("canvas");
  var ctx = canvas.getContext("2d");
  var probe = new Float32Array(4);
  requestAnimationFrame(function () {
    console.log(text, ctx, probe, navigator.userAgent, performance.now(), WebAssembly);
  });
})();
`)
	free, err := chunkFreeIdentifiers(f.dir, chunk("globals.js", rel))
	if err != nil {
		t.Fatalf("globals chunk: %v", err)
	}
	if len(free) != 0 {
		t.Fatalf("globals chunk reports %v, want none", free)
	}
}

// TestChunkClosureReportsEveryMissingSymbolSorted checks that one broken chunk
// lists all of its losses at once, in a stable order. A split usually drops
// several names together, and a one-name-per-run report costs one build cycle
// per name.
func TestChunkClosureReportsEveryMissingSymbolSorted(t *testing.T) {
	f := newFixture(t)
	rel := f.writeSource("50-many-reads.js", `(function () {
  zulu();
  alpha();
  mike();
})();
`)
	free, err := chunkFreeIdentifiers(f.dir, chunk("many.js", rel))
	if err != nil {
		t.Fatalf("many-reads chunk: %v", err)
	}
	want := []string{"alpha", "mike", "zulu"}
	if len(free) != len(want) {
		t.Fatalf("got %v, want %v", free, want)
	}
	for i := range want {
		if free[i] != want[i] {
			t.Fatalf("got %v, want %v (sorted)", free, want)
		}
	}
}

// TestChunkClosureRejectsUnparseableSource proves a syntax error fails the
// check instead of passing as "no free identifiers found".
func TestChunkClosureRejectsUnparseableSource(t *testing.T) {
	f := newFixture(t)
	rel := f.writeSource("60-broken-syntax.js", "(function () { var = ; })();\n")
	if _, err := chunkFreeIdentifiers(f.dir, chunk("syntax.js", rel)); err == nil {
		t.Fatal("chunkFreeIdentifiers accepted source the parser cannot read")
	}
}

// TestChunkClosureReportsAMissingSourceFile proves a chunk that names a file
// that does not exist fails, rather than silently building a shorter bundle.
func TestChunkClosureReportsAMissingSourceFile(t *testing.T) {
	f := newFixture(t)
	if _, err := chunkFreeIdentifiers(f.dir, chunk("absent.js", "bootstrap-src/does-not-exist.js")); err == nil {
		t.Fatal("chunkFreeIdentifiers accepted a chunk that names a missing file")
	}
}

// TestShippedChunksAreClosed runs the real gate over the real source tree, so
// `go test` fails on a bad split even when nobody runs the build. The build runs
// this check too, but a test that states it keeps the guard inside the test
// suite as well.
func TestShippedChunksAreClosed(t *testing.T) {
	dir := shippedClientJS(t)
	if err := verifyChunkClosure(dir); err != nil {
		t.Fatalf("a shipped chunk reads an identifier nothing declares:\n%v", err)
	}
}
