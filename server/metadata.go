package server

import (
	"encoding/json"
	"fmt"
	"log"
	neturl "net/url"
	"os"
	"path"
	"slices"
	"sort"
	"strings"

	"github.com/odvcencio/gosx"
)

// MetadataResolver derives request-scoped metadata from the current request
// context and the already-resolved parent metadata.
type MetadataResolver func(ctx *Context, parent ResolvedMetadata) (Metadata, error)

// Metadata describes typed document metadata for a server-rendered page.
type Metadata struct {
	Title        Title
	Description  string
	MetadataBase string

	Alternates   *Alternates
	Robots       *Robots
	Icons        *Icons
	Manifest     string
	Verification *Verification
	ThemeColor   []ThemeColor

	OpenGraph *OpenGraph
	Twitter   *Twitter

	JSONLD []any
	Other  []MetaTag
	Links  []LinkTag
}

// Title describes title inheritance and formatting rules.
type Title struct {
	Absolute string `json:"absolute,omitempty"`
	Default  string `json:"default,omitempty"`
	Template string `json:"template,omitempty"`
}

// Alternates describes canonical and alternate document URLs.
type Alternates struct {
	Canonical string            `json:"canonical,omitempty"`
	Languages map[string]string `json:"languages,omitempty"`
	Media     map[string]string `json:"media,omitempty"`
	Types     map[string]string `json:"types,omitempty"`
}

// Robots models semantic robots directives.
type Robots struct {
	Index       *bool        `json:"index,omitempty"`
	Follow      *bool        `json:"follow,omitempty"`
	NoArchive   bool         `json:"noArchive,omitempty"`
	NoImageAI   bool         `json:"noImageAI,omitempty"`
	NoTranslate bool         `json:"noTranslate,omitempty"`
	GoogleBot   *RobotsAgent `json:"googleBot,omitempty"`
}

// RobotsAgent models agent-specific robots directives.
type RobotsAgent struct {
	Index           *bool  `json:"index,omitempty"`
	Follow          *bool  `json:"follow,omitempty"`
	MaxSnippet      int    `json:"maxSnippet,omitempty"`
	MaxImagePreview string `json:"maxImagePreview,omitempty"`
	MaxVideoPreview int    `json:"maxVideoPreview,omitempty"`
}

// OpenGraph describes structured Open Graph metadata.
type OpenGraph struct {
	Type        string       `json:"type,omitempty"`
	URL         string       `json:"url,omitempty"`
	SiteName    string       `json:"siteName,omitempty"`
	Locale      string       `json:"locale,omitempty"`
	Title       string       `json:"title,omitempty"`
	Description string       `json:"description,omitempty"`
	Images      []MediaAsset `json:"images,omitempty"`

	Article *OpenGraphArticle `json:"article,omitempty"`
}

// OpenGraphArticle describes article-specific Open Graph metadata.
type OpenGraphArticle struct {
	PublishedTime string   `json:"publishedTime,omitempty"`
	ModifiedTime  string   `json:"modifiedTime,omitempty"`
	Authors       []string `json:"authors,omitempty"`
	Tags          []string `json:"tags,omitempty"`
	Section       string   `json:"section,omitempty"`
}

// Twitter describes structured Twitter card metadata.
type Twitter struct {
	Card        string       `json:"card,omitempty"`
	Site        string       `json:"site,omitempty"`
	Creator     string       `json:"creator,omitempty"`
	Title       string       `json:"title,omitempty"`
	Description string       `json:"description,omitempty"`
	Images      []MediaAsset `json:"images,omitempty"`
}

// MediaAsset describes a renderable metadata media asset.
type MediaAsset struct {
	URL    string `json:"url,omitempty"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
	Alt    string `json:"alt,omitempty"`
	Type   string `json:"type,omitempty"`
}

// Icons describes icon buckets with explicit replacement semantics.
type Icons struct {
	Icon     []IconAsset `json:"icon,omitempty"`
	Shortcut []IconAsset `json:"shortcut,omitempty"`
	Apple    []IconAsset `json:"apple,omitempty"`
	Other    []IconAsset `json:"other,omitempty"`
}

// IconAsset describes a structured icon link.
type IconAsset struct {
	URL   string `json:"url,omitempty"`
	Type  string `json:"type,omitempty"`
	Sizes string `json:"sizes,omitempty"`
	Color string `json:"color,omitempty"`
	Rel   string `json:"rel,omitempty"`
	Media string `json:"media,omitempty"`
}

// Verification describes verification tags for common providers.
type Verification struct {
	Google string            `json:"google,omitempty"`
	Bing   string            `json:"bing,omitempty"`
	Yandex string            `json:"yandex,omitempty"`
	Yahoo  string            `json:"yahoo,omitempty"`
	Other  map[string]string `json:"other,omitempty"`
}

// ThemeColor describes a theme-color declaration.
type ThemeColor struct {
	Color string `json:"color,omitempty"`
	Media string `json:"media,omitempty"`
}

// MetaTag represents a generic <meta> tag.
type MetaTag struct {
	Name     string `json:"name,omitempty"`
	Property string `json:"property,omitempty"`
	Content  string `json:"content,omitempty"`
	Media    string `json:"media,omitempty"`
}

// LinkTag represents a generic <link> tag.
type LinkTag struct {
	Rel         string
	Href        string
	Type        string
	Sizes       string
	Media       string
	HrefLang    string
	Color       string
	As          string
	CrossOrigin string
	Layer       CSSLayer
	Owner       string
	Source      string
}

// ResolvedMetadata is the normalized metadata payload passed to document
// renderers.
type ResolvedMetadata struct {
	Title        string
	Description  string
	CanonicalURL string
	MetadataBase *neturl.URL

	Alternates   ResolvedAlternates
	Robots       ResolvedRobots
	Icons        []ResolvedLink
	Manifest     string
	Verification []MetaTag
	ThemeColor   []MetaTag

	OpenGraph ResolvedOpenGraph
	Twitter   ResolvedTwitter

	JSONLD []any
	Other  []MetaTag
	Links  []LinkTag
}

// ResolvedAlternates holds normalized alternate URLs.
type ResolvedAlternates struct {
	Languages map[string]string
	Media     map[string]string
	Types     map[string]string
}

// ResolvedRobots holds normalized robots directives.
type ResolvedRobots struct {
	Index       *bool
	Follow      *bool
	NoArchive   bool
	NoImageAI   bool
	NoTranslate bool
	GoogleBot   *ResolvedRobotsAgent
}

// ResolvedRobotsAgent holds normalized agent-specific robots directives.
type ResolvedRobotsAgent struct {
	Index           *bool
	Follow          *bool
	MaxSnippet      int
	MaxImagePreview string
	MaxVideoPreview int
}

// ResolvedOpenGraph holds normalized Open Graph fields.
type ResolvedOpenGraph struct {
	Type        string
	URL         string
	SiteName    string
	Locale      string
	Title       string
	Description string
	Images      []MediaAsset

	Article *OpenGraphArticle
}

// ResolvedTwitter holds normalized Twitter card fields.
type ResolvedTwitter struct {
	Card        string
	Site        string
	Creator     string
	Title       string
	Description string
	Images      []MediaAsset
}

// ResolvedLink holds a normalized link tag for structured metadata renderers.
type ResolvedLink struct {
	Rel      string
	URL      string
	Type     string
	Sizes    string
	Color    string
	Media    string
	HrefLang string
}

type metadataIssue struct {
	level   string
	message string
}

// SiteMetadata preserves the legacy app-wide metadata defaults surface while
// adapting it into the structured metadata model.
type SiteMetadata struct {
	BaseURL      string
	Name         string
	DefaultImage string
	ImageWidth   int
	ImageHeight  int
	Type         string
	Locale       string
	TwitterSite  string
}

// Metadata converts the legacy site metadata defaults into the structured model.
func (s SiteMetadata) Metadata() Metadata {
	meta := Metadata{}
	if base := strings.TrimSpace(s.BaseURL); base != "" {
		meta.MetadataBase = base
	}
	if strings.TrimSpace(s.Name) != "" ||
		strings.TrimSpace(s.DefaultImage) != "" ||
		strings.TrimSpace(s.Type) != "" ||
		strings.TrimSpace(s.Locale) != "" {
		meta.OpenGraph = &OpenGraph{
			Type:     strings.TrimSpace(s.Type),
			SiteName: strings.TrimSpace(s.Name),
			Locale:   strings.TrimSpace(s.Locale),
		}
		if image := strings.TrimSpace(s.DefaultImage); image != "" {
			meta.OpenGraph.Images = []MediaAsset{{
				URL:    image,
				Width:  s.ImageWidth,
				Height: s.ImageHeight,
			}}
		}
	}
	if strings.TrimSpace(s.TwitterSite) != "" {
		meta.Twitter = &Twitter{Site: strings.TrimSpace(s.TwitterSite)}
	}
	return meta
}

// Head renders metadata into head nodes. Title is handled by the document shell.
func (m Metadata) Head() gosx.Node {
	return m.head(SiteMetadata{}, "")
}

func (m Metadata) head(site SiteMetadata, requestPath string) gosx.Node {
	resolved, err := resolveMetadata(mergeMetadata(site.Metadata(), m), requestPath)
	if err != nil {
		panic(err)
	}
	return resolved.Head()
}

// Head renders a resolved metadata payload into head nodes. Title is handled by
// the document shell.
func (m ResolvedMetadata) Head() gosx.Node {
	nodes := []gosx.Node{}

	if m.CanonicalURL != "" {
		nodes = append(nodes, LinkTag{Rel: "canonical", Href: m.CanonicalURL}.Node())
	}
	nodes = append(nodes, renderAlternateLinks(m.Alternates)...)
	nodes = append(nodes, renderRobotsMeta(m.Robots)...)
	if m.Description != "" {
		nodes = append(nodes, renderMetaTag(MetaTag{Name: "description", Content: m.Description}))
	}
	nodes = append(nodes, renderOpenGraphMeta(m.OpenGraph)...)
	nodes = append(nodes, renderTwitterMeta(m.Twitter)...)
	for _, tag := range m.Verification {
		nodes = append(nodes, renderMetaTag(tag))
	}
	for _, icon := range dedupeResolvedLinks(m.Icons) {
		nodes = append(nodes, LinkTag{
			Rel:      icon.Rel,
			Href:     icon.URL,
			Type:     icon.Type,
			Sizes:    icon.Sizes,
			Media:    icon.Media,
			HrefLang: icon.HrefLang,
			Color:    icon.Color,
		}.Node())
	}
	if m.Manifest != "" {
		nodes = append(nodes, LinkTag{Rel: "manifest", Href: m.Manifest}.Node())
	}
	for _, tag := range dedupeThemeColorTags(m.ThemeColor) {
		nodes = append(nodes, renderMetaTag(tag))
	}
	for _, item := range m.JSONLD {
		if node := renderJSONLDNode(item); !node.IsZero() {
			nodes = append(nodes, node)
		}
	}
	for _, tag := range m.Other {
		nodes = append(nodes, renderMetaTag(tag))
	}
	for _, link := range m.Links {
		nodes = append(nodes, link.Node())
	}
	if len(nodes) == 0 {
		return gosx.Text("")
	}
	return gosx.Fragment(nodes...)
}

// Node renders the link tag as a GoSX node.
func (l LinkTag) Node() gosx.Node {
	return gosx.El("link", gosx.Attrs(linkTagAttrs(l)...))
}

// IsZero reports whether the metadata contribution has no effect.
func (m Metadata) IsZero() bool {
	return isZeroTitle(m.Title) &&
		strings.TrimSpace(m.Description) == "" &&
		strings.TrimSpace(m.MetadataBase) == "" &&
		(m.Alternates == nil || isZeroAlternates(*m.Alternates)) &&
		(m.Robots == nil || isZeroRobots(*m.Robots)) &&
		(m.Icons == nil || isZeroIcons(*m.Icons)) &&
		strings.TrimSpace(m.Manifest) == "" &&
		(m.Verification == nil || isZeroVerification(*m.Verification)) &&
		len(m.ThemeColor) == 0 &&
		(m.OpenGraph == nil || isZeroOpenGraph(*m.OpenGraph)) &&
		(m.Twitter == nil || isZeroTwitter(*m.Twitter)) &&
		len(m.JSONLD) == 0 &&
		len(m.Other) == 0 &&
		len(m.Links) == 0
}

// MergeMetadata applies field-aware metadata merge semantics with child values
// overriding or extending parent values as appropriate.
func MergeMetadata(parent, child Metadata) Metadata {
	return mergeMetadata(parent, child)
}

func mergeMetadata(parent, child Metadata) Metadata {
	if parent.IsZero() {
		return cloneMetadata(child)
	}
	if child.IsZero() {
		return cloneMetadata(parent)
	}

	out := cloneMetadata(parent)
	out.Title = mergeTitle(parent.Title, child.Title)
	if value := strings.TrimSpace(child.Description); value != "" {
		out.Description = value
	}
	if value := strings.TrimSpace(child.MetadataBase); value != "" {
		out.MetadataBase = value
	}
	out.Alternates = mergeAlternates(parent.Alternates, child.Alternates)
	out.Robots = mergeRobots(parent.Robots, child.Robots)
	out.Icons = mergeIcons(parent.Icons, child.Icons)
	if value := strings.TrimSpace(child.Manifest); value != "" {
		out.Manifest = value
	}
	out.Verification = mergeVerification(parent.Verification, child.Verification)
	if len(child.ThemeColor) > 0 {
		out.ThemeColor = append(cloneThemeColors(parent.ThemeColor), cloneThemeColors(child.ThemeColor)...)
	}
	out.OpenGraph = mergeOpenGraph(parent.OpenGraph, child.OpenGraph)
	out.Twitter = mergeTwitter(parent.Twitter, child.Twitter)
	if len(child.JSONLD) > 0 {
		out.JSONLD = append(cloneAnySlice(parent.JSONLD), child.JSONLD...)
	}
	if len(child.Other) > 0 {
		out.Other = append(cloneMetaTags(parent.Other), cloneMetaTags(child.Other)...)
	}
	if len(child.Links) > 0 {
		out.Links = append(cloneLinkTags(parent.Links), cloneLinkTags(child.Links)...)
	}
	return out
}

func mergeTitle(parent, child Title) Title {
	out := parent
	if value := strings.TrimSpace(child.Absolute); value != "" {
		out.Default = value
	}
	if value := strings.TrimSpace(child.Default); value != "" {
		out.Default = applyTitleTemplate(strings.TrimSpace(parent.Template), value)
	}
	if value := strings.TrimSpace(child.Template); value != "" {
		out.Template = composeTitleTemplate(strings.TrimSpace(parent.Template), value)
	}
	out.Absolute = ""
	return out
}

func resolveMetadata(meta Metadata, requestPath string) (ResolvedMetadata, error) {
	resolved := ResolvedMetadata{
		Title:       resolveTitle(meta.Title),
		Description: strings.TrimSpace(meta.Description),
		JSONLD:      cloneAnySlice(meta.JSONLD),
		Other:       cloneMetaTags(meta.Other),
		Links:       cloneLinkTags(meta.Links),
	}

	base, issues := resolveMetadataBase(meta.MetadataBase)
	resolved.MetadataBase = base
	resolved.CanonicalURL = resolveCanonicalURL(base, meta.Alternates, requestPath)
	resolved.Alternates = resolveAlternates(base, meta.Alternates)
	resolved.Robots = resolveRobots(meta.Robots)
	resolved.Icons = resolveIcons(base, meta.Icons)
	resolved.Manifest = resolveMetadataURL(base, meta.Manifest)
	resolved.Verification = resolveVerification(meta.Verification)
	resolved.ThemeColor = resolveThemeColor(meta.ThemeColor)
	resolved.OpenGraph = resolveOpenGraph(base, meta.OpenGraph, resolved)
	resolved.Twitter = resolveTwitter(base, meta.Twitter, resolved)

	issues = append(issues, validateMetadata(meta, resolved)...)
	if err := reportMetadataIssues(issues); err != nil {
		return resolved, err
	}
	return resolved, nil
}

func resolveTitle(title Title) string {
	if value := strings.TrimSpace(title.Absolute); value != "" {
		return value
	}
	return strings.TrimSpace(title.Default)
}

func resolveMetadataBase(raw string) (*neturl.URL, []metadataIssue) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parsed, err := neturl.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, []metadataIssue{{
			level:   "error",
			message: fmt.Sprintf("metadataBase must be an absolute URL, got %q", raw),
		}}
	}
	return parsed, nil
}

func resolveCanonicalURL(base *neturl.URL, alternates *Alternates, requestPath string) string {
	raw := ""
	if alternates != nil {
		raw = strings.TrimSpace(alternates.Canonical)
	}
	if raw == "" && base != nil {
		raw = metadataRequestPath(requestPath)
	}
	return resolveMetadataURL(base, raw)
}

func resolveAlternates(base *neturl.URL, alternates *Alternates) ResolvedAlternates {
	resolved := ResolvedAlternates{
		Languages: map[string]string{},
		Media:     map[string]string{},
		Types:     map[string]string{},
	}
	if alternates == nil {
		return resolved
	}
	for key, value := range alternates.Languages {
		if resolvedValue := resolveMetadataURL(base, value); resolvedValue != "" {
			resolved.Languages[key] = resolvedValue
		}
	}
	for key, value := range alternates.Media {
		if resolvedValue := resolveMetadataURL(base, value); resolvedValue != "" {
			resolved.Media[key] = resolvedValue
		}
	}
	for key, value := range alternates.Types {
		if resolvedValue := resolveMetadataURL(base, value); resolvedValue != "" {
			resolved.Types[key] = resolvedValue
		}
	}
	return resolved
}

func resolveRobots(robots *Robots) ResolvedRobots {
	if robots == nil {
		return ResolvedRobots{}
	}
	resolved := ResolvedRobots{
		Index:       cloneBoolPtr(robots.Index),
		Follow:      cloneBoolPtr(robots.Follow),
		NoArchive:   robots.NoArchive,
		NoImageAI:   robots.NoImageAI,
		NoTranslate: robots.NoTranslate,
	}
	if robots.GoogleBot != nil {
		resolved.GoogleBot = &ResolvedRobotsAgent{
			Index:           cloneBoolPtr(robots.GoogleBot.Index),
			Follow:          cloneBoolPtr(robots.GoogleBot.Follow),
			MaxSnippet:      robots.GoogleBot.MaxSnippet,
			MaxImagePreview: strings.TrimSpace(robots.GoogleBot.MaxImagePreview),
			MaxVideoPreview: robots.GoogleBot.MaxVideoPreview,
		}
	}
	return resolved
}

func resolveIcons(base *neturl.URL, icons *Icons) []ResolvedLink {
	if icons == nil {
		return nil
	}
	links := []ResolvedLink{}
	links = append(links, resolveIconBucket(base, "icon", icons.Icon)...)
	links = append(links, resolveIconBucket(base, "shortcut icon", icons.Shortcut)...)
	links = append(links, resolveIconBucket(base, "apple-touch-icon", icons.Apple)...)
	links = append(links, resolveIconBucket(base, "", icons.Other)...)
	return links
}

func resolveVerification(verification *Verification) []MetaTag {
	if verification == nil {
		return nil
	}
	tags := []MetaTag{}
	if value := strings.TrimSpace(verification.Google); value != "" {
		tags = append(tags, MetaTag{Name: "google-site-verification", Content: value})
	}
	if value := strings.TrimSpace(verification.Bing); value != "" {
		tags = append(tags, MetaTag{Name: "msvalidate.01", Content: value})
	}
	if value := strings.TrimSpace(verification.Yandex); value != "" {
		tags = append(tags, MetaTag{Name: "yandex-verification", Content: value})
	}
	if value := strings.TrimSpace(verification.Yahoo); value != "" {
		tags = append(tags, MetaTag{Name: "y_key", Content: value})
	}
	if len(verification.Other) == 0 {
		return tags
	}
	keys := make([]string, 0, len(verification.Other))
	for key := range verification.Other {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if value := strings.TrimSpace(verification.Other[key]); value != "" {
			tags = append(tags, MetaTag{Name: key, Content: value})
		}
	}
	return tags
}

func resolveThemeColor(colors []ThemeColor) []MetaTag {
	if len(colors) == 0 {
		return nil
	}
	tags := make([]MetaTag, 0, len(colors))
	for _, color := range colors {
		if strings.TrimSpace(color.Color) == "" {
			continue
		}
		tags = append(tags, MetaTag{
			Name:    "theme-color",
			Content: strings.TrimSpace(color.Color),
			Media:   strings.TrimSpace(color.Media),
		})
	}
	return tags
}

func resolveOpenGraph(base *neturl.URL, input *OpenGraph, meta ResolvedMetadata) ResolvedOpenGraph {
	resolved := ResolvedOpenGraph{}
	if input != nil {
		resolved.Type = strings.TrimSpace(input.Type)
		resolved.URL = resolveMetadataURL(base, input.URL)
		resolved.SiteName = strings.TrimSpace(input.SiteName)
		resolved.Locale = strings.TrimSpace(input.Locale)
		resolved.Title = strings.TrimSpace(input.Title)
		resolved.Description = strings.TrimSpace(input.Description)
		resolved.Images = resolveMediaAssets(base, input.Images)
		if input.Article != nil {
			resolved.Article = cloneOpenGraphArticle(input.Article)
		}
	}
	if resolved.Title == "" {
		resolved.Title = meta.Title
	}
	if resolved.Description == "" {
		resolved.Description = meta.Description
	}
	if resolved.URL == "" {
		resolved.URL = meta.CanonicalURL
	}
	if resolved.Type == "" && (resolved.Title != "" || resolved.Description != "" || resolved.URL != "" || len(resolved.Images) > 0 || resolved.SiteName != "" || resolved.Locale != "") {
		resolved.Type = "website"
	}
	resolved.Images = dedupeMediaAssets(resolved.Images)
	return resolved
}

func resolveTwitter(base *neturl.URL, input *Twitter, meta ResolvedMetadata) ResolvedTwitter {
	resolved := ResolvedTwitter{}
	if input != nil {
		resolved.Card = strings.TrimSpace(input.Card)
		resolved.Site = strings.TrimSpace(input.Site)
		resolved.Creator = strings.TrimSpace(input.Creator)
		resolved.Title = strings.TrimSpace(input.Title)
		resolved.Description = strings.TrimSpace(input.Description)
		resolved.Images = resolveMediaAssets(base, input.Images)
	}
	if resolved.Title == "" {
		resolved.Title = meta.Title
	}
	if resolved.Description == "" {
		resolved.Description = meta.Description
	}
	if len(resolved.Images) == 0 {
		resolved.Images = cloneMediaAssets(meta.OpenGraph.Images)
	}
	if resolved.Card == "" && (resolved.Title != "" || resolved.Description != "" || len(resolved.Images) > 0 || resolved.Site != "" || resolved.Creator != "") {
		if len(resolved.Images) > 0 {
			resolved.Card = "summary_large_image"
		} else {
			resolved.Card = "summary"
		}
	}
	resolved.Images = dedupeMediaAssets(resolved.Images)
	return resolved
}

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

func resolveIconBucket(base *neturl.URL, fallbackRel string, assets []IconAsset) []ResolvedLink {
	if len(assets) == 0 {
		return nil
	}
	links := make([]ResolvedLink, 0, len(assets))
	for _, asset := range assets {
		if url := resolveMetadataURL(base, asset.URL); url != "" {
			rel := strings.TrimSpace(asset.Rel)
			if rel == "" {
				rel = fallbackRel
			}
			links = append(links, ResolvedLink{
				Rel:   rel,
				URL:   url,
				Type:  strings.TrimSpace(asset.Type),
				Sizes: strings.TrimSpace(asset.Sizes),
				Color: strings.TrimSpace(asset.Color),
				Media: strings.TrimSpace(asset.Media),
			})
		}
	}
	return links
}

func resolveMediaAssets(base *neturl.URL, assets []MediaAsset) []MediaAsset {
	if len(assets) == 0 {
		return nil
	}
	resolved := make([]MediaAsset, 0, len(assets))
	for _, asset := range assets {
		if url := resolveMetadataURL(base, asset.URL); url != "" {
			resolved = append(resolved, MediaAsset{
				URL:    url,
				Width:  asset.Width,
				Height: asset.Height,
				Alt:    strings.TrimSpace(asset.Alt),
				Type:   strings.TrimSpace(asset.Type),
			})
		}
	}
	return resolved
}

func resolveMetadataURL(base *neturl.URL, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	normalized := normalizeMetadataURL(raw)
	if base == nil {
		return normalized
	}
	return resolveAbsoluteURL(base.String(), normalized)
}

func metadataRequestPath(requestPath string) string {
	requestPath = strings.TrimSpace(requestPath)
	if requestPath == "" {
		return "/"
	}
	if parsed, err := neturl.Parse(requestPath); err == nil {
		if strings.TrimSpace(parsed.Path) != "" {
			requestPath = parsed.Path
		}
	}
	requestPath = path.Clean("/" + strings.TrimLeft(requestPath, "/"))
	if requestPath == "." || requestPath == "" {
		return "/"
	}
	return requestPath
}

func normalizeMetadataURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "data:") || strings.HasPrefix(raw, "//") {
		return raw
	}
	if parsed, err := neturl.Parse(raw); err == nil && (parsed.Scheme != "" || parsed.Host != "") {
		return raw
	}
	return AssetURL(raw)
}

func resolveAbsoluteURL(baseURL, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if baseURL == "" {
		return raw
	}
	base, err := neturl.Parse(strings.TrimSpace(baseURL))
	if err != nil || base.Scheme == "" || base.Host == "" {
		return raw
	}
	ref, err := neturl.Parse(raw)
	if err != nil {
		return raw
	}
	return base.ResolveReference(ref).String()
}

func validateMetadata(meta Metadata, resolved ResolvedMetadata) []metadataIssue {
	issues := []metadataIssue{}
	if template := strings.TrimSpace(meta.Title.Template); template != "" && !strings.Contains(template, "%s") {
		issues = append(issues, metadataIssue{
			level:   "error",
			message: fmt.Sprintf("title template %q must include %%s", template),
		})
	}
	if strings.TrimSpace(meta.MetadataBase) == "" {
		issues = appendRelativeURLWarnings(issues, meta)
	}
	if canonical := strings.TrimSpace(resolved.CanonicalURL); canonical != "" {
		if parsed, err := neturl.Parse(canonical); err != nil || parsed.Host == "" && parsed.Scheme != "" {
			issues = append(issues, metadataIssue{
				level:   "error",
				message: fmt.Sprintf("canonical URL %q is malformed", canonical),
			})
		}
	}
	if card := strings.TrimSpace(resolved.Twitter.Card); card != "" && !slices.Contains([]string{
		"summary",
		"summary_large_image",
		"app",
		"player",
	}, card) {
		issues = append(issues, metadataIssue{
			level:   "error",
			message: fmt.Sprintf("twitter card %q is unsupported", card),
		})
	}
	for _, image := range meta.OpenGraphImages() {
		if strings.TrimSpace(image.URL) == "" {
			issues = append(issues, metadataIssue{
				level:   "error",
				message: "open graph image entries must include a URL",
			})
			break
		}
	}
	for _, key := range sortedKeys(meta.AlternateLanguages()) {
		if !validHrefLang(key) {
			issues = append(issues, metadataIssue{
				level:   "error",
				message: fmt.Sprintf("hreflang key %q is invalid", key),
			})
		}
	}
	for _, value := range meta.JSONLD {
		if _, err := json.Marshal(value); err != nil {
			issues = append(issues, metadataIssue{
				level:   "error",
				message: fmt.Sprintf("json-ld value could not be serialized: %v", err),
			})
			break
		}
	}
	if meta.Robots != nil && meta.Robots.GoogleBot != nil {
		if value := meta.Robots.GoogleBot.MaxImagePreview; value != "" && !slices.Contains([]string{"none", "standard", "large"}, value) {
			issues = append(issues, metadataIssue{
				level:   "error",
				message: fmt.Sprintf("googlebot maxImagePreview %q is invalid", value),
			})
		}
	}
	return issues
}

func appendRelativeURLWarnings(issues []metadataIssue, meta Metadata) []metadataIssue {
	seen := map[string]struct{}{}
	for _, candidate := range relativeURLValidationTargets(meta) {
		if candidate == "" || !requiresMetadataBase(candidate) {
			continue
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		issues = append(issues, metadataIssue{
			level:   "warning",
			message: fmt.Sprintf("relative metadata URL %q should be resolved with metadataBase", candidate),
		})
	}
	return issues
}

func reportMetadataIssues(issues []metadataIssue) error {
	if !metadataValidationEnabled() || len(issues) == 0 {
		return nil
	}
	errorMessages := []string{}
	for _, issue := range issues {
		switch issue.level {
		case "error":
			errorMessages = append(errorMessages, issue.message)
		default:
			log.Printf("gosx metadata warning: %s", issue.message)
		}
	}
	if len(errorMessages) == 0 {
		return nil
	}
	return fmt.Errorf("metadata validation failed: %s", strings.Join(errorMessages, "; "))
}

func metadataValidationEnabled() bool {
	for _, key := range []string{"GOSX_ENV", "GO_ENV", "NODE_ENV"} {
		if strings.EqualFold(strings.TrimSpace(os.Getenv(key)), "development") {
			return true
		}
	}
	return false
}

func requiresMetadataBase(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.HasPrefix(raw, "data:") || strings.HasPrefix(raw, "//") {
		return false
	}
	parsed, err := neturl.Parse(raw)
	return err == nil && parsed.Scheme == "" && parsed.Host == ""
}

func validHrefLang(value string) bool {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return false
	}
	if value == "x-default" {
		return true
	}
	for _, part := range strings.Split(value, "-") {
		if len(part) < 2 || len(part) > 8 {
			return false
		}
		for _, r := range part {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
				return false
			}
		}
	}
	return true
}

func relativeURLValidationTargets(meta Metadata) []string {
	values := []string{}
	if meta.Alternates != nil {
		values = append(values, meta.Alternates.Canonical)
		for _, value := range meta.Alternates.Languages {
			values = append(values, value)
		}
		for _, value := range meta.Alternates.Media {
			values = append(values, value)
		}
		for _, value := range meta.Alternates.Types {
			values = append(values, value)
		}
	}
	if meta.OpenGraph != nil {
		values = append(values, meta.OpenGraph.URL)
		for _, image := range meta.OpenGraph.Images {
			values = append(values, image.URL)
		}
	}
	if meta.Twitter != nil {
		for _, image := range meta.Twitter.Images {
			values = append(values, image.URL)
		}
	}
	if meta.Icons != nil {
		for _, asset := range meta.Icons.Icon {
			values = append(values, asset.URL)
		}
		for _, asset := range meta.Icons.Shortcut {
			values = append(values, asset.URL)
		}
		for _, asset := range meta.Icons.Apple {
			values = append(values, asset.URL)
		}
		for _, asset := range meta.Icons.Other {
			values = append(values, asset.URL)
		}
	}
	values = append(values, meta.Manifest)
	return values
}

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func dedupeResolvedLinks(links []ResolvedLink) []ResolvedLink {
	if len(links) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]ResolvedLink, 0, len(links))
	for _, link := range links {
		key := strings.Join([]string{link.Rel, link.URL, link.Sizes, link.Media}, "\x00")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, link)
	}
	return out
}

func dedupeThemeColorTags(tags []MetaTag) []MetaTag {
	if len(tags) == 0 {
		return nil
	}
	order := []string{}
	values := map[string]MetaTag{}
	for _, tag := range tags {
		key := strings.TrimSpace(tag.Media)
		if _, ok := values[key]; !ok {
			order = append(order, key)
		}
		values[key] = tag
	}
	out := make([]MetaTag, 0, len(order))
	for _, key := range order {
		out = append(out, values[key])
	}
	return out
}

func dedupeMediaAssets(assets []MediaAsset) []MediaAsset {
	if len(assets) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]MediaAsset, 0, len(assets))
	for _, asset := range assets {
		key := strings.Join([]string{
			asset.URL,
			fmt.Sprintf("%d", asset.Width),
			fmt.Sprintf("%d", asset.Height),
			asset.Alt,
			asset.Type,
		}, "\x00")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, asset)
	}
	return out
}

func isZeroTitle(title Title) bool {
	return strings.TrimSpace(title.Absolute) == "" &&
		strings.TrimSpace(title.Default) == "" &&
		strings.TrimSpace(title.Template) == ""
}

func isZeroAlternates(alternates Alternates) bool {
	return strings.TrimSpace(alternates.Canonical) == "" &&
		len(alternates.Languages) == 0 &&
		len(alternates.Media) == 0 &&
		len(alternates.Types) == 0
}

func isZeroRobots(robots Robots) bool {
	return robots.Index == nil &&
		robots.Follow == nil &&
		!robots.NoArchive &&
		!robots.NoImageAI &&
		!robots.NoTranslate &&
		(robots.GoogleBot == nil || (robots.GoogleBot.Index == nil &&
			robots.GoogleBot.Follow == nil &&
			robots.GoogleBot.MaxSnippet == 0 &&
			strings.TrimSpace(robots.GoogleBot.MaxImagePreview) == "" &&
			robots.GoogleBot.MaxVideoPreview == 0))
}

func isZeroIcons(icons Icons) bool {
	return len(icons.Icon) == 0 &&
		len(icons.Shortcut) == 0 &&
		len(icons.Apple) == 0 &&
		len(icons.Other) == 0
}

func isZeroVerification(verification Verification) bool {
	return strings.TrimSpace(verification.Google) == "" &&
		strings.TrimSpace(verification.Bing) == "" &&
		strings.TrimSpace(verification.Yandex) == "" &&
		strings.TrimSpace(verification.Yahoo) == "" &&
		len(verification.Other) == 0
}

func isZeroOpenGraph(openGraph OpenGraph) bool {
	return strings.TrimSpace(openGraph.Type) == "" &&
		strings.TrimSpace(openGraph.URL) == "" &&
		strings.TrimSpace(openGraph.SiteName) == "" &&
		strings.TrimSpace(openGraph.Locale) == "" &&
		strings.TrimSpace(openGraph.Title) == "" &&
		strings.TrimSpace(openGraph.Description) == "" &&
		len(openGraph.Images) == 0 &&
		openGraph.Article == nil
}

func isZeroTwitter(twitter Twitter) bool {
	return strings.TrimSpace(twitter.Card) == "" &&
		strings.TrimSpace(twitter.Site) == "" &&
		strings.TrimSpace(twitter.Creator) == "" &&
		strings.TrimSpace(twitter.Title) == "" &&
		strings.TrimSpace(twitter.Description) == "" &&
		len(twitter.Images) == 0
}

func applyTitleTemplate(template, value string) string {
	template = strings.TrimSpace(template)
	value = strings.TrimSpace(value)
	if template == "" || value == "" {
		return value
	}
	return strings.ReplaceAll(template, "%s", value)
}

func composeTitleTemplate(parent, child string) string {
	parent = strings.TrimSpace(parent)
	child = strings.TrimSpace(child)
	switch {
	case child == "":
		return parent
	case parent == "":
		return child
	default:
		return applyTitleTemplate(parent, child)
	}
}

func cloneMetadata(meta Metadata) Metadata {
	return Metadata{
		Title:        meta.Title,
		Description:  meta.Description,
		MetadataBase: meta.MetadataBase,
		Alternates:   cloneAlternates(meta.Alternates),
		Robots:       cloneRobots(meta.Robots),
		Icons:        cloneIcons(meta.Icons),
		Manifest:     meta.Manifest,
		Verification: cloneVerification(meta.Verification),
		ThemeColor:   cloneThemeColors(meta.ThemeColor),
		OpenGraph:    cloneOpenGraph(meta.OpenGraph),
		Twitter:      cloneTwitter(meta.Twitter),
		JSONLD:       cloneAnySlice(meta.JSONLD),
		Other:        cloneMetaTags(meta.Other),
		Links:        cloneLinkTags(meta.Links),
	}
}

func cloneResolvedMetadata(meta ResolvedMetadata) ResolvedMetadata {
	out := ResolvedMetadata{
		Title:        meta.Title,
		Description:  meta.Description,
		CanonicalURL: meta.CanonicalURL,
		Alternates:   cloneResolvedAlternates(meta.Alternates),
		Robots:       cloneResolvedRobots(meta.Robots),
		Icons:        cloneResolvedLinks(meta.Icons),
		Manifest:     meta.Manifest,
		Verification: cloneMetaTags(meta.Verification),
		ThemeColor:   cloneMetaTags(meta.ThemeColor),
		OpenGraph:    cloneResolvedOpenGraph(meta.OpenGraph),
		Twitter:      cloneResolvedTwitter(meta.Twitter),
		JSONLD:       cloneAnySlice(meta.JSONLD),
		Other:        cloneMetaTags(meta.Other),
		Links:        cloneLinkTags(meta.Links),
	}
	if meta.MetadataBase != nil {
		base := *meta.MetadataBase
		out.MetadataBase = &base
	}
	return out
}

func cloneResolvedAlternates(alternates ResolvedAlternates) ResolvedAlternates {
	return ResolvedAlternates{
		Languages: cloneStringMap(alternates.Languages),
		Media:     cloneStringMap(alternates.Media),
		Types:     cloneStringMap(alternates.Types),
	}
}

func cloneResolvedRobots(robots ResolvedRobots) ResolvedRobots {
	out := ResolvedRobots{
		Index:       cloneBoolPtr(robots.Index),
		Follow:      cloneBoolPtr(robots.Follow),
		NoArchive:   robots.NoArchive,
		NoImageAI:   robots.NoImageAI,
		NoTranslate: robots.NoTranslate,
	}
	if robots.GoogleBot != nil {
		out.GoogleBot = &ResolvedRobotsAgent{
			Index:           cloneBoolPtr(robots.GoogleBot.Index),
			Follow:          cloneBoolPtr(robots.GoogleBot.Follow),
			MaxSnippet:      robots.GoogleBot.MaxSnippet,
			MaxImagePreview: robots.GoogleBot.MaxImagePreview,
			MaxVideoPreview: robots.GoogleBot.MaxVideoPreview,
		}
	}
	return out
}

func cloneResolvedLinks(links []ResolvedLink) []ResolvedLink {
	if len(links) == 0 {
		return nil
	}
	out := make([]ResolvedLink, len(links))
	copy(out, links)
	return out
}

func cloneResolvedOpenGraph(openGraph ResolvedOpenGraph) ResolvedOpenGraph {
	out := ResolvedOpenGraph{
		Type:        openGraph.Type,
		URL:         openGraph.URL,
		SiteName:    openGraph.SiteName,
		Locale:      openGraph.Locale,
		Title:       openGraph.Title,
		Description: openGraph.Description,
		Images:      cloneMediaAssets(openGraph.Images),
	}
	if openGraph.Article != nil {
		out.Article = cloneOpenGraphArticle(openGraph.Article)
	}
	return out
}

func cloneResolvedTwitter(twitter ResolvedTwitter) ResolvedTwitter {
	return ResolvedTwitter{
		Card:        twitter.Card,
		Site:        twitter.Site,
		Creator:     twitter.Creator,
		Title:       twitter.Title,
		Description: twitter.Description,
		Images:      cloneMediaAssets(twitter.Images),
	}
}

func cloneAlternates(alternates *Alternates) *Alternates {
	if alternates == nil {
		return nil
	}
	return &Alternates{
		Canonical: alternates.Canonical,
		Languages: cloneStringMap(alternates.Languages),
		Media:     cloneStringMap(alternates.Media),
		Types:     cloneStringMap(alternates.Types),
	}
}

func cloneRobots(robots *Robots) *Robots {
	if robots == nil {
		return nil
	}
	out := &Robots{
		Index:       cloneBoolPtr(robots.Index),
		Follow:      cloneBoolPtr(robots.Follow),
		NoArchive:   robots.NoArchive,
		NoImageAI:   robots.NoImageAI,
		NoTranslate: robots.NoTranslate,
	}
	if robots.GoogleBot != nil {
		out.GoogleBot = &RobotsAgent{
			Index:           cloneBoolPtr(robots.GoogleBot.Index),
			Follow:          cloneBoolPtr(robots.GoogleBot.Follow),
			MaxSnippet:      robots.GoogleBot.MaxSnippet,
			MaxImagePreview: robots.GoogleBot.MaxImagePreview,
			MaxVideoPreview: robots.GoogleBot.MaxVideoPreview,
		}
	}
	return out
}

func cloneIcons(icons *Icons) *Icons {
	if icons == nil {
		return nil
	}
	return &Icons{
		Icon:     cloneIconAssets(icons.Icon),
		Shortcut: cloneIconAssets(icons.Shortcut),
		Apple:    cloneIconAssets(icons.Apple),
		Other:    cloneIconAssets(icons.Other),
	}
}

func cloneVerification(verification *Verification) *Verification {
	if verification == nil {
		return nil
	}
	return &Verification{
		Google: verification.Google,
		Bing:   verification.Bing,
		Yandex: verification.Yandex,
		Yahoo:  verification.Yahoo,
		Other:  cloneStringMap(verification.Other),
	}
}

func cloneOpenGraph(openGraph *OpenGraph) *OpenGraph {
	if openGraph == nil {
		return nil
	}
	out := &OpenGraph{
		Type:        openGraph.Type,
		URL:         openGraph.URL,
		SiteName:    openGraph.SiteName,
		Locale:      openGraph.Locale,
		Title:       openGraph.Title,
		Description: openGraph.Description,
		Images:      cloneMediaAssets(openGraph.Images),
	}
	if openGraph.Article != nil {
		out.Article = cloneOpenGraphArticle(openGraph.Article)
	}
	return out
}

func cloneTwitter(twitter *Twitter) *Twitter {
	if twitter == nil {
		return nil
	}
	return &Twitter{
		Card:        twitter.Card,
		Site:        twitter.Site,
		Creator:     twitter.Creator,
		Title:       twitter.Title,
		Description: twitter.Description,
		Images:      cloneMediaAssets(twitter.Images),
	}
}

func mergeAlternates(parent, child *Alternates) *Alternates {
	if parent == nil && child == nil {
		return nil
	}
	out := cloneAlternates(parent)
	if out == nil {
		out = &Alternates{}
	}
	if child == nil {
		return out
	}
	if value := strings.TrimSpace(child.Canonical); value != "" {
		out.Canonical = value
	}
	out.Languages = mergeStringMaps(out.Languages, child.Languages)
	out.Media = mergeStringMaps(out.Media, child.Media)
	out.Types = mergeStringMaps(out.Types, child.Types)
	return out
}

func mergeRobots(parent, child *Robots) *Robots {
	if parent == nil && child == nil {
		return nil
	}
	out := cloneRobots(parent)
	if out == nil {
		out = &Robots{}
	}
	if child == nil {
		return out
	}
	if child.Index != nil {
		out.Index = cloneBoolPtr(child.Index)
	}
	if child.Follow != nil {
		out.Follow = cloneBoolPtr(child.Follow)
	}
	out.NoArchive = out.NoArchive || child.NoArchive
	out.NoImageAI = out.NoImageAI || child.NoImageAI
	out.NoTranslate = out.NoTranslate || child.NoTranslate
	if child.GoogleBot != nil {
		if out.GoogleBot == nil {
			out.GoogleBot = &RobotsAgent{}
		}
		if child.GoogleBot.Index != nil {
			out.GoogleBot.Index = cloneBoolPtr(child.GoogleBot.Index)
		}
		if child.GoogleBot.Follow != nil {
			out.GoogleBot.Follow = cloneBoolPtr(child.GoogleBot.Follow)
		}
		if child.GoogleBot.MaxSnippet != 0 {
			out.GoogleBot.MaxSnippet = child.GoogleBot.MaxSnippet
		}
		if value := strings.TrimSpace(child.GoogleBot.MaxImagePreview); value != "" {
			out.GoogleBot.MaxImagePreview = value
		}
		if child.GoogleBot.MaxVideoPreview != 0 {
			out.GoogleBot.MaxVideoPreview = child.GoogleBot.MaxVideoPreview
		}
	}
	return out
}

func mergeIcons(parent, child *Icons) *Icons {
	if parent == nil && child == nil {
		return nil
	}
	out := cloneIcons(parent)
	if out == nil {
		out = &Icons{}
	}
	if child == nil {
		return out
	}
	if child.Icon != nil {
		out.Icon = cloneIconAssets(child.Icon)
	}
	if child.Shortcut != nil {
		out.Shortcut = cloneIconAssets(child.Shortcut)
	}
	if child.Apple != nil {
		out.Apple = cloneIconAssets(child.Apple)
	}
	if child.Other != nil {
		out.Other = cloneIconAssets(child.Other)
	}
	return out
}

func mergeVerification(parent, child *Verification) *Verification {
	if parent == nil && child == nil {
		return nil
	}
	out := cloneVerification(parent)
	if out == nil {
		out = &Verification{}
	}
	if child == nil {
		return out
	}
	if value := strings.TrimSpace(child.Google); value != "" {
		out.Google = value
	}
	if value := strings.TrimSpace(child.Bing); value != "" {
		out.Bing = value
	}
	if value := strings.TrimSpace(child.Yandex); value != "" {
		out.Yandex = value
	}
	if value := strings.TrimSpace(child.Yahoo); value != "" {
		out.Yahoo = value
	}
	out.Other = mergeStringMaps(out.Other, child.Other)
	return out
}

func mergeOpenGraph(parent, child *OpenGraph) *OpenGraph {
	if parent == nil && child == nil {
		return nil
	}
	out := cloneOpenGraph(parent)
	if out == nil {
		out = &OpenGraph{}
	}
	if child == nil {
		return out
	}
	if value := strings.TrimSpace(child.Type); value != "" {
		out.Type = value
	}
	if value := strings.TrimSpace(child.URL); value != "" {
		out.URL = value
	}
	if value := strings.TrimSpace(child.SiteName); value != "" {
		out.SiteName = value
	}
	if value := strings.TrimSpace(child.Locale); value != "" {
		out.Locale = value
	}
	if value := strings.TrimSpace(child.Title); value != "" {
		out.Title = value
	}
	if value := strings.TrimSpace(child.Description); value != "" {
		out.Description = value
	}
	if len(child.Images) > 0 {
		parentImages := []MediaAsset(nil)
		if parent != nil {
			parentImages = parent.Images
		}
		out.Images = append(cloneMediaAssets(child.Images), cloneMediaAssets(parentImages)...)
	}
	if child.Article != nil {
		out.Article = mergeOpenGraphArticle(parentArticle(parent), child.Article)
	}
	return out
}

func mergeTwitter(parent, child *Twitter) *Twitter {
	if parent == nil && child == nil {
		return nil
	}
	out := cloneTwitter(parent)
	if out == nil {
		out = &Twitter{}
	}
	if child == nil {
		return out
	}
	if value := strings.TrimSpace(child.Card); value != "" {
		out.Card = value
	}
	if value := strings.TrimSpace(child.Site); value != "" {
		out.Site = value
	}
	if value := strings.TrimSpace(child.Creator); value != "" {
		out.Creator = value
	}
	if value := strings.TrimSpace(child.Title); value != "" {
		out.Title = value
	}
	if value := strings.TrimSpace(child.Description); value != "" {
		out.Description = value
	}
	if len(child.Images) > 0 {
		parentImages := []MediaAsset(nil)
		if parent != nil {
			parentImages = parent.Images
		}
		out.Images = append(cloneMediaAssets(child.Images), cloneMediaAssets(parentImages)...)
	}
	return out
}

func parentArticle(parent *OpenGraph) *OpenGraphArticle {
	if parent == nil {
		return nil
	}
	return parent.Article
}

func mergeOpenGraphArticle(parent, child *OpenGraphArticle) *OpenGraphArticle {
	if parent == nil && child == nil {
		return nil
	}
	out := &OpenGraphArticle{}
	if parent != nil {
		*out = *cloneOpenGraphArticle(parent)
	}
	if child == nil {
		return out
	}
	if value := strings.TrimSpace(child.PublishedTime); value != "" {
		out.PublishedTime = value
	}
	if value := strings.TrimSpace(child.ModifiedTime); value != "" {
		out.ModifiedTime = value
	}
	if value := strings.TrimSpace(child.Section); value != "" {
		out.Section = value
	}
	if len(child.Authors) > 0 {
		out.Authors = append(cloneStrings(child.Authors), cloneStrings(parentAuthors(parent))...)
	}
	if len(child.Tags) > 0 {
		out.Tags = append(cloneStrings(child.Tags), cloneStrings(parentTags(parent))...)
	}
	return out
}

func parentAuthors(parent *OpenGraphArticle) []string {
	if parent == nil {
		return nil
	}
	return parent.Authors
}

func parentTags(parent *OpenGraphArticle) []string {
	if parent == nil {
		return nil
	}
	return parent.Tags
}

func cloneOpenGraphArticle(article *OpenGraphArticle) *OpenGraphArticle {
	if article == nil {
		return nil
	}
	return &OpenGraphArticle{
		PublishedTime: article.PublishedTime,
		ModifiedTime:  article.ModifiedTime,
		Authors:       cloneStrings(article.Authors),
		Tags:          cloneStrings(article.Tags),
		Section:       article.Section,
	}
}

func cloneMediaAssets(assets []MediaAsset) []MediaAsset {
	if len(assets) == 0 {
		return nil
	}
	out := make([]MediaAsset, len(assets))
	copy(out, assets)
	return out
}

func cloneIconAssets(assets []IconAsset) []IconAsset {
	if len(assets) == 0 {
		return nil
	}
	out := make([]IconAsset, len(assets))
	copy(out, assets)
	return out
}

func cloneMetaTags(tags []MetaTag) []MetaTag {
	if len(tags) == 0 {
		return nil
	}
	out := make([]MetaTag, len(tags))
	copy(out, tags)
	return out
}

func cloneLinkTags(tags []LinkTag) []LinkTag {
	if len(tags) == 0 {
		return nil
	}
	out := make([]LinkTag, len(tags))
	copy(out, tags)
	return out
}

func cloneThemeColors(colors []ThemeColor) []ThemeColor {
	if len(colors) == 0 {
		return nil
	}
	out := make([]ThemeColor, len(colors))
	copy(out, colors)
	return out
}

func cloneStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, len(values))
	copy(out, values)
	return out
}

func cloneAnySlice(values []any) []any {
	if len(values) == 0 {
		return nil
	}
	out := make([]any, len(values))
	copy(out, values)
	return out
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func mergeStringMaps(base, extra map[string]string) map[string]string {
	if len(base) == 0 && len(extra) == 0 {
		return nil
	}
	out := cloneStringMap(base)
	if out == nil {
		out = map[string]string{}
	}
	for key, value := range extra {
		out[key] = value
	}
	return out
}

func cloneBoolPtr(value *bool) *bool {
	if value == nil {
		return nil
	}
	out := *value
	return &out
}

// OpenGraphImages returns the effective Open Graph image input slice.
func (m Metadata) OpenGraphImages() []MediaAsset {
	if m.OpenGraph == nil {
		return nil
	}
	return m.OpenGraph.Images
}

// AlternateLanguages returns the effective alternate-language input map.
func (m Metadata) AlternateLanguages() map[string]string {
	if m.Alternates == nil {
		return nil
	}
	return m.Alternates.Languages
}

// UnmarshalJSON accepts either a structured title object or a plain string.
func (t *Title) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err == nil {
		*t = Title{Default: value}
		return nil
	}
	type alias Title
	var decoded alias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*t = Title(decoded)
	return nil
}

// UnmarshalJSON accepts the new structured metadata model while still reading
// legacy sidecar keys during the transition.
func (m *Metadata) UnmarshalJSON(data []byte) error {
	type alias Metadata
	var decoded alias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*m = Metadata(decoded)

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if value, ok := raw["canonical"]; ok && (m.Alternates == nil || strings.TrimSpace(m.Alternates.Canonical) == "") {
		var canonical string
		if err := json.Unmarshal(value, &canonical); err == nil && strings.TrimSpace(canonical) != "" {
			if m.Alternates == nil {
				m.Alternates = &Alternates{}
			}
			m.Alternates.Canonical = canonical
		}
	}
	legacyImage := MediaAsset{}
	if value, ok := raw["image"]; ok && (m.OpenGraph == nil || len(m.OpenGraph.Images) == 0) {
		_ = json.Unmarshal(value, &legacyImage.URL)
	}
	if value, ok := raw["imageWidth"]; ok {
		_ = json.Unmarshal(value, &legacyImage.Width)
	}
	if value, ok := raw["imageHeight"]; ok {
		_ = json.Unmarshal(value, &legacyImage.Height)
	}
	if strings.TrimSpace(legacyImage.URL) != "" {
		if m.OpenGraph == nil {
			m.OpenGraph = &OpenGraph{}
		}
		m.OpenGraph.Images = append([]MediaAsset{legacyImage}, m.OpenGraph.Images...)
	}
	if value, ok := raw["type"]; ok && (m.OpenGraph == nil || strings.TrimSpace(m.OpenGraph.Type) == "") {
		var typ string
		if err := json.Unmarshal(value, &typ); err == nil && strings.TrimSpace(typ) != "" {
			if m.OpenGraph == nil {
				m.OpenGraph = &OpenGraph{}
			}
			m.OpenGraph.Type = typ
		}
	}
	if value, ok := raw["twitterCard"]; ok && (m.Twitter == nil || strings.TrimSpace(m.Twitter.Card) == "") {
		var card string
		if err := json.Unmarshal(value, &card); err == nil && strings.TrimSpace(card) != "" {
			if m.Twitter == nil {
				m.Twitter = &Twitter{}
			}
			m.Twitter.Card = card
		}
	}
	if value, ok := raw["robots"]; ok && m.Robots == nil {
		var legacy string
		if err := json.Unmarshal(value, &legacy); err == nil && strings.TrimSpace(legacy) != "" {
			m.Robots = parseLegacyRobots(legacy)
		}
	}
	if value, ok := raw["meta"]; ok && len(m.Other) == 0 {
		var tags []MetaTag
		if err := json.Unmarshal(value, &tags); err == nil {
			m.Other = tags
		}
	}
	if value, ok := raw["jsonLD"]; ok && len(m.JSONLD) == 0 {
		var payload []any
		if err := json.Unmarshal(value, &payload); err == nil {
			m.JSONLD = payload
		}
	}
	return nil
}

func parseLegacyRobots(value string) *Robots {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	robots := &Robots{}
	for _, part := range strings.Split(value, ",") {
		switch strings.ToLower(strings.TrimSpace(part)) {
		case "index":
			robots.Index = boolPtr(true)
		case "noindex":
			robots.Index = boolPtr(false)
		case "follow":
			robots.Follow = boolPtr(true)
		case "nofollow":
			robots.Follow = boolPtr(false)
		case "noarchive":
			robots.NoArchive = true
		case "noimageai":
			robots.NoImageAI = true
		case "notranslate":
			robots.NoTranslate = true
		}
	}
	return robots
}

func boolPtr(value bool) *bool {
	return &value
}
