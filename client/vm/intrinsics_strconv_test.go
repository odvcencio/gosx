package vm

import (
	"testing"
)

func TestStrconvItoa(t *testing.T) {
	fn, _ := LookupIntrinsic("strconv.Itoa")
	v, _ := fn([]Value{IntVal(42)})
	if v.Str != "42" {
		t.Errorf("Itoa(42) = %q, want 42", v.Str)
	}
}

func TestStrconvAtoiOK(t *testing.T) {
	fn, _ := LookupIntrinsic("strconv.Atoi")
	v, err := fn([]Value{StringVal("42")})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if int(v.Num) != 42 {
		t.Errorf("Atoi(\"42\") = %f, want 42", v.Num)
	}
}

func TestStrconvAtoiError(t *testing.T) {
	fn, _ := LookupIntrinsic("strconv.Atoi")
	if _, err := fn([]Value{StringVal("abc")}); err == nil {
		t.Error("Atoi(\"abc\") should error")
	}
}

func TestStrconvFormatFloat(t *testing.T) {
	fn, _ := LookupIntrinsic("strconv.FormatFloat")
	// Go: strconv.FormatFloat(3.14, 'f', 2, 64) = "3.14"
	v, err := fn([]Value{FloatVal(3.14), StringVal("f"), IntVal(2), IntVal(64)})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v.Str != "3.14" {
		t.Errorf("FormatFloat = %q, want 3.14", v.Str)
	}
}

func TestStrconvParseFloat(t *testing.T) {
	fn, _ := LookupIntrinsic("strconv.ParseFloat")
	v, err := fn([]Value{StringVal("2.5"), IntVal(64)})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v.Num != 2.5 {
		t.Errorf("ParseFloat(\"2.5\") = %f, want 2.5", v.Num)
	}
}
