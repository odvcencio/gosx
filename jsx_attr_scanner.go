package gosx

import gotreesitter "github.com/odvcencio/gotreesitter"

type jsxAttributeScanner struct {
	lang *gotreesitter.Language
}

func (s *jsxAttributeScanner) Create() any { return nil }

func (s *jsxAttributeScanner) Destroy(payload any) {}

func (s *jsxAttributeScanner) Serialize(payload any, buf []byte) int { return 0 }

func (s *jsxAttributeScanner) Deserialize(payload any, buf []byte) {}

func (s *jsxAttributeScanner) Scan(payload any, lexer *gotreesitter.ExternalLexer, validSymbols []bool) bool {
	if len(validSymbols) == 0 || !validSymbols[0] || s == nil || s.lang == nil {
		return false
	}
	if lexer.Lookahead() != '{' {
		return false
	}

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

func stripJSXAttributeExpression(text string) string {
	if len(text) >= 2 && text[0] == '{' && text[len(text)-1] == '}' {
		return text[1 : len(text)-1]
	}
	return text
}
