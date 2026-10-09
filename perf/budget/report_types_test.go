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

func TestReportSchemaRawIntegerSampleDomains(t *testing.T) {
	for _, domain := range []struct{ metric, unit string }{{"js_heap_peak", "B"}, {"long_tasks", "count"}} {
		for _, field := range []string{"samples", "baseSamples", "headSamples"} {
			for _, tc := range []struct {
				name, number string
				valid        bool
				value        float64
			}{
				{"safe-max", "9007199254740991", true, 9007199254740991},
				{"first-unsafe", "9007199254740992", false, 0},
				{"rounded-unsafe", "9007199254740993", false, 0},
				{"fraction", "1.5", false, 0},
				{"rounded-fraction", "9007199254740990.5", false, 0},
				{"rounded-over-bound", "9007199254740991.1", false, 0},
				{"integer-decimal", "9007199254740991.0", true, 9007199254740991},
				{"integer-exponent", "90071992547409910e-1", true, 9007199254740991},
				{"fraction-exponent", "90071992547409911e-1", false, 0},
				{"positive-exponent", "1e3", true, 1000},
				{"decimal-exponent", "0.001e3", true, 1},
				{"negative", "-1", false, 0},
				{"negative-underflow", "-1e-400", false, 0},
				{"underflow", "1e-400", false, 0},
				{"negative-zero", "-0.00", true, 0},
				{"zero-large-exponent", "0e-999999999", true, 0},
			} {
				t.Run(domain.unit+"/"+field+"/"+tc.name, func(t *testing.T) {
					records := publicTestRecords(t)
					var record any
					pointer := "/" + field + "/0"
					if field == "samples" {
						s := records[1].(*SeriesPoint)
						s.Cell.Metric, s.Cell.Unit = domain.metric, domain.unit
						s.Samples, s.N = []float64{1}, 1
						record = s
					} else {
						p := records[2].(*PairReport)
						p.Cells[0].Cell.Metric, p.Cells[0].Cell.Unit = domain.metric, domain.unit
						p.Cells[0].BaseSamples, p.Cells[0].HeadSamples, p.Cells[0].Pairs = []float64{1}, []float64{1}, 1
						record, pointer = p, "/cells/0"+pointer
					}
					data, err := json.Marshal(record)
					if err != nil {
						t.Fatal(err)
					}
					needle := []byte(`"` + field + `":[1]`)
					if !bytes.Contains(data, needle) {
						t.Fatal("sample replacement target missing")
					}
					data = bytes.Replace(data, needle, []byte(`"`+field+`":[`+tc.number+`]`), 1)
					decoded, err := DecodeRecord(bytes.NewReader(data))
					if !tc.valid {
						var input *InputError
						if !errors.As(err, &input) || input.Code != "invalid-input" || input.Pointer != pointer {
							t.Fatalf("invalid raw sample accepted or wrong location: %v", err)
						}
						return
					}
					if err != nil {
						t.Fatal("exact integer sample rejected:", err)
					}
					var got float64
					switch r := decoded.(type) {
					case *SeriesPoint:
						got = r.Samples[0]
					case *PairReport:
						got = r.Cells[0].BaseSamples[0]
						if field == "headSamples" {
							got = r.Cells[0].HeadSamples[0]
						}
					}
					if got != tc.value {
						t.Fatal("accepted integer sample changed value")
					}
				})
			}
		}
	}
}

func TestReportSchemaRawFractionalSampleDomains(t *testing.T) {
	for _, tc := range []struct {
		metric, unit, number string
		valid                bool
	}{
		{"lcp", "ms", "1.5", true},
		{"lcp", "ms", "-1e-400", false},
		{"dropped_frame_rate", "ratio", "0.5", true},
		{"dropped_frame_rate", "ratio", "1.000", true},
		{"dropped_frame_rate", "ratio", "10e-1", true},
		{"dropped_frame_rate", "ratio", "1.0000000000000000001", false},
		{"dropped_frame_rate", "ratio", "10000000000000000001e-19", false},
	} {
		t.Run(tc.metric+"/"+tc.number, func(t *testing.T) {
			record := publicTestRecords(t)[1].(*SeriesPoint)
			record.Cell.Metric, record.Cell.Unit = tc.metric, tc.unit
			record.Samples, record.N, record.Median, record.P75 = []float64{1}, 1, 1, 1
			data, err := json.Marshal(record)
			if err != nil {
				t.Fatal(err)
			}
			data = bytes.Replace(data, []byte(`"samples":[1]`), []byte(`"samples":[`+tc.number+`]`), 1)
			_, err = DecodeRecord(bytes.NewReader(data))
			if tc.valid {
				if err != nil {
					t.Fatal("valid fractional sample rejected:", err)
				}
				return
			}
			var input *InputError
			if !errors.As(err, &input) || input.Pointer != "/samples/0" {
				t.Fatalf("invalid raw sample accepted or wrong location: %v", err)
			}
		})
	}
}

func TestReportSchemaPairDomainErrorPointers(t *testing.T) {
	for _, field := range []string{"baseSamples", "headSamples", "adjustedInterval", "descriptiveInterval"} {
		t.Run(field, func(t *testing.T) {
			record := publicTestRecords(t)[2].(*PairReport)
			row := &record.Cells[0]
			row.BaseSamples, row.HeadSamples, row.Pairs = []float64{1}, []float64{1}, 1
			pointer := "/cells/0/" + field
			switch field {
			case "baseSamples":
				row.BaseSamples[0], pointer = -1, pointer+"/0"
			case "headSamples":
				row.HeadSamples[0], pointer = -1, pointer+"/0"
			case "adjustedInterval", "descriptiveInterval":
				lower, upper := 2.0, 1.0
				interval := &row.AdjustedInterval
				if field == "descriptiveInterval" {
					interval = &row.DescriptiveInterval
				}
				interval.Lower, interval.Upper, interval.Bounded = &lower, &upper, true
			}
			data, err := json.Marshal(record)
			if err != nil {
				t.Fatal(err)
			}
			_, decodeErr := DecodeRecord(bytes.NewReader(data))
			for _, check := range []struct {
				name string
				err  error
			}{{"domain", validateRecordDomains(record)}, {"decode", decodeErr}} {
				var input *InputError
				if !errors.As(check.err, &input) || input.Code != "invalid-input" || input.Pointer != pointer {
					t.Errorf("%s named the wrong field: %v", check.name, check.err)
				}
			}
		})
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

func TestReportSchemaPValueFractions(t *testing.T) {
	ptr := func(value string) *string { return &value }
	for _, tc := range []struct {
		name                   string
		numerator, denominator *string
		valid                  bool
	}{
		{"unknown", nil, nil, true},
		{"greater-than-one", ptr("2"), ptr("1"), false},
		{"numerator-only", ptr("1"), nil, false},
		{"denominator-only", nil, ptr("1"), false},
		{"equal", ptr("1"), ptr("1"), true},
		{"zero", ptr("0"), ptr("1"), true},
		{"leading-zero-over-one", ptr("0002"), ptr("1"), false},
		{"large-over-one", ptr("18446744073709551617"), ptr("18446744073709551616"), false},
		{"large-equal", ptr("18446744073709551617"), ptr("18446744073709551617"), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			record := publicTestRecords(t)[2].(*PairReport)
			record.Cells[0].PNumerator = tc.numerator
			record.Cells[0].PDenominator = tc.denominator
			data, err := json.Marshal(record)
			if err != nil {
				t.Fatal(err)
			}
			decoded, decodeErr := DecodeRecord(bytes.NewReader(data))
			for label, err := range map[string]error{"domain": validateRecordDomains(record), "decode": decodeErr} {
				if tc.valid {
					if err != nil {
						t.Fatalf("%s rejected valid p-value: %v", label, err)
					}
				} else {
					var input *InputError
					if !errors.As(err, &input) || input.Code != "invalid-input" || !strings.HasPrefix(input.Pointer, "/cells/0/p") {
						t.Errorf("%s accepted invalid p-value or returned wrong error: %v", label, err)
					}
				}
			}
			if tc.valid && !reflect.DeepEqual(decoded, record) {
				t.Fatal("round trip changed p-value components")
			}
		})
	}
}

func TestReportSchemaHubHistogramUnits(t *testing.T) {
	for _, tc := range []struct {
		name, queueUnit, rttUnit, invalidField string
	}{
		{"valid", "count", "ms", ""},
		{"queue-depth-time", "ms", "ms", "queueDepth"},
		{"rtt-bytes", "count", "B", "rtt"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			record := publicTestRecords(t)[3].(*FieldSnapshot)
			record.Hubs = []FieldHub{{
				App: "fixture", Hub: "room", Direction: "out", Kind: "binary",
				Messages: 2, PayloadBytes: 1024, ClientSeconds: 1, ReferenceClients: 1, ReasonCode: "ok",
				QueueDepth: &NumericHistogram{Unit: tc.queueUnit, Bounds: []float64{1}, CumulativeCounts: []int64{1, 2}, Count: 2},
				RTT:        &NumericHistogram{Unit: tc.rttUnit, Bounds: []float64{100}, CumulativeCounts: []int64{1, 2}, Count: 2},
			}}
			data, err := json.Marshal(record)
			if err != nil {
				t.Fatal(err)
			}
			decoded, decodeErr := DecodeRecord(bytes.NewReader(data))
			for label, err := range map[string]error{
				"schema": validateTyped("FieldSnapshot", record),
				"domain": validateRecordDomains(record), "decode": decodeErr,
			} {
				if tc.invalidField == "" {
					if err != nil {
						t.Fatalf("%s rejected valid histogram units: %v", label, err)
					}
				} else {
					var input *InputError
					if !errors.As(err, &input) || input.Code != "invalid-input" || !strings.HasPrefix(input.Pointer, "/hubs/0/"+tc.invalidField) {
						t.Errorf("%s accepted incorrect histogram unit or returned wrong error: %v", label, err)
					}
				}
			}
			if tc.invalidField == "" && !reflect.DeepEqual(decoded, record) {
				t.Fatal("round trip changed histograms")
			}
		})
	}
}
