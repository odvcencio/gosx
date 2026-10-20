package route

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/server"
)

func renderCopiesHandler(tb testing.TB) http.Handler {
	tb.Helper()
	tb.Setenv("GOSX_ENV", "production")
	root := tb.TempDir()
	for name, source := range map[string]string{
		"layout.gsx":    "package bench\nfunc Layout() Node {\n return <section><Slot /></section>\n}\n",
		"docs/page.gsx": "package bench\nfunc Page() Node {\n return <main>{data.body}</main>\n}\n",
		"global.css":    "main { color: black; }",
		"layout.css":    "section { display: block; }",
		"docs/page.css": "main { padding: 1rem; }",
	} {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			tb.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(source), 0600); err != nil {
			tb.Fatal(err)
		}
	}
	body := strings.Repeat("A paragraph for the full routed page. ", 2048)
	router := NewRouter()
	router.SetLayout(func(ctx *RouteContext, body gosx.Node) gosx.Node {
		return server.HTMLDocument(ctx.Document("Bench", body))
	})
	if err := router.AddDir(root, FileRoutesOptions{Render: func(ctx *RouteContext, page FilePage) (gosx.Node, error) {
		ctx.Data = map[string]any{"body": body}
		return DefaultFileRenderer(ctx, page)
	}}); err != nil {
		tb.Fatal(err)
	}
	h := router.Build()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/docs", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), body) {
		tb.Fatal("invalid routed page")
	}
	return h
}

func BenchmarkFileRoutedRenderCopies(b *testing.B) {
	h := renderCopiesHandler(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/docs", nil))
		if w.Code != 200 {
			b.Fatal(w.Code)
		}
	}
}

func TestFileRoutedRequestAllocationCeiling(t *testing.T) {
	h := renderCopiesHandler(t)
	allocs := testing.AllocsPerRun(10, func() {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/docs", nil))
		if w.Code != 200 {
			t.Fatal(w.Code)
		}
	})
	if allocs > 210 {
		t.Fatalf("file-routed request allocated %.0f objects; ceiling 210", allocs)
	}
}
