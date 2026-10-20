package scene

import "testing"

type transformRejectionMarshalProbe struct{ calls *int }

func (p transformRejectionMarshalProbe) MarshalJSON() ([]byte, error) {
	*p.calls++
	return []byte(`"payload"`), nil
}

func TestChangedObjectTransformsRejectBeforeMarshalingPayload(t *testing.T) {
	for _, field := range []struct {
		name string
		set  func(*ObjectIR)
	}{
		{"x", func(o *ObjectIR) { o.X = 1 }},
		{"y", func(o *ObjectIR) { o.Y = 1 }},
		{"z", func(o *ObjectIR) { o.Z = 1 }},
		{"rotationX", func(o *ObjectIR) { o.RotationX = 1 }},
		{"rotationY", func(o *ObjectIR) { o.RotationY = 1 }},
		{"rotationZ", func(o *ObjectIR) { o.RotationZ = 1 }},
		{"scaleX", func(o *ObjectIR) { o.ScaleX = 1 }},
		{"scaleY", func(o *ObjectIR) { o.ScaleY = 1 }},
		{"scaleZ", func(o *ObjectIR) { o.ScaleZ = 1 }},
	} {
		t.Run(field.name, func(t *testing.T) {
			calls := 0
			a := ObjectIR{ID: "mesh", CustomUniforms: map[string]any{"payload": transformRejectionMarshalProbe{&calls}}}
			b := a
			field.set(&b)
			if sceneRecordPointerJSONEqual(&a, &b) {
				t.Fatal("changed transform compared equal")
			}
			if calls != 0 {
				t.Fatalf("marshaled unchanged payload %d times", calls)
			}
			if referenceMarshalEqual(a, b) {
				t.Fatal("fast rejection disagrees with the wire comparison")
			}
		})
	}
}
