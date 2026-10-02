package main

import (
	"sort"
	"strings"
	"testing"
)

func applyShaderEdits(code string, edits []shaderEdit) string {
	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	for _, edit := range edits {
		code = code[:edit.start] + edit.text + code[edit.end:]
	}
	return code
}

func TestShaderLocalsPreserveInterfacesNumbersAndSwizzles(t *testing.T) {
	source := `uniform float globalValue;
vec4 sampleValue;
float shade(float inputValue) {
 float exponent = 1e-12, other = 2.5e+3;
 vec3 color = vec3(inputValue, exponent, other);
 // color.exponent is documentation, not code.
 return color.r + globalValue + sampleValue.a;
}`
	packed := applyShaderEdits(source, shaderLocalEdits(source, nil))
	if packed == source || len(packed) >= len(source) {
		t.Fatalf("shader did not shrink: %s", packed)
	}
	for _, text := range []string{"uniform float globalValue;", "vec4 sampleValue;", "float shade(", "1e-12", "2.5e+3", ".r", "globalValue", "sampleValue.a", "// color.exponent is documentation, not code."} {
		if !strings.Contains(packed, text) {
			t.Errorf("lost %q: %s", text, packed)
		}
	}
	if strings.Contains(packed, "float inputValue") || strings.Contains(packed, "float exponent") || strings.Contains(packed, ", other =") {
		t.Errorf("locals were not renamed: %s", packed)
	}
	if strings.Count(source, "\n") != strings.Count(packed, "\n") {
		t.Fatal("line map changed")
	}
}

func TestShaderLocalsRetainGlobalsAndPinnedHookNames(t *testing.T) {
	source := `float depth;
float first(float parameter) { float localValue = parameter; return localValue + depth; }
float second(float parameter) { float depth = parameter; return depth; }
void main() { vec3 color = vec3(first(1.)); color = color.rgb; }`
	packed := applyShaderEdits(source, shaderLocalEdits(source, map[string]bool{"color": true}))
	for _, text := range []string{"float depth;", "first(", "second(", "return depth;", "vec3 color =", "color.rgb"} {
		if !strings.Contains(packed, text) {
			t.Errorf("lost %q: %s", text, packed)
		}
	}
}

func TestBuiltinShaderPackingRetainsJavaScriptAndReplacementContracts(t *testing.T) {
	source := "const userShader = `float arbitrary(float userValue) { return userValue; }`;\n" +
		"const SCENE_TEST_SOURCE = [\"uniform vec3 u_color;\", \"void main() {\", \"float privateValue = 1e-6;\", \"float protectedValue = privateValue;\", \"}\"].join(\"\\n\");\n" +
		"const SCENE_TEMPLATE_SOURCE = `uniform float u_test;\n${shaderHeader}\nfloat shade(float incoming) { float privateValue = incoming; return privateValue; }`;\n" +
		"SCENE_TEST_SOURCE.replace(\"protectedValue\", \"runtimeHook\");\n"
	packed := packBuiltinGLSL(source)
	if packed == source {
		t.Fatal("built-in shader was not packed")
	}
	for _, text := range []string{"const userShader = `float arbitrary(float userValue) { return userValue; }`;", "uniform vec3 u_color;", "float protectedValue", "1e-6", "${shaderHeader}", "replace(\"protectedValue\", \"runtimeHook\")"} {
		if !strings.Contains(packed, text) {
			t.Errorf("lost %q: %s", text, packed)
		}
	}
	if strings.Count(source, "\n") != strings.Count(packed, "\n") {
		t.Fatal("line map changed")
	}
}

func TestCompactionPreservesTemplateShaderMarkersAndWhitespace(t *testing.T) {
	source := "// outside comment\nconst shader = `#version 300 es\n//GOSX_SKY_PHYSICAL\n\n\n  vec3 color;   \n`;\n// outside comment\n"
	got := compactSource(source)
	want := "const shader = `#version 300 es\n//GOSX_SKY_PHYSICAL\n\n\n  vec3 color;   \n`;\n"
	if got.code != want {
		t.Fatalf("literal changed: %q, want %q", got.code, want)
	}
	for i, line := range got.lineMap {
		if line != i+1 {
			t.Fatalf("line %d maps to %d", i, line)
		}
	}
}
