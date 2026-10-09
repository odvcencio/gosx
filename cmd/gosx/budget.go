package main

import (
	"bytes"
	"crypto/rand"
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
	if errors.Is(err, errBudgetChanged) {
		fmt.Fprintln(w, errBudgetChanged.Error())
		return status
	}
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
		defer r.Close()
		if err := writeBudgetAt(r, relative, data, inputs); err != nil {
			return budgetDiagnostic(stderr, err, 2, "environment", "output", "")
		}
		return 0
	}
	if *out != "" {
		output, err := filepath.Abs(*out)
		if err != nil {
			return budgetDiagnostic(stderr, nil, 2, "environment", "output", "")
		}
		if resolved, err := filepath.EvalSymlinks(output); err == nil {
			output = resolved
		}
		var source *budget.Inputs
		if output == inputs.BudgetPath() {
			source = inputs
		}
		if err := writeBudgetAtomically(output, data, source); err != nil {
			return budgetDiagnostic(stderr, err, 2, "environment", "output", "")
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
func writeBudgetAtomically(path string, data []byte, source ...*budget.Inputs) error {
	parent, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		return errors.New("output unavailable")
	}
	root, err := os.OpenRoot(parent)
	if err != nil {
		return errors.New("output unavailable")
	}
	defer root.Close()
	return writeBudgetAt(root, filepath.Base(path), data, source...)
}
func writeBudgetAt(root *os.Root, path string, data []byte, expected ...*budget.Inputs) error {
	temp, err := stageBudgetAt(root, path, data)
	if err != nil {
		return err
	}
	defer root.Remove(temp)
	var source *budget.Inputs
	if len(expected) > 0 {
		source = expected[0]
	}
	return replaceBudgetAt(root, path, temp, source)
}

func stageBudgetAt(root *os.Root, path string, data []byte) (temp string, resultErr error) {
	info, err := root.Stat(path)
	mode := os.FileMode(0644)
	if err == nil {
		if !info.Mode().IsRegular() {
			return "", errors.New("invalid output")
		}
		mode = info.Mode().Perm()
	} else if !os.IsNotExist(err) {
		return "", errors.New("invalid output")
	}
	temp = filepath.Join(filepath.Dir(path), ".gosx-budget-"+rand.Text())
	file, err := root.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return "", errors.New("output unavailable")
	}
	defer func() {
		if resultErr != nil {
			root.Remove(temp)
		}
	}()
	if err := file.Chmod(mode); err != nil {
		file.Close()
		return temp, errors.New("output unavailable")
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return temp, errors.New("output unavailable")
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return temp, errors.New("output unavailable")
	}
	if err := file.Close(); err != nil {
		return temp, errors.New("output unavailable")
	}
	return temp, nil
}

func replaceBudgetAt(root *os.Root, path, temp string, source *budget.Inputs) error {
	defer root.Remove(temp)
	lock, err := openBudgetLock(root, path+".lock")
	if err != nil {
		return errors.New("output unavailable")
	}
	defer lock.Close() // Keep this inode: unlinking a lock permits two writers.
	if err := acquireBudgetLock(lock); err != nil {
		return errors.New("output unavailable")
	}
	lockedInfo, err := lock.Stat()
	currentLock, statErr := root.Lstat(path + ".lock")
	if err != nil || statErr != nil || !os.SameFile(lockedInfo, currentLock) {
		return errors.New("output unavailable")
	}
	directory, err := openBudgetDirectory(root, filepath.Dir(path))
	if err != nil {
		return errors.New("output unavailable")
	}
	defer directory.Close()
	// Detect unsupported directory durability before modifying the source.
	if err := directory.Sync(); err != nil {
		return errors.New("output unavailable")
	}
	if source != nil {
		if err := compareBudgetSource(root, path, source); err != nil {
			return err
		}
	}
	if err := root.Rename(temp, path); err != nil {
		return errors.New("output unavailable")
	}
	if err := directory.Sync(); err != nil {
		return errors.New("output unavailable")
	}
	return nil
}

var errBudgetChanged = errors.New("budget file changed since it was read; re-run derive")

func compareBudgetSource(root *os.Root, path string, source *budget.Inputs) error {
	before, err := root.Lstat(path)
	if err != nil || !before.Mode().IsRegular() {
		return errBudgetChanged
	}
	file, err := root.OpenFile(path, os.O_RDONLY|budgetOpenFlags, 0)
	if err != nil {
		return errors.New("output unavailable")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || !os.SameFile(before, info) {
		return errBudgetChanged
	}
	data, err := io.ReadAll(io.LimitReader(file, 2<<20+1))
	if err != nil || len(data) > 2<<20 {
		return errBudgetChanged
	}
	after, err := file.Stat()
	current, pathErr := root.Lstat(path)
	if err != nil || pathErr != nil || !os.SameFile(after, current) || !source.BudgetFileMatches(after, data) {
		return errBudgetChanged
	}
	return nil
}

func openBudgetLock(root *os.Root, path string) (*os.File, error) {
	if info, err := root.Lstat(path); err == nil {
		if !info.Mode().IsRegular() {
			return nil, errors.New("invalid lock")
		}
	} else if !os.IsNotExist(err) {
		return nil, errors.New("invalid lock")
	}
	file, err := root.OpenFile(path, os.O_CREATE|os.O_RDWR|budgetOpenFlags, 0600)
	if err != nil {
		return nil, errors.New("invalid lock")
	}
	info, err := file.Stat()
	current, statErr := root.Lstat(path)
	if err != nil || statErr != nil || !info.Mode().IsRegular() || !current.Mode().IsRegular() || !os.SameFile(info, current) {
		file.Close()
		return nil, errors.New("invalid lock")
	}
	return file, nil
}
