package docs

import (
	"sort"
	"strings"
	"unicode"
)

// DocEntry is one public documentation guide. The catalog is the source of
// truth for search, persistent navigation, and sitemap generation.
type DocEntry struct {
	Title       string
	Href        string
	Description string
	Section     string
	Source      string
	Keywords    []string
	// Demo, when non-empty, is the slug of the catalog demo that proves this
	// guide's subject live in the browser. The docs shell renders a "See it
	// live" link from it, and the demos index mirrors it back as related
	// guides. Only set it when the demo genuinely exercises the capability
	// the guide documents; the cross-link tests enforce the pairing.
	Demo string
}

// DocSection groups guides into a stable learning path.
type DocSection struct {
	ID          string
	Title       string
	Description string
	Entries     []DocEntry
}

// DocSearchResult is a catalog entry ranked for a server-rendered query.
type DocSearchResult struct {
	Entry DocEntry
	Score int
}

var docsCatalog = []DocSection{
	{
		ID:          "start",
		Title:       "Start & foundations",
		Description: "Install GoSX, learn both component styles, and understand the compiler contract.",
		Entries: []DocEntry{
			{
				Title:       "Getting started",
				Href:        "/docs/getting-started",
				Description: "Create, run, and understand a GoSX project from the first command.",
				Section:     "start",
				Source:      "examples/gosx-docs/app/docs/getting-started/page.gsx",
				Keywords:    []string{"install", "quickstart", "init", "setup", "project"},
			},
			{
				Title:       "Typed component proof",
				Href:        "/docs/typed-live",
				Description: "The production-rendered strict component route behind this site.",
				Section:     "start",
				Source:      "examples/gosx-docs/app/docs/typed-live/page.gsx",
				Keywords:    []string{"strict", "typed", "tsx", "production", "proof", "props"},
			},
			{
				Title:       "Components",
				Href:        "/docs/components",
				Description: "Choose between strict typed components and the established Go-function style.",
				Section:     "start",
				Source:      "examples/gosx-docs/app/docs/components/page.gsx",
				Keywords:    []string{"component", "props", "strict", "tsx", "legacy", "markup"},
				Demo:        "playground",
			},
			{
				Title:       "Compiler",
				Href:        "/docs/compiler",
				Description: "Follow GSX through parsing, validation, lowering, type checks, and rendering.",
				Section:     "start",
				Source:      "examples/gosx-docs/app/docs/compiler/page.gsx",
				Keywords:    []string{"compiler", "grammar", "parser", "tree-sitter", "ir", "diagnostics"},
				Demo:        "playground",
			},
		},
	},
	{
		ID:          "server",
		Title:       "Server applications",
		Description: "Route requests, mutate data, protect users, stream work, and ship the result.",
		Entries: []DocEntry{
			{
				Title:       "Routing",
				Href:        "/docs/routing",
				Description: "File routes, layouts, dynamic parameters, loaders, actions, and route configuration.",
				Section:     "server",
				Source:      "examples/gosx-docs/app/docs/routing/page.gsx",
				Keywords:    []string{"routes", "layouts", "params", "loaders", "navigation", "metadata"},
			},
			{
				Title:       "Forms & actions",
				Href:        "/docs/forms",
				Description: "Handle server mutations with validation, field errors, CSRF, and redirects.",
				Section:     "server",
				Source:      "examples/gosx-docs/app/docs/forms/page.gsx",
				Keywords:    []string{"forms", "actions", "validation", "csrf", "redirect", "errors"},
				Demo:        "cms",
			},
			{
				Title:       "Auth",
				Href:        "/docs/auth",
				Description: "Sessions, magic links, passkeys, OAuth providers, and route guards.",
				Section:     "server",
				Source:      "examples/gosx-docs/app/docs/auth/page.gsx",
				Keywords:    []string{"auth", "sessions", "magic link", "passkeys", "webauthn", "oauth"},
			},
			{
				Title:       "Streaming",
				Href:        "/docs/streaming",
				Description: "Render visible fallbacks, defer slow regions, and replace them over one response.",
				Section:     "server",
				Source:      "examples/gosx-docs/app/docs/streaming/page.gsx",
				Keywords:    []string{"streaming", "defer", "suspense", "fallback", "ssr", "progressive"},
			},
			{
				Title:       "Runtime",
				Href:        "/docs/runtime",
				Description: "Managed navigation, script roles, prefetch, telemetry, and page disposal.",
				Section:     "server",
				Source:      "examples/gosx-docs/app/docs/runtime/page.gsx",
				Keywords:    []string{"runtime", "navigation", "prefetch", "telemetry", "lifecycle", "observability"},
			},
		},
	},
	{
		ID:          "interactive",
		Title:       "Interactive & realtime",
		Description: "Add reactive browser regions and shared multi-user state only where the app needs them.",
		Entries: []DocEntry{
			{
				Title:       "Signals",
				Href:        "/docs/signals",
				Description: "Use Go reactive values and the explicit signal subset compiled into islands.",
				Section:     "interactive",
				Source:      "examples/gosx-docs/app/docs/signals/page.gsx",
				Keywords:    []string{"signals", "derive", "watch", "batch", "reactive", "state"},
			},
			{
				Title:       "Islands",
				Href:        "/docs/islands",
				Description: "Hydrate explicit interactive regions on the shared GoSX browser VM.",
				Section:     "interactive",
				Source:      "examples/gosx-docs/app/docs/islands/page.gsx",
				Keywords:    []string{"islands", "hydration", "events", "handlers", "wasm", "vm"},
				Demo:        "playground",
			},
			{
				Title:       "Hubs & CRDT",
				Href:        "/docs/hubs",
				Description: "Coordinate presence, fanout, shared state, and conflict-free documents over WebSockets.",
				Section:     "interactive",
				Source:      "examples/gosx-docs/app/docs/hubs/page.gsx",
				Keywords:    []string{"hubs", "websocket", "presence", "crdt", "sync", "collaboration"},
				Demo:        "collab",
			},
		},
	},
	{
		ID:          "visual",
		Title:       "Visual systems",
		Description: "Manage media, motion, rendering surfaces, GPU scenes, and their operational limits.",
		Entries: []DocEntry{
			{
				Title:       "Images",
				Href:        "/docs/images",
				Description: "Resize local images and emit responsive markup through the managed asset surface.",
				Section:     "visual",
				Source:      "examples/gosx-docs/app/docs/images/page.gsx",
				Keywords:    []string{"images", "resize", "responsive", "cache", "assets", "media"},
			},
			{
				Title:       "Motion",
				Href:        "/docs/motion",
				Description: "Apply server-authored motion presets with reduced-motion behavior.",
				Section:     "visual",
				Source:      "examples/gosx-docs/app/docs/motion/page.gsx",
				Keywords:    []string{"animation", "motion", "transitions", "reduced motion", "accessibility"},
			},
			{
				Title:       "Text layout",
				Href:        "/docs/text-layout",
				Description: "Approximate text layout on the server and refine browser metrics when needed.",
				Section:     "visual",
				Source:      "examples/gosx-docs/app/docs/text-layout/page.gsx",
				Keywords:    []string{"text", "layout", "line breaking", "measurement", "font", "typography"},
			},
			{
				Title:       "Engines",
				Href:        "/docs/engines",
				Description: "Mount managed worker, surface, video, and GPU runtimes with explicit capabilities.",
				Section:     "visual",
				Source:      "examples/gosx-docs/app/docs/engines/page.gsx",
				Keywords:    []string{"engines", "surface", "worker", "webgpu", "wasm", "capabilities"},
			},
			{
				Title:       "3D engine",
				Href:        "/docs/scene3d",
				Description: "Declare typed scenes, materials, lighting, animation, and managed backend fallback in Go.",
				Section:     "visual",
				Source:      "examples/gosx-docs/app/docs/scene3d/page.gsx",
				Keywords:    []string{"3d", "scene3d", "webgpu", "webgl", "pbr", "scene graph", "selena"},
				Demo:        "scene3d",
			},
			{
				Title:       "Debugging Scene3D",
				Href:        "/docs/debugging-scene3d",
				Description: "Diagnose invisible geometry, capture failures, fallback, and GPU compositor bugs.",
				Section:     "visual",
				Source:      "examples/gosx-docs/app/docs/debugging-scene3d/page.gsx",
				Keywords:    []string{"scene3d", "debug", "webgpu", "webgl", "telemetry", "visual regression"},
				Demo:        "scene3d-bench",
			},
			{
				Title:       "Scene3D vs three.js",
				Href:        "/docs/scene3d-vs-threejs",
				Description: "Compare overlap, tradeoffs, browser bytes, and intentional surface-area differences.",
				Section:     "visual",
				Source:      "examples/gosx-docs/app/docs/scene3d-vs-threejs/page.gsx",
				Keywords:    []string{"scene3d", "three.js", "comparison", "bundle size", "webgpu", "tradeoffs"},
			},
		},
	},
	{
		ID:          "operations",
		Title:       "Operations & proof",
		Description: "Inspect what the framework ships, how this site uses it, and how the result reaches production.",
		Entries: []DocEntry{
			{
				Title:       "How this site works",
				Href:        "/docs/site",
				Description: "The routes, runtime surfaces, source, and deployment behind the documentation site.",
				Section:     "operations",
				Source:      "examples/gosx-docs/app/docs/site/page.gsx",
				Keywords:    []string{"dogfood", "architecture", "site", "routes", "runtime", "source", "deployment"},
			},
			{
				Title:       "Deployment",
				Href:        "/docs/deployment",
				Description: "Build, export, inspect, and operate a staged GoSX deployment bundle.",
				Section:     "operations",
				Source:      "examples/gosx-docs/app/docs/deployment/page.gsx",
				Keywords:    []string{"build", "deploy", "container", "static", "ssr", "isr", "offline"},
			},
		},
	},
}

// DocsCatalog returns an isolated copy so callers cannot mutate site-wide
// navigation or sitemap state.
func DocsCatalog() []DocSection {
	sections := make([]DocSection, len(docsCatalog))
	for i, section := range docsCatalog {
		sections[i] = section
		sections[i].Entries = make([]DocEntry, len(section.Entries))
		for j, entry := range section.Entries {
			sections[i].Entries[j] = cloneDocEntry(entry)
		}
	}
	return sections
}

// DocsCatalogRoutes returns every public catalog route in stable learning
// order, including the searchable documentation index itself.
func DocsCatalogRoutes() []string {
	routes := []string{"/docs"}
	for _, section := range docsCatalog {
		for _, entry := range section.Entries {
			routes = append(routes, entry.Href)
		}
	}
	return routes
}

// DocsPageLinks returns render-safe provenance and learning-path links for the
// current guide. The searchable index participates as the first step so every
// documentation page has a way forward and backward.
func DocsPageLinks(currentPath string) map[string]any {
	type catalogPage struct {
		title  string
		href   string
		source string
	}
	pages := []catalogPage{{
		title:  "Documentation",
		href:   "/docs",
		source: "examples/gosx-docs/app/docs/page.gsx",
	}}
	for _, section := range docsCatalog {
		for _, entry := range section.Entries {
			pages = append(pages, catalogPage{title: entry.Title, href: entry.Href, source: entry.Source})
		}
	}
	links := map[string]any{}
	for index, page := range pages {
		if page.href != currentPath {
			continue
		}
		if index > 0 {
			links["previous"] = map[string]any{
				"href":  pages[index-1].href,
				"label": pages[index-1].title,
			}
		}
		if index+1 < len(pages) {
			links["next"] = map[string]any{
				"href":  pages[index+1].href,
				"label": pages[index+1].title,
			}
		}
		if page.source != "" {
			ref := SiteBuildInfo()["revision"]
			if ref == "unknown" {
				ref = "main"
			}
			links["sourceURL"] = "https://github.com/odvcencio/gosx/blob/" + ref + "/" + page.source
			links["sourcePath"] = page.source
		}
		break
	}
	return links
}

// SearchDocsCatalog ranks catalog entries and requires every query token to
// match at least one searchable field. It deliberately runs on the server so
// search works without JavaScript.
func SearchDocsCatalog(query string) []DocSearchResult {
	terms := searchTerms(query)
	if len(terms) == 0 {
		return nil
	}

	results := make([]DocSearchResult, 0)
	for _, section := range docsCatalog {
		for _, entry := range section.Entries {
			score, matched := scoreDocEntry(section, entry, terms)
			if matched {
				results = append(results, DocSearchResult{Entry: cloneDocEntry(entry), Score: score})
			}
		}
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Score == results[j].Score {
			return results[i].Entry.Title < results[j].Entry.Title
		}
		return results[i].Score > results[j].Score
	})
	return results
}

func cloneDocEntry(entry DocEntry) DocEntry {
	entry.Keywords = append([]string(nil), entry.Keywords...)
	return entry
}

func searchTerms(query string) []string {
	query = strings.TrimSpace(strings.ToLower(query))
	runes := []rune(query)
	if len(runes) > 160 {
		query = string(runes[:160])
	}
	return strings.FieldsFunc(query, func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '-' || r == '+')
	})
}

func scoreDocEntry(section DocSection, entry DocEntry, terms []string) (int, bool) {
	title := strings.ToLower(entry.Title)
	description := strings.ToLower(entry.Description)
	sectionText := strings.ToLower(section.Title + " " + section.Description + " " + entry.Section)
	pathText := strings.ToLower(entry.Href + " " + entry.Source)
	keywords := strings.ToLower(strings.Join(entry.Keywords, " "))

	total := 0
	for _, term := range terms {
		score := 0
		if title == term {
			score += 24
		} else if strings.Contains(title, term) {
			score += 12
		}
		if containsWholeKeyword(entry.Keywords, term) {
			score += 10
		} else if strings.Contains(keywords, term) {
			score += 6
		}
		if strings.Contains(sectionText, term) {
			score += 4
		}
		if strings.Contains(description, term) {
			score += 3
		}
		if strings.Contains(pathText, term) {
			score += 2
		}
		if score == 0 {
			return 0, false
		}
		total += score
	}
	return total, true
}

func containsWholeKeyword(keywords []string, term string) bool {
	for _, keyword := range keywords {
		if strings.EqualFold(keyword, term) {
			return true
		}
	}
	return false
}

// DocsNavigation projects the catalog into render-safe grouped navigation with
// request-specific current-page styling.
func DocsNavigation(currentPath string) []map[string]any {
	sections := make([]map[string]any, 0, len(docsCatalog))
	for _, section := range docsCatalog {
		entries := make([]map[string]any, 0, len(section.Entries))
		for _, entry := range section.Entries {
			className := "docs-guide-link"
			var ariaCurrent any
			if currentPath == entry.Href {
				className += " is-current"
				ariaCurrent = "page"
			}
			entries = append(entries, map[string]any{
				"title":       entry.Title,
				"href":        entry.Href,
				"description": entry.Description,
				"className":   className,
				"ariaCurrent": ariaCurrent,
			})
		}
		sections = append(sections, map[string]any{
			"id":        section.ID,
			"headingID": "docs-nav-" + section.ID,
			"title":     section.Title,
			"entries":   entries,
		})
	}
	return sections
}

// DocsIndexClassName reports the render class for the searchable catalog link.
func DocsIndexClassName(currentPath string) string {
	className := "docs-guide-index"
	if currentPath == "/docs" {
		className += " is-current"
	}
	return className
}

// DocsIndexAriaCurrent marks the catalog link for assistive technology.
func DocsIndexAriaCurrent(currentPath string) any {
	if currentPath == "/docs" {
		return "page"
	}
	return nil
}
