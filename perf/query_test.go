//go:build browser

package perf

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestQueryNavigationTiming(t *testing.T) {
	d, err := New(WithHeadless(true), WithTimeout(10*time.Second))
	if err != nil {
		t.Skipf("skipping: %v", err)
	}
	defer d.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><head><title>Nav Test</title></head><body><p>hello</p></body></html>`)
	}))
	defer srv.Close()

	if err := d.Navigate(srv.URL); err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	if err := d.WaitReady(); err != nil {
		t.Fatalf("WaitReady: %v", err)
	}

	// Wait for load event to complete so timing entries are populated
	time.Sleep(500 * time.Millisecond)

	nav, err := QueryNavigationTiming(d)
	if err != nil {
		t.Fatalf("QueryNavigationTiming: %v", err)
	}

	if nav.TTFB <= 0 {
		t.Errorf("expected TTFB > 0, got %f", nav.TTFB)
	}
	if nav.DOMContentLoaded <= 0 {
		t.Errorf("expected DOMContentLoaded > 0, got %f", nav.DOMContentLoaded)
	}
}

func TestQueryRuntimeState(t *testing.T) {
	d, err := New(WithHeadless(true), WithTimeout(10*time.Second))
	if err != nil {
		t.Skipf("skipping: %v", err)
	}
	defer d.Close()

	if err := InjectDriver(d); err != nil {
		t.Fatalf("InjectDriver: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, minimalGosxPage)
	}))
	defer srv.Close()

	if err := d.Navigate(srv.URL); err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	if err := d.WaitReady(); err != nil {
		t.Fatalf("WaitReady: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	st, err := QueryRuntimeState(d)
	if err != nil {
		t.Fatalf("QueryRuntimeState: %v", err)
	}

	if !st.PerfReady {
		t.Error("expected PerfReady to be true")
	}
}
