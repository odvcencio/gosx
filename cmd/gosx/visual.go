package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/odvcencio/gosx/visual"
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
		},
		BaselinePath: *baseline,
		Threshold:    *threshold,
		Update:       *update,
		DiffOutPath:  *diffOut,
	}

	ctx := context.Background()
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
