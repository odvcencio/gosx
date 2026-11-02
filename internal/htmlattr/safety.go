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

// ReservedDispatchName identifies attributes owned by island event dispatch.
// Match HTML names without case sensitivity, including the bare event prefix.
func ReservedDispatchName(name string) bool {
	return equalFoldASCII(name, "data-gosx-handler") ||
		equalFoldASCII(name, "data-gosx-path") ||
		(len(name) >= len("data-gosx-on-") && equalFoldASCII(name[:len("data-gosx-on-")], "data-gosx-on-"))
}

// HTML attribute names fold ASCII letters only.
func equalFoldASCII(name, want string) bool {
	if len(name) != len(want) {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c != want[i] {
			return false
		}
	}
	return true
}

// SafeAuthoredName permits HTML names that cannot replace dispatch metadata.
func SafeAuthoredName(name string) bool {
	return ValidName(name) && !ReservedDispatchName(name)
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
// or CSS or framework behavior. Authors can supply trusted attributes explicitly.
func SafeSpreadName(name string) bool {
	name = strings.ToLower(name)
	return SafeAuthoredName(name) && !strings.HasPrefix(name, "on") && name != "style" && name != "srcdoc" && !strings.HasPrefix(name, "data-gosx-")
}

// IsURLAttr reports whether FilterURL sanitizes values of the named attribute.
func IsURLAttr(name string) bool {
	switch strings.ToLower(name) {
	case "href", "src", "action", "formaction", "xlink:href", "poster":
		return true
	}
	return false
}

// FilterURL allows web, email and telephone URLs and relative references.
// Unsafe schemes use an inert fragment, matching html/template's fail-closed
// approach. This runs before HTML escaping so entities remain literal data.
func FilterURL(name, value string) string {
	if !IsURLAttr(name) {
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
