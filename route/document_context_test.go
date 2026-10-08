package route

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"m31labs.dev/gosx"
	islandprogram "m31labs.dev/gosx/island/program"
	"m31labs.dev/gosx/server"
)

var _ server.DocumentFunc = server.HTMLDocument

func TestRouteContextDocumentPreservesNativePageState(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/docs?tab=runtime", nil)
	req.Header.Set("X-Request-ID", "request-123")
	router := NewRouter()
	ctx := router.newRouteContext(req)
	ctx.pattern = "GET /docs"
	ctx.SetStatus(http.StatusAccepted)
	ctx.SetLanguage(" en-US ")
	ctx.SetMetadata(server.Metadata{Title: server.Title{Absolute: "Runtime docs"}})
	ctx.AddHead(gosx.RawHTML(`<meta name="description" content="runtime">`))
	ctx.BodyAttrs(gosx.Attr("data-page", "runtime"))
	ctx.SetNonce("route-nonce")
	ctx.SetNavigationHead(func(nonce string) gosx.Node {
		return gosx.RawHTML(`<script data-route-navigation="true" nonce="` + nonce + `"></script>`)
	})
	ctx.Runtime().EnableBootstrap()
	ctx.Runtime().Island(islandprogram.CounterProgram(), map[string]int{"initial": 0})
	body := gosx.El("main", gosx.Text("runtime"))
	wantRuntime := ctx.RuntimeState().Summary()

	doc := ctx.Document("Fallback", body)
	if doc.Request != req {
		t.Fatal("document did not preserve the request")
	}
	if doc.Pattern != "GET /docs" || doc.Status != http.StatusAccepted {
		t.Fatalf("route/status lost: pattern=%q status=%d", doc.Pattern, doc.Status)
	}
	if doc.Title != "Runtime docs" || doc.Language != "en-US" {
		t.Fatalf("title/language lost: title=%q language=%q", doc.Title, doc.Language)
	}
	if doc.PageID != "gosx-doc-get-docs" || doc.Path != "/docs?tab=runtime" {
		t.Fatalf("page identity/path lost: id=%q path=%q", doc.PageID, doc.Path)
	}
	if doc.RequestID != "request-123" || doc.Metadata.Title.Absolute != "Runtime docs" {
		t.Fatalf("request ID/metadata lost: id=%q metadata=%#v", doc.RequestID, doc.Metadata)
	}
	if !doc.Bootstrap || doc.Runtime != wantRuntime || doc.RuntimeActive != wantRuntime.Runtime || !doc.Navigation || doc.Runtime.BootstrapMode == "none" {
		t.Fatalf("runtime/navigation state lost: bootstrap=%v runtimeActive=%v runtime=%v navigation=%v runtime=%#v want=%#v", doc.Bootstrap, doc.RuntimeActive, doc.Runtime.Runtime, doc.Navigation, doc.Runtime, wantRuntime)
	}
	if doc.Nonce != "route-nonce" || len(doc.BodyAttrs) == 0 || doc.Body.IsZero() {
		t.Fatalf("body attrs/nonce/body lost: attrs=%#v nonce=%q body=%#v", doc.BodyAttrs, doc.Nonce, doc.Body)
	}

	rendered := gosx.RenderHTML(server.HTMLDocument(doc))
	if got := strings.Count(rendered, "data-gosx-document-contract"); got != 1 {
		t.Fatalf("expected one framework document contract, got %d in %q", got, rendered)
	}
	for _, want := range []string{
		`lang="en-US"`,
		`data-gosx-document-id="gosx-doc-get-docs"`,
		`data-gosx-document-path="/docs?tab=runtime"`,
		`data-page="runtime"`,
		`content="runtime"`,
		`data-route-navigation="true"`,
		`nonce="route-nonce"`,
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered document lost %q: %s", want, rendered)
		}
	}
}

func TestRouteContextDocumentSuppressesRequestIDForSharedCache(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/shared", nil)
	req.Header.Set("X-Request-ID", "request-123")
	ctx := (&Router{}).newRouteContext(req)
	ctx.pattern = "GET /shared"
	ctx.CachePublic(time.Minute)

	doc := ctx.Document("Shared", gosx.Text("shared"))
	if doc.RequestID != "" {
		t.Fatalf("shared-cache document carried request ID %q", doc.RequestID)
	}
}

func TestRouteContextDocumentWiresPatternThroughBuiltRouter(t *testing.T) {
	var got *server.DocumentContext
	router := NewRouter()
	router.SetLayout(func(ctx *RouteContext, body gosx.Node) gosx.Node {
		got = ctx.Document("Docs", body)
		return server.HTMLDocument(got)
	})
	router.Add(Route{
		Pattern: "GET /docs",
		Handler: func(ctx *RouteContext) gosx.Node {
			return gosx.El("main", gosx.Text("docs"))
		},
	})
	built, err := router.BuildChecked()
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/docs", nil)
	w := httptest.NewRecorder()
	built.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if got == nil {
		t.Fatal("layout did not compose a document")
	}
	if got.Pattern != "GET /docs" || got.PageID != "gosx-doc-get-docs" || got.Path != "/docs" {
		t.Fatalf("built route state lost: pattern=%q pageID=%q path=%q", got.Pattern, got.PageID, got.Path)
	}
	if !strings.Contains(w.Body.String(), `data-gosx-document-id="gosx-doc-get-docs"`) {
		t.Fatalf("rendered document lost the route page ID: %s", w.Body.String())
	}
}
