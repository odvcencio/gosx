package wire

import (
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/html"
	"m31labs.dev/gosx/internal/pagecaps"
)

// Classify executable attributes before HTML namespace/metadata handling. SVG
// and other unsupported elements must still retain known executable references.
func scanExecutableReferenceAttribute(n *html.Node, a html.Attribute, out *referenceScanner) (bool, error) {
	if n.Namespace == "" && n.Data == "iframe" && a.Key == "src" && hasHTMLReferenceAttribute(n, "srcdoc") {
		out.drop(dropOverriddenFrameSource)
		return true, nil
	}
	if a.Key == "src" && n.Data == "script" && knownHTMLDataScript(attr(n, "type")) {
		out.drop(dropInertDataScript)
		return true, nil
	}
	handler := strings.HasPrefix(a.Key, "on") && len(a.Key) > 2
	code, executableURL := "", false
	if !handler && a.Key != "srcdoc" {
		code, executableURL = javascriptReferenceURLCode(a.Val)
	}
	if n.Data == "meta" && a.Key == "content" && strings.EqualFold(strings.TrimSpace(attr(n, "http-equiv")), "refresh") {
		code, executableURL = refreshJavascriptReferenceCode(a.Val)
	}
	if !handler && !executableURL {
		return false, nil
	}
	switch {
	case out.scriptsBlocked:
		out.drop(dropSandboxedExecutable)
		return true, nil
	case !handler && n.Namespace == "" && n.Data == "iframe" && a.Key == "src" && !frameReferenceScriptsAllowed(n):
		out.drop(dropSandboxedExecutable)
		return true, nil
	}
	if n.Namespace == "" && n.Data == "template" {
		// Only its contents are inert. Do not exempt the owning element's
		// callable handlers or unmodelled executable URL attributes.
		out.drop(dropUnresolved)
	}
	if handler {
		code = a.Val
		if strings.TrimSpace(code) == "" {
			out.drop(dropEmptySyntax)
			return true, nil
		}
		// Handlers are FunctionBody, so return statements need a function
		// context. The same JS token walker and capability policy inspect it.
		// https://html.spec.whatwg.org/multipage/webappapis.html#event-handler-content-attributes
		code = "function __gosx_reference_handler__(event){\n" + code + "\n}"
	} else {
		// JavaScript URL completion may replace the document with HTML. Its
		// returned value/markup is not modelled; preserve known code references
		// while retaining this uncertainty even for literal fetches.
		out.drop(dropUnresolved)
		decoded, err := url.PathUnescape(code)
		if err != nil || !utf8.ValidString(decoded) {
			out.drop(dropUnresolved)
			return true, referenceFailure()
		}
		code = decoded
	}
	err := scanInlineReferences([]byte(code), KindScript, out)
	if err != nil {
		// An uninterpretable attribute cannot establish coverage, but scanning
		// other attributes/elements can still retain their known references.
		out.drop(dropUnresolved)
	}
	return true, nil
}

func javascriptReferenceURLCode(value string) (string, bool) {
	// URL parsing removes ASCII tab/newline and trims leading whitespace.
	// Be conservative for other leading whitespace, as pagecaps is too.
	value = strings.TrimLeftFunc(value, unicode.IsSpace)
	if strings.ContainsAny(value, "\t\r\n") {
		value = strings.NewReplacer("\t", "", "\r", "", "\n", "").Replace(value)
	}
	const scheme = "javascript:"
	if len(value) < len(scheme) || !strings.EqualFold(value[:len(scheme)], scheme) {
		return "", false
	}
	return value[len(scheme):], true
}

func refreshJavascriptReferenceCode(value string) (string, bool) {
	if separator := strings.IndexAny(value, ";,"); separator >= 0 {
		value = value[separator+1:]
	}
	value = strings.TrimSpace(value)
	if len(value) >= 3 && strings.EqualFold(value[:3], "url") {
		value = strings.TrimSpace(value[3:])
		if !strings.HasPrefix(value, "=") {
			return "", false
		}
		value = strings.TrimSpace(value[1:])
	}
	if len(value) > 0 && (value[0] == '\'' || value[0] == '"') {
		quote := value[0]
		value = strings.TrimLeft(value, "'\"")
		if len(value) > 0 && value[len(value)-1] == quote {
			value = value[:len(value)-1]
		}
	}
	return javascriptReferenceURLCode(value)
}

func frameReferenceScriptsAllowed(n *html.Node) bool {
	if !hasHTMLReferenceAttribute(n, "sandbox") {
		return true
	}
	for _, token := range strings.FieldsFunc(attr(n, "sandbox"), func(r rune) bool { return r == ' ' || r == '\t' || r == '\r' || r == '\n' || r == '\f' }) {
		if len(token) == len("allow-scripts") && strings.EqualFold(token, "allow-scripts") {
			return true
		}
	}
	return false
}

func scanSrcdocReferences(n *html.Node, body string, out *referenceScanner) error {
	// Production scans each explicit document separately. This field is already
	// decoded and bounded by the shared tree; it belongs to its child node.
	if out.document != nil && out.document.Embedding(n) != nil {
		out.drop(dropNestedScan)
		return nil
	}
	// Constructed DOM fixtures use the same tree for their inline children.
	if out.srcdocDepth >= pagecaps.MaxSrcdocDepth || out.srcdocCount >= 4096 || out.srcdocBytes+len(body) > 16<<20 {
		out.drop(dropUnresolved)
		return referenceFailure()
	}
	out.srcdocCount++
	out.srcdocBytes += len(body)
	tree, err := pagecaps.ParseDocumentTree([]byte(body), "", nil)
	if err != nil {
		out.drop(dropUnresolved)
		return referenceFailure()
	}
	if !tree.Complete {
		out.drop(dropUnresolved)
	}
	allowed := !out.scriptsBlocked && frameReferenceScriptsAllowed(n)
	out.drop(dropNestedScan)
	for _, doc := range tree.Documents {
		doc.ScriptsAllowed = allowed && doc.ScriptsAllowed
		if err := scanDocumentReferences(doc, out); err != nil {
			return err
		}
	}
	return nil
}
