package main

import (
	"strings"
	"testing"
)

func TestCompactShaderSourcePreservesPatchSitesAndLineCount(t *testing.T) {
	source := "const SCENE_POST_VERTEX_SOURCE = [\n  \"#version 300 es\",\n  \"    void main() {\",\n  \"    vec4 p = vec4(1.0, 0.0, 1.0, 1.0);\",\n  \"    float ratio = 1.0 / 2.0;\",\n  \"//GOSX_SKY_PHYSICAL\",\n  \"void gosxApplyCustomVertex(inout vec3 position, inout vec3 normal, inout vec2 uv) {}\",\n  \"}\",\n].join(\"\\n\");\nconst runtime = \"  keep whitespace\";\n"
	result := compactShaderIndentation(source)
	if strings.Count(result, "\n") != strings.Count(source, "\n") {
		t.Fatal("source map lines moved")
	}
	for _, site := range []string{"#version 300 es", "void main() {", "//GOSX_SKY_PHYSICAL", "void gosxApplyCustomVertex(inout vec3 position, inout vec3 normal, inout vec2 uv) {}", `"  keep whitespace"`} {
		if !strings.Contains(result, site) {
			t.Fatalf("patch site changed: %s", site)
		}
	}
	if !strings.Contains(result, `"vec4 p=vec4(1.,0.,1.,1.);"`) {
		t.Fatalf("shader not compacted: %s", result)
	}
	if !strings.Contains(result, `"float ratio=1./2.;"`) {
		t.Fatalf("shader division spacing not compacted: %s", result)
	}
}

func TestCompactShaderSourceKeepsDetailInjectionAnchor(t *testing.T) {
	source := "const SCENE_PBR_FRAGMENT_SOURCE = [\n  \"    vec3 V = normalize(u_cameraPosition - v_worldPosition);\",\n].join(\"\\n\");\n"
	if got := compactShaderIndentation(source); got != source {
		t.Fatal("detail injection anchor changed")
	}
}
