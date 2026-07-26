//go:build !tinygo

package gosx

import gotreesitter "github.com/odvcencio/gotreesitter"

// gsxScanner lexes GSX externals. The CST still exposes `jsx_*` token names
// for compatibility with the generated grammar, but the scanner behavior is
// GSX-specific and Go-native.
type gsxScanner struct {
	lang *gotreesitter.Language
}

// Keep these in the same order as GosxGrammar appends g.Externals.
const (
	gsxExternalAttributeExpression = iota
	gsxExternalText
	gsxExternalRawText
)

func (s *gsxScanner) Create() any { return nil }

func (s *gsxScanner) Destroy(payload any) {}

func (s *gsxScanner) Serialize(payload any, buf []byte) int { return 0 }

func (s *gsxScanner) Deserialize(payload any, buf []byte) {}

func (s *gsxScanner) SupportsIncrementalReuse() bool { return true }

func (s *gsxScanner) Scan(payload any, lexer *gotreesitter.ExternalLexer, validSymbols []bool) bool {
	if s == nil || s.lang == nil {
		return false
	}
	// Raw text is only valid immediately inside <script>/<style>, so the
	// parser's validSymbols tells us when to swallow the body verbatim.
	// Check it before the ordinary text scan: inside a raw-text element the
	// `<` and `{` terminators of scanGSXText do not apply.
	if gsxValid(validSymbols, gsxExternalRawText) {
		if s.scanRawText(lexer) {
			return true
		}
	}
	if gsxValid(validSymbols, gsxExternalAttributeExpression) && lexer.Lookahead() == '{' {
		return s.scanAttributeExpression(lexer)
	}
	if gsxValid(validSymbols, gsxExternalText) {
		if s.scanGSXText(lexer) {
			return true
		}
	}
	return false
}

// rawTextCloseTags are the closing tags that terminate a raw-text body. HTML
// forbids nesting these elements, so the first match always closes the body
// the scanner is currently inside.
var rawTextCloseTags = []string{"script", "style"}

// scanRawText consumes a <script>/<style> body together with its closing tag,
// emitting the whole span as one jsx_raw_text token. `<` and `{` carry no GSX
// meaning here — `if (a < b) { f(); }` is script source, not an element
// followed by an expression hole.
//
// The closing tag is part of the token because the grammar rule owns no
// separate closing element; see jsx_raw_text_element in grammar.go for why.
// RawTextBody strips the tag back off for consumers.
//
// Returning false when no closing tag is found is essential: the parser can
// offer jsx_raw_text in states that are not really inside a raw-text element,
// and a greedy scan would otherwise swallow the rest of the file.
func (s *gsxScanner) scanRawText(lexer *gotreesitter.ExternalLexer) bool {
	// A body whose first non-space character is `{` is a GSX expression hole,
	// not script source: `<script>{ClientScript()}</script>` injects the value
	// of a Go call. Declining here hands the position to
	// jsx_expression_container. Returning false discards whatever this scan
	// advanced over, so the parser re-reads from the same place.
	//
	// The cost is that an inline script cannot OPEN with a bare JS block. Put
	// a statement before it, or move the script to a .js asset. Interpolation
	// is the far more common shape and predates raw-text elements.
	for {
		ch := lexer.Lookahead()
		if ch == 0 {
			return false
		}
		if ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' {
			lexer.Advance(false)
			continue
		}
		if ch == '{' {
			return false
		}
		break
	}

	for {
		ch := lexer.Lookahead()
		if ch == 0 {
			return false
		}
		if ch == '<' && s.consumeRawTextCloseTag(lexer) {
			lexer.MarkEnd()
			lexer.SetResultSymbol(s.lang.ExternalSymbols[gsxExternalRawText])
			return true
		}
		lexer.Advance(false)
	}
}

// consumeRawTextCloseTag advances over `</script>` or `</style>` (tag names are
// case-insensitive in HTML) and reports whether it matched. On a non-match the
// lexer has still advanced; scanRawText treats those characters as body text,
// which is correct because they were not a closing tag.
func (s *gsxScanner) consumeRawTextCloseTag(lexer *gotreesitter.ExternalLexer) bool {
	lexer.Advance(false) // '<'
	if lexer.Lookahead() != '/' {
		return false
	}
	lexer.Advance(false)

	var name []rune
	for {
		ch := lexer.Lookahead()
		if ch >= 'A' && ch <= 'Z' {
			ch += 'a' - 'A'
		}
		if ch < 'a' || ch > 'z' {
			break
		}
		name = append(name, ch)
		if len(name) > len("script") {
			return false
		}
		lexer.Advance(false)
	}

	if !isRawTextTag(string(name)) {
		return false
	}
	// Allow whitespace before `>`, as HTML does: `</script >`.
	for {
		ch := lexer.Lookahead()
		if ch != ' ' && ch != '\t' && ch != '\n' && ch != '\r' {
			break
		}
		lexer.Advance(false)
	}
	if lexer.Lookahead() != '>' {
		return false
	}
	lexer.Advance(false)
	return true
}

func isRawTextTag(name string) bool {
	for _, tag := range rawTextCloseTags {
		if name == tag {
			return true
		}
	}
	return false
}

// scanGSXText consumes characters that are valid inside GSX text (anything
// other than `{`, `<`, or end-of-input) and emits the jsx_text CST token.
// Unlike the regex-based internal lexer, the scanner can begin a text token
// immediately after a closing tag without requiring leading whitespace, which
// fixes parses like `<p>a<span>b</span>c</p>`.
func (s *gsxScanner) scanGSXText(lexer *gotreesitter.ExternalLexer) bool {
	consumed := 0
	for {
		ch := lexer.Lookahead()
		if ch == 0 || ch == '<' || ch == '{' {
			break
		}
		lexer.Advance(false)
		consumed++
	}
	if consumed == 0 {
		return false
	}
	lexer.MarkEnd()
	lexer.SetResultSymbol(s.lang.ExternalSymbols[gsxExternalText])
	return true
}

func (s *gsxScanner) scanAttributeExpression(lexer *gotreesitter.ExternalLexer) bool {
	depth := 0
	for {
		ch := lexer.Lookahead()
		if ch == 0 {
			return false
		}
		switch ch {
		case '{':
			depth++
			lexer.Advance(false)
		case '}':
			depth--
			lexer.Advance(false)
			if depth == 0 {
				lexer.MarkEnd()
				lexer.SetResultSymbol(s.lang.ExternalSymbols[gsxExternalAttributeExpression])
				return true
			}
		case '"':
			scanQuotedGoLiteral(lexer, '"')
		case '\'':
			scanQuotedGoLiteral(lexer, '\'')
		case '`':
			scanRawGoString(lexer)
		case '/':
			lexer.Advance(false)
			switch lexer.Lookahead() {
			case '/':
				scanGoLineComment(lexer)
			case '*':
				if !scanGoBlockComment(lexer) {
					return false
				}
			}
		default:
			lexer.Advance(false)
		}
	}
}

func gsxValid(vs []bool, idx int) bool { return idx < len(vs) && vs[idx] }

func scanQuotedGoLiteral(lexer *gotreesitter.ExternalLexer, quote rune) {
	lexer.Advance(false)
	for {
		ch := lexer.Lookahead()
		if ch == 0 {
			return
		}
		lexer.Advance(false)
		if ch == '\\' {
			if lexer.Lookahead() != 0 {
				lexer.Advance(false)
			}
			continue
		}
		if ch == quote {
			return
		}
	}
}

func scanRawGoString(lexer *gotreesitter.ExternalLexer) {
	lexer.Advance(false)
	for {
		ch := lexer.Lookahead()
		if ch == 0 {
			return
		}
		lexer.Advance(false)
		if ch == '`' {
			return
		}
	}
}

func scanGoLineComment(lexer *gotreesitter.ExternalLexer) {
	lexer.Advance(false)
	for {
		ch := lexer.Lookahead()
		if ch == 0 || ch == '\n' {
			return
		}
		lexer.Advance(false)
	}
}

func scanGoBlockComment(lexer *gotreesitter.ExternalLexer) bool {
	lexer.Advance(false)
	for {
		ch := lexer.Lookahead()
		if ch == 0 {
			return false
		}
		lexer.Advance(false)
		if ch == '*' && lexer.Lookahead() == '/' {
			lexer.Advance(false)
			return true
		}
	}
}

func stripGSXAttributeExpression(text string) string {
	if len(text) >= 2 && text[0] == '{' && text[len(text)-1] == '}' {
		return text[1 : len(text)-1]
	}
	return text
}
