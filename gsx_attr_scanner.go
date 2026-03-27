package gosx

import gotreesitter "github.com/odvcencio/gotreesitter"

// gsxScanner lexes GSX externals. The CST still exposes `jsx_*` token names
// for compatibility with the generated grammar, but the scanner behavior is
// GSX-specific and Go-native.
type gsxScanner struct {
	lang *gotreesitter.Language
}

func (s *gsxScanner) Create() any { return nil }

func (s *gsxScanner) Destroy(payload any) {}

func (s *gsxScanner) Serialize(payload any, buf []byte) int { return 0 }

func (s *gsxScanner) Deserialize(payload any, buf []byte) {}

func (s *gsxScanner) SupportsIncrementalReuse() bool { return true }

func (s *gsxScanner) Scan(payload any, lexer *gotreesitter.ExternalLexer, validSymbols []bool) bool {
	if s == nil || s.lang == nil {
		return false
	}
	if gsxValid(validSymbols, 0) && lexer.Lookahead() == '{' {
		return s.scanAttributeExpression(lexer)
	}
	return false
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
				lexer.SetResultSymbol(s.lang.ExternalSymbols[0])
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
