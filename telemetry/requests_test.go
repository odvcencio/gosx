//go:build !js || !wasm

package telemetry

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"m31labs.dev/gosx/server"
)

func requestInventory(tb testing.TB, maxSeries int) *Telemetry {
	tb.Helper()
	opts := Defaults()
	opts.Listen.Addr = "off"
	opts.Metrics.DisableOperations = true
	opts.Metrics.DisableClientEvents = true
	opts.Metrics.DisableRuntime = true
	opts.Metrics.DisableReadiness = true
	opts.Metrics.DisableScheduled = true
	opts.Activities.Disabled = true
	if maxSeries != 0 {
		opts.Metrics.MaxSeries = maxSeries
	}
	tel := &Telemetry{opts: opts}
	if err := tel.initializeRegistry(); err != nil {
		tb.Fatal(err)
	}
	tel.active.Store(true)
	return tel
}

func TestRequestCatalogIsPrivateFiniteAndImmutable(t *testing.T) {
	tel := requestInventory(t, 0)
	rows := []server.ObservationPattern{{Kind: "page", Pattern: "/match/{code}", Methods: []string{"GET"}}}
	tel.observeCatalog(rows)
	usage := tel.registry.Usage()
	rows[0].Pattern = "/private-modified-canary"
	rows[0].Methods[0] = "private-method-canary"
	tel.observeCatalog([]server.ObservationPattern{{Kind: "page", Pattern: "/late"}})
	tel.observeRequest("page", "/match/{code}", "GET", 200, 17, time.Millisecond, false)
	tel.observeRequest("page", "/match/{code}", "HEAD", 204, 0, time.Millisecond, false)
	tel.observeRequest("error", "/match/{code}", "GET", 503, 3, time.Millisecond, false)
	for i := 0; i < 1000; i++ {
		tel.observeRequest("page", fmt.Sprintf("/private-concrete-canary/%d", i), "private-method-canary", 999, 1, -time.Second, false)
	}
	tel.observeRequest("public", "/private-path-canary", "GET", 200, 1, 0, false)
	tel.observeRequest("", "", "GET", 0, 0, 0, true)
	if after := tel.registry.Usage(); after != usage {
		t.Fatal("request traffic changed admission", usage, after)
	}
	if got := hubSample(t, tel, "gosx_http_requests_total", "kind", "page", "route", "/match/{code}", "method", "GET", "status_class", "2xx").Counter; got != 1 {
		t.Fatal(got)
	}
	if got := hubSample(t, tel, "gosx_http_requests_total", "kind", "hub", "route", "(hub)", "method", "GET", "status_class", "hijacked").Counter; got != 1 {
		t.Fatal(got)
	}
	if got := hubSample(t, tel, "gosx_http_request_duration_seconds", "kind", "page", "route", "/match/{code}").Histogram.Count; got != 3 {
		t.Fatal("derived error did not share the page duration", got)
	}
	if got := hubSample(t, tel, "gosx_http_response_bytes_total", "kind", "page", "route", "/match/{code}").Counter; got != 17 {
		t.Fatal(got)
	}
	var text bytes.Buffer
	if err := tel.registry.WritePrometheus(&text); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text.String(), "private-") {
		t.Fatal("unchecked request data escaped into labels")
	}
}

func TestRouteAdmissionCollapsesWholePageAndError(t *testing.T) {
	tel := requestInventory(t, 800)
	before := tel.registry.Usage()
	tel.observeCatalog(inventoryRoutes("*"))
	table := tel.requests.table.Load()
	if len(table.rows) != 8 {
		t.Fatal("expected one complete page/error pair after the six sentinels", len(table.rows))
	}
	for key := range table.rows {
		if key.kind == "page" && table.rows[requestKey{"error", key.route}] == nil {
			t.Fatal("partial page admission", key)
		}
	}
	if usage := tel.registry.Usage(); usage.Samples != before.Samples+122 {
		t.Fatal("derived error reserved unexpected samples", before, usage)
	}
	tel.observeRequest("page", "/route/511", "GET", 200, 0, 0, false)
	if got := hubSample(t, tel, "gosx_http_requests_total", "kind", "other", "route", "(other)", "method", "GET", "status_class", "2xx").Counter; got != 1 {
		t.Fatal(got)
	}
}

func TestRequestAggregateAddsNoAllocations(t *testing.T) {
	tel := requestInventory(t, 0)
	tel.observeCatalog([]server.ObservationPattern{{Kind: "page", Pattern: "/match/{code}", Methods: []string{"GET"}}})
	if got := testing.AllocsPerRun(10000, func() {
		tel.observeRequest("page", "/match/{code}", "GET", 200, 64, time.Millisecond, false)
	}); got != 0 {
		t.Fatal("warm aggregation allocated", got)
	}
	tel.active.Store(false)
	before := hubSample(t, tel, "gosx_http_requests_total", "kind", "page", "route", "/match/{code}", "method", "GET", "status_class", "2xx").Counter
	tel.observeRequest("page", "/match/{code}", "GET", 200, 64, time.Millisecond, false)
	if after := hubSample(t, tel, "gosx_http_requests_total", "kind", "page", "route", "/match/{code}", "method", "GET", "status_class", "2xx").Counter; after != before {
		t.Fatal("closed request observer changed metrics")
	}
}
