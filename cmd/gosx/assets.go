package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/odvcencio/gosx/assetpipe"
)

func cmdAssets() {
	if err := runAssetsCommand(os.Args[2:], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "assets error: %v\n", err)
		os.Exit(1)
	}
}

func runAssetsCommand(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return runAssetPlanCommand(nil, stdout)
	}
	switch args[0] {
	case "plan":
		return runAssetPlanCommand(args[1:], stdout)
	case "help", "-h", "--help":
		assetsUsage(stdout)
		return nil
	default:
		return fmt.Errorf("unknown assets subcommand %q\nrun 'gosx assets help' for usage", args[0])
	}
}

func runAssetPlanCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("gosx assets plan", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	jsonOut := fs.Bool("json", false, "emit JSON")
	writePath := fs.String("write", "", "write JSON report to a file")
	maxProbeMB := fs.Int64("max-probe-mb", assetpipe.DefaultMaxProbeBytes>>20, "maximum bytes to read per probed asset, in MiB")
	turboBits := fs.Int("turboquant-bits", 12, "TurboQuant bit width to plan for vertex/transform streams")
	previewBits := fs.Int("preview-bits", 0, "optional preview TurboQuant bit width")
	if err := fs.Parse(args); err != nil {
		return err
	}
	roots := fs.Args()
	if len(roots) == 0 {
		roots = []string{"."}
	}
	report, err := assetpipe.Plan(roots, assetpipe.Options{
		MaxProbeBytes:         *maxProbeMB << 20,
		TurboQuantBitWidth:    *turboBits,
		TurboQuantPreviewBits: *previewBits,
	})
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal asset plan: %w", err)
	}
	if *writePath != "" {
		if dir := filepath.Dir(*writePath); dir != "." {
			if err := os.MkdirAll(dir, 0755); err != nil {
				return fmt.Errorf("create report directory: %w", err)
			}
		}
		if err := os.WriteFile(*writePath, data, 0644); err != nil {
			return fmt.Errorf("write %s: %w", *writePath, err)
		}
	}
	if *jsonOut {
		_, err = stdout.Write(append(data, '\n'))
		return err
	}
	printAssetPlan(stdout, report)
	return nil
}

func printAssetPlan(w io.Writer, report assetpipe.Report) {
	fmt.Fprintf(w, "Scene asset plan: %d assets, %d bytes, %d variants, %d optimization actions\n",
		report.Totals.Assets, report.Totals.Bytes, report.Totals.Variants, report.Totals.OptimizationActions)
	if report.Totals.Assets == 0 {
		return
	}
	fmt.Fprintf(w, "  glb=%d gltf=%d ktx2=%d textures=%d environments=%d audio=%d usdz=%d shaders=%d\n",
		report.Totals.GLB, report.Totals.GLTF, report.Totals.KTX2, report.Totals.Texture,
		report.Totals.Environment, report.Totals.Audio, report.Totals.USDZ, report.Totals.Shader)
	for _, asset := range report.Assets {
		fmt.Fprintf(w, "\n%s [%s, %d bytes]\n", asset.Path, asset.Kind, asset.Bytes)
		for _, warning := range asset.Warnings {
			fmt.Fprintf(w, "  warning: %s\n", warning)
		}
		for _, action := range asset.Actions {
			status := action.Status
			if status == "" {
				status = "candidate"
			}
			reason := strings.TrimSpace(action.Reason)
			if reason == "" {
				fmt.Fprintf(w, "  - %s (%s)\n", action.Name, status)
			} else {
				fmt.Fprintf(w, "  - %s (%s): %s\n", action.Name, status, reason)
			}
		}
		for _, variant := range asset.Variants {
			label := strings.TrimSpace(variant.Compression)
			if label == "" {
				label = strings.TrimSpace(variant.Kind)
			}
			if label == "" {
				label = "variant"
			}
			fmt.Fprintf(w, "    -> %s [%s]\n", variant.URI, label)
		}
	}
}

func assetsUsage(w io.Writer) {
	fmt.Fprintf(w, `gosx assets - Plan build-time optimization for Scene3D assets

Usage:
  gosx assets plan [--json] [--write report.json] [path...]

Examples:
  gosx assets plan public
  gosx assets plan --json --turboquant-bits 10 public/assets

`)
}

func writeBuildSceneAssetPlan(projectDir, distDir string) (*assetpipe.Report, error) {
	publicDir := filepath.Join(projectDir, "public")
	if !isDir(publicDir) {
		return nil, nil
	}
	report, err := assetpipe.Plan([]string{publicDir}, assetpipe.Options{})
	if err != nil {
		return nil, err
	}
	if report.Totals.Assets == 0 {
		return nil, nil
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal scene asset plan: %w", err)
	}
	path := filepath.Join(distDir, "scene-assets.json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		return nil, fmt.Errorf("write scene asset plan: %w", err)
	}
	return &report, nil
}
