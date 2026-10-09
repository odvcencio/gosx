package main

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"

	"m31labs.dev/gosx/perf/budget"
)

func cmdBudget() { os.Exit(RunBudget(os.Args[2:], os.Stdout, os.Stderr)) }

// RunBudget exposes deterministic budget commands without process exit or
// implicit writes. Diagnostics never include argument values or OS errors.
func RunBudget(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return budgetDiagnostic(stderr, nil, 2, "invalid-input", "cli", "/command")
	}
	if isHelpArg(args[0]) {
		budgetUsage(stdout)
		return 0
	}
	switch args[0] {
	case "derive":
		return runBudgetDerive(args[1:], stdout, stderr)
	case "explain":
		return runBudgetExplain(args[1:], stdout, stderr)
	default:
		return budgetDiagnostic(stderr, nil, 2, "invalid-input", "cli", "/command")
	}
}
func budgetUsage(w io.Writer) {
	fmt.Fprint(w, `gosx budget - Derive and inspect performance allocations

Usage:
  gosx budget derive --budget FILE [--root DIR] [--check | --write | --out FILE]
  gosx budget explain --budget FILE --page-type TYPE [--root DIR] [--json]

Derive prints proposed JSON unless --check, --write or --out is supplied.
Explain --json encodes the complete explanation text as a JSON string.
`)
}
func budgetDiagnostic(w io.Writer, err error, status int, code, reference, pointer string) int {
	var input *budget.InputError
	if errors.As(err, &input) {
		code, reference, pointer = input.Code, input.Reference, input.Pointer
	}
	fmt.Fprintf(w, "%s: %s#%s\n", code, reference, pointer)
	return status
}
func runBudgetDerive(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("budget derive", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	path := fs.String("budget", "", "configuration file")
	root := fs.String("root", "", "project root")
	check := fs.Bool("check", false, "verify recorded arithmetic")
	write := fs.Bool("write", false, "replace the budget atomically")
	out := fs.String("out", "", "write proposed JSON")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 || *path == "" || *check && *write || *out != "" && (*check || *write) {
		return budgetDiagnostic(stderr, nil, 2, "invalid-input", "cli", "/flags")
	}
	inputs, err := budget.LoadDerivationInputs(*path, budget.LoadOptions{RootDir: *root})
	if err != nil {
		return budgetDiagnostic(stderr, err, 2, "invalid-input", "budget", "")
	}
	derived, err := budget.Derive(inputs.File, inputs.Profile, inputs.Coefficients)
	if err != nil {
		var input *budget.InputError
		if errors.As(err, &input) {
			return budgetDiagnostic(stderr, err, 2, "invalid-input", "budget", "")
		}
		return budgetDiagnostic(stderr, nil, 1, "derivation", "budget", "/pageTypes")
	}
	if *check {
		if !sameBudgetDerivations(inputs.File, derived) {
			return budgetDiagnostic(stderr, nil, 1, "derivation", "budget", "/pageTypes")
		}
		return 0
	}
	data, err := json.MarshalIndent(derived, "", "  ")
	if err != nil {
		return budgetDiagnostic(stderr, nil, 2, "invalid-input", "budget", "")
	}
	data = append(data, '\n')
	if *write {
		canonical, err := filepath.Abs(*path)
		if err == nil {
			canonical, err = filepath.EvalSymlinks(canonical)
		}
		if err != nil {
			return budgetDiagnostic(stderr, nil, 2, "environment", "output", "")
		}
		relative, err := filepath.Rel(inputs.RootDir(), canonical)
		if err != nil || relative == ".." || filepath.IsAbs(relative) {
			return budgetDiagnostic(stderr, nil, 2, "invalid-input", "output", "")
		}
		r, err := os.OpenRoot(inputs.RootDir())
		if err != nil {
			return budgetDiagnostic(stderr, nil, 2, "environment", "output", "")
		}
		file, err := r.Open(relative)
		if err != nil {
			r.Close()
			return budgetDiagnostic(stderr, nil, 2, "environment", "output", "")
		}
		current, readErr := io.ReadAll(io.LimitReader(file, 2<<20+1))
		closeErr := file.Close()
		defer r.Close()
		if readErr != nil || closeErr != nil || len(current) > 2<<20 {
			return budgetDiagnostic(stderr, nil, 2, "environment", "output", "")
		}
		sum := sha256.Sum256(current)
		if hex.EncodeToString(sum[:]) != inputs.BudgetSHA256 {
			return budgetDiagnostic(stderr, nil, 2, "invalid-input", "budget", "")
		}
		if err := writeBudgetAt(r, relative, data); err != nil {
			return budgetDiagnostic(stderr, nil, 2, "environment", "output", "")
		}
		return 0
	}
	if *out != "" {
		if err := writeBudgetAtomically(*out, data); err != nil {
			return budgetDiagnostic(stderr, nil, 2, "environment", "output", "")
		}
		return 0
	}
	if _, err := stdout.Write(data); err != nil {
		return budgetDiagnostic(stderr, nil, 2, "environment", "output", "")
	}
	return 0
}
func sameBudgetDerivations(old, derived budget.File) bool {
	for name, page := range old.PageTypes {
		want := derived.PageTypes[name]
		if !reflect.DeepEqual(page.Allocation, want.Allocation) || !reflect.DeepEqual(page.AfterReadyAllocation, want.AfterReadyAllocation) || page.WireEnvelope != want.WireEnvelope {
			return false
		}
	}
	return true
}
func runBudgetExplain(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("budget explain", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	path := fs.String("budget", "", "configuration file")
	root := fs.String("root", "", "project root")
	page := fs.String("page-type", "", "configured page type")
	asJSON := fs.Bool("json", false, "encode explanation text as JSON")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 || *path == "" || *page == "" {
		return budgetDiagnostic(stderr, nil, 2, "invalid-input", "cli", "/flags")
	}
	inputs, err := budget.LoadInputs(*path, budget.LoadOptions{RootDir: *root})
	if err != nil {
		return budgetDiagnostic(stderr, err, 2, "invalid-input", "budget", "")
	}
	if _, ok := inputs.File.PageTypes[*page]; !ok {
		return budgetDiagnostic(stderr, nil, 2, "invalid-input", "cli", "/page-type")
	}
	var text bytes.Buffer
	if err := budget.Explain(&text, inputs.File, inputs.Profile, inputs.Coefficients, *page); err != nil {
		return budgetDiagnostic(stderr, err, 2, "invalid-input", "budget", "")
	}
	data := text.Bytes()
	if *asJSON {
		data, err = json.Marshal(text.String())
		if err != nil {
			return budgetDiagnostic(stderr, nil, 2, "invalid-input", "explanation", "")
		}
		data = append(data, '\n')
	}
	if _, err := stdout.Write(data); err != nil {
		return budgetDiagnostic(stderr, nil, 2, "environment", "output", "")
	}
	return 0
}

// Write and sync a sibling file before rename. A failed write never truncates
// the destination; all owned temporary files are removed on failure.
func writeBudgetAtomically(path string, data []byte) error {
	parent, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		return errors.New("output unavailable")
	}
	root, err := os.OpenRoot(parent)
	if err != nil {
		return errors.New("output unavailable")
	}
	defer root.Close()
	return writeBudgetAt(root, filepath.Base(path), data)
}
func writeBudgetAt(root *os.Root, path string, data []byte) error {
	info, err := root.Stat(path)
	mode := os.FileMode(0644)
	if err == nil {
		if !info.Mode().IsRegular() {
			return errors.New("invalid output")
		}
		mode = info.Mode().Perm()
	} else if !os.IsNotExist(err) {
		return errors.New("invalid output")
	}
	temp := filepath.Join(filepath.Dir(path), ".gosx-budget-"+rand.Text())
	file, err := root.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return errors.New("output unavailable")
	}
	defer root.Remove(temp)
	if err := file.Chmod(mode); err != nil {
		file.Close()
		return errors.New("output unavailable")
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return errors.New("output unavailable")
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return errors.New("output unavailable")
	}
	if err := file.Close(); err != nil {
		return errors.New("output unavailable")
	}
	if err := root.Rename(temp, path); err != nil {
		return errors.New("output unavailable")
	}
	return nil
}
