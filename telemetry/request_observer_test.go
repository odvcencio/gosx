//go:build !js || !wasm

package telemetry

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

func enableRequests(t *testing.T, app *server.App) *Telemetry {
	t.Helper()
	o := aggregateCoreOptions(t)
	o.Metrics.DisableRequests = false
	tel, err := Enable(app, o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := tel.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return tel
}

func TestRequestObserverAppAttachment(t *testing.T) {
	app := server.New()
	app.DisableCompression()
	app.Mount("GET /api/{id}", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(201)
		_, _ = w.Write([]byte("payload"))
	}))
	app.Page("GET /page/{id}", func(*server.Context) gosx.Node { return gosx.Text("page") })
	tel := enableRequests(t, app)
	h := app.Build()
	usage := tel.registry.Usage()
	for _, path := range []string{"/api/123", "/api/456"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path+"?private-query-canary", nil))
		if w.Code != 201 {
			t.Fatal(w.Code)
		}
	}
	if got := hubSample(t, tel, "gosx_http_requests_total", "kind", "mount", "route", "/api/{id}", "method", "GET", "status_class", "2xx").Counter; got != 2 {
		t.Fatal(got)
	}
	if got := hubSample(t, tel, "gosx_http_response_bytes_total", "kind", "mount", "route", "/api/{id}").Counter; got != 14 {
		t.Fatal(got)
	}
	if got := hubSample(t, tel, "gosx_http_request_duration_seconds", "kind", "mount", "route", "/api/{id}").Histogram.Count; got != 2 {
		t.Fatal(got)
	}
	if got := hubSample(t, tel, "gosx_http_requests_in_flight").Gauge; got != 0 {
		t.Fatal(got)
	}
	if after := tel.registry.Usage(); after != usage {
		t.Fatal("traffic changed reservations")
	}
	var text bytes.Buffer
	if err := tel.registry.WritePrometheus(&text); err != nil {
		t.Fatal(err)
	}
	for _, canary := range []string{"private-query-canary", "/api/123", "/api/456"} {
		if strings.Contains(text.String(), canary) {
			t.Fatalf("request data escaped: %s", canary)
		}
	}
}

func TestRequestObserverCoversMiddlewareAndConcurrentRequests(t *testing.T) {
	const clients = 32
	app := server.New()
	entered := make(chan struct{}, clients)
	release := make(chan struct{})
	app.Use(func(_ http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			entered <- struct{}{}
			<-release
			w.WriteHeader(403)
		})
	})
	tel := enableRequests(t, app)
	h := app.Build()
	var done sync.WaitGroup
	for range clients {
		done.Add(1)
		go func() {
			defer done.Done()
			h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/blocked", nil))
		}()
	}
	for range clients {
		<-entered
	}
	got := hubSample(t, tel, "gosx_http_requests_in_flight").Gauge
	close(release)
	done.Wait()
	if got != clients {
		t.Fatalf("in-flight=%g want=%d", got, clients)
	}
	if got := hubSample(t, tel, "gosx_http_requests_in_flight").Gauge; got != 0 {
		t.Fatal(got)
	}
	if got := hubSample(t, tel, "gosx_http_requests_total", "kind", "other", "route", "(other)", "method", "GET", "status_class", "4xx").Counter; got != clients {
		t.Fatal(got)
	}
}

func TestRequestObserverMountedRouterCountsOnce(t *testing.T) {
	router := route.NewRouter()
	router.SetLayout(func(_ *route.RouteContext, node gosx.Node) gosx.Node { return node })
	router.Add(route.Route{Pattern: "GET /round/{id}", Handler: func(*route.RouteContext) gosx.Node { return gosx.Text("round") }})
	innerCalls, outerCalls := 0, 0
	router.UseObserver(server.RequestObserverFunc(func(server.RequestEvent) { innerCalls++ }))
	app := server.New()
	app.UseObserver(server.RequestObserverFunc(func(server.RequestEvent) { outerCalls++ }))
	app.Mount("/", router.Build())
	tel := enableRequests(t, app)
	h := app.Build()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/round/123", nil))
	if innerCalls != 1 || outerCalls != 1 {
		t.Fatal(innerCalls, outerCalls)
	}
	if got := hubSample(t, tel, "gosx_http_requests_total", "kind", "page", "route", "/round/{id}", "method", "GET", "status_class", "2xx").Counter; got != 1 {
		t.Fatal("built-in telemetry double counted", got)
	}
}

func TestRequestObserverUnknownPathsAndMethodsStayFinite(t *testing.T) {
	app := server.New()
	app.Mount("/unsafe/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		server.MarkObservedRequest(r, "page", r.URL.Path)
		_, _ = w.Write([]byte("ok"))
	}))
	tel := enableRequests(t, app)
	h := app.Build()
	usage := tel.registry.Usage()
	for i := range 1000 {
		r := httptest.NewRequest("CUSTOM", fmt.Sprintf("/unsafe/private-path-canary/%d", i), nil)
		r.Header.Set("User-Agent", "private-agent-canary")
		r.Header.Set("Cookie", "private-cookie-canary")
		h.ServeHTTP(httptest.NewRecorder(), r)
	}
	if after := tel.registry.Usage(); after != usage {
		t.Fatal("unknown traffic learned labels")
	}
	if got := hubSample(t, tel, "gosx_http_requests_total", "kind", "other", "route", "(other)", "method", "other", "status_class", "2xx").Counter; got != 1000 {
		t.Fatal(got)
	}
	var text bytes.Buffer
	if err := tel.registry.WritePrometheus(&text); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text.String(), "private-") {
		t.Fatal("unchecked request data was retained")
	}
}

func TestRequestObserverClosedAndDisabled(t *testing.T) {
	app := server.New()
	app.Mount("GET /ok", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))
	tel := enableRequests(t, app)
	h := app.Build()
	if err := tel.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/ok", nil))
	if got := hubSample(t, tel, "gosx_http_requests_total", "kind", "mount", "route", "/ok", "method", "GET", "status_class", "2xx").Counter; got != 0 {
		t.Fatal("post-close request changed counters")
	}
	for _, disabled := range []*Telemetry{nil, {}} {
		o := requestObserver{owner: disabled}
		o.ObserveRequestStart()
		o.Observe(server.RequestEvent{Status: 200})
	}
}

func TestRequestObserverDerivedErrorsAndHead(t *testing.T) {
	app := server.New()
	app.Page("GET /panic/{id}", func(*server.Context) gosx.Node { panic("synthetic_failure") })
	app.Page("GET /page/{id}", func(*server.Context) gosx.Node { return gosx.Text("page") })
	tel := enableRequests(t, app)
	h := app.Build()
	for _, tc := range []struct {
		method, path string
		status       int
	}{
		{"GET", "/panic/123", 500},
		{"HEAD", "/page/456", 200},
		{"GET", "/unknown", 404},
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
		if w.Code != tc.status {
			t.Fatalf("%s %s: %d", tc.method, tc.path, w.Code)
		}
	}
	for _, labels := range [][]string{
		{"kind", "error", "route", "/panic/{id}", "method", "GET", "status_class", "5xx"},
		{"kind", "page", "route", "/page/{id}", "method", "HEAD", "status_class", "2xx"},
		{"kind", "not_found", "route", "(unmatched)", "method", "GET", "status_class", "4xx"},
	} {
		if got := hubSample(t, tel, "gosx_http_requests_total", labels...).Counter; got != 1 {
			t.Fatal(labels, got)
		}
	}
	if got := hubSample(t, tel, "gosx_http_request_duration_seconds", "kind", "page", "route", "/panic/{id}").Histogram.Count; got != 1 {
		t.Fatal("error did not use the page duration", got)
	}
}
