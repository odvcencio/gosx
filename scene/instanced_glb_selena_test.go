package scene

import (
	"encoding/json"
	"strings"
	"testing"
)

func instancedGLBSelenaMaterial() CustomMaterial {
	pad := strings.Repeat("// selena instanced skin shader padding\n", (shaderLibThreshold/39)+2)
	return CustomMaterial{
		StandardMaterial: StandardMaterial{Color: "#9bdcff", Texture: "/hero-albedo.png"},
		ShaderBackend:    "selena",
		ShaderLayout: map[string]any{
			"material": "HeroGraphic",
			"uniformBlock": map[string]any{"fields": []any{
				map[string]any{"name": "mvp", "type": "mat4", "offset": float64(0), "size": float64(64)},
			}},
		},
		ShaderSource:      "material HeroGraphic",
		ShaderSourceFiles: map[string]string{"hero.sel": "material HeroGraphic"},
		VertexGLSL:        pad + "attribute vec3 position; void main(){gl_Position=vec4(position,1.0);}",
		FragmentGLSL:      pad + "precision mediump float; void main(){gl_FragColor=vec4(1.0);}",
		VertexWGSL:        pad + "@vertex fn vertexMain() -> @builtin(position) vec4<f32>{return vec4<f32>();}",
		FragmentWGSL:      pad + "@fragment fn fragmentMain() -> @location(0) vec4<f32>{return vec4<f32>(1.0);}",
		Uniforms:          map[string]any{"albedo": "/hero-albedo.png", "threshold": 0.1},
	}
}

func TestInstancedGLBMeshPreservesSelenaMaterial(t *testing.T) {
	material := instancedGLBSelenaMaterial()
	ir := (Props{Graph: NewGraph(InstancedGLBMesh{
		ID: "heroes", Src: "/hero.glb", Material: material,
		Instances: []MeshInstance{{ID: "vesper", Animation: "Idle", AnimationLoop: true}},
	})}).SceneIR()
	if len(ir.InstancedGLBMeshes) != 1 {
		t.Fatalf("instanced GLB batches = %d, want 1", len(ir.InstancedGLBMeshes))
	}
	batch := ir.InstancedGLBMeshes[0]
	if batch.MaterialKind != "custom" || batch.ShaderBackend != "selena" {
		t.Fatalf("material identity lost: kind=%q backend=%q", batch.MaterialKind, batch.ShaderBackend)
	}
	if batch.CustomVertex != material.VertexGLSL || batch.CustomFragment != material.FragmentGLSL ||
		batch.CustomVertexWGSL != material.VertexWGSL || batch.CustomFragmentWGSL != material.FragmentWGSL {
		t.Fatal("compiled Selena shader sources were not preserved by typed lowering")
	}
	if batch.CustomUniforms["albedo"] != "/hero-albedo.png" || batch.ShaderLayout["material"] != "HeroGraphic" ||
		batch.ShaderSourceFiles["hero.sel"] != "material HeroGraphic" {
		t.Fatalf("compiled Selena metadata lost: uniforms=%#v layout=%#v sourceFiles=%#v", batch.CustomUniforms, batch.ShaderLayout, batch.ShaderSourceFiles)
	}

	direct, err := json.Marshal(batch)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"customVertex", "customFragment", "customVertexWGSL", "customFragmentWGSL", "customUniforms", "shaderBackend", "shaderLayout", "shaderSourceFiles"} {
		if !strings.Contains(string(direct), `"`+field+`"`) {
			t.Errorf("direct typed JSON omitted %s", field)
		}
	}

	legacy := batch.legacyProps()
	if legacy["shaderBackend"] != "selena" || legacy["customVertex"] != material.VertexGLSL || legacy["customFragmentWGSL"] != material.FragmentWGSL {
		t.Fatalf("legacy serialization lost Selena payload: %#v", legacy)
	}
}

func TestInstancedGLBSelenaMaterialMarshalHoistsAndInflates(t *testing.T) {
	material := instancedGLBSelenaMaterial()
	base := (Props{Graph: NewGraph(InstancedGLBMesh{
		ID: "heroes-a", Src: "/hero.glb", Material: material, Instances: []MeshInstance{{ID: "a"}},
	})}).SceneIR().InstancedGLBMeshes[0]
	second := base
	second.ID = "heroes-b"
	ir := SceneIR{Schema: SceneIRSchema, InstancedGLBMeshes: []InstancedGLBMeshIR{base, second}}

	wire, err := json.Marshal(ir)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(wire, &raw); err != nil {
		t.Fatal(err)
	}
	if len(raw["shaderLib"].(map[string]any)) != 4 {
		t.Fatalf("shaderLib = %#v, want four deduplicated Selena stages", raw["shaderLib"])
	}
	first := raw["instancedGLBMeshes"].([]any)[0].(map[string]any)
	for _, ref := range []string{"customVertexRef", "customFragmentRef", "customVertexWGSLRef", "customFragmentWGSLRef"} {
		if first[ref] == "" || first[ref] == nil {
			t.Errorf("marshaled batch omitted %s", ref)
		}
	}
	if _, ok := first["customVertex"]; ok {
		t.Error("marshaled batch retained hoisted customVertex inline")
	}

	var decoded SceneIR
	if err := json.Unmarshal(wire, &decoded); err != nil {
		t.Fatal(err)
	}
	got := decoded.InstancedGLBMeshes[0]
	if got.CustomVertex != material.VertexGLSL || got.CustomFragmentWGSL != material.FragmentWGSL || got.ShaderBackend != "selena" {
		t.Fatal("SceneIR unmarshal did not inflate the complete Selena material")
	}
	if base.CustomVertex == "" || base.CustomVertexRef != "" {
		t.Fatal("MarshalJSON mutated the caller's instanced GLB batch")
	}
}

func TestInstancedGLBSelenaMaterialDiffPayload(t *testing.T) {
	material := instancedGLBSelenaMaterial()
	batch := (Props{Graph: NewGraph(InstancedGLBMesh{
		ID: "heroes", Src: "/hero.glb", Material: material, Instances: []MeshInstance{{ID: "vesper"}},
	})}).SceneIR().InstancedGLBMeshes[0]
	commands := DiffCommands(SceneIR{}, SceneIR{InstancedGLBMeshes: []InstancedGLBMeshIR{batch}})
	if len(commands) != 1 || commands[0].Kind != CommandSetInstancedGLBMeshes {
		t.Fatalf("diff commands = %#v", commands)
	}
	payload := commandPayloadMap(t, commands[0])
	record := payload["instancedGLBMeshes"].([]any)[0].(map[string]any)
	if record["shaderBackend"] != "selena" || record["customVertex"] != material.VertexGLSL || record["customFragmentWGSL"] != material.FragmentWGSL {
		t.Fatalf("diff payload lost Selena material: %#v", record)
	}
}

func TestInstancedGLBSelenaMaterialMapShaderLibRoundTrip(t *testing.T) {
	source := strings.Repeat("// compiled selena stage\n", (shaderLibThreshold/25)+2)
	scene := map[string]any{
		"instancedGLBMeshes": []any{
			map[string]any{"id": "a", "customVertex": source},
			map[string]any{"id": "b", "customVertex": source},
		},
	}
	ApplyShaderLib(scene)
	if _, ok := scene["shaderLib"]; !ok {
		t.Fatal("map-side shader library did not hoist repeated instanced GLB shader")
	}
	for index, raw := range scene["instancedGLBMeshes"].([]any) {
		record := raw.(map[string]any)
		if record["customVertexRef"] == nil {
			t.Errorf("batch %d missing customVertexRef", index)
		}
		if _, ok := record["customVertex"]; ok {
			t.Errorf("batch %d retained hoisted customVertex", index)
		}
	}
	inflateShaderLib(scene)
	for index, raw := range scene["instancedGLBMeshes"].([]any) {
		record := raw.(map[string]any)
		if record["customVertex"] != source {
			t.Errorf("batch %d did not inflate customVertex", index)
		}
	}
}
