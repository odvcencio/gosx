package docs

import (
	"log"
	"strings"

	"m31labs.dev/gosx/route"
)

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		Bindings: demoLayoutBindings,
	}); err != nil {
		log.Fatal(err)
	}
}

func demoLayoutBindings(ctx *route.RouteContext, page route.FilePage, _ any) route.FileTemplateBindings {
	current, hasCurrent := currentDemoForLayout(ctx, page)
	values := map[string]any{
		"currentDemoSlug":        "",
		"currentDemoTitle":       "Demo details",
		"currentDemoLesson":      "Choose a demo to inspect how it is built.",
		"currentDemoFacets":      "—",
		"currentDemoPackages":    "—",
		"currentDemoRenderMode":  "—",
		"currentDemoLimitations": "—",
		"currentDemoSourceURL":   nil,
		"currentDemoSourcePath":  "",
	}
	if hasCurrent {
		values["currentDemoSlug"] = current.Slug
		values["currentDemoTitle"] = current.Title
		values["currentDemoLesson"] = current.Lesson
		values["currentDemoFacets"] = demoValues(current.Facets)
		values["currentDemoPackages"] = demoValues(current.Packages)
		values["currentDemoRenderMode"] = current.RenderMode
		values["currentDemoLimitations"] = current.Limitations
		values["currentDemoSourceURL"] = demoSourceURL(current.SourcePath)
		values["currentDemoSourcePath"] = current.SourcePath
	}

	return route.FileTemplateBindings{
		Values: values,
		Funcs: map[string]any{
			"Demos":         Demos,
			"demoValues":    demoValues,
			"demoSourceURL": demoSourceURL,
			"demoAriaCurrent": func(slug string) any {
				if hasCurrent && slug == current.Slug {
					return "page"
				}
				return nil
			},
			"demoCurrentManaged": func(slug string) any {
				if hasCurrent && slug == current.Slug {
					return "true"
				}
				return nil
			},
		},
	}
}

func currentDemoForLayout(ctx *route.RouteContext, page route.FilePage) (DemoDefinition, bool) {
	currentPath := page.RoutePath
	if ctx != nil && ctx.Request != nil && ctx.Request.URL != nil && ctx.Request.URL.Path != "" {
		currentPath = ctx.Request.URL.Path
	}
	const prefix = "/demos/"
	if !strings.HasPrefix(currentPath, prefix) {
		return DemoDefinition{}, false
	}
	slug := strings.Trim(strings.TrimPrefix(currentPath, prefix), "/")
	if slug == "" || strings.Contains(slug, "/") {
		return DemoDefinition{}, false
	}
	return FindDemo(slug)
}
