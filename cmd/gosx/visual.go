package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"m31labs.dev/gosx/visual"
)

func cmdVisual() {
	fs := flag.NewFlagSet("visual", flag.ExitOnError)
	update := fs.Bool("update", false, "write captured screenshot to baseline (instead of comparing)")
	baseline := fs.String("baseline", "", "explicit baseline PNG path (default: testdata/visual/<urlhash>.png)")
	threshold := fs.Float64("threshold", 0.0, "max allowed pixel diff percentage (0-100)")
	viewportW := fs.Int("w", 1440, "viewport width in pixels")
	viewportH := fs.Int("h", 900, "viewport height in pixels")
	scale := fs.Float64("scale", 1.0, "device pixel ratio")
	waitDur := fs.Duration("wait", 2*time.Second, "settle time after navigation before capture")
	waitSel := fs.String("wait-selector", "body", "CSS selector to wait for before capture")
	selector := fs.String("selector", "", "CSS selector to capture (default: full viewport)")
	evalJS := fs.String("eval", "", "JavaScript to run after the page is ready (e.g. to hide UI chrome)")
	timeout := fs.Duration("timeout", 60*time.Second, "overall capture timeout")
	diffOut := fs.String("diff", "", "where to write the diff image on failure")
	jsonOut := fs.Bool("json", false, "emit result as JSON")
	requireBackend := fs.String("require-backend", "", "hard-fail unless the mounted Scene3D backend is webgpu, webgl, or any-gpu (default: no check)")
	pixelEvidenceOut := fs.String("ouroboros-pixels-out", "", "write O0.2 Scene3D pixel evidence to this artifact directory")
	pixelMode := fs.String("ouroboros-mode", string(visual.PixelModeRecordBaseline), "O0.2 pixel mode: record-baseline or candidate")
	pixelBaselineRoot := fs.String("ouroboros-baseline-root", "", "existing O0.2 pixel baseline root for candidate comparisons")
	pixelRouteID := fs.String("ouroboros-route-id", "", "O0.2 route ID for pixel evidence, such as R08 or R10")
	pixelSamples := fs.Int("ouroboros-pixel-samples", 3, "O0.2 pixel captures per state")
	pixelInitialWait := fs.Duration("ouroboros-initial-wait", 0, "O0.2 wait after first rendered frame before initial capture")
	pixelSettledWait := fs.Duration("ouroboros-settled-wait", 3*time.Second, "O0.2 settled-state wait before capture")
	pixelWarmupFrames := fs.Int("ouroboros-warmup-frames", 30, "O0.2 settled-state minimum frame advance after initial readiness")
	pixelCanvasSelector := fs.String("ouroboros-canvas-selector", "canvas", "canvas selector for O0.2 pixel evidence")
	pixelAllowOverwrite := fs.Bool("ouroboros-allow-overwrite", false, "allow O0.2 record-baseline to write into a non-empty artifact directory")
	pixelForceWebGL := fs.Bool("ouroboros-force-webgl", false, "set the O0.2 probe-only Scene3D WebGL flag before navigation")
	pixelBaseRevision := fs.String("ouroboros-base-revision", "", "O0.2 source base revision for pixel evidence")
	pixelOverlayHash := fs.String("ouroboros-overlay-hash", "", "O0.2 source overlay hash for pixel evidence")
	pixelInventorySHA256 := fs.String("ouroboros-inventory-sha256", "", "O0.2 source inventory SHA-256 for pixel evidence")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `gosx visual - pixel-level visual regression testing

Usage:
  gosx visual [flags] <url>

Flags:
`)
		fs.PrintDefaults()
		fmt.Fprintf(os.Stderr, `
Examples:
  # Create a baseline for the first time:
  gosx visual --update http://localhost:8080/

  # Re-check against the baseline:
  gosx visual http://localhost:8080/

  # Allow up to 0.05%% pixel drift (good for animated content):
  gosx visual --threshold 0.05 http://localhost:8080/

  # Mobile viewport:
  gosx visual -w 375 -h 812 http://localhost:8080/

  # Hide dynamic chrome before capture:
  gosx visual --eval "document.querySelector('.clock').remove()" http://localhost:8080/

  # Refuse a Scene3D capture that silently fell back to the 2D canvas
  # renderer instead of WebGPU (see /docs/debugging-scene3d on gosx-docs):
  gosx visual --require-backend webgpu http://localhost:8080/scene

  # Record O0.2 canvas-pixel evidence into a caller-selected new artifact root:
  gosx visual --ouroboros-pixels-out build/ouroboros/o0.2/pixels/R08 \
    --ouroboros-route-id R08 --require-backend webgl --ouroboros-force-webgl \
    --ouroboros-base-revision abc1234 --ouroboros-overlay-hash sha256:clean \
    --ouroboros-inventory-sha256 sha256:0000000000000000000000000000000000000000000000000000000000000000 \
    http://localhost:8080/scene/basic

Environment:
  CHROME_WS_URL  If set, connects to a remote headless-shell service
                 (e.g. ws://chrome-headless:9222) instead of launching a
                 local Chrome binary. Required for CI / in-cluster usage.
  CHROME_PATH    Path to a local Chrome binary (dev mode only).
`)
	}

	if err := fs.Parse(os.Args[2:]); err != nil {
		os.Exit(2)
	}

	if !visual.ValidRequireBackend(*requireBackend) {
		fatal("visual: --require-backend must be one of webgpu, webgl, any-gpu (got %q)", *requireBackend)
	}

	if fs.NArg() < 1 {
		fs.Usage()
		os.Exit(2)
	}
	url := fs.Arg(0)

	opts := visual.AssertOptions{
		CaptureOptions: visual.CaptureOptions{
			Viewport:          visual.Viewport{Width: *viewportW, Height: *viewportH, Scale: *scale},
			Wait:              *waitDur,
			WaitSelector:      *waitSel,
			Selector:          *selector,
			EvalBeforeCapture: *evalJS,
			Timeout:           *timeout,
			RequireBackend:    visual.RequireBackend(*requireBackend),
		},
		BaselinePath: *baseline,
		Threshold:    *threshold,
		Update:       *update,
		DiffOutPath:  *diffOut,
	}

	ctx := context.Background()
	if strings.TrimSpace(*pixelEvidenceOut) != "" {
		if !visual.ValidPixelEvidenceMode(*pixelMode) {
			fatal("visual: --ouroboros-mode must be record-baseline or candidate (got %q)", *pixelMode)
		}
		manifest, err := visual.CapturePixelEvidence(ctx, url, visual.PixelEvidenceOptions{
			Mode:         visual.PixelEvidenceMode(*pixelMode),
			RouteID:      *pixelRouteID,
			ArtifactRoot: *pixelEvidenceOut,
			BaselineRoot: *pixelBaselineRoot,
			Source: visual.PixelSourceIdentity{
				BaseRevision:    *pixelBaseRevision,
				OverlayHash:     *pixelOverlayHash,
				InventorySHA256: *pixelInventorySHA256,
			},
			Backend:        visual.RequireBackend(*requireBackend),
			Samples:        *pixelSamples,
			Viewport:       visual.Viewport{Width: *viewportW, Height: *viewportH, Scale: *scale},
			InitialWait:    *pixelInitialWait,
			SettledWait:    *pixelSettledWait,
			WarmupFrames:   *pixelWarmupFrames,
			WaitSelector:   *waitSel,
			CanvasSelector: *pixelCanvasSelector,
			Timeout:        *timeout,
			ThresholdPct:   *threshold,
			AllowOverwrite: *pixelAllowOverwrite,
			ForceWebGL:     *pixelForceWebGL,
		})
		if err != nil {
			fatal("visual: O0.2 pixel evidence failed: %v", err)
		}
		out, _ := json.MarshalIndent(manifest, "", "  ")
		fmt.Println(string(out))
		return
	}
	err := visual.Assert(ctx, url, opts)

	if *update {
		if err != nil {
			fatal("visual: update failed: %v", err)
		}
		if *jsonOut {
			out, _ := json.Marshal(map[string]any{
				"ok":       true,
				"url":      url,
				"baseline": effectiveBaselinePath(url, *baseline),
				"action":   "updated",
			})
			fmt.Println(string(out))
		} else {
			fmt.Printf("updated baseline: %s\n", effectiveBaselinePath(url, *baseline))
		}
		return
	}

	var mismatch *visual.AssertMismatch
	if errors.As(err, &mismatch) {
		if *jsonOut {
			out, _ := json.Marshal(map[string]any{
				"ok":          false,
				"url":         url,
				"baseline":    mismatch.BaselinePath,
				"diff":        mismatch.DiffPath,
				"diff_pct":    mismatch.Result.DiffPct,
				"mismatched":  mismatch.Result.Mismatched,
				"total":       mismatch.Result.Total,
				"dim_matches": mismatch.Result.DimensionsMatch,
				"threshold":   *threshold,
			})
			fmt.Println(string(out))
		} else {
			fmt.Fprintln(os.Stderr, mismatch.Error())
		}
		os.Exit(1)
	}
	if err != nil {
		fatal("visual: %v", err)
	}

	if *jsonOut {
		out, _ := json.Marshal(map[string]any{
			"ok":       true,
			"url":      url,
			"baseline": effectiveBaselinePath(url, *baseline),
			"action":   "match",
		})
		fmt.Println(string(out))
	} else {
		fmt.Printf("match: %s\n", url)
	}
}

func effectiveBaselinePath(url, explicit string) string {
	if explicit != "" {
		return explicit
	}
	return visual.DefaultBaselinePath(url)
}

func visualUsage(w io.Writer) {
	fmt.Fprintf(w, `gosx visual - Pixel-level visual regression testing

Usage:
  gosx visual [flags] <url>

Common flags:
  --update                  write captured screenshot to baseline
  --baseline <path>         explicit baseline PNG path
  --threshold <pct>         maximum allowed pixel diff percentage
  -w <px> -h <px>           viewport width and height
  --selector <css>          capture one element instead of the full viewport
  --eval <javascript>       run JavaScript before capture
  --require-backend <name>  hard-fail unless Scene3D mounted webgpu, webgl, or any-gpu
  --ouroboros-pixels-out    write O0.2 Scene3D pixel evidence to an artifact directory
                            requires explicit --require-backend webgpu or webgl
  --ouroboros-base-revision source base revision for O0.2 pixel evidence
  --ouroboros-overlay-hash  source overlay hash for O0.2 pixel evidence
  --ouroboros-inventory-sha256
                            source inventory SHA-256 for O0.2 pixel evidence
  --json                    emit JSON

`)
}
