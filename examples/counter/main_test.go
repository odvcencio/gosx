package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCounterRendersCompiledIsland(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	app, err := newApp(filepath.Dir(thisFile))
	if err != nil {
		t.Fatal(err)
	}
	handler := app.Build()

	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: status %d\n%s", path, rec.Code, rec.Body)
		}
		return rec
	}

	page := get("/?count=5").Body.String()
	if !strings.Contains(page, `data-gosx-island="Counter"`) {
		t.Errorf("page does not mount the Counter island:\n%s", page)
	}
	if !strings.Contains(page, `<noscript>`) || !strings.Contains(page, `href="/?count=6"`) {
		t.Errorf("page lacks the no-JavaScript links for count 5:\n%s", page)
	}

	// The compiled program is what the island hydrates from. (A mount under
	// /gosx/ is shadowed by the runtime asset handler, so check the compiler
	// output directly rather than over HTTP.)
	_, data, err := compileIsland("counter.gsx", "Counter")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"Counter"`) {
		t.Errorf("compiled program does not name the Counter island")
	}
}
