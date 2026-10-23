package scene

import (
	"encoding/json"
	"math"
	"math/rand/v2"
	"reflect"
	"testing"
)

func TestObjectProofMatchesAuthoredSchema(t *testing.T) {
	if !objectProofSchemaMatches() {
		t.Fatal("ObjectIR proof is stale; run go generate ./scene")
	}
	for _, schema := range []struct {
		name    string
		typ     reflect.Type
		matches func(reflect.Type) bool
	}{
		{"ObjectIR", reflect.TypeFor[ObjectIR](), objectProofSchemaMatchesType},
		{"MaterialTextureDescriptors", reflect.TypeFor[MaterialTextureDescriptors](), materialTextureDescriptorsProofSchemaMatchesType},
		{"TextureDescriptor", reflect.TypeFor[TextureDescriptor](), textureDescriptorProofSchemaMatchesType},
	} {
		t.Run(schema.name, func(t *testing.T) { assertProofSchema(t, schema.typ, schema.matches) })
	}
}

func assertProofSchema(t *testing.T, typ reflect.Type, matches func(reflect.Type) bool) {
	t.Helper()
	fields := make([]reflect.StructField, typ.NumField())
	for i := range fields {
		fields[i] = typ.Field(i)
	}
	if matches(reflect.TypeFor[int]()) || matches(reflect.StructOf(fields[:len(fields)-1])) {
		t.Fatal("schema proof accepted a different record shape")
	}
	for i := range fields {
		original := fields[i]
		fields[i].Name += "Changed"
		if matches(reflect.StructOf(fields)) {
			t.Fatalf("schema proof ignored field name %s", original.Name)
		}
		fields[i] = original
		fields[i].Tag = `json:"changed"`
		if matches(reflect.StructOf(fields)) {
			t.Fatalf("schema proof ignored JSON tag for %s", original.Name)
		}
		fields[i] = original
		if original.Type.Kind() == reflect.String {
			fields[i].Type = reflect.TypeFor[int]()
		} else {
			fields[i].Type = reflect.TypeFor[string]()
		}
		if matches(reflect.StructOf(fields)) {
			t.Fatalf("schema proof ignored field type %s", original.Name)
		}
		fields[i] = original
	}
}

// The oracle retains the pre-optimization normalization and asks the JSON
// encoder directly. It never calls either optimized equality proof.
func referenceObjectPatch(previous, next ObjectIR, state bool) (Command, bool) {
	if state && next.Pickable == nil && !next.Interactive && (previous.Pickable != nil || previous.Interactive) {
		return Command{}, false
	}
	previous.X, previous.Y, previous.Z = next.X, next.Y, next.Z
	previous.RotationX, previous.RotationY, previous.RotationZ = next.RotationX, next.RotationY, next.RotationZ
	previous.ScaleX, previous.ScaleY, previous.ScaleZ = next.ScaleX, next.ScaleY, next.ScaleZ
	previous.ParentMatrix = next.ParentMatrix
	if state {
		previous.Interactive, previous.Label, previous.InteractiveOrder = next.Interactive, next.Label, next.InteractiveOrder
		previous.Pickable, previous.Visible, previous.Selected = next.Pickable, next.Visible, next.Selected
	}
	if !referenceMarshalEqual(previous, next) {
		return Command{}, false
	}
	matrix, ok := transformPatchParentMatrix(next.ParentMatrix)
	if !ok {
		return Command{}, false
	}
	patch := TransformPatch{X: next.X, Y: next.Y, Z: next.Z, RotationX: next.RotationX, RotationY: next.RotationY, RotationZ: next.RotationZ, ScaleX: resolveIRScale(next.ScaleX), ScaleY: resolveIRScale(next.ScaleY), ScaleZ: resolveIRScale(next.ScaleZ), ParentMatrix: matrix}
	if !state {
		return SetTransformCommand(next.ID, patch), true
	}
	return Command{Kind: CommandSetTransform, ObjectID: next.ID, Data: ObjectStatePatch{TransformPatch: patch, Interactive: next.Interactive, Label: next.Label, InteractiveOrder: next.InteractiveOrder, Pickable: next.Pickable, Visible: next.Visible == nil || *next.Visible, Selected: next.Selected}}, true
}

func assertObjectPatchMatchesReference(t *testing.T, previous, next ObjectIR) {
	t.Helper()
	for _, state := range []bool{false, true} {
		want, wantOK := referenceObjectPatch(previous, next, state)
		var got Command
		var ok bool
		if state {
			got, ok = objectStatePatch(&previous, &next)
		} else {
			got, ok = objectTransformPatch(&previous, &next)
		}
		if ok != wantOK {
			t.Fatalf("state=%v patch eligibility=%v, reference=%v", state, ok, wantOK)
		}
		if !ok {
			continue
		}
		encoded, err := json.Marshal(got)
		if err != nil {
			t.Fatal(err)
		}
		expected, err := json.Marshal(want)
		if err != nil {
			t.Fatal(err)
		}
		if string(encoded) != string(expected) {
			t.Fatalf("state=%v patch=%s, reference=%s", state, encoded, expected)
		}
	}
}

func TestObjectPatchProofMatchesReferenceForEveryField(t *testing.T) {
	typ := reflect.TypeFor[ObjectIR]()
	generator := &recordGenerator{rng: rand.New(rand.NewPCG(0x3d, 0xc0a57)), nonFinite: true}
	for i := range typ.NumField() {
		t.Run(typ.Field(i).Name, func(t *testing.T) {
			for range 4 {
				previous := benchObjectIR(7)
				next := previous
				next.X += .5
				generator.fill(reflect.ValueOf(&next).Elem().Field(i), 0)
				assertObjectPatchMatchesReference(t, previous, next)
				next.Selected = true
				assertObjectPatchMatchesReference(t, previous, next)
			}
		})
	}
}

func TestObjectPatchProofPreservesNonFiniteAndJSONEdges(t *testing.T) {
	previous := benchObjectIR(7)
	for _, name := range []string{"X", "Y", "Z", "RotationX", "RotationY", "RotationZ", "ScaleX", "ScaleY", "ScaleZ"} {
		t.Run(name, func(t *testing.T) {
			for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
				next := previous
				reflect.ValueOf(&next).Elem().FieldByName(name).SetFloat(value)
				assertObjectPatchMatchesReference(t, previous, next)
			}
		})
	}
	cases := map[string]func(*ObjectIR, *ObjectIR){
		"nil and empty points":   func(a, b *ObjectIR) { b.Points = LinePoints{} },
		"nil and empty uniforms": func(a, b *ObjectIR) { b.CustomUniforms = map[string]any{} },
		"scalar signed zero":     func(a, b *ObjectIR) { a.NormalScale = new(float64); n := negativeZero; b.NormalScale = &n },
		"omitted signed zero":    func(a, b *ObjectIR) { a.LineWidth = 0; b.LineWidth = negativeZero },
		"array signed zero": func(a, b *ObjectIR) {
			a.CustomUniforms = map[string]any{"v": []float64{0}}
			b.CustomUniforms = map[string]any{"v": []float64{negativeZero}}
		},
		"invalid UTF8":            func(a, b *ObjectIR) { a.Color = "bad\xff"; b.Color = "bad\xfe" },
		"state label replacement": func(a, b *ObjectIR) { a.Label = "bad\xff"; b.Label = "bad\xfe" },
		"different dynamic types": func(a, b *ObjectIR) {
			a.CustomUniforms = map[string]any{"v": int(1)}
			b.CustomUniforms = map[string]any{"v": float64(1)}
		},
		"empty parent matrix": func(a, b *ObjectIR) { b.ParentMatrix = []float64{} },
		"nonfinite parent matrix": func(a, b *ObjectIR) {
			b.ParentMatrix = []float64{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, math.NaN(), 0, 0, 1}
		},
		"invalid affine bottom row": func(a, b *ObjectIR) { b.ParentMatrix = []float64{1, 0, 0, 1, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1} },
		"valid parent matrix":       func(a, b *ObjectIR) { b.ParentMatrix = []float64{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 3, 0, 0, 1} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			a, b := previous, previous
			b.X += .5
			mutate(&a, &b)
			assertObjectPatchMatchesReference(t, a, b)
			b.Selected = true
			assertObjectPatchMatchesReference(t, a, b)
		})
	}
}

func TestNestedObjectProofPreservesStructuralAndJSONSemantics(t *testing.T) {
	type wrapper struct {
		Object ObjectIR
		hidden ObjectIR
	}
	a := wrapper{Object: benchObjectIR(1), hidden: benchObjectIR(2)}
	b := a
	if !provenJSONEqual(reflect.ValueOf(&a).Elem(), reflect.ValueOf(&b).Elem()) {
		t.Fatal("identical nested records were not proven equal")
	}
	b.hidden.X++
	if provenJSONEqual(reflect.ValueOf(&a).Elem(), reflect.ValueOf(&b).Elem()) {
		t.Fatal("structural proof ignored an unexported nested edit")
	}
	if !sceneRecordJSONEqual(a, b) {
		t.Fatal("JSON fallback lost unexported-field semantics")
	}
	b = a
	b.Object.X++
	if sceneRecordJSONEqual(a, b) {
		t.Fatal("nested typed proof lost an exported edit")
	}
}
