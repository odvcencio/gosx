package server

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"m31labs.dev/gosx"
)

func TestDevelopmentRenderErrorPage(t *testing.T) {
	for _, tc := range []struct {
		name, dev, mode string
		details         bool
	}{
		{"dev", "1", "development", true},
		{"ordinary server", "", "", false},
		{"production", "", "production", false},
		{"production overrides dev", "1", "production", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GOSX_DEV", tc.dev)
			t.Setenv("GOSX_ENV", tc.mode)
			app := New()
			app.SetPublicDir("")
			app.Page("/", func(*Context) gosx.Node {
				panic(errors.New("app/page.gsx:7:18: prop Label: want string\n    expression: props.Label + `<script>alert(1)</script>`"))
			})
			w := httptest.NewRecorder()
			app.Build().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
			if w.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d", w.Code)
			}
			body := w.Body.String()
			for _, detail := range []string{"app/page.gsx:7:18", "want string", "props.Label"} {
				if strings.Contains(body, detail) != tc.details {
					t.Fatalf("details=%v, body=%q", tc.details, body)
				}
			}
			if strings.Contains(body, "<script>alert(1)</script>") {
				t.Fatalf("error source was rendered as HTML: %q", body)
			}
			if tc.details && (w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(body, "&lt;script&gt;")) {
				t.Fatalf("dev response headers=%v body=%q", w.Header(), body)
			}
		})
	}
}

func TestDevelopmentErrorBypassesBrokenAppChrome(t *testing.T) {
	t.Setenv("GOSX_DEV", "1")
	t.Setenv("GOSX_ENV", "development")
	app := New()
	app.SetPublicDir("")
	app.SetLayout(func(string, gosx.Node) gosx.Node { panic("broken layout") })
	app.SetErrorPage(func(*Context, error) gosx.Node { panic("broken error component") })
	app.Page("/", func(*Context) gosx.Node { panic(errors.New("app/page.gsx:4:9: render failed")) })
	w := httptest.NewRecorder()
	app.Build().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusInternalServerError || !strings.Contains(w.Body.String(), "app/page.gsx:4:9") {
		t.Fatalf("status=%d body=%q", w.Code, w.Body.String())
	}
}
