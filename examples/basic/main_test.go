package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

func TestPagesRenderLoaderData(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	handler, err := newHandler(filepath.Dir(thisFile))
	if err != nil {
		t.Fatal(err)
	}

	get := func(path string) string {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: status %d\n%s", path, rec.Code, rec.Body)
		}
		return rec.Body.String()
	}

	home := get("/")
	if !regexp.MustCompile(`Server rendered at\s*<span class="stamp">\d{4}-\d{2}-\d{2}T`).MatchString(home) {
		t.Errorf("home page does not show the loader's server time:\n%s", home)
	}

	counter := get("/counter?count=3")
	for _, want := range []string{`<span class="count">3</span>`, `href="/counter?count=4"`, `href="/counter?count=2"`} {
		if !strings.Contains(counter, want) {
			t.Errorf("counter page lacks %q:\n%s", want, counter)
		}
	}
}
