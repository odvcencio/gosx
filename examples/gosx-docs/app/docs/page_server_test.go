package docs

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
)

func TestDocsIndexAndLayoutModulesRegister(t *testing.T) {
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source")
	}
	dir := filepath.Dir(testFile)
	for _, name := range []string{"page.gsx", "layout.gsx"} {
		module, found := route.DefaultFileModuleRegistry().Lookup(filepath.Join(dir, name))
		if !found {
			t.Fatalf("module for %s is not registered", name)
		}
		if module.Bindings == nil {
			t.Fatalf("module for %s has no bindings", name)
		}
		if name == "page.gsx" && module.Load == nil {
			t.Fatalf("module for %s has no loader", name)
		}
		if name == "layout.gsx" {
			bindings := module.Bindings(&route.RouteContext{Request: httptest.NewRequest(http.MethodGet, "/docs", nil)}, route.FilePage{RoutePath: "/docs"}, nil)
			if bindings.Values["docsIndexCurrent"] != "page" {
				t.Fatalf("layout docsIndexCurrent = %#v", bindings.Values["docsIndexCurrent"])
			}
		}
	}
}

func TestDocsIndexRendersLoadedCatalogThroughNestedLayout(t *testing.T) {
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source")
	}
	router := route.NewRouter()
	if err := router.AddDir(filepath.Dir(filepath.Dir(testFile)), route.FileRoutesOptions{}); err != nil {
		t.Fatalf("add docs app routes: %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/docs?q=strict+typed+props", nil)
	response := httptest.NewRecorder()
	router.Build().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("GET /docs status = %d", response.Code)
	}
	body := response.Body.String()
	for _, want := range []string{"Documentation", "Start &amp; foundations", "Typed component proof", `value="strict typed props"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("rendered /docs missing %q in %s", want, body)
		}
	}
}

func TestNormalizedDocsQueryTrimsAndCapsRunes(t *testing.T) {
	if got := normalizedDocsQuery("  WebGPU fallback  "); got != "WebGPU fallback" {
		t.Fatalf("normalized query = %q", got)
	}
	got := normalizedDocsQuery(strings.Repeat("界", 100))
	if len([]rune(got)) != 80 {
		t.Fatalf("query runes = %d, want 80", len([]rune(got)))
	}
}

func TestDocsResultSummary(t *testing.T) {
	for _, tc := range []struct {
		query string
		count int
		want  string
	}{
		{"", 0, "Search titles"},
		{"forms", 0, `No guides match "forms".`},
		{"forms", 1, `1 guide matches "forms".`},
		{"forms", 2, `2 guides match "forms".`},
	} {
		if got := docsResultSummary(tc.query, tc.count); !strings.Contains(got, tc.want) {
			t.Fatalf("summary(%q, %d) = %q, want %q", tc.query, tc.count, got, tc.want)
		}
	}
}

func TestDocsSearchResultDataExposesEvidence(t *testing.T) {
	results := docsSearchResultData([]docsapp.DocSearchResult{{
		Entry: docsapp.DocEntry{
			Title:       "Forms & actions",
			Href:        "/docs/forms",
			Description: "Server actions.",
			Section:     "server",
			Source:      "app/docs/forms/page.gsx",
			Keywords:    []string{"forms", "actions"},
		},
		Score: 12,
	}})
	if len(results) != 1 || results[0]["section"] != "Server applications" || results[0]["source"] == "" || results[0]["keywords"] != "forms · actions" {
		t.Fatalf("result data = %#v", results)
	}
}
