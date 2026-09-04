package scene

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func cutoffSame(a, b AlphaCutoff) bool {
	if a.IsZero() || b.IsZero() {
		return a.IsZero() == b.IsZero()
	}
	if a.Disabled() || b.Disabled() {
		return a.Disabled() == b.Disabled()
	}
	av, aok := a.Value()
	bv, bok := b.Value()
	return aok && bok && av == bv
}

// TestAlphaCutoffJSONTriState pins the wire contract: omitted emits no key,
// explicit disable emits null, and numeric values emit numbers — including
// the explicit zero and values above 1.
func TestAlphaCutoffJSONTriState(t *testing.T) {
	data, err := json.Marshal(ObjectIR{ID: "a", AlphaCutoff: Cutoff(0)})
	if err != nil {
		t.Fatalf("marshal zero cutoff: %v", err)
	}
	if !strings.Contains(string(data), `"alphaCutoff":0`) {
		t.Fatalf("explicit zero cutoff lost: %s", data)
	}
	data, err = json.Marshal(ObjectIR{ID: "a", AlphaCutoff: Cutoff(1.5)})
	if err != nil {
		t.Fatalf("marshal 1.5 cutoff: %v", err)
	}
	if !strings.Contains(string(data), `"alphaCutoff":1.5`) {
		t.Fatalf("1.5 cutoff lost: %s", data)
	}
	data, err = json.Marshal(ObjectIR{ID: "a", AlphaCutoff: CutoffDisabled()})
	if err != nil {
		t.Fatalf("marshal disabled cutoff: %v", err)
	}
	if !strings.Contains(string(data), `"alphaCutoff":null`) {
		t.Fatalf("explicit disable lost: %s", data)
	}
	absent, err := json.Marshal(ObjectIR{ID: "a"})
	if err != nil {
		t.Fatalf("marshal absent cutoff: %v", err)
	}
	if strings.Contains(string(absent), "alphaCutoff") {
		t.Fatalf("absent cutoff emitted a JSON key: %s", absent)
	}
}

// TestAlphaCutoffRoundTrip proves every state survives marshal then
// unmarshal unchanged.
func TestAlphaCutoffRoundTrip(t *testing.T) {
	for _, want := range []AlphaCutoff{{}, CutoffDisabled(), Cutoff(0), Cutoff(1.5), Cutoff(2)} {
		data, err := json.Marshal(ObjectIR{ID: "a", AlphaCutoff: want})
		if err != nil {
			t.Fatalf("marshal %v: %v", want, err)
		}
		var got ObjectIR
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("unmarshal %s: %v", data, err)
		}
		if !cutoffSame(got.AlphaCutoff, want) {
			t.Fatalf("round trip changed %v into %v", want, got.AlphaCutoff)
		}
	}
}

// TestAlphaCutoffUnmarshalNullVersusAbsent pins null = disabled and absent =
// omitted, and that invalid input leaves the receiver untouched.
func TestAlphaCutoffUnmarshalNullVersusAbsent(t *testing.T) {
	var omitted struct {
		C AlphaCutoff `json:"alphaCutoff,omitzero"`
	}
	if err := json.Unmarshal([]byte(`{}`), &omitted); err != nil {
		t.Fatalf("unmarshal absent: %v", err)
	}
	if !omitted.C.IsZero() {
		t.Fatalf("absent key lowered to %v, want omitted", omitted.C)
	}
	var disabled struct {
		C AlphaCutoff `json:"alphaCutoff,omitzero"`
	}
	if err := json.Unmarshal([]byte(`{"alphaCutoff":null}`), &disabled); err != nil {
		t.Fatalf("unmarshal null: %v", err)
	}
	if !disabled.C.Disabled() {
		t.Fatalf("null lowered to %v, want disabled", disabled.C)
	}
	kept := Cutoff(0.4)
	for _, bad := range [][]byte{[]byte(`-1`), []byte(`"high"`), []byte(`true`)} {
		if err := json.Unmarshal(bad, &kept); err == nil {
			t.Fatalf("invalid alphaCutoff %s unmarshal succeeded", bad)
		}
		if v, ok := kept.Value(); !ok || v != 0.4 {
			t.Fatalf("failed unmarshal mutated receiver: %v", kept)
		}
	}
}

// TestAlphaCutoffMarshalRejectsInvalidNumbers proves an authored invalid
// number is an error, never a silent downgrade to disabled.
func TestAlphaCutoffMarshalRejectsInvalidNumbers(t *testing.T) {
	for _, bad := range []AlphaCutoff{Cutoff(-0.5), Cutoff(math.NaN()), Cutoff(math.Inf(1)), Cutoff(math.Inf(-1))} {
		if _, err := json.Marshal(bad); err == nil {
			t.Fatalf("invalid cutoff %v marshaled without error", bad)
		}
		if _, err := json.Marshal(ObjectIR{ID: "a", AlphaCutoff: bad}); err == nil {
			t.Fatalf("record with invalid cutoff %v marshaled without error", bad)
		}
	}
}

// TestAlphaCutoffTypedGraphLowering exercises the real
// Props{Graph: NewGraph(...)}.SceneIR() lowering path for all four record
// kinds — object, model, instanced mesh, and GLB-instanced mesh.
func TestAlphaCutoffTypedGraphLowering(t *testing.T) {
	build := func(cutoff AlphaCutoff) SceneIR {
		mat := StandardMaterial{Color: "#ffffff", AlphaCutoff: cutoff}
		graph := NewGraph(
			Mesh{ID: "cut-mesh", Geometry: CubeGeometry{Size: 1}, Material: mat},
			Model{ID: "cut-model", Src: "/m.glb", Material: mat},
			InstancedMesh{ID: "cut-inst", Count: 1, Geometry: CubeGeometry{Size: 1}, Material: mat, Positions: []Vector3{{X: 0, Y: 0, Z: 0}}},
			InstancedGLBMesh{ID: "cut-glb", Src: "/g.glb", Material: mat, Instances: []MeshInstance{{ID: "i0"}}},
		)
		return Props{Graph: graph}.SceneIR()
	}
	check := func(ir SceneIR, want AlphaCutoff) {
		t.Helper()
		if len(ir.Objects) != 1 || len(ir.Models) != 1 || len(ir.InstancedMeshes) != 1 || len(ir.InstancedGLBMeshes) != 1 {
			t.Fatalf("typed lowering lost records: %d objects, %d models, %d instanced, %d GLB",
				len(ir.Objects), len(ir.Models), len(ir.InstancedMeshes), len(ir.InstancedGLBMeshes))
		}
		for _, got := range []AlphaCutoff{ir.Objects[0].AlphaCutoff, ir.Models[0].AlphaCutoff, ir.InstancedMeshes[0].AlphaCutoff, ir.InstancedGLBMeshes[0].AlphaCutoff} {
			if !cutoffSame(got, want) {
				t.Fatalf("typed lowering alphaCutoff = %v, want %v", got, want)
			}
		}
	}
	check(build(AlphaCutoff{}), AlphaCutoff{})
	check(build(CutoffDisabled()), CutoffDisabled())
	check(build(Cutoff(0)), Cutoff(0))
	check(build(Cutoff(0.5)), Cutoff(0.5))
}

// TestAlphaCutoffLegacyMapPaths covers typed exports and map imports for all
// four legacyProps shapes plus applyMaterialProps, and proves the source map
// is never mutated.
func TestAlphaCutoffLegacyMapPaths(t *testing.T) {
	var typed ObjectIR
	applyMaterialToObjectIR(&typed, StandardMaterial{AlphaCutoff: Cutoff(0.5)})
	if v, ok := typed.AlphaCutoff.Value(); !ok || v != 0.5 {
		t.Fatalf("typed standard alphaCutoff = %v, want 0.5", typed.AlphaCutoff)
	}
	var zero ObjectIR
	applyMaterialToObjectIR(&zero, StandardMaterial{AlphaCutoff: Cutoff(0)})
	if v, ok := zero.AlphaCutoff.Value(); !ok || v != 0 {
		t.Fatalf("typed zero alphaCutoff = %v, want present 0", zero.AlphaCutoff)
	}
	var absent ObjectIR
	applyMaterialToObjectIR(&absent, StandardMaterial{})
	if !absent.AlphaCutoff.IsZero() {
		t.Fatalf("absent StandardMaterial alphaCutoff = %v, want omitted", absent.AlphaCutoff)
	}

	src := map[string]any{"alphaCutoff": float64(0.5)}
	var fromMap ObjectIR
	applyMaterialProps(&fromMap, src)
	if v, ok := fromMap.AlphaCutoff.Value(); !ok || v != 0.5 {
		t.Fatalf("legacy map alphaCutoff = %v, want 0.5", fromMap.AlphaCutoff)
	}
	if got := src["alphaCutoff"]; got != float64(0.5) {
		t.Fatalf("source map mutated: %v", got)
	}

	nilMap := map[string]any{"alphaCutoff": nil}
	var fromNil ObjectIR
	applyMaterialProps(&fromNil, nilMap)
	if !fromNil.AlphaCutoff.Disabled() {
		t.Fatalf("explicit nil alphaCutoff = %v, want disabled", fromNil.AlphaCutoff)
	}
	if v, present := nilMap["alphaCutoff"]; !present || v != nil {
		t.Fatalf("source map mutated by nil handling: %v", nilMap)
	}

	var missing ObjectIR
	applyMaterialProps(&missing, map[string]any{"roughness": float64(1)})
	if !missing.AlphaCutoff.IsZero() {
		t.Fatalf("missing alphaCutoff key = %v, want omitted", missing.AlphaCutoff)
	}

	neg := map[string]any{"alphaCutoff": float64(-2)}
	var fromNeg ObjectIR
	applyMaterialProps(&fromNeg, neg)
	if v, ok := fromNeg.AlphaCutoff.Value(); !ok || v != -2 {
		t.Fatalf("negative alphaCutoff = %v, want authored -2 preserved", fromNeg.AlphaCutoff)
	}
	if got := neg["alphaCutoff"]; got != float64(-2) {
		t.Fatalf("source map mutated by invalid value: %v", got)
	}

	model := ModelIR{ObjectIR: ObjectIR{ID: "m"}}
	applyMaterialToObjectIR(&model.ObjectIR, StandardMaterial{AlphaCutoff: Cutoff(0.25)})
	if v, ok := model.AlphaCutoff.Value(); !ok || v != 0.25 {
		t.Fatalf("model embedded alphaCutoff = %v, want 0.25", model.AlphaCutoff)
	}

	inst := InstancedMeshIR{ID: "i", Kind: "box", Count: 1, AlphaCutoff: Cutoff(0)}
	if v, ok := inst.legacyProps()["alphaCutoff"].(float64); !ok || v != 0 {
		t.Fatalf("instanced legacy alphaCutoff = %v, want present 0", inst.legacyProps()["alphaCutoff"])
	}
	if _, ok := (InstancedMeshIR{ID: "i", Kind: "box", Count: 1}).legacyProps()["alphaCutoff"]; ok {
		t.Fatal("absent instanced alphaCutoff must not emit a legacy key")
	}
	if m := materialFromInstancedIR(inst); !cutoffSame(m.AlphaCutoff, Cutoff(0)) {
		t.Fatalf("canonical instanced alphaCutoff = %v, want 0", m.AlphaCutoff)
	}

	glb := InstancedGLBMeshIR{ID: "g", Src: "/m.glb", AlphaCutoff: CutoffDisabled()}
	if v, ok := glb.legacyProps()["alphaCutoff"]; !ok || v != nil {
		t.Fatalf("GLB legacy alphaCutoff = %v, want present nil", v)
	}
	if _, ok := (InstancedGLBMeshIR{ID: "g", Src: "/m.glb"}).legacyProps()["alphaCutoff"]; ok {
		t.Fatal("absent GLB alphaCutoff must not emit a legacy key")
	}

	mat := StandardMaterial{AlphaCutoff: CutoffDisabled()}
	legacy := mat.legacyMaterial()
	if v, ok := legacy["alphaCutoff"]; !ok || v != nil {
		t.Fatalf("StandardMaterial legacy alphaCutoff = %v, want present nil", v)
	}
}

// TestAlphaCutoffLegacyExporters pins the alphaCutoff legacy wire contract
// for all four record exporters with exact expected constants.
func TestAlphaCutoffLegacyExporters(t *testing.T) {
	cases := []struct {
		name     string
		cutoff   AlphaCutoff
		wantKey  bool
		wantVal  any
		wantJSON string // substring expected in marshaled JSON; "" means key must be absent
	}{
		{"absent", AlphaCutoff{}, false, nil, ""},
		{"disabled", CutoffDisabled(), true, nil, `"alphaCutoff":null`},
		{"zero", Cutoff(0), true, float64(0), `"alphaCutoff":0`},
		{"half", Cutoff(0.5), true, float64(0.5), `"alphaCutoff":0.5`},
		{"above-one", Cutoff(1.5), true, float64(1.5), `"alphaCutoff":1.5`},
	}
	build := func(kind string, c AlphaCutoff) any {
		switch kind {
		case "object":
			return ObjectIR{ID: "a", Kind: "box", AlphaCutoff: c}
		case "model":
			return ModelIR{ObjectIR: ObjectIR{ID: "m", Kind: "box", AlphaCutoff: c}, Src: "/m.glb"}
		case "instanced":
			return InstancedMeshIR{ID: "i", Kind: "box", Count: 1, AlphaCutoff: c}
		case "glb":
			return InstancedGLBMeshIR{ID: "g", Src: "/g.glb", AlphaCutoff: c}
		}
		t.Fatalf("unknown record kind %q", kind)
		return nil
	}
	for _, kind := range []string{"object", "model", "instanced", "glb"} {
		for _, tc := range cases {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				rec := build(kind, tc.cutoff)
				props := rec.(interface{ legacyProps() map[string]any }).legacyProps()
				got, ok := props["alphaCutoff"]
				if ok != tc.wantKey {
					t.Fatalf("legacyProps alphaCutoff present = %v, want %v", ok, tc.wantKey)
				}
				if tc.wantKey && got != tc.wantVal {
					t.Fatalf("legacyProps alphaCutoff = %#v, want %#v", got, tc.wantVal)
				}
				data, err := json.Marshal(rec)
				if err != nil {
					t.Fatalf("marshal: %v", err)
				}
				if tc.wantJSON == "" {
					if strings.Contains(string(data), "alphaCutoff") {
						t.Fatalf("absent cutoff emitted a legacy JSON key: %s", data)
					}
					return
				}
				if !strings.Contains(string(data), tc.wantJSON) {
					t.Fatalf("legacy JSON = %s, want substring %s", data, tc.wantJSON)
				}
			})
		}
	}
}

// TestAlphaCutoffCanonicalMaterialSeparation proves canonical dedup keeps the
// three states and distinct values apart, and collapses identical ones.
func TestAlphaCutoffCanonicalMaterialSeparation(t *testing.T) {
	build := func(objects ...ObjectIR) []IRMaterial {
		mats := []IRMaterial{}
		indexes := map[string]int{}
		for _, object := range objects {
			appendIRMaterial(&mats, indexes, materialFromObjectIR(object))
		}
		return mats
	}
	if got := build(ObjectIR{ID: "a"}, ObjectIR{ID: "b", AlphaCutoff: CutoffDisabled()}, ObjectIR{ID: "c", AlphaCutoff: Cutoff(0)}, ObjectIR{ID: "d", AlphaCutoff: Cutoff(0.5)}); len(got) != 4 {
		t.Fatalf("omitted/disabled/0/0.5 collapsed to %d canonical materials, want 4", len(got))
	}
	if got := build(ObjectIR{ID: "a"}, ObjectIR{ID: "b"}); len(got) != 1 {
		t.Fatalf("two omitted materials deduped to %d, want 1", len(got))
	}
	variant := IRMaterial{Variants: map[string]IRMaterialVariant{"full": {AlphaCutoff: Cutoff(0.5)}}}
	data, err := json.Marshal(variant)
	if err != nil {
		t.Fatalf("marshal variant: %v", err)
	}
	var out IRMaterial
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal variant: %v", err)
	}
	if v, ok := out.Variants["full"].AlphaCutoff.Value(); !ok || v != 0.5 {
		t.Fatalf("variant alphaCutoff = %v, want 0.5", out.Variants["full"].AlphaCutoff)
	}
}

// TestAlphaCutoffDiffCommands proves cutoff-only changes reach the wire for
// every tri-state transition and that identical scenes stay silent.
func TestAlphaCutoffDiffCommands(t *testing.T) {
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
	next.Objects[0].AlphaCutoff = Cutoff(0)
	all := payloads(DiffCommands(prev, next))
	if len(all) == 0 || !has(all, `"alphaCutoff":0`) {
		t.Fatalf("absent -> zero transition lost: %v", all)
	}

	prev, next = base(), base()
	next.Objects[0].AlphaCutoff = Cutoff(0.5)
	all = payloads(DiffCommands(prev, next))
	if len(all) == 0 || !has(all, `"alphaCutoff":0.5`) {
		t.Fatalf("absent -> value transition lost: %v", all)
	}

	prev, next = base(), base()
	prev.Objects[0].AlphaCutoff = Cutoff(0.5)
	next.Objects[0].AlphaCutoff = CutoffDisabled()
	all = payloads(DiffCommands(prev, next))
	if len(all) == 0 || !has(all, `"alphaCutoff":null`) {
		t.Fatalf("value -> null transition lost: %v", all)
	}

	prev, next = base(), base()
	prev.Objects[0].AlphaCutoff = Cutoff(0.5)
	all = payloads(DiffCommands(prev, next))
	if len(all) == 0 {
		t.Fatal("value -> absent reset produced no DiffCommands")
	}
	if has(all, `"alphaCutoff"`) {
		t.Fatalf("reset commands still carry an alphaCutoff key: %v", all)
	}

	prev, next = base(), base()
	prev.Objects[0].AlphaCutoff = Cutoff(0.5)
	next.Objects[0].AlphaCutoff = Cutoff(0.5)
	if cmds := DiffCommands(prev, next); len(cmds) != 0 {
		t.Fatalf("identical cutoffs produced %d commands", len(cmds))
	}
	if cmds := DiffCommands(base(), base()); len(cmds) != 0 {
		t.Fatalf("identical scenes produced %d commands", len(cmds))
	}
}
