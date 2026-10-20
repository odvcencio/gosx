package vm

import (
	"testing"

	"m31labs.dev/gosx/island/program"
)

func binaryBooleanAttrProgram(t *testing.T) *program.Program {
	t.Helper()
	source := &program.Program{
		Name: "BooleanAttrs",
		Exprs: []program.Expr{
			{Op: program.OpSignalGet, Value: "enabled", Type: program.TypeBool},
			{Op: program.OpLitBool, Value: "false", Type: program.TypeBool},
			{Op: program.OpLitBool, Value: "true", Type: program.TypeBool},
			{Op: program.OpSignalSet, Value: "enabled", Operands: []program.ExprID{2}, Type: program.TypeBool},
			{Op: program.OpSignalSet, Value: "enabled", Operands: []program.ExprID{1}, Type: program.TypeBool},
		},
		Signals: []program.SignalDef{{Name: "enabled", Type: program.TypeBool, Init: 1}},
		Handlers: []program.Handler{
			{Name: "enable", Body: []program.ExprID{3}},
			{Name: "disable", Body: []program.ExprID{4}},
		},
		Nodes: []program.Node{{Kind: program.NodeElement, Tag: "input", Attrs: []program.Attr{
			{Kind: program.AttrExpr, Name: "hidden", Expr: 0},
			{Kind: program.AttrExpr, Name: "required", Expr: 0},
			{Kind: program.AttrExpr, Name: "disabled", Expr: 0},
			{Kind: program.AttrExpr, Name: "selected", Expr: 0},
			{Kind: program.AttrExpr, Name: "checked", Expr: 0},
			{Kind: program.AttrExpr, Name: "aria-pressed", Expr: 0},
			{Kind: program.AttrExpr, Name: "spellcheck", Expr: 0},
		}}},
		Root: 0,
	}
	data, err := program.EncodeBinary(source)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := program.DecodeBinary(data)
	if err != nil {
		t.Fatal(err)
	}
	return decoded
}

func TestBinaryIslandBooleanAttrsDiffAsPresence(t *testing.T) {
	island := NewIsland(binaryBooleanAttrProgram(t), `{}`)
	initial := island.CurrentTree().Nodes[0].Attrs
	if len(initial) != 2 || initial[0].Name != "aria-pressed" || initial[0].Value != "false" || initial[1].Name != "spellcheck" || initial[1].Value != "false" {
		t.Fatalf("initial attrs = %#v, want only textual false attrs", initial)
	}

	enabled := island.Dispatch("enable", `{}`)
	set := map[string]PatchOp{}
	for _, op := range enabled {
		if op.Kind == PatchSetAttr {
			set[op.AttrName] = op
		}
	}
	for _, name := range []string{"hidden", "required", "disabled", "selected", "checked"} {
		op, ok := set[name]
		if !ok || op.Text != "" {
			t.Fatalf("enable patches = %#v, want presence SetAttr for %s", enabled, name)
		}
	}
	if set["aria-pressed"].Text != "true" || set["spellcheck"].Text != "true" {
		t.Fatalf("non-boolean attrs must retain textual true: %#v", enabled)
	}

	disabled := island.Dispatch("disable", `{}`)
	removed := map[string]bool{}
	for _, op := range disabled {
		if op.Kind == PatchRemoveAttr {
			removed[op.AttrName] = true
		}
	}
	for _, name := range []string{"hidden", "required", "disabled", "selected", "checked"} {
		if !removed[name] {
			t.Fatalf("disable patches = %#v, want RemoveAttr for %s", disabled, name)
		}
	}
}
