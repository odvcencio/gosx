package server

import (
	"m31labs.dev/gosx"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestManagedLinkAppRenderBoundaries(t *testing.T) {
	for _, mode := range []string{"default", "custom", "legacy", "stream"} {
		t.Run(mode, func(t *testing.T) {
			app := New()
			link := func(label string) gosx.Node { return gosx.ManagedLink("/docs?tab=one", nil, gosx.Text(label)) }
			app.Page("GET /docs", func(ctx *Context) gosx.Node {
				ctx.AddHead(link("head"))
				if mode == "stream" {
					return ctx.Defer(link("pending"), func() (gosx.Node, error) { return link("resolved"), nil })
				}
				return link("body")
			})
			switch mode {
			case "custom":
				app.SetDocument(func(doc *DocumentContext) gosx.Node {
					return gosx.El("html", gosx.El("head", doc.Head), gosx.El("body", doc.Body, link("custom")))
				})
			case "legacy":
				app.SetLayout(func(_ string, body gosx.Node) gosx.Node {
					return gosx.El("html", gosx.El("head"), gosx.El("body", body, link("legacy")))
				})
			}
			for _, target := range []string{"/docs?tab=one", "/docs?tab=two"} {
				w := httptest.NewRecorder()
				app.Build().ServeHTTP(w, httptest.NewRequest("GET", target, nil))
				if w.Code != 200 {
					t.Fatalf("status %d: %s", w.Code, w.Body.String())
				}
				html := w.Body.String()
				relation := "page"
				if target == "/docs?tab=two" {
					relation = "none"
				}
				labels := []string{"head", "body"}
				if mode == "stream" {
					labels = []string{"head", "pending", "resolved"}
				}
				if mode == "custom" || mode == "legacy" {
					labels = append(labels, mode)
				}
				for _, label := range labels {
					end := strings.Index(html, ">"+label+"</a>")
					if end < 0 {
						t.Fatalf("missing %s: %s", label, html)
					}
					start := strings.LastIndex(html[:end], "<a ")
					if start < 0 || !strings.Contains(html[start:end], `data-gosx-link-current="`+relation+`"`) {
						t.Fatalf("%s %s: %s", target, label, html)
					}
				}
			}
		})
	}
}

func TestManagedLinkLegacyServerLinkUnchanged(t *testing.T) {
	node := Link("/docs", gosx.Text("Docs"))
	want := `<a href="/docs" data-gosx-link data-gosx-link-state="idle" data-gosx-link-current-policy="auto" data-gosx-prefetch-state="idle" data-gosx-enhance="navigation" data-gosx-enhance-layer="bootstrap" data-gosx-fallback="native-link">Docs</a>`
	if got := gosx.RenderHTMLWithContext(node, gosx.RenderContext{RequestTarget: "/docs"}); got != want {
		t.Fatalf("got %s", got)
	}
	if got := gosx.RenderHTML(node); got != want {
		t.Fatalf("got %s", got)
	}
}
