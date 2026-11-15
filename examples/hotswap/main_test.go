package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/server"
)

// TestCounterIslandCompiles keeps counter.gsx, the file the README tells you to
// edit, compiling to an island program, and checks the page mounts it.
func TestCounterIslandCompiles(t *testing.T) {
	prog, data, err := compileIsland("counter.gsx", "Counter")
	if err != nil {
		t.Fatal(err)
	}
	if prog.Name != "Counter" || len(data) == 0 {
		t.Fatalf("unexpected island: name=%q bytes=%d", prog.Name, len(data))
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	app := server.New()
	app.Route("/", func(_ *http.Request) gosx.Node { return counterPage(prog) })
	app.Build().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `data-gosx-island="Counter"`) {
		t.Fatalf("page does not mount the island: status %d\n%s", rec.Code, rec.Body)
	}
}
