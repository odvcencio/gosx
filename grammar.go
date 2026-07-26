//go:build !tinygo

package gosx

// GosxGrammar returns a grammar extending Go with native GSX component syntax.
// File extension: .gsx
//
// Supported syntax:
//
//	<div class="counter">...</div>       -> element tags
//	<Counter count={n} />                -> component tags (capitalized)
//	{expression}                         -> expression holes
//	{cond ? a : b}                       -> ternary conditional (text or attribute)
//	<>...</>                             -> fragments
//	<div>{cond && <span>yes</span>}</div> -> conditional via Go expressions
func GosxGrammar() *Grammar {
	return ExtendGrammar("gosx", GoGrammar(), func(g *Grammar) {

		// The generated CST keeps `jsx_*` node names for compatibility with the
		// existing lowering, formatting, and transpile pipeline. The language
		// surface, editor tooling, and public docs should still treat this as GSX.

		// ---------------------------------------------------------------
		// GSX element: <tag attr="val" attr={expr}>children</tag>
		// ---------------------------------------------------------------

		// Tag names: identifiers, custom elements, or dotted paths (pkg.Component)
		g.Define("jsx_identifier",
			Pat(`[a-zA-Z_][a-zA-Z0-9_]*`))

		g.Define("jsx_html_tag_name",
			Pat(`[a-z][a-zA-Z0-9_-]*`))

		g.Define("jsx_dotted_name",
			Seq(
				Field("object", Sym("jsx_identifier")),
				Str("."),
				Field("property", Sym("jsx_identifier")),
			))

		g.Define("jsx_tag_name",
			Choice(
				Sym("jsx_dotted_name"),
				Sym("jsx_html_tag_name"),
				Sym("jsx_identifier"),
			))

		// ---------------------------------------------------------------
		// Attributes
		// ---------------------------------------------------------------

		g.Externals = append(g.Externals, Sym("jsx_attribute_expression"))

		// String attribute value: "hello" or "f(\"x\")".
		//
		// A backslash escapes the next character, so an attribute value can
		// carry a double quote. Without the escape branch the lexer stops at
		// the first inner quote: `data-on-click="f(\"x\")"` then lexes as the
		// value `"f(\"`, a bare boolean attribute `x`, and a trailing run that
		// no rule accepts.
		//
		// Before gotreesitter v0.35.0 that split produced no error node, so
		// the wrong parse shipped in silence. The parser now reports it, which
		// is how this gap surfaced.
		//
		// The whole literal stays one token, so consumers keep reading the
		// value with Node.Text and trimming the quotes.
		g.Define("jsx_string_literal",
			Token(Seq(
				Str(`"`),
				Repeat(Choice(
					Pat(`[^"\\]+`),
					Pat(`\\.`),
				)),
				Str(`"`),
			)))

		// Expression container: {expr}
		g.Define("jsx_expression_container",
			Seq(
				Str("{"),
				Field("expression", Sym("_expression")),
				Str("}"),
			))

		g.Define("jsx_attr_name",
			Pat(`[a-zA-Z_][a-zA-Z0-9_:-]*`))

		// Attribute: name="value" or name={expr} or name (boolean)
		g.Define("jsx_attribute",
			Choice(
				Seq(
					Field("name", Sym("jsx_attr_name")),
					Str("="),
					Field("value", Sym("jsx_attribute_expression")),
				),
				Seq(
					Field("name", Sym("jsx_attr_name")),
					Str("="),
					Field("value", Sym("jsx_string_literal")),
				),
				Seq(
					Field("name", Sym("jsx_attr_name")),
				),
			))

		// Spread attribute: {...expr}
		g.Define("jsx_spread_attribute",
			Seq(
				Str("{"),
				Str("..."),
				Field("expression", Sym("_expression")),
				Str("}"),
			))

		// ---------------------------------------------------------------
		// Children
		// ---------------------------------------------------------------

		// Text content between tags. Produced by the external scanner so that
		// text can begin immediately after a closing tag (e.g. `<p>a<span>b</span>c</p>`)
		// without requiring intervening whitespace. The internal LR lexer cannot
		// resolve the boundary between a closing tag and adjacent text without
		// a whitespace separator; an external scanner has unconstrained
		// lookahead and can.
		g.Externals = append(g.Externals, Sym("jsx_text"))

		// Body of a raw-text element (<script>/<style>). Produced by the
		// external scanner, which runs it to the matching closing tag without
		// interpreting `<` or `{`. HTML gives these two elements the same
		// treatment: their content is script/stylesheet source, not markup.
		// Without this, JS like `if (a < b) { f(); }` parses as a GSX element
		// plus an expression hole and the body is silently corrupted.
		g.Externals = append(g.Externals, Sym("jsx_raw_text"))

		// Child can be: element, raw-text element, expression, text, or fragment
		g.Define("_jsx_child",
			Choice(
				Sym("jsx_element"),
				Sym("jsx_raw_text_element"),
				Sym("jsx_self_closing_element"),
				Sym("jsx_expression_container"),
				Sym("jsx_fragment"),
				Sym("jsx_text"),
			))

		// ---------------------------------------------------------------
		// Elements
		// ---------------------------------------------------------------

		// Opening tag: <tag attrs...>
		g.Define("jsx_opening_element",
			Seq(
				Str("<"),
				Field("name", Sym("jsx_tag_name")),
				Repeat(Field("attributes", Choice(
					Sym("jsx_attribute"),
					Sym("jsx_spread_attribute"),
				))),
				Str(">"),
			))

		// Closing tag: </tag>
		g.Define("jsx_closing_element",
			Seq(
				Str("<"),
				Str("/"),
				Field("name", Sym("jsx_tag_name")),
				Str(">"),
			))

		// Full element: <tag attrs>children</tag>
		g.Define("jsx_element",
			Seq(
				Field("open", Sym("jsx_opening_element")),
				Repeat(Field("children", Sym("_jsx_child"))),
				Field("close", Sym("jsx_closing_element")),
			))

		// ---------------------------------------------------------------
		// Raw-text elements (<script>, <style>)
		// ---------------------------------------------------------------

		// `<` and the tag name are ONE token so the raw-text path is chosen by
		// the lexer, not the parser. Splitting them (`<` then a name token)
		// gives jsx_opening_element and jsx_raw_opening_element a common `<`
		// prefix; the LR generator then resolves that state in favor of the
		// raw rule and every nested ordinary element (`<div><span>…`) fails.
		// Longest-match lexing keeps `<span` on the ordinary path because
		// `<script`/`<style` simply do not match it.
		g.Define("jsx_raw_text_start_tag",
			Token(Prec(1, Seq(
				Str("<"),
				Choice(Str("script"), Str("style")),
			))))

		g.Define("jsx_raw_opening_element",
			Seq(
				Field("name", Sym("jsx_raw_text_start_tag")),
				Repeat(Field("attributes", Choice(
					Sym("jsx_attribute"),
					Sym("jsx_spread_attribute"),
				))),
				Str(">"),
			))

		// <script>...</script> — body captured verbatim by the scanner.
		//
		// jsx_raw_text spans the body AND its closing tag, so this rule shares
		// no sub-rule with jsx_element. That separation is deliberate: when the
		// raw element ended with the ordinary jsx_closing_element, the LR
		// generator merged the two "inside an element" states and then offered
		// jsx_raw_text after every ordinary opening tag, which broke plain
		// `<div>text</div>`. Owning the close here keeps the automata disjoint.
		// Two body shapes:
		//   <script>if (a < b) { go(); }</script>   -> jsx_raw_text (script source)
		//   <script>{ClientScript()}</script>       -> expression hole (Go value)
		// The scanner decides between them by declining raw text when the body
		// opens with `{`, so only one of these alternatives is ever live at a
		// given position. The raw-text alternative carries its own closing tag
		// inside the token; the interpolated one needs an explicit close.
		g.Define("jsx_raw_text_element",
			Seq(
				Field("open", Sym("jsx_raw_opening_element")),
				Choice(
					Field("children", Sym("jsx_raw_text")),
					Seq(
						Field("children", Sym("jsx_expression_container")),
						Field("close", Sym("jsx_closing_element")),
					),
					Field("close", Sym("jsx_closing_element")),
				),
			))

		// Self-closing element: <tag attrs />
		g.Define("jsx_self_closing_element",
			Seq(
				Str("<"),
				Field("name", Sym("jsx_tag_name")),
				Repeat(Field("attributes", Choice(
					Sym("jsx_attribute"),
					Sym("jsx_spread_attribute"),
				))),
				Str("/"),
				Str(">"),
			))

		// Fragment: <>children</>
		g.Define("jsx_fragment",
			Seq(
				Str("<"),
				Str(">"),
				Repeat(Field("children", Sym("_jsx_child"))),
				Str("<"),
				Str("/"),
				Str(">"),
			))

		// ---------------------------------------------------------------
		// Island annotation (v0.2+ but IR supports it from v0.1)
		// ---------------------------------------------------------------
		// gosx:island directive on component
		g.Define("jsx_island_directive",
			Seq(
				Str("//gosx:island"),
			))

		// ---------------------------------------------------------------
		// Hook into Go grammar
		// ---------------------------------------------------------------

		// GSX tags are valid Go expressions
		AppendChoice(g, "_expression", Choice(
			PrecDynamic(5, Sym("jsx_element")),
			PrecDynamic(5, Sym("jsx_self_closing_element")),
			PrecDynamic(5, Sym("jsx_fragment")),
		))

		// Ternary conditional: `cond ? a : b`. Go has no ternary, but the island
		// expression DSL (ir/exprparse.go) does — and attribute `{...}` values
		// already accept it because they are raw-captured by the external
		// scanner. Adding it to _expression lets the same conditional appear in a
		// text-child `{...}` container (`<span>{c.Get() ? "a" : "b"}</span>`),
		// which previously errored as invalid Go. Lowering takes the node's raw
		// text, so the DSL parser evaluates it; no IR change is needed.
		// PrecRight(0) binds looser than `||` (binary level 1) and is
		// right-associative, matching C-family ternary.
		g.Define("gsx_ternary_expression",
			PrecRight(0, Seq(
				Field("condition", Sym("_expression")),
				Str("?"),
				Field("consequence", Sym("_expression")),
				Str(":"),
				Field("alternative", Sym("_expression")),
			)))
		AppendChoice(g, "_expression", Sym("gsx_ternary_expression"))
	})
}
