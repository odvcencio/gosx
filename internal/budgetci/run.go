package budgetci

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"m31labs.dev/gosx/perf/budget"
)

type RunOptions struct {
	Root, Scratch, LocalBase, BudgetPath string       `json:"-"`
	Reviews                              ReviewSource `json:"-"`
}

type revisionCollector func(context.Context, string, string, string, *budget.Inputs, *budget.Report) (*budget.Report, error)

// Run builds and measures the exact base and head with the head's immutable
// input contract. Phase zero retains every violation in a report-only result.
func Run(ctx context.Context, opts RunOptions) (*budget.Report, error) {
	return run(ctx, opts, collectRevision)
}

func run(ctx context.Context, opts RunOptions, collect revisionCollector) (report *budget.Report, resultErr error) {
	identity, err := LoadIdentity(ctx, opts.Root, opts.LocalBase)
	if err != nil {
		return nil, err
	}
	if opts.BudgetPath == "" {
		opts.BudgetPath = "perf/budgets/gosx.budget.json"
	}
	if !privateGitPath(opts.BudgetPath) || collect == nil {
		return nil, failure("invalid-input", "/run/options")
	}
	inputs, err := budget.LoadInputs(filepath.Join(opts.Root, filepath.FromSlash(opts.BudgetPath)), budget.LoadOptions{RootDir: opts.Root})
	if err != nil {
		return nil, err
	}
	// Head inputs are intentionally used for both revisions. Older input files
	// cannot silently change the comparison's coefficient or fixture domain.
	owned, err := os.MkdirTemp(opts.Scratch, "budget-comparison-")
	if err != nil {
		return nil, failure("environment", "/run/scratch")
	}
	defer func() {
		if os.RemoveAll(owned) != nil {
			report, resultErr = nil, failure("cleanup", "/run/scratch")
		}
	}()
	var base *budget.Report
	for _, revision := range []struct{ name, sha string }{{"base", identity.Base}, {"head", identity.Head}} {
		source, err := cloneRevision(ctx, opts.Root, revision.sha, owned, revision.name)
		if err != nil {
			return nil, err
		}
		measured, err := collect(ctx, source, revision.sha, owned, inputs, base)
		if err != nil {
			return nil, err
		}
		if measured == nil || measured.Info.SHA != revision.sha {
			return nil, failure("wrong-fixture", "/run/"+revision.name)
		}
		if base == nil {
			base = measured
		} else {
			report = measured
		}
	}
	if err := cleanSource(ctx, opts.Root, identity.Head); err != nil {
		return nil, err
	}
	text, err := commandOutput(ctx, "git", "-C", opts.Root, "show", "-s", "--format=%B", identity.Head)
	if err != nil {
		return nil, failure("environment", "/run/trailers")
	}
	trailers, err := budget.ParseTrailers(string(text))
	if err != nil {
		return nil, err
	}
	approvals := []budget.TrustedApproval{}
	if identity.Repository != "" {
		approvals, err = Approvals(ctx, ApprovalOptions{Root: opts.Root, BudgetPath: opts.BudgetPath, HeadSHA: identity.Head, Repository: identity.Repository, PullRequest: identity.PullRequest, Exceptions: inputs.File.Exceptions, Source: opts.Reviews})
		if err != nil {
			return nil, err
		}
	}
	return budget.Check(budget.CheckOptions{File: inputs.File, Profile: inputs.Profile, Coefficients: inputs.Coefficients, Head: report, Base: base, Trailers: trailers, Approvals: approvals, Now: identity.Started, ReportOnly: true})
}

func privateGitPath(path string) bool {
	if path == "" || filepath.IsAbs(path) || strings.ContainsAny(path, "\\:\x00") {
		return false
	}
	for _, part := range strings.Split(path, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

func cloneRevision(ctx context.Context, root, sha, scratch, label string) (string, error) {
	if !commitPattern.MatchString(sha) || label != "base" && label != "head" {
		return "", failure("invalid-input", "/run/source")
	}
	source := filepath.Join(scratch, label)
	if err := productionCommand(ctx, root, filepath.Join(scratch, label+"-clone.log"), "git", "clone", "--quiet", "--shared", "--no-checkout", "--", root, source); err != nil {
		return "", err
	}
	if err := productionCommand(ctx, source, filepath.Join(scratch, label+"-checkout.log"), "git", "checkout", "--quiet", "--detach", sha); err != nil {
		return "", err
	}
	if err := cleanSource(ctx, source, sha); err != nil {
		return "", err
	}
	return source, nil
}

func collectRevision(ctx context.Context, source, sha, scratch string, inputs *budget.Inputs, base *budget.Report) (report *budget.Report, resultErr error) {
	compiler, err := BuildCompiler(ctx, source, sha, scratch)
	if err != nil {
		return nil, err
	}
	defer func() {
		if compiler.Close() != nil {
			report, resultErr = nil, failure("cleanup", "/run/compiler")
		}
	}()
	seen := map[string]bool{}
	for _, route := range inputs.File.Routes {
		seen[route.App] = true
	}
	apps := make([]string, 0, len(seen))
	for app := range seen {
		apps = append(apps, app)
	}
	sort.Strings(apps)
	bindings := []budget.CollectionBinding{}
	for _, app := range apps {
		built, err := compiler.BuildApplication(ctx, app, scratch)
		if err != nil {
			return nil, err
		}
		defer func() {
			if built.Close() != nil {
				report, resultErr = nil, failure("cleanup", "/run/app")
			}
		}()
		server, err := built.Serve(ctx, filepath.Join(compiler.owned, app+"-server.log"))
		if err != nil {
			return nil, err
		}
		defer func() {
			if server.Close() != nil {
				report, resultErr = nil, failure("cleanup", "/run/server")
			}
		}()
		proof, err := budget.ProduceFixture(ctx, budget.ProducerOptions{Inputs: inputs, Build: built.Manifest, App: app, DistDir: built.DistDir, BaseURL: server.BaseURL, SourceSHA: sha, Client: server.Client})
		if err != nil {
			return nil, err
		}
		bindings = append(bindings, budget.CollectionBinding{App: app, DistDir: built.DistDir, BaseURL: server.BaseURL, ArtifactSHA256: proof})
	}
	return budget.Collect(ctx, budget.CollectOptions{Inputs: inputs, Bindings: bindings, SHA: sha, Base: base})
}
