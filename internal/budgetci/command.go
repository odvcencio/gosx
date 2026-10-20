package budgetci

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"m31labs.dev/gosx/perf/budget"
)

type runCommand func(context.Context, RunOptions) (*budget.Report, error)

// Execute is the hosted wrapper's native entry point. It accepts private
// locations and local replay inputs; CI identity and time have no flags.
func Execute(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return execute(ctx, args, stdout, stderr, Run)
}

func execute(ctx context.Context, args []string, stdout, stderr io.Writer, run runCommand) int {
	diagnostic := func(err error) int {
		var typed *budget.InputError
		if !errors.As(err, &typed) {
			err = failure("environment", "/command")
		}
		fmt.Fprintln(stderr, err)
		return 2
	}
	if ctx == nil || ctx.Err() != nil || run == nil {
		return diagnostic(failure("invalid-input", "/command"))
	}
	fs := flag.NewFlagSet("budget-gate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	root := fs.String("root", ".", "private source checkout")
	scratch := fs.String("scratch", os.TempDir(), "owned build scratch parent")
	base := fs.String("base", "", "local replay base commit")
	path := fs.String("budget", "perf/budgets/gosx.budget.json", "tracked budget input")
	output := fs.String("out", "build/budget-public", "new public artifact directory")
	publicCheck := fs.Bool("public-check", false, "validate the complete artifact directory")
	if fs.Parse(args) != nil || fs.NArg() != 0 || *root == "" || *scratch == "" || *output == "" || !privateGitPath(*path) || *publicCheck && *base != "" {
		return diagnostic(failure("invalid-input", "/command/flags"))
	}
	if mode := os.Getenv("BUDGET_MODE"); mode != "" && mode != "report-only" {
		return diagnostic(failure("invalid-input", "/command/mode"))
	}
	if os.Getenv("GITHUB_ACTIONS") == "true" && *base != "" {
		return diagnostic(failure("invalid-input", "/command/base"))
	}
	absolute, err := filepath.Abs(*root)
	if err != nil {
		return diagnostic(failure("environment", "/command/root"))
	}
	inputs, err := budget.LoadInputs(filepath.Join(absolute, filepath.FromSlash(*path)), budget.LoadOptions{RootDir: absolute})
	if err != nil {
		return diagnostic(err)
	}
	validator, err := inputs.PublicValidator()
	if err != nil {
		return diagnostic(err)
	}
	if *publicCheck {
		if err := ValidateArtifacts(validator, *output); err != nil {
			return diagnostic(err)
		}
		if _, err := fmt.Fprintln(stdout, "performance budget artifacts validated"); err != nil {
			return diagnostic(failure("environment", "/command/output"))
		}
		return 0
	}
	report, err := run(ctx, RunOptions{Root: absolute, Scratch: *scratch, LocalBase: *base, BudgetPath: *path, Reviews: GitHubReviews{}})
	if err != nil {
		return diagnostic(err)
	}
	if report == nil || report.Mode != "report-only" || report.Info.ProfileSHA256 != inputs.File.Profile.SHA256 || report.Info.CoefficientSHA256 != inputs.File.Coefficients.SHA256 || report.Info.ToolchainSHA256 != inputs.File.Toolchain.SHA256 || report.Info.FixtureSHA256 != inputs.File.Fixtures.SHA256 {
		return diagnostic(failure("wrong-fixture", "/command/report"))
	}
	if err := cleanSource(ctx, absolute, report.Info.SHA); err != nil {
		return diagnostic(err)
	}
	if err := PublishArtifacts(validator, report, *output); err != nil {
		return diagnostic(err)
	}
	if _, err := fmt.Fprintln(stdout, "performance budget report written (report-only)"); err != nil {
		return diagnostic(failure("environment", "/command/output"))
	}
	return 0
}
