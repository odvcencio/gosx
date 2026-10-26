package scene

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"testing"
)

func checkCutoffRecord[T any](t *testing.T, record T) {
	t.Helper()
	duplicate := record
	got, want := sceneRecordPointerJSONEqual(&record, &duplicate), referenceMarshalEqual(record, duplicate)
	if got != want {
		t.Fatalf("record equality=%v, JSON oracle=%v", got, want)
	}
}

func TestSceneRecordAlphaCutoffRejectsInvalidEncoding(t *testing.T) {
	cases := map[string]AlphaCutoff{"negative": Cutoff(-.5), "NaN": Cutoff(math.NaN()), "positive infinity": Cutoff(math.Inf(1)), "negative infinity": Cutoff(math.Inf(-1)), "invalid state": {state: alphaCutoffState(255)}}
	for name, cutoff := range cases {
		t.Run(name, func(t *testing.T) {
			object := ObjectIR{ID: "mesh", AlphaCutoff: cutoff}
			if _, err := json.Marshal(object); err == nil {
				t.Fatal("fixture must be rejected by JSON")
			}
			checkCutoffRecord(t, object)
			checkCutoffRecord(t, ModelIR{ObjectIR: object, Src: "/rock.glb"})
			checkCutoffRecord(t, InstancedMeshIR{ID: "instances", AlphaCutoff: cutoff})
			checkCutoffRecord(t, InstancedGLBMeshIR{ID: "glb-instances", AlphaCutoff: cutoff})
			checkCutoffRecord(t, ObjectIR{ID: "uniform", CustomUniforms: map[string]any{"cutoff": cutoff}})
		})
	}
}

func TestSceneRecordAlphaCutoffValidStates(t *testing.T) {
	for _, cutoff := range []AlphaCutoff{{}, CutoffDisabled(), Cutoff(0), Cutoff(negativeZero), Cutoff(.5), Cutoff(2)} {
		object := ObjectIR{ID: "mesh", AlphaCutoff: cutoff}
		checkCutoffRecord(t, object)
		checkCutoffRecord(t, ModelIR{ObjectIR: object, Src: "/rock.glb"})
		checkCutoffRecord(t, InstancedMeshIR{ID: "instances", AlphaCutoff: cutoff})
	}
	// Numeric signed zeros are present values; the custom marshaler emits their
	// distinct signs, unlike an omitted scalar transform field.
	a, b := ObjectIR{AlphaCutoff: Cutoff(0)}, ObjectIR{AlphaCutoff: Cutoff(negativeZero)}
	if got, want := sceneRecordPointerJSONEqual(&a, &b), referenceMarshalEqual(a, b); got != want || got {
		t.Fatalf("signed numeric zeros equality=%v, oracle=%v", got, want)
	}
}

func TestInvalidAlphaCutoffDiffKeepsJSONError(t *testing.T) {
	for _, patch := range []DiffOptions{{}, {PatchTransforms: true}, {PatchObjectState: true}} {
		object := ObjectIR{ID: "mesh", AlphaCutoff: Cutoff(-.5)}
		previous, next := SceneIR{Objects: []ObjectIR{object}}, SceneIR{Objects: []ObjectIR{object}}
		diff := DiffScene(previous, next, patch)
		if len(diff.Commands) != 2 {
			t.Fatalf("invalid record silently skipped: commands=%d", len(diff.Commands))
		}
		if _, err := MarshalCommands(diff.Commands); err == nil {
			t.Fatal("invalid cutoff must remain a JSON error")
		}
	}
}

func TestAlphaCutoffProofReadsUnexportedField(t *testing.T) {
	type wrapper struct{ cutoff AlphaCutoff }
	value := reflect.ValueOf(wrapper{cutoff: Cutoff(-.5)}).Field(0)
	if got := alphaCutoffProofValue(value); got.state != alphaCutoffNumeric || got.value != -.5 {
		t.Fatalf("private cutoff read changed value: %v", got)
	}
	// JSON ignores the private field; the record fallback still preserves that.
	checkCutoffRecord(t, wrapper{cutoff: Cutoff(-.5)})
}

func BenchmarkSceneRecordAlphaCutoff(b *testing.B) {
	for _, test := range []struct {
		name   string
		cutoff AlphaCutoff
	}{{"omitted", AlphaCutoff{}}, {"disabled", CutoffDisabled()}, {"numeric", Cutoff(.5)}, {"invalid", Cutoff(-.5)}} {
		b.Run(test.name, func(b *testing.B) {
			cutoff := test.cutoff
			a, c := benchObjectIR(7), benchObjectIR(7)
			a.AlphaCutoff, c.AlphaCutoff = cutoff, cutoff
			want := referenceMarshalEqual(a, c)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if got := sceneRecordPointerJSONEqual(&a, &c); got != want {
					b.Fatalf("equality=%v, oracle=%v", got, want)
				}
			}
		})
	}
}

func TestOmittedAlphaCutoffRequiresFieldOmission(t *testing.T) {
	c := AlphaCutoff{}
	checkCutoffRecord(t, c)
	checkCutoffRecord(t, &c)
	checkCutoffRecord(t, ObjectIR{ID: "value", CustomUniforms: map[string]any{"cutoff": c}})
	checkCutoffRecord(t, ObjectIR{ID: "pointer", CustomUniforms: map[string]any{"cutoff": &c}})
	type plain struct {
		Cutoff AlphaCutoff `json:"cutoff"`
	}
	type omitted struct {
		Cutoff AlphaCutoff `json:"cutoff,omitzero"`
	}
	type similarTag struct {
		Cutoff AlphaCutoff `json:"cutoff,omitzeroExtra"`
	}
	checkCutoffRecord(t, plain{c})
	checkCutoffRecord(t, omitted{c})
	checkCutoffRecord(t, similarTag{c})
	for _, unused := range []float64{-.5, math.NaN(), math.Inf(1)} {
		x := ObjectIR{ID: "a", AlphaCutoff: AlphaCutoff{value: unused}}
		checkCutoffRecord(t, x)
	}
}

func TestAlphaCutoffWarmProofDoesNotAllocate(t *testing.T) {
	object := ObjectIR{ID: "mesh", AlphaCutoff: Cutoff(.5)}
	instances := InstancedMeshIR{ID: "instances", AlphaCutoff: Cutoff(.5)}
	objectOther, instancesOther := object, instances
	// Prime lazy schema/field plans before measuring the steady-state proof.
	if !sceneRecordPointerJSONEqual(&object, &objectOther) || !sceneRecordPointerJSONEqual(&instances, &instancesOther) {
		t.Fatal("valid fixtures must prove equal")
	}
	for name, proof := range map[string]func(){
		"object": func() {
			if !sceneRecordPointerJSONEqual(&object, &objectOther) {
				panic("equality")
			}
		},
		"instances": func() {
			if !sceneRecordPointerJSONEqual(&instances, &instancesOther) {
				panic("equality")
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			if n := testing.AllocsPerRun(100, proof); n != 0 {
				t.Fatalf("warm proof allocates %g times", n)
			}
		})
	}
}

// This marshaler deliberately emits internal cutoff bits instead of honoring
// the field tag. A structural proof must keep those bits significant.
type customCutoffRecord struct {
	Cutoff AlphaCutoff `json:"cutoff,omitzero"`
}

func (v customCutoffRecord) MarshalJSON() ([]byte, error) {
	return json.Marshal(fmt.Sprintf("%v", v.Cutoff))
}

type objectWithCustomJSON ObjectIR

func (v objectWithCustomJSON) MarshalJSON() ([]byte, error) { return json.Marshal("custom") }

func TestCutoffProofDoesNotApplyTagsToCustomMarshalers(t *testing.T) {
	for _, state := range []alphaCutoffState{alphaCutoffOmitted, alphaCutoffDisabled} {
		a := customCutoffRecord{Cutoff: AlphaCutoff{state: state, value: 1}}
		b := customCutoffRecord{Cutoff: AlphaCutoff{state: state, value: 2}}
		if got, want := sceneRecordPointerJSONEqual(&a, &b), referenceMarshalEqual(a, b); got != want || got {
			t.Fatalf("custom marshaler equality=%v, oracle=%v", got, want)
		}
	}
	if objectProofSchemaMatchesType(reflect.TypeFor[objectWithCustomJSON]()) {
		t.Fatal("typed object proof must decline a custom marshaler")
	}
}

type objectWithPointerJSON ObjectIR

func (v *objectWithPointerJSON) MarshalJSON() ([]byte, error) { return json.Marshal("pointer-custom") }

func TestObjectProofDeclinesPointerMarshaler(t *testing.T) {
	if objectProofSchemaMatchesType(reflect.TypeFor[objectWithPointerJSON]()) {
		t.Fatal("typed proof must decline pointer marshalers")
	}
}
