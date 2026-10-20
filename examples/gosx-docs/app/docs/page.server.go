package docs

import (
	"fmt"
	"strings"

	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
)

func init() {
	docsapp.RegisterStaticDocsPage(
		"Documentation",
		"Search the complete GoSX guide and follow a server-first learning path from components to realtime and GPU systems.",
		route.FileModuleOptions{
			Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
				query := normalizedDocsQuery(ctx.Query("q"))
				results := docsapp.SearchDocsCatalog(query)
				return map[string]any{
					"mode":        "",
					"title":       "Documentation",
					"description": "Search the complete GoSX guide or follow a server-first path from typed components to realtime and GPU systems.",
					"tags":        []string{"guides", "server search", "source linked"},
					"toc": []map[string]string{
						{"href": "#search", "label": "Search"},
						{"href": "#browse", "label": "Browse all guides"},
					},
					"query":             query,
					"hasQuery":          query != "",
					"hasResults":        len(results) > 0,
					"noResults":         query != "" && len(results) == 0,
					"resultSummary":     docsResultSummary(query, len(results)),
					"results":           docsSearchResultData(results),
					"directorySections": docsDirectoryData(docsapp.DocsCatalog()),
				}, nil
			},
		},
	)
}

func normalizedDocsQuery(query string) string {
	query = strings.TrimSpace(query)
	runes := []rune(query)
	if len(runes) > 80 {
		query = string(runes[:80])
	}
	return query
}

func docsResultSummary(query string, count int) string {
	if query == "" {
		return "Search titles, descriptions, keywords, routes, and source paths."
	}
	if count == 0 {
		return fmt.Sprintf("No guides match %q.", query)
	}
	if count == 1 {
		return fmt.Sprintf("1 guide matches %q.", query)
	}
	return fmt.Sprintf("%d guides match %q.", count, query)
}

func docsSearchResultData(results []docsapp.DocSearchResult) []map[string]any {
	sections := docSectionTitles(docsapp.DocsCatalog())
	data := make([]map[string]any, 0, len(results))
	for _, result := range results {
		entry := result.Entry
		data = append(data, map[string]any{
			"title":       entry.Title,
			"href":        entry.Href,
			"description": entry.Description,
			"section":     sections[entry.Section],
			"source":      entry.Source,
			"keywords":    strings.Join(entry.Keywords, " · "),
		})
	}
	return data
}

func docsDirectoryData(sections []docsapp.DocSection) []map[string]any {
	data := make([]map[string]any, 0, len(sections))
	for _, section := range sections {
		entries := make([]map[string]any, 0, len(section.Entries))
		for _, entry := range section.Entries {
			entries = append(entries, map[string]any{
				"title":       entry.Title,
				"href":        entry.Href,
				"description": entry.Description,
				"source":      entry.Source,
			})
		}
		data = append(data, map[string]any{
			"id":          section.ID,
			"title":       section.Title,
			"description": section.Description,
			"entries":     entries,
		})
	}
	return data
}

func docSectionTitles(sections []docsapp.DocSection) map[string]string {
	titles := make(map[string]string, len(sections))
	for _, section := range sections {
		titles[section.ID] = section.Title
	}
	return titles
}
