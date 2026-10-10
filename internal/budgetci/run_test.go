//go:build linux

package budgetci

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"m31labs.dev/gosx/perf/budget"
)

func runFixture(t *testing.T) (RunOptions, string) {
	t.Helper()
	t.Setenv("GITHUB_ACTIONS", "false")
	root := t.TempDir()
	gitFixture(t, root, "init", "-q")
	gitFixture(t, root, "config", "user.name", "Fixture")
	gitFixture(t, root, "config", "user.email", "fixture")
	files := []string{"perf/budget/testdata/budget.v2.json", "perf/budget/testdata/profile.v1.json", "perf/budget/testdata/coefficients.v1.json", "perf/budget/testdata/toolchain.v1.json", "perf/budget/testdata/catalog.v1.json", "perf/budget/testdata/interaction.v1.json", "perf/wire/testdata/counter/page.gsx", "examples/gosx-docs/public/fonts/Inter-400.woff2"}
	for _, file := range files {
		data, err := os.ReadFile(filepath.Join("../..", file))
		if err != nil || os.MkdirAll(filepath.Dir(filepath.Join(root, file)), 0700) != nil || os.WriteFile(filepath.Join(root, file), data, 0600) != nil {
			t.Fatal("native comparison fixture failed", err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "marker"), []byte("base"), 0600); err != nil {
		t.Fatal(err)
	}
	path := "perf/budget/testdata/budget.v2.json"
	inputs, err := budget.LoadInputs(filepath.Join(root, path), budget.LoadOptions{RootDir: root})
	if err != nil {
		t.Fatal(err)
	}
	derived, err := budget.Derive(inputs.File, inputs.Profile, inputs.Coefficients)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(derived)
	if err := os.WriteFile(filepath.Join(root, path), data, 0600); err != nil {
		t.Fatal(err)
	}
	gitFixture(t, root, "add", ".")
	gitFixture(t, root, "commit", "-qm", "Add comparison base")
	base := gitFixture(t, root, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(root, "marker"), []byte("head"), 0600); err != nil {
		t.Fatal(err)
	}
	gitFixture(t, root, "add", "marker")
	gitFixture(t, root, "commit", "-qm", "Add comparison head")
	head := gitFixture(t, root, "rev-parse", "HEAD")
	return RunOptions{Root: root, Scratch: t.TempDir(), LocalBase: base, BudgetPath: path}, head
}

// Synthetic byte observations test the comparison controller. They do not
// stand in for a production build, measurement or timing certification.
func syntheticRunReport(t *testing.T, inputs *budget.Inputs, sha string, base *budget.Report) *budget.Report {
	t.Helper()
	data, err := os.ReadFile("../../perf/budget/testdata/public-report.v1.json")
	var report budget.Report
	if err != nil || json.Unmarshal(data, &report) != nil {
		t.Fatal("synthetic observation invalid", err)
	}
	artifact := strings.Repeat("1", 64)
	report.Info.SHA, report.Info.ArtifactSHA256 = sha, &artifact
	report.Info.ProfileSHA256, report.Info.CoefficientSHA256 = inputs.File.Profile.SHA256, inputs.File.Coefficients.SHA256
	report.Info.ToolchainSHA256, report.Info.FixtureSHA256 = inputs.File.Toolchain.SHA256, inputs.File.Fixtures.SHA256
	if base != nil {
		report.Info.BaseSHA, report.Info.BaseArtifactSHA256 = base.Info.SHA, *base.Info.ArtifactSHA256
	}
	report.Rows[0].Policies = []budget.PolicyResult{{Name: "html-compressed", Passed: true}, {Name: "assets-compressed", Passed: true}, {Name: "inline-app-executable", Passed: true}}
	report.Rows[0].AllocationBytes = inputs.File.PageTypes["island"].Allocation.TotalBytes
	report.Rows[0].FrameworkCeilingBytes = inputs.File.PageTypes["island"].Allocation.FrameworkBytes
	return &report
}

func TestRunMeasuresExactCleanBaseThenHeadAndCleansScratch(t *testing.T) {
	opts, head := runFixture(t)
	var calls []string
	var roots []string
	var first *budget.Inputs
	collect := func(ctx context.Context, source, sha, scratch string, inputs *budget.Inputs, base *budget.Report) (*budget.Report, error) {
		calls = append(calls, sha)
		roots = append(roots, source)
		if err := cleanSource(ctx, source, sha); err != nil || scratch == opts.Root {
			t.Fatal("measured checkout is not isolated", err)
		}
		data, _ := os.ReadFile(filepath.Join(source, "marker"))
		if len(calls) == 1 {
			first = inputs
			if base != nil || string(data) != "base" {
				t.Fatal("base substituted with head")
			}
		} else if base == nil || base.Info.SHA != opts.LocalBase || first != inputs || string(data) != "head" {
			t.Fatal("input domain or base observation changed")
		}
		return syntheticRunReport(t, inputs, sha, base), nil
	}
	report, err := run(context.Background(), opts, collect)
	if err != nil || !reflect.DeepEqual(calls, []string{opts.LocalBase, head}) || report.Info.SHA != head || report.Info.BaseSHA != opts.LocalBase || report.Mode != "report-only" || !report.Passed {
		t.Fatal("native comparison differs", calls, err)
	}
	for _, root := range roots {
		if _, err := os.Stat(root); !os.IsNotExist(err) {
			t.Fatal("measured checkout retained")
		}
	}
	if gitFixture(t, opts.Root, "rev-parse", "HEAD") != head || gitFixture(t, opts.Root, "status", "--porcelain") != "" {
		t.Fatal("source worktree changed")
	}
	if data, _ := json.Marshal(opts); string(data) != "{}" {
		t.Fatal("private comparison bindings became public")
	}
}

func TestRunRetainsViolationsAndUnroundedDeltasInReportOnly(t *testing.T) {
	for _, violating := range []bool{false, true} {
		t.Run(map[bool]string{false: "small-growth", true: "allocation"}[violating], func(t *testing.T) {
			opts, _ := runFixture(t)
			report, err := run(context.Background(), opts, func(_ context.Context, _, sha, _ string, inputs *budget.Inputs, base *budget.Report) (*budget.Report, error) {
				r := syntheticRunReport(t, inputs, sha, base)
				if base != nil {
					growth := int64(1)
					if violating {
						growth = inputs.File.PageTypes["island"].Allocation.TotalBytes
					}
					r.Rows[0].NormalizedBytes += growth
					r.Rows[0].AppBytes += growth
					r.Rows[0].PhaseBytes.Critical += growth
				}
				return r, nil
			})
			if err != nil || budget.CheckExitCode(report, err) != 0 || report.Mode != "report-only" || report.Passed == violating {
				t.Fatal("report-only lost its decision", err)
			}
			if !violating && (report.Rows[0].DeltaBytes == nil || *report.Rows[0].DeltaBytes != 1) {
				t.Fatal("one-byte delta rounded away")
			}
			if violating && len(report.Violations) == 0 {
				t.Fatal("violations erased")
			}
		})
	}
}

func TestRunRefusesMissingBaseCollectionAndChangedSource(t *testing.T) {
	for _, cause := range []string{"missing-base", "base-tool-error", "wrong-source", "dirty-source", "bad-budget-path", "cancelled"} {
		t.Run(cause, func(t *testing.T) {
			opts, _ := runFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if cause == "bad-budget-path" {
				opts.BudgetPath = "../private.json"
			}
			if cause == "cancelled" {
				cancel()
			}
			var roots []string
			report, err := run(ctx, opts, func(_ context.Context, source, sha, _ string, inputs *budget.Inputs, base *budget.Report) (*budget.Report, error) {
				roots = append(roots, source)
				switch cause {
				case "missing-base":
					return nil, nil
				case "base-tool-error":
					return nil, failure("environment", "/test/collection")
				case "wrong-source":
					sha = strings.Repeat("f", 40)
				case "dirty-source":
					os.WriteFile(filepath.Join(opts.Root, "marker"), []byte("changed"), 0600)
				}
				return syntheticRunReport(t, inputs, sha, base), nil
			})
			var typed *budget.InputError
			if report != nil || !errors.As(err, &typed) || budget.CheckExitCode(report, err) != 2 || strings.Contains(err.Error(), opts.Root) {
				t.Fatal("collection failure suppressed or exposed location", err)
			}
			for _, root := range roots {
				if _, err := os.Stat(root); !os.IsNotExist(err) {
					t.Fatal("failed comparison left checkout")
				}
			}
			if (cause == "cancelled" || cause == "bad-budget-path") && len(roots) != 0 {
				t.Fatal("invalid input started a collection")
			}
		})
	}
}

func TestRunRejectsProvenanceSubstitutionBeforeReporting(t *testing.T) {
	for _, cause := range []string{"profile", "coefficients", "toolchain", "catalog", "artifact", "base-sha", "base-artifact", "epoch", "transport"} {
		t.Run(cause, func(t *testing.T) {
			opts, _ := runFixture(t)
			report, err := run(context.Background(), opts, func(_ context.Context, _, sha, _ string, inputs *budget.Inputs, base *budget.Report) (*budget.Report, error) {
				r := syntheticRunReport(t, inputs, sha, base)
				if base != nil {
					hash := strings.Repeat("2", 64)
					switch cause {
					case "profile":
						r.Info.ProfileSHA256 = hash
					case "coefficients":
						r.Info.CoefficientSHA256 = hash
					case "toolchain":
						r.Info.ToolchainSHA256 = hash
					case "catalog":
						r.Info.FixtureSHA256 = hash
					case "artifact":
						r.Info.ArtifactSHA256 = nil
					case "base-sha":
						r.Info.BaseSHA = strings.Repeat("f", 40)
					case "base-artifact":
						r.Info.BaseArtifactSHA256 = hash
					case "epoch":
						r.Info.EpochSHA256 = &hash
					case "transport":
						r.Info.Transport = "h2-tls"
					}
				}
				return r, nil
			})
			var typed *budget.InputError
			if report != nil || !errors.As(err, &typed) || budget.CheckExitCode(report, err) != 2 {
				t.Fatal("source-compatible but incomparable observations accepted", err)
			}
		})
	}
}

func TestRunFreezesTrustedEventBeforeBuilding(t *testing.T) {
	opts, head := runFixture(t)
	event := map[string]any{"repository": map[string]any{"full_name": "fixture/project"}, "pull_request": map[string]any{"number": 1, "head": map[string]any{"sha": head}, "base": map[string]any{"sha": opts.LocalBase, "repo": map[string]any{"full_name": "fixture/project"}}}}
	data, _ := json.Marshal(event)
	path := filepath.Join(t.TempDir(), "event.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("GITHUB_REPOSITORY", "fixture/project")
	t.Setenv("GITHUB_EVENT_NAME", "pull_request")
	t.Setenv("GITHUB_EVENT_PATH", path)
	base := opts.LocalBase
	opts.LocalBase = ""
	report, err := run(context.Background(), opts, func(_ context.Context, _, sha, _ string, inputs *budget.Inputs, previous *budget.Report) (*budget.Report, error) {
		if previous == nil {
			// A later event update must not substitute either measured revision.
			if err := os.WriteFile(path, []byte(`{"after":"different"}`), 0600); err != nil {
				t.Fatal(err)
			}
		}
		return syntheticRunReport(t, inputs, sha, previous), nil
	})
	if err != nil || !report.Passed || report.Info.BaseSHA != base || report.Info.SHA != head {
		t.Fatal("event identity was not frozen", err)
	}
}
