package main

import (
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/server"
)

func TestDocsClientFixesUseReleaseVersionedURLs(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	configureDocsTestSecret(t)
	app, err := buildDocsApp(server.ResolveAppRoot(thisFile), "8080")
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	handler := app.Build()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/demos/scene3d-bench", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("benchmark response: %d", rec.Code)
	}
	for _, asset := range []string{"scene3d-bench-client.js"} {
		assetURL := "/" + asset + "?release=" + gosx.Version
		if !strings.Contains(rec.Body.String(), `src="`+assetURL+`"`) {
			t.Errorf("missing release-versioned %s", asset)
		}
		assetResponse := httptest.NewRecorder()
		handler.ServeHTTP(assetResponse, httptest.NewRequest(http.MethodGet, assetURL, nil))
		if assetResponse.Code != http.StatusOK || strings.Contains(assetResponse.Header().Get("Cache-Control"), "immutable") {
			t.Errorf("%s: status %d cache %q; release hints must not claim content immutability", asset, assetResponse.Code, assetResponse.Header().Get("Cache-Control"))
		}
	}
	if strings.Contains(rec.Body.String(), "docs-navigation.js") {
		t.Fatal("native popover must not reference the retired docs keyboard helper")
	}
	retired := httptest.NewRecorder()
	handler.ServeHTTP(retired, httptest.NewRequest(http.MethodGet, "/docs-navigation.js?release="+gosx.Version, nil))
	if retired.Code != http.StatusNotFound {
		t.Fatalf("retired docs keyboard helper status = %d, want 404", retired.Code)
	}
}
