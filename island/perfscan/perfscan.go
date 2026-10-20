// Package perfscan reads script and link references out of rendered page head
// HTML for the performance asset graph. It lives outside package island on
// purpose: it needs golang.org/x/net/html, and every GoSX app that imports
// island would then need that module in its go.mod.
package perfscan

import (
	"io"
	"strings"

	xhtml "golang.org/x/net/html"
)

// Ref is one script or link start tag. URL is src for script tags and href for
// link tags; Rel and As are the link attributes. When an attribute repeats,
// the last value wins.
type Ref struct {
	Tag string
	URL string
	Rel string
	As  string
}

// Scan returns every script and link start tag in head, in document order.
func Scan(head string) ([]Ref, error) {
	var refs []Ref
	tokens := xhtml.NewTokenizer(strings.NewReader(head))
	for {
		kind := tokens.Next()
		if kind == xhtml.ErrorToken {
			if tokens.Err() != io.EOF {
				return nil, tokens.Err()
			}
			return refs, nil
		}
		if kind != xhtml.StartTagToken && kind != xhtml.SelfClosingTagToken {
			continue
		}
		token := tokens.Token()
		if token.Data != "script" && token.Data != "link" {
			continue
		}
		ref := Ref{Tag: token.Data}
		for _, attr := range token.Attr {
			switch attr.Key {
			case "src":
				if token.Data == "script" {
					ref.URL = attr.Val
				}
			case "href":
				if token.Data == "link" {
					ref.URL = attr.Val
				}
			case "rel":
				ref.Rel = attr.Val
			case "as":
				ref.As = attr.Val
			}
		}
		refs = append(refs, ref)
	}
}
