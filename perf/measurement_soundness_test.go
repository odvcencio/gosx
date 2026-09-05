package perf

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestResolveNetworkMetricsTreatAbsentWaterfallAsUnavailable(t *testing.T) {
	page := PageReport{URL: "https://example.test/"}

	for _, metric := range []string{"network_kb", "requests"} {
		if actual, ok := ResolvePageMetric(metric, &page); ok {
			t.Fatalf("%s resolved absent waterfall as found zero: actual=%v", metric, actual)
		}
	}
}

func TestEvaluateBudgetFailsMandatoryMissingNetworkMetric(t *testing.T) {
	report := &Report{PageReport: PageReport{URL: "https://example.test/"}}
	budget := &BudgetFile{
		DefaultProfile: "default",
		Profiles: map[string]BudgetProfile{
			"default": {Assertions: []string{"network_kb <= 0"}},
		},
	}

	result, err := EvaluateBudget(report, budget, "")
	if err != nil {
		t.Fatalf("EvaluateBudget: %v", err)
	}
	if result.Passed {
		t.Fatalf("mandatory missing network metric must fail, got pass: %+v", result)
	}
	assertion := result.Pages[0].Assertions[0]
	if assertion.Found || assertion.Passed {
		t.Fatalf("mandatory missing network metric was not explicit failure: %+v", assertion)
	}
}

func TestCompareReportsDoesNotTreatMissingCandidateWaterfallAsZeroImprovement(t *testing.T) {
	baseline := &Report{
		URL: "baseline",
		Pages: []PageReport{{
			URL:                   "https://example.test/",
			Resources:             []ResourceEntry{{Name: "app.js", TransferSize: 100 * 1024}},
			TotalBytesTransferred: 100 * 1024,
			Measurements: &MeasurementStatus{
				Version: measurementStatusVersion,
				Groups: map[string]CollectorStatus{
					"resource-waterfall": {State: MeasurementOK},
				},
			},
		}},
	}
	candidate := &Report{
		URL:   "candidate",
		Pages: []PageReport{{URL: "https://example.test/"}},
	}

	cmp := CompareReports(baseline, candidate)
	networkRow := findComparedMetric(cmp.Metrics, "Network bytes")
	if networkRow == nil {
		t.Fatalf("comparison should surface incomplete network evidence: %+v", cmp.Metrics)
	}
	if !networkRow.BaselineFound || networkRow.CandidateFound || !networkRow.Incomplete {
		t.Fatalf("missing candidate waterfall was not explicit inconclusive evidence: %+v", *networkRow)
	}
	if strings.Contains(networkRow.IncompleteReason, "improved") {
		t.Fatalf("incomplete network comparison must not be labelled improvement: %+v", *networkRow)
	}
	requestsRow := findComparedMetric(cmp.Metrics, "Requests")
	if requestsRow == nil || !requestsRow.Incomplete || requestsRow.CandidateFound {
		t.Fatalf("missing candidate requests evidence was not explicit inconclusive evidence: %+v", cmp.Metrics)
	}
	if AnyRegression(cmp, 5) {
		t.Fatalf("incomplete comparison evidence must not be labelled a regression")
	}
	if !AnyIncomplete(cmp) {
		t.Fatalf("incomplete required comparison evidence must have distinct incomplete gate")
	}
	data, err := json.Marshal(networkRow)
	if err != nil {
		t.Fatalf("Marshal incomplete row: %v", err)
	}
	if strings.Contains(string(data), "deltaPct") || !strings.Contains(string(data), `"candidateFound":false`) || !strings.Contains(string(data), `"incomplete":true`) {
		t.Fatalf("incomplete JSON row should omit deltaPct and expose flags/reason: %s", data)
	}
}

func TestCompareReportsWithoutPagesIsInconclusive(t *testing.T) {
	cmp := CompareReports(&Report{URL: "baseline"}, &Report{URL: "candidate"})
	if len(cmp.Metrics) != 1 || !cmp.Metrics[0].Incomplete {
		t.Fatalf("missing pages should produce explicit incomplete comparison: %+v", cmp.Metrics)
	}
	if AnyRegression(cmp, 5) || !AnyIncomplete(cmp) {
		t.Fatalf("empty comparison evidence must be incomplete, not regression")
	}
	if out := FormatComparison(cmp, 5); !strings.Contains(out, "inconclusive") || !strings.Contains(out, "no comparable pages") {
		t.Fatalf("formatted empty comparison should be inconclusive:\n%s", out)
	}
}

func TestComparisonMeasuredZeroDeltaRemainsInJSON(t *testing.T) {
	row := ComparedMetric{Name: "Network bytes", BaselineFound: true, CandidateFound: true}
	data, err := json.Marshal(row)
	if err != nil {
		t.Fatalf("Marshal measured zero row: %v", err)
	}
	if !strings.Contains(string(data), `"deltaPct":0`) {
		t.Fatalf("measured zero row should retain numeric deltaPct: %s", data)
	}
}

func TestCollectPageReportWebGLFailureDoesNotHideSuccessfulWaterfall(t *testing.T) {
	queries := successfulPageReportQueries()
	webglErr := errors.New("webgl unavailable")
	queries.evaluate = func(_ *Driver, expr string, out interface{}) error {
		switch out.(type) {
		case *WebGLInfo:
			return webglErr
		default:
			return nil
		}
	}
	queries.resourceWaterfall = func(*Driver) ([]ResourceEntry, error) {
		return []ResourceEntry{{Name: "app.js", InitiatorType: "script", TransferSize: 8 * 1024, ResponseEnd: 9}}, nil
	}

	report, err := collectPageReport(nil, "https://example.test/", queries, nil)
	if err != nil {
		t.Fatalf("collectPageReport: %v", err)
	}
	if report.WebGL != nil {
		t.Fatalf("failed optional WebGL probe should stay unavailable, got %+v", report.WebGL)
	}
	if got, ok := ResolvePageMetric("network_kb", report); !ok || got != 8 {
		t.Fatalf("successful waterfall must remain observable despite WebGL failure: got %.2f found=%v", got, ok)
	}
}

func TestCollectPageReportWebGLProbeUsesBoundedChildContextAndCancelsIt(t *testing.T) {
	parent := &Driver{
		ctx:         context.Background(),
		ctxCancel:   func() {},
		allocCancel: func() {},
	}
	queries := successfulPageReportQueries()
	queries.resourceWaterfall = func(*Driver) ([]ResourceEntry, error) {
		return []ResourceEntry{}, nil
	}
	childContextSeen := false
	childCanceled := make(chan struct{})
	queries.evaluate = func(d *Driver, expr string, out interface{}) error {
		if !strings.Contains(expr, "__gosx_perf_webgl_info") {
			return nil
		}
		deadline, ok := d.Context().Deadline()
		if !ok || time.Until(deadline) > 3*time.Second {
			t.Fatalf("webgl probe did not use a short child deadline: deadline=%v ok=%v", deadline, ok)
		}
		if d.Context() == parent.Context() {
			t.Fatalf("webgl probe reused parent context")
		}
		childContextSeen = true
		go func(ctx context.Context) {
			<-ctx.Done()
			close(childCanceled)
		}(d.Context())
		return errors.New("gpu probe timeout")
	}

	report, err := collectPageReport(parent, "https://example.test/", queries, nil)
	if err != nil {
		t.Fatalf("collectPageReport: %v", err)
	}
	if !childContextSeen {
		t.Fatalf("webgl probe was not exercised")
	}
	select {
	case <-childCanceled:
	case <-time.After(time.Second):
		t.Fatalf("webgl child context was not canceled after probe")
	}
	if parent.Context().Err() != nil {
		t.Fatalf("webgl child cancellation canceled parent: %v", parent.Context().Err())
	}
	if status := report.measurementStatus("webgl-info"); status.State != MeasurementFailed {
		t.Fatalf("webgl status = %+v, want failed", status)
	}
	if _, ok := ResolvePageMetric("network_kb", report); !ok {
		t.Fatalf("resource waterfall should remain usable after webgl failure")
	}
}

func TestCollectPageReportOptionalParentCancellationReturnsNilReport(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*pageReportQueries, context.CancelFunc)
		wantPhase string
	}{
		{
			name: "vitals",
			mutate: func(q *pageReportQueries, cancel context.CancelFunc) {
				q.evaluate = func(*Driver, string, interface{}) error {
					cancel()
					return context.Canceled
				}
			},
			wantPhase: "collect/vitals",
		},
		{
			name: "resource waterfall",
			mutate: func(q *pageReportQueries, cancel context.CancelFunc) {
				q.resourceWaterfall = func(*Driver) ([]ResourceEntry, error) {
					cancel()
					return nil, context.Canceled
				}
			},
			wantPhase: "collect/resource-waterfall",
		},
		{
			name: "long tasks",
			mutate: func(q *pageReportQueries, cancel context.CancelFunc) {
				q.evaluate = func(_ *Driver, expr string, out interface{}) error {
					if strings.Contains(expr, "longTasks") {
						cancel()
						return context.Canceled
					}
					return nil
				}
			},
			wantPhase: "collect/long-tasks",
		},
		{
			name: "webgl",
			mutate: func(q *pageReportQueries, cancel context.CancelFunc) {
				q.evaluate = func(_ *Driver, expr string, out interface{}) error {
					if strings.Contains(expr, "__gosx_perf_webgl_info") {
						cancel()
						return context.Canceled
					}
					return nil
				}
			},
			wantPhase: "collect/webgl-info",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			driver := &Driver{ctx: ctx, ctxCancel: cancel, allocCancel: func() {}}
			queries := successfulPageReportQueries()
			tt.mutate(&queries, cancel)

			report, err := collectPageReport(driver, "https://example.test/", queries, nil)
			if err == nil || report != nil {
				t.Fatalf("parent cancellation should return nil report and error, got report=%+v err=%v", report, err)
			}
			if !strings.Contains(err.Error(), tt.wantPhase) {
				t.Fatalf("error %q does not include phase %q", err, tt.wantPhase)
			}
		})
	}
}

func TestCollectPageReportOptionalParentCancellationPrefersRouteCause(t *testing.T) {
	unrelated := errors.New("collector returned stale optional error")
	tests := []struct {
		name      string
		mutate    func(*pageReportQueries, context.CancelFunc)
		wantCause error
		wantPhase string
	}{
		{
			name: "vitals cancel nil",
			mutate: func(q *pageReportQueries, cancel context.CancelFunc) {
				q.evaluate = func(_ *Driver, expr string, out interface{}) error {
					if strings.Contains(expr, "largestContentfulPaint") {
						cancel()
						return nil
					}
					return nil
				}
			},
			wantCause: context.Canceled,
			wantPhase: "collect/vitals",
		},
		{
			name: "long tasks cancel unrelated",
			mutate: func(q *pageReportQueries, cancel context.CancelFunc) {
				q.evaluate = func(_ *Driver, expr string, out interface{}) error {
					if strings.Contains(expr, "longTasks") {
						cancel()
						return unrelated
					}
					return nil
				}
			},
			wantCause: context.Canceled,
			wantPhase: "collect/long-tasks",
		},
		{
			name: "resource cancel nil",
			mutate: func(q *pageReportQueries, cancel context.CancelFunc) {
				q.resourceWaterfall = func(*Driver) ([]ResourceEntry, error) {
					cancel()
					return nil, nil
				}
			},
			wantCause: context.Canceled,
			wantPhase: "collect/resource-waterfall",
		},
		{
			name: "webgl cancel nil last optional phase",
			mutate: func(q *pageReportQueries, cancel context.CancelFunc) {
				q.evaluate = func(_ *Driver, expr string, out interface{}) error {
					if strings.Contains(expr, "__gosx_perf_webgl_info") {
						cancel()
						return nil
					}
					return nil
				}
			},
			wantCause: context.Canceled,
			wantPhase: "collect/webgl-info",
		},
		{
			name: "webgl cancel unrelated last optional phase",
			mutate: func(q *pageReportQueries, cancel context.CancelFunc) {
				q.evaluate = func(_ *Driver, expr string, out interface{}) error {
					if strings.Contains(expr, "__gosx_perf_webgl_info") {
						cancel()
						return unrelated
					}
					return nil
				}
			},
			wantCause: context.Canceled,
			wantPhase: "collect/webgl-info",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			driver := &Driver{ctx: ctx, ctxCancel: cancel, allocCancel: func() {}}
			queries := successfulPageReportQueries()
			tt.mutate(&queries, cancel)

			report, err := collectPageReport(driver, "https://example.test/", queries, nil)
			if report != nil || err == nil {
				t.Fatalf("route cancellation should return nil report and error, got report=%+v err=%v", report, err)
			}
			if !errors.Is(err, tt.wantCause) {
				t.Fatalf("error should preserve route cause %v, got %v", tt.wantCause, err)
			}
			if errors.Is(err, unrelated) {
				t.Fatalf("route cancellation should take precedence over unrelated optional error: %v", err)
			}
			if !strings.Contains(err.Error(), tt.wantPhase) {
				t.Fatalf("error %q does not include phase %q", err, tt.wantPhase)
			}
		})
	}
}

func TestCollectPageReportOptionalParentDeadlinePrefersRouteCause(t *testing.T) {
	unrelated := errors.New("collector returned stale optional error")
	tests := []struct {
		name      string
		mutate    func(*testing.T, context.Context, *pageReportQueries)
		wantPhase string
	}{
		{
			name: "resource deadline nil",
			mutate: func(t *testing.T, ctx context.Context, q *pageReportQueries) {
				q.resourceWaterfall = func(*Driver) ([]ResourceEntry, error) {
					select {
					case <-ctx.Done():
					case <-time.After(time.Second):
						t.Fatal("resource query did not observe parent deadline")
					}
					return nil, nil
				}
			},
			wantPhase: "collect/resource-waterfall",
		},
		{
			name: "resource deadline unrelated",
			mutate: func(t *testing.T, ctx context.Context, q *pageReportQueries) {
				q.resourceWaterfall = func(*Driver) ([]ResourceEntry, error) {
					select {
					case <-ctx.Done():
					case <-time.After(time.Second):
						t.Fatal("resource query did not observe parent deadline")
					}
					return nil, unrelated
				}
			},
			wantPhase: "collect/resource-waterfall",
		},
		{
			name: "webgl deadline nil last optional phase",
			mutate: func(t *testing.T, ctx context.Context, q *pageReportQueries) {
				q.evaluate = func(_ *Driver, expr string, out interface{}) error {
					if !strings.Contains(expr, "__gosx_perf_webgl_info") {
						return nil
					}
					select {
					case <-ctx.Done():
					case <-time.After(time.Second):
						t.Fatal("webgl query did not observe parent deadline")
					}
					return nil
				}
			},
			wantPhase: "collect/webgl-info",
		},
		{
			name: "webgl deadline unrelated last optional phase",
			mutate: func(t *testing.T, ctx context.Context, q *pageReportQueries) {
				q.evaluate = func(_ *Driver, expr string, out interface{}) error {
					if !strings.Contains(expr, "__gosx_perf_webgl_info") {
						return nil
					}
					select {
					case <-ctx.Done():
					case <-time.After(time.Second):
						t.Fatal("webgl query did not observe parent deadline")
					}
					return unrelated
				}
			},
			wantPhase: "collect/webgl-info",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()
			driver := &Driver{ctx: ctx, ctxCancel: cancel, allocCancel: func() {}}
			queries := successfulPageReportQueries()
			tt.mutate(t, ctx, &queries)

			report, err := collectPageReport(driver, "https://example.test/", queries, nil)
			if report != nil || err == nil {
				t.Fatalf("route deadline should return nil report and error, got report=%+v err=%v", report, err)
			}
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("error should preserve route deadline cause, got %v", err)
			}
			if errors.Is(err, unrelated) {
				t.Fatalf("route deadline should take precedence over unrelated optional error: %v", err)
			}
			if !strings.Contains(err.Error(), tt.wantPhase) {
				t.Fatalf("error %q does not include phase %q", err, tt.wantPhase)
			}
		})
	}
}

func TestCompareReportsFIDWithoutInputIsIncomplete(t *testing.T) {
	baseline := &Report{
		URL: "baseline",
		Pages: []PageReport{{
			URL:               "https://example.test/",
			FirstInputDelayMs: 0,
			Measurements: &MeasurementStatus{
				Version: measurementStatusVersion,
				Groups: map[string]CollectorStatus{
					"fid": {State: MeasurementUnobserved, Reason: "no first input entry was observed"},
				},
			},
		}},
	}
	candidate := &Report{
		URL: "candidate",
		Pages: []PageReport{{
			URL:               "https://example.test/",
			FirstInputDelayMs: 0,
			Measurements: &MeasurementStatus{
				Version: measurementStatusVersion,
				Groups: map[string]CollectorStatus{
					"fid": {State: MeasurementUnobserved, Reason: "no first input entry was observed"},
				},
			},
		}},
	}

	cmp := CompareReports(baseline, candidate)
	fid := findComparedMetric(cmp.Metrics, "FID")
	if fid == nil || !fid.Incomplete || fid.BaselineFound || fid.CandidateFound {
		t.Fatalf("FID without scenario input should be an explicit incomplete comparison: %+v", fid)
	}
	if AnyRegression(cmp, 5) || !AnyIncomplete(cmp) {
		t.Fatalf("FID no-input comparison should be incomplete, not regression")
	}
	if !strings.Contains(fid.IncompleteReason, "no first input") {
		t.Fatalf("FID incomplete reason should explain missing input sample: %+v", fid)
	}
}

func TestCollectPageReportRecordsOptionalMeasurementStatuses(t *testing.T) {
	tests := []struct {
		name          string
		vitalsStatus  map[string]any
		longStatus    map[string]any
		wantMetric    string
		wantFound     bool
		wantState     MeasurementState
		wantLongState MeasurementState
	}{
		{
			name: "lcp observed zero is measured",
			vitalsStatus: map[string]any{
				"version":         1,
				"lcp":             map[string]any{"supported": true, "installed": true, "observed": true},
				"cls":             map[string]any{"supported": true, "installed": true},
				"fid":             map[string]any{"supported": true, "installed": true, "observed": true},
				"runtimeCounters": map[string]any{"available": true},
			},
			longStatus:    map[string]any{"version": 1, "longTasks": map[string]any{"supported": true, "installed": true}},
			wantMetric:    "lcp",
			wantFound:     true,
			wantState:     MeasurementOK,
			wantLongState: MeasurementOK,
		},
		{
			name: "lcp without sample is unobserved",
			vitalsStatus: map[string]any{
				"version":         1,
				"lcp":             map[string]any{"supported": true, "installed": true, "observed": false},
				"cls":             map[string]any{"supported": true, "installed": true},
				"fid":             map[string]any{"supported": true, "installed": true, "observed": true},
				"runtimeCounters": map[string]any{"available": true},
			},
			longStatus:    map[string]any{"version": 1, "longTasks": map[string]any{"supported": true, "installed": true}},
			wantMetric:    "lcp",
			wantFound:     false,
			wantState:     MeasurementUnobserved,
			wantLongState: MeasurementOK,
		},
		{
			name: "unsupported observer stays unavailable",
			vitalsStatus: map[string]any{
				"version":         1,
				"lcp":             map[string]any{"supported": false, "installed": false},
				"cls":             map[string]any{"supported": true, "installed": true},
				"fid":             map[string]any{"supported": true, "installed": true, "observed": true},
				"runtimeCounters": map[string]any{"available": true},
			},
			longStatus:    map[string]any{"version": 1, "longTasks": map[string]any{"supported": false}},
			wantMetric:    "lcp",
			wantFound:     false,
			wantState:     MeasurementUnsupported,
			wantLongState: MeasurementUnsupported,
		},
		{
			name: "missing browser status version fails closed",
			vitalsStatus: map[string]any{
				"lcp":             map[string]any{"supported": true, "installed": true, "observed": true},
				"cls":             map[string]any{"supported": true, "installed": true},
				"fid":             map[string]any{"supported": true, "installed": true, "observed": true},
				"runtimeCounters": map[string]any{"available": true},
			},
			longStatus:    map[string]any{"longTasks": map[string]any{"supported": true, "installed": true}},
			wantMetric:    "signal_writes",
			wantFound:     false,
			wantState:     MeasurementUnknown,
			wantLongState: MeasurementUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			queries := successfulPageReportQueries()
			queries.evaluate = func(_ *Driver, expr string, out interface{}) error {
				switch {
				case strings.Contains(expr, "largestContentfulPaint"):
					payload := map[string]any{
						"available":       true,
						"lcp":             0,
						"cls":             0,
						"fid":             0,
						"signalWrites":    0,
						"signalReads":     0,
						"hubMessageCount": 0,
						"hubMessageBytes": 0,
						"hubSendCount":    0,
						"status":          tt.vitalsStatus,
					}
					return marshalInto(payload, out)
				case strings.Contains(expr, "longTasks"):
					return marshalInto(map[string]any{"available": true, "status": tt.longStatus, "tasks": []any{}}, out)
				case strings.Contains(expr, "__gosx_perf_webgl_info"):
					return nil
				default:
					return nil
				}
			}
			queries.resourceWaterfall = func(*Driver) ([]ResourceEntry, error) { return []ResourceEntry{}, nil }

			report, err := collectPageReport(nil, "https://example.test/", queries, nil)
			if err != nil {
				t.Fatalf("collectPageReport: %v", err)
			}
			if _, ok := ResolvePageMetric(tt.wantMetric, report); ok != tt.wantFound {
				t.Fatalf("%s found=%v, want %v; statuses=%+v", tt.wantMetric, ok, tt.wantFound, report.Measurements)
			}
			if got := report.measurementStatus("lcp").State; got != tt.wantState {
				t.Fatalf("lcp state = %q, want %q", got, tt.wantState)
			}
			if got := report.measurementStatus("long-tasks").State; got != tt.wantLongState {
				t.Fatalf("long-tasks state = %q, want %q", got, tt.wantLongState)
			}
		})
	}
}

func TestSuccessfulEmptyWaterfallIsMeasuredZero(t *testing.T) {
	queries := successfulPageReportQueries()
	queries.resourceWaterfall = func(*Driver) ([]ResourceEntry, error) { return []ResourceEntry{}, nil }

	report, err := collectPageReport(nil, "https://example.test/", queries, nil)
	if err != nil {
		t.Fatalf("collectPageReport: %v", err)
	}
	for _, metric := range []string{"network_kb", "requests"} {
		got, ok := ResolvePageMetric(metric, report)
		if !ok || got != 0 {
			t.Fatalf("%s = %.2f, %v; want measured zero", metric, got, ok)
		}
	}
	if waterfall := FormatWaterfallTable(&Report{PageReport: *report}); !strings.Contains(waterfall, "Resource Waterfall (0 entries)") {
		t.Fatalf("empty measured waterfall should be explicit:\n%s", waterfall)
	}
}

func TestLegacyAndUnsupportedMeasurementStatusCannotProveOptionalMetrics(t *testing.T) {
	pages := []PageReport{
		{
			URL:                      "legacy",
			LargestContentfulPaintMs: 0,
			TotalBytesTransferred:    0,
			LongTaskCount:            0,
		},
		{
			URL:                      "unsupported-version",
			LargestContentfulPaintMs: 0,
			TotalBytesTransferred:    0,
			Measurements: &MeasurementStatus{Version: 99, Groups: map[string]CollectorStatus{
				"lcp":                {State: MeasurementOK},
				"resource-waterfall": {State: MeasurementOK},
			}},
		},
	}
	for i := range pages {
		for _, metric := range []string{"lcp", "network_kb"} {
			if got, ok := ResolvePageMetric(metric, &pages[i]); ok {
				t.Fatalf("%s page %s resolved as %.2f despite absent/unsupported status", metric, pages[i].URL, got)
			}
		}
	}
}

func TestEvalAssertionsExposesMandatoryUnavailableAndOptionalSkip(t *testing.T) {
	report := &Report{PageReport: PageReport{URL: "https://example.test/"}}
	assertions := []Assertion{
		{Metric: "network_kb", Op: "<=", Value: 0},
		{Metric: "lcp", Op: "<=", Value: 1000, Optional: true},
	}

	results := EvalAssertions(assertions, report)
	if results[0].Passed || results[0].Found || results[0].Reason == "" {
		t.Fatalf("mandatory unavailable assertion was not explicit: %+v", results[0])
	}
	if !results[1].Passed || !results[1].Skipped || results[1].Found || results[1].Reason == "" {
		t.Fatalf("optional unavailable assertion was not visible skip: %+v", results[1])
	}
}

func marshalInto(payload any, out interface{}) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

func findComparedMetric(metrics []ComparedMetric, name string) *ComparedMetric {
	for i := range metrics {
		if metrics[i].Name == name {
			return &metrics[i]
		}
	}
	return nil
}

func markMeasurementGroupsOK(page *PageReport, groups ...string) {
	if page.Measurements == nil {
		page.Measurements = &MeasurementStatus{Version: measurementStatusVersion, Groups: map[string]CollectorStatus{}}
	}
	if page.Measurements.Groups == nil {
		page.Measurements.Groups = map[string]CollectorStatus{}
	}
	for _, group := range groups {
		page.Measurements.Groups[group] = CollectorStatus{State: MeasurementOK}
	}
}
