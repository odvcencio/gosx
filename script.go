package gosx

import (
	"encoding/json"
	"strings"
)

// InlineScript renders an executable script owned by the current request.
// Callers should pass the nonce carried by their request context. The script
// body is treated as JavaScript source, not HTML text, and every case variant
// of a closing </script sequence is escaped so authored source cannot end the
// element early.
func InlineScript(source, nonce string, extra ...any) Node {
	attrs := Attrs(extra...)
	if nonce != "" {
		attrs = append(attrs, Attr("nonce", nonce))
	}
	return El("script", attrs, RawHTML(escapeScriptText(source)))
}

// JSONScript renders JSON data in a non-executable application/json script
// element. The nonce is still attached because a strict CSP can require every
// script element, including JSON data consumed by a framework runtime, to be
// explicitly authorized. An encoding failure returns an empty text node rather
// than emitting a partial or unsafe payload.
func JSONScript(id, nonce string, value any, extra ...any) Node {
	payload, err := json.Marshal(value)
	if err != nil {
		return Text("")
	}
	attrs := Attrs(extra...)
	attrs = append(attrs, Attr("type", "application/json"))
	if id != "" {
		attrs = append(attrs, Attr("id", id))
	}
	if nonce != "" {
		attrs = append(attrs, Attr("nonce", nonce))
	}
	return El("script", attrs, RawHTML(escapeScriptText(string(payload))))
}

// escapeScriptText prevents the HTML parser from recognizing an authored
// closing script tag while preserving the JavaScript/JSON value seen by the
// browser after the element is parsed.
func escapeScriptText(source string) string {
	if !strings.Contains(strings.ToLower(source), "</script") {
		return source
	}
	var b strings.Builder
	b.Grow(len(source) + 8)
	for i := 0; i < len(source); {
		if source[i] == '<' && i+8 <= len(source) && strings.EqualFold(source[i:i+8], "</script") {
			b.WriteString("<\\/")
			b.WriteString(source[i+2 : i+8])
			i += 8
			continue
		}
		b.WriteByte(source[i])
		i++
	}
	return b.String()
}
