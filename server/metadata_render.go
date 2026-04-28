package server

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/odvcencio/gosx"
)

func renderMetaTag(tag MetaTag) gosx.Node {
	return gosx.El("meta", gosx.Attrs(metaTagAttrs(tag)...))
}

func linkTagAttrs(l LinkTag) []any {
	attrs := []any{}
	attrs = appendNonEmptyAttr(attrs, "rel", l.Rel)
	attrs = appendNonEmptyAttr(attrs, "href", AssetURL(l.Href))
	attrs = appendNonEmptyAttr(attrs, "type", l.Type)
	attrs = appendNonEmptyAttr(attrs, "sizes", l.Sizes)
	attrs = appendNonEmptyAttr(attrs, "media", l.Media)
	attrs = appendNonEmptyAttr(attrs, "hreflang", l.HrefLang)
	attrs = appendNonEmptyAttr(attrs, "color", l.Color)
	attrs = appendNonEmptyAttr(attrs, "as", l.As)
	attrs = appendNonEmptyAttr(attrs, "crossorigin", l.CrossOrigin)
	if linkTagIsStylesheet(l.Rel) {
		attrs = appendStylesheetLinkAttrs(attrs, l)
	}
	return attrs
}

func appendStylesheetLinkAttrs(attrs []any, l LinkTag) []any {
	layer := normalizeCSSLayer(l.Layer)
	attrs = append(attrs,
		gosx.Attr("data-gosx-css-layer", string(layer)),
		gosx.Attr("data-gosx-css-owner", NormalizeStylesheetOwner(layer, l.Owner)),
		gosx.Attr("data-gosx-css-source", linkTagSource(l)),
	)
	return attrs
}

func linkTagSource(l LinkTag) string {
	if source := strings.TrimSpace(l.Source); source != "" {
		return source
	}
	return stylesheetSource(l.Href)
}

func linkTagIsStylesheet(rel string) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(rel)), "stylesheet")
}

func metaTagAttrs(tag MetaTag) []any {
	attrs := []any{}
	attrs = appendNonEmptyAttr(attrs, "name", tag.Name)
	attrs = appendNonEmptyAttr(attrs, "property", tag.Property)
	attrs = appendNonEmptyAttr(attrs, "content", tag.Content)
	attrs = appendNonEmptyAttr(attrs, "media", tag.Media)
	return attrs
}

func appendNonEmptyAttr(attrs []any, name, value string) []any {
	if value == "" {
		return attrs
	}
	return append(attrs, gosx.Attr(name, value))
}

func renderAlternateLinks(alternates ResolvedAlternates) []gosx.Node {
	nodes := []gosx.Node{}
	for _, key := range sortedKeys(alternates.Languages) {
		nodes = append(nodes, LinkTag{
			Rel:      "alternate",
			Href:     alternates.Languages[key],
			HrefLang: key,
		}.Node())
	}
	for _, key := range sortedKeys(alternates.Media) {
		nodes = append(nodes, LinkTag{
			Rel:   "alternate",
			Href:  alternates.Media[key],
			Media: key,
		}.Node())
	}
	for _, key := range sortedKeys(alternates.Types) {
		nodes = append(nodes, LinkTag{
			Rel:  "alternate",
			Href: alternates.Types[key],
			Type: key,
		}.Node())
	}
	return nodes
}

func renderRobotsMeta(robots ResolvedRobots) []gosx.Node {
	nodes := []gosx.Node{}
	if content := robots.content(); content != "" {
		nodes = append(nodes, renderMetaTag(MetaTag{Name: "robots", Content: content}))
	}
	if robots.GoogleBot != nil {
		if content := robots.GoogleBot.content(); content != "" {
			nodes = append(nodes, renderMetaTag(MetaTag{Name: "googlebot", Content: content}))
		}
	}
	return nodes
}

func renderOpenGraphMeta(openGraph ResolvedOpenGraph) []gosx.Node {
	nodes := []gosx.Node{}
	appendOG := func(property, content string) {
		content = strings.TrimSpace(content)
		if content == "" {
			return
		}
		nodes = append(nodes, renderMetaTag(MetaTag{Property: property, Content: content}))
	}
	appendOG("og:title", openGraph.Title)
	appendOG("og:description", openGraph.Description)
	appendOG("og:type", openGraph.Type)
	appendOG("og:url", openGraph.URL)
	appendOG("og:site_name", openGraph.SiteName)
	appendOG("og:locale", openGraph.Locale)
	for _, image := range openGraph.Images {
		appendOG("og:image", image.URL)
		if image.Width > 0 {
			appendOG("og:image:width", fmt.Sprintf("%d", image.Width))
		}
		if image.Height > 0 {
			appendOG("og:image:height", fmt.Sprintf("%d", image.Height))
		}
		appendOG("og:image:alt", image.Alt)
		appendOG("og:image:type", image.Type)
	}
	if openGraph.Article != nil {
		appendOG("article:published_time", openGraph.Article.PublishedTime)
		appendOG("article:modified_time", openGraph.Article.ModifiedTime)
		appendOG("article:section", openGraph.Article.Section)
		for _, author := range openGraph.Article.Authors {
			appendOG("article:author", author)
		}
		for _, tag := range openGraph.Article.Tags {
			appendOG("article:tag", tag)
		}
	}
	return nodes
}

func renderTwitterMeta(twitter ResolvedTwitter) []gosx.Node {
	nodes := []gosx.Node{}
	appendTwitter := func(name, content string) {
		content = strings.TrimSpace(content)
		if content == "" {
			return
		}
		nodes = append(nodes, renderMetaTag(MetaTag{Name: name, Content: content}))
	}
	appendTwitter("twitter:card", twitter.Card)
	appendTwitter("twitter:site", twitter.Site)
	appendTwitter("twitter:creator", twitter.Creator)
	appendTwitter("twitter:title", twitter.Title)
	appendTwitter("twitter:description", twitter.Description)
	for _, image := range twitter.Images {
		appendTwitter("twitter:image", image.URL)
		appendTwitter("twitter:image:alt", image.Alt)
	}
	return nodes
}

func renderJSONLDNode(item any) gosx.Node {
	if item == nil {
		return gosx.Text("")
	}
	payload, err := json.Marshal(item)
	if err != nil {
		return gosx.Text("")
	}
	safe := strings.NewReplacer(
		"<", "\\u003c",
		">", "\\u003e",
		"&", "\\u0026",
	).Replace(string(payload))
	return gosx.RawHTML(`<script type="application/ld+json">` + safe + `</script>`)
}

func (r ResolvedRobots) content() string {
	parts := []string{}
	parts = appendRobotIndexParts(parts, r.Index)
	parts = appendRobotFollowParts(parts, r.Follow)
	if r.NoArchive {
		parts = append(parts, "noarchive")
	}
	if r.NoImageAI {
		parts = append(parts, "noimageai")
	}
	if r.NoTranslate {
		parts = append(parts, "notranslate")
	}
	return strings.Join(parts, ",")
}

func (r ResolvedRobotsAgent) content() string {
	parts := []string{}
	parts = appendRobotIndexParts(parts, r.Index)
	parts = appendRobotFollowParts(parts, r.Follow)
	if r.MaxSnippet != 0 {
		parts = append(parts, fmt.Sprintf("max-snippet:%d", r.MaxSnippet))
	}
	if value := strings.TrimSpace(r.MaxImagePreview); value != "" {
		parts = append(parts, "max-image-preview:"+value)
	}
	if r.MaxVideoPreview != 0 {
		parts = append(parts, fmt.Sprintf("max-video-preview:%d", r.MaxVideoPreview))
	}
	return strings.Join(parts, ",")
}

func appendRobotIndexParts(parts []string, value *bool) []string {
	if value == nil {
		return parts
	}
	if *value {
		return append(parts, "index")
	}
	return append(parts, "noindex")
}

func appendRobotFollowParts(parts []string, value *bool) []string {
	if value == nil {
		return parts
	}
	if *value {
		return append(parts, "follow")
	}
	return append(parts, "nofollow")
}
