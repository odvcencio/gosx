package scene

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestStandardMaterialIOROptionalVersusZero pins the absent-vs-explicit-zero
// contract: an unset IOR emits no ior key anywhere, while an explicitly
// authored zero survives as a present zero (the special 1-reflectance case).
func TestStandardMaterialIOROptionalVersusZero(t *testing.T) {
	if _, ok := (StandardMaterial{}).legacyMaterial()["ior"]; ok {
		t.Fatal("absent StandardMaterial IOR must not emit a legacy ior key")
	}
	if got := (StandardMaterial{IOR: Float(0)}).legacyMaterial()["ior"]; got != float64(0) {
		t.Fatalf("explicit zero IOR lowered to %v, want 0", got)
	}

	data, err := json.Marshal(ObjectIR{ID: "a", IOR: Float(0)})
	if err != nil {
		t.Fatalf("marshal ObjectIR: %v", err)
	}
	if !strings.Contains(string(data), `"ior":0`) {
		t.Fatalf("explicit zero IOR lost in JSON: %s", data)
	}
	absent, err := json.Marshal(ObjectIR{ID: "a"})
	if err != nil {
		t.Fatalf("marshal ObjectIR: %v", err)
	}
	if strings.Contains(string(absent), "ior") {
		t.Fatalf("absent IOR emitted a JSON key: %s", absent)
	}
}

// TestIORTypedGraphLowering exercises the real Props{Graph: NewGraph(...)}.SceneIR()
// lowering path for all four record kinds — ordinary object, model (embedded
// ObjectIR), instanced mesh, and GLB-instanced mesh — for absent, explicit
// zero, and valued IOR. Literal IR maps cannot catch a missed lowerer.
func TestIORTypedGraphLowering(t *testing.T) {
	build := func(ior *float64) SceneIR {
		mat := StandardMaterial{Color: "#ffffff", IOR: ior}
		graph := NewGraph(
			Mesh{ID: "ior-mesh", Geometry: CubeGeometry{Size: 1}, Material: mat},
			Model{ID: "ior-model", Src: "/m.glb", Material: mat},
			InstancedMesh{ID: "ior-inst", Count: 1, Geometry: CubeGeometry{Size: 1}, Material: mat, Positions: []Vector3{{X: 0, Y: 0, Z: 0}}},
			InstancedGLBMesh{ID: "ior-glb", Src: "/g.glb", Material: mat, Instances: []MeshInstance{{ID: "i0"}}},
		)
		return Props{Graph: graph}.SceneIR()
	}
	check := func(ir SceneIR, want *float64) {
		t.Helper()
		if len(ir.Objects) != 1 || len(ir.Models) != 1 || len(ir.InstancedMeshes) != 1 || len(ir.InstancedGLBMeshes) != 1 {
			t.Fatalf("typed lowering lost records: %d objects, %d models, %d instanced, %d GLB",
				len(ir.Objects), len(ir.Models), len(ir.InstancedMeshes), len(ir.InstancedGLBMeshes))
		}
		for _, got := range []*float64{ir.Objects[0].IOR, ir.Models[0].IOR, ir.InstancedMeshes[0].IOR, ir.InstancedGLBMeshes[0].IOR} {
			if (got == nil) != (want == nil) || (got != nil && *got != *want) {
				t.Fatalf("typed lowering IOR = %v, want %v", got, want)
			}
		}
	}
	check(build(nil), nil)
	check(build(Float(0)), Float(0))
	check(build(Float(1.33)), Float(1.33))
}

// TestIORTypedAndLegacyLowering covers the direct typed and legacy map paths,
// including model (embedded ObjectIR), instanced, and GLB records.
func TestIORTypedAndLegacyLowering(t *testing.T) {
	var typed ObjectIR
	applyMaterialToObjectIR(&typed, StandardMaterial{IOR: Float(1.5)})
	if typed.IOR == nil || *typed.IOR != 1.5 {
		t.Fatalf("typed standard IOR = %v, want 1.5", typed.IOR)
	}
	var zero ObjectIR
	applyMaterialToObjectIR(&zero, StandardMaterial{IOR: Float(0)})
	if zero.IOR == nil || *zero.IOR != 0 {
		t.Fatalf("typed standard zero IOR = %v, want present 0", zero.IOR)
	}
	var absent ObjectIR
	applyMaterialToObjectIR(&absent, StandardMaterial{})
	if absent.IOR != nil {
		t.Fatalf("absent StandardMaterial IOR lowered to %v, want nil", absent.IOR)
	}

	var fromMap ObjectIR
	applyMaterialProps(&fromMap, map[string]any{"ior": float64(0)})
	if fromMap.IOR == nil || *fromMap.IOR != 0 {
		t.Fatalf("legacy map zero IOR = %v, want present 0", fromMap.IOR)
	}
	var fromMapValue ObjectIR
	applyMaterialProps(&fromMapValue, map[string]any{"ior": float64(2.4)})
	if fromMapValue.IOR == nil || *fromMapValue.IOR != 2.4 {
		t.Fatalf("legacy map IOR = %v, want 2.4", fromMapValue.IOR)
	}

	model := ModelIR{ObjectIR: ObjectIR{ID: "m"}}
	applyMaterialToObjectIR(&model.ObjectIR, StandardMaterial{IOR: Float(2)})
	if model.IOR == nil || *model.IOR != 2 {
		t.Fatalf("model embedded IOR = %v, want 2", model.IOR)
	}

	inst := InstancedMeshIR{ID: "i", Kind: "box", Count: 1, IOR: Float(0)}
	if v, ok := inst.legacyProps()["ior"].(float64); !ok || v != 0 {
		t.Fatalf("instanced legacy ior = %v, want present 0", inst.legacyProps()["ior"])
	}
	if _, ok := (InstancedMeshIR{ID: "i", Kind: "box", Count: 1}).legacyProps()["ior"]; ok {
		t.Fatal("absent instanced IOR must not emit a legacy ior key")
	}
	if m := materialFromInstancedIR(inst); m.IOR == nil || *m.IOR != 0 {
		t.Fatalf("canonical instanced IOR = %v, want present 0", m.IOR)
	}

	glb := InstancedGLBMeshIR{ID: "g", Src: "/m.glb", IOR: Float(1.33)}
	if v, ok := glb.legacyProps()["ior"].(float64); !ok || v != 1.33 {
		t.Fatalf("GLB legacy ior = %v, want 1.33", glb.legacyProps()["ior"])
	}
	if _, ok := (InstancedGLBMeshIR{ID: "g", Src: "/m.glb"}).legacyProps()["ior"]; ok {
		t.Fatal("absent GLB IOR must not emit a legacy ior key")
	}
}

// TestIORDiffCommands proves an IOR-only change reaches the wire as a command
// with the actual material value, including the absent -> explicit-zero
// transition and a 1.33 -> 2.42 value change, and that a reset back to absent
// drops the ior key entirely (remove plus create) without forgetting the zero
// case.
func TestIORDiffCommands(t *testing.T) {
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
	hasIOR := func(all []string, needle string) bool {
		for _, payload := range all {
			if strings.Contains(payload, needle) {
				return true
			}
		}
		return false
	}

	prev, next := base(), base()
	next.Objects[0].IOR = Float(1.5)
	all := payloads(DiffCommands(prev, next))
	if len(all) == 0 {
		t.Fatal("IOR-only change produced no DiffCommands")
	}
	if !hasIOR(all, `"ior":1.5`) {
		t.Fatalf("emitted commands carry no ior 1.5 value: %v", all)
	}

	prev, next = base(), base()
	prev.Objects[0].IOR = Float(1.33)
	next.Objects[0].IOR = Float(2.42)
	all = payloads(DiffCommands(prev, next))
	if len(all) == 0 {
		t.Fatal("1.33 -> 2.42 IOR change produced no DiffCommands")
	}
	if !hasIOR(all, `"ior":2.42`) {
		t.Fatalf("emitted commands carry no ior 2.42 value: %v", all)
	}

	prev, next = base(), base()
	next.Objects[0].IOR = Float(0)
	all = payloads(DiffCommands(prev, next))
	if len(all) == 0 {
		t.Fatal("absent -> explicit-zero IOR change produced no DiffCommands")
	}
	if !hasIOR(all, `"ior":0`) {
		t.Fatalf("emitted commands carry no explicit-zero ior: %v", all)
	}

	prev, next = base(), base()
	prev.Objects[0].IOR = Float(1.5)
	all = payloads(DiffCommands(prev, next))
	if len(all) == 0 {
		t.Fatal("valued IOR reset produced no DiffCommands")
	}
	if hasIOR(all, `"ior":`) {
		t.Fatalf("reset commands still carry an ior key: %v", all)
	}

	prev, next = base(), base()
	prev.Objects[0].IOR = Float(0)
	all = payloads(DiffCommands(prev, next))
	if len(all) == 0 {
		t.Fatal("zero IOR reset produced no DiffCommands")
	}
	if hasIOR(all, `"ior":`) {
		t.Fatalf("zero reset commands still carry an ior key: %v", all)
	}

	if cmds := DiffCommands(base(), base()); len(cmds) != 0 {
		t.Fatalf("identical scenes produced %d commands", len(cmds))
	}
}

// TestIORCanonicalMaterialSeparation proves the canonical material dedup keeps
// absent, explicit-zero, and valued IOR as distinct profiles, and collapses
// identical ones.
func TestIORCanonicalMaterialSeparation(t *testing.T) {
	build := func(objects ...ObjectIR) []IRMaterial {
		mats := []IRMaterial{}
		indexes := map[string]int{}
		for _, object := range objects {
			appendIRMaterial(&mats, indexes, materialFromObjectIR(object))
		}
		return mats
	}
	if got := build(ObjectIR{ID: "a"}, ObjectIR{ID: "b", IOR: Float(1.5)}, ObjectIR{ID: "c", IOR: Float(0)}); len(got) != 3 {
		t.Fatalf("absent/1.5/0 collapsed to %d canonical materials, want 3", len(got))
	}
	if got := build(ObjectIR{ID: "a"}, ObjectIR{ID: "b"}); len(got) != 1 {
		t.Fatalf("two absent-IOR materials deduped to %d, want 1", len(got))
	}
	if got := build(ObjectIR{ID: "a"}, ObjectIR{ID: "b", IOR: Float(0)}); len(got) != 2 {
		t.Fatalf("absent and explicit-zero deduped to %d, want 2", len(got))
	}
}

// TestIORCanonicalMaterialJSON covers IRMaterial and IRMaterialVariant JSON
// propagation, including the explicit-zero key.
func TestIORCanonicalMaterialJSON(t *testing.T) {
	data, err := json.Marshal(IRMaterial{IOR: Float(0)})
	if err != nil {
		t.Fatalf("marshal IRMaterial: %v", err)
	}
	if !strings.Contains(string(data), `"ior":0`) {
		t.Fatalf("IRMaterial explicit zero IOR lost: %s", data)
	}
	absent, err := json.Marshal(IRMaterial{})
	if err != nil {
		t.Fatalf("marshal IRMaterial: %v", err)
	}
	if strings.Contains(string(absent), "ior") {
		t.Fatalf("absent IRMaterial IOR emitted a JSON key: %s", absent)
	}
	variant, err := json.Marshal(IRMaterial{Variants: map[string]IRMaterialVariant{"full": {IOR: Float(1.25)}}})
	if err != nil {
		t.Fatalf("marshal variant: %v", err)
	}
	var out IRMaterial
	if err := json.Unmarshal(variant, &out); err != nil {
		t.Fatalf("unmarshal variant: %v", err)
	}
	if got := out.Variants["full"].IOR; got == nil || *got != 1.25 {
		t.Fatalf("variant IOR = %v, want 1.25", got)
	}
}
