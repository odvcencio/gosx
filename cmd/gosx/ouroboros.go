package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"m31labs.dev/gosx/perf/ouroboros"
)

func cmdOuroboros() {
	if len(os.Args) < 3 || isHelpArg(os.Args[2]) {
		ouroborosUsage(os.Stdout)
		return
	}
	switch os.Args[2] {
	case "export-corpus":
		if len(os.Args) > 3 && isHelpArg(os.Args[3]) {
			ouroborosExportCorpusUsage(os.Stdout)
			return
		}
		cmdOuroborosExportCorpus(os.Args[3:])
	case "inventory":
		if len(os.Args) > 3 && isHelpArg(os.Args[3]) {
			ouroborosInventoryUsage(os.Stdout)
			return
		}
		cmdOuroborosInventory(os.Args[3:])
	case "source-identity":
		if len(os.Args) > 3 && isHelpArg(os.Args[3]) {
			ouroborosSourceIdentityUsage(os.Stdout)
			return
		}
		cmdOuroborosSourceIdentity(os.Args[3:])
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

func cmdOuroborosSourceIdentity(args []string) {
	fs := flag.NewFlagSet("ouroboros source-identity", flag.ExitOnError)
	root := fs.String("root", ".", "repository root")
	inventory := fs.String("inventory", "", "strict O0.2 source inventory JSON")
	artifactRoot := fs.String("artifact-root", "", "future browser artifact root")
	outPath := fs.String("out", "", "write source identity handoff JSON to this path")
	if err := fs.Parse(args); err != nil {
		fatal("%v", err)
	}
	if fs.NArg() != 0 {
		fatal("gosx ouroboros source-identity does not take positional arguments")
	}
	if strings.TrimSpace(*inventory) == "" {
		fatal("gosx ouroboros source-identity requires --inventory")
	}
	if strings.TrimSpace(*artifactRoot) == "" {
		fatal("gosx ouroboros source-identity requires --artifact-root")
	}
	if strings.TrimSpace(*outPath) == "" {
		fatal("gosx ouroboros source-identity requires --out")
	}
	if err := rejectSourceIdentityOutUnderArtifactRoot(*outPath, *artifactRoot); err != nil {
		fatal("gosx ouroboros source-identity: %v", err)
	}
	if _, err := os.Lstat(*artifactRoot); err == nil {
		fatal("gosx ouroboros source-identity: future artifact root already exists: %s", *artifactRoot)
	} else if !os.IsNotExist(err) {
		fatal("gosx ouroboros source-identity: inspect future artifact root: %v", err)
	}
	if err := ouroboros.EnsureNewJSONFilePath(*outPath); err != nil {
		fatal("gosx ouroboros source-identity: %v", err)
	}
	handoff, err := ouroboros.BuildSourceIdentityHandoff(context.Background(), *root, *inventory, *artifactRoot)
	if err != nil {
		fatal("gosx ouroboros source-identity: %v", err)
	}
	if err := ouroboros.ValidateSourceIdentityHandoff(handoff); err != nil {
		fatal("gosx ouroboros source-identity: built invalid handoff: %v", err)
	}
	if err := ouroboros.WriteNewJSONFile(*outPath, handoff); err != nil {
		fatal("gosx ouroboros source-identity: %v", err)
	}
}

func rejectSourceIdentityOutUnderArtifactRoot(outPath, artifactRoot string) error {
	outAbs, err := filepath.Abs(outPath)
	if err != nil {
		return err
	}
	rootAbs, err := filepath.Abs(artifactRoot)
	if err != nil {
		return err
	}
	outAbs = filepath.Clean(outAbs)
	rootAbs = filepath.Clean(rootAbs)
	if sameOrContainedPath(outAbs, rootAbs) {
		return fmt.Errorf("--out must not be equal to or inside --artifact-root")
	}
	outReal, err := resolvePathWithExistingSymlinks(outAbs)
	if err != nil {
		return err
	}
	rootReal, err := resolvePathWithExistingSymlinks(rootAbs)
	if err != nil {
		return err
	}
	if sameOrContainedPath(outReal, rootReal) {
		return fmt.Errorf("--out must not resolve inside --artifact-root")
	}
	return nil
}

func resolvePathWithExistingSymlinks(path string) (string, error) {
	path = filepath.Clean(path)
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(resolved), nil
	}
	remaining := []string{}
	current := path
	for {
		if current == "." || current == string(filepath.Separator) || current == filepath.VolumeName(current)+string(filepath.Separator) {
			break
		}
		if _, err := os.Lstat(current); err == nil {
			resolved, err := filepath.EvalSymlinks(current)
			if err != nil {
				return "", err
			}
			parts := append([]string{resolved}, remaining...)
			return filepath.Clean(filepath.Join(parts...)), nil
		}
		remaining = append([]string{filepath.Base(current)}, remaining...)
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return path, nil
}

func sameOrContainedPath(child, root string) bool {
	rel, err := filepath.Rel(root, child)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel))
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
  gosx ouroboros export-corpus --root <repo> --out <dir> --corpus <fixtures.v1.json> --fixture-app <dir> --docs-app <dir>
  gosx ouroboros inventory [--root <repo>] [--out <file>] [--artifact-root <dir>] [--no-canopy]
  gosx ouroboros source-identity --root <repo> --inventory <file> --artifact-root <future-browser-out> --out <file>
  gosx ouroboros compare --baseline <manifest|root> --candidate <manifest|root> --budget <file> [--out <file>]

Commands:
  export-corpus
              Build and publish the strict O0.2 canonical size corpus dist
  inventory   Collect O0.2 source inventory, compatibility surface, drift, and overlay evidence
  source-identity
              Write deterministic canonical source identity handoff for a future browser run
  compare     Compare O0.2 browser artifact roots and exit on regressions

`)
}

func ouroborosInventoryUsage(w interface{ Write([]byte) (int, error) }) {
	fmt.Fprintf(w, `gosx ouroboros inventory - Collect O0.2 source inventory

Usage:
  gosx ouroboros inventory [--root <repo>] [--out <file>] [--artifact-root <dir>] [--no-canopy]

`)
}

func ouroborosSourceIdentityUsage(w interface{ Write([]byte) (int, error) }) {
	fmt.Fprintf(w, `gosx ouroboros source-identity - Predict canonical O0.2 source identity

Usage:
  gosx ouroboros source-identity --root <repo> --inventory <file> --artifact-root <future-browser-out> --out <file>

Notes:
  The command validates a strict fresh inventory and replay reconstruction.
  It predicts source/source-inventory.json for the future artifact root.
  It does not create or mutate the future artifact root, and refuses to overwrite --out.

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
