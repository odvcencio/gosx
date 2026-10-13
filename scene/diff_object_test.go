package scene

import (
	"math"
	"reflect"
	"testing"
)

func TestObjectScalarEditsRejectWithoutMarshalingMesh(t *testing.T) {
	for _, i := range objectScalarFields {
		t.Run(reflect.TypeFor[ObjectIR]().Field(i).Name, func(t *testing.T) {
			calls := 0
			a := ObjectIR{ID: "mesh", CustomUniforms: map[string]any{"payload": transformRejectionMarshalProbe{&calls}}}
			b := a
			field := reflect.ValueOf(&b).Elem().Field(i)
			if field.Kind() == reflect.Pointer {
				field.Set(reflect.New(field.Type().Elem()))
				field = field.Elem()
			}
			switch field.Kind() {
			case reflect.Bool:
				field.SetBool(!field.Bool())
			case reflect.String:
				field.SetString(field.String() + "changed")
			case reflect.Int, reflect.Int64:
				field.SetInt(3)
			case reflect.Uint, reflect.Uint64:
				field.SetUint(3)
			case reflect.Float64:
				field.SetFloat(.75)
			}
			if sceneRecordPointerJSONEqual(&a, &b) || calls != 0 {
				t.Fatalf("edit was lost or encoded unchanged payload: calls=%d", calls)
			}
			if referenceMarshalEqual(a, b) {
				t.Fatal("rejection disagrees with JSON")
			}
		})
	}
}

func TestMeshRejectionKeepsJSONSemantics(t *testing.T) {
	for _, tc := range []struct {
		name string
		a, b MeshVertices
	}{
		{"empty optional streams", MeshVertices{}, MeshVertices{Positions: []float64{}, Indices: []uint32{}}},
		{"signed array zero", MeshVertices{Positions: []float64{0}}, MeshVertices{Positions: []float64{negativeZero}}},
		{"changed index", MeshVertices{Indices: []uint32{0, 1, 2}}, MeshVertices{Indices: []uint32{0, 2, 1}}},
		{"changed count", MeshVertices{Count: 1}, MeshVertices{Count: 2}},
		{"nonfinite streams", MeshVertices{Normals: []float64{math.Inf(1)}}, MeshVertices{Normals: []float64{math.Inf(1)}}},
		{"changed custom attributes", MeshVertices{}, MeshVertices{Attributes: map[string]MeshAttribute{"weight": {ItemSize: 1, Data: []float64{1}}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, b := ObjectIR{ID: "mesh", Vertices: &tc.a}, ObjectIR{ID: "mesh", Vertices: &tc.b}
			if got, want := sceneRecordPointerJSONEqual(&a, &b), referenceMarshalEqual(a, b); got != want {
				t.Fatalf("equal=%v, JSON equal=%v", got, want)
			}
		})
	}
}

func TestObjectStringRejectionKeepsUTF8ReplacementSemantics(t *testing.T) {
	for _, pair := range [][2]string{{"bad\xff", "bad\xfe"}, {"bad\xff", "bad\ufffd"}, {"bad\xff", "different"}} {
		a, b := ObjectIR{ID: "mesh", Label: pair[0]}, ObjectIR{ID: "mesh", Label: pair[1]}
		if got, want := sceneRecordPointerJSONEqual(&a, &b), referenceMarshalEqual(a, b); got != want {
			t.Fatalf("labels %q and %q: equal=%v, JSON equal=%v", pair[0], pair[1], got, want)
		}
	}
}

func BenchmarkDiffSceneInteractionMeshEdits(b *testing.B) {
	previous, next := SceneIR{}, SceneIR{}
	for i := 0; i < 28; i++ {
		mesh := ObjectIR{ID: string(rune('a' + i)), Kind: "gltf-mesh", Vertices: &MeshVertices{Positions: make([]float64, 18000), Count: 6000}, Interactive: true}
		previous.Objects = append(previous.Objects, mesh)
		mesh.Interactive = false
		next.Objects = append(next.Objects, mesh)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		DiffScene(previous, next, DiffOptions{PatchTransforms: true})
	}
}
