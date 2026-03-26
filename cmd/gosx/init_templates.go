package main

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

const (
	initTemplateApp  = "app"
	initTemplateDocs = "docs"
)

type scaffoldFile struct {
	Path     string
	Contents string
}

//go:embed templates/docs
var initTemplateFS embed.FS

func normalizeInitTemplate(template string) (string, error) {
	template = strings.ToLower(strings.TrimSpace(template))
	if template == "" {
		return initTemplateApp, nil
	}
	switch template {
	case initTemplateApp, initTemplateDocs:
		return template, nil
	default:
		return "", fmt.Errorf("unknown init template %q", template)
	}
}

func scaffoldFilesForTemplate(module, template string) ([]scaffoldFile, error) {
	switch template {
	case initTemplateApp:
		return []scaffoldFile{
			{Path: "go.mod", Contents: goModTemplate(module)},
			{Path: "main.go", Contents: mainTemplate(module)},
			{Path: ".env", Contents: envTemplate()},
			{Path: ".gitignore", Contents: gitignoreTemplate()},
			{Path: "app/layout.gsx", Contents: appLayoutTemplate()},
			{Path: "app/page.server.go", Contents: appHomeServerTemplate()},
			{Path: "app/page.gsx", Contents: appHomeTemplate()},
			{Path: "app/stack/page.server.go", Contents: appStackServerTemplate()},
			{Path: "app/stack/page.gsx", Contents: appStackTemplate()},
			{Path: "app/not-found.gsx", Contents: appNotFoundTemplate()},
			{Path: "app/error.gsx", Contents: appErrorTemplate()},
			{Path: "modules/modules.go", Contents: modulesTemplate(module)},
			{Path: "public/styles.css", Contents: stylesTemplate()},
		}, nil
	case initTemplateDocs:
		return docsTemplateFiles(module)
	default:
		return nil, fmt.Errorf("unknown init template %q", template)
	}
}

func docsTemplateFiles(module string) ([]scaffoldFile, error) {
	files := []scaffoldFile{
		{Path: "go.mod", Contents: goModTemplate(module)},
		{Path: ".env", Contents: docsEnvTemplate()},
		{Path: ".gitignore", Contents: gitignoreTemplate()},
	}

	err := fs.WalkDir(initTemplateFS, "templates/docs", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		contents, err := fs.ReadFile(initTemplateFS, path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		rel := strings.TrimPrefix(path, "templates/docs/")
		if rel == "" {
			return nil
		}
		if strings.HasSuffix(rel, ".gotmpl") {
			rel = strings.TrimSuffix(rel, ".gotmpl") + ".go"
		}
		files = append(files, scaffoldFile{
			Path:     rel,
			Contents: strings.ReplaceAll(string(contents), "__MODULE__", module),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})
	return files, nil
}

func docsEnvTemplate() string {
	return `PORT=8080
SESSION_SECRET=change-me-in-production
GOSX_ENV=development
`
}
