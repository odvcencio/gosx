//go:build !tinygo

package gosx

import gotreesitter "github.com/odvcencio/gotreesitter"

// External token names. GosxGrammar appends the three jsx_* externals to the
// base Go grammar, which owns _automatic_semicolon.
//
// Resolve these names against the language at run time. Do not hard-code the
// external indices. GoGrammar declared no externals until gotreesitter
// v0.35.0, so the jsx_* tokens once sat at 0, 1, and 2. Go then gained real
// automatic semicolon insertion through _automatic_semicolon, which took
// index 0 and pushed every jsx_* token up by one. Fixed indices read the
// wrong validSymbols slot after that change and every parse failed, Go source
// included.
const (
	gsxExternalNameAttributeExpression = "jsx_attribute_expression"
	gsxExternalNameText                = "jsx_text"
	gsxExternalNameRawText             = "jsx_raw_text"
	gsxExternalNameAutoSemicolon       = "_automatic_semicolon"
)

// gsxScanner lexes GSX externals. The CST still exposes `jsx_*` token names
// for compatibility with the generated grammar, but the scanner behavior is
// GSX-specific and Go-native.
//
// The scanner also owns the base Go grammar's _automatic_semicolon token,
// because a language carries exactly one external scanner and GoSX extends Go.
type gsxScanner struct {
	lang *gotreesitter.Language

	// External token indices into validSymbols. A value of -1 means the
	// language does not declare that external.
	idxAttributeExpression int
	idxText                int
	idxRawText             int
	idxAutoSemicolon       int
}

// newGSXScanner binds a scanner to lang and resolves the external indices.
func newGSXScanner(lang *gotreesitter.Language) *gsxScanner {
	s := &gsxScanner{
		lang:                   lang,
		idxAttributeExpression: externalIndexByName(lang, gsxExternalNameAttributeExpression),
		idxText:                externalIndexByName(lang, gsxExternalNameText),
		idxRawText:             externalIndexByName(lang, gsxExternalNameRawText),
		idxAutoSemicolon:       externalIndexByName(lang, gsxExternalNameAutoSemicolon),
	}
	return s
}

// externalIndexByName returns the validSymbols index of the named external
// token, or -1 when the language does not declare it.
func externalIndexByName(lang *gotreesitter.Language, name string) int {
	if lang == nil {
		return -1
	}
	for i, sym := range lang.ExternalSymbols {
		if int(sym) < len(lang.SymbolNames) && lang.SymbolNames[sym] == name {
			return i
		}
	}
	return -1
}

// externalSymbol returns the concrete symbol ID for an external index.
func (s *gsxScanner) externalSymbol(idx int) gotreesitter.Symbol {
	return s.lang.ExternalSymbols[idx]
}

func (s *gsxScanner) Create() any { return nil }

func (s *gsxScanner) Destroy(payload any) {}

func (s *gsxScanner) Serialize(payload any, buf []byte) int { return 0 }

func (s *gsxScanner) Deserialize(payload any, buf []byte) {}

func (s *gsxScanner) SupportsIncrementalReuse() bool { return true }

func (s *gsxScanner) ExternalScannerIsStateless() bool { return true }

func (s *gsxScanner) PreservesStateOnScanFailure() bool { return true }

func (s *gsxScanner) Scan(payload any, lexer *gotreesitter.ExternalLexer, validSymbols []bool) bool {
	if s == nil || s.lang == nil {
		return false
	}
	// Raw text is only valid immediately inside <script>/<style>, so the
	// parser's validSymbols tells us when to swallow the body verbatim.
	// Check it before the ordinary text scan: inside a raw-text element the
	// `<` and `{` terminators of scanGSXText do not apply.
	if gsxValid(validSymbols, s.idxRawText) {
		if s.scanRawText(lexer) {
			return true
		}
	}
	if gsxValid(validSymbols, s.idxAttributeExpression) && lexer.Lookahead() == '{' {
		return s.scanAttributeExpression(lexer)
	}
	if gsxValid(validSymbols, s.idxText) {
		if s.scanGSXText(lexer) {
			return true
		}
	}
	// Go's terminator rule comes last. The GSX externals win any position
	// where both are valid, because GSX text may start with a newline.
	if gsxValid(validSymbols, s.idxAutoSemicolon) {
		if s.scanAutomaticSemicolon(lexer) {
			return true
		}
	}
	return false
}

// scanAutomaticSemicolon resolves Go's automatic semicolon insertion.
//
// The base Go grammar routes its `terminator` rule through this external
// token. A shared lexer DFA cannot choose between the zero-width end-of-file
// sentinel and the one-byte newline pattern, so it always takes the zero-width
// accept and drops the trailing newline from the enclosing statement. An
// external scanner reads the raw bytes and decides without a tie-break.
//
// This mirrors grammars.GoExternalScanner. GoSX cannot call that scanner,
// because it writes a Go-grammar symbol ID that the extended GSX grammar
// renumbers.
func (s *gsxScanner) scanAutomaticSemicolon(lexer *gotreesitter.ExternalLexer) bool {
	// Skip horizontal whitespace. The newline itself decides the match.
	// Leave comments alone: decline instead, and the parser matches the
	// comment as an extra and then calls this scanner again.
	for {
		switch lexer.Lookahead() {
		case ' ', '\t':
			lexer.Advance(true)
			continue
		}
		break
	}

	switch lexer.Lookahead() {
	case '\r':
		lexer.Advance(false)
		if lexer.Lookahead() != '\n' {
			return false
		}
		lexer.Advance(false)
		lexer.MarkEnd()
		lexer.SetResultSymbol(s.externalSymbol(s.idxAutoSemicolon))
		return true
	case '\n':
		// Take the newline as the token span, as the `/\n/` alternative does.
		lexer.Advance(false)
		lexer.MarkEnd()
		lexer.SetResultSymbol(s.externalSymbol(s.idxAutoSemicolon))
		return true
	case 0:
		// End of file. Match zero width, as the `'\0'` alternative does.
		lexer.MarkEnd()
		lexer.SetResultSymbol(s.externalSymbol(s.idxAutoSemicolon))
		return true
	default:
		// An explicit `;`, a comment start, or a syntax error. Decline and
		// let the DFA match it.
		return false
	}
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
			lexer.SetResultSymbol(s.externalSymbol(s.idxRawText))
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
	lexer.SetResultSymbol(s.externalSymbol(s.idxText))
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
				lexer.SetResultSymbol(s.externalSymbol(s.idxAttributeExpression))
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

// gsxValid reports whether the parser accepts the external token at idx.
// An idx of -1 means the language does not declare that external.
func gsxValid(vs []bool, idx int) bool { return idx >= 0 && idx < len(vs) && vs[idx] }

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
