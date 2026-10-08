package budget

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

const testBudgetFooter = "Perf-Budget: scope=asset:framework/js/core.js metric=brotli delta=+2000B disposition=permanent issue=#7; because=Support the selected feature"
const testTimingFooter = "Perf-Timing: cell=fixture|/counter/|island|hard-cold|none|island_interactive delta=+12ms issue=#7; because=Explain the interaction change"

func TestTrailerCorpus(t *testing.T) {
	data, err := os.ReadFile("testdata/trailers.v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name, Text string
		Entries    int
		Error      bool
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, test := range cases {
		t.Run(test.Name, func(t *testing.T) {
			got, err := ParseTrailers(test.Text)
			if (err != nil) != test.Error || err == nil && len(got.Entries) != test.Entries {
				t.Fatal("corpus result differs", got, err)
			}
		})
	}
}

func TestTrailerTerminalFencesCRLFAndOtherGitTrailers(t *testing.T) {
	for _, fence := range []string{strings.Repeat(string(rune(96)), 3), "~~~", strings.Repeat(string(rune(96)), 4)} {
		for _, text := range []string{"Example\n\n" + fence + "text\n\n" + testBudgetFooter, "Example\n\n" + fence + "text\n" + testBudgetFooter + "\n" + fence} {
			got, err := ParseTrailers(text)
			if err != nil || len(got.Entries) != 0 {
				t.Fatal("fenced example granted authorization", got, err)
			}
		}
		// A shorter closing marker must not close a longer fence.
		if len(fence) == 4 {
			got, err := ParseTrailers("Example\n\n" + fence + "\n" + fence[:3] + "\n\n" + testBudgetFooter)
			if err != nil || len(got.Entries) != 0 {
				t.Fatal("short fence closer granted authorization")
			}
		}
		got, err := ParseTrailers("Example\n\n" + fence + "text\nexample\n" + fence + "\n\n" + testBudgetFooter)
		if err != nil || len(got.Entries) != 1 {
			t.Fatal("closed fence hid terminal footer", got, err)
		}
	}
	text := "Improve loading\n\nRefs: #9\n" + testBudgetFooter + "\n" + testTimingFooter + "\n\n"
	lf, err := ParseTrailers(text)
	crlf, crErr := ParseTrailers(strings.ReplaceAll(text, "\n", "\r\n"))
	if err != nil || crErr != nil || !reflect.DeepEqual(lf, crlf) || len(lf.Entries) != 2 {
		t.Fatal("CRLF or unrelated trailers changed result", lf, err, crErr)
	}
	got, err := ParseTrailers("Improve loading\n\n" + testBudgetFooter + "\nOrdinary trailing prose")
	if err != nil || len(got.Entries) != 0 {
		t.Fatal("nonterminal footer granted authorization")
	}
}

func TestTrailerRejectsMalformedKeysUnitsScopesAndReasons(t *testing.T) {
	for _, test := range []struct{ name, old, replacement string }{
		{"zero", "+2000B", "+0B"}, {"leading-zero", "+2000B", "+02000B"}, {"fraction", "+2000B", "+1.5B"},
		{"negative", "+2000B", "-2000B"}, {"overflow", "+2000B", "+9223372036854775808B"}, {"unsafe-json-integer", "+2000B", "+9007199254740992B"},
		{"issue-zero", "issue=#7", "issue=#0"}, {"issue-leading-zero", "issue=#7", "issue=#07"}, {"issue-overflow", "issue=#7", "issue=#9223372036854775808"},
		{"metric", "metric=brotli", "metric=gpuInitialBytes"}, {"policy", "metric=brotli", "metric=policy"},
		{"unknown-key", "issue=#7", "other=#7"}, {"missing-key", " issue=#7", ""},
		{"asset-traversal", "framework/js/core.js", "framework/../core.js"}, {"absolute-path", "framework/js/core.js", "/core.js"},
		{"scope-unicode", "framework/js/core.js", "framework/js/café.js"}, {"scope-query", "asset:framework/js/core.js", "route:fixture:/counter/?q=1"},
		{"scope-fragment", "asset:framework/js/core.js", "route:fixture:/counter/#anchor"}, {"type-backend", "asset:framework/js/core.js", "type:static-webgpu"},
		{"temporary-missing-date", "disposition=permanent", "disposition=temporary"}, {"permanent-date", "issue=#7;", "issue=#7 expires=2026-10-30;"},
		{"release-tag", "issue=#7;", "issue=#7 expires=v0.42;"}, {"short-reason", "Support the selected feature", "Too short"},
		{"long-reason", "Support the selected feature", strings.Repeat("x", 241)}, {"semicolon", "Support the selected feature", "Support; the selected feature"},
		{"control", "Support the selected feature", "Support\tselected feature"}, {"case", "Perf-Budget:", "PERF-BUDGET:"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseTrailers("Improve loading\n\n" + strings.Replace(testBudgetFooter, test.old, test.replacement, 1))
			var typed *InputError
			if !errors.As(err, &typed) || typed.Code != "invalid-trailer" || typed.Reference != "trailers" || typed.Pointer != "/lines/2" {
				t.Fatal("malformed footer accepted or error lost fixed location", err)
			}
		})
	}
	valid := strings.Replace(testBudgetFooter, "Support the selected feature", "Réduire le travail au démarrage", 1)
	if got, err := ParseTrailers("Improve loading\n\n" + valid); err != nil || len(got.Entries) != 1 {
		t.Fatal("printable UTF-8 reason rejected", err)
	}
	for _, text := range []string{"Improve loading\n\n" + testBudgetFooter + "\n" + testBudgetFooter, "Improve loading\n\n " + testBudgetFooter, string([]byte{0xff}), strings.Repeat("x", maxInputBytes+1)} {
		if _, err := ParseTrailers(text); err == nil {
			t.Fatal("duplicate, indentation or bounded input accepted")
		}
	}
}

func TestTrailerTimingRequiresAllSixExactComponentsAndMilliseconds(t *testing.T) {
	for _, replacement := range []string{
		"fixture|/counter/|island|hard-cold|island_interactive",
		"fixture|/counter/?q=1|island|hard-cold|none|island_interactive",
		"fixture|/counter/|island|other|none|island_interactive",
		"fixture|/counter/|island|hard-cold|webgpu|island_interactive",
		"fixture|/counter/|island|hard-cold|none|js_heap_peak",
		"fixture|/counter/|island|hard-cold|none|long_tasks",
		"fixture|/counter/|island|hard-cold|none|unknown",
	} {
		text := strings.Replace(testTimingFooter, "fixture|/counter/|island|hard-cold|none|island_interactive", replacement, 1)
		if _, err := ParseTrailers("Improve loading\n\n" + text); err == nil {
			t.Fatal("malformed timing cell accepted")
		}
	}
	if _, err := ParseTrailers("Improve loading\n\n" + strings.Replace(testTimingFooter, "+12ms", "+1.5ms", 1)); err == nil {
		t.Fatal("fractional timing delta accepted")
	}
	valid := strings.Replace(testTimingFooter, "island|hard-cold|none|island_interactive", "scene3d/js-webgpu|hard-cold|webgpu|fif", 1)
	if got, err := ParseTrailers("Improve loading\n\n" + valid); err != nil || len(got.Entries) != 1 {
		t.Fatal("valid scene timing footer rejected", err)
	}
}
