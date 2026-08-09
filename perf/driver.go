package perf

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// Driver wraps a chromedp allocator and session context.
type Driver struct {
	allocCancel context.CancelFunc
	ctxCancel   context.CancelFunc
	ctx         context.Context
	state       *driverState
	timeout     time.Duration
	headless    bool
	noSandbox   bool
	remoteWSURL string
	remoteRaw   string
	allocator   AllocatorSelection
}

type driverState struct {
	runGate  chan struct{}
	mu       sync.Mutex
	unusable bool
	err      error
}

// Option configures a Driver before launch.
type Option func(*Driver)

// AllocatorMode identifies how chromedp reaches Chrome.
type AllocatorMode string

const (
	AllocatorModeLocal  AllocatorMode = "local-exec"
	AllocatorModeRemote AllocatorMode = "remote-cdp"
)

const defaultOperationTimeout = 30 * time.Second
const operationCleanupTimeout = 250 * time.Millisecond

var errDriverUnusable = fmt.Errorf("perf: browser driver is unusable after timed-out operation")

var activeDriverOperationTimers atomic.Int64

// AllocatorSelection is the resolved browser allocator plan for a Driver.
type AllocatorSelection struct {
	Mode                        AllocatorMode
	ChromePath                  string
	SanitizedRemoteWebSocketURL string
	RemoteWebSocketURLSHA256    string
}

type allocatorResolution struct {
	selection          AllocatorSelection
	remoteWebSocketURL string
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
		state: &driverState{
			runGate: make(chan struct{}, 1),
		},
	}
	d.state.runGate <- struct{}{}
	for _, o := range opts {
		o(d)
	}

	resolution, err := resolveAllocator(d.remoteWSURL)
	if err != nil {
		return nil, err
	}
	selection := resolution.selection
	d.allocator = selection
	d.remoteRaw = resolution.remoteWebSocketURL

	var allocCtx context.Context
	var allocCancel context.CancelFunc
	if selection.Mode == AllocatorModeRemote {
		allocCtx, allocCancel = chromedp.NewRemoteAllocator(context.Background(), resolution.remoteWebSocketURL)
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
			// for requestAdapter() to succeed - these flags just remove the
			// hard disable so the browser can try.
			chromedp.Flag("enable-unsafe-webgpu", true),
			// Chromium 139+ no longer falls back to the Swiftshader software
			// rasterizer automatically in headless mode - it must be opted in
			// explicitly, or requestAdapter()/requestDevice() (and WebGL2) both
			// fail outright on any host without a real GPU (no /dev/dri, common
			// in headless CI / sandboxed dev containers), and Scene3D silently
			// degrades all the way to the canvas2d fallback renderer, which never
			// emits "scene3d-render" performance measures - so scene_frame_count/
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

	// Wrap both cancels so Close handles them together.
	d.allocCancel = allocCancel
	d.ctxCancel = ctxCancel
	d.ctx = ctx

	return d, nil
}

// ResolveAllocatorSelection picks remote CDP when an option or CHROME_WS_URL is set.
func ResolveAllocatorSelection(remoteWebSocketURL string) (AllocatorSelection, error) {
	resolution, err := resolveAllocator(remoteWebSocketURL)
	return resolution.selection, err
}

func resolveAllocator(remoteWebSocketURL string) (allocatorResolution, error) {
	wsURL := strings.TrimSpace(remoteWebSocketURL)
	if wsURL == "" {
		wsURL = strings.TrimSpace(os.Getenv("CHROME_WS_URL"))
	}
	if wsURL != "" {
		if err := validateChromeWebSocketURL(wsURL); err != nil {
			return allocatorResolution{selection: AllocatorSelection{Mode: AllocatorModeRemote}}, err
		}
		return allocatorResolution{
			remoteWebSocketURL: wsURL,
			selection: AllocatorSelection{
				Mode:                        AllocatorModeRemote,
				SanitizedRemoteWebSocketURL: SanitizeChromeWebSocketURL(wsURL),
				RemoteWebSocketURLSHA256:    HashChromeWebSocketURL(wsURL),
			},
		}, nil
	}
	chromePath, err := FindChrome()
	if err != nil {
		return allocatorResolution{selection: AllocatorSelection{Mode: AllocatorModeLocal}}, err
	}
	return allocatorResolution{selection: AllocatorSelection{Mode: AllocatorModeLocal, ChromePath: chromePath}}, nil
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
	return d.Run()
}

// WithOperationContext returns a shallow Driver that uses a derived CDP context.
func (d *Driver) WithOperationContext(parent context.Context, timeout time.Duration) (*Driver, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	opCtx := d.ctx
	var cancel context.CancelFunc
	if timeout > 0 {
		opCtx, cancel = context.WithTimeout(opCtx, timeout)
	} else {
		opCtx, cancel = context.WithCancel(opCtx)
	}
	done := make(chan struct{})
	go func() {
		select {
		case <-parent.Done():
			cancel()
		case <-opCtx.Done():
		case <-done:
		}
	}()
	clone := &Driver{
		allocCancel: func() {},
		ctxCancel:   cancel,
		ctx:         opCtx,
		state:       d.state,
		timeout:     d.timeout,
		headless:    d.headless,
		noSandbox:   d.noSandbox,
		remoteWSURL: d.remoteWSURL,
		remoteRaw:   d.remoteRaw,
		allocator:   d.allocator,
	}
	var once sync.Once
	return clone, func() {
		once.Do(func() {
			close(done)
			cancel()
		})
	}
}

// Close shuts down the browser and cleans up all contexts.
func (d *Driver) Close() error {
	d.ctxCancel()
	d.allocCancel()
	return nil
}

// Context returns the Driver status context. Remote mode hides chromedp values.
func (d *Driver) Context() context.Context {
	if d.allocator.Mode == AllocatorModeRemote {
		return statusOnlyContext{Context: d.ctx}
	}
	return d.ctx
}

// Done reports when the Driver base context ends.
func (d *Driver) Done() <-chan struct{} {
	return d.ctx.Done()
}

// ListenTarget subscribes to Chrome target events for the Driver lifecycle.
func (d *Driver) ListenTarget(fn func(ev any)) context.CancelFunc {
	listenCtx, cancel := context.WithCancel(d.ctx)
	chromedp.ListenTarget(listenCtx, fn)
	return cancel
}

// Run executes chromedp actions with a bounded operation context.
func (d *Driver) Run(actions ...chromedp.Action) error {
	if d == nil {
		return fmt.Errorf("perf: nil driver")
	}
	if len(actions) == 0 {
		actions = []chromedp.Action{chromedp.ActionFunc(func(context.Context) error { return nil })}
	}
	if err := d.unusableError(); err != nil {
		return d.redactRemoteError(err)
	}
	if err := d.acquireRunGate(context.Background()); err != nil {
		return d.redactRemoteError(err)
	}
	defer d.releaseRunGate()

	if err := d.unusableError(); err != nil {
		return d.redactRemoteError(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- chromedp.Run(d.ctx, actions...)
	}()
	timer := time.NewTimer(d.operationTimeout())
	var cleanupOnce sync.Once
	cleanup := func() {
		cleanupOnce.Do(func() {
			timer.Stop()
			activeDriverOperationTimers.Add(-1)
		})
	}
	activeDriverOperationTimers.Add(1)
	select {
	case err := <-done:
		cleanup()
		return d.redactRemoteError(err)
	case <-timer.C:
		cleanup()
		select {
		case err := <-done:
			if err != nil {
				return d.redactRemoteError(err)
			}
			return d.redactRemoteError(context.DeadlineExceeded)
		case <-time.After(operationCleanupTimeout):
			d.markUnusable()
			return d.redactRemoteError(errDriverUnusable)
		}
	case <-d.Done():
		cleanup()
		return d.redactRemoteError(d.ctx.Err())
	}
}

// RunFunc executes a CDP operation that needs direct access to the operation context.
func (d *Driver) RunFunc(fn func(context.Context) error) error {
	if fn == nil {
		return d.Run()
	}
	return d.Run(chromedp.ActionFunc(fn))
}

func (d *Driver) operationTimeout() time.Duration {
	if d.timeout > 0 {
		return d.timeout
	}
	return defaultOperationTimeout
}

func (d *Driver) acquireRunGate(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-d.state.runGate:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-d.Done():
		return d.ctx.Err()
	}
}

func (d *Driver) releaseRunGate() {
	select {
	case d.state.runGate <- struct{}{}:
	default:
	}
}

func (d *Driver) unusableError() error {
	if d == nil || d.state == nil {
		return nil
	}
	d.state.mu.Lock()
	defer d.state.mu.Unlock()
	if !d.state.unusable {
		return nil
	}
	if d.state.err != nil {
		return d.state.err
	}
	return errDriverUnusable
}

func (d *Driver) markUnusable() {
	if d == nil || d.state == nil {
		return
	}
	d.state.mu.Lock()
	already := d.state.unusable
	d.state.unusable = true
	if d.state.err == nil {
		d.state.err = errDriverUnusable
	}
	d.state.mu.Unlock()
	if !already {
		d.ctxCancel()
		d.allocCancel()
	}
}

// Navigate goes to the given URL and waits for DOMContentLoaded.
func (d *Driver) Navigate(url string) error {
	return d.Run(chromedp.Navigate(url))
}

// Evaluate runs JavaScript in the page and unmarshals the result into dst.
// If dst is nil the result is discarded. If the expression returns a Promise,
// Evaluate awaits it and returns the resolved value, letting REPL users and
// query helpers write `(async () => { ... })()` without needing a separate
// polling loop.
func (d *Driver) Evaluate(expr string, dst interface{}) error {
	return d.Run(chromedp.Evaluate(expr, dst, func(p *runtime.EvaluateParams) *runtime.EvaluateParams {
		return p.WithAwaitPromise(true)
	}))
}

// WaitReady waits for the page body to be present in the DOM.
func (d *Driver) WaitReady() error {
	return d.Run(chromedp.WaitReady("body", chromedp.ByQuery))
}

func (d *Driver) redactRemoteError(err error) error {
	if err == nil || d.allocator.Mode != AllocatorModeRemote {
		return err
	}
	return fmt.Errorf("%s", RedactChromeRemoteEndpointText(d.remoteRaw, err.Error()))
}

type statusOnlyContext struct {
	context.Context
}

func (c statusOnlyContext) Value(key any) any { return nil }

func (s AllocatorSelection) String() string {
	body, err := json.Marshal(s)
	if err != nil {
		return string(s.Mode)
	}
	return string(body)
}

var chromeRemoteEndpointPattern = regexp.MustCompile(`(?i)\b(?:wss?|https?)://[^\s"'<>]+`)

// RedactChromeRemoteEndpointText removes CDP endpoints and derived secret tokens from text.
func RedactChromeRemoteEndpointText(rawEndpoint, text string) string {
	redacted := chromeRemoteEndpointPattern.ReplaceAllString(text, "remote-cdp-endpoint")
	for _, value := range chromeRemoteRedactionTokens(rawEndpoint) {
		redacted = strings.ReplaceAll(redacted, value, "remote-cdp-redacted")
	}
	return redacted
}

func chromeRemoteRedactionTokens(rawEndpoint string) []string {
	rawEndpoint = strings.TrimSpace(rawEndpoint)
	if rawEndpoint == "" {
		return nil
	}
	tokens := []string{rawEndpoint}
	u, err := url.Parse(rawEndpoint)
	if err != nil {
		return compactRedactionTokens(tokens)
	}
	tokens = append(tokens,
		u.String(),
		u.Path,
		strings.TrimPrefix(u.Path, "/"),
		u.EscapedPath(),
		strings.TrimPrefix(u.EscapedPath(), "/"),
		u.RawQuery,
		u.Fragment,
	)
	if base := pathBase(u.Path); base != "" {
		tokens = append(tokens, base)
	}
	if base := pathBase(u.EscapedPath()); base != "" {
		tokens = append(tokens, base)
	}
	if u.User != nil {
		tokens = append(tokens, u.User.Username())
		if pass, ok := u.User.Password(); ok {
			tokens = append(tokens, pass)
		}
	}
	for key, values := range u.Query() {
		tokens = append(tokens, key)
		tokens = append(tokens, values...)
	}
	return compactRedactionTokens(tokens)
}

func compactRedactionTokens(tokens []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(tokens))
	for _, token := range tokens {
		token = strings.TrimSpace(token)
		if token == "" || token == "/" {
			continue
		}
		if _, ok := seen[token]; ok {
			continue
		}
		seen[token] = struct{}{}
		out = append(out, token)
	}
	return out
}

func pathBase(value string) string {
	value = strings.TrimRight(value, "/")
	if value == "" {
		return ""
	}
	idx := strings.LastIndex(value, "/")
	if idx < 0 {
		return value
	}
	return value[idx+1:]
}

func envFlag(name string) bool {
	switch strings.ToLower(os.Getenv(name)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
