package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"m31labs.dev/gosx/perf/ouroboros"
)

func cmdPerfOuroboros(args []string) {
	if len(args) > 0 && isHelpArg(args[0]) {
		perfOuroborosUsage(os.Stdout)
		return
	}
	fs := flag.NewFlagSet("perf ouroboros", flag.ExitOnError)
	root := fs.String("root", ".", "GoSX repository root")
	corpus := fs.String("corpus", "", "O0.2 fixture corpus JSON")
	inventory := fs.String("inventory", "", "O0.2 source inventory JSON to verify against current overlay")
	sourceIdentity := fs.String("source-identity", "", "O0.2 source identity handoff JSON to bind canonical runs")
	out := fs.String("out", "", "artifact root for raw samples and summaries")
	evidenceRoot := fs.String("evidence-root", "", "existing root for accepted O02-F evidence refs")
	pixelManifest := fs.String("pixel-manifest", "", "comma-separated O02-F pixel manifest refs under --evidence-root")
	baseURL := fs.String("base-url", "", "ready fixture server base URL")
	docsBaseURL := fs.String("docs-base-url", "", "ready docs server base URL for external R10")
	fixtureApp := fs.String("fixture-app", "", "fixture app directory for --serve")
	serve := fs.Bool("serve", false, "start the fixture app with go run .")
	port := fs.Int("port", 0, "fixture server port, 0 chooses a free port")
	samples := fs.String("samples", "smoke", "sample plan: smoke | baseline")
	routes := fs.String("routes", "", "comma-separated route IDs to run")
	headless := fs.Bool("headless", true, "run Chrome in headless mode")
	chromeWSURL := fs.String("chrome-ws-url", "", "Chrome DevTools WebSocket URL override; raw value is never written to artifacts")
	timeout := fs.Duration("timeout", 30*time.Second, "browser operation timeout")
	trace := fs.Bool("trace", true, "capture Chrome traces per sample")
	coverage := fs.Bool("coverage", true, "capture JS coverage per sample")
	heap := fs.Bool("heap-snapshots", false, "write heap snapshots per sample")
	viewport := fs.String("viewport", "1280x720", "viewport as WIDTHxHEIGHT")
	dpr := fs.Float64("dpr", 1, "device scale factor recorded for the run")
	envClass := fs.String("environment", "headless-logic", "environment class label")
	if err := fs.Parse(args); err != nil {
		fatal("%v", err)
	}
	if fs.NArg() != 0 {
		fatal("gosx perf ouroboros does not take positional arguments")
	}
	width, height, err := parseViewport(*viewport)
	if err != nil {
		fatal("gosx perf ouroboros: %v", err)
	}
	selected := splitCSV(*routes)
	result, err := ouroboros.RunBrowserBaseline(context.Background(), ouroboros.BrowserBaselineOptions{
		RepoRoot:           *root,
		CorpusPath:         *corpus,
		InventoryPath:      *inventory,
		SourceIdentityPath: *sourceIdentity,
		ArtifactRoot:       *out,
		EvidenceRoot:       *evidenceRoot,
		PixelManifest:      *pixelManifest,
		BaseURL:            *baseURL,
		DocsBaseURL:        *docsBaseURL,
		FixtureApp:         *fixtureApp,
		Serve:              *serve,
		Port:               *port,
		Samples:            *samples,
		Routes:             selected,
		Headless:           *headless,
		ChromeWebSocketURL: strings.TrimSpace(*chromeWSURL),
		Timeout:            *timeout,
		HeapSnapshots:      *heap,
		Trace:              *trace,
		Coverage:           *coverage,
		ViewportWidth:      width,
		ViewportHeight:     height,
		DPR:                *dpr,
		Environment:        *envClass,
	})
	if err != nil {
		fatal("gosx perf ouroboros: %v", err)
	}
	fmt.Printf("Ouroboros browser baseline artifacts\n")
	fmt.Printf("  manifest     %s\n", result.ManifestPath)
	fmt.Printf("  environment  %s\n", result.EnvironmentPath)
	fmt.Printf("  raw samples  %s\n", result.RawSamplesPath)
	fmt.Printf("  summary      %s\n", result.SummaryPath)
	fmt.Printf("  samples      %d (%d discarded)\n", result.SampleCount, result.DiscardedCount)
	if !result.Canonical {
		fmt.Printf("  canonical    false (reduced smoke cannot update O0.2)\n")
	}
}

func perfOuroborosUsage(w interface{ Write([]byte) (int, error) }) {
	fmt.Fprintf(w, `gosx perf ouroboros - Run the O0.2 browser baseline sampler

Usage:
  gosx perf ouroboros --serve --port 8080 --base-url http://127.0.0.1:8080 --samples baseline --out build/ouroboros/o0.2/current --inventory build/ouroboros/o0.2/source/source-inventory.json --source-identity build/ouroboros/o0.2/source-identity.json --evidence-root build/ouroboros/o0.2/evidence --pixel-manifest r08/webgpu/pixel-evidence.json,r08/webgl/pixel-evidence.json,r10/webgpu/pixel-evidence.json,r10/webgl/pixel-evidence.json --chrome-ws-url "$CHROME_WS_URL"
  gosx perf ouroboros --serve --routes R00,R01 --samples smoke --out build/ouroboros/o0.2/browser-smoke

Notes:
  smoke runs are reduced and can never update the canonical O0.2 baseline.
  baseline runs record canonical sample counts and must use a verified source overlay.
  --source-identity narrows acceptance; the run recomputes source identity from --inventory and --out.
  remote Chrome endpoints are recorded only as a connection class and SHA-256.

`)
}

func parseViewport(value string) (int, int, error) {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(value)), "x")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("viewport must be WIDTHxHEIGHT")
	}
	var width, height int
	if _, err := fmt.Sscanf(parts[0], "%d", &width); err != nil {
		return 0, 0, fmt.Errorf("invalid viewport width %q", parts[0])
	}
	if _, err := fmt.Sscanf(parts[1], "%d", &height); err != nil {
		return 0, 0, fmt.Errorf("invalid viewport height %q", parts[1])
	}
	if width <= 0 || height <= 0 {
		return 0, 0, fmt.Errorf("viewport dimensions must be positive")
	}
	return width, height, nil
}

func splitCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
