package main

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testsupport_test.go holds the fixture helpers the other test files share.
//
// The tests build synthetic chunks in a temporary directory. A synthetic chunk
// is small enough to state the expected result exactly, and it lets a test
// create the failure the tool must catch (a chunk that reads a symbol nothing
// declares, a stale bundle, a corrupt sidecar) without touching the 90 shipped
// source files. Separate tests then run the same checks over the real
// client/js tree.

// fixture is a temporary client/js directory plus the chunk table that
// describes it.
type fixture struct {
	dir string // the temporary client/js directory
	t   *testing.T
}

// newFixture creates a temporary client/js directory that holds bootstrap-src.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "bootstrap-src"), 0o755); err != nil {
		t.Fatalf("create bootstrap-src: %v", err)
	}
	return &fixture{dir: dir, t: t}
}

// writeSource writes one source file below bootstrap-src and returns the
// relative path a chunk entry uses.
func (f *fixture) writeSource(name, body string) string {
	f.t.Helper()
	rel := "bootstrap-src/" + name
	path := filepath.Join(f.dir, filepath.FromSlash(rel))
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		f.t.Fatalf("write %s: %v", rel, err)
	}
	return rel
}

// appendSource adds text to a source file that already exists. The --check
// tests use it to make one bundle stale.
func (f *fixture) appendSource(rel, extra string) {
	f.t.Helper()
	path := filepath.Join(f.dir, filepath.FromSlash(rel))
	current, err := os.ReadFile(path)
	if err != nil {
		f.t.Fatalf("read %s: %v", rel, err)
	}
	if err := os.WriteFile(path, append(current, extra...), 0o644); err != nil {
		f.t.Fatalf("append %s: %v", rel, err)
	}
}

// overwrite replaces a built artifact inside the fixture directory. The
// staleness tests use it to damage one artifact at a time.
func (f *fixture) overwrite(rel, body string) {
	f.t.Helper()
	if err := os.WriteFile(f.path(filepath.FromSlash(rel)), []byte(body), 0o644); err != nil {
		f.t.Fatalf("overwrite %s: %v", rel, err)
	}
}

// remove deletes a built artifact inside the fixture directory.
func (f *fixture) remove(rel string) {
	f.t.Helper()
	if err := os.Remove(f.path(filepath.FromSlash(rel))); err != nil {
		f.t.Fatalf("remove %s: %v", rel, err)
	}
}

// path resolves a path inside the fixture directory.
func (f *fixture) path(parts ...string) string {
	return filepath.Join(append([]string{f.dir}, parts...)...)
}

// read returns the bytes of a file inside the fixture directory.
func (f *fixture) read(rel string) []byte {
	f.t.Helper()
	data, err := os.ReadFile(f.path(filepath.FromSlash(rel)))
	if err != nil {
		f.t.Fatalf("read %s: %v", rel, err)
	}
	return data
}

// chunk builds one chunk entry from a bundle name and its source files.
func chunk(name string, rels ...string) output {
	entry := output{name: name}
	for _, rel := range rels {
		entry.sources = append(entry.sources, sourceFile(rel))
	}
	return entry
}

// useOutputs replaces the package chunk table for one test and restores it
// afterwards. The table is a package variable, so every entry point the tool
// exposes (run, verifyChunkClosure, chunksManifestJSON) then sees the fixture.
func useOutputs(t *testing.T, entries ...output) {
	t.Helper()
	previous := outputs
	outputs = entries
	t.Cleanup(func() { outputs = previous })
}

// runTool calls the tool's entry point with the given command line, the way the
// binary does. It resets flag.CommandLine first, because run registers its
// flags on that set and a second registration of the same flag panics.
func runTool(t *testing.T, args ...string) error {
	t.Helper()
	previousArgs := os.Args
	previousFlags := flag.CommandLine
	t.Cleanup(func() {
		os.Args = previousArgs
		flag.CommandLine = previousFlags
	})
	flag.CommandLine = flag.NewFlagSet("buildbootstrap", flag.ContinueOnError)
	flag.CommandLine.SetOutput(&strings.Builder{})
	os.Args = append([]string{"buildbootstrap"}, args...)
	return run()
}

// shippedClientJS resolves the real client/js directory. It fails, and never
// skips: the tests that read the shipped tree are the ones that cover the
// bundles the repository ships, so a lookup failure must be red.
func shippedClientJS(t *testing.T) string {
	t.Helper()
	dir, err := findClientJS("")
	if err != nil {
		t.Fatalf("cannot locate client/js: %v", err)
	}
	return dir
}

// compressibleSource returns a JavaScript source that both compressors beat.
// The tool drops a sidecar that is not smaller than the raw bundle, so a
// sidecar test needs a payload with real redundancy.
func compressibleSource(prefix string, count int) string {
	var b strings.Builder
	for i := 0; i < count; i++ {
		b.WriteString("  function ")
		b.WriteString(prefix)
		b.WriteString(itoa(i))
		b.WriteString("(alpha, beta) {\n")
		b.WriteString("    // one line of prose that repeats, so the compressors have redundancy to find\n")
		b.WriteString("    var accumulator = alpha + beta + " + itoa(i) + ";\n")
		b.WriteString("    return accumulator * 2;\n")
		b.WriteString("  }\n")
	}
	return b.String()
}

// itoa avoids a strconv import in every caller.
func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var digits []byte
	for v > 0 {
		digits = append([]byte{byte('0' + v%10)}, digits...)
		v /= 10
	}
	return string(digits)
}
