package gosx

// GosxGrammar returns a grammar extending Go with JSX-like component syntax.
// File extension: .gsx
//
// Supported syntax:
//
//	<div class="counter">...</div>       -> element tags
//	<Counter count={n} />                -> component tags (capitalized)
//	{expression}                         -> expression holes
//	<>...</>                             -> fragments
//	<div>{cond && <span>yes</span>}</div> -> conditional via Go expressions
func GosxGrammar() *Grammar {
	return ExtendGrammar("gosx", GoGrammar(), func(g *Grammar) {

		// ---------------------------------------------------------------
		// JSX element: <tag attr="val" attr={expr}>children</tag>
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

		// String attribute value: "hello"
		g.Define("jsx_string_literal",
			Token(Seq(
				Str(`"`),
				Pat(`[^"]*`),
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
			Seq(
				Field("name", Sym("jsx_attr_name")),
				Optional(Seq(
					Str("="),
					Field("value", Choice(
						Sym("jsx_string_literal"),
						Sym("jsx_expression_container"),
					)),
				)),
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

		// Text content between tags (no braces or angle brackets)
		g.Define("jsx_text",
			Token(Pat(`[^{}<>]+`)))

		// Child can be: element, expression, text, or fragment
		g.Define("_jsx_child",
			Choice(
				Sym("jsx_element"),
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

		// JSX expressions are valid Go expressions
		AppendChoice(g, "_expression", Choice(
			PrecDynamic(5, Sym("jsx_element")),
			PrecDynamic(5, Sym("jsx_self_closing_element")),
			PrecDynamic(5, Sym("jsx_fragment")),
		))
	})
}
