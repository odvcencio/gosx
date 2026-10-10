package wire

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"m31labs.dev/gosx/internal/httpcache"
)

// BudgetSchema names the budget file format.
const BudgetSchema = "gosx.wire-budget/v1"

// Metric names used in budget limits.
const (
	MetricTotalWireBytes       = "totalWireBytes"
	MetricFrameworkJSWireBytes = "frameworkJsWireBytes"
	MetricHTMLWireBytes        = "htmlWireBytes"
	MetricJSWireBytes          = "jsWireBytes"
	MetricWASMWireBytes        = "wasmWireBytes"
	MetricCSSWireBytes         = "cssWireBytes"
	MetricImageWireBytes       = "imageWireBytes"
	MetricLazyJSWireBytes      = "lazyJsWireBytes"
	MetricRequests             = "requests"
	MetricInlineScriptBytes    = "inlineScriptBytes"
)

// Metrics lists every limit in report order.
var Metrics = []string{
	MetricTotalWireBytes,
	MetricFrameworkJSWireBytes,
	MetricHTMLWireBytes,
	MetricJSWireBytes,
	MetricWASMWireBytes,
	MetricCSSWireBytes,
	MetricImageWireBytes,
	MetricLazyJSWireBytes,
	MetricRequests,
	MetricInlineScriptBytes,
}

// Policy names. Each is a yes/no property of a route's responses.
const (
	// PolicyHTMLCompressed: the document is sent with br or gzip.
	PolicyHTMLCompressed = "html-compressed"
	// PolicyAssetsCompressed: every script, style, WASM and data response
	// over 1 KiB is sent with br or gzip.
	PolicyAssetsCompressed = "assets-compressed"
	// PolicyNoCookie: no response on the route sets a cookie.
	PolicyNoCookie = "no-cookie"
	// PolicyImmutableHashed: every response cached as immutable has a
	// content-hashed URL, so an upgrade can never serve stale bytes.
	PolicyImmutableHashed = "immutable-hashed"
	// PolicyRuntimeHashed: every framework script and WASM response has a
	// content-hashed URL and is cached as immutable.
	PolicyRuntimeHashed = "runtime-hashed"
	// PolicyNoInlineRuntime: the document carries no inline executable
	// script over 2 KiB, so the runtime is cached once rather than resent in
	// every page.
	PolicyNoInlineRuntime = "no-inline-runtime"
	// PolicyHTMLShareable: the document's Cache-Control does not forbid
	// shared caching (no private, no-store).
	PolicyHTMLShareable = "html-shareable"
)

// Policies lists every policy in report order.
var Policies = []string{
	PolicyHTMLCompressed,
	PolicyAssetsCompressed,
	PolicyNoCookie,
	PolicyImmutableHashed,
	PolicyRuntimeHashed,
	PolicyNoInlineRuntime,
	PolicyHTMLShareable,
}

// Budget is the committed per-route limit file.
type Budget struct {
	Schema    string               `json:"schema"`
	Note      string               `json:"note,omitempty"`
	Tolerance Tolerance            `json:"tolerance"`
	Apps      map[string]AppBudget `json:"apps"`
}

// Tolerance says how far below its limit a measurement may fall before the
// limit is stale and must be lowered.
type Tolerance struct {
	BytesPercent float64 `json:"bytesPercent"`
	BytesMin     int64   `json:"bytesMin"`
}

// AppBudget maps route paths to their budgets.
type AppBudget map[string]RouteBudget

// RouteBudget holds the limits and required policies for one route.
type RouteBudget struct {
	Limits  map[string]int64 `json:"limits"`
	Require []string         `json:"require"`
	// Raise names each limit or policy that this change loosens compared
	// with the base branch, with the reason. The ratchet check fails on any
	// loosening that is not named here.
	Raise map[string]string `json:"raise,omitempty"`
}

// ReadBudget loads a budget file.
func ReadBudget(path string) (Budget, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Budget{}, err
	}
	return ParseBudget(data)
}

// ParseBudget decodes a budget file.
func ParseBudget(data []byte) (Budget, error) {
	var b Budget
	if err := json.Unmarshal(data, &b); err != nil {
		return Budget{}, err
	}
	if b.Schema != BudgetSchema {
		return Budget{}, fmt.Errorf("budget schema %q, want %q", b.Schema, BudgetSchema)
	}
	for app, routes := range b.Apps {
		for route, rb := range routes {
			for m := range rb.Limits {
				if !contains(Metrics, m) {
					return Budget{}, fmt.Errorf("%s %s: unknown limit %q", app, route, m)
				}
			}
			for _, p := range rb.Require {
				if !contains(Policies, p) {
					return Budget{}, fmt.Errorf("%s %s: unknown policy %q", app, route, p)
				}
			}
		}
	}
	return b, nil
}

// Value returns the named metric of a measured route.
func (r Route) Value(metric string) int64 {
	switch metric {
	case MetricTotalWireBytes:
		return r.TotalWireBytes
	case MetricFrameworkJSWireBytes:
		return r.FrameworkJSWireBytes
	case MetricHTMLWireBytes:
		return r.WireBytes[KindDocument]
	case MetricJSWireBytes:
		return r.WireBytes[KindScript]
	case MetricWASMWireBytes:
		return r.WireBytes[KindWASM]
	case MetricCSSWireBytes:
		return r.WireBytes[KindStyle]
	case MetricLazyJSWireBytes:
		return r.LazyWireBytes
	case MetricImageWireBytes:
		return r.WireBytes[KindImage]
	case MetricRequests:
		return int64(r.Requests)
	case MetricInlineScriptBytes:
		return r.InlineScriptBytes
	}
	return 0
}

// PolicyResult is the outcome of one policy on one route.
type PolicyResult struct {
	Pass   bool
	Reason string
}

// EvaluatePolicies checks every policy against a measured route.
func (r Route) EvaluatePolicies() map[string]PolicyResult {
	out := map[string]PolicyResult{}
	compressed := func(enc string) bool { return enc == "br" || enc == "gzip" }

	if compressed(r.Document.ContentEncoding) {
		out[PolicyHTMLCompressed] = PolicyResult{Pass: true}
	} else {
		out[PolicyHTMLCompressed] = PolicyResult{Reason: fmt.Sprintf("document sent with encoding %q", r.Document.ContentEncoding)}
	}

	var bad []string
	for _, res := range r.Resources {
		if res.Kind == KindOther || res.Kind == KindRedirect || res.Kind == KindImage || res.Kind == KindFont || res.DecodedBytes <= 1024 {
			continue
		}
		if !compressed(res.ContentEncoding) {
			bad = append(bad, res.URL)
		}
	}
	out[PolicyAssetsCompressed] = result(bad, "sent uncompressed")

	bad = nil
	if r.Document.SetCookie {
		bad = append(bad, r.Document.URL)
	}
	for _, res := range r.Resources {
		if res.SetCookie {
			bad = append(bad, res.URL)
		}
	}
	out[PolicyNoCookie] = result(bad, "sets a cookie")

	bad = nil
	for _, res := range append([]Resource{r.Document}, r.Resources...) {
		if res.Immutable && !res.Hashed {
			bad = append(bad, res.URL)
		}
	}
	out[PolicyImmutableHashed] = result(bad, "cached immutable without a content hash in the URL")

	bad = nil
	for _, res := range r.Resources {
		if !(res.Framework || isFramework(pathOf(res.URL))) || (res.Kind != KindScript && res.Kind != KindWASM && res.Kind != KindLazyScript) {
			continue
		}
		if !res.Hashed || !res.Immutable {
			bad = append(bad, res.URL)
		}
	}
	out[PolicyRuntimeHashed] = result(bad, "framework asset without a hashed immutable URL")

	if r.InlineScriptMax > 2048 {
		out[PolicyNoInlineRuntime] = PolicyResult{Reason: fmt.Sprintf("largest inline script is %d bytes", r.InlineScriptMax)}
	} else {
		out[PolicyNoInlineRuntime] = PolicyResult{Pass: true}
	}

	cache, validCache := httpcache.ParseDirectives(r.Document.CacheControl)
	if !validCache || cache.Has("private") || cache.Has("no-store") {
		out[PolicyHTMLShareable] = PolicyResult{Reason: fmt.Sprintf("document Cache-Control %q", r.Document.CacheControl)}
	} else {
		out[PolicyHTMLShareable] = PolicyResult{Pass: true}
	}
	return out
}

func pathOf(raw string) string {
	if i := strings.IndexAny(raw, "?#"); i >= 0 {
		return raw[:i]
	}
	return raw
}

func result(bad []string, what string) PolicyResult {
	if len(bad) == 0 {
		return PolicyResult{Pass: true}
	}
	sort.Strings(bad)
	shown := bad
	if len(shown) > 3 {
		shown = append(append([]string{}, bad[:3]...), fmt.Sprintf("and %d more", len(bad)-3))
	}
	return PolicyResult{Reason: strings.Join(shown, ", ") + ": " + what}
}

// Finding is one gate failure.
type Finding struct {
	App     string `json:"app"`
	Route   string `json:"route"`
	Subject string `json:"subject"`
	Message string `json:"message"`
}

func (f Finding) String() string {
	return fmt.Sprintf("%s %s %s: %s", f.App, f.Route, f.Subject, f.Message)
}

// slack returns how far below the limit a measurement may fall before the
// limit counts as stale.
func (t Tolerance) slack(metric string, limit int64) int64 {
	if metric == MetricRequests {
		return 0
	}
	s := int64(float64(limit) * t.BytesPercent / 100)
	if s < t.BytesMin {
		s = t.BytesMin
	}
	return s
}

// headroom is the margin -write leaves above a measurement: half the stale
// slack, so a freshly written limit absorbs byte noise without being stale.
func (t Tolerance) headroom(metric string, measured int64) int64 {
	if metric == MetricRequests {
		return 0
	}
	return t.slack(metric, measured) / 2
}

// Check compares measured routes with the budget. It fails on a limit
// exceeded, a limit left stale after an improvement, a required policy that
// fails, a policy that now passes but is not yet required, and any route that
// is measured but not budgeted or budgeted but not measured.
func Check(b Budget, measured []Route) []Finding {
	var out []Finding
	seen := map[string]bool{}
	for _, r := range measured {
		key := r.App + " " + r.Route
		seen[key] = true
		rb, ok := b.Apps[r.App][r.Route]
		if !ok {
			out = append(out, Finding{r.App, r.Route, "route", "measured but has no budget; add it (make wire-gate-update)"})
			continue
		}
		for _, m := range Metrics {
			limit, ok := rb.Limits[m]
			if !ok {
				out = append(out, Finding{r.App, r.Route, m, "no limit; add it (make wire-gate-update)"})
				continue
			}
			v := r.Value(m)
			switch {
			case v > limit:
				out = append(out, Finding{r.App, r.Route, m, fmt.Sprintf("%d is over the limit %d (+%d)", v, limit, v-limit)})
			case limit-v > b.Tolerance.slack(m, limit):
				out = append(out, Finding{r.App, r.Route, m, fmt.Sprintf("%d is well under the limit %d; lower the limit to %d (make wire-gate-update)", v, limit, v+b.Tolerance.headroom(m, v))})
			}
		}
		results := r.EvaluatePolicies()
		for _, p := range Policies {
			res := results[p]
			required := contains(rb.Require, p)
			switch {
			case required && !res.Pass:
				out = append(out, Finding{r.App, r.Route, p, res.Reason})
			case !required && res.Pass:
				out = append(out, Finding{r.App, r.Route, p, "now passes; add it to require (make wire-gate-update)"})
			}
		}
	}
	for app, routes := range b.Apps {
		for route := range routes {
			if !seen[app+" "+route] {
				out = append(out, Finding{app, route, "route", "budgeted but not measured"})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].App != out[j].App {
			return out[i].App < out[j].App
		}
		return out[i].Route < out[j].Route
	})
	return out
}

// Update returns the budget tightened to the measurements: each limit is set
// to the measurement plus headroom when that is lower than the current limit
// (or when allowRaise is set), and every passing policy is added to require.
// Routes that are measured but not budgeted are added.
func Update(b Budget, measured []Route, allowRaise bool) Budget {
	if b.Apps == nil {
		b.Apps = map[string]AppBudget{}
	}
	for _, r := range measured {
		if b.Apps[r.App] == nil {
			b.Apps[r.App] = AppBudget{}
		}
		rb := b.Apps[r.App][r.Route]
		if rb.Limits == nil {
			rb.Limits = map[string]int64{}
		}
		for _, m := range Metrics {
			want := r.Value(m) + b.Tolerance.headroom(m, r.Value(m))
			cur, ok := rb.Limits[m]
			stale := ok && cur-r.Value(m) > b.Tolerance.slack(m, cur)
			if !ok || stale || (allowRaise && want > cur) {
				rb.Limits[m] = want
			}
		}
		results := r.EvaluatePolicies()
		for _, p := range Policies {
			if results[p].Pass && !contains(rb.Require, p) {
				rb.Require = append(rb.Require, p)
			}
		}
		sort.SliceStable(rb.Require, func(i, j int) bool { return indexOf(Policies, rb.Require[i]) < indexOf(Policies, rb.Require[j]) })
		b.Apps[r.App][r.Route] = rb
	}
	return b
}

// Ratchet compares a head budget with its base-branch version. Every limit
// that rose and every required policy that was dropped must be named in the
// route's raise map with a reason.
func Ratchet(base, head Budget) []Finding {
	var out []Finding
	for app, routes := range base.Apps {
		for route, br := range routes {
			hr, ok := head.Apps[app][route]
			if !ok {
				out = append(out, Finding{app, route, "route", "removed from the budget; name it in another route's raise map or keep it"})
				continue
			}
			for m, limit := range br.Limits {
				if hv, ok := hr.Limits[m]; (!ok || hv > limit) && strings.TrimSpace(hr.Raise[m]) == "" {
					out = append(out, Finding{app, route, m, fmt.Sprintf("limit raised from %d to %d without a reason in raise[%q]", limit, hv, m)})
				}
			}
			for _, p := range br.Require {
				if !contains(hr.Require, p) && strings.TrimSpace(hr.Raise[p]) == "" {
					out = append(out, Finding{app, route, p, fmt.Sprintf("policy dropped from require without a reason in raise[%q]", p)})
				}
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out
}

// Marshal renders a budget with stable key order.
func (b Budget) Marshal() ([]byte, error) {
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func contains(list []string, s string) bool { return indexOf(list, s) >= 0 }

func indexOf(list []string, s string) int {
	for i, v := range list {
		if v == s {
			return i
		}
	}
	return -1
}
