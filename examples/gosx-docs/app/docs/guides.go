package docs

import (
	"fmt"
	"html"
	"net/url"
	"path"
	"regexp"
	"strings"
	"sync"
	"unicode"

	"github.com/odvcencio/mdpp"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/content"
	guidesource "m31labs.dev/gosx/docs"
	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

// The guides listed in the docs catalog with a docs/*.md source render from
// that Markdown file. One file is the single source: GitHub shows it, and this
// route renders the same text, so a guide cannot exist in two versions.

const (
	guideRoutePrefix = "/docs/guides/"
)

var (
	guideH1      = regexp.MustCompile(`(?s)^\s*<h1[^>]*>.*?</h1>\s*`)
	guideHeading = regexp.MustCompile(`(?s)<(h[23])>(.*?)</h[23]>`)
	guideTag     = regexp.MustCompile(`<[^>]*>`)
	guideHref    = regexp.MustCompile(`href="([^"]*)"`)
	guideCache   sync.Map // slug -> renderedGuide; the Markdown is embedded, so it never changes
)

// guideAnchor builds a heading id the way GitHub does, so a link written for
// the file on GitHub also works on the site: lower case, punctuation dropped,
// each space becomes a hyphen ("A & B" is "a--b").
func guideAnchor(text string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(text) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_':
			b.WriteRune(r)
		case unicode.IsSpace(r):
			b.WriteByte('-')
		}
	}
	return b.String()
}

func init() {
	docsapp.RegisterDocsPageAt("docs/guides/[slug]/page.gsx", "Guide", "A GoSX guide rendered from its Markdown source.", route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, _ route.FilePage) (any, error) {
			entry, ok := guideEntry(ctx.Param("slug"))
			if !ok {
				return nil, route.NotFound("guide")
			}
			guide, err := cachedGuide(ctx.Param("slug"), entry)
			if err != nil {
				return nil, err
			}
			return map[string]any{
				"mode":        "light",
				"title":       entry.Title,
				"description": entry.Description,
				"tags":        guideTags(entry),
				"toc":         guide.toc,
				"body":        gosx.RawHTML(guide.html),
			}, nil
		},
		Metadata: func(ctx *route.RouteContext, _ route.FilePage, _ any) (server.Metadata, error) {
			entry, ok := guideEntry(ctx.Param("slug"))
			if !ok {
				return server.Metadata{}, nil
			}
			return server.Metadata{
				Title:       server.Title{Absolute: entry.Title + " | GoSX"},
				Description: entry.Description,
			}, nil
		},
	})
}

type renderedGuide struct {
	html string
	toc  []map[string]string
}

// guideEntry finds the catalog entry whose route is /docs/guides/<slug>.
func guideEntry(slug string) (docsapp.DocEntry, bool) {
	href := guideRoutePrefix + slug
	for _, section := range docsapp.DocsCatalog() {
		for _, entry := range section.Entries {
			if entry.Href == href && strings.HasPrefix(entry.Source, "docs/") && strings.HasSuffix(entry.Source, ".md") {
				return entry, true
			}
		}
	}
	return docsapp.DocEntry{}, false
}

func guideTags(entry docsapp.DocEntry) []string {
	tags := entry.Keywords
	if len(tags) > 4 {
		tags = tags[:4]
	}
	return append([]string(nil), tags...)
}

func cachedGuide(slug string, entry docsapp.DocEntry) (renderedGuide, error) {
	if cached, ok := guideCache.Load(slug); ok {
		return cached.(renderedGuide), nil
	}
	guide, err := renderGuide(entry)
	if err != nil {
		return renderedGuide{}, err
	}
	guideCache.Store(slug, guide)
	return guide, nil
}

// renderGuide renders a catalog entry's Markdown source to sanitized HTML,
// adds heading ids and a table of contents, and points relative links at the
// site or the repository.
func renderGuide(entry docsapp.DocEntry) (renderedGuide, error) {
	name := strings.TrimPrefix(entry.Source, "docs/")
	source, err := guidesource.Guides.ReadFile(name)
	if err != nil {
		return renderedGuide{}, fmt.Errorf("guide %s: %w", entry.Source, err)
	}
	doc := content.ParseDocument("docs", name, string(source))
	body, err := doc.HTML(mdpp.RenderOptions{Sanitize: true})
	if err != nil {
		return renderedGuide{}, fmt.Errorf("render %s: %w", entry.Source, err)
	}
	body = guideH1.ReplaceAllString(body, "")

	var toc []map[string]string
	seen := map[string]int{}
	body = guideHeading.ReplaceAllStringFunc(body, func(match string) string {
		parts := guideHeading.FindStringSubmatch(match)
		level, inner := parts[1], parts[2]
		text := html.UnescapeString(guideTag.ReplaceAllString(inner, ""))
		id := guideAnchor(text)
		if id == "" {
			id = "section"
		}
		base := id
		if n := seen[base]; n > 0 {
			id = fmt.Sprintf("%s-%d", base, n)
		}
		seen[base]++
		if level == "h2" {
			toc = append(toc, map[string]string{"href": "#" + id, "label": text})
		}
		return fmt.Sprintf(`<%s id="%s">%s</%s>`, level, id, inner, level)
	})
	body = guideHref.ReplaceAllStringFunc(body, func(match string) string {
		href := html.UnescapeString(guideHref.FindStringSubmatch(match)[1])
		return `href="` + html.EscapeString(guideLink(href)) + `"`
	})
	return renderedGuide{html: body, toc: toc}, nil
}

// guideLink maps a link written for GitHub onto the site: a link to another
// cataloged guide becomes its site route, and any other relative link becomes
// a repository URL.
func guideLink(href string) string {
	if href == "" || strings.HasPrefix(href, "#") || strings.HasPrefix(href, "/") {
		return href
	}
	if u, err := url.Parse(href); err != nil || u.Scheme != "" || u.Host != "" {
		return href
	}
	target, fragment, _ := strings.Cut(href, "#")
	resolved := path.Clean(path.Join("docs", target))
	if strings.HasSuffix(resolved, ".md") {
		for _, section := range docsapp.DocsCatalog() {
			for _, entry := range section.Entries {
				if entry.Source == resolved {
					if fragment != "" {
						return entry.Href + "#" + fragment
					}
					return entry.Href
				}
			}
		}
	}
	out := guideBlobBase() + resolved
	if fragment != "" {
		out += "#" + fragment
	}
	return out
}

// guideBlobBase links repository files at the deployed revision when the build
// knows it, so a link matches the code the page describes.
func guideBlobBase() string {
	ref := docsapp.SiteBuildInfo()["revision"]
	if ref == "" || ref == "unknown" {
		ref = "main"
	}
	return "https://github.com/odvcencio/gosx/blob/" + ref + "/"
}
