package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCompactFunctionsIgnoreShaderStringsCommentsAndRegex(t *testing.T) {
	code := `const shader = "function fake(){} 😀"; const regex = /function\\s*\\(/; /* function comment(){} */ function outer(a) { function inner(b) {return b;} return inner(a); }`
	functions, err := compactFunctions(code)
	if err != nil {
		t.Fatal(err)
	}
	if len(functions) != 2 || functions[0].parent != -1 || functions[1].parent != 0 {
		t.Fatalf("function scopes: %+v", functions)
	}
	prefix := strings.Split(code, "function outer")[0]
	want := len([]rune(prefix)) + 1
	if functions[0].column != want {
		t.Fatalf("outer column %d, want %d", functions[0].column, want)
	}
	if functions[1].endColumn >= functions[0].endColumn {
		t.Fatal("inner scope extends beyond outer")
	}
}

func TestCompactWebGLMapRetainsFunctionOriginsAndParentScope(t *testing.T) {
	f := newFixture(t)
	head := f.writeSource("head.js", "(function () {\n")
	bodyText := "function outer(a) {\n  function inner(b) { return b + 1; }\n  return inner(a);\n}\n"
	body := f.writeSource("body.js", bodyText)
	tail := f.writeSource("tail.js", "window.gosxFixture = outer;\n})();\n")
	entry := chunk("bootstrap-feature-scene3d-webgl.js", head, body, tail)
	original, err := buildCompactedBundle(f.dir, entry)
	if err != nil {
		t.Fatal(err)
	}
	first, err := minifyESBuild(entry, original)
	if err != nil {
		t.Fatal(err)
	}
	final, err := minifyCompactBundle(entry, first)
	if err != nil {
		t.Fatal(err)
	}
	var m esbuildMap
	if err := json.Unmarshal([]byte(final.m), &m); err != nil {
		t.Fatal(err)
	}
	functions, err := compactFunctions(final.code)
	if err != nil {
		t.Fatal(err)
	}
	if len(functions) != 3 {
		t.Fatalf("functions: %+v", functions)
	}
	rows, err := compactMapRows(m.Mappings)
	if err != nil {
		t.Fatal(err)
	}
	originAt := func(line, column int) compactOrigin {
		t.Helper()
		origin := compactOrigin{source: -1}
		for _, entry := range rows[line] {
			if entry.column > column {
				break
			}
			origin = entry
		}
		return origin
	}
	for i, originalLine := range []int{0, 0, 1} {
		origin := originAt(functions[i].line, functions[i].column)
		wantSource := head
		if i > 0 {
			wantSource = body
		}
		if origin.source < 0 || m.Sources[origin.source] != wantSource || origin.line != originalLine {
			t.Fatalf("function %d origin %+v sources %v", i, origin, m.Sources)
		}
	}
	origin := originAt(functions[2].endLine, functions[2].endColumn)
	if origin.source < 0 || m.Sources[origin.source] != body || origin.line != 0 {
		t.Fatalf("parent not restored after inner: %+v", origin)
	}
	for i, source := range m.Sources {
		if source == body && m.SourcesContent[i] != bodyText {
			t.Fatal("original body lost")
		}
	}
	debug, err := buildBundle(f.dir, entry, "esbuild", true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(debug.code, "sourceMappingURL") || debug.m == final.m {
		t.Fatal("debug build lost full esbuild map")
	}
}

func TestCompactShaderSourcePreservesPatchSitesAndLineCount(t *testing.T) {
	source := "const SCENE_PBR_VERTEX_SOURCE = [\n  \"#version 300 es\",\n  \"    void main() {\",\n  \"    vec4 p = vec4(1.0, 0.0, 1.0, 1.0);\",\n  \"void gosxApplyCustomVertex(inout vec3 position, inout vec3 normal, inout vec2 uv) {}\",\n  \"}\",\n].join(\"\\n\");\nconst runtime = \"  keep whitespace\";\n"
	result := compactShaderIndentation(source)
	if strings.Count(result, "\n") != strings.Count(source, "\n") {
		t.Fatal("source map lines moved")
	}
	for _, site := range []string{"#version 300 es", "void main() {", "void gosxApplyCustomVertex(inout vec3 position, inout vec3 normal, inout vec2 uv) {}", `"  keep whitespace"`} {
		if !strings.Contains(result, site) {
			t.Fatalf("patch site changed: %s", site)
		}
	}
	if !strings.Contains(result, `"vec4 p=vec4(1.,0.,1.,1.);"`) {
		t.Fatalf("shader not compacted: %s", result)
	}
}
