package budget

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func testPublicValidator(t *testing.T) *PublicValidator {
	t.Helper()
	root, err := inputRoot("go.mod", LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	v, err := NewPublicValidator(filepath.Join(root, "perf/budget/testdata/catalog.v1.json"), LoadOptions{RootDir: root}, []HubBudget{{App: "fixture", Hub: "lobby"}})
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func testPopulatedPublicRecords(t *testing.T) []any {
	records := publicTestRecords(t)
	report := records[0].(*Report)
	n := int64(100)
	multiplier := int64(4000)
	report.Info.BenchmarkIndexRounded = &n
	report.Info.CPUMultiplierMilli = &multiplier
	report.Info.ChromeProduct = "154.0.8034.0"
	report.Info.ChromeSnapshot = "1688711"
	report.Assets[0].BaseSizes = &SizeTriple{Raw: 100, Gzip: 50, Brotli: 40}
	report.Acknowledgments = []Ack{{Kind: "budget", Scope: "asset:app/fixture/counter", Metric: "brotli", Delta: 1024, Issue: 1, Disposition: "permanent", ReasonCode: "feature"}}
	report.Violations = []CountReason{{ReasonCode: "allocation", Count: 1}}
	series := records[1].(*SeriesPoint)
	series.InvalidReasons = []CountReason{{ReasonCode: "hidden", Count: 1}}
	pair := records[2].(*PairReport)
	lower, upper := -1.0, 1.0
	pair.Cells[0].AdjustedInterval = Interval{Lower: &lower, Upper: &upper, ConfidencePPM: 996875, Bounded: true}
	pair.Cells[0].DescriptiveInterval = Interval{Lower: &lower, Upper: &upper, ConfidencePPM: 950000, Bounded: true}
	pair.Cells[0].InvalidReasons = []CountReason{{ReasonCode: "environment", Count: 1}}
	field := records[3].(*FieldSnapshot)
	field.Hubs = []FieldHub{{App: "fixture", Hub: "lobby", Direction: "out", Kind: "text", Messages: 1, PayloadBytes: 12, ClientSeconds: 1, ReferenceClients: 1, ReasonCode: "ok",
		QueueDepth: &NumericHistogram{Unit: "count", Bounds: []float64{1}, CumulativeCounts: []int64{0, 1}, Count: 1}, RTT: &NumericHistogram{Unit: "ms", Bounds: []float64{1}, CumulativeCounts: []int64{0, 1}, Count: 1}}}
	return records
}

func TestPublicAcceptsEveryRecordAndZeroSampleSkip(t *testing.T) {
	v := testPublicValidator(t)
	for _, record := range testPopulatedPublicRecords(t) {
		data, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		if err := v.Validate(bytes.NewReader(data), "json"); err != nil {
			t.Fatalf("%T: %v", record, err)
		}
	}
	status := publicTestRecords(t)[4]
	data, _ := json.Marshal(status)
	if bytes.Contains(data, []byte("samples")) {
		t.Fatal("skip contains samples")
	}
	if err := ValidatePublic(bytes.NewReader(data), "json"); err != nil {
		t.Fatal(err)
	}
}

// Plant each forbidden category in every object branch, including optional and
// nested histograms, intervals, policy results, sizes and acknowledgment records.
func TestPublicRejectsPrivateFieldsInEveryBranch(t *testing.T) {
	v := testPublicValidator(t)
	plants := map[string]any{"hostname": "example.invalid", "username": "example-user", "localPath": `C:\Users\example\private`, "wslPath": "/home/example/private", "ip": "192.0.2.1", "endpointHash": strings.Repeat("a", 64), "adapter": "Private GPU adapter", "error": "private failure details", "url": "https://example.invalid/private"}
	for _, record := range testPopulatedPublicRecords(t) {
		data, _ := json.Marshal(record)
		var raw any
		if json.Unmarshal(data, &raw) != nil {
			t.Fatal("fixture decode")
		}
		var walk func(any, string)
		walk = func(value any, p string) {
			switch x := value.(type) {
			case map[string]any:
				for key, plant := range plants {
					x[key] = plant
					modified, _ := json.Marshal(raw)
					err := v.Validate(bytes.NewReader(modified), "json")
					var typed *InputError
					if !errors.As(err, &typed) || typed.Code != "invalid-input" || strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "192.0.2.1") {
						t.Fatalf("%T %s: private field accepted or echoed: %v", record, p, err)
					}
					delete(x, key)
				}
				for key, child := range x {
					walk(child, p+"/"+key)
				}
			case []any:
				for i, child := range x {
					walk(child, p+"/"+strconv.Itoa(i))
				}
			}
		}
		walk(raw, "")
	}
}

func TestPublicRejectsUnregisteredIdentifierValues(t *testing.T) {
	v := testPublicValidator(t)
	for _, name := range []string{"app", "concrete-route", "query", "fragment", "page-type", "asset", "owner", "source", "untracked-source", "dependency", "policy", "budget-scope", "budget-metric", "timing-scope", "timing-unit", "field-route", "hub", "series", "pair"} {
		t.Run(name, func(t *testing.T) {
			records := testPopulatedPublicRecords(t)
			var record any = records[0]
			r := record.(*Report)
			switch name {
			case "app":
				r.Rows[0].App = "private-app"
			case "concrete-route":
				r.Rows[0].RouteTemplate = "/counter/123/"
			case "query":
				r.Rows[0].RouteTemplate = "/counter/?q=private"
			case "fragment":
				r.Rows[0].RouteTemplate = "/counter/#private"
			case "page-type":
				r.Rows[0].PageType = "enhanced"
			case "asset":
				r.Assets[0].ID = "private/path"
			case "owner":
				r.Assets[0].Owner = "framework"
			case "source":
				r.Assets[0].ChangedSources = []string{"private/source.go"}
			case "untracked-source":
				r.Assets[0].ChangedSources = []string{"perf/budget/testdata/public-untracked.tmp"}
			case "dependency":
				r.Assets[0].Dependencies = []string{"private/dependency"}
			case "policy":
				r.Rows[0].Policies[0].Name = "private-policy"
			case "budget-scope":
				r.Acknowledgments[0].Scope = "type:enhanced"
			case "budget-metric":
				r.Acknowledgments[0].Metric = "private-metric"
			case "timing-scope":
				r.Acknowledgments[0] = Ack{Kind: "timing", Scope: "fixture|/private/|island|hard-cold|none|lcp", Metric: "lcp", Delta: 1, Issue: 1, Disposition: "timing", ReasonCode: "feature"}
			case "timing-unit":
				r.Acknowledgments[0] = Ack{Kind: "timing", Scope: "fixture|/counter/|island|hard-cold|none|js_heap_peak", Metric: "js_heap_peak", Delta: 1, Issue: 1, Disposition: "timing", ReasonCode: "feature"}
			case "field-route":
				record = records[3]
				record.(*FieldSnapshot).Vitals[0].RouteTemplate = "/private/"
			case "hub":
				record = records[3]
				record.(*FieldSnapshot).Hubs[0].Hub = "private-hub"
			case "series":
				record = records[1]
				record.(*SeriesPoint).Cell.App = "private-app"
			case "pair":
				record = records[2]
				record.(*PairReport).Cells[0].Cell.RouteTemplate = "/private/"
			}
			data, _ := json.Marshal(record)
			if err := v.Validate(bytes.NewReader(data), "json"); err == nil || strings.Contains(err.Error(), "private") {
				t.Fatalf("identifier accepted or echoed: %v", err)
			}
		})
	}
}

func TestPublicTrackedMembershipDoesNotUseExistence(t *testing.T) {
	root, err := inputRoot("go.mod", LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// A separate repository gives the test complete ownership of its files/index.
	dir := t.TempDir()
	for path, data := range map[string][]byte{"go.mod": []byte("module fixture\n"), "source.gsx": []byte("<p>fixture</p>"), "untracked.gsx": []byte("<p>untracked</p>")} {
		if err := os.WriteFile(filepath.Join(dir, path), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	catalog, err := os.ReadFile(filepath.Join(root, "perf/budget/testdata/catalog.v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	catalog = bytes.ReplaceAll(catalog, []byte("perf/wire/testdata/counter/page.gsx"), []byte("source.gsx"))
	if err := os.WriteFile(filepath.Join(dir, "catalog.json"), catalog, 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "--quiet"}, {"add", "go.mod", "source.gsx", "catalog.json"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if cmd.Run() != nil {
			t.Fatal("fixture Git command failed")
		}
	}
	v, err := NewPublicValidator(filepath.Join(dir, "catalog.json"), LoadOptions{RootDir: dir}, nil)
	if err != nil {
		t.Fatal(err)
	}
	r := publicTestReport(t)
	r.Assets[0].ChangedSources = []string{"source.gsx"}
	data, _ := json.Marshal(r)
	if err := v.Validate(bytes.NewReader(data), "json"); err != nil {
		t.Fatal(err)
	}
	r.Assets[0].ChangedSources = []string{"untracked.gsx"}
	data, _ = json.Marshal(r)
	if err := v.Validate(bytes.NewReader(data), "json"); err == nil {
		t.Fatal("untracked source accepted")
	}
	catalog = bytes.ReplaceAll(catalog, []byte("source.gsx"), []byte("untracked.gsx"))
	if err := os.WriteFile(filepath.Join(dir, "catalog.json"), catalog, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewPublicValidator(filepath.Join(dir, "catalog.json"), LoadOptions{RootDir: dir}, nil); err == nil {
		t.Fatal("untracked catalog route source accepted")
	}
}

func TestPublicJSONLAndRenderedMarkdown(t *testing.T) {
	v := testPublicValidator(t)
	r := testPopulatedPublicRecords(t)[0].(*Report)
	var jsonOut, markdown bytes.Buffer
	if err := v.WriteJSON(&jsonOut, *r); err != nil {
		t.Fatal(err)
	}
	if jsonOut.Bytes()[len(jsonOut.Bytes())-1] != '\n' || !bytes.HasPrefix(jsonOut.Bytes(), []byte(`{"acknowledgments":`)) {
		t.Fatal("JSON is not sorted with LF")
	}
	if err := v.WriteMarkdown(&markdown, *r); err != nil {
		t.Fatal(err)
	}
	if err := v.Validate(bytes.NewReader(markdown.Bytes()), "markdown"); err != nil {
		t.Fatal(err)
	}
	for _, modified := range [][]byte{
		append([]byte("[details](file:///private/path)\n"), markdown.Bytes()...),
		append(append([]byte{}, markdown.Bytes()...), []byte("\nhttps://example.invalid/private\n")...),
		bytes.Replace(markdown.Bytes(), []byte("| fixture |"), []byte("| private-app |"), 1),
		bytes.Replace(markdown.Bytes(), []byte("Mode:"), []byte("Private machine:"), 1),
	} {
		if err := v.Validate(bytes.NewReader(modified), "markdown"); err == nil {
			t.Fatal("modified Markdown accepted")
		}
	}
	var jsonl bytes.Buffer
	for _, record := range testPopulatedPublicRecords(t) {
		data, _ := json.Marshal(record)
		jsonl.Write(data)
		jsonl.WriteByte('\n')
	}
	if err := v.Validate(&jsonl, "jsonl"); err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{"", "\n", jsonOut.String() + "\n", jsonOut.String() + `{"hostname":"example.invalid"}`, strings.Repeat(" ", maxInputBytes+1)} {
		if err := v.Validate(strings.NewReader(input), "jsonl"); err == nil {
			t.Fatal("invalid JSONL accepted")
		}
	}
	if err := v.Validate(strings.NewReader("{}"), "xml"); err == nil {
		t.Fatal("unknown format accepted")
	}
}

type publicFailedReader struct{}

func (publicFailedReader) Read([]byte) (int, error) { return 0, errors.New("private reader error") }

type publicFailedWriter struct{}

func (publicFailedWriter) Write([]byte) (int, error) { return 0, errors.New("private writer error") }

func TestPublicWritersValidateBeforeOutputAndFieldRead(t *testing.T) {
	r := publicTestReport(t)
	var out bytes.Buffer
	if err := WriteJSON(&out, *r); err != nil {
		t.Fatal(err)
	}
	if err := ValidatePublic(bytes.NewReader(out.Bytes()), "json"); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := WriteMarkdown(&out, *r); err != nil {
		t.Fatal(err)
	}
	r.Assets[0].ChangedSources = []string{"private/file.go"}
	out.Reset()
	for _, write := range []func(io.Writer, Report) error{WriteJSON, WriteMarkdown} {
		if err := write(&out, *r); err == nil || out.Len() != 0 {
			t.Fatal("writer published invalid data")
		}
	}
	r = publicTestReport(t)
	var typed *InputError
	if err := WriteJSON(publicFailedWriter{}, *r); !errors.As(err, &typed) || typed.Code != "write-failed" || strings.Contains(err.Error(), "private") {
		t.Fatal("writer failure leaked or lost")
	}
	if err := ValidatePublic(publicFailedReader{}, "json"); err == nil || strings.Contains(err.Error(), "private") {
		t.Fatal("reader error leaked")
	}
	field := publicTestRecords(t)[3].(*FieldSnapshot)
	data, _ := json.Marshal(field)
	decoded, err := ReadField(bytes.NewReader(data))
	if err != nil || !reflect.DeepEqual(decoded, field) {
		t.Fatal("field read", err)
	}
	data, _ = json.Marshal(publicTestRecords(t)[4])
	if _, err := ReadField(bytes.NewReader(data)); err == nil {
		t.Fatal("non-field root accepted")
	}
}
