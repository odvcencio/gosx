package program

import "testing"

func TestTypedMapIndexRoundTrip(t *testing.T) {
	original := &Program{Name: "map", Exprs: []Expr{{Op: OpIndex, Operands: []ExprID{0, 1}, Value: IndexTypedMap, Type: TypeBool}}}
	for _, codec := range []struct {
		name   string
		encode func(*Program) ([]byte, error)
		decode func([]byte) (*Program, error)
	}{{"json", EncodeJSON, DecodeJSON}, {"binary", EncodeBinary, DecodeBinary}} {
		t.Run(codec.name, func(t *testing.T) {
			data, err := codec.encode(original)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := codec.decode(data)
			if err != nil {
				t.Fatal(err)
			}
			if got := decoded.Exprs[0]; got.Value != IndexTypedMap || got.Type != TypeBool {
				t.Fatalf("round trip lost map type: %+v", got)
			}
		})
	}
}
