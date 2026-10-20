package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/server"
)

// A page that explicitly requires a legacy chunk, with no entry of that kind,
// must still advertise the chunk so static export copies it.
func TestExportDiscoversExplicitLegacyFeatureChunks(t *testing.T) {
	app := server.New()
	app.Page("GET /explicit", func(ctx *server.Context) gosx.Node {
		for _, name := range []string{"hubs", "controllers", "textlayout"} {
			if err := ctx.Runtime().RequireFeature(name); err != nil {
				t.Error(err)
			}
		}
		return ctx.Engine(engine.Config{Name: "Plain", Kind: engine.KindSurface}, gosx.Text("x"))
	})
	w := httptest.NewRecorder()
	app.Build().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/explicit", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	refs := map[string]struct{}{}
	addExportRuntimeAssetRefs(refs, w.Body.String())
	got := strings.Join(sortedExportRuntimeAssetRefs(refs), "\n")
	for _, want := range []string{
		"/gosx/bootstrap-feature-hubs.js",
		"/gosx/bootstrap-feature-controllers.js",
		"/gosx/bootstrap-feature-textlayout.js",
		"/gosx/bootstrap-feature-engines.js",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("export refs lack %s:\n%s", want, got)
		}
	}
}
