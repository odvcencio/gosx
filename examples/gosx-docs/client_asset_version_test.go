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
	app.Build().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/demos/scene3d-bench", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("benchmark response: %d", rec.Code)
	}
	for _, asset := range []string{"docs-navigation.js", "scene3d-bench-client.js"} {
		if !strings.Contains(rec.Body.String(), `src="/`+asset+`?v=`+gosx.Version+`"`) {
			t.Errorf("missing release-versioned %s", asset)
		}
	}
}
