package route

import "strings"

// GSX-only plain-value ternary support for `{expr}` holes.
//
// WHY: `cond ? cons : alt` is not valid Go syntax — go/parser.ParseExpr
// rejects the "?" token unconditionally, the same way real Go does. The
// JSX-branched form (`cond ? <a> : <b>`) never reaches this file at all:
// ir/lower.go's lowerConditionalExprContainer recognizes it at compile
// time, while the tree still carries structure, and rewrites it into <If>
// subtrees before either branch's text ever becomes a NodeExpr hole. A
// PLAIN-value ternary (both branches ordinary expressions, e.g.
// `props.Flag ? "yes" : "no"`) has no JSX to rewrite around, so
// lowerConditionalExprContainer explicitly leaves it as literal hole text
// (see its "plain value ternary — the DSL handles it as a hole" comment)
// for compiledFileExpr to evaluate directly — and until this file existed,
// nothing did: go/parser's rejection made compiledFileExpr cache a nil
// entry, so the hole silently rendered empty instead of "yes"
// (docs/expression-support-matrix.md's ternary_plain_value case).
//
// The island DSL's own parser (ir/exprparse.go) and transpile-through-
// real-Go both already had no such gap — transpile via <If>, the DSL via
// its own '?' handling — so this closes route as the last holdout.

// lowerTernaryExprSource lowers a plain-value ternary hole that go/parser
// has already rejected. ok is false when src has no top-level ternary at
// all (an ordinary malformed hole — compiledFileExpr's existing "render
// empty" fallback still applies) or when any of the three parts fails to
// lower on its own.
//
// Each part lowers through compiledFileExpr, not a direct parser call, so
// it shares the ordinary cache and — since GSX ternaries are
// right-associative (`a ? b : c ? d : e` means `a ? b : (c ? d : e)`,
// matching splitTopLevelTernary's matching rule) — a chained ternary in
// the alternative position lowers correctly through the same recursion,
// no special-casing needed.
func lowerTernaryExprSource(src string) (fileExprFunc, bool) {
	condSrc, consSrc, altSrc, ok := splitTopLevelTernary(src)
	if !ok {
		return nil, false
	}
	cond := compiledFileExpr(condSrc)
	cons := compiledFileExpr(consSrc)
	alt := compiledFileExpr(altSrc)
	if cond == nil || cons == nil || alt == nil {
		return nil, false
	}
	return func(env fileRenderEnv) any {
		if truthy(cond(env)) {
			return cons(env)
		}
		return alt(env)
	}, true
}

// splitTopLevelTernary finds src's outermost `cond ? cons : alt` and
// returns its three parts trimmed of surrounding whitespace. ok is false
// when src has no top-level '?' — the common case, since compiledFileExpr
// calls this only after go/parser has already rejected src for some
// reason, not all of which are a ternary.
//
// "Top-level" means outside (), [], {} nesting and outside any string,
// rune, or raw-string literal — the same exclusions go/parser itself
// would apply were "?" a legal operator. Byte-wise scanning is safe here
// because every rune this function treats specially ('(', ')', '[', ']',
// '{', '}', '"', '\”, '`', '?', ':', '\\') is ASCII, and UTF-8
// continuation bytes are always >= 0x80 — a multibyte rune inside a
// literal can never be misread as one of these.
//
// The matching ':' is found with a ternary-depth counter, not simply the
// first top-level ':', so a right-associative chain resolves correctly:
// `a ? b : c ? d : e` must split as cond="a" cons="b" alt="c ? d : e" (the
// alt re-splits on its own recursive compiledFileExpr call), not
// mis-pair the first '?' with the SECOND ':' just because both are
// top-level.
func splitTopLevelTernary(src string) (cond, cons, alt string, ok bool) {
	depth := 0
	questionAt := -1
	ternaryDepth := 0

	for i := 0; i < len(src); i++ {
		switch src[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case '"', '\'', '`':
			end := skipStringLiteral(src, i)
			if end < 0 {
				// Unterminated literal: src is malformed for a reason this
				// function isn't equipped to fix. Let the caller's existing
				// "not a ternary" fallback handle it.
				return "", "", "", false
			}
			i = end
		case '?':
			if depth != 0 {
				continue
			}
			if questionAt < 0 {
				questionAt = i
			}
			ternaryDepth++
		case ':':
			if depth != 0 || questionAt < 0 {
				continue
			}
			ternaryDepth--
			if ternaryDepth == 0 {
				return strings.TrimSpace(src[:questionAt]),
					strings.TrimSpace(src[questionAt+1 : i]),
					strings.TrimSpace(src[i+1:]),
					true
			}
		}
	}
	return "", "", "", false
}

// skipStringLiteral returns the index of the closing quote matching the
// string/rune/raw-string literal that starts at src[start], or -1 if src
// ends before the literal closes. A raw string (“ ` “) has no escape
// sequences in Go; the others honor a backslash escaping the next byte,
// including an escaped quote of the same kind.
func skipStringLiteral(src string, start int) int {
	quote := src[start]
	for i := start + 1; i < len(src); i++ {
		switch src[i] {
		case '\\':
			if quote != '`' {
				i++
			}
		case quote:
			return i
		}
	}
	return -1
}
