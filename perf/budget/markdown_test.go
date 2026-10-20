package budget

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestMarkdownReviewColumnsAndCompleteRecord(t *testing.T) {
	r := publicTestReport(t)
	base, delta := int64(1536), int64(512)
	r.Rows[0].BaseBytes, r.Rows[0].DeltaBytes = &base, &delta
	r.Rows[0].Status, r.Rows[0].ReasonCode = "fail", "framework-share"
	r.Assets[0].BaseSizes = &SizeTriple{Raw: 100, Gzip: 80, Brotli: 40}
	r.Assets[0].Phase = "dormant"
	r.ExceptionIDs = []string{"EX-2026-001"}
	expires := "2026-01-30"
	r.Acknowledgments = []Ack{{Kind: "budget", Scope: "asset:app/fixture/counter", Metric: "brotli", Delta: 1024, Issue: 7, Disposition: "temporary", Expires: &expires, ReasonCode: "feature"}}
	r.Acknowledgments = append(r.Acknowledgments, Ack{Kind: "timing", Scope: "fixture|/counter/|island|hard-cold|none|lcp", Metric: "lcp", Delta: 1, Issue: 8, Disposition: "timing", ReasonCode: "feature"})
	r.Violations = []CountReason{{ReasonCode: "framework-share", Count: 1}}
	var rendered bytes.Buffer
	v := testPublicValidator(t)
	if err := v.WriteMarkdown(&rendered, *r); err != nil {
		t.Fatal(err)
	}
	const table = "| App | Route template | Type/scenario | Base normalized B | Head normalized B | Delta B | Actual wire B | N | Framework/F | App remaining | Headroom | Status |\n| --- | --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |\n| fixture | /counter/ | island/hard-cold/none | 1536 | 2048 | 512 | 2048 | 4096 | 1024/2048 | 1024 | 2048 | fail (framework-share) |\n"
	for _, expected := range []string{table,
		"| Logical asset | Raw/gzip/Brotli base→head | Delta B | Phase | Changed tracked sources |",
		"| app/fixture/counter | 100/80/40→128/64/48 | 28/-16/8 | dormant | perf/wire/testdata/counter/page.gsx |",
		"| fixture | /counter/ | island | assets-compressed | true |",
		"| EX-2026-001 | admitted |",
		"| budget | asset:app/fixture/counter | brotli | 1024 | #7 | temporary | 2026-01-30 |",
		"| timing | fixture\\|/counter/\\|island\\|hard-cold\\|none\\|lcp | lcp | 1 | #8 | timing | none |",
		"Coverage: routes 1/1; assets 1/1; reachability `known`.",
		"Violation `framework-share`: 1.",
		"canonical `true`; runner `cpu-linux`; transport `local-h1`.",
	} {
		if !strings.Contains(rendered.String(), expected) {
			t.Fatalf("missing review information: %s", expected)
		}
	}
	if err := v.Validate(bytes.NewReader(rendered.Bytes()), "markdown"); err != nil {
		t.Fatal(err)
	}
	start := bytes.Index(rendered.Bytes(), []byte("```json\n")) + len("```json\n")
	end := bytes.LastIndex(rendered.Bytes(), []byte("```\n"))
	record, err := DecodeRecord(bytes.NewReader(rendered.Bytes()[start:end]))
	if err != nil || !reflect.DeepEqual(record, r) {
		t.Fatalf("backing record lost information: %v", err)
	}
	changed := bytes.Replace(rendered.Bytes(), []byte("| 1536 | 2048 | 512 |"), []byte("| 1536 | 2049 | 512 |"), 1)
	if err := v.Validate(bytes.NewReader(changed), "markdown"); err == nil {
		t.Fatal("altered displayed bytes accepted")
	}
}

func TestMarkdownUnavailableBaseAndIntegerBoundaries(t *testing.T) {
	r := publicTestReport(t)
	const maxBytes int64 = 9007199254740991
	r.Assets[0].Raw = maxBytes
	r.Assets[0].BaseSizes = &SizeTriple{Raw: maxBytes, Gzip: maxBytes, Brotli: maxBytes}
	var out bytes.Buffer
	if err := testPublicValidator(t).WriteMarkdown(&out, *r); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"base `unavailable`", "| unavailable | 2048 | unavailable |",
		"0/-9007199254740927/-9007199254740943",
	} {
		if !strings.Contains(out.String(), expected) {
			t.Fatalf("missing exact integer or unavailable marker: %s", expected)
		}
	}
	r.Assets[0].BaseSizes = nil
	out.Reset()
	if err := testPublicValidator(t).WriteMarkdown(&out, *r); err != nil || !strings.Contains(out.String(), "| unavailable→9007199254740991/64/48 | unavailable |") {
		t.Fatalf("asset baseline was fabricated: %v", err)
	}
}

func TestMarkdownKeepsFailingRowsAndDoesNotMutateInputs(t *testing.T) {
	r := publicTestReport(t)
	r.Rows[0].Scenario = "hard-warm"
	cold := r.Rows[0]
	cold.Scenario, cold.Status, cold.ReasonCode = "hard-cold", "fail", "allocation"
	r.Rows = append(r.Rows, cold)
	r.Assets[0].ChangedSources = []string{"perf/wire/testdata/counter/page.gsx", "perf/budget/public.go"}
	framework := r.Assets[0]
	framework.ID, framework.Owner, framework.Kind = "framework/island-core", "framework", "wasm"
	framework.Dependencies, framework.Phase = []string{}, "after-ready"
	r.Assets = append(r.Assets, framework)
	r.ExceptionIDs = []string{"EX-2026-002", "EX-2026-001"}
	before, _ := json.Marshal(r)
	var out bytes.Buffer
	if err := testPublicValidator(t).WriteMarkdown(&out, *r); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if !strings.Contains(text, "fail (allocation)") || strings.Count(text, "| island/") != 2 || strings.Index(text, "island/hard-cold") > strings.Index(text, "island/hard-warm") {
		t.Fatal("failing row collapsed or row order unstable")
	}
	if !strings.Contains(text, "| after-ready | perf/budget/public.go, perf/wire/testdata/counter/page.gsx |") || strings.Index(text, "EX-2026-001 | admitted") > strings.Index(text, "EX-2026-002 | admitted") {
		t.Fatal("phase or sorted source/exception information missing")
	}
	after, _ := json.Marshal(r)
	if !bytes.Equal(before, after) {
		t.Fatal("rendering mutated caller slices")
	}
}

func TestMarkdownRejectsPrivateRecordBeforeWriting(t *testing.T) {
	v := testPublicValidator(t)
	for _, mutate := range []func(*Report){
		func(r *Report) { r.Rows[0].RouteTemplate = "/counter/?secret=1" },
		func(r *Report) { r.Assets[0].ChangedSources = []string{"unregistered/private.go"} },
		func(r *Report) { r.Info.Runner = "unregistered-runner" },
		func(r *Report) { r.Rows[0].Policies[0].Name = "private-policy" },
	} {
		r := publicTestReport(t)
		mutate(r)
		var out bytes.Buffer
		if err := v.WriteMarkdown(&out, *r); err == nil || out.Len() != 0 {
			t.Fatalf("invalid record emitted bytes: %v", err)
		}
	}
}
