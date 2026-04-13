// Package visual provides pixel-level visual regression testing for GoSX
// pages and arbitrary URLs via headless Chrome.
//
// The typical workflow is:
//
//  1. Call Assert() with a URL and a baseline path. On first run (with
//     Update=true) it captures the page and writes the baseline.
//  2. Subsequent runs re-capture, compare against the baseline with
//     pixelmatch, and fail the test if the diff exceeds Threshold.
//  3. On failure, the diff image (mismatched pixels highlighted red) is
//     written next to the baseline so a human can review.
//
// The CDP allocator is picked based on CHROME_WS_URL — if set, connects
// to a remote headless-shell service (cluster-friendly); otherwise launches
// a local Chrome binary via perf.FindChrome. This lets the same package
// power developer-machine runs, CI jobs, and in-cluster scheduled audits.
//
// For Scene3D pages, a visual assert needs a deterministic render seed
// because particle positions are derived from wall-clock time. Pages can
// honor a `?__gosx_visual_seed=HEX` query param and freeze their RNG when
// it's present — see app/galaxy.go in the m31labs.dev repo for a working
// example.
package visual

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/odvcencio/gosx/perf"
	"github.com/orisano/pixelmatch"
)

// Viewport is the browser viewport (device-independent pixels).
type Viewport struct {
	Width  int
	Height int
	Scale  float64 // device pixel ratio; 0 = default 1.0
}

// Common viewports for convenience.
var (
	ViewportDesktop = Viewport{Width: 1440, Height: 900}
	ViewportTablet  = Viewport{Width: 1024, Height: 768}
	ViewportMobile  = Viewport{Width: 375, Height: 812}
	ViewportOG      = Viewport{Width: 1200, Height: 630}
)

// CaptureOptions tunes a single screenshot.
type CaptureOptions struct {
	// Viewport to emulate. Defaults to ViewportDesktop when zero.
	Viewport Viewport

	// Wait is the settle time after navigation and after the element is
	// visible, before the screenshot is taken. Gives animations, fonts,
	// and hydration a chance to stabilize. Defaults to 2s.
	Wait time.Duration

	// WaitSelector, if set, overrides the default `body` wait-visible.
	// Useful for pages that finish rendering after a specific element
	// appears (e.g. `canvas` for Scene3D).
	WaitSelector string

	// Selector, if set, captures only the bounding box of that element
	// instead of the full viewport.
	Selector string

	// EvalBeforeCapture is JS to run after the scene is ready but before
	// the screenshot. Useful for hiding dynamic UI chrome.
	EvalBeforeCapture string

	// Timeout bounds the entire capture. Defaults to 60s.
	Timeout time.Duration
}

// applyDefaults fills in zero-value fields.
func (o *CaptureOptions) applyDefaults() {
	if o.Viewport.Width == 0 || o.Viewport.Height == 0 {
		o.Viewport = ViewportDesktop
	}
	if o.Viewport.Scale == 0 {
		o.Viewport.Scale = 1
	}
	if o.Wait == 0 {
		o.Wait = 2 * time.Second
	}
	if o.WaitSelector == "" {
		o.WaitSelector = "body"
	}
	if o.Timeout == 0 {
		o.Timeout = 60 * time.Second
	}
}

// Capture navigates to url and returns a PNG screenshot.
//
// The allocator is selected by newAllocator: CHROME_WS_URL (remote) if set,
// otherwise a locally-launched Chrome via perf.FindChrome.
func Capture(ctx context.Context, url string, opts CaptureOptions) ([]byte, error) {
	opts.applyDefaults()

	allocCtx, allocCancel, err := newAllocator(ctx)
	if err != nil {
		return nil, fmt.Errorf("visual: allocator: %w", err)
	}
	defer allocCancel()

	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	defer browserCancel()

	runCtx, runCancel := context.WithTimeout(browserCtx, opts.Timeout)
	defer runCancel()

	actions := []chromedp.Action{
		chromedp.EmulateViewport(
			int64(opts.Viewport.Width),
			int64(opts.Viewport.Height),
			chromedp.EmulateScale(opts.Viewport.Scale),
		),
		chromedp.Navigate(url),
		chromedp.WaitVisible(opts.WaitSelector, chromedp.ByQuery),
	}

	if opts.EvalBeforeCapture != "" {
		actions = append(actions, chromedp.Evaluate(opts.EvalBeforeCapture, nil))
	}

	actions = append(actions, chromedp.Sleep(opts.Wait))

	var buf []byte
	if opts.Selector != "" {
		actions = append(actions, chromedp.Screenshot(opts.Selector, &buf, chromedp.NodeVisible, chromedp.ByQuery))
	} else {
		actions = append(actions, chromedp.CaptureScreenshot(&buf))
	}

	if err := chromedp.Run(runCtx, actions...); err != nil {
		return nil, fmt.Errorf("visual: capture %s: %w", url, err)
	}
	if len(buf) == 0 {
		return nil, fmt.Errorf("visual: capture %s: empty screenshot", url)
	}
	return buf, nil
}

// newAllocator picks between a remote CHROME_WS_URL and a local Chrome
// launch. Remote is preferred because it's the single-source-of-truth for
// browser environment in CI and k8s clusters.
func newAllocator(ctx context.Context) (context.Context, context.CancelFunc, error) {
	if wsURL := strings.TrimSpace(os.Getenv("CHROME_WS_URL")); wsURL != "" {
		allocCtx, cancel := chromedp.NewRemoteAllocator(ctx, wsURL)
		return allocCtx, cancel, nil
	}

	chromePath, err := perf.FindChrome()
	if err != nil {
		return nil, nil, err
	}

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(chromePath),
		chromedp.Flag("headless", "new"),
		chromedp.Flag("hide-scrollbars", true),
		chromedp.Flag("mute-audio", true),
		chromedp.Flag("use-gl", "angle"),
		chromedp.Flag("use-angle", "swiftshader"),
		chromedp.Flag("enable-unsafe-swiftshader", true),
		chromedp.Flag("enable-webgl", true),
		chromedp.Flag("ignore-gpu-blocklist", true),
		chromedp.Flag("no-sandbox", true),
	)
	allocCtx, cancel := chromedp.NewExecAllocator(ctx, opts...)
	return allocCtx, cancel, nil
}

// DiffResult summarizes a pixelmatch comparison.
type DiffResult struct {
	// Diff is an image with mismatched pixels highlighted (red/yellow
	// overlay on a greyed background).
	Diff image.Image

	// Mismatched is the number of pixels that differ beyond the
	// pixelmatch AA threshold.
	Mismatched int

	// Total is the total pixel count (width * height).
	Total int

	// DiffPct is Mismatched / Total as a percentage (0-100).
	DiffPct float64

	// DimensionsMatch is false when baseline and current have different
	// widths or heights. When false, Diff is nil and DiffPct is 100.
	DimensionsMatch bool
}

// Diff compares two PNG byte buffers and returns a DiffResult.
func Diff(baseline, current []byte) (DiffResult, error) {
	baseImg, err := png.Decode(bytes.NewReader(baseline))
	if err != nil {
		return DiffResult{}, fmt.Errorf("visual: decode baseline: %w", err)
	}
	currImg, err := png.Decode(bytes.NewReader(current))
	if err != nil {
		return DiffResult{}, fmt.Errorf("visual: decode current: %w", err)
	}

	baseRect := baseImg.Bounds()
	currRect := currImg.Bounds()

	if baseRect != currRect {
		// Build a side-by-side diagnostic image so reviewers can see both
		// canvases at once. Baseline on the left, current on the right.
		maxH := baseRect.Dy()
		if currRect.Dy() > maxH {
			maxH = currRect.Dy()
		}
		totalW := baseRect.Dx() + currRect.Dx() + 4
		side := image.NewRGBA(image.Rect(0, 0, totalW, maxH))
		// Fill magenta to make dimension-mismatches obvious.
		for y := 0; y < maxH; y++ {
			for x := 0; x < totalW; x++ {
				side.SetRGBA(x, y, color.RGBA{R: 128, G: 0, B: 128, A: 255})
			}
		}
		for y := 0; y < baseRect.Dy(); y++ {
			for x := 0; x < baseRect.Dx(); x++ {
				side.Set(x, y, baseImg.At(x+baseRect.Min.X, y+baseRect.Min.Y))
			}
		}
		offX := baseRect.Dx() + 4
		for y := 0; y < currRect.Dy(); y++ {
			for x := 0; x < currRect.Dx(); x++ {
				side.Set(offX+x, y, currImg.At(x+currRect.Min.X, y+currRect.Min.Y))
			}
		}
		return DiffResult{
			Diff:            side,
			Mismatched:      baseRect.Dx() * baseRect.Dy(),
			Total:           baseRect.Dx() * baseRect.Dy(),
			DiffPct:         100,
			DimensionsMatch: false,
		}, nil
	}

	diffImg := image.NewRGBA(baseRect)
	// Start with a grey wash so unchanged pixels are visually muted.
	for y := baseRect.Min.Y; y < baseRect.Max.Y; y++ {
		for x := baseRect.Min.X; x < baseRect.Max.X; x++ {
			base := color.RGBAModel.Convert(baseImg.At(x, y)).(color.RGBA)
			g := uint8((int(base.R) + int(base.G) + int(base.B)) / 3 / 2)
			diffImg.SetRGBA(x, y, color.RGBA{R: g, G: g, B: g, A: 255})
		}
	}

	var diffOut image.Image = diffImg
	mismatched, err := pixelmatch.MatchPixel(
		baseImg, currImg,
		pixelmatch.Threshold(0.1),
		pixelmatch.IncludeAntiAlias,
		pixelmatch.WriteTo(&diffOut),
	)
	if err != nil {
		return DiffResult{}, fmt.Errorf("visual: pixelmatch: %w", err)
	}

	total := baseRect.Dx() * baseRect.Dy()
	pct := float64(mismatched) / float64(total) * 100

	return DiffResult{
		Diff:            diffOut,
		Mismatched:      mismatched,
		Total:           total,
		DiffPct:         pct,
		DimensionsMatch: true,
	}, nil
}

// AssertOptions configures a baseline-backed assert.
type AssertOptions struct {
	CaptureOptions

	// BaselinePath is the PNG file to compare against. If empty, it is
	// derived from url as testdata/visual/<sha>.png.
	BaselinePath string

	// Threshold is the maximum DiffPct (0-100) that still passes. A
	// value of 0.05 means 0.05% of pixels can differ.
	Threshold float64

	// Update writes the captured screenshot to BaselinePath and returns
	// nil (no comparison). Use this to seed or refresh baselines.
	Update bool

	// DiffOutPath is where the diff image is written on failure. When
	// empty it defaults to BaselinePath with .diff.png suffix.
	DiffOutPath string
}

// AssertMismatch is returned when a capture differs from its baseline
// beyond the configured threshold. The diff image has been written to
// disk at DiffPath.
type AssertMismatch struct {
	URL          string
	BaselinePath string
	DiffPath     string
	Result       DiffResult
}

func (e *AssertMismatch) Error() string {
	return fmt.Sprintf("visual: %s drifted from %s by %.3f%% (%d/%d px); diff: %s",
		e.URL, e.BaselinePath, e.Result.DiffPct, e.Result.Mismatched, e.Result.Total, e.DiffPath)
}

// Assert captures url and compares it against a baseline PNG. Returns nil
// on match, *AssertMismatch on drift, or a wrapped error on capture/IO
// failure. When opts.Update is true, the current capture is written to
// BaselinePath and nil is returned.
func Assert(ctx context.Context, url string, opts AssertOptions) error {
	if strings.TrimSpace(opts.BaselinePath) == "" {
		opts.BaselinePath = DefaultBaselinePath(url)
	}
	if strings.TrimSpace(opts.DiffOutPath) == "" {
		opts.DiffOutPath = strings.TrimSuffix(opts.BaselinePath, ".png") + ".diff.png"
	}

	current, err := Capture(ctx, url, opts.CaptureOptions)
	if err != nil {
		return err
	}

	if opts.Update {
		if err := os.MkdirAll(filepath.Dir(opts.BaselinePath), 0o755); err != nil {
			return fmt.Errorf("visual: mkdir baseline: %w", err)
		}
		if err := os.WriteFile(opts.BaselinePath, current, 0o644); err != nil {
			return fmt.Errorf("visual: write baseline: %w", err)
		}
		return nil
	}

	baseline, err := os.ReadFile(opts.BaselinePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("visual: baseline %s does not exist (run with Update=true to create)", opts.BaselinePath)
		}
		return fmt.Errorf("visual: read baseline: %w", err)
	}

	result, err := Diff(baseline, current)
	if err != nil {
		return err
	}

	if result.DiffPct > opts.Threshold {
		if err := writePNG(opts.DiffOutPath, result.Diff); err != nil {
			return fmt.Errorf("visual: write diff: %w", err)
		}
		// Also drop the raw current capture next to the diff for debugging.
		currentPath := strings.TrimSuffix(opts.BaselinePath, ".png") + ".current.png"
		_ = os.WriteFile(currentPath, current, 0o644)
		return &AssertMismatch{
			URL:          url,
			BaselinePath: opts.BaselinePath,
			DiffPath:     opts.DiffOutPath,
			Result:       result,
		}
	}
	return nil
}

// DefaultBaselinePath hashes url into a stable file path under
// testdata/visual/. Callers that want human-readable paths should set
// BaselinePath explicitly.
func DefaultBaselinePath(url string) string {
	sum := sha256.Sum256([]byte(url))
	return filepath.Join("testdata", "visual", hex.EncodeToString(sum[:8])+".png")
}

func writePNG(path string, img image.Image) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

// Verify write-interface satisfies io.Writer so callers can chain.
var _ io.Writer = (*bytes.Buffer)(nil)
