package vm

import (
	"testing"
)

func TestStringsSplit(t *testing.T) {
	fn, _ := LookupIntrinsic("strings.Split")
	v, err := fn([]Value{StringVal("a,b,c"), StringVal(",")})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(v.Items) != 3 || v.Items[0].Str != "a" || v.Items[1].Str != "b" || v.Items[2].Str != "c" {
		t.Errorf("Split = %+v, want [a b c]", v.Items)
	}
}

func TestStringsJoin(t *testing.T) {
	fn, _ := LookupIntrinsic("strings.Join")
	v, _ := fn([]Value{
		ArrayVal([]Value{StringVal("a"), StringVal("b"), StringVal("c")}),
		StringVal("-"),
	})
	if v.Str != "a-b-c" {
		t.Errorf("Join = %q, want %q", v.Str, "a-b-c")
	}
}

func TestStringsTrimSpace(t *testing.T) {
	fn, _ := LookupIntrinsic("strings.TrimSpace")
	v, _ := fn([]Value{StringVal("  hi  ")})
	if v.Str != "hi" {
		t.Errorf("TrimSpace = %q, want %q", v.Str, "hi")
	}
}

func TestStringsToLowerUpper(t *testing.T) {
	low, _ := LookupIntrinsic("strings.ToLower")
	up, _ := LookupIntrinsic("strings.ToUpper")
	v, _ := low([]Value{StringVal("HeLLo")})
	if v.Str != "hello" {
		t.Errorf("ToLower = %q", v.Str)
	}
	v, _ = up([]Value{StringVal("HeLLo")})
	if v.Str != "HELLO" {
		t.Errorf("ToUpper = %q", v.Str)
	}
}

func TestStringsReplaceVariants(t *testing.T) {
	fn, _ := LookupIntrinsic("strings.Replace")
	// 3 args → ReplaceAll
	v, _ := fn([]Value{StringVal("a-b-a"), StringVal("a"), StringVal("X")})
	if v.Str != "X-b-X" {
		t.Errorf("ReplaceAll = %q, want X-b-X", v.Str)
	}
	// 4 args → Replace with n
	v, _ = fn([]Value{StringVal("a-b-a"), StringVal("a"), StringVal("X"), IntVal(1)})
	if v.Str != "X-b-a" {
		t.Errorf("Replace(n=1) = %q, want X-b-a", v.Str)
	}
}

func TestStringsContainsPrefixSuffix(t *testing.T) {
	c, _ := LookupIntrinsic("strings.Contains")
	p, _ := LookupIntrinsic("strings.HasPrefix")
	s, _ := LookupIntrinsic("strings.HasSuffix")
	v, _ := c([]Value{StringVal("foobar"), StringVal("oob")})
	if !v.Bool {
		t.Error("Contains(foobar, oob) should be true")
	}
	v, _ = p([]Value{StringVal("foobar"), StringVal("foo")})
	if !v.Bool {
		t.Error("HasPrefix(foobar, foo) should be true")
	}
	v, _ = s([]Value{StringVal("foobar"), StringVal("bar")})
	if !v.Bool {
		t.Error("HasSuffix(foobar, bar) should be true")
	}
}
