package perf

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// Driver wraps a chromedp allocator and session context.
type Driver struct {
	allocCancel context.CancelFunc
	ctxCancel   context.CancelFunc
	ctx         context.Context
	timeout     time.Duration
	headless    bool
	noSandbox   bool
}

// Option configures a Driver before launch.
type Option func(*Driver)

// WithHeadless controls whether Chrome runs in headless mode (default true).
func WithHeadless(v bool) Option {
	return func(d *Driver) { d.headless = v }
}

// WithTimeout sets the overall browser operation timeout (default 30s).
func WithTimeout(dur time.Duration) Option {
	return func(d *Driver) { d.timeout = dur }
}

// WithNoSandbox disables Chrome's process sandbox for constrained CI runners.
func WithNoSandbox(v bool) Option {
	return func(d *Driver) { d.noSandbox = v }
}

// New launches a Chrome instance and returns a Driver.
// Uses FindChrome to locate the binary.
func New(opts ...Option) (*Driver, error) {
	d := &Driver{
		headless:  true,
		timeout:   30 * time.Second,
		noSandbox: envFlag("GOSX_CHROME_NO_SANDBOX"),
	}
	for _, o := range opts {
		o(d)
	}

	chromePath, err := FindChrome()
	if err != nil {
		return nil, err
	}

	allocOpts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(chromePath),
		chromedp.Flag("headless", d.headless),
		// WebGPU/GPU enablement for headless. Without these, navigator.gpu
		// exists but requestAdapter() returns null, so scene3d falls back
		// to WebGL2 even when the site explicitly probes WebGPU. Kept
		// conservative: only enable-unsafe-webgpu, no ANGLE or Vulkan
		// overrides that would fail on systems without those drivers
		// (WSL, headless CI, etc.). A real GPU + driver is still required
		// for requestAdapter() to succeed — these flags just remove the
		// hard disable so the browser can try.
		chromedp.Flag("enable-unsafe-webgpu", true),
	)
	if d.noSandbox {
		allocOpts = append(allocOpts, chromedp.Flag("no-sandbox", true))
	}

	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), allocOpts...)

	ctx, ctxCancel := chromedp.NewContext(allocCtx)
	ctx, timeoutCancel := context.WithTimeout(ctx, d.timeout)

	// Wrap both cancels so Close handles them together.
	origCtxCancel := ctxCancel
	d.allocCancel = allocCancel
	d.ctxCancel = func() {
		timeoutCancel()
		origCtxCancel()
	}
	d.ctx = ctx

	return d, nil
}

// Close shuts down the browser and cleans up all contexts.
func (d *Driver) Close() error {
	d.ctxCancel()
	d.allocCancel()
	return nil
}

// Context returns the chromedp context for running actions directly.
func (d *Driver) Context() context.Context {
	return d.ctx
}

// Navigate goes to the given URL and waits for DOMContentLoaded.
func (d *Driver) Navigate(url string) error {
	return chromedp.Run(d.ctx, chromedp.Navigate(url))
}

// Evaluate runs JavaScript in the page and unmarshals the result into dst.
// If dst is nil the result is discarded. If the expression returns a Promise,
// Evaluate awaits it and returns the resolved value — letting REPL users and
// query helpers write `(async () => { ... })()` without needing a separate
// polling loop.
func (d *Driver) Evaluate(expr string, dst interface{}) error {
	return chromedp.Run(d.ctx, chromedp.Evaluate(expr, dst, func(p *runtime.EvaluateParams) *runtime.EvaluateParams {
		return p.WithAwaitPromise(true)
	}))
}

// WaitReady waits for the page body to be present in the DOM.
func (d *Driver) WaitReady() error {
	return chromedp.Run(d.ctx, chromedp.WaitReady("body", chromedp.ByQuery))
}

func envFlag(name string) bool {
	switch strings.ToLower(os.Getenv(name)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
