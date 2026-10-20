package budget

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"
)

func testAcknowledgeOptions(t *testing.T, footer string) AcknowledgeOptions {
	t.Helper()
	parsed, err := ParseTrailers("Improve loading\n\n" + footer)
	if err != nil {
		t.Fatal(err)
	}
	return AcknowledgeOptions{Trailers: parsed, Now: time.Date(2026, 10, 8, 23, 0, 0, 0, time.UTC),
		File: File{PageTypes: map[string]PageType{"island": {Allocation: Derivation{TotalBytes: 50000, FrameworkBytes: 10000}}},
			Routes: []RouteRule{{App: "fixture", RouteTemplate: "/counter/", PageTypes: []string{"island"}}, {App: "fixture", RouteTemplate: "/posts/{id}/", PageTypes: []string{"island"}}}},
		Assets: []AssetReport{{ID: "framework/js/core.js"}}}
}
func TestAcknowledgeExactDeltaAndRegisteredScopes(t *testing.T) {
	opts := testAcknowledgeOptions(t, testBudgetFooter)
	opts.Required = []GrowthRequirement{{"budget", "asset:framework/js/core.js", "brotli", 2000}}
	before, _ := json.Marshal(opts.File)
	out, err := ValidateAcknowledgments(opts)
	after, _ := json.Marshal(opts.File)
	if err != nil || len(out) != 1 || !reflect.DeepEqual(before, after) || out[0].ReasonCode != "growth-ack" {
		t.Fatal("valid growth failed or changed a cap", out, err)
	}
	data, _ := json.Marshal(out)
	if strings.Contains(string(data), "Support the selected feature") {
		t.Fatal("free-form reason copied into public output")
	}
	for _, cause := range []string{"undersized", "wrong-metric", "wrong-asset", "blanket-route", "concrete-route", "unknown-type", "duplicate-required", "duplicate-provided", "zero-now", "native-invalid"} {
		t.Run(cause, func(t *testing.T) {
			opts := testAcknowledgeOptions(t, testBudgetFooter)
			opts.Required = []GrowthRequirement{{"budget", "asset:framework/js/core.js", "brotli", 2000}}
			switch cause {
			case "undersized":
				opts.Required[0].Delta = 2001
			case "wrong-metric":
				opts.Required[0].Metric = "raw"
			case "wrong-asset":
				opts.Trailers.Entries[0].Scope = "asset:framework/js/other.js"
			case "blanket-route":
				opts.Trailers.Entries[0].Scope = "route:fixture:/counter/"
			case "concrete-route":
				opts.Trailers.Entries[0].Scope = "route:fixture:/posts/123/"
			case "unknown-type":
				opts.Trailers.Entries[0].Scope = "type:enhanced"
			case "duplicate-required":
				opts.Required = append(opts.Required, opts.Required[0])
			case "duplicate-provided":
				opts.Trailers.Entries = append(opts.Trailers.Entries, opts.Trailers.Entries[0])
			case "zero-now":
				opts.Now = time.Time{}
			case "native-invalid":
				opts.Trailers.Entries[0].Disposition = "permanent"
				opts.Trailers.Entries[0].Delta = 0
			}
			if _, err := ValidateAcknowledgments(opts); err == nil {
				t.Fatal("invalid or blanket acknowledgment accepted")
			}
		})
	}
	for _, scope := range []string{"route:fixture:/posts/{id}/", "type:island"} {
		footer := strings.Replace(testBudgetFooter, "asset:framework/js/core.js", scope, 1)
		opts := testAcknowledgeOptions(t, footer)
		opts.Required = []GrowthRequirement{{"budget", scope, "brotli", 2000}}
		if _, err := ValidateAcknowledgments(opts); err != nil {
			t.Fatal("registered template/type rejected", err)
		}
	}
}

func TestAcknowledgeTemporaryUTCExpiryDoesNotBecomeAnAllowance(t *testing.T) {
	for _, date := range []string{"2026-10-08", "2026-10-09", "2026-11-07", "2026-11-08"} {
		footer := strings.Replace(testBudgetFooter, "disposition=permanent issue=#7;", "disposition=temporary issue=#7 expires="+date+";", 1)
		opts := testAcknowledgeOptions(t, footer)
		opts.Required = []GrowthRequirement{{"budget", "asset:framework/js/core.js", "brotli", 2000}}
		out, err := ValidateAcknowledgments(opts)
		valid := date == "2026-10-09" || date == "2026-11-07"
		if (err == nil) != valid {
			t.Fatal("UTC expiry admission differs", date, err)
		}
		if valid {
			*out[0].Expires = "changed"
			if *opts.Trailers.Entries[0].Expires != date || len(opts.File.Exceptions) != 0 || opts.File.PageTypes["island"].Allocation.FrameworkBytes != 10000 {
				t.Fatal("expiry output aliased input or footer added allowance")
			}
		}
	}
}

func TestAcknowledgeTimingCeilsHLAndUsesEveryCellComponent(t *testing.T) {
	cell := Cell{App: "fixture", RouteTemplate: "/counter/", PageType: "island", Scenario: "hard-cold", Backend: "none", Metric: "island_interactive", Unit: "ms"}
	requirement, err := TimingGrowth(cell, 11.01)
	if err != nil || requirement.Delta != 12 {
		t.Fatal("HL not rounded up", requirement, err)
	}
	opts := testAcknowledgeOptions(t, testTimingFooter)
	opts.Required = []GrowthRequirement{requirement}
	if _, err := ValidateAcknowledgments(opts); err != nil {
		t.Fatal("timing acknowledgment rejected", err)
	}
	parts := strings.Split(requirement.Scope, "|")
	for i, replacement := range []string{"other", "/posts/{id}/", "enhanced", "hard-warm", "webgpu", "lcp"} {
		changed := append([]string{}, parts...)
		changed[i] = replacement
		opts := testAcknowledgeOptions(t, testTimingFooter)
		wrong := requirement
		wrong.Scope = strings.Join(changed, "|")
		opts.Required = []GrowthRequirement{wrong}
		if _, err := ValidateAcknowledgments(opts); err == nil {
			t.Fatal("another timing cell authorized", i)
		}
	}
	for _, hl := range []float64{0, -1, math.NaN(), math.Inf(1), 9007199254740992} {
		if _, err := TimingGrowth(cell, hl); err == nil {
			t.Fatal("invalid timing growth accepted")
		}
	}
	cell.Unit = "B"
	if _, err := TimingGrowth(cell, 12); err == nil {
		t.Fatal("byte metric used a timing footer")
	}
}

func TestAcknowledgeTripwireAndMinimalSharedAssetGrowth(t *testing.T) {
	for _, test := range []struct{ allocation, want int64 }{{0, 1024}, {100000, 1024}, {102400, 1024}, {102401, 1025}, {math.MaxInt64, 92233720368547759}} {
		got, err := GrowthTripwire(test.allocation)
		if err != nil || got != test.want {
			t.Fatal("tripwire rounding or overflow differs", got, err)
		}
	}
	if _, err := GrowthTripwire(-1); err == nil {
		t.Fatal("negative allocation accepted")
	}
	base := Report{Assets: []AssetReport{{ID: "framework/js/core.js", Owner: "framework", Phase: "dormant", SHA256: strings.Repeat("a", 64), Raw: 5000, Gzip: 4000, Brotli: 3000}},
		Rows: []Row{{App: "fixture", RouteTemplate: "/counter/", PageType: "island", Scenario: "hard-cold", Backend: "none", NormalizedBytes: 50000, FrameworkBytes: 10000}}}
	head := base
	head.Assets = append([]AssetReport{}, base.Assets...)
	head.Rows = append([]Row{}, base.Rows...)
	head.Assets[0].Raw += 1024
	head.Assets[0].Gzip += 1025
	head.Assets[0].Brotli += 2000
	head.Rows[0].NormalizedBytes += 2000
	head.Rows[0].FrameworkBytes += 2000
	head.Rows[0].AllocationBytes = 100000
	head.Rows[0].FrameworkCeilingBytes = 100000
	// One physical shared chunk still needs one footer per crossed metric.
	head.Assets = append(head.Assets, head.Assets[0])
	required, err := RequiredByteGrowth(head, base, map[string]int64{"framework/js/core.js": 100000})
	want := []GrowthRequirement{{"budget", "asset:framework/js/core.js", "brotli", 2000}, {"budget", "asset:framework/js/core.js", "gzip", 1025},
		{"budget", "route:fixture:/counter/", "frameworkBytes", 2000}, {"budget", "route:fixture:/counter/", "totalBytes", 2000}}
	if err != nil || !reflect.DeepEqual(required, want) {
		t.Fatal("exact tripwire set or shared chunk deduplication differs", required, err)
	}
	otherBase, otherHead := base.Rows[0], head.Rows[0]
	otherBase.RouteTemplate, otherHead.RouteTemplate = "/other/", "/other/"
	base.Rows, head.Rows = append(base.Rows, otherBase), append(head.Rows, otherHead)
	shared, err := RequiredByteGrowth(head, base, map[string]int64{"framework/js/core.js": 100000})
	if err != nil || len(shared) != 6 {
		t.Fatal("shared chunk omitted an independent route tripwire", shared, err)
	}
	// Missing route provenance cannot fabricate a zero baseline.
	if _, err := RequiredByteGrowth(head, Report{}, nil); err == nil {
		t.Fatal("missing base route treated as zero")
	}
	head.Assets[1].Raw++
	if _, err := RequiredByteGrowth(head, base, nil); err == nil {
		t.Fatal("conflicting shared asset accepted")
	}
}
