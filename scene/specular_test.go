package scene

import (
	"encoding/json"
	"strings"
	"testing"
)

func specColor(v [3]float64) *[3]float64 { return &v }

// TestStandardMaterialSpecularOptionalVersusZero pins the absent-vs-explicit
// contract: unset fields emit no keys anywhere, while explicit zero intensity
// and explicit black color survive as present values.
func TestStandardMaterialSpecularOptionalVersusZero(t *testing.T) {
	legacy := (StandardMaterial{}).legacyMaterial()
	if _, ok := legacy["specularIntensity"]; ok {
		t.Fatal("absent StandardMaterial specularIntensity must not emit a legacy key")
	}
	if _, ok := legacy["specularColor"]; ok {
		t.Fatal("absent StandardMaterial specularColor must not emit a legacy key")
	}
	if got := (StandardMaterial{SpecularIntensity: Float(0)}).legacyMaterial()["specularIntensity"]; got != float64(0) {
		t.Fatalf("explicit zero specularIntensity lowered to %v, want 0", got)
	}
	black := (StandardMaterial{SpecularColor: specColor([3]float64{0, 0, 0})}).legacyMaterial()["specularColor"]
	if c, ok := black.([]float64); !ok || c[0] != 0 || c[1] != 0 || c[2] != 0 {
		t.Fatalf("explicit black specularColor lowered to %v, want present [0 0 0]", black)
	}

	data, err := json.Marshal(ObjectIR{ID: "a", SpecularIntensity: Float(0), SpecularColor: specColor([3]float64{0, 0, 0})})
	if err != nil {
		t.Fatalf("marshal ObjectIR: %v", err)
	}
	if !strings.Contains(string(data), `"specularIntensity":0`) || !strings.Contains(string(data), `"specularColor":[0,0,0]`) {
		t.Fatalf("explicit zero values lost in JSON: %s", data)
	}
	absent, err := json.Marshal(ObjectIR{ID: "a"})
	if err != nil {
		t.Fatalf("marshal ObjectIR: %v", err)
	}
	if strings.Contains(string(absent), "specular") {
		t.Fatalf("absent specular fields emitted JSON keys: %s", absent)
	}
}

// TestSpecularTypedGraphLowering exercises Props{Graph: ...}.SceneIR() for all
// four record kinds so a missed lowerer cannot hide.
func TestSpecularTypedGraphLowering(t *testing.T) {
	build := func(intensity *float64, color *[3]float64) SceneIR {
		mat := StandardMaterial{Color: "#ffffff", SpecularIntensity: intensity, SpecularColor: color}
		graph := NewGraph(
			Mesh{ID: "spec-mesh", Geometry: CubeGeometry{Size: 1}, Material: mat},
			Model{ID: "spec-model", Src: "/m.glb", Material: mat},
			InstancedMesh{ID: "spec-inst", Count: 1, Geometry: CubeGeometry{Size: 1}, Material: mat, Positions: []Vector3{{X: 0, Y: 0, Z: 0}}},
			InstancedGLBMesh{ID: "spec-glb", Src: "/g.glb", Material: mat, Instances: []MeshInstance{{ID: "i0"}}},
		)
		return Props{Graph: graph}.SceneIR()
	}
	hdr := specColor([3]float64{2.5, 1, 0.5})
	check := func(ir SceneIR, wantI *float64, wantC *[3]float64) {
		t.Helper()
		if len(ir.Objects) != 1 || len(ir.Models) != 1 || len(ir.InstancedMeshes) != 1 || len(ir.InstancedGLBMeshes) != 1 {
			t.Fatalf("typed lowering lost records: %d/%d/%d/%d", len(ir.Objects), len(ir.Models), len(ir.InstancedMeshes), len(ir.InstancedGLBMeshes))
		}
		for _, got := range []*float64{ir.Objects[0].SpecularIntensity, ir.Models[0].SpecularIntensity, ir.InstancedMeshes[0].SpecularIntensity, ir.InstancedGLBMeshes[0].SpecularIntensity} {
			if (got == nil) != (wantI == nil) || (got != nil && *got != *wantI) {
				t.Fatalf("typed lowering SpecularIntensity = %v, want %v", got, wantI)
			}
		}
		for _, got := range []*[3]float64{ir.Objects[0].SpecularColor, ir.Models[0].SpecularColor, ir.InstancedMeshes[0].SpecularColor, ir.InstancedGLBMeshes[0].SpecularColor} {
			if (got == nil) != (wantC == nil) || (got != nil && *got != *wantC) {
				t.Fatalf("typed lowering SpecularColor = %v, want %v", got, wantC)
			}
		}
	}
	check(build(nil, nil), nil, nil)
	check(build(Float(0), specColor([3]float64{0, 0, 0})), Float(0), specColor([3]float64{0, 0, 0}))
	check(build(Float(1), hdr), Float(1), hdr)
	check(build(Float(1), specColor([3]float64{1, 1, 1})), Float(1), specColor([3]float64{1, 1, 1}))
}

// TestSpecularTypedAndLegacyLowering covers the direct typed and legacy map
// paths, accepted array shapes, snapshot behavior, and invalid-shape handling.
func TestSpecularTypedAndLegacyLowering(t *testing.T) {
	hdr := [3]float64{2.5, 1, 0.5}
	var typed ObjectIR
	applyMaterialToObjectIR(&typed, StandardMaterial{SpecularIntensity: Float(0.5), SpecularColor: specColor(hdr)})
	if typed.SpecularIntensity == nil || *typed.SpecularIntensity != 0.5 {
		t.Fatalf("typed standard specularIntensity = %v, want 0.5", typed.SpecularIntensity)
	}
	if typed.SpecularColor == nil || *typed.SpecularColor != hdr {
		t.Fatalf("typed standard specularColor = %v, want %v", typed.SpecularColor, hdr)
	}
	var absent ObjectIR
	applyMaterialToObjectIR(&absent, StandardMaterial{})
	if absent.SpecularIntensity != nil || absent.SpecularColor != nil {
		t.Fatalf("absent StandardMaterial lowered to %v/%v, want nil/nil", absent.SpecularIntensity, absent.SpecularColor)
	}

	// Snapshot: mutating the authored slice after lowering must not change
	// the lowered record.
	authored := []float64{1, 0.5, 0.25}
	var snap ObjectIR
	applyMaterialProps(&snap, map[string]any{"specularColor": authored})
	authored[0] = 99
	if snap.SpecularColor == nil || snap.SpecularColor[0] != 1 {
		t.Fatalf("lowered specularColor aliased the authored slice: %v", snap.SpecularColor)
	}

	cases := []struct {
		name string
		in   any
		want *[3]float64
	}{
		{"floatSlice", []float64{1, 0.5, 0.25}, &[3]float64{1, 0.5, 0.25}},
		{"fixedArray", [3]float64{0.1, 0.2, 0.3}, &[3]float64{0.1, 0.2, 0.3}},
		{"anySlice", []any{float64(2), float64(1), float64(0)}, &[3]float64{2, 1, 0}},
		{"shortSlice", []float64{1, 2}, nil},
		{"stringColor", "red", nil},
		{"badElement", []any{float64(1), "x", float64(3)}, nil},
		{"nil", nil, nil},
	}
	for _, tc := range cases {
		var rec ObjectIR
		applyMaterialProps(&rec, map[string]any{"specularColor": tc.in})
		if (rec.SpecularColor == nil) != (tc.want == nil) || (rec.SpecularColor != nil && *rec.SpecularColor != *tc.want) {
			t.Fatalf("%s: specularColor = %v, want %v", tc.name, rec.SpecularColor, tc.want)
		}
	}

	var inst ObjectIR
	applyMaterialProps(&inst, map[string]any{"specularIntensity": float64(0)})
	if inst.SpecularIntensity == nil || *inst.SpecularIntensity != 0 {
		t.Fatalf("legacy map zero specularIntensity = %v, want present 0", inst.SpecularIntensity)
	}

	glbRec := InstancedGLBMeshIR{ID: "g", Src: "/m.glb", SpecularIntensity: Float(0.75), SpecularColor: specColor(hdr)}
	props := glbRec.legacyProps()
	if v, ok := props["specularIntensity"].(float64); !ok || v != 0.75 {
		t.Fatalf("GLB legacy specularIntensity = %v, want 0.75", props["specularIntensity"])
	}
	if c, ok := props["specularColor"].([]float64); !ok || len(c) != 3 || c[0] != hdr[0] || c[1] != hdr[1] || c[2] != hdr[2] {
		t.Fatalf("GLB legacy specularColor = %v, want %v", props["specularColor"], hdr)
	}
	if p := (InstancedGLBMeshIR{ID: "g", Src: "/m.glb"}).legacyProps(); func() bool {
		_, a := p["specularIntensity"]
		_, b := p["specularColor"]
		return a || b
	}() {
		t.Fatal("absent GLB specular fields must not emit legacy keys")
	}

	inst2 := InstancedMeshIR{ID: "i", Kind: "box", Count: 1, SpecularIntensity: Float(0)}
	if v, ok := inst2.legacyProps()["specularIntensity"].(float64); !ok || v != 0 {
		t.Fatalf("instanced legacy specularIntensity = %v, want present 0", inst2.legacyProps()["specularIntensity"])
	}
	if _, ok := (InstancedMeshIR{ID: "i", Kind: "box", Count: 1}).legacyProps()["specularIntensity"]; ok {
		t.Fatal("absent instanced specularIntensity must not emit a legacy key")
	}

	canonical := materialFromObjectIR(ObjectIR{ID: "a", SpecularIntensity: Float(0.5), SpecularColor: specColor(hdr)})
	if canonical.SpecularIntensity == nil || *canonical.SpecularIntensity != 0.5 || canonical.SpecularColor == nil || *canonical.SpecularColor != hdr {
		t.Fatalf("canonical material specular = %v/%v, want 0.5/%v", canonical.SpecularIntensity, canonical.SpecularColor, hdr)
	}
}

// TestSpecularDiffCommands proves intensity- and color-only changes reach the
// wire with real values, and that resets drop the keys entirely.
func TestSpecularDiffCommands(t *testing.T) {
	base := func() SceneIR {
		return SceneIR{Schema: SceneIRSchema, Objects: []ObjectIR{{ID: "a", Kind: "box"}}}
	}
	payloads := func(cmds []Command) []string {
		out := make([]string, 0, len(cmds))
		for _, cmd := range cmds {
			data, err := json.Marshal(cmd)
			if err != nil {
				t.Fatalf("marshal command: %v", err)
			}
			out = append(out, string(data))
		}
		return out
	}
	has := func(all []string, needle string) bool {
		for _, payload := range all {
			if strings.Contains(payload, needle) {
				return true
			}
		}
		return false
	}

	prev, next := base(), base()
	next.Objects[0].SpecularIntensity = Float(0.5)
	next.Objects[0].SpecularColor = specColor([3]float64{2.5, 1, 0.5})
	all := payloads(DiffCommands(prev, next))
	if len(all) == 0 || !has(all, `"specularIntensity":0.5`) || !has(all, `"specularColor":[2.5,1,0.5]`) {
		t.Fatalf("specular change produced wrong commands: %v", all)
	}

	prev, next = base(), base()
	prev.Objects[0].SpecularIntensity = Float(0.25)
	next.Objects[0].SpecularIntensity = Float(0)
	all = payloads(DiffCommands(prev, next))
	if len(all) == 0 || !has(all, `"specularIntensity":0`) {
		t.Fatalf("value -> explicit-zero change lost: %v", all)
	}

	prev, next = base(), base()
	prev.Objects[0].SpecularIntensity = Float(0.5)
	prev.Objects[0].SpecularColor = specColor([3]float64{1, 1, 1})
	all = payloads(DiffCommands(prev, next))
	if len(all) == 0 {
		t.Fatal("specular reset produced no DiffCommands")
	}
	if has(all, `"specularIntensity":`) || has(all, `"specularColor":`) {
		t.Fatalf("reset commands still carry specular keys: %v", all)
	}

	if cmds := DiffCommands(base(), base()); len(cmds) != 0 {
		t.Fatalf("identical scenes produced %d commands", len(cmds))
	}
}

// TestSpecularCanonicalMaterialSeparation proves the canonical dedup keeps
// absent, zero-intensity, black-color, and HDR profiles distinct.
func TestSpecularCanonicalMaterialSeparation(t *testing.T) {
	build := func(objects ...ObjectIR) []IRMaterial {
		mats := []IRMaterial{}
		indexes := map[string]int{}
		for _, object := range objects {
			appendIRMaterial(&mats, indexes, materialFromObjectIR(object))
		}
		return mats
	}
	got := build(
		ObjectIR{ID: "a"},
		ObjectIR{ID: "b", SpecularIntensity: Float(0)},
		ObjectIR{ID: "c", SpecularColor: specColor([3]float64{0, 0, 0})},
		ObjectIR{ID: "d", SpecularColor: specColor([3]float64{2.5, 1, 0.5})},
		ObjectIR{ID: "e", SpecularIntensity: Float(1), SpecularColor: specColor([3]float64{1, 1, 1})},
	)
	if len(got) != 5 {
		t.Fatalf("distinct specular profiles collapsed to %d canonical materials, want 5", len(got))
	}
	if got := build(ObjectIR{ID: "a"}, ObjectIR{ID: "b"}); len(got) != 1 {
		t.Fatalf("two absent-specular materials deduped to %d, want 1", len(got))
	}
	if got := build(
		ObjectIR{ID: "a", SpecularIntensity: Float(1), SpecularColor: specColor([3]float64{1, 1, 1})},
		ObjectIR{ID: "b", SpecularIntensity: Float(1), SpecularColor: specColor([3]float64{1, 1, 1})},
	); len(got) != 1 {
		t.Fatalf("identical one/white specular materials deduped to %d, want 1", len(got))
	}
}

// TestSpecularCanonicalMaterialJSON covers IRMaterial and IRMaterialVariant
// JSON propagation, including explicit-zero keys.
func TestSpecularCanonicalMaterialJSON(t *testing.T) {
	data, err := json.Marshal(IRMaterial{SpecularIntensity: Float(0), SpecularColor: specColor([3]float64{0, 0, 0})})
	if err != nil {
		t.Fatalf("marshal IRMaterial: %v", err)
	}
	if !strings.Contains(string(data), `"specularIntensity":0`) || !strings.Contains(string(data), `"specularColor":[0,0,0]`) {
		t.Fatalf("IRMaterial explicit zeros lost: %s", data)
	}
	absent, err := json.Marshal(IRMaterial{})
	if err != nil {
		t.Fatalf("marshal IRMaterial: %v", err)
	}
	if strings.Contains(string(absent), "specular") {
		t.Fatalf("absent IRMaterial specular fields emitted JSON keys: %s", absent)
	}
	variant, err := json.Marshal(IRMaterial{Variants: map[string]IRMaterialVariant{"full": {SpecularIntensity: Float(1), SpecularColor: specColor([3]float64{2.5, 1, 0.5})}}})
	if err != nil {
		t.Fatalf("marshal variant: %v", err)
	}
	var out IRMaterial
	if err := json.Unmarshal(variant, &out); err != nil {
		t.Fatalf("unmarshal variant: %v", err)
	}
	v := out.Variants["full"]
	if v.SpecularIntensity == nil || *v.SpecularIntensity != 1 || v.SpecularColor == nil || *v.SpecularColor != [3]float64{2.5, 1, 0.5} {
		t.Fatalf("variant specular = %v/%v, want 1/[2.5 1 0.5]", v.SpecularIntensity, v.SpecularColor)
	}
}
