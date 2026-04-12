package perf

import (
	"context"
	"time"

	"github.com/chromedp/chromedp"
)

// Driver wraps a chromedp allocator and session context.
type Driver struct {
	allocCancel context.CancelFunc
	ctxCancel   context.CancelFunc
	ctx         context.Context
	timeout     time.Duration
	headless    bool
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

// New launches a Chrome instance and returns a Driver.
// Uses FindChrome to locate the binary.
func New(opts ...Option) (*Driver, error) {
	d := &Driver{
		headless: true,
		timeout:  30 * time.Second,
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
	)

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
// If dst is nil the result is discarded.
func (d *Driver) Evaluate(expr string, dst interface{}) error {
	return chromedp.Run(d.ctx, chromedp.Evaluate(expr, dst))
}

// WaitReady waits for the page body to be present in the DOM.
func (d *Driver) WaitReady() error {
	return chromedp.Run(d.ctx, chromedp.WaitReady("body", chromedp.ByQuery))
}
