package gosx

import (
	"fmt"
	"m31labs.dev/gosx/internal/navigationlink"
	"strings"
)

// ManagedLink creates a request-aware navigation anchor. Unlike server.Link,
// its current relation is resolved when HTML is rendered, not when constructed.
// Attribute and child slices are copied so rendering never mutates the caller.
func ManagedLink(href string, attrs AttrList, children ...Node) Node {
	supplied := append([]nodeAttr{Attr("href", href)}, attrs...)
	return Node{kind: kindManagedLink, tag: "a", text: href, attrs: supplied, children: append([]Node(nil), children...)}
}

// ManagedLinkAttributes resolves the managed navigation contract while keeping
// ordinary authored attribute order. File renderers can share this operation
// without parsing HTML. The input slice is never changed.
func ManagedLinkAttributes(attrs AttrList, ctx RenderContext) AttrList {
	href := ""
	for _, attr := range attrs {
		if attr.name == "href" {
			href = fmt.Sprint(attr.value)
			break
		}
	}
	if strings.TrimSpace(ctx.RequestTarget) == "" {
		ctx.RequestTarget = "/"
	}
	return managedLinkAttrs(href, attrs, ctx)
}

func managedLinkAttrs(href string, attrs []nodeAttr, ctx RenderContext) []nodeAttr {
	out := make([]nodeAttr, 0, len(attrs)+10)
	for _, attr := range attrs {
		switch strings.TrimSpace(attr.name) {
		case "current", "data-gosx-link-current-policy", "data-gosx-link-current", "prefetch", "data-gosx-prefetch":
			continue
		}
		if attr.name == "ariaCurrent" {
			attr.name = "aria-current"
		}
		out = append(out, attr)
	}
	return append(out, ManagedLinkContractAttributes(href, attrs, ctx)...)
}

// ManagedLinkContractAttributes returns only framework-added navigation attributes.
// Callers retain their own authored attribute emission and supply already-resolved
// values when their language has separate direct-versus-spread precedence.
func ManagedLinkContractAttributes(href string, attrs AttrList, ctx RenderContext) AttrList {
	if strings.TrimSpace(ctx.RequestTarget) == "" {
		ctx.RequestTarget = "/"
	}
	lookup := func(names ...string) any {
		for _, name := range names {
			for _, attr := range attrs {
				if attr.name == name {
					return attr.value
				}
			}
		}
		return nil
	}
	stringValue := func(value any) string {
		if value == nil {
			return ""
		}
		return fmt.Sprint(value)
	}
	policy := navigationlink.NormalizeNavigationLinkCurrentPolicy(stringValue(lookup("data-gosx-link-current-policy", "data-gosx-link-current", "current")))
	current := navigationlink.ResolveNavigationLinkCurrent(href, ctx.RequestTarget, policy)
	prefetch, hasPrefetch := navigationlink.NormalizeNavigationLinkPrefetch(stringValue(lookup("data-gosx-prefetch", "prefetch")))
	var out AttrList
	if lookup("data-gosx-link") == nil {
		out = append(out, BoolAttr("data-gosx-link"))
	}
	for _, pair := range [][2]string{{"data-gosx-link-state", "idle"}, {"data-gosx-enhance", "navigation"}, {"data-gosx-enhance-layer", "bootstrap"}, {"data-gosx-fallback", "native-link"}} {
		if lookup(pair[0]) == nil {
			out = append(out, Attr(pair[0], pair[1]))
		}
	}
	out = append(out, Attr("data-gosx-link-current-policy", policy), Attr("data-gosx-link-current", current))
	if lookup("data-gosx-prefetch-state") == nil {
		out = append(out, Attr("data-gosx-prefetch-state", "idle"))
	}
	if hasPrefetch {
		out = append(out, Attr("data-gosx-prefetch", prefetch))
	}
	if current == "page" && lookup("aria-current", "ariaCurrent") == nil {
		out = append(out, Attr("aria-current", "page"), Attr("data-gosx-aria-current-managed", "true"))
	}
	return out
}
