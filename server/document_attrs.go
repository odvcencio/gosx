package server

import (
	"fmt"
	"html"
	"net/url"
	"path"
	"strings"

	"github.com/odvcencio/gosx"
)

type documentAttr struct {
	name  string
	value string
}

func documentHTMLAttrs(doc *DocumentContext) string {
	return renderDocumentAttrValues(documentHTMLAttrValues(doc))
}

func documentBodyAttrs(doc *DocumentContext) string {
	return renderDocumentAttrValues(documentBodyAttrValues(doc))
}

func DocumentAttrs(doc *DocumentContext) gosx.AttrList {
	return documentAttrList(documentHTMLAttrValues(doc))
}

func DocumentBodyAttrs(doc *DocumentContext) gosx.AttrList {
	return documentAttrList(documentBodyAttrValues(doc))
}

func documentHTMLAttrValues(doc *DocumentContext) []documentAttr {
	return appendDocumentContextAttrs([]documentAttr{
		{name: "data-gosx-document", value: "true"},
	}, doc, true)
}

func documentBodyAttrValues(doc *DocumentContext) []documentAttr {
	return appendDocumentContextAttrs([]documentAttr{
		{name: "data-gosx-document-body", value: "true"},
		{name: "data-gosx-enhancement-layer", value: "html"},
	}, doc, false)
}

func appendDocumentContextAttrs(attrs []documentAttr, doc *DocumentContext, includePath bool) []documentAttr {
	if doc == nil {
		return attrs
	}
	if pageID := strings.TrimSpace(doc.PageID); pageID != "" {
		attrs = append(attrs, documentAttr{name: "data-gosx-document-id", value: pageID})
	}
	if includePath {
		if currentPath := strings.TrimSpace(doc.Path); currentPath != "" {
			attrs = append(attrs, documentAttr{name: "data-gosx-document-path", value: currentPath})
		}
	}
	if doc.Navigation {
		attrs = append(attrs, documentNavigationAttrValues(doc)...)
	}
	if mode := documentBootstrapMode(doc.Runtime.BootstrapMode); mode != "none" {
		attrs = append(attrs, documentAttr{name: "data-gosx-bootstrap-mode", value: mode})
	}
	return attrs
}

func documentNavigationAttrValues(doc *DocumentContext) []documentAttr {
	return []documentAttr{
		{name: "data-gosx-navigation-state", value: "idle"},
		{name: "data-gosx-navigation-current-path", value: documentCurrentPath(doc)},
	}
}

func renderDocumentAttrValues(attrs []documentAttr) string {
	var b strings.Builder
	for _, attr := range attrs {
		fmt.Fprintf(&b, ` %s="%s"`, attr.name, html.EscapeString(attr.value))
	}
	return b.String()
}

func documentAttrList(attrs []documentAttr) gosx.AttrList {
	values := make([]any, 0, len(attrs))
	for _, attr := range attrs {
		values = append(values, gosx.Attr(attr.name, attr.value))
	}
	return gosx.Attrs(values...)
}

func documentCurrentPath(doc *DocumentContext) string {
	if doc == nil {
		return "/"
	}
	return firstNormalizedDocumentCurrentPath(
		documentRequestPath(doc),
		doc.Path,
	)
}

func documentRequestPath(doc *DocumentContext) string {
	if doc == nil || doc.Request == nil || doc.Request.URL == nil {
		return ""
	}
	return doc.Request.URL.Path
}

func firstNormalizedDocumentCurrentPath(values ...string) string {
	for _, value := range values {
		if current, ok := normalizeDocumentCurrentPath(value); ok {
			return current
		}
	}
	return "/"
}

func normalizeDocumentCurrentPath(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	parsed, err := url.Parse(value)
	if err == nil {
		if current, ok := normalizeDocumentCurrentPathSegment(parsed.Path); ok {
			return current, true
		}
		if strings.HasPrefix(value, "?") || strings.HasPrefix(value, "#") {
			return "/", true
		}
	}
	return normalizeDocumentCurrentPathSegment(value)
}

func normalizeDocumentCurrentPathSegment(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	if strings.HasPrefix(value, "?") || strings.HasPrefix(value, "#") {
		return "/", true
	}
	if !strings.HasPrefix(value, "/") {
		value = "/" + value
	}
	return path.Clean(value), true
}
