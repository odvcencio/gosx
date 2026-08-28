package docs

import (
	"log"

	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
)

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		Bindings: func(ctx *route.RouteContext, page route.FilePage, data any) route.FileTemplateBindings {
			currentPath := page.RoutePath
			if ctx != nil && ctx.Request != nil && ctx.Request.URL != nil && ctx.Request.URL.Path != "" {
				currentPath = ctx.Request.URL.Path
			}
			pageLinks := docsapp.DocsPageLinks(currentPath)
			return route.FileTemplateBindings{Values: map[string]any{
				"docsNavigation":       docsapp.DocsNavigation(currentPath),
				"docsIndexClassName":   docsapp.DocsIndexClassName(currentPath),
				"docsIndexCurrent":     docsapp.DocsIndexAriaCurrent(currentPath),
				"docsSectionClassName": docsLayoutSectionClassName(data),
				"docsPrevious":         pageLinks["previous"],
				"docsNext":             pageLinks["next"],
				"docsSourceURL":        pageLinks["sourceURL"],
				"docsSourcePath":       pageLinks["sourcePath"],
				"site":                 docsapp.SiteBuildInfo(),
			}}
		},
	}); err != nil {
		log.Fatal(err)
	}
}

func docsLayoutSectionClassName(data any) string {
	className := "docs-section"
	values, ok := data.(map[string]any)
	if ok && values["mode"] == "light" {
		className += " light"
	}
	return className
}
