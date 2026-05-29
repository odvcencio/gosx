package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"m31labs.dev/gosx/assetpipe"
	"m31labs.dev/gosx/internal/version"
	"m31labs.dev/gosx/scene/capability"
	"m31labs.dev/gosx/scene/cert"
	sceneinspect "m31labs.dev/gosx/scene/inspect"
	sceneschema "m31labs.dev/gosx/scene/schema"
)

type sceneValidationFileReport struct {
	Path   string             `json:"path"`
	Report sceneschema.Report `json:"report"`
}

type sceneValidationCommandReport struct {
	Version string                      `json:"version"`
	Valid   bool                        `json:"valid"`
	Summary sceneValidationSummary      `json:"summary"`
	Files   []sceneValidationFileReport `json:"files"`
}

type sceneValidationSummary struct {
	Files          int                          `json:"files"`
	Passed         int                          `json:"passed"`
	Failed         int                          `json:"failed"`
	Diagnostics    int                          `json:"diagnostics"`
	SeverityCounts map[sceneschema.Severity]int `json:"severityCounts"`
}

type sceneInspectionCommandReport struct {
	Version       string                      `json:"version"`
	Valid         bool                        `json:"valid"`
	Scenes        []sceneinspect.SceneReport  `json:"scenes"`
	Certification *cert.Report                `json:"certification,omitempty"`
	Budgets       []sceneinspect.BudgetResult `json:"budgets,omitempty"`
	AssetPlan     *assetpipe.Report           `json:"assetPlan,omitempty"`
}

var errNoSceneJSONFiles = errors.New("no SceneIR JSON files found")

func cmdScene() {
	if err := runSceneCommand(os.Args[2:], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "scene error: %v\n", err)
		os.Exit(1)
	}
}

func runSceneCommand(args []string, stdout io.Writer) error {
	if len(args) == 0 || isHelpArg(args[0]) {
		sceneUsage(stdout)
		return nil
	}
	switch args[0] {
	case "certify":
		return runSceneCertifyCommand(args[1:], stdout)
	case "inspect":
		return runSceneInspectCommand(args[1:], stdout)
	case "schema":
		return runSceneSchemaCommand(args[1:], stdout)
	case "validate":
		return runSceneValidateCommand(args[1:], stdout)
	case "help", "-h", "--help":
		sceneUsage(stdout)
		return nil
	default:
		return fmt.Errorf("unknown scene subcommand %q\nrun 'gosx scene help' for usage", args[0])
	}
}

func runSceneInspectCommand(args []string, stdout io.Writer) error {
	if len(args) > 0 && isHelpArg(args[0]) {
		sceneInspectUsage(stdout)
		return nil
	}
	fs := flag.NewFlagSet("gosx scene inspect", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	jsonOut := fs.Bool("json", false, "emit JSON")
	strict := fs.Bool("strict", false, "treat strict-only checks and unknown budget measurements as errors")
	budgetPath := fs.String("budget", "", "scene budget JSON file")
	withCert := fs.Bool("cert", false, "include Scene3D certification summary")
	assetsRoot := fs.String("assets", "", "asset root to inventory with the Scene3D asset planner")
	maxTexturePixels := fs.Int("max-texture-pixels", 0, "maximum allowed HTML texture pixels")
	if err := fs.Parse(args); err != nil {
		return err
	}
	paths := fs.Args()
	if len(paths) == 0 {
		return errors.New("scene inspect requires a SceneIR JSON file or directory")
	}

	var budget *sceneinspect.SceneBudget
	if strings.TrimSpace(*budgetPath) != "" {
		parsed, err := readSceneBudgetFile(*budgetPath)
		if err != nil {
			return err
		}
		budget = &parsed
	}

	report := sceneInspectionCommandReport{
		Version: version.Current,
		Valid:   true,
		Scenes:  []sceneinspect.SceneReport{},
	}
	if *withCert {
		certReport := cert.BuildReport()
		report.Certification = &certReport
		if *strict && len(certReport.Summary.StrictFailures) > 0 {
			report.Valid = false
		}
	}
	if strings.TrimSpace(*assetsRoot) != "" {
		plan, err := assetpipe.Plan([]string{*assetsRoot}, assetpipe.Options{})
		if err != nil {
			return err
		}
		report.AssetPlan = &plan
	}

	files, err := collectSceneJSONFiles(paths)
	if err != nil {
		return err
	}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		sceneReport, err := sceneinspect.InspectJSON(file, data, sceneinspect.Options{
			Strict:           *strict,
			MaxTexturePixels: *maxTexturePixels,
		})
		if err != nil {
			return err
		}
		report.Scenes = append(report.Scenes, sceneReport)
		if !sceneReport.Validation.Valid {
			report.Valid = false
		}
		if budget != nil {
			results := sceneinspect.EvaluateBudget(sceneReport, *budget, *strict)
			report.Budgets = append(report.Budgets, results...)
			if sceneinspect.BudgetFailed(results, *strict) {
				report.Valid = false
			}
		}
	}

	if *jsonOut {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal scene inspection report: %w", err)
		}
		if _, err := stdout.Write(append(data, '\n')); err != nil {
			return err
		}
	} else {
		printSceneInspection(stdout, report)
	}
	if !report.Valid {
		return errors.New("scene inspection failed")
	}
	return nil
}

func runSceneValidateCommand(args []string, stdout io.Writer) error {
	if len(args) > 0 && isHelpArg(args[0]) {
		sceneValidateUsage(stdout)
		return nil
	}
	fs := flag.NewFlagSet("gosx scene validate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	jsonOut := fs.Bool("json", false, "emit JSON")
	strict := fs.Bool("strict", false, "treat strict-only checks as errors")
	maxTexturePixels := fs.Int("max-texture-pixels", 0, "maximum allowed HTML texture pixels")
	if err := fs.Parse(args); err != nil {
		return err
	}
	paths := fs.Args()
	if len(paths) == 0 {
		return errors.New("scene validate requires a SceneIR JSON file or directory")
	}
	report, err := validateScenePaths(paths, sceneschema.Options{
		Strict:           *strict,
		MaxTexturePixels: *maxTexturePixels,
	})
	if err != nil {
		return err
	}
	if *jsonOut {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal scene validation report: %w", err)
		}
		if _, err := stdout.Write(append(data, '\n')); err != nil {
			return err
		}
	} else {
		printSceneValidation(stdout, report)
	}
	if !report.Valid {
		return errors.New("scene validation failed")
	}
	return nil
}

func runSceneSchemaCommand(args []string, stdout io.Writer) error {
	if len(args) > 0 && isHelpArg(args[0]) {
		sceneSchemaUsage(stdout)
		return nil
	}
	fs := flag.NewFlagSet("gosx scene schema", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	outPath := fs.String("out", "", "write schema JSON to path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected scene schema operand %q", fs.Arg(0))
	}
	data, err := readSceneSchemaJSON()
	if err != nil {
		return err
	}
	if *outPath != "" {
		return os.WriteFile(*outPath, data, 0644)
	}
	if _, err := stdout.Write(data); err != nil {
		return err
	}
	if len(data) == 0 || data[len(data)-1] != '\n' {
		_, err = fmt.Fprintln(stdout)
	}
	return err
}

// sceneBackendCapsExtract is a minimal struct for extracting only the
// backendCaps field from a serialized scene JSON. The full SceneIR type has
// custom MarshalJSON methods on its sub-types (ObjectIR, ModelIR) that do not
// affect deserialization, but using the minimal extract is simpler and avoids
// any round-trip surprises.
type sceneBackendCapsExtract struct {
	BackendCaps *capability.BackendCaps `json:"backendCaps"`
}

func runSceneCertifyCommand(args []string, stdout io.Writer) error {
	if len(args) > 0 && isHelpArg(args[0]) {
		sceneCertifyUsage(stdout)
		return nil
	}
	fs := flag.NewFlagSet("gosx scene certify", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	jsonOut := fs.Bool("json", false, "emit JSON")
	strict := fs.Bool("strict", false, "fail if the current certification floor is not met")
	backend := fs.String("backend", "", "check scene-file against a specific backend (webgpu|webgl|canvas2d)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	// When --backend is not set, preserve existing behavior: no operands allowed.
	if *backend == "" {
		if fs.NArg() > 0 {
			return fmt.Errorf("unexpected scene certify operand %q", fs.Arg(0))
		}
		report := cert.BuildReport()
		if *jsonOut {
			data, err := json.MarshalIndent(report, "", "  ")
			if err != nil {
				return fmt.Errorf("marshal certification report: %w", err)
			}
			if _, err := stdout.Write(append(data, '\n')); err != nil {
				return err
			}
		} else {
			printSceneCertification(stdout, report)
		}
		if *strict && len(report.Summary.StrictFailures) > 0 {
			return errors.New("scene certification strict gate failed")
		}
		return nil
	}

	// --backend is set: require exactly one scene-file operand.
	targetBackend := capability.Backend(*backend)
	switch targetBackend {
	case capability.BackendWebGPU, capability.BackendWebGL, capability.BackendCanvas2D:
		// valid
	default:
		return fmt.Errorf("unknown backend %q: must be one of webgpu, webgl, canvas2d", *backend)
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("scene certify --backend requires exactly one scene-file operand")
	}
	scenePath := fs.Arg(0)
	data, err := os.ReadFile(scenePath)
	if err != nil {
		return fmt.Errorf("read scene file %q: %w", scenePath, err)
	}
	var extract sceneBackendCapsExtract
	if err := json.Unmarshal(data, &extract); err != nil {
		return fmt.Errorf("parse scene file %q: %w", scenePath, err)
	}
	caps := extract.BackendCaps

	// Compute structural cert report (additive — always included).
	report := cert.BuildReport()

	// Determine whether the target backend is capable.
	capable := false
	if caps != nil {
		for _, b := range caps.Capable {
			if b == targetBackend {
				capable = true
				break
			}
		}
	}

	// Gather reasons that exclude the target backend from caps.Reasons.
	var excludingReasons []capability.CapReason
	if caps != nil {
		for _, r := range caps.Reasons {
			if r.Excludes == targetBackend {
				excludingReasons = append(excludingReasons, r)
			}
		}
	}

	if *jsonOut {
		type backendCapsSection struct {
			Target   capability.Backend     `json:"target"`
			Capable  bool                   `json:"capable"`
			Degraded []capability.Feature   `json:"degraded,omitempty"`
			Reasons  []capability.CapReason `json:"reasons,omitempty"`
			NoCaps   bool                   `json:"noCaps,omitempty"`
		}
		type certifyWithBackend struct {
			cert.Report
			BackendCaps backendCapsSection `json:"backendCaps"`
		}
		section := backendCapsSection{
			Target:  targetBackend,
			Capable: capable,
		}
		if caps == nil {
			section.NoCaps = true
		} else {
			section.Degraded = caps.Degraded[targetBackend]
			section.Reasons = caps.Reasons
		}
		combined := certifyWithBackend{Report: report, BackendCaps: section}
		out, err := json.MarshalIndent(combined, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal certification report: %w", err)
		}
		if _, err := stdout.Write(append(out, '\n')); err != nil {
			return err
		}
	} else {
		printSceneCertification(stdout, report)
		fmt.Fprintf(stdout, "\nBackend: %s\n", targetBackend)
		if caps == nil {
			fmt.Fprintln(stdout, "  no backendCaps in scene (older scene format)")
		} else {
			if capable {
				fmt.Fprintf(stdout, "  Capable: yes\n")
			} else {
				fmt.Fprintf(stdout, "  Capable: no\n")
			}
			if degraded := caps.Degraded[targetBackend]; len(degraded) > 0 {
				features := make([]string, len(degraded))
				for i, f := range degraded {
					features[i] = string(f)
				}
				fmt.Fprintf(stdout, "  Degraded: %s\n", strings.Join(features, ", "))
			}
			if len(caps.Reasons) > 0 {
				fmt.Fprintln(stdout, "  Reasons:")
				for _, r := range caps.Reasons {
					if r.Excludes != "" {
						fmt.Fprintf(stdout, "    %s excludes %s\n", r.Feature, r.Excludes)
					} else if r.Degrades != "" {
						fmt.Fprintf(stdout, "    %s degrades %s\n", r.Feature, r.Degrades)
					}
				}
			}
		}
	}

	// Strict gate for structural failures (no-backend behavior).
	if *strict && len(report.Summary.StrictFailures) > 0 {
		return errors.New("scene certification strict gate failed")
	}
	// Strict gate for backend capability.
	if *strict {
		if caps == nil {
			return errors.New("scene certify --strict: no backendCaps in scene")
		}
		if !capable {
			// Build error message naming the excluding features.
			var features []string
			for _, r := range excludingReasons {
				features = append(features, string(r.Feature))
			}
			if len(features) == 0 {
				return fmt.Errorf("scene certify --strict: backend %s is not capable", targetBackend)
			}
			return fmt.Errorf("scene certify --strict: backend %s excluded by: %s", targetBackend, strings.Join(features, ", "))
		}
	}
	return nil
}

func validateScenePaths(paths []string, opts sceneschema.Options) (sceneValidationCommandReport, error) {
	report := sceneValidationCommandReport{
		Version: version.Current,
		Valid:   true,
		Summary: newSceneValidationSummary(),
	}
	files, err := collectSceneJSONFiles(paths)
	if err != nil {
		return report, err
	}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return report, err
		}
		fileReport := sceneschema.ValidateJSON(data, opts)
		report.Files = append(report.Files, sceneValidationFileReport{Path: file, Report: fileReport})
		report.Summary.Files++
		if !fileReport.Valid {
			report.Valid = false
			report.Summary.Failed++
		} else {
			report.Summary.Passed++
		}
		for _, diag := range fileReport.Diagnostics {
			report.Summary.Diagnostics++
			report.Summary.SeverityCounts[diag.Severity]++
		}
	}
	return report, nil
}

func collectSceneJSONFiles(paths []string) ([]string, error) {
	var files []string
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			files = append(files, path)
			continue
		}
		err = filepath.WalkDir(path, func(walkPath string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				name := d.Name()
				if name == ".git" || name == "node_modules" || name == "dist" || name == "build" {
					return filepath.SkipDir
				}
				return nil
			}
			if isSceneJSONPath(walkPath) {
				files = append(files, walkPath)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(files)
	if len(files) == 0 {
		return nil, errNoSceneJSONFiles
	}
	return files, nil
}

func readSceneBudgetFile(path string) (sceneinspect.SceneBudget, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return sceneinspect.SceneBudget{}, fmt.Errorf("read scene budget %s: %w", path, err)
	}
	budget, err := sceneinspect.ParseBudget(data)
	if err != nil {
		return sceneinspect.SceneBudget{}, fmt.Errorf("parse scene budget %s: %w", path, err)
	}
	return budget, nil
}

func newSceneValidationSummary() sceneValidationSummary {
	summary := sceneValidationSummary{SeverityCounts: map[sceneschema.Severity]int{}}
	for _, severity := range sceneSeverityOrder() {
		summary.SeverityCounts[severity] = 0
	}
	return summary
}

func readSceneSchemaJSON() ([]byte, error) {
	if data := sceneschema.JSONSchema(); len(data) > 0 {
		return data, nil
	}
	for _, path := range sceneSchemaCandidatePaths() {
		data, err := os.ReadFile(path)
		if err == nil {
			return data, nil
		}
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("read scene schema %s: %w", path, err)
		}
	}
	return nil, errors.New("scene schema JSON not found")
}

func sceneSchemaCandidatePaths() []string {
	return []string{filepath.Join("scene", "schema", "schema.json")}
}

func isSceneJSONPath(path string) bool {
	name := strings.ToLower(filepath.Base(path))
	return name == "scene.json" || strings.HasSuffix(name, ".scene.json") || strings.HasSuffix(name, ".sceneir.json")
}

func printSceneCertification(w io.Writer, report cert.Report) {
	fmt.Fprintf(w, "Scene3D certification: %d features\n", report.Summary.Features)
	statuses := []cert.Status{cert.Complete, cert.Partial, cert.Fallback, cert.Unsupported, cert.NotApplicable}
	for _, status := range statuses {
		if count := report.Summary.StatusCounts[status]; count > 0 {
			fmt.Fprintf(w, "  %s: %d\n", status, count)
		}
	}
	if len(report.Summary.StrictFailures) == 0 {
		fmt.Fprintln(w, "Strict gate: pass")
	} else {
		fmt.Fprintf(w, "Strict gate: fail (%d)\n", len(report.Summary.StrictFailures))
		for _, failure := range report.Summary.StrictFailures {
			fmt.Fprintf(w, "  %s %s is %s; %s\n", failure.Feature, failure.Dimension, failure.Status, failure.Required)
		}
	}

	byCategory := map[string]int{}
	for _, entry := range report.Entries {
		byCategory[entry.Category]++
	}
	categories := make([]string, 0, len(byCategory))
	for category := range byCategory {
		categories = append(categories, category)
	}
	sort.Strings(categories)
	fmt.Fprintln(w, "Categories:")
	for _, category := range categories {
		fmt.Fprintf(w, "  %s: %d\n", category, byCategory[category])
	}
}

func printSceneValidation(w io.Writer, report sceneValidationCommandReport) {
	status := "pass"
	if !report.Valid {
		status = "fail"
	}
	fmt.Fprintf(w, "Scene3D validation: %s\n", status)
	fmt.Fprintf(w, "Files: %d passed, %d failed, %d total\n", report.Summary.Passed, report.Summary.Failed, report.Summary.Files)
	fmt.Fprintf(w, "Diagnostics: %d total", report.Summary.Diagnostics)
	for _, severity := range sceneSeverityOrder() {
		fmt.Fprintf(w, ", %s=%d", severity, report.Summary.SeverityCounts[severity])
	}
	fmt.Fprintln(w)
	for _, file := range report.Files {
		fileStatus := "pass"
		if !file.Report.Valid {
			fileStatus = "fail"
		}
		fmt.Fprintf(w, "\n%s: %s\n", file.Path, fileStatus)
		for _, diag := range file.Report.Diagnostics {
			fmt.Fprintf(w, "  %s %s", diag.Severity, diag.Code)
			if diag.Path != "" {
				fmt.Fprintf(w, " [%s]", diag.Path)
			}
			if diag.ID != "" {
				fmt.Fprintf(w, " id=%s", diag.ID)
			}
			fmt.Fprintf(w, ": %s\n", diag.Message)
		}
	}
}

func printSceneInspection(w io.Writer, report sceneInspectionCommandReport) {
	status := "pass"
	if !report.Valid {
		status = "fail"
	}
	fmt.Fprintf(w, "Scene3D inspection: %s\n", status)
	fmt.Fprintf(w, "Scenes: %d\n", len(report.Scenes))
	if report.Certification != nil {
		summary := report.Certification.Summary
		fmt.Fprintf(w, "Certification: %d features, %d strict failures\n", summary.Features, len(summary.StrictFailures))
	}
	if report.AssetPlan != nil {
		fmt.Fprintf(w, "Asset plan: %d assets, %s expected GPU bytes\n", report.AssetPlan.Totals.Assets, formatByteCount(report.AssetPlan.Budget.ExpectedGPUBytes))
	}

	budgetsByScene := map[string][]sceneinspect.BudgetResult{}
	for _, result := range report.Budgets {
		budgetsByScene[result.Scene] = append(budgetsByScene[result.Scene], result)
	}

	for _, sceneReport := range report.Scenes {
		fmt.Fprintf(w, "\n%s\n", sceneReport.Path)
		fmt.Fprintf(w, "  Surface: %s\n", sceneReport.Surface.ID)
		fmt.Fprintf(w, "  Backend intent: %s\n", strings.Join(sceneReport.Surface.BackendIntent, " -> "))
		fmt.Fprintf(w, "  Objects: %d meshes, %d models, %d instanced meshes, %d point layers, %d HTML surfaces\n",
			sceneReport.Surface.Objects,
			sceneReport.Surface.Models,
			sceneReport.Surface.InstancedMeshes+sceneReport.Surface.InstancedGLBMeshes,
			sceneReport.Surface.Points,
			sceneReport.Surface.HTML,
		)
		fmt.Fprintf(w, "  Draw calls: %d estimated, uploads: %d estimated\n", sceneReport.Surface.EstimatedDrawCalls, sceneReport.Surface.EstimatedUploadCount)
		fmt.Fprintf(w, "  GPU memory estimate: %s total (geometry %s, textures %s, html textures %s, shadow %s, postfx %s)\n",
			formatByteCount(sceneReport.Memory.TotalGPUBytes),
			formatByteCount(sceneReport.Memory.GeometryBytes+sceneReport.Memory.InstanceBytes+sceneReport.Memory.PointBytes+sceneReport.Memory.ParticleBytes),
			formatByteCount(sceneReport.Memory.TextureBytes),
			formatByteCount(sceneReport.Memory.HTMLTextureBytes),
			formatByteCount(sceneReport.Memory.ShadowBytes),
			formatByteCount(sceneReport.Memory.PostFXBytes),
		)
		if len(sceneReport.Assets.Sources) > 0 || sceneReport.Assets.Models > 0 || sceneReport.Assets.Textures > 0 || sceneReport.Assets.HTMLTextureSurfaces > 0 {
			fmt.Fprintf(w, "  Assets: %d models, %d textures, %d HTML texture surfaces, %d unique sources\n",
				sceneReport.Assets.Models,
				sceneReport.Assets.Textures,
				sceneReport.Assets.HTMLTextureSurfaces,
				len(sceneReport.Assets.Sources),
			)
		}
		if len(sceneReport.FeatureUse) > 0 {
			fmt.Fprintf(w, "  Features: %s\n", formatFeatureUse(sceneReport.FeatureUse))
		}
		if len(sceneReport.Fallbacks) > 0 {
			fmt.Fprintln(w, "  Fallbacks:")
			for _, fallback := range sceneReport.Fallbacks {
				if fallback.ID != "" {
					fmt.Fprintf(w, "    %s %s: %s\n", fallback.Feature, fallback.ID, fallback.Reason)
				} else {
					fmt.Fprintf(w, "    %s: %s\n", fallback.Feature, fallback.Reason)
				}
			}
		}
		if !sceneReport.Validation.Valid || len(sceneReport.Validation.Diagnostics) > 0 {
			state := "pass"
			if !sceneReport.Validation.Valid {
				state = "fail"
			}
			fmt.Fprintf(w, "  Validation: %s, %d diagnostics\n", state, len(sceneReport.Validation.Diagnostics))
			for _, diag := range sceneReport.Validation.Diagnostics {
				fmt.Fprintf(w, "    %s %s", diag.Severity, diag.Code)
				if diag.Path != "" {
					fmt.Fprintf(w, " [%s]", diag.Path)
				}
				fmt.Fprintf(w, ": %s\n", diag.Message)
			}
		}
		if results := budgetsByScene[sceneReport.Path]; len(results) > 0 {
			fmt.Fprintln(w, "  Budgets:")
			for _, result := range results {
				fmt.Fprintf(w, "    %s: %s", result.Category, result.Status)
				if result.Limit > 0 {
					fmt.Fprintf(w, " actual=%.0f limit=%.0f", result.Actual, result.Limit)
				}
				if result.Message != "" {
					fmt.Fprintf(w, " (%s)", result.Message)
				}
				fmt.Fprintln(w)
			}
		}
	}
}

func sceneSeverityOrder() []sceneschema.Severity {
	return []sceneschema.Severity{sceneschema.Info, sceneschema.Warn, sceneschema.Error, sceneschema.Fatal}
}

func formatFeatureUse(features map[string]int) string {
	keys := make([]string, 0, len(features))
	for key := range features {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", key, features[key]))
	}
	return strings.Join(parts, ", ")
}

func formatByteCount(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	value := float64(bytes)
	for _, suffix := range []string{"KiB", "MiB", "GiB", "TiB"} {
		value /= unit
		if value < unit {
			return fmt.Sprintf("%.1f %s", value, suffix)
		}
	}
	return fmt.Sprintf("%.1f PiB", value/unit)
}

func sceneUsage(w io.Writer) {
	fmt.Fprintf(w, `gosx scene - Inspect and certify Scene3D contracts

Usage:
  gosx scene certify [--json] [--strict] [--backend webgpu|webgl|canvas2d <scene-file>]
  gosx scene inspect [--json] [--strict] [--cert] [--budget file] [--assets root] <file-or-dir>...
  gosx scene schema [--out path]
  gosx scene validate [--json] [--strict] [--max-texture-pixels N] <file-or-dir>...

	`)
}

func sceneInspectUsage(w io.Writer) {
	fmt.Fprintf(w, `gosx scene inspect - Inspect SceneIR feature use, fallbacks, assets, and budgets

Usage:
  gosx scene inspect [--json] [--strict] [--cert] [--budget file] [--assets root] [--max-texture-pixels N] <file-or-dir>...

Directory scans include scene.json, *.scene.json, and *.sceneir.json files.

`)
}

func sceneCertifyUsage(w io.Writer) {
	fmt.Fprintf(w, `gosx scene certify - Check Scene3D feature certification

Usage:
  gosx scene certify [--json] [--strict]
  gosx scene certify [--json] [--strict] --backend <webgpu|webgl|canvas2d> <scene-file>

Flags:
  --json             Emit JSON output.
  --strict           Fail if the certification floor is not met or if --backend
                     is set and the target backend is not capable.
  --backend <name>   Check a serialized scene file against the named backend.
                     Requires exactly one scene-file operand. The scene file
                     must contain a backendCaps field (produced by Props.SceneIR()).

`)
}

func sceneSchemaUsage(w io.Writer) {
	fmt.Fprintf(w, `gosx scene schema - Emit the strict SceneIR JSON schema

Usage:
  gosx scene schema [--out path]

`)
}

func sceneValidateUsage(w io.Writer) {
	fmt.Fprintf(w, `gosx scene validate - Validate SceneIR JSON files

Usage:
  gosx scene validate [--json] [--strict] [--max-texture-pixels N] <file-or-dir>...

Directory scans include scene.json, *.scene.json, and *.sceneir.json files.

`)
}
