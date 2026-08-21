//go:build !tinygo

package gosx

import (
	"strings"
	"testing"

	gotreesitter "github.com/odvcencio/gotreesitter"
)

// Regressions found by the gotreesitter v0.20.4 to v0.47.0 upgrade.
//
// Each test pins a behaviour that the upgrade changed. Read the comment on
// each one before you relax it.

// TestExternalTokenIndicesResolveByName pins the scanner's index resolution.
//
// gsxScanner once hard-coded the external token indices 0, 1, and 2. The base
// Go grammar declared no externals then. Go later gained _automatic_semicolon,
// which took index 0 and moved every jsx_* token up by one. The fixed indices
// then read the wrong validSymbols slot and every parse failed.
func TestExternalTokenIndicesResolveByName(t *testing.T) {
	lang, err := Language()
	if err != nil {
		t.Fatalf("Language(): %v", err)
	}
	s := newGSXScanner(lang)

	for _, tc := range []struct {
		name string
		idx  int
	}{
		{gsxExternalNameAttributeExpression, s.idxAttributeExpression},
		{gsxExternalNameText, s.idxText},
		{gsxExternalNameScriptRawText, s.idxScriptRawText},
		{gsxExternalNameStyleRawText, s.idxStyleRawText},
		{gsxExternalNameAutoSemicolon, s.idxAutoSemicolon},
	} {
		if tc.idx < 0 {
			t.Errorf("external %q not found in the language", tc.name)
			continue
		}
		if tc.idx >= len(lang.ExternalSymbols) {
			t.Errorf("external %q index %d is out of range", tc.name, tc.idx)
			continue
		}
		got := lang.SymbolNames[lang.ExternalSymbols[tc.idx]]
		if got != tc.name {
			t.Errorf("external index %d holds %q, want %q", tc.idx, got, tc.name)
		}
	}
}

// TestAutomaticSemicolonKeepsTrailingBrace pins Go's automatic semicolon
// insertion.
//
// Go's terminator rule runs through the _automatic_semicolon external token.
// The scanner must take the newline as the token span. A zero-width match
// drops the final byte of the enclosing declaration, so the closing brace
// falls outside the function node.
func TestAutomaticSemicolonKeepsTrailingBrace(t *testing.T) {
	source := []byte("package main\n\nfunc App() Node {\n\treturn <div>hi</div>\n}\n")
	tree, lang, err := Parse(source)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	root := tree.RootNode()
	if root.HasError() {
		t.Fatalf("parse error:\n%s", dumpCST(root, lang))
	}
	var fn = firstNodeOfType(root, lang, "function_declaration")
	if fn == nil {
		t.Fatal("no function_declaration")
	}
	got := string(source[fn.StartByte():fn.EndByte()])
	if !strings.HasSuffix(got, "}") {
		t.Fatalf("function_declaration span drops its closing brace: %q", got)
	}
}

// TestCompileRejectsSourceWithoutPackageClause pins the garbage-input guard.
//
// The Go grammar accepts a file with no package clause and reports no error
// node, so text such as "not a valid gsx file" lexes as bare identifiers.
// Compile must still report an error, because callers read a compile error as
// the signal that the input is bad.
func TestCompileRejectsSourceWithoutPackageClause(t *testing.T) {
	for _, src := range []string{
		"not a valid gsx file",
		"this is not go source at all",
		"func App() Node { return <div>hi</div> }\n",
	} {
		if _, err := Compile([]byte(src)); err == nil {
			t.Errorf("Compile(%q) returned no error, want a package-clause error", src)
		}
	}
}

// TestCompileAcceptsLeadingComments checks the guard skips comments.
func TestCompileAcceptsLeadingComments(t *testing.T) {
	source := []byte("// Package demo shows a component.\npackage demo\n\nfunc App() Node {\n\treturn <div>hi</div>\n}\n")
	if _, err := Compile(source); err != nil {
		t.Fatalf("Compile with a leading comment: %v", err)
	}
}

// TestAttributeStringLiteralAcceptsEscapedQuotes pins escape support in
// attribute string values.
//
// jsx_string_literal once matched `"` then any run without a quote then `"`.
// An attribute such as `data-on-click="f(\"x\")"` therefore split into the
// value `"f(\"`, a bare attribute `x`, and an unparsable tail. That split
// produced no error node before gotreesitter v0.35.0, so the wrong parse
// shipped in silence.
func TestAttributeStringLiteralAcceptsEscapedQuotes(t *testing.T) {
	source := []byte("package demo\n\nfunc App() Node {\n\treturn <button data-on-click=\"theme.Set(\\\"light\\\")\">light</button>\n}\n")
	tree, lang, err := Parse(source)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	root := tree.RootNode()
	if root.HasError() {
		t.Fatalf("parse error:\n%s", dumpCST(root, lang))
	}

	open := firstNodeOfType(root, lang, "jsx_opening_element")
	if open == nil {
		t.Fatal("no jsx_opening_element")
	}
	var attrs []string
	for i := 0; i < open.NamedChildCount(); i++ {
		child := open.NamedChild(i)
		if child != nil && child.Type(lang) == "jsx_attribute" {
			attrs = append(attrs, string(source[child.StartByte():child.EndByte()]))
		}
	}
	if len(attrs) != 1 {
		t.Fatalf("got %d attributes %q, want 1", len(attrs), attrs)
	}
	const want = `data-on-click="theme.Set(\"light\")"`
	if attrs[0] != want {
		t.Fatalf("attribute = %q, want %q", attrs[0], want)
	}
}

// firstNodeOfType returns the first node of the given type in document order.
func firstNodeOfType(n *gotreesitter.Node, lang *gotreesitter.Language, typ string) *gotreesitter.Node {
	if n == nil {
		return nil
	}
	if n.Type(lang) == typ {
		return n
	}
	for i := 0; i < n.ChildCount(); i++ {
		if got := firstNodeOfType(n.Child(i), lang, typ); got != nil {
			return got
		}
	}
	return nil
}
