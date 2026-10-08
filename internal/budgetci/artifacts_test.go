//go:build linux

package budgetci

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx/perf/budget"
)

func artifactFixture(t *testing.T) (RunOptions, *budget.PublicValidator, *budget.Report) {
	t.Helper()
	opts, head := runFixture(t)
	inputs, err := budget.LoadInputs(filepath.Join(opts.Root, opts.BudgetPath), budget.LoadOptions{RootDir: opts.Root})
	if err != nil {
		t.Fatal(err)
	}
	validator, err := inputs.PublicValidator()
	if err != nil {
		t.Fatal(err)
	}
	report := syntheticRunReport(t, inputs, head, nil)
	report.Mode = "report-only"
	return opts, validator, report
}

func TestArtifactsPublishOnlyValidatedMatchingRepresentations(t *testing.T) {
	_, validator, report := artifactFixture(t)
	output := filepath.Join(t.TempDir(), "public")
	if err := PublishArtifacts(validator, report, output); err != nil {
		t.Fatal(err)
	}
	if err := ValidateArtifacts(validator, output); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(output)
	if err != nil || len(entries) != 2 || entries[0].Name() != "report.json" || entries[1].Name() != "report.md" {
		t.Fatal("unexpected public files", err)
	}
	before, _ := os.ReadFile(filepath.Join(output, "report.json"))
	if err := PublishArtifacts(validator, report, output); err == nil {
		t.Fatal("existing artifacts replaced")
	}
	after, _ := os.ReadFile(filepath.Join(output, "report.json"))
	if !bytes.Equal(before, after) {
		t.Fatal("existing report changed")
	}
}

func TestArtifactsRejectPrivateAndUnregisteredDataBeforeWriting(t *testing.T) {
	for _, cause := range []string{"source", "route", "asset", "policy", "mode"} {
		t.Run(cause, func(t *testing.T) {
			_, validator, report := artifactFixture(t)
			switch cause {
			case "source":
				report.Assets[0].ChangedSources = []string{"private/diagnostics.txt"}
			case "route":
				report.Rows[0].RouteTemplate = "/counter/?private=1"
			case "asset":
				report.Assets[0].ID = "private/diagnostics.txt"
			case "policy":
				report.Rows[0].Policies[0].Name = "private-value"
			case "mode":
				report.Mode = "private-value"
			}
			output := filepath.Join(t.TempDir(), "public")
			if err := PublishArtifacts(validator, report, output); err == nil || strings.Contains(err.Error(), "private-value") {
				t.Fatal("invalid data accepted or echoed", err)
			}
			if _, err := os.Stat(output); !os.IsNotExist(err) {
				t.Fatal("invalid artifacts were written")
			}
		})
	}
}

func TestArtifactsRejectIncompleteExtraLinkedAndMismatchedFiles(t *testing.T) {
	for _, cause := range []string{"extra", "missing", "link", "directory", "oversized", "mismatched"} {
		t.Run(cause, func(t *testing.T) {
			_, validator, report := artifactFixture(t)
			output := filepath.Join(t.TempDir(), "public")
			if err := PublishArtifacts(validator, report, output); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(output, "report.md")
			switch cause {
			case "extra":
				os.WriteFile(filepath.Join(output, "server.log"), []byte("private diagnostic"), 0600)
			case "missing":
				os.Remove(path)
			case "link", "directory":
				os.Remove(path)
				if cause == "link" {
					os.Symlink("report.json", path)
				} else {
					os.Mkdir(path, 0700)
				}
			case "oversized":
				os.WriteFile(path, bytes.Repeat([]byte("x"), nativeLimit+1), 0600)
			case "mismatched":
				report.Info.SHA = strings.Repeat("f", 40)
				var markdown bytes.Buffer
				if err := validator.WriteMarkdown(&markdown, *report); err != nil {
					t.Fatal(err)
				}
				os.WriteFile(path, markdown.Bytes(), 0600)
			}
			if err := ValidateArtifacts(validator, output); err == nil {
				t.Fatal("unsafe artifact set accepted")
			}
		})
	}
}

func TestArtifactCommandReportOnlyAndValidation(t *testing.T) {
	opts, _, report := artifactFixture(t)
	output := filepath.Join(t.TempDir(), "public")
	args := []string{"--root", opts.Root, "--budget", opts.BudgetPath, "--scratch", opts.Scratch, "--base", opts.LocalBase, "--out", output}
	var stdout, stderr bytes.Buffer
	calls := 0
	runner := func(_ context.Context, got RunOptions) (*budget.Report, error) {
		calls++
		if got.Root != opts.Root || got.BudgetPath != opts.BudgetPath || got.LocalBase != opts.LocalBase || got.Reviews == nil {
			t.Fatal("native bindings changed")
		}
		report.Passed = false
		return report, nil
	}
	if code := execute(context.Background(), args, &stdout, &stderr, runner); code != 0 || calls != 1 || stderr.Len() != 0 {
		t.Fatal("report-only violation suppressed output or failed", code, stderr.String())
	}
	check := []string{"--root", opts.Root, "--budget", opts.BudgetPath, "--out", output, "--public-check"}
	if code := execute(context.Background(), check, &stdout, &stderr, runner); code != 0 || calls != 1 {
		t.Fatal("public check started a production build", code, stderr.String())
	}
}

func TestArtifactCommandRejectsOverridesAndKeepsToolErrorsFatal(t *testing.T) {
	for _, cause := range []string{"now", "chunks", "mode", "ci-base", "tool", "enforcing-report", "changed-provenance", "private-error", "extra-argument"} {
		t.Run(cause, func(t *testing.T) {
			opts, _, report := artifactFixture(t)
			output := filepath.Join(t.TempDir(), "public")
			args := []string{"--root", opts.Root, "--budget", opts.BudgetPath, "--out", output}
			switch cause {
			case "now":
				args = append(args, "--now", "2026-10-08")
			case "chunks":
				args = append(args, "--chunks-only")
			case "mode":
				t.Setenv("BUDGET_MODE", "enforce")
			case "ci-base":
				t.Setenv("GITHUB_ACTIONS", "true")
				args = append(args, "--base", opts.LocalBase)
			case "extra-argument":
				args = append(args, "private-value")
			}
			var stdout, stderr bytes.Buffer
			code := execute(context.Background(), args, &stdout, &stderr, func(context.Context, RunOptions) (*budget.Report, error) {
				switch cause {
				case "tool":
					return nil, failure("environment", "/test/tool")
				case "private-error":
					return nil, errors.New("private-value")
				case "enforcing-report":
					report.Mode = "enforce"
				case "changed-provenance":
					report.Info.ProfileSHA256 = strings.Repeat("2", 64)
				}
				return report, nil
			})
			if code != 2 || stdout.Len() != 0 || strings.Contains(stderr.String(), "private-value") || strings.Contains(stderr.String(), opts.Root) {
				t.Fatal("tool error suppressed or exposed private input", code)
			}
			if _, err := os.Stat(output); !os.IsNotExist(err) {
				t.Fatal("failed command wrote public artifacts")
			}
		})
	}
}

type failedArtifactOutput struct{}

func (failedArtifactOutput) Write([]byte) (int, error) {
	return 0, errors.New("private output failure")
}

func TestArtifactCommandOutputFailureIsToolError(t *testing.T) {
	opts, validator, report := artifactFixture(t)
	output := filepath.Join(t.TempDir(), "public")
	if err := PublishArtifacts(validator, report, output); err != nil {
		t.Fatal(err)
	}
	args := []string{"--root", opts.Root, "--budget", opts.BudgetPath, "--out", output, "--public-check"}
	var stderr bytes.Buffer
	code := execute(context.Background(), args, failedArtifactOutput{}, &stderr, func(context.Context, RunOptions) (*budget.Report, error) {
		t.Fatal("validation started a production build")
		return nil, nil
	})
	if code != 2 || strings.Contains(stderr.String(), "private output failure") {
		t.Fatal("output failure suppressed or leaked", code)
	}
}
