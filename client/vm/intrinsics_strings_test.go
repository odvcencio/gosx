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
	if len(v.List()) != 3 || v.List()[0].Text() != "a" || v.List()[1].Text() != "b" || v.List()[2].Text() != "c" {
		t.Errorf("Split = %+v, want [a b c]", v.List())
	}
}

func TestStringsJoin(t *testing.T) {
	fn, _ := LookupIntrinsic("strings.Join")
	v, _ := fn([]Value{
		ArrayVal([]Value{StringVal("a"), StringVal("b"), StringVal("c")}),
		StringVal("-"),
	})
	if v.Text() != "a-b-c" {
		t.Errorf("Join = %q, want %q", v.Text(), "a-b-c")
	}
}

func TestStringsTrimSpace(t *testing.T) {
	fn, _ := LookupIntrinsic("strings.TrimSpace")
	v, _ := fn([]Value{StringVal("  hi  ")})
	if v.Text() != "hi" {
		t.Errorf("TrimSpace = %q, want %q", v.Text(), "hi")
	}
}

func TestStringsToLowerUpper(t *testing.T) {
	low, _ := LookupIntrinsic("strings.ToLower")
	up, _ := LookupIntrinsic("strings.ToUpper")
	v, _ := low([]Value{StringVal("HeLLo")})
	if v.Text() != "hello" {
		t.Errorf("ToLower = %q", v.Text())
	}
	v, _ = up([]Value{StringVal("HeLLo")})
	if v.Text() != "HELLO" {
		t.Errorf("ToUpper = %q", v.Text())
	}
}

func TestStringsReplaceVariants(t *testing.T) {
	fn, _ := LookupIntrinsic("strings.Replace")
	// 3 args → ReplaceAll
	v, _ := fn([]Value{StringVal("a-b-a"), StringVal("a"), StringVal("X")})
	if v.Text() != "X-b-X" {
		t.Errorf("ReplaceAll = %q, want X-b-X", v.Text())
	}
	// 4 args → Replace with n
	v, _ = fn([]Value{StringVal("a-b-a"), StringVal("a"), StringVal("X"), IntVal(1)})
	if v.Text() != "X-b-a" {
		t.Errorf("Replace(n=1) = %q, want X-b-a", v.Text())
	}
}

func TestStringsContainsPrefixSuffix(t *testing.T) {
	c, _ := LookupIntrinsic("strings.Contains")
	p, _ := LookupIntrinsic("strings.HasPrefix")
	s, _ := LookupIntrinsic("strings.HasSuffix")
	v, _ := c([]Value{StringVal("foobar"), StringVal("oob")})
	if !v.Truth() {
		t.Error("Contains(foobar, oob) should be true")
	}
	v, _ = p([]Value{StringVal("foobar"), StringVal("foo")})
	if !v.Truth() {
		t.Error("HasPrefix(foobar, foo) should be true")
	}
	v, _ = s([]Value{StringVal("foobar"), StringVal("bar")})
	if !v.Truth() {
		t.Error("HasSuffix(foobar, bar) should be true")
	}
}
