package ir

import (
	"strings"
	"testing"

	"m31labs.dev/gosx/island/program"
)

func TestBrowserPointerCaptureCompiles(t *testing.T) {
	scope := &ExprScope{Browser: true, EventFields: map[string]bool{"pointerID": true}}
	exprs, root, err := ParseExpr("browser.CapturePointer(pointerID)", scope)
	if err != nil {
		t.Fatal(err)
	}
	got := exprs[root]
	if got.Op != program.OpHostCall || got.Value != "browser.CapturePointer" || got.Type != program.TypeBool || len(got.Operands) != 1 {
		t.Fatalf("expr = %#v", got)
	}
	if _, _, err := ParseExpr("browser.ReleasePointer(pointerID)", scope); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ParseExpr("browser.CapturePointer()", scope); err == nil || !strings.Contains(err.Error(), "exactly 1 effect argument") {
		t.Fatalf("arity error = %v", err)
	}
}
