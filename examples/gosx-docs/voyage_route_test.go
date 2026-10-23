package main

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"m31labs.dev/gosx/server"
)

func TestVoyageRouteServesTheSpectralSeaWithOnlyGoSXRuntimeScripts(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	configureDocsTestSecret(t)
	app, err := buildDocsApp(server.ResolveAppRoot(thisFile), "8080")
	if err != nil {
		t.Fatal(err)
	}
	handler := app.Build()
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/demos/voyage?view=waterline&period=storm-360x", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("voyage returned %d", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{`data-view="waterline"`, `data-period="storm-360x"`, `"spectrum"`, `"presentation"`, `"waterDriftZ"`, "Calm", "Storm", "1,000×", "data-gosx-scene3d-ocean-fft-url"} {
		if !strings.Contains(body, want) {
			t.Errorf("served voyage page is missing %q", want)
		}
	}
	// The page ships GoSX runtime chunks only: every script source is under /gosx/.
	for _, m := range regexp.MustCompile(`<script[^>]*\ssrc="([^"]+)"`).FindAllStringSubmatch(body, -1) {
		if !strings.Contains(m[1], "/gosx/") {
			t.Errorf("voyage loads a script that is not a GoSX runtime chunk: %s", m[1])
		}
	}
}
