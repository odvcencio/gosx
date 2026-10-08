package server

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/internal/telemetryerr"
)

type catalogObserverFunc func([]ObservationPattern)

func (f catalogObserverFunc) ObserveCatalog(rows []ObservationPattern) { f(rows) }

func TestObservationCatalogConfigurationErrorsUseServerNamespace(t *testing.T) {
	var absent *App
	for _, app := range []*App{absent, New()} {
		err := app.UseObservationCatalogObserver(nil)
		var config *telemetryerr.ConfigError
		if err == nil || !strings.HasPrefix(err.Error(), "server:") || !errors.As(err, &config) || config.Field != "catalog_observer" {
			t.Fatalf("required observer: %v", err)
		}
	}
	a := New()
	a.Build()
	err := a.UseObservationCatalogObserver(catalogObserverFunc(func([]ObservationPattern) {}))
	if err == nil || !strings.HasPrefix(err.Error(), "server:") || !errors.Is(err, telemetryerr.ErrAfterBuild) {
		t.Fatalf("configuration closed: %v", err)
	}
}

type catalogHandler struct {
	rows     []ObservationPattern
	calls    int
	overflow bool
}

func (h *catalogHandler) ServeHTTP(http.ResponseWriter, *http.Request) {}
func (h *catalogHandler) ObservationPatterns(int) ([]ObservationPattern, bool) {
	h.calls++
	return h.rows, h.overflow
}

func TestObservationCatalogCopiesMergesAndNotifiesOnce(t *testing.T) {
	a := New()
	a.Page("GET /local/{id}", func(*Context) gosx.Node { return gosx.Text("ok") })
	a.API("POST /api", func(*Context) (any, error) { return nil, nil })
	a.Redirect("GET /old", "/local", http.StatusFound)
	a.Rewrite("GET /alias", "/local")
	get := &catalogHandler{rows: []ObservationPattern{{Kind: "page", Pattern: "/relative/{id}", Methods: []string{"GET", "HEAD"}}}}
	post := &catalogHandler{rows: []ObservationPattern{{Kind: "page", Pattern: "/relative/{id}", Methods: []string{"POST"}}}}
	a.Mount("/one/", get)
	a.Mount("/two/", post)
	a.Mount("/plain/", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	var got []ObservationPattern
	count := 0
	if err := a.UseObservationCatalogObserver(catalogObserverFunc(func(rows []ObservationPattern) {
		rows[0].Kind = "changed"
		rows[len(rows)-1].Methods[0] = "changed"
	})); err != nil {
		t.Fatal(err)
	}
	if err := a.UseObservationCatalogObserver(catalogObserverFunc(func(rows []ObservationPattern) {
		count++
		got = rows
	})); err != nil {
		t.Fatal(err)
	}
	a.Build()
	a.Build()
	if count != 1 || get.calls != 1 || post.calls != 1 {
		t.Fatalf("callbacks=%d provider calls=%d,%d", count, get.calls, post.calls)
	}
	want := []ObservationPattern{
		{Kind: "api", Pattern: "/api", Methods: []string{"POST"}},
		{Kind: "error", Pattern: "/local/{id}", Methods: []string{"GET", "HEAD"}},
		{Kind: "error", Pattern: "/relative/{id}", Methods: []string{"GET", "HEAD", "POST"}},
		{Kind: "mount", Pattern: "/one/", Methods: []string{"*"}},
		{Kind: "mount", Pattern: "/plain/", Methods: []string{"*"}},
		{Kind: "mount", Pattern: "/two/", Methods: []string{"*"}},
		{Kind: "page", Pattern: "/local/{id}", Methods: []string{"GET", "HEAD"}},
		{Kind: "page", Pattern: "/relative/{id}", Methods: []string{"GET", "HEAD", "POST"}},
		{Kind: "redirect", Pattern: "/old", Methods: []string{"GET", "HEAD"}},
		{Kind: "rewrite", Pattern: "/alias", Methods: []string{"GET", "HEAD"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v; want %#v", got, want)
	}
	if get.rows[0].Methods[0] != "GET" || post.rows[0].Methods[0] != "POST" {
		t.Fatal("provider slices changed")
	}
	if err := a.UseObservationCatalogObserver(catalogObserverFunc(func([]ObservationPattern) {})); !errors.Is(err, telemetryerr.ErrAfterBuild) {
		t.Fatal(err)
	}
}

func TestObservationCatalogBoundsAndIsolation(t *testing.T) {
	a := New()
	h := &catalogHandler{rows: []ObservationPattern{{Kind: "page", Pattern: "/declared", Methods: []string{"GET"}}}, overflow: true}
	a.Mount("/plain/", h)
	rows, overflow := a.ObservationPatterns(1)
	if len(rows) != 2 || !overflow || rows[0].Kind != "error" || rows[1].Kind != "page" {
		t.Fatalf("selection: %#v overflow=%v", rows, overflow)
	}
	rows, _ = a.ObservationPatterns(0)
	rows[1].Methods[0] = "changed"
	if h.rows[0].Methods[0] != "GET" {
		t.Fatal("result aliases provider")
	}
	count := 0
	_ = a.UseObservationCatalogObserver(catalogObserverFunc(func([]ObservationPattern) { panic("private panic") }))
	_ = a.UseObservationCatalogObserver(catalogObserverFunc(func([]ObservationPattern) { count++ }))
	a.Build().ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/plain/", nil))
	if count != 1 {
		t.Fatal("panic prevented later observer")
	}
	if err := (*App)(nil).UseObservationCatalogObserver(nil); err == nil {
		t.Fatal("nil accepted")
	}
	if err := New().UseObservationCatalogObserver(nil); err == nil {
		t.Fatal("nil observer accepted")
	}
}

func TestObservationCatalogPageCapacityExcludesErrors(t *testing.T) {
	for _, count := range []int{400, 512, 600} {
		a := New()
		for i := count - 1; i >= 0; i-- {
			a.Page(fmt.Sprintf("GET /p/%03d", i), func(*Context) gosx.Node { return gosx.Text("page") })
		}
		rows, overflow := a.ObservationPatterns(0)
		admitted := min(count, 512)
		if len(rows) != 2*admitted || overflow != (count > 512) {
			t.Fatalf("pages=%d rows=%d overflow=%v", count, len(rows), overflow)
		}
		for i := 0; i < admitted; i++ {
			errorRow, page := rows[i], rows[i+admitted]
			if errorRow.Kind != "error" || page.Kind != "page" || errorRow.Pattern != page.Pattern || !reflect.DeepEqual(errorRow.Methods, []string{"GET", "HEAD"}) || !reflect.DeepEqual(page.Methods, errorRow.Methods) {
				t.Fatalf("page/error pair: %#v %#v", errorRow, page)
			}
		}
	}
}
