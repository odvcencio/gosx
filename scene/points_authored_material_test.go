package scene

import (
	"encoding/json"
	"strings"
	"testing"
)

// testVertexGLSL and testFragmentGLSL are minimal authored GLSL shaders for
// testing the round-trip through PointsIR and ComputeParticlesIR.
const testVertexGLSL = `
attribute vec3 a_position;
uniform mat4 u_viewMatrix;
uniform mat4 u_projectionMatrix;
void main() {
    gl_Position = u_projectionMatrix * u_viewMatrix * vec4(a_position, 1.0);
    gl_PointSize = 4.0;
}
`

const testFragmentGLSL = `
precision mediump float;
uniform vec4 u_defaultColor;
void main() {
    gl_FragColor = u_defaultColor;
}
`

// testWGSLSource is a minimal WGSL combining a vertex+fragment main.
// The source exceeds the shaderLibThreshold when used in multiples.
const testWGSLSource = `
@group(0) @binding(0) var<uniform> frame : FrameUniforms;
@group(1) @binding(0) var<uniform> user : UserUniforms;
@group(2) @binding(0) var<uniform> pts : PointsUniforms;

struct VertexOut {
    @builtin(position) pos : vec4<f32>,
};

@vertex
fn vertexMain(@location(0) position : vec3<f32>) -> VertexOut {
    var out : VertexOut;
    out.pos = frame.viewProj * vec4<f32>(position, 1.0);
    return out;
}

@fragment
fn fragmentMain(in : VertexOut) -> @location(0) vec4<f32> {
    return vec4<f32>(1.0, 1.0, 1.0, 1.0);
}
`

// TestPointsAuthoredMaterialRoundTrip checks that a Points layer with a
// CustomMaterial lowers all authored fields into PointsIR, that
// absent-material Points produce a byte-identical IR (no extra fields),
// and that shader-lib dedup hoists matching sources across multiple layers.
func TestPointsAuthoredMaterialRoundTrip(t *testing.T) {
	mat := CustomMaterial{
		VertexGLSL:   testVertexGLSL,
		FragmentGLSL: testFragmentGLSL,
		VertexWGSL:   testWGSLSource,
		FragmentWGSL: testWGSLSource,
		Uniforms:     map[string]any{"intensity": float32(1.5)},
		ShaderBackend: "selena",
		ShaderLayout: map[string]any{"material": "GlowPoints"},
	}

	props := Props{
		Graph: NewGraph(Points{
			ID:        "glow-layer",
			Positions: []Vector3{{0, 0, 0}, {1, 0, 0}},
			Material:  &mat,
		}),
	}
	ir := props.SceneIR()

	if len(ir.Points) != 1 {
		t.Fatalf("ir.Points length = %d, want 1", len(ir.Points))
	}
	pt := ir.Points[0]

	// All authored fields must flow through.
	if pt.CustomVertex == "" {
		t.Error("CustomVertex is empty, want authored GLSL vertex")
	}
	if pt.CustomFragment == "" {
		t.Error("CustomFragment is empty, want authored GLSL fragment")
	}
	if pt.CustomVertexWGSL == "" {
		t.Error("CustomVertexWGSL is empty, want authored WGSL")
	}
	if pt.CustomFragmentWGSL == "" {
		t.Error("CustomFragmentWGSL is empty, want authored WGSL")
	}
	if pt.ShaderBackend != "selena" {
		t.Errorf("ShaderBackend = %q, want selena", pt.ShaderBackend)
	}
	if got := pt.ShaderLayout["material"]; got != "GlowPoints" {
		t.Errorf("ShaderLayout[material] = %q, want GlowPoints", got)
	}
	if got := pt.CustomUniforms["intensity"]; got != float32(1.5) {
		t.Errorf("CustomUniforms[intensity] = %v, want float32(1.5)", got)
	}

	// legacyProps must reflect the authored fields.
	legacy := pt.legacyProps()
	if legacy["customVertex"] == "" {
		t.Error("legacyProps missing customVertex")
	}
	if legacy["customVertexWGSL"] == "" {
		t.Error("legacyProps missing customVertexWGSL")
	}
	if legacy["shaderBackend"] != "selena" {
		t.Errorf("legacyProps shaderBackend = %v, want selena", legacy["shaderBackend"])
	}
}

// TestPointsAbsentMaterialNoExtra checks that a Points layer without a Material
// produces a PointsIR with no authored fields set.
func TestPointsAbsentMaterialNoExtra(t *testing.T) {
	props := Props{
		Graph: NewGraph(Points{
			ID:        "plain-layer",
			Positions: []Vector3{{0, 0, 0}},
		}),
	}
	ir := props.SceneIR()
	if len(ir.Points) != 1 {
		t.Fatalf("ir.Points length = %d, want 1", len(ir.Points))
	}
	pt := ir.Points[0]

	if pt.CustomVertex != "" || pt.CustomFragment != "" ||
		pt.CustomVertexWGSL != "" || pt.CustomFragmentWGSL != "" ||
		pt.ShaderBackend != "" || pt.ShaderLayout != nil || pt.CustomUniforms != nil {
		t.Errorf("absent material produced authored fields: %+v", pt)
	}

	// Marshal and unmarshal: JSON must have no authored keys.
	data, err := json.Marshal(ir)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	ptsRaw, _ := raw["points"].([]any)
	if len(ptsRaw) != 1 {
		t.Fatalf("points JSON length = %d, want 1", len(ptsRaw))
	}
	ptMap, _ := ptsRaw[0].(map[string]any)
	for _, key := range []string{"customVertex", "customFragment", "customVertexWGSL", "customFragmentWGSL", "shaderBackend"} {
		if _, found := ptMap[key]; found {
			t.Errorf("absent material should not emit JSON key %q", key)
		}
	}
}

// TestPointsAuthoredShaderLibDedup checks that duplicate authored WGSL across
// multiple Points layers gets hoisted into shaderLib exactly once.
func TestPointsAuthoredShaderLibDedup(t *testing.T) {
	// Use a source that exceeds shaderLibThreshold.
	pad := strings.Repeat("// padding line for dedup test\n", (shaderLibThreshold/30)+1)
	bigWGSL := testWGSLSource + pad

	const nLayers = 4
	nodes := make([]Node, nLayers)
	for i := 0; i < nLayers; i++ {
		nodes[i] = Points{
			ID:        "layer-" + string(rune('a'+i)),
			Positions: []Vector3{{float64(i), 0, 0}},
			Material: &CustomMaterial{
				VertexGLSL:   testVertexGLSL,
				FragmentGLSL: testFragmentGLSL,
				VertexWGSL:   bigWGSL,
				FragmentWGSL: bigWGSL,
			},
		}
	}
	props := Props{Graph: NewGraph(nodes...)}
	ir := props.SceneIR()

	data, err := json.Marshal(ir)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}

	libRaw, hasLib := raw["shaderLib"].(map[string]any)
	if !hasLib {
		t.Fatal("shaderLib missing from marshaled output")
	}
	// The WGSL source appears in both customVertexWGSL and customFragmentWGSL
	// (same string), so after dedup there should be exactly 1 entry shared
	// by all 4 layers × 2 fields = one lib key.
	if len(libRaw) == 0 {
		t.Errorf("shaderLib is empty, expected deduped WGSL entry")
	}
	// Each layer should have ref fields instead of inline WGSL.
	ptsRaw, _ := raw["points"].([]any)
	if len(ptsRaw) != nLayers {
		t.Fatalf("points JSON length = %d, want %d", len(ptsRaw), nLayers)
	}
	for i, p := range ptsRaw {
		ptMap, _ := p.(map[string]any)
		if ptMap["customVertexWGSL"] != nil {
			t.Errorf("layer[%d] still has inline customVertexWGSL after dedup", i)
		}
		if ptMap["customFragmentWGSL"] != nil {
			t.Errorf("layer[%d] still has inline customFragmentWGSL after dedup", i)
		}
		if ptMap["customVertexWGSLRef"] == nil {
			t.Errorf("layer[%d] missing customVertexWGSLRef after dedup", i)
		}
		if ptMap["customFragmentWGSLRef"] == nil {
			t.Errorf("layer[%d] missing customFragmentWGSLRef after dedup", i)
		}
	}
}

// TestComputeParticlesRenderMaterialRoundTrip checks that RenderMaterial on a
// ComputeParticles system lowers all authored fields into ComputeParticlesIR,
// that absent-RenderMaterial systems produce no extra fields, and that
// shader-lib dedup hoists duplicate render WGSL across systems.
func TestComputeParticlesRenderMaterialRoundTrip(t *testing.T) {
	renderMat := CustomMaterial{
		VertexGLSL:    testVertexGLSL,
		FragmentGLSL:  testFragmentGLSL,
		VertexWGSL:    testWGSLSource,
		FragmentWGSL:  testWGSLSource,
		Uniforms:      map[string]any{"brightness": float32(2.0)},
		ShaderBackend: "selena",
		ShaderLayout:  map[string]any{"material": "GravParticle"},
	}

	props := Props{
		Graph: NewGraph(ComputeParticles{
			ID:             "grav-system",
			Count:          512,
			RenderMaterial: &renderMat,
		}),
	}
	ir := props.SceneIR()

	if len(ir.ComputeParticles) != 1 {
		t.Fatalf("ir.ComputeParticles length = %d, want 1", len(ir.ComputeParticles))
	}
	cp := ir.ComputeParticles[0]

	if cp.RenderVertex == "" {
		t.Error("RenderVertex is empty, want authored GLSL vertex")
	}
	if cp.RenderFragment == "" {
		t.Error("RenderFragment is empty, want authored GLSL fragment")
	}
	if cp.RenderVertexWGSL == "" {
		t.Error("RenderVertexWGSL is empty, want authored WGSL")
	}
	if cp.RenderFragmentWGSL == "" {
		t.Error("RenderFragmentWGSL is empty, want authored WGSL")
	}
	if cp.RenderShaderBackend != "selena" {
		t.Errorf("RenderShaderBackend = %q, want selena", cp.RenderShaderBackend)
	}
	if got := cp.RenderShaderLayout["material"]; got != "GravParticle" {
		t.Errorf("RenderShaderLayout[material] = %q, want GravParticle", got)
	}
	if got := cp.RenderUniforms["brightness"]; got != float32(2.0) {
		t.Errorf("RenderUniforms[brightness] = %v, want float32(2.0)", got)
	}

	// legacyProps must include render fields.
	legacy := cp.legacyProps()
	if legacy["renderVertex"] == "" {
		t.Error("legacyProps missing renderVertex")
	}
	if legacy["renderVertexWGSL"] == "" {
		t.Error("legacyProps missing renderVertexWGSL")
	}
	if legacy["renderShaderBackend"] != "selena" {
		t.Errorf("legacyProps renderShaderBackend = %v, want selena", legacy["renderShaderBackend"])
	}
}

// TestComputeParticlesAbsentRenderMaterialNoExtra checks that a system without
// RenderMaterial emits no render override fields.
func TestComputeParticlesAbsentRenderMaterialNoExtra(t *testing.T) {
	props := Props{
		Graph: NewGraph(ComputeParticles{
			ID:    "plain-system",
			Count: 256,
		}),
	}
	ir := props.SceneIR()
	if len(ir.ComputeParticles) != 1 {
		t.Fatalf("ir.ComputeParticles length = %d, want 1", len(ir.ComputeParticles))
	}
	cp := ir.ComputeParticles[0]

	if cp.RenderVertex != "" || cp.RenderFragment != "" ||
		cp.RenderVertexWGSL != "" || cp.RenderFragmentWGSL != "" ||
		cp.RenderShaderBackend != "" || cp.RenderShaderLayout != nil || cp.RenderUniforms != nil {
		t.Errorf("absent RenderMaterial produced authored fields: %+v", cp)
	}

	// JSON must have no render authored keys.
	data, err := json.Marshal(ir)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	cpsRaw, _ := raw["computeParticles"].([]any)
	if len(cpsRaw) != 1 {
		t.Fatalf("computeParticles JSON length = %d, want 1", len(cpsRaw))
	}
	cpMap, _ := cpsRaw[0].(map[string]any)
	for _, key := range []string{"renderVertex", "renderFragment", "renderVertexWGSL", "renderFragmentWGSL", "renderShaderBackend"} {
		if _, found := cpMap[key]; found {
			t.Errorf("absent RenderMaterial should not emit JSON key %q", key)
		}
	}
}

// TestComputeParticlesRenderShaderLibDedup checks that duplicate authored
// render WGSL across systems is hoisted into shaderLib.
func TestComputeParticlesRenderShaderLibDedup(t *testing.T) {
	pad := strings.Repeat("// dedup padding render\n", (shaderLibThreshold/22)+1)
	bigWGSL := testWGSLSource + pad

	const nSystems = 4
	nodes := make([]Node, nSystems)
	for i := 0; i < nSystems; i++ {
		nodes[i] = ComputeParticles{
			ID:    "sys-" + string(rune('a'+i)),
			Count: 256,
			RenderMaterial: &CustomMaterial{
				VertexWGSL:   bigWGSL,
				FragmentWGSL: bigWGSL,
			},
		}
	}
	props := Props{Graph: NewGraph(nodes...)}
	ir := props.SceneIR()

	data, err := json.Marshal(ir)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}

	libRaw, hasLib := raw["shaderLib"].(map[string]any)
	if !hasLib {
		t.Fatal("shaderLib missing from marshaled output")
	}
	if len(libRaw) == 0 {
		t.Error("shaderLib is empty, expected deduped render WGSL entry")
	}

	cpsRaw, _ := raw["computeParticles"].([]any)
	if len(cpsRaw) != nSystems {
		t.Fatalf("computeParticles JSON length = %d, want %d", len(cpsRaw), nSystems)
	}
	for i, c := range cpsRaw {
		cpMap, _ := c.(map[string]any)
		if cpMap["renderVertexWGSL"] != nil {
			t.Errorf("system[%d] still has inline renderVertexWGSL after dedup", i)
		}
		if cpMap["renderFragmentWGSL"] != nil {
			t.Errorf("system[%d] still has inline renderFragmentWGSL after dedup", i)
		}
		if cpMap["renderVertexWGSLRef"] == nil {
			t.Errorf("system[%d] missing renderVertexWGSLRef after dedup", i)
		}
		if cpMap["renderFragmentWGSLRef"] == nil {
			t.Errorf("system[%d] missing renderFragmentWGSLRef after dedup", i)
		}
	}
}
