package ir

// isValidGoIdent, isGoIdentStart, and isGoIdentContinue are plain string
// helpers with no gotreesitter dependency. They live in their own
// unconstrained file (rather than lower.go, which is `!tinygo` — see that
// file's doc comment) because validate.go (always built, including under
// TinyGo) needs isValidGoIdent too.
func isValidGoIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if i == 0 {
			if !isGoIdentStart(r) {
				return false
			}
		} else {
			if !isGoIdentContinue(r) {
				return false
			}
		}
	}
	return true
}

func isGoIdentStart(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_'
}

func isGoIdentContinue(r rune) bool {
	return isGoIdentStart(r) || (r >= '0' && r <= '9')
}
