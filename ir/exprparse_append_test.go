package ir

import (
	"strings"
	"testing"

	"m31labs.dev/gosx/client/vm"
	"m31labs.dev/gosx/island/program"
)

func TestParseGoAppend(t *testing.T) {
	for _, tc := range []struct {
		source string
		want   []string
	}{
		{`append([]string{}, "GoSX")`, []string{"GoSX"}},
		{`append([]string{"Go"}, "GoSX", "Selena")`, []string{"Go", "GoSX", "Selena"}},
		{`append([]string{"GoSX"})`, []string{"GoSX"}},
		{`append(append([]string{}, "Go"), "GoSX")`, []string{"Go", "GoSX"}},
	} {
		t.Run(tc.source, func(t *testing.T) {
			exprs, root, err := ParseExpr(tc.source, &ExprScope{})
			if err != nil {
				t.Fatal(err)
			}
			machine := vm.NewVM(&program.Program{Exprs: exprs}, nil)
			got := machine.Eval(root).List()
			if len(got) != len(tc.want) {
				t.Fatalf("append length = %d, want %d", len(got), len(tc.want))
			}
			for i, want := range tc.want {
				if got[i].Text() != want {
					t.Fatalf("append[%d] = %q, want %q", i, got[i].Text(), want)
				}
			}
		})
	}
}

func TestParseGoAppendKeepsHandlerBinding(t *testing.T) {
	exprs, root, err := ParseExpr(`append()`, &ExprScope{Handlers: map[string]bool{"append": true}})
	if err != nil {
		t.Fatal(err)
	}
	if exprs[root].Op != program.OpCall || exprs[root].Value != "append" {
		t.Fatal("the builtin replaced a declared handler named append")
	}
}

func TestParseGoAppendPreservesEarlierSliceValue(t *testing.T) {
	exprs, root, err := ParseExpr(`append(seed, "GoSX", "Selena")`, &ExprScope{Props: map[string]bool{"seed": true}})
	if err != nil {
		t.Fatal(err)
	}
	backing := make([]vm.Value, 1, 4)
	backing[0] = vm.StringVal("Go")
	seed := vm.ArrayVal(backing)
	machine := vm.NewVM(&program.Program{Exprs: exprs}, map[string]vm.Value{"seed": seed})
	got := machine.Eval(root).List()
	got[0] = vm.StringVal("changed")
	if len(seed.List()) != 1 || seed.List()[0].Text() != "Go" || backing[:cap(backing)][1].Text() != "" {
		t.Fatal("append changed a previous slice value or its backing storage")
	}
}

func TestParseGoAppendRejectsMissingSlice(t *testing.T) {
	for _, source := range []string{`append()`, `append(42)`, `append("Go", "SX")`, `append(true, false)`} {
		_, _, err := ParseExpr(source, &ExprScope{})
		if err == nil || !strings.Contains(err.Error(), "append requires") {
			t.Fatalf("missing slice in %s: %v", source, err)
		}
	}
}
