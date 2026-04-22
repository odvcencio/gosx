package perf

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
)

// BudgetFile declares reusable performance profiles and optional route
// mappings for reports produced by gosx perf --json.
type BudgetFile struct {
	DefaultProfile string                   `json:"defaultProfile,omitempty"`
	Profiles       map[string]BudgetProfile `json:"profiles"`
	Routes         []BudgetRoute            `json:"routes,omitempty"`
}

// BudgetProfile is a named set of perf assertions.
type BudgetProfile struct {
	Description string   `json:"description,omitempty"`
	Assertions  []string `json:"assertions"`
}

// BudgetRoute applies a profile and optional extra assertions to one route.
// URL may be a full URL, an absolute path, or "*" as a catch-all.
type BudgetRoute struct {
	URL        string   `json:"url"`
	Profile    string   `json:"profile,omitempty"`
	Assertions []string `json:"assertions,omitempty"`
}

// BudgetCheckResult is the top-level result of evaluating a budget file.
type BudgetCheckResult struct {
	Passed bool               `json:"passed"`
	Pages  []BudgetPageResult `json:"pages"`
}

// BudgetPageResult contains all assertion outcomes for one page.
type BudgetPageResult struct {
	URL        string                  `json:"url"`
	Profile    string                  `json:"profile,omitempty"`
	Assertions []BudgetAssertionResult `json:"assertions"`
	Passed     bool                    `json:"passed"`
}

// BudgetAssertionResult is one evaluated assertion from a budget profile.
type BudgetAssertionResult struct {
	Expression string  `json:"expression"`
	Metric     string  `json:"metric"`
	Op         string  `json:"op"`
	Value      float64 `json:"value"`
	Actual     float64 `json:"actual"`
	Passed     bool    `json:"passed"`
	Found      bool    `json:"found"`
}

// LoadBudgetFile reads a JSON perf budget file.
func LoadBudgetFile(path string) (*BudgetFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var budget BudgetFile
	if err := json.Unmarshal(data, &budget); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &budget, nil
}

// EvaluateBudget evaluates a report against a budget file. If forceProfile is
// non-empty, that profile is applied to every page and route mappings are
// ignored.
func EvaluateBudget(report *Report, budget *BudgetFile, forceProfile string) (BudgetCheckResult, error) {
	if budget == nil {
		return BudgetCheckResult{}, fmt.Errorf("nil budget")
	}
	if len(budget.Profiles) == 0 {
		return BudgetCheckResult{}, fmt.Errorf("budget has no profiles")
	}
	pages := reportPages(report)
	if len(pages) == 0 {
		return BudgetCheckResult{}, fmt.Errorf("report has no pages")
	}

	result := BudgetCheckResult{Passed: true}
	for i := range pages {
		page := pages[i]
		profileName, expressions, err := budgetExpressionsForPage(budget, page.URL, forceProfile)
		if err != nil {
			return BudgetCheckResult{}, err
		}
		pageResult := evalBudgetPage(page, profileName, expressions)
		if !pageResult.Passed {
			result.Passed = false
		}
		result.Pages = append(result.Pages, pageResult)
	}
	return result, nil
}

// FormatBudgetResult renders a human-readable budget gate report.
func FormatBudgetResult(result BudgetCheckResult) string {
	var b strings.Builder
	status := "passed"
	if !result.Passed {
		status = "failed"
	}
	b.WriteString("gosx perf budget - " + status + "\n")
	for _, page := range result.Pages {
		pageStatus := "ok"
		if !page.Passed {
			pageStatus = "fail"
		}
		profile := page.Profile
		if profile == "" {
			profile = "custom"
		}
		b.WriteString(fmt.Sprintf("\n  %s  profile=%s  %s\n", page.URL, profile, pageStatus))
		for _, assertion := range page.Assertions {
			mark := "ok"
			if !assertion.Passed {
				mark = "fail"
			}
			if !assertion.Found {
				b.WriteString(fmt.Sprintf("    %-4s %-28s metric not found\n", mark, assertion.Expression))
				continue
			}
			b.WriteString(fmt.Sprintf("    %-4s %-28s actual %.2f\n", mark, assertion.Expression, assertion.Actual))
		}
	}
	return b.String()
}

func reportPages(report *Report) []PageReport {
	if report == nil {
		return nil
	}
	if len(report.Pages) > 0 {
		return report.Pages
	}
	if report.PageReport.URL != "" {
		return []PageReport{report.PageReport}
	}
	return nil
}

func budgetExpressionsForPage(budget *BudgetFile, pageURL string, forceProfile string) (string, []string, error) {
	if forceProfile != "" {
		profile, ok := budget.Profiles[forceProfile]
		if !ok {
			return "", nil, fmt.Errorf("unknown budget profile %q", forceProfile)
		}
		return forceProfile, append([]string(nil), profile.Assertions...), nil
	}

	if route, ok := matchBudgetRoute(budget.Routes, pageURL); ok {
		return expressionsForRoute(budget, route)
	}
	if budget.DefaultProfile == "" {
		return "", nil, fmt.Errorf("no budget route matched %q and no defaultProfile is set", pageURL)
	}
	profile, ok := budget.Profiles[budget.DefaultProfile]
	if !ok {
		return "", nil, fmt.Errorf("unknown defaultProfile %q", budget.DefaultProfile)
	}
	return budget.DefaultProfile, append([]string(nil), profile.Assertions...), nil
}

func expressionsForRoute(budget *BudgetFile, route BudgetRoute) (string, []string, error) {
	var expressions []string
	if route.Profile != "" {
		profile, ok := budget.Profiles[route.Profile]
		if !ok {
			return "", nil, fmt.Errorf("unknown budget profile %q for route %q", route.Profile, route.URL)
		}
		expressions = append(expressions, profile.Assertions...)
	}
	expressions = append(expressions, route.Assertions...)
	if len(expressions) == 0 {
		return route.Profile, nil, fmt.Errorf("route %q has no profile or assertions", route.URL)
	}
	return route.Profile, append([]string(nil), expressions...), nil
}

func matchBudgetRoute(routes []BudgetRoute, pageURL string) (BudgetRoute, bool) {
	for _, route := range routes {
		if route.URL == "*" || sameURLOrPath(route.URL, pageURL) {
			return route, true
		}
	}
	return BudgetRoute{}, false
}

func sameURLOrPath(pattern, raw string) bool {
	if pattern == raw {
		return true
	}
	patternURL, patternErr := url.Parse(pattern)
	rawURL, rawErr := url.Parse(raw)
	if patternErr == nil && rawErr == nil && patternURL.IsAbs() {
		if !rawURL.IsAbs() {
			return false
		}
		return strings.EqualFold(patternURL.Scheme, rawURL.Scheme) &&
			strings.EqualFold(patternURL.Host, rawURL.Host) &&
			samePath(patternURL.Path, rawURL.Path)
	}
	patternPath := urlPath(pattern)
	rawPath := urlPath(raw)
	if patternPath == "" || rawPath == "" {
		return false
	}
	return samePath(patternPath, rawPath)
}

func urlPath(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if u.IsAbs() && u.Path == "" {
		return "/"
	}
	if u.Path == "" {
		return raw
	}
	return u.Path
}

func samePath(a, b string) bool {
	return strings.TrimRight(a, "/") == strings.TrimRight(b, "/")
}

func evalBudgetPage(page PageReport, profile string, expressions []string) BudgetPageResult {
	result := BudgetPageResult{
		URL:     page.URL,
		Profile: profile,
		Passed:  true,
	}
	for _, expr := range expressions {
		assertion, err := ParseAssertion(expr)
		if err != nil {
			result.Assertions = append(result.Assertions, BudgetAssertionResult{
				Expression: expr,
				Passed:     false,
				Found:      false,
			})
			result.Passed = false
			continue
		}
		actual, ok := ResolvePageMetric(assertion.Metric, &page)
		passed := ok && compare(actual, assertion.Op, assertion.Value)
		if !passed {
			result.Passed = false
		}
		result.Assertions = append(result.Assertions, BudgetAssertionResult{
			Expression: expr,
			Metric:     assertion.Metric,
			Op:         assertion.Op,
			Value:      assertion.Value,
			Actual:     actual,
			Passed:     passed,
			Found:      ok,
		})
	}
	return result
}

func coverageUnusedKB(entries []CoverageEntry) float64 {
	total := 0
	for _, e := range entries {
		total += e.UnusedBytes
	}
	return float64(total) / 1024
}
