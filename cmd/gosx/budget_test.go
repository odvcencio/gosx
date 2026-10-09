package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"m31labs.dev/gosx/perf/budget"
)

func budgetCommandFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	// Copy only the small closed inputs and their tracked references.
	repo, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"budget.v2.json", "profile.v1.json", "coefficients.v1.json", "toolchain.v1.json", "catalog.v1.json", "interaction.v1.json"} {
		source := filepath.Join(repo, "perf/budget/testdata", name)
		body, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		dest := filepath.Join(root, "perf/budget/testdata", name)
		if err := os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dest, body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"perf/wire/testdata/counter/page.gsx"} {
		body, err := os.ReadFile(filepath.Join(repo, name))
		if err != nil {
			t.Fatal(err)
		}
		dest := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dest, body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	// Toolchain font references, when present, must remain hash-bound too.
	var tool budget.Toolchain
	body, err := os.ReadFile(filepath.Join(root, "perf/budget/testdata/toolchain.v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(body, &tool); err != nil {
		t.Fatal(err)
	}
	for _, font := range tool.Fonts {
		body, err := os.ReadFile(filepath.Join(repo, font.File))
		if err != nil {
			t.Fatal(err)
		}
		dest := filepath.Join(root, font.File)
		if err := os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dest, body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return root, filepath.Join(root, "perf/budget/testdata/budget.v2.json")
}
func runBudgetTest(args ...string) (int, string, string) {
	var out, diagnostics bytes.Buffer
	code := RunBudget(args, &out, &diagnostics)
	return code, out.String(), diagnostics.String()
}
func TestBudgetDeriveProposalCheckAndAtomicWrite(t *testing.T) {
	root, path := budgetCommandFixture(t)
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	originalInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"derive", "--budget", path, "--root", root}
	code, out, diagnostics := runBudgetTest(args...)
	if code != 0 || diagnostics != "" || !strings.HasSuffix(out, "\n") {
		t.Fatal(code, diagnostics)
	}
	var proposed budget.File
	if err := json.Unmarshal([]byte(out), &proposed); err != nil {
		t.Fatal(err)
	}
	inputs, err := budget.LoadInputs(path, budget.LoadOptions{RootDir: root})
	if err != nil {
		t.Fatal(err)
	}
	want, err := budget.Derive(inputs.File, inputs.Profile, inputs.Coefficients)
	if err != nil || !reflect.DeepEqual(proposed, want) {
		t.Fatal("proposal lost exact arithmetic", err)
	}
	current, _ := os.ReadFile(path)
	if !bytes.Equal(current, original) {
		t.Fatal("default derive wrote the budget")
	}
	code, output, diagnostics := runBudgetTest(append(args, "--check")...)
	if code != 1 || output != "" || diagnostics != "derivation: budget#/pageTypes\n" {
		t.Fatal("recorded mismatch was not status 1", code, diagnostics)
	}
	code, output, diagnostics = runBudgetTest(append(args, "--write")...)
	if code != 0 || output != "" || diagnostics != "" {
		t.Fatal(code, diagnostics)
	}
	current, _ = os.ReadFile(path)
	if !bytes.Equal(current, []byte(out)) {
		t.Fatal("atomic write differs from proposed JSON")
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != originalInfo.Mode().Perm() {
		t.Fatal("write changed permissions", err)
	}
	code, output, diagnostics = runBudgetTest(append(args, "--check")...)
	if code != 0 || output != "" || diagnostics != "" {
		t.Fatal("written derivation did not verify", code, diagnostics)
	}
	matches, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".gosx-budget-*"))
	if len(matches) != 0 {
		t.Fatal("atomic write left scratch")
	}
}

func TestBudgetDeriveEditedReserve(t *testing.T) {
	for _, mode := range []string{"preview", "check", "write", "out"} {
		t.Run(mode, func(t *testing.T) {
			root, path := budgetCommandFixture(t)
			file, err := budget.Load(path, budget.LoadOptions{RootDir: root})
			if err != nil {
				t.Fatal(err)
			}
			page := file.PageTypes["island"]
			page.AppReserveBytes += 1024
			file.PageTypes["island"] = page
			edited, err := json.Marshal(file)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, edited, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := budget.Load(path, budget.LoadOptions{RootDir: root}); err == nil {
				t.Fatal("strict loading accepted stale allocations")
			}
			if code, _, _ := runBudgetTest("explain", "--budget", path, "--root", root, "--page-type", "island"); code != 2 {
				t.Fatal("explain accepted stale allocations", code)
			}
			args := []string{"derive", "--budget", path, "--root", root}
			dest := filepath.Join(root, "derived.json")
			switch mode {
			case "check", "write":
				args = append(args, "--"+mode)
			case "out":
				args = append(args, "--out", dest)
			}
			code, out, diagnostics := runBudgetTest(args...)
			if mode == "check" {
				if code != 1 || out != "" || diagnostics != "derivation: budget#/pageTypes\n" {
					t.Fatal("stale check did not reach derivation", code, diagnostics)
				}
			} else if code != 0 || diagnostics != "" || mode != "preview" && out != "" {
				t.Fatal("edited reserve could not be derived", code, diagnostics)
			}
			if mode != "write" {
				current, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(current, edited) {
					t.Fatal("derive changed the source without --write", err)
				}
			}
			if mode == "check" {
				return
			}
			if mode == "preview" {
				if err := os.WriteFile(dest, []byte(out), 0600); err != nil {
					t.Fatal(err)
				}
			} else if mode == "write" {
				dest = path
			}
			regenerated, err := budget.Load(dest, budget.LoadOptions{RootDir: root})
			if err != nil {
				t.Fatal("regenerated file failed strict loading", err)
			}
			got := regenerated.PageTypes["island"]
			if got.AppReserveBytes != page.AppReserveBytes || got.Allocation.AppCriticalReserveBytes != page.AppReserveBytes {
				t.Fatal("derivation did not adopt the edited reserve")
			}
			if code, _, diagnostics := runBudgetTest("derive", "--budget", dest, "--root", root, "--check"); code != 0 {
				t.Fatal("regenerated arithmetic did not verify", code, diagnostics)
			}
		})
	}
}

func TestBudgetDeriveOutAndInvalidFlagMatrix(t *testing.T) {
	root, path := budgetCommandFixture(t)
	dest := filepath.Join(root, "proposed.json")
	code, out, diagnostics := runBudgetTest("derive", "--budget", path, "--root", root, "--out", dest)
	if code != 0 || out != "" || diagnostics != "" {
		t.Fatal(code, diagnostics)
	}
	if body, err := os.ReadFile(dest); err != nil || len(body) == 0 {
		t.Fatal("output not written", err)
	}
	for _, args := range [][]string{
		nil, {"unknown-secret"}, {"derive"}, {"derive", "--budget", path, "--check", "--write"},
		{"derive", "--budget", path, "--check", "--out", dest}, {"derive", "--budget", path, "--write", "--out", dest},
		{"derive", "--budget", path, "unexpected-secret"}, {"derive", "--unknown-secret=value"},
		{"explain", "--budget", path}, {"explain", "--budget", path, "--page-type", "unregistered-secret", "--root", root},
	} {
		code, out, diagnostics := runBudgetTest(args...)
		if code != 2 || out != "" || strings.Contains(diagnostics, "secret") || strings.Contains(diagnostics, root) {
			t.Fatal("invalid flag leaked or passed", args, code, diagnostics)
		}
	}
}
func TestBudgetExplainTextAndJSONPreserveAllSensitivity(t *testing.T) {
	root, path := budgetCommandFixture(t)
	args := []string{"explain", "--budget", path, "--root", root, "--page-type", "island"}
	code, text, diagnostics := runBudgetTest(args...)
	if code != 0 || diagnostics != "" {
		t.Fatal(code, diagnostics)
	}
	for _, term := range []string{"desktop-cpu-proxy", "provisional", "ci95=unavailable", "after-ready", "window:", "jsMicrosPerKB +10%", "rtt +10%"} {
		if !strings.Contains(text, term) {
			t.Fatal("explanation omitted a coefficient or sensitivity", term)
		}
	}
	code, encoded, diagnostics := runBudgetTest(append(args, "--json")...)
	var decoded string
	if code != 0 || diagnostics != "" || json.Unmarshal([]byte(encoded), &decoded) != nil || decoded != text {
		t.Fatal("JSON explanation changed arithmetic", code, diagnostics)
	}
	if strings.Contains(text, root) {
		t.Fatal("explanation leaked its project root")
	}
}
func TestBudgetDiagnosticsNeverEchoFilesystemOrWriterErrors(t *testing.T) {
	for _, command := range []string{"derive", "explain"} {
		args := []string{command, "--budget", "/absent/synthetic-secret.json"}
		if command == "explain" {
			args = append(args, "--page-type", "island")
		}
		code, out, diagnostics := runBudgetTest(args...)
		if code != 2 || out != "" || strings.Contains(diagnostics, "secret") || strings.Contains(diagnostics, "absent") {
			t.Fatal("filesystem error escaped", code, diagnostics)
		}
	}
	root, path := budgetCommandFixture(t)
	for _, command := range []string{"derive", "explain"} {
		args := []string{command, "--budget", path, "--root", root}
		if command == "explain" {
			args = append(args, "--page-type", "island")
		}
		var diagnostics bytes.Buffer
		if code := RunBudget(args, budgetFailWriter{}, &diagnostics); code != 2 || strings.Contains(diagnostics.String(), "synthetic-secret") {
			t.Fatal("writer error leaked or passed", code, diagnostics.String())
		}
	}
}

type budgetFailWriter struct{}

func (budgetFailWriter) Write([]byte) (int, error) {
	return 0, errors.New("synthetic-secret writer failure")
}
func TestBudgetAtomicFailureAndConfinedRename(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "target.json")
	if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := writeBudgetAtomically(filepath.Join(path, "child.json"), []byte("new")); err == nil {
		t.Fatal("invalid parent accepted")
	}
	if body, _ := os.ReadFile(path); string(body) != "original" {
		t.Fatal("failed write truncated original")
	}
	outside := t.TempDir()
	sentinel := filepath.Join(outside, "target.json")
	os.WriteFile(sentinel, []byte("outside"), 0600)
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := writeBudgetAt(r, "escape/target.json", []byte("new")); err == nil {
		t.Fatal("atomic rename escaped root")
	}
	if body, _ := os.ReadFile(sentinel); string(body) != "outside" {
		t.Fatal("confined write changed outside file")
	}
	matches, _ := filepath.Glob(filepath.Join(root, ".gosx-budget-*"))
	if len(matches) != 0 {
		t.Fatal("failed atomic write left scratch")
	}
}
func TestBudgetHelpAdvertisesImplementedCommands(t *testing.T) {
	code, out, diagnostics := runBudgetTest("--help")
	if code != 0 || diagnostics != "" || !strings.Contains(out, "budget derive") || strings.Contains(out, "budget init") {
		t.Fatal("help includes unimplemented command", code, out)
	}
	var help bytes.Buffer
	if !commandUsage("budget", &help) || help.String() != out {
		t.Fatal("root command help differs")
	}
}
