// Command wiregate measures GoSX routes over the network and checks them
// against perf/budgets/wire.json.
//
//	wiregate check  -budget FILE -app NAME=URL... [-report FILE] [-markdown FILE] [-compare FILE] [-write] [-allow-raise]
//	wiregate ratchet -base FILE -head FILE
//	wiregate table  -report FILE [-compare FILE]
//
// check exits 1 when a limit is exceeded or stale, a required policy fails, a
// passing policy is not yet required, or a route is missing on either side.
// -write rewrites the budget to the measurements instead of failing (limits
// only move down unless -allow-raise is set).
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"m31labs.dev/gosx/perf/wire"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "wiregate: %v\n", err)
		os.Exit(1)
	}
}

type multi []string

func (m *multi) String() string     { return strings.Join(*m, ",") }
func (m *multi) Set(v string) error { *m = append(*m, v); return nil }

// Report is the JSON measurement file.
type Report struct {
	Schema     string       `json:"schema"`
	MeasuredAt time.Time    `json:"measuredAt"`
	Routes     []wire.Route `json:"routes"`
}

const reportSchema = "gosx.wire-report/v1"

var errGate = errors.New("gate failed")

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: wiregate check|ratchet|table [flags]")
	}
	switch args[0] {
	case "check":
		return runCheck(args[1:], stdout, stderr)
	case "ratchet":
		return runRatchet(args[1:], stdout)
	case "table":
		return runTable(args[1:], stdout)
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runCheck(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	budgetPath := fs.String("budget", "perf/budgets/wire.json", "budget file")
	reportPath := fs.String("report", "", "write the measurements as JSON")
	markdownPath := fs.String("markdown", "", "write a Markdown table of the measurements")
	comparePath := fs.String("compare", "", "earlier report to show as the before column")
	write := fs.Bool("write", false, "rewrite the budget to the measurements instead of failing")
	allowRaise := fs.Bool("allow-raise", false, "with -write, also raise limits (the ratchet check then needs a reason)")
	timeout := fs.Duration("timeout", 2*time.Minute, "overall timeout")
	var apps, routes multi
	fs.Var(&apps, "app", "NAME=BASE_URL of a running app (repeatable)")
	fs.Var(&routes, "route", "NAME=/path to measure in addition to the budgeted routes (repeatable)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	budget, err := wire.ReadBudget(*budgetPath)
	if err != nil {
		return fmt.Errorf("read budget: %w", err)
	}
	bases := map[string]string{}
	for _, a := range apps {
		name, base, ok := strings.Cut(a, "=")
		if !ok || name == "" || base == "" {
			return fmt.Errorf("-app %q: want NAME=BASE_URL", a)
		}
		bases[name] = base
	}
	want := map[string]map[string]bool{}
	for app, rs := range budget.Apps {
		if _, ok := bases[app]; !ok {
			return fmt.Errorf("budget names app %q but no -app %s=URL was given", app, app)
		}
		for r := range rs {
			if want[app] == nil {
				want[app] = map[string]bool{}
			}
			want[app][r] = true
		}
	}
	for _, r := range routes {
		app, p, ok := strings.Cut(r, "=")
		if !ok || !strings.HasPrefix(p, "/") {
			return fmt.Errorf("-route %q: want NAME=/path", r)
		}
		if _, ok := bases[app]; !ok {
			return fmt.Errorf("-route %q: no -app %s=URL", r, app)
		}
		if want[app] == nil {
			want[app] = map[string]bool{}
		}
		want[app][p] = true
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	report := Report{Schema: reportSchema, MeasuredAt: time.Now().UTC()}
	for _, app := range sortedKeys(want) {
		for _, route := range sortedKeys(want[app]) {
			m, err := wire.Crawl(ctx, wire.Options{}, app, bases[app], route)
			if err != nil {
				return fmt.Errorf("%s %s: %w", app, route, err)
			}
			report.Routes = append(report.Routes, m)
		}
	}

	if *reportPath != "" {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(*reportPath, append(data, '\n'), 0o644); err != nil {
			return err
		}
	}
	var before *Report
	if *comparePath != "" {
		r, err := readReport(*comparePath)
		if err != nil {
			return fmt.Errorf("read -compare: %w", err)
		}
		before = &r
	}
	table := Table(report, before)
	fmt.Fprint(stdout, table)
	if *markdownPath != "" {
		if err := os.WriteFile(*markdownPath, []byte(table), 0o644); err != nil {
			return err
		}
	}

	if *write {
		updated := wire.Update(budget, report.Routes, *allowRaise)
		data, err := updated.Marshal()
		if err != nil {
			return err
		}
		if err := os.WriteFile(*budgetPath, data, 0o644); err != nil {
			return err
		}
		fmt.Fprintf(stderr, "wiregate: wrote %s\n", *budgetPath)
		budget = updated
	}

	findings := wire.Check(budget, report.Routes)
	if len(findings) == 0 {
		fmt.Fprintf(stderr, "wiregate: %d routes within budget\n", len(report.Routes))
		return nil
	}
	for _, f := range findings {
		fmt.Fprintf(stderr, "FAIL %s\n", f)
	}
	return fmt.Errorf("%w: %d findings", errGate, len(findings))
}

func runRatchet(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("ratchet", flag.ContinueOnError)
	basePath := fs.String("base", "", "budget file from the base branch")
	headPath := fs.String("head", "perf/budgets/wire.json", "budget file of this change")
	initial := fs.Bool("initial", false, "the base branch has no budget file (checked by the caller); skip the ratchet")
	if err := fs.Parse(args); err != nil {
		return err
	}
	head, err := wire.ReadBudget(*headPath)
	if err != nil {
		return fmt.Errorf("read head budget: %w", err)
	}
	if *initial {
		fmt.Fprintln(stdout, "wiregate: the base branch has no budget; nothing to ratchet against")
		return nil
	}
	if *basePath == "" {
		return errors.New("ratchet needs -base FILE, or -initial when the base branch has no budget")
	}
	// A missing or empty base file is an error, never a pass: a failed
	// fetch of the base revision must not skip the ratchet.
	baseData, err := os.ReadFile(*basePath)
	if err != nil {
		return fmt.Errorf("read base budget: %w", err)
	}
	base, err := wire.ParseBudget(baseData)
	if err != nil {
		return fmt.Errorf("read base budget: %w", err)
	}
	findings := wire.Ratchet(base, head)
	for _, f := range findings {
		fmt.Fprintf(stdout, "FAIL %s\n", f)
	}
	if len(findings) > 0 {
		return fmt.Errorf("%w: %d loosened limits or policies without a reason", errGate, len(findings))
	}
	fmt.Fprintln(stdout, "wiregate: budget only tightened")
	return nil
}

func runTable(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("table", flag.ContinueOnError)
	reportPath := fs.String("report", "", "report JSON")
	comparePath := fs.String("compare", "", "earlier report (before column)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	after, err := readReport(*reportPath)
	if err != nil {
		return err
	}
	var before *Report
	if *comparePath != "" {
		r, err := readReport(*comparePath)
		if err != nil {
			return err
		}
		before = &r
	}
	fmt.Fprint(stdout, Table(after, before))
	return nil
}

func readReport(path string) (Report, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Report{}, err
	}
	var r Report
	if err := json.Unmarshal(data, &r); err != nil {
		return Report{}, err
	}
	if r.Schema != reportSchema {
		return Report{}, fmt.Errorf("%s: schema %q, want %q", path, r.Schema, reportSchema)
	}
	return r, nil
}

// Table renders a Markdown table, one row per route. With a before report,
// each cell reads "before -> after".
func Table(after Report, before *Report) string {
	prev := map[string]wire.Route{}
	if before != nil {
		for _, r := range before.Routes {
			prev[r.App+" "+r.Route] = r
		}
	}
	var b strings.Builder
	b.WriteString("| App | Route | Total | Framework JS+WASM | HTML | JS | WASM | CSS | Images | On-demand JS | Requests | Inline JS | Policies failing |\n")
	b.WriteString("|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---|\n")
	for _, r := range after.Routes {
		p, hasPrev := prev[r.App+" "+r.Route]
		cell := func(metric string, bytes bool) string {
			v := r.Value(metric)
			if !hasPrev {
				return format(v, bytes)
			}
			pv := p.Value(metric)
			if pv == v {
				return format(v, bytes)
			}
			return format(pv, bytes) + " → " + format(v, bytes)
		}
		failing := failingPolicies(r)
		if hasPrev {
			if pf := failingPolicies(p); pf != failing {
				failing = pf + " → " + failing
			}
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s |\n",
			r.App, r.Route,
			cell(wire.MetricTotalWireBytes, true),
			cell(wire.MetricFrameworkJSWireBytes, true),
			cell(wire.MetricHTMLWireBytes, true),
			cell(wire.MetricJSWireBytes, true),
			cell(wire.MetricWASMWireBytes, true),
			cell(wire.MetricCSSWireBytes, true),
			cell(wire.MetricImageWireBytes, true),
			cell(wire.MetricLazyJSWireBytes, true),
			cell(wire.MetricRequests, false),
			cell(wire.MetricInlineScriptBytes, true),
			failing)
	}
	b.WriteString("\nBytes are as sent on the wire (after br or gzip), fetched with `Accept-Encoding: br, gzip` and a mobile user agent. Inline JS is uncompressed. On-demand JS is every runtime chunk the page advertises for loading on demand; it is not in Total or Requests.\n")
	return b.String()
}

func failingPolicies(r wire.Route) string {
	results := r.EvaluatePolicies()
	var bad []string
	for _, p := range wire.Policies {
		if !results[p].Pass {
			bad = append(bad, p)
		}
	}
	if len(bad) == 0 {
		return "none"
	}
	return strings.Join(bad, ", ")
}

func format(v int64, bytes bool) string {
	if !bytes {
		return fmt.Sprintf("%d", v)
	}
	if v < 1000 {
		return fmt.Sprintf("%d B", v)
	}
	return fmt.Sprintf("%.1f KB", float64(v)/1000)
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
