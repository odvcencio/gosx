package ir

import (
	"testing"

	"m31labs.dev/gosx/island/program"
)

func TestTypedMapIndexEvidenceIsScoped(t *testing.T) {
	for _, tc := range []struct {
		name, declaration string
		want              bool
		typ               program.ExprType
	}{
		{"bool", "map[string]bool", true, program.TypeBool},
		{"string", "map[string]string", true, program.TypeString},
		{"named map", "Flags", false, program.TypeAny},
		{"named element", "map[string]Flag", false, program.TypeAny},
		{"other key", "map[int]bool", false, program.TypeAny},
	} {
		t.Run(tc.name, func(t *testing.T) {
			exprs, root, err := ParseExpr(`props.M[props.Key]`, nil)
			if err != nil {
				t.Fatal(err)
			}
			annotateTypedMapIndexes(exprs, map[string]string{"M": tc.declaration}, nil)
			got := exprs[root]
			if marked := got.Value == program.IndexTypedMap; marked != tc.want || (marked && got.Type != tc.typ) {
				t.Fatalf("index = %+v, want marked=%v type=%v", got, tc.want, tc.typ)
			}
			if exprs[got.Operands[0]].Value == program.IndexTypedMap {
				t.Fatal("props field selection was marked as a map lookup")
			}
		})
	}
}

func TestTypedMapIndexDoesNotAssumeShadowedBuiltins(t *testing.T) {
	for _, shadow := range []string{"bool", "string"} {
		exprs, root, err := ParseExpr(`props.M["k"]`, nil)
		if err != nil {
			t.Fatal(err)
		}
		annotateTypedMapIndexes(exprs, map[string]string{"M": "map[string]bool"}, map[string]bool{shadow: true})
		if exprs[root].Value == program.IndexTypedMap {
			t.Fatalf("marked index with shadowed %s: %+v", shadow, exprs[root])
		}
	}
}

func TestTypedMapIndexDoesNotUseRootTypesInComposedComponent(t *testing.T) {
	lowerer := newIslandLowerer(&Program{}, "Root", nil)
	lowerer.propsFields = map[string]string{"M": "map[string]bool"}
	context := newIslandInlineContext()
	context.component = "Child"
	context.props["M"] = islandInlineExpr{source: "other"}
	_, err := lowerer.addExprWithContext(`props.M["k"]`, context, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, expr := range lowerer.dst.Exprs {
		if expr.Value == program.IndexTypedMap {
			t.Fatalf("child used root map declaration: %+v", expr)
		}
	}
}
