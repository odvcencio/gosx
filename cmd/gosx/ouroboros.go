package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"m31labs.dev/gosx/perf/ouroboros"
)

func cmdOuroboros() {
	if len(os.Args) < 3 || isHelpArg(os.Args[2]) {
		ouroborosUsage(os.Stdout)
		return
	}
	switch os.Args[2] {
	case "inventory":
		if len(os.Args) > 3 && isHelpArg(os.Args[3]) {
			ouroborosInventoryUsage(os.Stdout)
			return
		}
		cmdOuroborosInventory(os.Args[3:])
	case "compare":
		if len(os.Args) > 3 && isHelpArg(os.Args[3]) {
			ouroborosCompareUsage(os.Stdout)
			return
		}
		cmdOuroborosCompare(os.Args[3:])
	default:
		fatal("unknown ouroboros command: %s\nrun 'gosx ouroboros --help' for usage", os.Args[2])
	}
}

func cmdOuroborosInventory(args []string) {
	fs := flag.NewFlagSet("ouroboros inventory", flag.ExitOnError)
	root := fs.String("root", ".", "repository root to inventory")
	outPath := fs.String("out", "", "write source inventory JSON to this path")
	artifactRoot := fs.String("artifact-root", "", "artifact root recorded in the manifest")
	noCanopy := fs.Bool("no-canopy", false, "skip Canopy index evidence")
	if err := fs.Parse(args); err != nil {
		fatal("%v", err)
	}
	if fs.NArg() != 0 {
		fatal("gosx ouroboros inventory does not take positional arguments")
	}
	inv, err := ouroboros.Collect(context.Background(), ouroboros.CollectOptions{
		RepoRoot:     *root,
		ArtifactRoot: *artifactRoot,
		Canopy:       !*noCanopy,
		Git:          true,
	})
	if err != nil {
		fatal("ouroboros inventory: %v", err)
	}
	if *outPath == "" {
		if err := ouroboros.WriteJSON(os.Stdout, inv); err != nil {
			fatal("write inventory: %v", err)
		}
		return
	}
	if err := os.MkdirAll(filepath.Dir(*outPath), 0o755); err != nil {
		fatal("create inventory directory: %v", err)
	}
	artifactDir := filepath.Dir(*outPath)
	inv.Overlay.PatchPath = filepath.ToSlash(filepath.Join(artifactDir, "tracked-overlay.patch"))
	inv.Overlay.ArchivePath = filepath.ToSlash(filepath.Join(artifactDir, "untracked-sources"))
	if err := ouroboros.ValidateInventory(inv); err != nil {
		fatal("validate inventory: %v", err)
	}
	if err := ouroboros.WriteOverlayArtifacts(context.Background(), *root, artifactDir, inv.Overlay); err != nil {
		fatal("write overlay artifacts: %v", err)
	}
	f, err := os.Create(*outPath)
	if err != nil {
		fatal("create inventory: %v", err)
	}
	defer f.Close()
	if err := ouroboros.WriteJSON(f, inv); err != nil {
		fatal("write inventory: %v", err)
	}
}

func cmdOuroborosCompare(args []string) {
	fs := flag.NewFlagSet("ouroboros compare", flag.ExitOnError)
	baseline := fs.String("baseline", "", "baseline manifest.json or artifact root")
	candidate := fs.String("candidate", "", "candidate manifest.json or artifact root")
	budget := fs.String("budget", filepath.Join("perf", "ouroboros", "budgets.v1.json"), "O0.2 compare budget JSON")
	out := fs.String("out", "", "write compare report JSON to this path")
	mode := fs.String("mode", ouroboros.CompareModeCanonical, "compare mode: canonical or smoke")
	jsonOut := fs.Bool("json", false, "print compare report JSON to stdout")
	baselinePixelRoot := fs.String("baseline-pixel-root", "", "external baseline pixel artifact root")
	candidatePixelRoot := fs.String("candidate-pixel-root", "", "external candidate pixel artifact root")
	var candidatePixelManifests stringSlice
	fs.Var(&candidatePixelManifests, "candidate-pixel-manifest", "candidate pixel comparison manifest path; repeatable")
	if err := fs.Parse(args); err != nil {
		fatal("%v", err)
	}
	if fs.NArg() != 0 {
		fatal("gosx ouroboros compare does not take positional arguments")
	}
	report, err := ouroboros.CompareOuroborosArtifacts(ouroboros.CompareOptions{
		BaselineManifest:       *baseline,
		CandidateManifest:      *candidate,
		BudgetPath:             *budget,
		Mode:                   *mode,
		OutPath:                *out,
		BaselinePixelRoot:      *baselinePixelRoot,
		CandidatePixelRoot:     *candidatePixelRoot,
		CandidatePixelManifest: append([]string{}, candidatePixelManifests...),
	})
	if err != nil {
		fatal("gosx ouroboros compare: %v", err)
	}
	if *jsonOut {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			fatal("gosx ouroboros compare: json: %v", err)
		}
		fmt.Println(string(data))
	}
	if report.ExitCode != 0 {
		os.Exit(report.ExitCode)
	}
}

func ouroborosUsage(w interface{ Write([]byte) (int, error) }) {
	fmt.Fprintf(w, `gosx ouroboros - Ouroboros runtime baseline tools

Usage:
  gosx ouroboros inventory [--root <repo>] [--out <file>] [--artifact-root <dir>] [--no-canopy]
  gosx ouroboros compare --baseline <manifest|root> --candidate <manifest|root> --budget <file> [--out <file>]

Commands:
  inventory   Collect O0.2 source inventory, compatibility surface, drift, and overlay evidence
  compare     Compare O0.2 browser artifact roots and exit on regressions

`)
}

func ouroborosInventoryUsage(w interface{ Write([]byte) (int, error) }) {
	fmt.Fprintf(w, `gosx ouroboros inventory - Collect O0.2 source inventory

Usage:
  gosx ouroboros inventory [--root <repo>] [--out <file>] [--artifact-root <dir>] [--no-canopy]

`)
}

func ouroborosCompareUsage(w interface{ Write([]byte) (int, error) }) {
	fmt.Fprintf(w, `gosx ouroboros compare - Compare O0.2 browser baseline artifacts

Usage:
  gosx ouroboros compare --baseline <manifest|root> --candidate <manifest|root> --budget <file> [--out <file>] [--mode canonical|smoke] [--json]

Flags:
  --baseline <manifest|root>                  baseline artifact manifest or root
  --candidate <manifest|root>                 candidate artifact manifest or root
  --budget <file>                             compare budget JSON; defaults to perf/ouroboros/budgets.v1.json
  --out <file>                                write gosx.ouroboros.compare.v1 report
  --mode canonical|smoke                      canonical requires canonical baseline; smoke rejects canonical manifests
  --baseline-pixel-root <dir>                 external baseline pixel artifact root
  --candidate-pixel-root <dir>                external candidate pixel artifact root
  --candidate-pixel-manifest <file>           candidate pixel comparison manifest; repeatable
  --json                                      print report JSON to stdout

Exit codes:
  0 pass
  1 regression or malformed evidence
  2 inconclusive unstable required metric

`)
}
