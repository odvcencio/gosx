package budget

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"os"
	"reflect"
	"strings"
	"testing"
)

func publicTestReport(t *testing.T) *Report {
	t.Helper()
	data, err := os.ReadFile("testdata/public-report.v1.json")
	if err != nil {
		t.Fatal(err)
	}
	record, err := DecodeRecord(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return record.(*Report)
}

func publicTestRecords(t *testing.T) []any {
	t.Helper()
	r := publicTestReport(t)
	cell := Cell{App: "fixture", RouteTemplate: "/counter/", PageType: "island", Scenario: "hard-cold", Metric: "lcp", Backend: "none", Unit: "ms"}
	return []any{r,
		&SeriesPoint{Schema: "gosx.perf-series/v1", Info: r.Info, Cell: cell, At: "2026-01-01T00:00:00Z", RunOrdinal: 1, N: 2, Samples: []float64{1, 2}, Median: 1.5, P75: 2, MAD: .5, InvalidReasons: []CountReason{}},
		&PairReport{Schema: "gosx.perf-pair/v1", Info: r.Info, FamilySize: 1, Looks: []int64{10, 20, 30, 40}, Seed: math.MaxUint64, Test: "signed-rank", PlanSHA256: strings.Repeat("1", 64), Status: "partial",
			Cells: []PairCell{{Cell: cell, BaseSamples: []float64{}, HeadSamples: []float64{}, AdjustedInterval: Interval{ConfidencePPM: 996875}, DescriptiveInterval: Interval{ConfidencePPM: 950000}, Decision: "inconclusive", InvalidReasons: []CountReason{}}}},
		&FieldSnapshot{Schema: "gosx.perf-field/v1", WindowStart: "2026-01-01T00:00:00Z", WindowEnd: "2026-01-01T00:15:00Z", Vitals: []FieldVital{{App: "fixture", RouteTemplate: "/counter/", Metric: "lcp", Device: "phone", Count: 2, Bounds: []float64{1000}, CumulativeCounts: []int64{1, 2}, ReasonCode: "ok"}}, Hubs: []FieldHub{}},
		&RunStatus{Schema: "gosx.perf-run/v1", Info: r.Info, Status: "skipped-busy", ReasonCode: "busy", PlannedCells: 2},
	}
}

func TestReportSchemaTypedRoundTrips(t *testing.T) {
	for _, record := range publicTestRecords(t) {
		data, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := DecodeRecord(bytes.NewReader(data))
		if err != nil || !reflect.DeepEqual(decoded, record) {
			t.Fatalf("%T round trip: %v", record, err)
		}
	}
}

func TestReportSchemaNoFreeMapsOrNativeOptions(t *testing.T) {
	var walk func(reflect.Type)
	seen := map[reflect.Type]bool{}
	walk = func(typ reflect.Type) {
		if seen[typ] {
			return
		}
		seen[typ] = true
		switch typ.Kind() {
		case reflect.Map, reflect.Interface:
			t.Fatalf("untyped public branch: %s", typ)
		case reflect.Pointer, reflect.Slice:
			walk(typ.Elem())
		case reflect.Struct:
			for i := 0; i < typ.NumField(); i++ {
				walk(typ.Field(i).Type)
			}
		}
	}
	for _, record := range publicTestRecords(t) {
		walk(reflect.TypeOf(record))
	}
}

func TestReportSchemaRejectsDomains(t *testing.T) {
	for _, name := range []string{"coverage", "ownership", "browser-ip", "index", "sample-count", "ratio", "byte-fraction", "minute", "unit", "pairs", "interval", "histogram", "window", "skip-count", "nonfinite"} {
		t.Run(name, func(t *testing.T) {
			records := publicTestRecords(t)
			var record any = records[0]
			switch name {
			case "coverage":
				record.(*Report).Coverage.AssetsMeasured++
			case "ownership":
				record.(*Report).Rows[0].AppBytes++
			case "browser-ip":
				record.(*Report).Info.ChromeProduct = "192.168.1.1"
			case "index":
				value := int64(101)
				record.(*Report).Info.BenchmarkIndexRounded = &value
			case "sample-count":
				record = records[1]
				record.(*SeriesPoint).N++
			case "ratio":
				record = records[1]
				s := record.(*SeriesPoint)
				s.Cell.Metric, s.Cell.Unit = "dropped_frame_rate", "ratio"
				s.Samples[0] = 1.1
			case "byte-fraction":
				record = records[1]
				s := record.(*SeriesPoint)
				s.Cell.Metric, s.Cell.Unit = "js_heap_peak", "B"
				s.Samples[0] = 1.5
			case "minute":
				record = records[1]
				record.(*SeriesPoint).At = "2026-01-01T00:00:01Z"
			case "unit":
				record = records[1]
				record.(*SeriesPoint).Cell.Unit = "B"
			case "pairs":
				record = records[2]
				record.(*PairReport).Cells[0].Pairs = 1
			case "interval":
				record = records[2]
				record.(*PairReport).Cells[0].AdjustedInterval.Bounded = true
			case "histogram":
				record = records[3]
				record.(*FieldSnapshot).Vitals[0].CumulativeCounts = []int64{2, 1}
			case "window":
				record = records[3]
				record.(*FieldSnapshot).WindowEnd = record.(*FieldSnapshot).WindowStart
			case "skip-count":
				record = records[4]
				record.(*RunStatus).CompletedCells = 1
			case "nonfinite":
				record = records[1]
				record.(*SeriesPoint).Samples[0] = math.Inf(1)
			}
			var err error
			if name == "nonfinite" {
				err = validateRecordDomains(record)
			} else {
				data, marshalErr := json.Marshal(record)
				if marshalErr != nil {
					t.Fatal(marshalErr)
				}
				_, err = DecodeRecord(bytes.NewReader(data))
			}
			var typed *InputError
			if !errors.As(err, &typed) || typed.Code != "invalid-input" || typed.Pointer == "" || strings.Contains(err.Error(), "192.168.1.1") {
				t.Fatalf("unsafe or missing domain error: %v", err)
			}
		})
	}
}

func TestReportSchemaFractionalSummaryAndUnknownCoverage(t *testing.T) {
	records := publicTestRecords(t)
	s := records[1].(*SeriesPoint)
	s.Cell.Metric, s.Cell.Unit = "js_heap_peak", "B"
	data, _ := json.Marshal(s)
	if _, err := DecodeRecord(bytes.NewReader(data)); err != nil {
		t.Fatal("integer samples can have fractional medians", err)
	}
	r := records[0].(*Report)
	r.Coverage.Reachability = "unknown"
	r.Coverage.AssetsMeasured++
	data, _ = json.Marshal(r)
	if _, err := DecodeRecord(bytes.NewReader(data)); err != nil {
		t.Fatal("unknown coverage findings must remain reportable", err)
	}
}

func TestReportSchemaLimitsAndPrivateRoots(t *testing.T) {
	data, err := os.ReadFile("testdata/public-report.v1.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, invalid := range [][]byte{
		append(append([]byte{}, data...), data...),
		bytes.Replace(data, []byte(`"muted": true`), []byte(`"muted": true, "muted": true`), 1),
		bytes.Replace(data, []byte(`"cpu-linux"`), []byte{'"', 'x', 0xff, '"'}, 1),
		bytes.Repeat([]byte(" "), maxInputBytes+1),
		[]byte(`{"schema":"gosx.budget/v2"}`),
	} {
		if _, err := DecodeRecord(bytes.NewReader(invalid)); err == nil {
			t.Fatal("invalid or private root accepted")
		}
	}
}
