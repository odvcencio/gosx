package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"m31labs.dev/gosx"
)

func TestISRRegenerationPreservesBasePath(t *testing.T) {
	const prefix = "/.proxy/game"
	for _, strips := range []bool{false, true} {
		t.Run(fmt.Sprint(strips), func(t *testing.T) {
			root := t.TempDir()
			target := filepath.Join(root, "static", ".proxy", "game", "news", "index.html")
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				t.Fatal(err)
			}
			writeISRManifest(t, root, isrManifest{BasePath: prefix, Routes: []isrRoute{{Path: prefix + "/news", File: ".proxy/game/news/index.html", RevalidateSeconds: 60}}})
			app := New()
			if err := app.SetBasePath(prefix, BasePathOptions{ProxyStripsPrefix: strips}); err != nil {
				t.Fatal(err)
			}
			app.SetRuntimeRoot(root)
			app.EnableISR()
			var calls atomic.Int32
			app.Page("GET /news", func(ctx *Context) gosx.Node {
				n := calls.Add(1)
				ctx.AddHead(gosx.El("link", gosx.Attrs(gosx.Attr("rel", "stylesheet"), gosx.Attr("href", "/style.css"))))
				return gosx.El("a", gosx.Attrs(gosx.Attr("href", "/next")), gosx.Text(fmt.Sprintf("revision %d", n)))
			})
			handler := app.Build()
			get := func() *httptest.ResponseRecorder {
				requestPath := prefix + "/news/"
				if strips {
					requestPath = "/news/"
				}
				req := httptest.NewRequest(http.MethodGet, requestPath, nil)
				req.Header.Set("Accept", "text/html")
				res := httptest.NewRecorder()
				handler.ServeHTTP(res, req)
				return res
			}
			assertURLs := func(body string) {
				t.Helper()
				for _, want := range []string{`content="/.proxy/game"`, `href="/.proxy/game/next"`, `href="/.proxy/game/style.css"`} {
					if !strings.Contains(body, want) {
						t.Fatalf("regenerated page missing %s: %s", want, body)
					}
				}
			}
			first := get()
			if first.Code != 200 || first.Header().Get("X-GoSX-ISR") != "MISS" {
				t.Fatal(first.Code, first.Header(), first.Body.String())
			}
			assertURLs(first.Body.String())
			assertURLs(readFileMaybe(target))
			if hit := get(); hit.Header().Get("X-GoSX-ISR") != "HIT" || calls.Load() != 1 {
				t.Fatal("regenerated artifact not reused", hit.Header(), calls.Load())
			}
			if app.RevalidatePath("/news") == 0 {
				t.Fatal("revalidation did not find internal route")
			}
			stale := get()
			if stale.Header().Get("X-GoSX-ISR") != "STALE" {
				t.Fatal("expected stale regeneration", stale.Header())
			}
			deadline := time.Now().Add(3 * time.Second)
			for !strings.Contains(readFileMaybe(target), "revision 2") && time.Now().Before(deadline) {
				time.Sleep(10 * time.Millisecond)
			}
			body := readFileMaybe(target)
			if !strings.Contains(body, "revision 2") {
				t.Fatal("stale artifact not regenerated", body)
			}
			assertURLs(body)
		})
	}
}

func TestISRManifestPublicPathsKeepPublicFiles(t *testing.T) {
	for _, manifest := range []isrManifest{
		{BasePath: "/.proxy/game", Routes: []isrRoute{{Path: "/.proxy/game/", File: ".proxy/game/index.html"}, {Path: "/.proxy/game/news", File: ".proxy/game/news/index.html"}, {Path: "/.proxy/games", File: ".proxy/games/index.html"}}},
		{BasePath: "/.proxy/game", Pages: []string{"/.proxy/game/", "/.proxy/game/news", "/.proxy/games"}},
	} {
		routes := routesForISRManifest(manifest)
		for i, want := range []isrRoute{{Path: "/", File: ".proxy/game/index.html"}, {Path: "/news", File: ".proxy/game/news/index.html"}, {Path: "/.proxy/games", File: ".proxy/games/index.html"}} {
			if routes[i].Path != want.Path || routes[i].File != want.File {
				t.Fatalf("route %d: %+v, want %+v", i, routes[i], want)
			}
		}
	}
}
