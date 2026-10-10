package main

import (
	"bytes"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"

	"m31labs.dev/gosx/perf/budget"
)

func budgetPublicValidator(root string) (*budget.PublicValidator, error) {
	if root == "" {
		absolute, err := filepath.Abs(".")
		if err != nil {
			return nil, &budget.InputError{Code: "environment", Reference: "public-catalog"}
		}
		root = absolute
		for {
			if info, err := os.Stat(filepath.Join(root, "go.mod")); err == nil && info.Mode().IsRegular() {
				break
			}
			parent := filepath.Dir(root)
			if parent == root {
				return nil, &budget.InputError{Code: "invalid-input", Reference: "public-catalog"}
			}
			root = parent
		}
	}
	budgetPath := filepath.Join(root, "perf/budgets/gosx.budget.json")
	if _, err := os.Stat(budgetPath); err == nil {
		inputs, err := budget.LoadInputs(budgetPath, budget.LoadOptions{RootDir: root})
		if err != nil {
			return nil, err
		}
		return inputs.PublicValidator()
	} else if !os.IsNotExist(err) {
		return nil, &budget.InputError{Code: "environment", Reference: "public-catalog"}
	}
	catalog := filepath.Join(root, "perf/fixtures/catalog.v1.json")
	if _, err := os.Stat(catalog); os.IsNotExist(err) {
		catalog = filepath.Join(root, "perf/budget/testdata/catalog.v1.json")
	}
	return budget.NewPublicValidator(catalog, budget.LoadOptions{RootDir: root}, nil)
}
func runBudgetPublicCheck(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("budget public-check", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	format := fs.String("format", "auto", "json, jsonl, markdown or auto")
	root := fs.String("root", "", "project root")
	if err := fs.Parse(args); err != nil || fs.NArg() == 0 || *format != "auto" && *format != "json" && *format != "jsonl" && *format != "markdown" {
		return budgetDiagnostic(stderr, nil, 2, "invalid-input", "cli", "/flags")
	}
	validator, err := budgetPublicValidator(*root)
	if err != nil {
		return budgetDiagnostic(stderr, err, 2, "invalid-input", "public-catalog", "")
	}
	for _, path := range fs.Args() {
		selected := *format
		if selected == "auto" {
			switch strings.ToLower(filepath.Ext(path)) {
			case ".json":
				selected = "json"
			case ".jsonl":
				selected = "jsonl"
			case ".md", ".markdown":
				selected = "markdown"
			default:
				return budgetDiagnostic(stderr, nil, 2, "invalid-input", "cli", "/format")
			}
		}
		file, err := os.Open(path)
		if err != nil {
			return budgetDiagnostic(stderr, nil, 2, "environment", "public-record", "")
		}
		err = validator.Validate(file, selected)
		closeErr := file.Close()
		if err != nil {
			return budgetDiagnostic(stderr, err, 2, "invalid-input", "public-record", "")
		}
		if closeErr != nil {
			return budgetDiagnostic(stderr, nil, 2, "cleanup-failed", "public-record", "")
		}
	}
	return 0
}
func runBudgetReport(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("budget report", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	path := fs.String("report", "", "public report JSON")
	out := fs.String("out", "", "Markdown output")
	root := fs.String("root", "", "project root")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 || *path == "" || *out == "" {
		return budgetDiagnostic(stderr, nil, 2, "invalid-input", "cli", "/flags")
	}
	validator, err := budgetPublicValidator(*root)
	if err != nil {
		return budgetDiagnostic(stderr, err, 2, "invalid-input", "public-catalog", "")
	}
	file, err := os.Open(*path)
	if err != nil {
		return budgetDiagnostic(stderr, nil, 2, "environment", "report", "")
	}
	data, readErr := io.ReadAll(io.LimitReader(file, 2<<20+1))
	closeErr := file.Close()
	if len(data) > 2<<20 {
		return budgetDiagnostic(stderr, nil, 2, "invalid-input", "report", "")
	}
	if readErr != nil || closeErr != nil {
		return budgetDiagnostic(stderr, nil, 2, "environment", "report", "")
	}
	if err := validator.Validate(bytes.NewReader(data), "json"); err != nil {
		return budgetDiagnostic(stderr, err, 2, "invalid-input", "report", "")
	}
	record, err := budget.DecodeRecord(bytes.NewReader(data))
	if err != nil {
		return budgetDiagnostic(stderr, err, 2, "invalid-input", "report", "")
	}
	report, ok := record.(*budget.Report)
	if !ok {
		return budgetDiagnostic(stderr, nil, 2, "invalid-input", "report", "/schema")
	}
	var rendered bytes.Buffer
	if err := validator.WriteMarkdown(&rendered, *report); err != nil {
		return budgetDiagnostic(stderr, err, 2, "invalid-input", "report", "")
	}
	if err := writeBudgetAtomically(*out, rendered.Bytes()); err != nil {
		return budgetDiagnostic(stderr, nil, 2, "environment", "output", "")
	}
	return 0
}
