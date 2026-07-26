package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"

	"m31labs.dev/gosx/internal/version"
	"m31labs.dev/gosx/scene"
	"m31labs.dev/gosx/scene/harness"
	"m31labs.dev/gosx/scene/imagediff"
	sceneinspect "m31labs.dev/gosx/scene/inspect"
	"m31labs.dev/gosx/scene/preview"
	sceneschema "m31labs.dev/gosx/scene/schema"
)

// gosx scene check runs the whole browser-free authoring loop in one command:
// validate the document, cost it, render it, prove the render reproduces, and
// compare it to a baseline. Every number in the report comes from a step that
// actually ran. When a step does not run, the report says so instead of
// reporting a pass.

const sceneCheckSchema = "gosx.scene3d.check/v1"

type sceneCheckReport struct {
	Schema  string `json:"schema"`
	Version string `json:"version"`
	// Valid is the single verdict. It is false when any step that ran failed.
	Valid bool   `json:"valid"`
	Scene string `json:"scene"`
	// Steps records which parts of the loop ran. A step that was skipped is
	// never counted as a pass.
	Steps      sceneCheckSteps             `json:"steps"`
	Validation sceneschema.Report          `json:"validation"`
	Inspection sceneinspect.SceneReport    `json:"inspection"`
	Budgets    []sceneinspect.BudgetResult `json:"budgets,omitempty"`
	Harness    harness.Report              `json:"harness"`
	Golden     *sceneCheckGolden           `json:"golden,omitempty"`
	Outputs    sceneCheckOutputs           `json:"outputs"`
	Problems   []string                    `json:"problems,omitempty"`
}

type sceneCheckSteps struct {
	Validated         bool `json:"validated"`
	Inspected         bool `json:"inspected"`
	Rendered          bool `json:"rendered"`
	BudgetEvaluated   bool `json:"budgetEvaluated"`
	AssetsResolved    bool `json:"assetsResolved"`
	DeterminismRuns   int  `json:"determinismRuns"`
	GoldenCompared    bool `json:"goldenCompared"`
	GoldenBaselineNew bool `json:"goldenBaselineWritten"`
}

type sceneCheckGolden struct {
	Reference string `json:"reference"`
	// Written is true when this run created the baseline instead of comparing
	// against one. A written baseline is not evidence that nothing changed.
	Written bool              `json:"written"`
	Diff    *imagediff.Result `json:"diff,omitempty"`
}

type sceneCheckOutputs struct {
	Render string `json:"render,omitempty"`
	Diff   string `json:"diff,omitempty"`
	Golden string `json:"golden,omitempty"`
}

func runSceneCheckCommand(args []string, stdout io.Writer) error {
	if len(args) > 0 && isHelpArg(args[0]) {
		sceneCheckUsage(stdout)
		return nil
	}
	fs := flag.NewFlagSet("gosx scene check", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	jsonOut := fs.Bool("json", false, "emit the whole report as JSON")
	strict := fs.Bool("strict", false, "treat strict-only checks as errors")
	assetsRoot := fs.String("assets", "", "asset root that scene asset sources resolve against")
	budgetPath := fs.String("budget", "", "scene budget JSON file")
	maxTexturePixels := fs.Int("max-texture-pixels", 0, "maximum allowed HTML texture pixels")
	renderOut := fs.String("out", "", "write the rendered frame to this PNG path")
	width := fs.Int("width", 640, "render width in pixels")
	height := fs.Int("height", 400, "render height in pixels")
	timeSeconds := fs.Float64("time", 0, "animation time in seconds")
	background := fs.String("background", "", "override scene background color")
	fast := fs.Bool("fast", false, "skip shadows and post-FX and cap curved geometry")
	goldenPath := fs.String("golden", "", "baseline PNG to compare the render against")
	writeGolden := fs.Bool("write-golden", false, "create the baseline when it is missing")
	diffOut := fs.String("diff-out", "", "write the localized diff image to this PNG path")
	tolerance := fs.Int("tolerance", 0, "largest per-channel difference that counts as unchanged")
	repeat := fs.Int("repeat", 1, "render the frame this many times to prove the output reproduces")
	cameraX := fs.Float64("camera-x", 0, "override camera X")
	cameraY := fs.Float64("camera-y", 0, "override camera Y")
	cameraZ := fs.Float64("camera-z", 0, "override camera Z")
	fov := fs.Float64("fov", 0, "override vertical field of view in degrees")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("scene check requires exactly one SceneIR or Scene3D props JSON file")
	}
	if *width <= 0 || *height <= 0 {
		return errors.New("scene check width and height must be positive")
	}
	inputPath := fs.Arg(0)
	data, err := os.ReadFile(inputPath)
	if err != nil {
		return fmt.Errorf("read scene %s: %w", inputPath, err)
	}

	report := sceneCheckReport{
		Schema: sceneCheckSchema, Version: version.Current, Valid: true, Scene: inputPath,
	}
	fail := func(message string) {
		report.Valid = false
		report.Problems = append(report.Problems, message)
	}

	// 1) Validate the document and 2) cost it. InspectJSON runs the strict
	// SceneIR validation itself and adds asset-reachability errors to the same
	// report, so both steps read one verdict.
	inspectOpts := sceneinspect.Options{Strict: *strict, MaxTexturePixels: *maxTexturePixels}
	if root := strings.TrimSpace(*assetsRoot); root != "" {
		inspectOpts.AssetRoots = []string{root}
	}
	inspection, err := sceneinspect.InspectJSON(inputPath, data, inspectOpts)
	if err != nil {
		return err
	}
	report.Inspection = inspection
	report.Validation = inspection.Validation
	report.Steps.Validated = true
	report.Steps.Inspected = true
	report.Steps.AssetsResolved = inspection.AssetResolution != nil && inspection.AssetResolution.Checked
	if !report.Validation.Valid {
		fail("scene document is invalid")
	}

	// 3) Evaluate the budget against the measured inspection, not a constant.
	if path := strings.TrimSpace(*budgetPath); path != "" {
		budget, err := readSceneBudgetFile(path)
		if err != nil {
			return err
		}
		report.Budgets = sceneinspect.EvaluateBudget(inspection, budget, *strict)
		report.Steps.BudgetEvaluated = true
		if sceneinspect.BudgetFailed(report.Budgets, *strict) {
			fail("scene budget failed")
		}
	}

	// 4) Render the frame through the harness so the report carries real frame
	// telemetry rather than a claim that a render happened.
	opts := preview.Options{Width: *width, Height: *height, Time: *timeSeconds, Background: *background,
		DisableShadows: *fast, DisablePostFX: *fast}
	if *fast {
		opts.MaxSegments = 12
	}
	opts.AssetRoots = inspectOpts.AssetRoots
	if *cameraX != 0 || *cameraY != 0 || *cameraZ != 0 || *fov != 0 {
		cameraFOV := *fov
		if cameraFOV == 0 {
			cameraFOV = 50
		}
		opts.Camera = &scene.PerspectiveCamera{Position: scene.Vec3(*cameraX, *cameraY, *cameraZ), FOV: cameraFOV, Near: 0.1, Far: 100}
	}
	session, err := harness.NewFromJSON(data, opts)
	if err != nil {
		return err
	}
	result, err := session.Render(*timeSeconds)
	if err != nil {
		return err
	}
	report.Steps.Rendered = true

	if path := strings.TrimSpace(*renderOut); path != "" {
		if err := writePreviewPNG(path, result); err != nil {
			return err
		}
		report.Outputs.Render = path
	}

	// 5) Prove the render reproduces. The verdict comes from repeated renders.
	if *repeat > 1 {
		if _, err := session.CertifyDeterminism("repeat render", *timeSeconds, *repeat); err != nil {
			return err
		}
		report.Steps.DeterminismRuns = *repeat
	}

	// 6) Compare the frame to its baseline, or create the baseline and say so.
	if path := strings.TrimSpace(*goldenPath); path != "" {
		golden := &sceneCheckGolden{Reference: path}
		report.Golden = golden
		if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
			if !*writeGolden {
				fail(fmt.Sprintf("baseline %s does not exist; pass --write-golden to create it", path))
			} else {
				if err := writePreviewPNG(path, result); err != nil {
					return err
				}
				golden.Written = true
				report.Steps.GoldenBaselineNew = true
				report.Outputs.Golden = path
			}
		} else {
			diff, compareErr := session.RequireGoldenFile("baseline", path, imagediff.Options{Tolerance: *tolerance})
			if compareErr != nil {
				return compareErr
			}
			golden.Diff = &diff
			report.Steps.GoldenCompared = true
			// RequireGoldenFile already records a problem that names the
			// baseline, so do not add a second copy of the same message.
			if out := strings.TrimSpace(*diffOut); out != "" {
				if err := writeDiffPNG(out, diff); err != nil {
					return err
				}
				report.Outputs.Diff = out
			}
		}
	}

	report.Harness = session.Report()
	if !report.Harness.Valid {
		for _, problem := range report.Harness.Problems {
			if !containsString(report.Problems, problem) {
				report.Problems = append(report.Problems, problem)
			}
		}
		report.Valid = false
	}

	if *jsonOut {
		encoded, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal scene check report: %w", err)
		}
		if _, err := stdout.Write(append(encoded, '\n')); err != nil {
			return err
		}
	} else {
		printSceneCheck(stdout, report)
	}
	if !report.Valid {
		return errors.New("scene check failed")
	}
	return nil
}

func runSceneDiffCommand(args []string, stdout io.Writer) error {
	if len(args) > 0 && isHelpArg(args[0]) {
		sceneDiffUsage(stdout)
		return nil
	}
	fs := flag.NewFlagSet("gosx scene diff", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	jsonOut := fs.Bool("json", false, "emit the comparison as JSON")
	outPath := fs.String("out", "", "write the diff image to this PNG path")
	tolerance := fs.Int("tolerance", 0, "largest per-channel difference that counts as unchanged")
	tileSize := fs.Int("tile", 16, "region grid edge length in pixels")
	maxRegions := fs.Int("max-regions", 16, "maximum number of regions to list")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 2 {
		return errors.New("scene diff requires a reference PNG and a candidate PNG")
	}
	referencePath, candidatePath := fs.Arg(0), fs.Arg(1)
	reference, err := os.ReadFile(referencePath)
	if err != nil {
		return fmt.Errorf("read reference %s: %w", referencePath, err)
	}
	candidate, err := os.ReadFile(candidatePath)
	if err != nil {
		return fmt.Errorf("read candidate %s: %w", candidatePath, err)
	}
	result, err := imagediff.ComparePNG(reference, candidate, imagediff.Options{
		Tolerance: *tolerance, TileSize: *tileSize, MaxRegions: *maxRegions,
	})
	if err != nil {
		return err
	}
	if path := strings.TrimSpace(*outPath); path != "" {
		if err := writeDiffPNG(path, result); err != nil {
			return err
		}
	}
	if *jsonOut {
		encoded, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal scene diff report: %w", err)
		}
		if _, err := stdout.Write(append(encoded, '\n')); err != nil {
			return err
		}
	} else {
		fmt.Fprintf(stdout, "Scene3D frame diff: %s\n", result.Summary())
		fmt.Fprintf(stdout, "  reference %s %s\n", referencePath, result.ReferenceSHA256[:16])
		fmt.Fprintf(stdout, "  candidate %s %s\n", candidatePath, result.CandidateSHA256[:16])
		for index, region := range result.Regions {
			fmt.Fprintf(stdout, "  region %d: %s\n", index+1, region)
		}
		if path := strings.TrimSpace(*outPath); path != "" {
			fmt.Fprintf(stdout, "  diff image: %s\n", path)
		}
	}
	if !result.Identical {
		return errors.New("frames differ")
	}
	return nil
}

func printSceneCheck(w io.Writer, report sceneCheckReport) {
	status := "pass"
	if !report.Valid {
		status = "fail"
	}
	fmt.Fprintf(w, "Scene3D check: %s\n", status)
	fmt.Fprintf(w, "Scene: %s\n", report.Scene)

	fmt.Fprintf(w, "  Validation: %s, %d diagnostics\n", passFail(report.Validation.Valid), len(report.Validation.Diagnostics))
	for _, diag := range report.Validation.Diagnostics {
		fmt.Fprintf(w, "    %s %s", diag.Severity, diag.Code)
		if diag.Path != "" {
			fmt.Fprintf(w, " [%s]", diag.Path)
		}
		if diag.ID != "" {
			fmt.Fprintf(w, " id=%s", diag.ID)
		}
		fmt.Fprintf(w, ": %s\n", diag.Message)
	}

	fmt.Fprintf(w, "  Cost: %s GPU memory, %d estimated draw calls, %d estimated uploads\n",
		formatByteCount(report.Inspection.Memory.TotalGPUBytes),
		report.Inspection.Surface.EstimatedDrawCalls,
		report.Inspection.Surface.EstimatedUploadCount)
	if resolution := report.Inspection.AssetResolution; resolution != nil {
		if resolution.Checked {
			fmt.Fprintf(w, "  Assets: %d resolved, %d unresolved, %d remote (roots: %s)\n",
				resolution.Resolved, resolution.Unresolved, resolution.Remote, strings.Join(resolution.Roots, ", "))
		} else if resolution.Sources > 0 {
			fmt.Fprintf(w, "  Assets: %d sources, not checked (pass --assets to resolve them)\n", resolution.Sources)
		}
	}
	for _, budget := range report.Budgets {
		fmt.Fprintf(w, "  Budget %s: %s", budget.Category, budget.Status)
		if budget.Limit > 0 {
			fmt.Fprintf(w, " actual=%.0f limit=%.0f", budget.Actual, budget.Limit)
		}
		fmt.Fprintln(w)
	}

	for _, event := range report.Harness.Events {
		switch {
		case event.Frame != nil:
			frame := event.Frame
			fmt.Fprintf(w, "  Frame t=%.3f: %dx%d, coverage %.4f, %d unique colors, pixels %s\n",
				frame.Time, frame.Width, frame.Height, frame.Coverage, frame.UniqueColors, frame.PixelHash[:16])
			for _, diag := range frame.Diagnostics {
				fmt.Fprintf(w, "    %s %s", diag.Severity, diag.Code)
				if diag.Target != "" {
					fmt.Fprintf(w, " [%s]", diag.Target)
				}
				fmt.Fprintf(w, ": %s\n", diag.Message)
			}
		case event.Determinism != nil:
			d := event.Determinism
			fmt.Fprintf(w, "  Determinism: %d runs, %s\n", d.Runs, passFail(d.Identical))
			for _, divergence := range d.Divergences {
				fmt.Fprintf(w, "    run %d changed %d pixels, max delta %d\n",
					divergence.Run, divergence.ChangedPixels, divergence.MaxChannelDelta)
			}
		case event.Golden != nil:
			fmt.Fprintf(w, "  Golden %s: %s\n", event.Golden.Source, event.Golden.Diff.Summary())
			for index, region := range event.Golden.Diff.Regions {
				fmt.Fprintf(w, "    region %d: %s\n", index+1, region)
			}
		}
	}
	if report.Golden != nil && report.Golden.Written {
		fmt.Fprintf(w, "  Golden: wrote a new baseline at %s; this run proved nothing about change\n", report.Golden.Reference)
	}
	for name, path := range map[string]string{"render": report.Outputs.Render, "diff": report.Outputs.Diff} {
		if path != "" {
			fmt.Fprintf(w, "  Wrote %s: %s\n", name, path)
		}
	}
	fmt.Fprintf(w, "  Report hash: %s\n", report.Harness.ContentHash)
	for _, problem := range report.Problems {
		fmt.Fprintf(w, "  Problem: %s\n", problem)
	}
}

func passFail(ok bool) string {
	if ok {
		return "pass"
	}
	return "fail"
}

func containsString(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func writePreviewPNG(path string, result *preview.Result) error {
	file, err := createSceneOutputFile(path)
	if err != nil {
		return err
	}
	encodeErr := preview.WritePNG(file, result)
	closeErr := file.Close()
	if encodeErr != nil {
		return fmt.Errorf("encode scene preview: %w", encodeErr)
	}
	return closeErr
}

func writeDiffPNG(path string, result imagediff.Result) error {
	file, err := createSceneOutputFile(path)
	if err != nil {
		return err
	}
	encodeErr := png.Encode(file, result.Image)
	closeErr := file.Close()
	if encodeErr != nil {
		return fmt.Errorf("encode diff image: %w", encodeErr)
	}
	return closeErr
}

func createSceneOutputFile(path string) (*os.File, error) {
	if parent := filepath.Dir(path); parent != "." {
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return nil, fmt.Errorf("create directory for %s: %w", path, err)
		}
	}
	file, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("create %s: %w", path, err)
	}
	return file, nil
}

func sceneCheckUsage(w io.Writer) {
	fmt.Fprint(w, `gosx scene check - Run the whole browser-free authoring loop in one command

Usage:
  gosx scene check [--json] [--strict] [--assets root] [--budget file]
                   [--out render.png] [--width N] [--height N] [--time seconds] [--fast]
                   [--golden baseline.png] [--write-golden] [--diff-out diff.png]
                   [--tolerance N] [--repeat N]
                   [--background color] [--camera-x N --camera-y N --camera-z N --fov degrees]
                   <scene-file>

The command validates the document, costs it, resolves its assets against the
asset root, renders one frame on the CPU rasterizer, optionally repeats that
render to prove it reproduces, and compares the frame to a baseline image.

Every number in the report comes from a step that ran. A step that was skipped
is reported as skipped, never as a pass. A newly written baseline is reported as
written, because creating a baseline proves nothing about change.

`)
}

func sceneDiffUsage(w io.Writer) {
	fmt.Fprint(w, `gosx scene diff - Localize the difference between two rendered frames

Usage:
  gosx scene diff [--json] [--out diff.png] [--tolerance N] [--tile N] [--max-regions N]
                  <reference.png> <candidate.png>

The command reports how many pixels changed, the largest per-channel change,
and the pixel regions that contain the change. It exits non-zero when the two
frames differ. No browser and no pixel-diff service is involved.

`)
}
