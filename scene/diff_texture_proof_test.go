package scene

import (
	"math/rand/v2"
	"reflect"
	"testing"
)

func TestTextureProofNamedScalarSchema(t *testing.T) {
	types := []reflect.Type{reflect.TypeFor[TextureColorSpace](), reflect.TypeFor[TextureRole]()}
	if !textureScalarProofSchemaMatches(types...) {
		t.Fatal("current named scalar kinds rejected")
	}
	if textureScalarProofSchemaMatches(types[:1]...) || textureScalarProofSchemaMatches(append(types, reflect.TypeFor[string]())...) {
		t.Fatal("different scalar count accepted")
	}
	for i := range types {
		for _, replacement := range []reflect.Type{nil, reflect.TypeFor[int](), reflect.TypeFor[float64](), reflect.TypeFor[struct{}]()} {
			changed := append([]reflect.Type(nil), types...)
			changed[i] = replacement
			if textureScalarProofSchemaMatches(changed...) {
				t.Fatalf("changed scalar %d accepted: %v", i, replacement)
			}
		}
	}
}

func testTextureDescriptor() TextureDescriptor {
	return TextureDescriptor{URI: "/textures/rock.ktx2", Role: TextureRoleBaseColor, ColorSpace: TextureColorSpaceSRGB, Channels: "rgba", View: "2d", Format: "rgba8", MipLevels: 5, Width: 256, Height: 128, Faces: 1}
}

func assertTextureProofMatchesReference(t *testing.T, a, b MaterialTextureDescriptors) {
	t.Helper()
	// This is the pre-optimization structural walk; it still handles material
	// structs generically, independently of the new generated field accesses.
	wantProof := provenJSONEqual(reflect.ValueOf(&a).Elem(), reflect.ValueOf(&b).Elem())
	if got := provenMaterialTextureDescriptorsEqual(&a, &b); got != wantProof {
		t.Fatalf("typed proof=%v, structural reference=%v", got, wantProof)
	}
	previous, next := benchObjectIR(3), benchObjectIR(3)
	previous.TextureDescriptors, next.TextureDescriptors = a, b
	if got, want := sceneRecordPointerJSONEqual(&previous, &next), referenceMarshalEqual(previous, next); got != want {
		t.Fatalf("record equality=%v, JSON oracle=%v", got, want)
	}
	next.X += .5
	assertObjectPatchMatchesReference(t, previous, next)
	next.Selected = true
	assertObjectPatchMatchesReference(t, previous, next)
}

func TestTextureProofEverySlotAndField(t *testing.T) {
	descriptor := testTextureDescriptor()
	typ := reflect.TypeFor[MaterialTextureDescriptors]()
	for slot := range typ.NumField() {
		if typ.Field(slot).Type != reflect.TypeFor[TextureDescriptor]() {
			continue
		}
		for field := range reflect.TypeFor[TextureDescriptor]().NumField() {
			name := typ.Field(slot).Name + "/" + reflect.TypeFor[TextureDescriptor]().Field(field).Name
			t.Run(name, func(t *testing.T) {
				var a MaterialTextureDescriptors
				reflect.ValueOf(&a).Elem().Field(slot).Set(reflect.ValueOf(descriptor))
				b := a
				assertTextureProofMatchesReference(t, a, b)
				value := reflect.ValueOf(&b).Elem().Field(slot).Field(field)
				switch value.Kind() {
				case reflect.String:
					value.SetString(value.String() + "-changed")
				case reflect.Int:
					value.SetInt(value.Int() + 1)
				default:
					t.Fatalf("add a mutation for new descriptor field %v", value.Type())
				}
				assertTextureProofMatchesReference(t, a, b)
			})
		}
	}
}

func TestTextureProofJSONEdges(t *testing.T) {
	d := testTextureDescriptor()
	cases := map[string]struct{ a, b MaterialTextureDescriptors }{
		"nil and empty data":                        {MaterialTextureDescriptors{}, MaterialTextureDescriptors{Data: map[string]TextureDescriptor{}}},
		"equal data in different order":             {MaterialTextureDescriptors{Data: map[string]TextureDescriptor{"a": d, "b": d}}, MaterialTextureDescriptors{Data: map[string]TextureDescriptor{"b": d, "a": d}}},
		"different data keys same count":            {MaterialTextureDescriptors{Data: map[string]TextureDescriptor{"a": d}}, MaterialTextureDescriptors{Data: map[string]TextureDescriptor{"b": d}}},
		"changed data record":                       {MaterialTextureDescriptors{Data: map[string]TextureDescriptor{"a": d}}, MaterialTextureDescriptors{Data: map[string]TextureDescriptor{"a": {URI: d.URI, Width: 512}}}},
		"different invalid UTF8 URI same JSON":      {MaterialTextureDescriptors{BaseColor: TextureDescriptor{URI: "/bad\xff"}}, MaterialTextureDescriptors{BaseColor: TextureDescriptor{URI: "/bad\xfe"}}},
		"different invalid UTF8 role same JSON":     {MaterialTextureDescriptors{BaseColor: TextureDescriptor{URI: d.URI, Role: TextureRole("bad\xff")}}, MaterialTextureDescriptors{BaseColor: TextureDescriptor{URI: d.URI, Role: TextureRole("bad\xfe")}}},
		"different invalid UTF8 data key same JSON": {MaterialTextureDescriptors{Data: map[string]TextureDescriptor{"bad\xff": d}}, MaterialTextureDescriptors{Data: map[string]TextureDescriptor{"bad\xfe": d}}},
		"omitted descriptor metadata":               {MaterialTextureDescriptors{BaseColor: TextureDescriptor{Width: 128}}, MaterialTextureDescriptors{BaseColor: TextureDescriptor{Width: 256}}},
		"whitespace URI omits slot":                 {MaterialTextureDescriptors{BaseColor: TextureDescriptor{URI: " ", Width: 128}}, MaterialTextureDescriptors{BaseColor: TextureDescriptor{URI: "\t", Width: 256}}},
		"omitted slot with live slot":               {MaterialTextureDescriptors{BaseColor: d, Normal: TextureDescriptor{Width: 128}}, MaterialTextureDescriptors{BaseColor: d, Normal: TextureDescriptor{Width: 256}}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) { assertTextureProofMatchesReference(t, c.a, c.b) })
	}
}

func TestTextureProofRandomizedJSONReference(t *testing.T) {
	g := &recordGenerator{rng: rand.New(rand.NewPCG(0x7e0a, 0x3d)), nonFinite: true}
	for i := range 300 {
		var a, b MaterialTextureDescriptors
		g.fill(reflect.ValueOf(&a).Elem(), 0)
		if i%3 == 0 {
			b = a
		} else {
			g.fill(reflect.ValueOf(&b).Elem(), 0)
		}
		assertTextureProofMatchesReference(t, a, b)
	}
}
