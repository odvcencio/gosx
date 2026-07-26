package assetpipe

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Action status values. Plan only ever emits the first four. Execute writes
// the last three, so a reader can always tell a plan from a result.
const (
	StatusCandidate = "candidate"
	StatusPlanned   = "planned"
	StatusPresent   = "present"
	StatusReady     = "ready"
	StatusExecuted  = "executed"
	StatusSkipped   = "skipped"
	StatusFailed    = "failed"
)

// Variant state values. An empty State means the variant is a plan, which
// keeps the JSON that `gosx assets plan` writes unchanged.
const (
	VariantPlanned = "planned"
	VariantBuilt   = "built"
)

// ExecuteSchemaVersion versions the execution report contract.
const ExecuteSchemaVersion = 1

// DefaultMaxExecuteBytes bounds how much of one source asset the executor
// reads into memory. Planning caps reads at MaxProbeBytes because it only
// needs headers. Execution must read a whole asset, so the bound is larger,
// but it still refuses a file that would dominate a build machine. A 16K
// equirectangular HDR file holds about 400 MB, so 512 MiB accepts every
// practical environment map and rejects a runaway one.
const DefaultMaxExecuteBytes = int64(512 << 20)

// ExecuteOptions controls the build step that turns a plan into files.
type ExecuteOptions struct {
	// Root is the directory the report's asset paths are relative to. When
	// empty, Execute uses the first root of the report.
	Root string
	// OutputDir receives the generated variants. When empty, Execute writes
	// each variant next to its source, at the URI the plan named.
	OutputDir string
	// MaxExecuteBytes bounds one source read. Zero selects
	// DefaultMaxExecuteBytes.
	MaxExecuteBytes int64
	// Only restricts execution to the named actions. An empty slice runs
	// every action the executor supports.
	Only []string
	// IBL controls the environment stage.
	IBL IBLOptions
	// LOD controls the mesh simplification stage.
	LOD LODOptions
	// Optimize controls the mesh optimization stage.
	Optimize OptimizeOptions
}

// ExecutionReport records what the build step did. It never claims a file the
// executor did not write.
type ExecutionReport struct {
	SchemaVersion int            `json:"schemaVersion"`
	Root          string         `json:"root"`
	OutputDir     string         `json:"outputDir"`
	Results       []ActionResult `json:"results,omitempty"`
	Warnings      []string       `json:"warnings,omitempty"`
	Totals        ExecuteTotals  `json:"totals"`
}

// ExecuteTotals summarizes an execution run.
type ExecuteTotals struct {
	Executed      int   `json:"executed"`
	Skipped       int   `json:"skipped"`
	Failed        int   `json:"failed"`
	OutputFiles   int   `json:"outputFiles"`
	OutputBytes   int64 `json:"outputBytes"`
	DurationMS    int64 `json:"durationMs"`
	SourceBytes   int64 `json:"sourceBytes"`
	PeakReadBytes int64 `json:"peakReadBytes,omitempty"`
}

// ActionResult records one executed, skipped, or failed action.
type ActionResult struct {
	Path        string            `json:"path"`
	Action      string            `json:"action"`
	Status      string            `json:"status"`
	Reason      string            `json:"reason,omitempty"`
	DurationMS  int64             `json:"durationMs,omitempty"`
	SourceBytes int64             `json:"sourceBytes,omitempty"`
	OutputBytes int64             `json:"outputBytes,omitempty"`
	Outputs     []Variant         `json:"outputs,omitempty"`
	Metrics     map[string]string `json:"metrics,omitempty"`
}

// executor runs one action for one asset.
type executor struct {
	action string
	kinds  []string
	run    func(*executeContext, Asset) (actionOutcome, error)
}

// actionOutcome is what an executor produces for one asset.
type actionOutcome struct {
	outputs []Variant
	metrics map[string]string
	// skipReason marks a deliberate skip. The action status becomes
	// "skipped" and the plan keeps its original status.
	skipReason string
}

type executeContext struct {
	root      string
	outputDir string
	maxBytes  int64
	opts      ExecuteOptions
	warnings  []string
	// brdfLUT caches the scene-independent split-sum table across assets.
	brdfLUT       []byte
	brdfLUTPath   string
	sourceBytes   int64
	peakReadBytes int64
}

var errSourceTooLarge = errors.New("asset exceeds the execution read bound")

// Execute materializes the actions in report that the pipeline can run in
// pure Go. It returns an updated plan report and a record of the work.
//
// Execute never mutates the input report. Planning stays fast and free of
// side effects, so `gosx assets plan` keeps its behaviour.
func Execute(report Report, opts ExecuteOptions) (Report, ExecutionReport, error) {
	start := time.Now()
	root := strings.TrimSpace(opts.Root)
	if root == "" && len(report.Roots) > 0 {
		root = report.Roots[0]
	}
	if root == "" {
		return report, ExecutionReport{}, fmt.Errorf("assetpipe: execute needs a root directory")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return report, ExecutionReport{}, fmt.Errorf("assetpipe: resolve root: %w", err)
	}
	outputDir := strings.TrimSpace(opts.OutputDir)
	if outputDir == "" {
		outputDir = absRoot
	}
	absOutput, err := filepath.Abs(outputDir)
	if err != nil {
		return report, ExecutionReport{}, fmt.Errorf("assetpipe: resolve output directory: %w", err)
	}
	maxBytes := opts.MaxExecuteBytes
	if maxBytes <= 0 {
		maxBytes = DefaultMaxExecuteBytes
	}

	ctx := &executeContext{root: absRoot, outputDir: absOutput, maxBytes: maxBytes, opts: opts}
	allowed := map[string]bool{}
	for _, name := range opts.Only {
		allowed[strings.TrimSpace(name)] = true
	}

	out := Report{
		SchemaVersion: report.SchemaVersion,
		Roots:         append([]string(nil), report.Roots...),
		Assets:        make([]Asset, 0, len(report.Assets)),
		Warnings:      append([]string(nil), report.Warnings...),
		Diagnostics:   append([]Diagnostic(nil), report.Diagnostics...),
		SkinManifest:  report.SkinManifest,
	}
	execReport := ExecutionReport{SchemaVersion: ExecuteSchemaVersion, Root: filepath.ToSlash(absRoot), OutputDir: filepath.ToSlash(absOutput)}

	for _, asset := range report.Assets {
		updated := cloneAsset(asset)
		executedActions := map[string]bool{}
		var built []Variant
		for i, action := range updated.Actions {
			exec, ok := executorFor(action.Name, updated.Kind)
			if !ok {
				continue
			}
			if len(allowed) > 0 && !allowed[action.Name] {
				continue
			}
			if action.Status == StatusExecuted {
				continue
			}
			actionStart := time.Now()
			outcome, err := exec.run(ctx, updated)
			elapsed := time.Since(actionStart)
			result := ActionResult{
				Path:       updated.Path,
				Action:     action.Name,
				DurationMS: elapsed.Milliseconds(),
				Metrics:    outcome.metrics,
			}
			switch {
			case err != nil:
				result.Status = StatusFailed
				result.Reason = err.Error()
				updated.Actions[i].Status = StatusFailed
				updated.Actions[i].Reason = err.Error()
				execReport.Totals.Failed++
			case outcome.skipReason != "":
				result.Status = StatusSkipped
				result.Reason = outcome.skipReason
				execReport.Totals.Skipped++
			default:
				result.Status = StatusExecuted
				result.Outputs = outcome.outputs
				updated.Actions[i].Status = StatusExecuted
				executedActions[action.Name] = true
				built = append(built, outcome.outputs...)
				execReport.Totals.Executed++
				for _, output := range outcome.outputs {
					result.OutputBytes += output.Bytes
					execReport.Totals.OutputBytes += output.Bytes
					execReport.Totals.OutputFiles++
				}
			}
			execReport.Results = append(execReport.Results, result)
		}
		if len(executedActions) > 0 {
			updated.Variants = mergeVariants(updated.Variants, built, executedActions)
		}
		out.Assets = append(out.Assets, updated)
	}

	out.Totals = reportTotals(out.Assets)
	out.Budget = reportBudget(out.Assets)
	out.Totals.ExpectedGPUBytes = out.Budget.ExpectedGPUBytes
	out.Totals.FirstFrameUploadBytes = out.Budget.FirstFrameUploadBytes
	execReport.Warnings = ctx.warnings
	execReport.Totals.DurationMS = time.Since(start).Milliseconds()
	execReport.Totals.SourceBytes = ctx.sourceBytes
	execReport.Totals.PeakReadBytes = ctx.peakReadBytes
	return out, execReport, nil
}

// mergeVariants drops the planned variants of every executed action and keeps
// the real outputs in their place. Variants of actions that did not run stay
// as plans.
func mergeVariants(planned, built []Variant, executed map[string]bool) []Variant {
	out := make([]Variant, 0, len(planned)+len(built))
	for _, variant := range planned {
		if executed[variant.SourceAction] {
			continue
		}
		out = append(out, variant)
	}
	out = append(out, built...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].URI < out[j].URI })
	return out
}

func cloneAsset(asset Asset) Asset {
	out := asset
	out.Actions = append([]Action(nil), asset.Actions...)
	out.Variants = append([]Variant(nil), asset.Variants...)
	out.Warnings = append([]string(nil), asset.Warnings...)
	out.Diagnostics = append([]Diagnostic(nil), asset.Diagnostics...)
	return out
}

func executorFor(action, kind string) (executor, bool) {
	for _, candidate := range executors {
		if candidate.action != action {
			continue
		}
		if len(candidate.kinds) == 0 {
			return candidate, true
		}
		for _, allowed := range candidate.kinds {
			if allowed == kind {
				return candidate, true
			}
		}
	}
	return executor{}, false
}

// executors lists every action the build step can materialize today.
var executors = []executor{
	{action: "prefilter-ibl-ggx", kinds: []string{"environment"}, run: runPrefilterIBL},
	{action: "generate-split-sum-lut", kinds: []string{"environment"}, run: runSplitSumLUT},
	{action: "build-lod-stack", kinds: []string{"glb", "gltf"}, run: runBuildLODStack},
	{action: "optimize-mesh", kinds: []string{"glb", "gltf"}, run: runOptimizeMesh},
}

// SupportedActions lists the action names Execute can materialize.
func SupportedActions() []string {
	out := make([]string, 0, len(executors))
	for _, candidate := range executors {
		out = append(out, candidate.action)
	}
	sort.Strings(out)
	return out
}

// readSource reads one asset with the execution bound applied.
func (c *executeContext) readSource(relPath string) ([]byte, error) {
	path := filepath.Join(c.root, filepath.FromSlash(relPath))
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > c.maxBytes {
		return nil, fmt.Errorf("%w: %s is %d bytes, bound is %d", errSourceTooLarge, relPath, info.Size(), c.maxBytes)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	c.sourceBytes += int64(len(data))
	if int64(len(data)) > c.peakReadBytes {
		c.peakReadBytes = int64(len(data))
	}
	return data, nil
}

// writeOutput writes one generated file below the output directory and
// returns its size.
func (c *executeContext) writeOutput(relURI string, data []byte) (int64, error) {
	path := filepath.Join(c.outputDir, filepath.FromSlash(relURI))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return 0, fmt.Errorf("create output directory: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return 0, fmt.Errorf("write %s: %w", relURI, err)
	}
	return int64(len(data)), nil
}

func (c *executeContext) warn(format string, args ...any) {
	c.warnings = append(c.warnings, fmt.Sprintf(format, args...))
}

// plannedVariantURI finds the URI the plan reserved for an action, so the
// executor writes where the plan promised.
func plannedVariantURI(asset Asset, sourceAction, fallbackSuffix, fallbackExt string) string {
	for _, variant := range asset.Variants {
		if variant.SourceAction == sourceAction {
			return variant.URI
		}
	}
	return siblingVariant(asset.Path, fallbackSuffix, fallbackExt)
}
