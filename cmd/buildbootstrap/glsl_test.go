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

func TestBuiltinShaderPackingPreservesIndentedReplacementTargets(t *testing.T) {
	source := "const SCENE_TEST_SOURCE = [\"void main() {\", \"    vec3 V = normalize(u_cameraPosition - v_worldPosition);\", \"    float payloadValue = V.x;\", \"    float localValue = payloadValue;\", \"}\"].join(\"\\n\");\n" +
		"SCENE_TEST_SOURCE.replace(\"    vec3 V = normalize(u_cameraPosition - v_worldPosition);\", `detailApply(payloadValue);`);\n"
	packed := packBuiltinGLSL(source)
	if !strings.Contains(packed, "\"    vec3 V = normalize(u_cameraPosition - v_worldPosition);") {
		t.Fatalf("indented shader hook changed: %s", packed)
	}
	if !strings.Contains(packed, "float payloadValue") {
		t.Fatal("replacement expression lost its source binding")
	}
	if strings.Contains(packed, "float localValue") {
		t.Fatal("unprotected shader locals were not packed")
	}
}

func TestWGSLPackingKeepsInterfacesHooksAndJavaScript(t *testing.T) {
	source := "const userShader = `fn shade(userValue: f32) -> f32 { return userValue; }`;\n" +
		"const WGSL_PBR_FRAGMENT = [\"struct Input { position: vec4f, };\", \"@fragment fn main(vertexInput: Input) -> @location(0) vec4f {\", \"    let _a = 1.0;\", \"    let scaledPosition = vertexInput.position * _a;\", \"    let protectedValue = scaledPosition;\", \"    return protectedValue;\", \"}\"].join(\"\\n\");\n" +
		"WGSL_PBR_FRAGMENT.replace(\"    let protectedValue = scaledPosition;\", \"    let protectedValue = scaledPosition * 2.0;\");\n" +
		"const SCENE_WATER_TEST_SOURCE = [\"fn shade(inputValue: f32) -> f32 {\", \"    var localValue = inputValue + 1e-6;\", \"    return localValue;\", \"}\"].join(\"\\n\");\n"
	packed := packBuiltinWGSL(source)
	for _, text := range []string{
		"const userShader = `fn shade(userValue: f32) -> f32 { return userValue; }`;",
		"struct Input { position: vec4f, };", "@fragment fn main(", "@location(0) vec4f", ".position * _a", "let _a = 1.0;",
		"    let protectedValue = scaledPosition;", "    let protectedValue = scaledPosition * 2.0;", "1e-6",
	} {
		if !strings.Contains(packed, text) {
			t.Errorf("lost %q: %s", text, packed)
		}
	}
	for _, text := range []string{"vertexInput", "inputValue", "localValue"} {
		if strings.Contains(packed, text) {
			t.Errorf("local %s was not packed: %s", text, packed)
		}
	}
	if strings.Count(packed, "\n") != strings.Count(source, "\n") {
		t.Fatal("line map changed")
	}
}

func TestWGSLPackingPreservesAttributeLabelsThatMatchParameters(t *testing.T) {
	source := "const WGSL_SHADOW_VERTEX = `@vertex fn vertexMain(@location(0) position: vec3f) -> @builtin(position) vec4f { return vec4f(position, 1.0); }`;\n" +
		"const WGSL_FRAGMENT = `@fragment fn fragmentMain(@builtin(front_facing) front_facing: bool, @location(0) @interpolate(flat) flat: f32) -> @location(0) vec4f { return select(vec4f(flat), vec4f(1.0), front_facing); }`;\n"
	packed := packBuiltinWGSL(source)
	for _, label := range []string{"@builtin(position)", "@builtin(front_facing)", "@interpolate(flat)", "@location(0)"} {
		if !strings.Contains(packed, label) {
			t.Errorf("interface attribute %q changed: %s", label, packed)
		}
	}
	for _, parameter := range []string{"position: vec3f", "front_facing: bool", "flat: f32", "vec4f(position", "vec4f(flat"} {
		if strings.Contains(packed, parameter) {
			t.Errorf("function binding %q was not packed: %s", parameter, packed)
		}
	}
}
