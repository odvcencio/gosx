package htmlattr

import (
	"strings"
	"unicode"
)

// ValidName rejects characters that can end or split an HTML name token.
func ValidName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if unicode.IsSpace(r) || unicode.IsControl(r) || strings.ContainsRune("\"'<>/=`", r) {
			return false
		}
	}
	return true
}

// ValidTag permits HTML, SVG and custom element names, without HTML syntax.
func ValidTag(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' {
			continue
		}
		if i > 0 && (r >= '0' && r <= '9' || r == '-' || r == ':') {
			continue
		}
		return false
	}
	return true
}

// SafeSpreadName permits data-driven attributes without executable handlers
// or CSS. Authors can supply trusted style explicitly instead of in a spread.
func SafeSpreadName(name string) bool {
	name = strings.ToLower(name)
	return ValidName(name) && !strings.HasPrefix(name, "on") && name != "style"
}

// FilterURL allows web, email and telephone URLs and relative references.
// Unsafe schemes use an inert fragment, matching html/template's fail-closed
// approach. This runs before HTML escaping so entities remain literal data.
func FilterURL(name, value string) string {
	switch strings.ToLower(name) {
	case "href", "src", "action", "formaction", "xlink:href", "poster":
	default:
		return value
	}
	// Browsers ignore leading whitespace and ASCII controls in URL schemes.
	normalized := strings.TrimSpace(value)
	normalized = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, normalized)
	if colon := strings.IndexByte(normalized, ':'); colon >= 0 && !strings.ContainsAny(normalized[:colon], "/?#") {
		switch strings.ToLower(normalized[:colon]) {
		case "http", "https", "mailto", "tel":
		default:
			return "#"
		}
	}
	return value
}
