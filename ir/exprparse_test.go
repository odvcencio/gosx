package ir

import (
	"testing"

	"github.com/odvcencio/gosx/island/program"
)

func TestParseIntLiteral(t *testing.T) {
	exprs, rootID, err := ParseExpr("42", &ExprScope{})
	if err != nil {
		t.Fatal(err)
	}
	if exprs[rootID].Op != program.OpLitInt {
		t.Fatal("expected LitInt")
	}
	if exprs[rootID].Value != "42" {
		t.Fatalf("expected 42, got %s", exprs[rootID].Value)
	}
}

func TestParseNegativeIntLiteral(t *testing.T) {
	exprs, rootID, err := ParseExpr("-3", &ExprScope{})
	if err != nil {
		t.Fatal(err)
	}
	if exprs[rootID].Op != program.OpLitInt {
		t.Fatal("expected LitInt")
	}
	if exprs[rootID].Value != "-3" {
		t.Fatalf("expected -3, got %s", exprs[rootID].Value)
	}
}

func TestParseFloatLiteral(t *testing.T) {
	exprs, rootID, err := ParseExpr("3.14", &ExprScope{})
	if err != nil {
		t.Fatal(err)
	}
	if exprs[rootID].Op != program.OpLitFloat {
		t.Fatal("expected LitFloat")
	}
	if exprs[rootID].Value != "3.14" {
		t.Fatalf("expected 3.14, got %s", exprs[rootID].Value)
	}
}

func TestParseStringLiteral(t *testing.T) {
	exprs, rootID, err := ParseExpr(`"hello"`, &ExprScope{})
	if err != nil {
		t.Fatal(err)
	}
	if exprs[rootID].Op != program.OpLitString {
		t.Fatal("expected LitString")
	}
	if exprs[rootID].Value != "hello" {
		t.Fatalf("expected hello, got %s", exprs[rootID].Value)
	}
}

func TestParseBoolLiteral(t *testing.T) {
	exprs, rootID, err := ParseExpr("true", &ExprScope{})
	if err != nil {
		t.Fatal(err)
	}
	if exprs[rootID].Op != program.OpLitBool {
		t.Fatal("expected LitBool")
	}
}

func TestParseBoolFalse(t *testing.T) {
	exprs, rootID, err := ParseExpr("false", &ExprScope{})
	if err != nil {
		t.Fatal(err)
	}
	if exprs[rootID].Op != program.OpLitBool {
		t.Fatal("expected LitBool")
	}
	if exprs[rootID].Value != "false" {
		t.Fatalf("expected false, got %s", exprs[rootID].Value)
	}
}

func TestParseSignalGet(t *testing.T) {
	scope := &ExprScope{Signals: map[string]bool{"count": true}}
	exprs, rootID, err := ParseExpr("count", scope)
	if err != nil {
		t.Fatal(err)
	}
	if exprs[rootID].Op != program.OpSignalGet {
		t.Fatal("expected SignalGet")
	}
	if exprs[rootID].Value != "count" {
		t.Fatal("expected count")
	}
}

func TestParsePropGet(t *testing.T) {
	scope := &ExprScope{Props: map[string]bool{"name": true}}
	exprs, rootID, err := ParseExpr("name", scope)
	if err != nil {
		t.Fatal(err)
	}
	if exprs[rootID].Op != program.OpPropGet {
		t.Fatal("expected PropGet")
	}
}

func TestParseAddition(t *testing.T) {
	scope := &ExprScope{Signals: map[string]bool{"a": true, "b": true}}
	exprs, rootID, err := ParseExpr("a + b", scope)
	if err != nil {
		t.Fatal(err)
	}
	if exprs[rootID].Op != program.OpAdd {
		t.Fatal("expected Add")
	}
	if len(exprs[rootID].Operands) != 2 {
		t.Fatal("expected 2 operands")
	}
}

func TestParseSubtraction(t *testing.T) {
	scope := &ExprScope{Signals: map[string]bool{"a": true, "b": true}}
	exprs, rootID, err := ParseExpr("a - b", scope)
	if err != nil {
		t.Fatal(err)
	}
	if exprs[rootID].Op != program.OpSub {
		t.Fatal("expected Sub")
	}
}

func TestParseMultiplication(t *testing.T) {
	scope := &ExprScope{Signals: map[string]bool{"a": true, "b": true}}
	exprs, rootID, err := ParseExpr("a * b", scope)
	if err != nil {
		t.Fatal(err)
	}
	if exprs[rootID].Op != program.OpMul {
		t.Fatal("expected Mul")
	}
}

func TestParseComparison(t *testing.T) {
	scope := &ExprScope{Signals: map[string]bool{"x": true}}
	exprs, rootID, err := ParseExpr("x > 5", scope)
	if err != nil {
		t.Fatal(err)
	}
	if exprs[rootID].Op != program.OpGt {
		t.Fatal("expected Gt")
	}
}

func TestParseEquality(t *testing.T) {
	scope := &ExprScope{Signals: map[string]bool{"x": true}}
	exprs, rootID, err := ParseExpr("x == 5", scope)
	if err != nil {
		t.Fatal(err)
	}
	if exprs[rootID].Op != program.OpEq {
		t.Fatal("expected Eq")
	}
}

func TestParseNotEqual(t *testing.T) {
	scope := &ExprScope{Signals: map[string]bool{"x": true}}
	exprs, rootID, err := ParseExpr("x != 0", scope)
	if err != nil {
		t.Fatal(err)
	}
	if exprs[rootID].Op != program.OpNeq {
		t.Fatal("expected Neq")
	}
}

func TestParseLessEqual(t *testing.T) {
	scope := &ExprScope{Signals: map[string]bool{"x": true}}
	exprs, rootID, err := ParseExpr("x <= 10", scope)
	if err != nil {
		t.Fatal(err)
	}
	if exprs[rootID].Op != program.OpLte {
		t.Fatal("expected Lte")
	}
}

func TestParseGreaterEqual(t *testing.T) {
	scope := &ExprScope{Signals: map[string]bool{"x": true}}
	exprs, rootID, err := ParseExpr("x >= 0", scope)
	if err != nil {
		t.Fatal(err)
	}
	if exprs[rootID].Op != program.OpGte {
		t.Fatal("expected Gte")
	}
}

func TestParseLogicalAnd(t *testing.T) {
	scope := &ExprScope{Signals: map[string]bool{"a": true, "b": true}}
	exprs, rootID, err := ParseExpr("a && b", scope)
	if err != nil {
		t.Fatal(err)
	}
	if exprs[rootID].Op != program.OpAnd {
		t.Fatal("expected And")
	}
}

func TestParseLogicalOr(t *testing.T) {
	scope := &ExprScope{Signals: map[string]bool{"a": true, "b": true}}
	exprs, rootID, err := ParseExpr("a || b", scope)
	if err != nil {
		t.Fatal(err)
	}
	if exprs[rootID].Op != program.OpOr {
		t.Fatal("expected Or")
	}
}

func TestParseSignalSetMethod(t *testing.T) {
	scope := &ExprScope{Signals: map[string]bool{"count": true}}
	exprs, rootID, err := ParseExpr("count.Set(0)", scope)
	if err != nil {
		t.Fatal(err)
	}
	if exprs[rootID].Op != program.OpSignalSet {
		t.Fatal("expected SignalSet")
	}
	if exprs[rootID].Value != "count" {
		t.Fatal("expected count")
	}
}

func TestParseSignalGetMethod(t *testing.T) {
	scope := &ExprScope{Signals: map[string]bool{"count": true}}
	exprs, rootID, err := ParseExpr("count.Get()", scope)
	if err != nil {
		t.Fatal(err)
	}
	if exprs[rootID].Op != program.OpSignalGet {
		t.Fatal("expected SignalGet")
	}
}

func TestParseSignalUpdateMethod(t *testing.T) {
	scope := &ExprScope{
		Signals:  map[string]bool{"count": true},
		Handlers: map[string]bool{"increment": true},
	}
	exprs, rootID, err := ParseExpr("count.Update(increment)", scope)
	if err != nil {
		t.Fatal(err)
	}
	if exprs[rootID].Op != program.OpSignalUpdate {
		t.Fatal("expected SignalUpdate")
	}
	if exprs[rootID].Value != "count" {
		t.Fatal("expected count")
	}
}

func TestParseHandlerCall(t *testing.T) {
	scope := &ExprScope{Handlers: map[string]bool{"handleClick": true}}
	exprs, rootID, err := ParseExpr("handleClick()", scope)
	if err != nil {
		t.Fatal(err)
	}
	if exprs[rootID].Op != program.OpCall {
		t.Fatal("expected Call")
	}
	if exprs[rootID].Value != "handleClick" {
		t.Fatal("expected handleClick")
	}
}

func TestParseNegation(t *testing.T) {
	scope := &ExprScope{Signals: map[string]bool{"x": true}}
	exprs, rootID, err := ParseExpr("!x", scope)
	if err != nil {
		t.Fatal(err)
	}
	if exprs[rootID].Op != program.OpNot {
		t.Fatal("expected Not")
	}
}

func TestParseUnaryMinus(t *testing.T) {
	scope := &ExprScope{Signals: map[string]bool{"x": true}}
	exprs, rootID, err := ParseExpr("-x", scope)
	if err != nil {
		t.Fatal(err)
	}
	if exprs[rootID].Op != program.OpNeg {
		t.Fatal("expected Neg")
	}
}

func TestParseUnknownIdentifier(t *testing.T) {
	_, _, err := ParseExpr("unknown", &ExprScope{})
	if err == nil {
		t.Fatal("expected error for unknown identifier")
	}
}

func TestParseGoroutineRejected(t *testing.T) {
	_, _, err := ParseExpr("go func(){}", &ExprScope{})
	if err == nil {
		t.Fatal("expected error for goroutine launch")
	}
}

func TestParseChannelReceiveRejected(t *testing.T) {
	_, _, err := ParseExpr("<-ch", &ExprScope{})
	if err == nil {
		t.Fatal("expected error for channel receive")
	}
}

func TestParseEmptyString(t *testing.T) {
	_, _, err := ParseExpr("", &ExprScope{})
	if err == nil {
		t.Fatal("expected error for empty expression")
	}
}
