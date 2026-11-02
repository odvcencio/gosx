package evalparity

import (
	"testing"

	"m31labs.dev/gosx/ir"
	"m31labs.dev/gosx/island/program"
)

func TestMapIndexWithShadowedBoolIsNotMarkedBuiltin(t *testing.T) {
	compiled, index, err := compileCase(Case{
		ID:          "shadowed_bool_map",
		ExtraTypes:  "type bool int",
		PropsFields: "M map[string]bool",
		Expr:        `props.M["missing"]`,
	})
	if err != nil {
		t.Fatal(err)
	}
	lowered, err := ir.LowerIsland(compiled, index)
	if err != nil {
		t.Fatal(err)
	}
	for _, expr := range lowered.Exprs {
		if expr.Op == program.OpIndex && expr.Value == program.IndexTypedMap {
			t.Fatalf("shadowed bool was treated as the predeclared type: %+v", expr)
		}
	}
}
