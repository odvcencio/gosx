//go:build !js || !wasm

package telemetry

import (
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"testing"
	"time"

	"m31labs.dev/gosx/server"
)

type requestBenchWriter struct{ header http.Header }

func (w *requestBenchWriter) Header() http.Header       { return w.header }
func (*requestBenchWriter) WriteHeader(int)             {}
func (*requestBenchWriter) Write(p []byte) (int, error) { return len(p), nil }

func observedRequestPair(tb testing.TB) (http.Handler, http.Handler, *http.Request, http.ResponseWriter) {
	tb.Helper()
	tel := requestInventory(tb, 0)
	tel.observeCatalog([]server.ObservationPattern{{Kind: "page", Pattern: "/round/{id}", Methods: []string{"GET"}}})
	data := make([]byte, 64)
	leaf := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		server.MarkObservedRequest(r, "page", "GET /round/{id}")
		_, _ = w.Write(data)
	})
	base := server.ObserveHandler(leaf, []server.RequestObserver{server.RequestObserverFunc(func(server.RequestEvent) {})})
	head := server.ObserveHandler(leaf, []server.RequestObserver{requestObserver{owner: tel}})
	return base, head, httptest.NewRequest("GET", "/round/123", nil), &requestBenchWriter{header: make(http.Header)}
}

func TestFullRequestObserverAddsNoAllocations(t *testing.T) {
	base, head, r, w := observedRequestPair(t)
	baseline := testing.AllocsPerRun(10000, func() { base.ServeHTTP(w, r) })
	observed := testing.AllocsPerRun(10000, func() { head.ServeHTTP(w, r) })
	t.Logf("baseline=%g observed=%g added=%g", baseline, observed, observed-baseline)
	if observed != baseline {
		t.Fatal("full request aggregation added allocations")
	}
}

func BenchmarkFullRequestObserver(b *testing.B) {
	for _, observed := range []bool{false, true} {
		name := "baseline"
		if observed {
			name = "telemetry"
		}
		b.Run(name, func(b *testing.B) {
			base, head, r, w := observedRequestPair(b)
			h := base
			if observed {
				h = head
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				h.ServeHTTP(w, r)
			}
		})
	}
}

// Timing receipts are opt-in; ordinary race runs retain the allocation gate.
func TestRequestObserverTimingPairs(t *testing.T) {
	if os.Getenv("GOSX_TELEMETRY_REQUEST_TIMING_PAIRS") != "1" {
		t.Skip("opt-in timing receipt")
	}
	previous := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(previous)
	base, head, r, w := observedRequestPair(t)
	const requests = 200000
	measure := func(h http.Handler) float64 {
		runtime.GC()
		start := time.Now()
		for range requests {
			h.ServeHTTP(w, r)
		}
		return float64(time.Since(start).Nanoseconds()) / requests
	}
	for i := range 10 {
		var baseline, observed float64
		if i%2 == 0 {
			baseline, observed = measure(base), measure(head)
		} else {
			observed, baseline = measure(head), measure(base)
		}
		t.Logf("pair=%d baseline_ns=%.3f observed_ns=%.3f delta_ns=%.3f", i+1, baseline, observed, observed-baseline)
	}
}
