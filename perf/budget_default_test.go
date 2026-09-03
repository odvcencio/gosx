package perf

import (
	"path/filepath"
	"testing"
)

func TestDefaultBudgetFileProfilesResolveAndAssertionsEvaluate(t *testing.T) {
	budget, err := LoadBudgetFile(filepath.Join("budgets", "default.json"))
	if err != nil {
		t.Fatalf("LoadBudgetFile: %v", err)
	}
	if budget.DefaultProfile == "" {
		t.Fatal("default budget has no defaultProfile")
	}
	if _, ok := budget.Profiles[budget.DefaultProfile]; !ok {
		t.Fatalf("defaultProfile %q is not configured", budget.DefaultProfile)
	}

	neutralPage := func(url string) PageReport {
		return PageReport{
			URL:              url,
			CoverageCaptured: true,
			Scene: &SceneMetric{
				Presentation: &PresentationMetric{
					TelemetrySeries: TelemetrySeries{Stats: FrameStats{Count: 1}},
				},
				GPU: &SceneGPUTelemetry{
					Total: &TelemetrySeries{Stats: FrameStats{Count: 1}},
				},
			},
		}
	}

	for name, profile := range budget.Profiles {
		t.Run("profile/"+name, func(t *testing.T) {
			if len(profile.Assertions) == 0 {
				t.Fatalf("profile %q has no assertions", name)
			}
			for _, expression := range profile.Assertions {
				if _, err := ParseAssertion(expression); err != nil {
					t.Errorf("ParseAssertion(%q): %v", expression, err)
				}
			}

			result, err := EvaluateBudget(
				&Report{PageReport: neutralPage("https://example.test/")},
				budget,
				name,
			)
			if err != nil {
				t.Fatalf("EvaluateBudget profile %q: %v", name, err)
			}
			if !result.Passed {
				t.Fatalf("neutral report failed profile %q: %+v", name, result)
			}
			for _, assertion := range result.Pages[0].Assertions {
				if !assertion.Found {
					t.Errorf("profile %q metric %q was not evaluated", name, assertion.Metric)
				}
			}
		})
	}

	defaultResult, err := EvaluateBudget(
		&Report{PageReport: neutralPage("https://example.test/__default-budget-contract__")},
		budget,
		"",
	)
	if err != nil {
		t.Fatalf("EvaluateBudget default profile: %v", err)
	}
	if got := defaultResult.Pages[0].Profile; got != budget.DefaultProfile {
		t.Fatalf("unmatched route resolved profile %q, want default %q", got, budget.DefaultProfile)
	}
	if !defaultResult.Passed {
		t.Fatalf("neutral report failed default profile %q: %+v", budget.DefaultProfile, defaultResult)
	}

	for _, route := range budget.Routes {
		t.Run("route/"+route.URL, func(t *testing.T) {
			for _, expression := range route.Assertions {
				if _, err := ParseAssertion(expression); err != nil {
					t.Errorf("ParseAssertion(%q): %v", expression, err)
				}
			}

			pageURL := "https://example.test" + route.URL
			result, err := EvaluateBudget(&Report{PageReport: neutralPage(pageURL)}, budget, "")
			if err != nil {
				t.Fatalf("EvaluateBudget route %q: %v", route.URL, err)
			}
			if got := result.Pages[0].Profile; got != route.Profile {
				t.Fatalf("route %q resolved profile %q, want %q", route.URL, got, route.Profile)
			}
			if !result.Passed {
				t.Fatalf("neutral report failed route %q: %+v", route.URL, result)
			}
			for _, assertion := range result.Pages[0].Assertions {
				if !assertion.Found {
					t.Errorf("route %q metric %q was not evaluated", route.URL, assertion.Metric)
				}
			}
		})
	}
}
