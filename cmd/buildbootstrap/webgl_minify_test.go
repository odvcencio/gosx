package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	esbuild "github.com/evanw/esbuild/pkg/api"
)

// webgl_minify_test.go guards the property names the WebGL chunk renames.
//
// minifyESBuild renames every property on webGLPrivatePropertyPattern, and it
// does so in bootstrap-feature-scene3d-webgl.js only. A name on that list is
// therefore safe only while no other source reads or writes a property of the
// same name: the other chunk keeps the authored name, the WebGL chunk uses the
// short one, and the two stop meeting.
//
// The repository has paid for this once. The list held "frame",
// "vertexShader" and "fragmentShader":
//
//   - The Selena lighting chunk publishes {frame, value, shadowSlot}. The WebGL
//     chunk called it as api.r(...), so every frame threw "b.r is not a
//     function" once the lighting chunk loaded, and the canvas stayed empty.
//   - The WebGL chunk hands {program, vertexShader, fragmentShader} records to
//     the glTF chunk and to the WebGL post-effect chunk. Both delete the two
//     shaders by their authored names, read undefined, and left them alive.
//
// No other gate saw either one: the monolith is not renamed, so every browser
// test that loads bootstrap.js passed, and --check only proves that the built
// chunk matches its sources.

const webGLChunkName = "bootstrap-feature-scene3d-webgl.js"

// webGLPrivatePropertyNames returns the names on the rename list.
func webGLPrivatePropertyNames(t *testing.T) []string {
	t.Helper()
	return privatePropertyNames(t, webGLPrivatePropertyPattern)
}

// privatePropertyNames splits a rename pattern into its names.
func privatePropertyNames(t *testing.T, pattern string) []string {
	t.Helper()
	body, ok := strings.CutPrefix(pattern, "^(")
	body, closed := strings.CutSuffix(body, ")$")
	if !ok || !closed || strings.ContainsAny(body, `()[]\.*+?`) {
		t.Fatalf("a rename pattern must stay a plain list of whole names, ^(a|b|c)$; got %q", pattern)
	}
	return strings.Split(body, "|")
}

// renamedProperties returns the names on the rename list that code uses as a
// property name. It asks esbuild, because esbuild does the renaming: a dotted
// access, an object key, a shorthand key, a quoted key and a destructuring key
// all count, and a string value or a local variable does not.
func renamedProperties(name, code, pattern string) ([]string, error) {
	result := esbuild.Transform(code, esbuild.TransformOptions{
		Loader:       esbuild.LoaderJS,
		Sourcefile:   name,
		MangleProps:  pattern,
		MangleQuoted: esbuild.MangleQuotedTrue,
		MangleCache:  map[string]interface{}{},
	})
	if len(result.Errors) > 0 {
		return nil, fmt.Errorf("esbuild %s: %s", name, result.Errors[0].Text)
	}
	names := make([]string, 0, len(result.MangleCache))
	for property := range result.MangleCache {
		names = append(names, property)
	}
	sort.Strings(names)
	return names, nil
}

// webGLRenameLeak is one renamed name that a source outside the WebGL chunk
// also uses as a property name.
type webGLRenameLeak struct {
	chunk   string
	name    string
	sources []string // the sources of that chunk that mention the name
}

// webGLRenameLeaks reads every chunk in entries and reports each renamed name
// that code outside the WebGL chunk's own sources uses as a property name.
//
// A source that also ships in the WebGL chunk is left out of the other chunk
// before the check. The monolith carries the whole renderer under its authored
// names, which is consistent with itself; what matters is the code that can
// meet the renamed chunk on one page.
func webGLRenameLeaks(dir string, entries []output) ([]webGLRenameLeak, error) {
	return renameLeaks(dir, entries, webGLChunkName, webGLPrivatePropertyPattern)
}

// renameLeaks is webGLRenameLeaks for any chunk and its rename pattern.
func renameLeaks(dir string, entries []output, chunkName, pattern string) ([]webGLRenameLeak, error) {
	inChunk := map[string]bool{}
	for _, entry := range entries {
		if entry.name != chunkName {
			continue
		}
		for _, src := range entry.sources {
			inChunk[src.rel] = true
		}
	}
	var leaks []webGLRenameLeak
	for _, entry := range entries {
		if entry.name == chunkName {
			continue
		}
		bodies := make([]string, len(entry.sources))
		for i, src := range entry.sources {
			if inChunk[src.rel] {
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(src.rel)))
			if err != nil {
				return nil, err
			}
			bodies[i] = normalizeNewlines(string(data))
		}
		code, err := transpileTypedChunk(entry, bodies)
		if err != nil {
			return nil, fmt.Errorf("transpile %s without the WebGL chunk's sources: %w", entry.name, err)
		}
		names, err := renamedProperties(entry.name, code, pattern)
		if err != nil {
			return nil, err
		}
		for _, name := range names {
			leak := webGLRenameLeak{chunk: entry.name, name: name}
			word := regexp.MustCompile(`(^|[^\w$])` + regexp.QuoteMeta(name) + `([^\w$]|$)`)
			for i, src := range entry.sources {
				if word.MatchString(bodies[i]) {
					leak.sources = append(leak.sources, src.rel)
				}
			}
			leaks = append(leaks, leak)
		}
	}
	return leaks, nil
}

// TestWebGLRenamedPropertiesStayInsideTheWebGLChunk is the gate. It fails when
// a name on the rename list is also a property name in any other chunk.
//
// To fix a failure, take the name off webGLPrivatePropertyPattern, or give the
// WebGL chunk's private field a name nothing else uses. Do not add an
// exception here: whether two same-named properties are "really" the same
// object is exactly the judgment that failed before.
func TestWebGLRenamedPropertiesStayInsideTheWebGLChunk(t *testing.T) {
	checkRenamedPropertiesStayInside(t, webGLChunkName, webGLPrivatePropertyPattern)
}

// TestWebGPURenamedPropertiesStayInsideTheWebGPUChunk is the same gate for the
// WebGPU chunk and webGPUPrivatePropertyPattern.
func TestWebGPURenamedPropertiesStayInsideTheWebGPUChunk(t *testing.T) {
	checkRenamedPropertiesStayInside(t, webGPUChunkName, webGPUPrivatePropertyPattern)
}

func checkRenamedPropertiesStayInside(t *testing.T, chunkName, pattern string) {
	t.Helper()
	leaks, err := renameLeaks(shippedClientJS(t), outputs, chunkName, pattern)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string][]string{}
	for _, leak := range leaks {
		byName[leak.name] = append(byName[leak.name], fmt.Sprintf("%s (%s)", leak.chunk, strings.Join(leak.sources, ", ")))
	}
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		t.Errorf("%s renames the property %q, and other chunks use that name too:\n  %s", chunkName, name, strings.Join(byName[name], "\n  "))
	}
}

// TestWebGLRenameListHasNoUnusedName keeps the list honest in the other
// direction: a name the WebGL chunk no longer uses renames nothing, and it
// would start renaming the day another file adopts the word.
func TestWebGLRenameListHasNoUnusedName(t *testing.T) {
	checkRenameListHasNoUnusedName(t, webGLChunkName, webGLPrivatePropertyPattern)
}

// TestWebGPURenameListHasNoUnusedName is the same check for the WebGPU list.
func TestWebGPURenameListHasNoUnusedName(t *testing.T) {
	checkRenameListHasNoUnusedName(t, webGPUChunkName, webGPUPrivatePropertyPattern)
}

const webGPUChunkName = "bootstrap-feature-scene3d-webgpu.js"

func checkRenameListHasNoUnusedName(t *testing.T, chunkName, pattern string) {
	t.Helper()
	dir := shippedClientJS(t)
	for _, entry := range outputs {
		if entry.name != chunkName {
			continue
		}
		bodies := make([]string, len(entry.sources))
		for i, src := range entry.sources {
			data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(src.rel)))
			if err != nil {
				t.Fatal(err)
			}
			bodies[i] = normalizeNewlines(string(data))
		}
		code, err := transpileTypedChunk(entry, bodies)
		if err != nil {
			t.Fatal(err)
		}
		used, err := renamedProperties(entry.name, code, pattern)
		if err != nil {
			t.Fatal(err)
		}
		isUsed := map[string]bool{}
		for _, name := range used {
			isUsed[name] = true
		}
		for _, name := range privatePropertyNames(t, pattern) {
			if !isUsed[name] {
				t.Errorf("the rename list for %s names %q, but the chunk has no property of that name", chunkName, name)
			}
		}
		return
	}
	t.Fatalf("the chunk table has no %s", chunkName)
}

// TestWebGLRenameLeakIsReported proves the check on a chunk table small enough
// to read: the WebGL chunk owns a record with a renamed field, one chunk calls
// across the boundary by that name, and one chunk only mentions the word.
func TestWebGLRenameLeakIsReported(t *testing.T) {
	name := webGLPrivatePropertyNames(t)[0]
	f := newFixture(t)
	renderer := f.writeSource("renderer.js", "(function () {\n  var cache = {};\n  cache."+name+" = 1;\n  window.__renderer = function () { return cache."+name+"; };\n})();\n")
	shared := f.writeSource("shared.js", "(function () {\n  var kept = {};\n  kept."+name+" = 2;\n  window.__shared = kept;\n})();\n")
	caller := f.writeSource("caller.js", "(function () {\n  window.__caller = function (api) { return api."+name+"(); };\n})();\n")
	shorthand := f.writeSource("shorthand.js", "(function () {\n  var "+name+" = 3;\n  window.__shorthand = { "+name+" };\n})();\n")
	bystander := f.writeSource("bystander.js", "(function () {\n  // "+name+" is only a word here.\n  var "+name+" = \""+name+"\";\n  window.__bystander = function () { return "+name+"; };\n})();\n")
	entries := []output{
		chunk(webGLChunkName, renderer, shared),
		chunk("bootstrap.js", renderer, shared, bystander),
		chunk("bootstrap-feature-caller.js", caller),
		chunk("bootstrap-feature-shorthand.js", shorthand),
		chunk("bootstrap-feature-bystander.js", bystander),
	}
	leaks, err := webGLRenameLeaks(f.dir, entries)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(leaks))
	for _, leak := range leaks {
		got = append(got, leak.chunk+" "+leak.name+" "+strings.Join(leak.sources, ","))
	}
	sort.Strings(got)
	want := []string{
		"bootstrap-feature-caller.js " + name + " " + caller,
		"bootstrap-feature-shorthand.js " + name + " " + shorthand,
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("leaks:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
