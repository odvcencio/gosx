package docs

import (
	"strings"
	"testing"

	"m31labs.dev/gosx"
)

func TestPagesGuidesPreserveAnchorsAndCodeBlocks(t *testing.T) {
	for slug, anchors := range map[string][]string{
		"getting-started": {"overview", "install", "create-a-project", "project-structure", "authoring-styles", "dev-server", "next-steps"},
		"deployment":      {"build-output", "static-export", "edge-output", "server-deployment", "isr", "offline-windows", "docker"},
	} {
		t.Run(slug, func(t *testing.T) {
			data, err := LoadMarkdownDoc(slug)
			if err != nil {
				t.Fatal(err)
			}
			body := gosx.RenderHTML(data["content"].(gosx.Node))
			for _, id := range anchors {
				if strings.Count(body, `id="`+id+`"`) != 1 {
					t.Errorf("missing or duplicate historical anchor %q", id)
				}
			}
			if len(data["toc"].([]map[string]string)) != len(anchors) {
				t.Fatal("table of contents changed")
			}
			if !strings.Contains(body, "gosx") || !strings.Contains(body, "code-block") {
				t.Fatal("existing code renderer was not used")
			}
			if data["title"] == "" || data["description"] == "" {
				t.Fatal("metadata lost")
			}
		})
	}
}
