package pagecaps

import "strings"

// ScriptExecutes selects executable JavaScript for an active script element
// in a module-capable browser. Callers determine inert tree contexts.
// HTML's type/language precedence and classic nomodule rule are defined in
// https://html.spec.whatwg.org/multipage/scripting.html#prepare-the-script-element
// The MIME essence list is defined in MIME Sniffing §4.6. The measurement
// contract ignores MIME parameters; module is a separate exact token.
func ScriptExecutes(namespace string, attributes map[string]string) bool {
	if namespace != "" && namespace != "svg" {
		return false
	}
	typ, hasType := attributes["type"]
	if !hasType || typ == "" {
		typ = "text/javascript"
		if !hasType && namespace == "" && attributes["language"] != "" {
			typ = "text/" + attributes["language"]
		}
	}
	if scriptASCIILower(typ) == "module" {
		return true
	}
	// The legacy language value is concatenated literally. An explicit type
	// permits surrounding ASCII whitespace; Unicode whitespace is significant.
	if hasType {
		typ = strings.Trim(typ, " \t\r\n\f")
	}
	essence := typ
	if hasType {
		essence, _, _ = strings.Cut(typ, ";")
		essence = strings.Trim(essence, " \t\r\n\f")
	}
	switch scriptASCIILower(essence) {
	case "text/javascript", "application/javascript", "application/ecmascript",
		"application/x-ecmascript", "application/x-javascript", "text/ecmascript",
		"text/javascript1.0", "text/javascript1.1", "text/javascript1.2",
		"text/javascript1.3", "text/javascript1.4", "text/javascript1.5",
		"text/jscript", "text/livescript", "text/x-ecmascript", "text/x-javascript":
		_, noModule := attributes["nomodule"]
		// SVG uses the shared MIME classification, without HTML's obsolete
		// language fallback or its nomodule attribute.
		return namespace == "svg" || !noModule
	}
	return false
}

func scriptASCIILower(value string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'A' && r <= 'Z' {
			return r + ('a' - 'A')
		}
		return r
	}, value)
}
