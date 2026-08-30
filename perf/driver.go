package perf

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
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
	remoteWSURL string
	allocator   AllocatorSelection
}

// Option configures a Driver before launch.
type Option func(*Driver)

// AllocatorMode identifies how chromedp reaches Chrome.
type AllocatorMode string

const (
	AllocatorModeLocal  AllocatorMode = "local-exec"
	AllocatorModeRemote AllocatorMode = "remote-cdp"
)

// AllocatorSelection is the resolved browser allocator plan for a Driver.
type AllocatorSelection struct {
	Mode                        AllocatorMode
	ChromePath                  string
	RemoteWebSocketURL          string
	SanitizedRemoteWebSocketURL string
	RemoteWebSocketURLSHA256    string
}

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

// WithRemoteWebSocketURL connects the Driver to an existing CDP endpoint.
func WithRemoteWebSocketURL(wsURL string) Option {
	return func(d *Driver) { d.remoteWSURL = strings.TrimSpace(wsURL) }
}

// New creates a browser Driver from a remote CDP endpoint or a local Chrome binary.
// Local mode uses FindChrome to locate the binary.
func New(opts ...Option) (*Driver, error) {
	d := &Driver{
		headless:  true,
		timeout:   30 * time.Second,
		noSandbox: envFlag("GOSX_CHROME_NO_SANDBOX"),
	}
	for _, o := range opts {
		o(d)
	}

	selection, err := ResolveAllocatorSelection(d.remoteWSURL)
	if err != nil {
		return nil, err
	}
	d.allocator = selection

	var allocCtx context.Context
	var allocCancel context.CancelFunc
	if selection.Mode == AllocatorModeRemote {
		allocCtx, allocCancel = chromedp.NewRemoteAllocator(context.Background(), selection.RemoteWebSocketURL)
	} else {
		allocOpts := append(chromedp.DefaultExecAllocatorOptions[:],
			chromedp.ExecPath(selection.ChromePath),
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
			// Chromium 139+ no longer falls back to the Swiftshader software
			// rasterizer automatically in headless mode — it must be opted in
			// explicitly, or requestAdapter()/requestDevice() (and WebGL2) both
			// fail outright on any host without a real GPU (no /dev/dri, common
			// in headless CI / sandboxed dev containers), and Scene3D silently
			// degrades all the way to the canvas2d fallback renderer, which never
			// emits "scene3d-render" performance measures — so scene_frame_count/
			// scene_p95/scene_p99/scene_frame_max all report "metric not found"
			// even though the page itself renders fine. See chromedp's own
			// DisableGPU doc comment (allocate.go) and
			// https://chromestatus.com/feature/5166674414927872. This only adds
			// the opt-in fallback flag (not --disable-gpu), so a host with a real
			// GPU still uses hardware acceleration first.
			chromedp.Flag("enable-unsafe-swiftshader", true),
			// enable-unsafe-swiftshader alone is not sufficient on this box: Chrome
			// still needs an explicit ANGLE backend selection and the GPU blocklist
			// overridden, or it never actually routes to the Swiftshader/SwANGLE
			// software path (requestDevice() keeps failing). Matches the flag set
			// m31labs.dev's scripts/galaxy-visual-smoke.go already uses successfully
			// against this same class of GPU-less sandbox.
			chromedp.Flag("use-gl", "angle"),
			chromedp.Flag("use-angle", "gl-egl"),
			chromedp.Flag("ignore-gpu-blocklist", true),
		)
		if d.noSandbox {
			allocOpts = append(allocOpts, chromedp.Flag("no-sandbox", true))
		}
		allocCtx, allocCancel = chromedp.NewExecAllocator(context.Background(), allocOpts...)
	}

	ctx, ctxCancel := chromedp.NewContext(allocCtx)
	timeoutCancel := func() {}
	if d.timeout > 0 {
		ctx, timeoutCancel = context.WithTimeout(ctx, d.timeout)
	}

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

// ResolveAllocatorSelection picks remote CDP when an option or CHROME_WS_URL is set.
func ResolveAllocatorSelection(remoteWebSocketURL string) (AllocatorSelection, error) {
	wsURL := strings.TrimSpace(remoteWebSocketURL)
	if wsURL == "" {
		wsURL = strings.TrimSpace(os.Getenv("CHROME_WS_URL"))
	}
	if wsURL != "" {
		if err := validateChromeWebSocketURL(wsURL); err != nil {
			return AllocatorSelection{Mode: AllocatorModeRemote}, err
		}
		return AllocatorSelection{
			Mode:                        AllocatorModeRemote,
			RemoteWebSocketURL:          wsURL,
			SanitizedRemoteWebSocketURL: SanitizeChromeWebSocketURL(wsURL),
			RemoteWebSocketURLSHA256:    HashChromeWebSocketURL(wsURL),
		}, nil
	}
	chromePath, err := FindChrome()
	if err != nil {
		return AllocatorSelection{Mode: AllocatorModeLocal}, err
	}
	return AllocatorSelection{Mode: AllocatorModeLocal, ChromePath: chromePath}, nil
}

func validateChromeWebSocketURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("invalid Chrome remote endpoint: expected http, https, ws, or wss URL with host")
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "ws", "wss":
		return nil
	default:
		return fmt.Errorf("invalid Chrome remote endpoint: unsupported scheme %q", u.Scheme)
	}
}

// SanitizeChromeWebSocketURL removes credentials, path, query values, and fragments.
func SanitizeChromeWebSocketURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "remote-cdp"
	}
	u.User = nil
	u.Path = ""
	u.RawPath = ""
	u.OmitHost = false
	u.RawQuery = ""
	u.ForceQuery = false
	u.Fragment = ""
	return u.String()
}

// HashChromeWebSocketURL returns a stable identity for the full input endpoint.
func HashChromeWebSocketURL(raw string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(raw)))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// AllocatorSelection reports the Driver allocator plan.
func (d *Driver) AllocatorSelection() AllocatorSelection {
	return d.allocator
}

// BindTarget performs the first chromedp Run on the long-lived driver context.
func (d *Driver) BindTarget() error {
	return chromedp.Run(d.ctx)
}

// WithOperationContext returns a shallow Driver that uses a derived CDP context.
func (d *Driver) WithOperationContext(parent context.Context, timeout time.Duration) (*Driver, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	baseCtx := d.ctx
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	now := time.Now()
	deadline, hasDeadline := earliestContextDeadline(baseCtx, parent)
	if timeout > 0 {
		timeoutDeadline := now.Add(timeout)
		if !hasDeadline || timeoutDeadline.Before(deadline) {
			deadline = timeoutDeadline
			hasDeadline = true
		}
	}

	opCtx := baseCtx
	var cancel context.CancelFunc
	if hasDeadline {
		opCtx, cancel = context.WithDeadline(opCtx, deadline)
	} else {
		opCtx, cancel = context.WithCancel(opCtx)
	}
	done := make(chan struct{})
	go func() {
		select {
		case <-parent.Done():
			if parentDeadline, ok := parent.Deadline(); ok &&
				parent.Err() == context.DeadlineExceeded &&
				hasDeadline &&
				parentDeadline.Equal(deadline) {
				return
			}
			cancel()
		case <-opCtx.Done():
		case <-done:
		}
	}()
	clone := *d
	clone.ctx = opCtx
	clone.ctxCancel = cancel
	clone.allocCancel = func() {}
	var once sync.Once
	return &clone, func() {
		once.Do(func() {
			close(done)
			cancel()
		})
	}
}

func earliestContextDeadline(contexts ...context.Context) (time.Time, bool) {
	var earliest time.Time
	found := false
	for _, ctx := range contexts {
		if ctx == nil {
			continue
		}
		deadline, ok := ctx.Deadline()
		if !ok {
			continue
		}
		if !found || deadline.Before(earliest) {
			earliest = deadline
			found = true
		}
	}
	return earliest, found
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
