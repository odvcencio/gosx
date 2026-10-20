package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"m31labs.dev/gosx/perf/budget"
)

type budgetBindings map[string]string

func (v budgetBindings) String() string { return "" }
func (v budgetBindings) Set(text string) error {
	key, value, ok := strings.Cut(text, "=")
	if !ok || key == "" || value == "" || v[key] != "" {
		return errors.New("invalid binding")
	}
	v[key] = value
	return nil
}

func runBudgetCheck(args []string, stdout, stderr io.Writer) int {
	return runBudgetCheckWith(args, stdout, stderr, budget.Collect)
}
func runBudgetCheckWith(args []string, stdout, stderr io.Writer, collect func(context.Context, budget.CollectOptions) (*budget.Report, error)) int {
	fs := flag.NewFlagSet("budget check", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	path := fs.String("budget", "", "configuration file")
	root := fs.String("root", "", "project root")
	basePath := fs.String("base-report", "", "canonical base report")
	pairPath := fs.String("pair-report", "", "validated paired timing evidence")
	trailersPath := fs.String("trailers", "", "change description")
	reportPath := fs.String("report", "", "public JSON output")
	markdownPath := fs.String("markdown", "", "public Markdown output")
	reportOnly := fs.Bool("report-only", false, "retain violations without failure exit")
	chunks := fs.Bool("chunks-only", false, "collect advisory artifact inventory")
	date := fs.String("now", "", "local UTC date override")
	apps, dists := budgetBindings{}, budgetBindings{}
	fs.Var(apps, "app", "registered app and endpoint")
	fs.Var(dists, "dist", "registered app and directory")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 || *path == "" || !*chunks && *basePath == "" || *chunks && (*trailersPath != "" || *pairPath != "") || *reportPath != "" && *reportPath == *markdownPath {
		return budgetDiagnostic(stderr, nil, 2, "invalid-input", "cli", "/flags")
	}
	if *reportPath != "" && *markdownPath != "" {
		jsonPath, e1 := filepath.Abs(*reportPath)
		mdPath, e2 := filepath.Abs(*markdownPath)
		if e1 != nil || e2 != nil || jsonPath == mdPath {
			return budgetDiagnostic(stderr, nil, 2, "invalid-input", "cli", "/flags")
		}
	}
	now := time.Now().UTC()
	if *date != "" {
		var err error
		now, err = time.Parse("2006-01-02", *date)
		if err != nil || now.Format("2006-01-02") != *date || os.Getenv("GITHUB_ACTIONS") == "true" {
			return budgetDiagnostic(stderr, nil, 2, "invalid-input", "cli", "/now")
		}
	}
	inputs, err := budget.LoadInputs(*path, budget.LoadOptions{RootDir: *root})
	if err != nil {
		return budgetDiagnostic(stderr, err, 2, "invalid-input", "budget", "")
	}
	v, err := inputs.PublicValidator()
	if err != nil {
		return budgetDiagnostic(stderr, err, 2, "invalid-input", "public-catalog", "")
	}
	var base *budget.Report
	if *basePath != "" {
		data, err := readBudgetNativeFile(*basePath)
		if err != nil {
			return budgetDiagnostic(stderr, err, 2, "invalid-input", "base", "")
		}
		if err := v.Validate(bytes.NewReader(data), "json"); err != nil {
			return budgetDiagnostic(stderr, err, 2, "invalid-input", "base", "")
		}
		record, err := budget.DecodeRecord(bytes.NewReader(data))
		if err != nil {
			return budgetDiagnostic(stderr, err, 2, "invalid-input", "base", "")
		}
		var ok bool
		base, ok = record.(*budget.Report)
		if !ok {
			return budgetDiagnostic(stderr, nil, 2, "invalid-input", "base", "/schema")
		}
	}
	headSHA, baseSHA, err := budgetGitIdentity(inputs.RootDir())
	if err != nil {
		return budgetDiagnostic(stderr, err, 2, "environment", "source", "")
	}
	if base != nil && baseSHA != "" && base.Info.SHA != baseSHA {
		return budgetDiagnostic(stderr, nil, 2, "wrong-fixture", "base", "/info/sha")
	}
	if base != nil && baseSHA == "" {
		cmd := exec.Command("git", "-C", inputs.RootDir(), "merge-base", "--is-ancestor", base.Info.SHA, headSHA)
		if cmd.Run() != nil {
			return budgetDiagnostic(stderr, nil, 2, "wrong-fixture", "base", "/info/sha")
		}
	}
	configured := map[string]bool{}
	var pair *budget.PairReport
	if *pairPath != "" {
		data, err := readBudgetNativeFile(*pairPath)
		if err != nil {
			return budgetDiagnostic(stderr, err, 2, "invalid-input", "pair", "")
		}
		if err := v.Validate(bytes.NewReader(data), "json"); err != nil {
			return budgetDiagnostic(stderr, err, 2, "invalid-input", "pair", "")
		}
		record, err := budget.DecodeRecord(bytes.NewReader(data))
		if err != nil {
			return budgetDiagnostic(stderr, err, 2, "invalid-input", "pair", "")
		}
		var ok bool
		pair, ok = record.(*budget.PairReport)
		if !ok {
			return budgetDiagnostic(stderr, nil, 2, "invalid-input", "pair", "/schema")
		}
	}
	for _, route := range inputs.File.Routes {
		configured[route.App] = true
	}
	if len(dists) != len(configured) || !*chunks && len(apps) != len(configured) || *chunks && len(apps) != 0 && len(apps) != len(configured) {
		return budgetDiagnostic(stderr, nil, 2, "wrong-fixture", "cli", "/bindings")
	}
	bindings := []budget.CollectionBinding{}
	for app := range configured {
		if dists[app] == "" || !*chunks && apps[app] == "" {
			return budgetDiagnostic(stderr, nil, 2, "wrong-fixture", "cli", "/bindings")
		}
		bindings = append(bindings, budget.CollectionBinding{App: app, DistDir: dists[app], BaseURL: apps[app], ArtifactSHA256: os.Getenv("GOSX_BUDGET_PRODUCER_SHA256_" + strings.ToUpper(strings.ReplaceAll(app, "-", "_")))})
	}
	trailers := budget.Trailers{Entries: []budget.Ack{}}
	if *trailersPath != "" {
		data, err := readBudgetNativeFile(*trailersPath)
		if err != nil {
			return budgetDiagnostic(stderr, err, 2, "invalid-input", "trailers", "")
		}
		trailers, err = budget.ParseTrailers(string(data))
		if err != nil {
			return budgetDiagnostic(stderr, err, 2, "invalid-trailer", "trailers", "")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	report, err := collect(ctx, budget.CollectOptions{Inputs: inputs, Bindings: bindings, SHA: headSHA, Base: base, ChunksOnly: *chunks})
	if err != nil {
		return budgetDiagnostic(stderr, err, 2, "environment", "collection", "")
	}
	if !*chunks {
		report, err = budget.Check(budget.CheckOptions{File: inputs.File, Profile: inputs.Profile, Coefficients: inputs.Coefficients, Head: report, Base: base, Pair: pair, Trailers: trailers, Now: now, ReportOnly: *reportOnly})
		if err != nil {
			return budgetDiagnostic(stderr, err, 2, "invalid-input", "check", "")
		}
	} else if report == nil || report.Mode != "chunks-only" || report.Passed || len(report.Rows) != 0 || report.Coverage.Reachability != "unknown" {
		return budgetDiagnostic(stderr, nil, 2, "invalid-input", "collection", "")
	}
	var jsonOut, markdown bytes.Buffer
	if err := v.WriteJSON(&jsonOut, *report); err != nil {
		return budgetDiagnostic(stderr, err, 2, "invalid-input", "report", "")
	}
	if *markdownPath != "" {
		if err := v.WriteMarkdown(&markdown, *report); err != nil {
			return budgetDiagnostic(stderr, err, 2, "invalid-input", "report", "")
		}
	}
	if *reportPath != "" {
		if err := writeBudgetAtomically(*reportPath, jsonOut.Bytes()); err != nil {
			return budgetDiagnostic(stderr, nil, 2, "environment", "output", "")
		}
	} else {
		if n, err := stdout.Write(jsonOut.Bytes()); err != nil || n != jsonOut.Len() {
			return budgetDiagnostic(stderr, nil, 2, "environment", "output", "")
		}
	}
	if *markdownPath != "" {
		if err := writeBudgetAtomically(*markdownPath, markdown.Bytes()); err != nil {
			return budgetDiagnostic(stderr, nil, 2, "environment", "output", "")
		}
	}
	if *chunks {
		return 0
	}
	return budget.CheckExitCode(report, nil)
}

func readBudgetNativeFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, &budget.InputError{Code: "environment", Reference: "input"}
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, &budget.InputError{Code: "invalid-input", Reference: "input"}
	}
	data, err := io.ReadAll(io.LimitReader(f, 2<<20+1))
	if err != nil || len(data) > 2<<20 {
		return nil, &budget.InputError{Code: "invalid-input", Reference: "input"}
	}
	return data, nil
}

// CI identity comes from the runner's event record and the checked-out commit.
// It never accepts a reference or a date override from a pull request argument.
func budgetGitIdentity(root string) (string, string, error) {
	bad := func() (string, string, error) {
		return "", "", &budget.InputError{Code: "wrong-fixture", Reference: "source"}
	}
	data, err := exec.Command("git", "-C", root, "rev-parse", "HEAD").Output()
	if err != nil {
		return bad()
	}
	head := strings.TrimSpace(string(data))
	if os.Getenv("GITHUB_ACTIONS") != "true" {
		return head, "", nil
	}
	data, err = readBudgetNativeFile(os.Getenv("GITHUB_EVENT_PATH"))
	if err != nil {
		return bad()
	}
	var event struct {
		After       string `json:"after"`
		Before      string `json:"before"`
		PullRequest *struct {
			Head struct {
				SHA string `json:"sha"`
			} `json:"head"`
			Base struct {
				SHA string `json:"sha"`
			} `json:"base"`
		} `json:"pull_request"`
	}
	if json.Unmarshal(data, &event) != nil {
		return bad()
	}
	if event.PullRequest != nil {
		if event.PullRequest.Head.SHA != head || event.PullRequest.Base.SHA == "" {
			return bad()
		}
		return head, event.PullRequest.Base.SHA, nil
	}
	if event.After != head || event.Before == "" || strings.Trim(event.Before, "0") == "" {
		return bad()
	}
	return head, event.Before, nil
}
