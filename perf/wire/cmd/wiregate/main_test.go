package main

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"m31labs.dev/gosx/perf/wire"
)

const budgetJSON = `{"schema":"gosx.wire-budget/v1","tolerance":{"bytesPercent":2,"bytesMin":512},"apps":{"a":{"/":{"limits":{"totalWireBytes":100},"require":["no-cookie"]}}}}`

func TestRatchetNeverPassesOnAMissingBase(t *testing.T) {
	dir := t.TempDir()
	head := filepath.Join(dir, "head.json")
	if err := os.WriteFile(head, []byte(budgetJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	empty := filepath.Join(dir, "empty.json")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	for name, args := range map[string][]string{
		"missing file": {"ratchet", "-base", filepath.Join(dir, "absent.json"), "-head", head},
		"empty file":   {"ratchet", "-base", empty, "-head", head},
		"no base flag": {"ratchet", "-head", head},
	} {
		if err := run(args, &out, &out); err == nil {
			t.Errorf("%s: ratchet passed, want an error", name)
		}
	}
	if err := run([]string{"ratchet", "-initial", "-head", head}, &out, &out); err != nil {
		t.Fatalf("-initial: %v", err)
	}
	if err := run([]string{"ratchet", "-base", head, "-head", head}, &out, &out); err != nil {
		t.Fatalf("identical budgets: %v", err)
	}

	raised := filepath.Join(dir, "raised.json")
	if err := os.WriteFile(raised, bytes.Replace([]byte(budgetJSON), []byte(`"totalWireBytes":100`), []byte(`"totalWireBytes":101`), 1), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"ratchet", "-base", head, "-head", raised}, &out, &out); !errors.Is(err, errGate) {
		t.Fatalf("raised limit: err = %v, want the gate to fail", err)
	}
}

func TestCheckSharesCacheWithinVisitAndResetsBetweenApps(t *testing.T) {
	var downloads atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/gosx/nav.0123456789abcdef.js" {
			downloads.Add(1)
			w.Header().Set("Cache-Control", "public, max-age=3600, immutable")
			w.Write([]byte("navigation();"))
			return
		}
		w.Write([]byte(`<script src="/gosx/nav.0123456789abcdef.js"></script>`))
	}))
	defer srv.Close()
	budget := wire.Budget{
		Schema:    wire.BudgetSchema,
		Tolerance: wire.Tolerance{BytesMin: 1000000},
		Apps:      map[string]wire.AppBudget{},
	}
	for _, app := range []string{"a", "b"} {
		budget.Apps[app] = wire.AppBudget{}
		for _, route := range []string{"/", "/next"} {
			rb := wire.RouteBudget{Limits: map[string]int64{}}
			for _, metric := range wire.Metrics {
				rb.Limits[metric] = 1000000
			}
			rb.Limits[wire.MetricRequests] = 1
			if route == "/" {
				rb.Limits[wire.MetricRequests] = 2
			}
			for _, policy := range wire.Policies {
				if policy != wire.PolicyHTMLCompressed {
					rb.Require = append(rb.Require, policy)
				}
			}
			budget.Apps[app][route] = rb
		}
	}
	data, err := budget.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	budgetPath, reportPath := filepath.Join(dir, "budget.json"), filepath.Join(dir, "report.json")
	if err := os.WriteFile(budgetPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := run([]string{"check", "-budget", budgetPath, "-app", "a=" + srv.URL, "-app", "b=" + srv.URL, "-report", reportPath}, &out, &out); err != nil {
		t.Fatalf("check: %v\n%s", err, &out)
	}
	if downloads.Load() != 2 {
		t.Fatalf("downloads=%d want=2, one per app visit", downloads.Load())
	}
	report, err := readReport(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	if report.Methodology != visitMethodology || len(report.Routes) != 4 {
		t.Fatalf("report=%+v", report)
	}
	for i, route := range report.Routes {
		if route.Resources[0].CacheHit != (i%2 == 1) {
			t.Fatalf("route %d cache metadata=%+v", i, route.Resources)
		}
	}
}
