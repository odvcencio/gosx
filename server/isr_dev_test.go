package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx"
)

func TestISRDevelopmentRendersCurrentSource(t *testing.T) {
	for _, development := range []bool{true, false} {
		name, flag := "production", ""
		if development {
			name, flag = "development", "1"
		}
		t.Run(name, func(t *testing.T) {
			t.Setenv("GOSX_DEV", flag)
			root := t.TempDir()
			dist := filepath.Join(root, "dist")
			if err := os.MkdirAll(filepath.Join(dist, "static"), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dist, "static", "index.html"), []byte("old production HTML"), 0644); err != nil {
				t.Fatal(err)
			}
			writeISRManifest(t, dist, isrManifest{Routes: []isrRoute{{Path: "/", File: "index.html"}}})
			app := New()
			app.SetRuntimeRoot(root)
			app.EnableISR()
			current, calls := "current source", 0
			app.Page("GET /", func(ctx *Context) gosx.Node {
				calls++
				return gosx.Text(current)
			})
			handler := app.Build()
			for _, value := range []string{"current source", "updated source"} {
				current = value
				req := httptest.NewRequest(http.MethodGet, "/", nil)
				req.Header.Set("Accept", "text/html")
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, req)
				wantBody, wantISR := "old production HTML", "HIT"
				if development {
					wantBody, wantISR = current, ""
				}
				if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), wantBody) || w.Header().Get("X-GoSX-ISR") != wantISR {
					t.Fatalf("%s: status=%d ISR=%q body=%q; want 200, ISR=%q, body containing %q", name, w.Code, w.Header().Get("X-GoSX-ISR"), w.Body.String(), wantISR, wantBody)
				}
			}
			wantCalls := 0
			if development {
				wantCalls = 2
			}
			if calls != wantCalls {
				t.Fatalf("origin called %d times, want %d", calls, wantCalls)
			}
		})
	}
}
