// Package chrometest provides bounded Chrome startup for browser tests.
// It is internal test infrastructure and is not part of the GoSX product
// runtime or browser asset surface.
package chrometest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

const diagnosticsLimit = 8 << 10

var defaultStartupPolicy = startupPolicy{
	attempts:       2,
	attemptTimeout: 30 * time.Second,
	overallTimeout: 65 * time.Second,
	dialTimeout:    3 * time.Second,
	retryDelay:     100 * time.Millisecond,
}

var defaultChromeArgs = []string{
	"--no-first-run",
	"--no-default-browser-check",
	"--headless",
	"--hide-scrollbars",
	"--mute-audio",
	"--disable-background-networking",
	"--enable-features=NetworkService,NetworkServiceInProcess",
	"--disable-background-timer-throttling",
	"--disable-backgrounding-occluded-windows",
	"--disable-breakpad",
	"--disable-client-side-phishing-detection",
	"--disable-default-apps",
	"--disable-dev-shm-usage",
	"--disable-extensions",
	"--disable-features=site-per-process,Translate,BlinkGenPropertyTrees",
	"--disable-hang-monitor",
	"--disable-ipc-flooding-protection",
	"--disable-popup-blocking",
	"--disable-prompt-on-repost",
	"--disable-renderer-backgrounding",
	"--disable-sync",
	"--force-color-profile=srgb",
	"--metrics-recording-only",
	"--safebrowsing-disable-auto-update",
	"--enable-automation",
	"--password-store=basic",
	"--use-mock-keychain",
}

type startupPolicy struct {
	attempts       int
	attemptTimeout time.Duration
	overallTimeout time.Duration
	dialTimeout    time.Duration
	retryDelay     time.Duration
}

// Browser is a bound Chrome process and its first tab. Close is idempotent.
type Browser struct {
	Context         context.Context
	cancelBrowser   context.CancelFunc
	cancelAllocator context.CancelFunc
	process         *chromeProcess
	diagnostics     *tailWriter
	closeOnce       sync.Once
}

// Close releases the tab, remote allocator, Chrome process, and temporary
// profile. Chrome output is drained before the profile is removed.
func (browser *Browser) Close() {
	if browser == nil {
		return
	}
	browser.closeOnce.Do(func() {
		browser.cancelAllocator()
		browser.process.stop()
		browser.cancelBrowser()
		chromedp.FromContext(browser.Context).Allocator.Wait()
	})
}

// Diagnostics returns bounded, redacted Chrome output captured since launch.
func (browser *Browser) Diagnostics() string {
	if browser == nil || browser.diagnostics == nil {
		return ""
	}
	return redactDiagnostics(browser.diagnostics.String())
}

// Start launches Chrome with a fresh profile and an ephemeral loopback CDP
// port. If Chrome remains alive but does not finish binding its empty tab within
// the attempt deadline, the helper stops and drains it before one retry with a
// new process/profile/port. Process exits and invalid endpoints fail without a
// retry. Each attempt has a 30-second startup limit; the overall startup budget
// is 65 seconds, including a process/pipe/CDP cleanup allowance. Caller
// cancellation interrupts the attempt and synchronously joins cleanup.
//
// Extra arguments must be Chrome flags. The profile and debugging address/port
// are owned by this helper and cannot be overridden.
func Start(ctx context.Context, executable string, extraArgs ...string) (*Browser, error) {
	return startWithPolicy(ctx, executable, defaultStartupPolicy, extraArgs...)
}

func startWithPolicy(ctx context.Context, executable string, policy startupPolicy, extraArgs ...string) (*Browser, error) {
	if ctx == nil || strings.TrimSpace(executable) == "" {
		return nil, errors.New("chrome startup: missing context or executable")
	}
	if err := validatePolicy(policy); err != nil {
		return nil, err
	}
	if err := validateExtraArgs(extraArgs); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	overallDeadline := time.Now().Add(policy.overallTimeout)
	failures := make([]string, 0, policy.attempts)
	for attempt := 1; attempt <= policy.attempts; attempt++ {
		deadline := time.Now().Add(policy.attemptTimeout)
		if latest := overallDeadline.Add(-cleanupAllowance); deadline.After(latest) {
			deadline = latest
		}
		if !time.Now().Before(deadline) {
			return nil, fmt.Errorf("chrome startup exhausted overall budget: %s", strings.Join(failures, "; "))
		}
		browser, failure := launchAttempt(ctx, executable, policy, deadline, extraArgs)
		if failure == nil {
			return browser, nil
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}

		failures = append(failures, fmt.Sprintf("attempt %d: %s", attempt, failure.summary(policy.attemptTimeout)))
		if !failure.transient || attempt == policy.attempts {
			return nil, fmt.Errorf("chrome startup failed after %d attempt(s): %s", attempt, strings.Join(failures, "; "))
		}
		if !time.Now().Add(policy.retryDelay).Before(overallDeadline.Add(-cleanupAllowance)) {
			return nil, fmt.Errorf("chrome startup exhausted overall budget: %s", strings.Join(failures, "; "))
		}

		timer := time.NewTimer(policy.retryDelay)
		select {
		case <-ctx.Done():
			stopTimer(timer)
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	return nil, errors.New("chrome startup: exhausted bounded attempts")
}

func validatePolicy(policy startupPolicy) error {
	if policy.attempts < 1 || policy.attempts > 3 || policy.attemptTimeout <= 0 || policy.attemptTimeout > 30*time.Second ||
		policy.overallTimeout <= cleanupAllowance || policy.overallTimeout > 65*time.Second ||
		policy.dialTimeout <= 0 || policy.dialTimeout > policy.attemptTimeout || policy.retryDelay < 0 || policy.retryDelay > time.Second {
		return errors.New("chrome startup: invalid bounded startup policy")
	}
	return nil
}

func validateExtraArgs(extraArgs []string) error {
	if len(extraArgs) > 32 {
		return errors.New("chrome startup: too many extra flags")
	}
	for index, arg := range extraArgs {
		if len(arg) < 3 || len(arg) > 512 || !strings.HasPrefix(arg, "--") || hasControl(arg) {
			return fmt.Errorf("chrome startup: invalid extra flag at index %d", index)
		}
		name := strings.TrimPrefix(strings.SplitN(arg, "=", 2)[0], "--")
		switch name {
		case "remote-debugging-address", "remote-debugging-port", "remote-debugging-pipe", "user-data-dir":
			return fmt.Errorf("chrome startup: reserved extra flag at index %d", index)
		}
	}
	return nil
}

type attemptFailure struct {
	err         error
	diagnostics string
	transient   bool
	timedOut    bool
}

func (failure *attemptFailure) summary(timeout time.Duration) string {
	var reason string
	if failure.timedOut {
		reason = fmt.Sprintf("Chrome startup exceeded its deadline (attempt limit %s)", timeout)
	} else {
		reason = redactDiagnostics(failure.err.Error())
	}
	if failure.diagnostics != "" {
		reason += "; Chrome output: " + failure.diagnostics
	}
	return reason
}

func launchAttempt(ctx context.Context, executable string, policy startupPolicy, deadline time.Time, extraArgs []string) (*Browser, *attemptFailure) {
	diagnostics := &tailWriter{limit: diagnosticsLimit}
	profileDir, err := os.MkdirTemp("", "gosx-chrome-startup-")
	if err != nil {
		return nil, &attemptFailure{err: errors.New("create temporary Chrome profile")}
	}
	process, err := startChromeProcess(ctx, executable, profileDir, diagnostics, extraArgs)
	if err != nil {
		_ = os.RemoveAll(profileDir)
		return nil, &attemptFailure{err: fmt.Errorf("start Chrome process: %s", redactDiagnostics(err.Error()))}
	}

	timer := time.NewTimer(time.Until(deadline))
	defer stopTimer(timer)
	fail := func(err error, timedOut bool) (*Browser, *attemptFailure) {
		// An exited process is not a startup hang, even if its exit races the
		// timer. Never hide a concrete launch failure behind a retry.
		select {
		case <-process.done:
			err = process.exitError("Chrome exited during startup")
			timedOut = false
		default:
		}
		process.stop()
		return nil, &attemptFailure{err: err, timedOut: timedOut, transient: timedOut,
			diagnostics: redactDiagnostics(diagnostics.String())}
	}
	select {
	case endpoint := <-process.endpoint:
		if err := validateDevToolsEndpoint(endpoint); err != nil {
			return fail(err, false)
		}
		allocatorContext, cancelAllocator := chromedp.NewRemoteAllocator(ctx, endpoint, chromedp.NoModifyURL)
		browserContext, cancelBrowser := chromedp.NewContext(
			allocatorContext,
			chromedp.WithBrowserOption(chromedp.WithDialTimeout(policy.dialTimeout)),
		)
		result := make(chan error, 1)
		go func() {
			result <- chromedp.Run(browserContext, chromedp.ActionFunc(func(ctx context.Context) error {
				// RemoteAllocator creates a new tab beside Chrome's initial
				// about:blank. Activate it before returning: an inactive tab
				// suppresses requestAnimationFrame (including chromedp.Poll).
				state := chromedp.FromContext(ctx)
				return target.ActivateTarget(state.Target.TargetID).Do(cdp.WithExecutor(ctx, state.Browser))
			}))
		}()
		cleanup := func(join bool) {
			// Cancel and kill before waiting: RemoteAllocator's socket has its
			// own context, and a half-open CDP connection must not hold cleanup.
			cancelAllocator()
			process.stop()
			if join {
				<-result
			}
			cancelBrowser()
			chromedp.FromContext(browserContext).Allocator.Wait()
		}
		select {
		case err := <-result:
			select {
			case <-process.done:
				err = process.exitError("Chrome exited during CDP bind")
			default:
			}
			if err == nil && ctx.Err() == nil && time.Now().Before(deadline) {
				return &Browser{
					Context:         browserContext,
					cancelBrowser:   cancelBrowser,
					cancelAllocator: cancelAllocator,
					process:         process,
					diagnostics:     diagnostics,
				}, nil
			}
			// CDP protocol/dial failures are concrete errors and are not retried.
			if err == nil {
				err = context.DeadlineExceeded
			}
			cleanup(false)
			return nil, &attemptFailure{err: err, diagnostics: redactDiagnostics(diagnostics.String())}
		case <-process.done:
			cleanup(true)
			return nil, &attemptFailure{
				err:         process.exitError("Chrome exited after publishing DevTools endpoint but before CDP bind"),
				diagnostics: redactDiagnostics(diagnostics.String()),
			}
		case <-timer.C:
			// Capture process state before cleanup kills it, preserving concrete
			// exits even when exit and timeout become ready together.
			transient := true
			startupErr := error(context.DeadlineExceeded)
			select {
			case <-process.done:
				transient = false
				startupErr = process.exitError("Chrome exited during CDP bind")
			default:
			}
			cleanup(true)
			return nil, &attemptFailure{
				err:         startupErr,
				diagnostics: redactDiagnostics(diagnostics.String()),
				transient:   transient,
				timedOut:    transient,
			}
		case <-ctx.Done():
			cleanup(true)
			return nil, &attemptFailure{err: ctx.Err(), diagnostics: redactDiagnostics(diagnostics.String())}
		}

	case <-process.done:
		return fail(process.exitError("Chrome exited before publishing DevTools endpoint"), false)

	case <-timer.C:
		return fail(context.DeadlineExceeded, true)

	case <-ctx.Done():
		return fail(ctx.Err(), false)
	}
}

const (
	processWaitDelay = 2 * time.Second
	cleanupAllowance = processWaitDelay + time.Second
)

type chromeProcess struct {
	cancel     context.CancelFunc
	terminate  func() error
	done       chan struct{}
	drained    chan struct{}
	output     *os.File
	endpoint   chan string
	profileDir string

	mu       sync.Mutex
	waitErr  error
	stopOnce sync.Once
}

func startChromeProcess(ctx context.Context, executable, profileDir string, diagnostics *tailWriter, extraArgs []string) (*chromeProcess, error) {
	processContext, cancel := context.WithCancel(ctx)
	args := make([]string, 0, len(defaultChromeArgs)+len(extraArgs)+4)
	args = append(args, defaultChromeArgs...)
	args = append(args, extraArgs...)
	args = append(args,
		"--remote-debugging-address=127.0.0.1",
		"--remote-debugging-port=0",
		"--user-data-dir="+profileDir,
		"about:blank",
	)
	cmd := exec.CommandContext(processContext, executable, args...)
	configureProcess(cmd)
	cmd.WaitDelay = processWaitDelay
	endpoint := make(chan string, 1)
	output, childOutput, err := os.Pipe()
	if err != nil {
		cancel()
		return nil, err
	}
	// Own the pipe drain separately from cmd.Wait. Otherwise exec.Cmd waits
	// for copy goroutines before reporting exit, and a descendant holding the
	// pipe open can turn an immediate fatal process exit into a timeout/retry.
	cmd.Stdout = childOutput
	cmd.Stderr = childOutput
	if err := cmd.Start(); err != nil {
		_ = childOutput.Close()
		_ = output.Close()
		cancel()
		return nil, err
	}
	_ = childOutput.Close()

	process := &chromeProcess{
		cancel:     cancel,
		terminate:  cmd.Cancel,
		done:       make(chan struct{}),
		drained:    make(chan struct{}),
		output:     output,
		endpoint:   endpoint,
		profileDir: profileDir,
	}
	go func() {
		writer := &endpointWriter{diagnostics: diagnostics, endpoint: endpoint, endpointOnce: new(sync.Once)}
		_, _ = io.Copy(writer, output)
		writer.Flush()
		close(process.drained)
	}()
	go func() {
		waitErr := cmd.Wait()
		process.mu.Lock()
		process.waitErr = waitErr
		process.mu.Unlock()
		close(process.done)
	}()
	return process, nil
}

func (process *chromeProcess) stop() {
	if process == nil {
		return
	}
	process.stopOnce.Do(func() {
		// Also terminate after an early parent exit: CommandContext may have
		// already stopped watching while descendants still own the pipes.
		_ = process.terminate()
		process.cancel()
		<-process.done
		timer := time.NewTimer(processWaitDelay)
		select {
		case <-process.drained:
		case <-timer.C:
			// Non-Linux platforms may not kill descendant processes as a
			// group. Closing our read end still bounds the pipe-drain wait.
		}
		stopTimer(timer)
		_ = process.output.Close()
		<-process.drained
		_ = os.RemoveAll(process.profileDir)
	})
}

func (process *chromeProcess) exitError(prefix string) error {
	process.mu.Lock()
	defer process.mu.Unlock()
	if process.waitErr == nil {
		return errors.New(prefix)
	}
	return fmt.Errorf("%s: %s", prefix, redactDiagnostics(process.waitErr.Error()))
}

type endpointWriter struct {
	mu           sync.Mutex
	partial      []byte
	diagnostics  *tailWriter
	endpoint     chan<- string
	endpointOnce *sync.Once
}

var endpointPattern = regexp.MustCompile(`DevTools listening on\s+(wss?://[^\s]+)`)

func (writer *endpointWriter) Write(data []byte) (int, error) {
	_, _ = writer.diagnostics.Write(data)
	writer.mu.Lock()
	defer writer.mu.Unlock()
	writer.partial = append(writer.partial, data...)
	for {
		newline := strings.IndexByte(string(writer.partial), '\n')
		if newline < 0 {
			if len(writer.partial) > 4096 {
				writer.scan(writer.partial)
				writer.partial = append(writer.partial[:0], writer.partial[len(writer.partial)-2048:]...)
			}
			break
		}
		writer.scan(writer.partial[:newline])
		writer.partial = writer.partial[newline+1:]
	}
	return len(data), nil
}

func (writer *endpointWriter) Flush() {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	writer.scan(writer.partial)
	writer.partial = nil
}

func (writer *endpointWriter) scan(line []byte) {
	match := endpointPattern.FindSubmatch(line)
	if len(match) != 2 {
		return
	}
	endpoint := string(match[1])
	writer.endpointOnce.Do(func() {
		writer.endpoint <- endpoint
	})
}

func validateDevToolsEndpoint(raw string) error {
	if raw == "" || len(raw) > 2048 || hasControl(raw) {
		return errors.New("Chrome published an invalid DevTools endpoint")
	}
	endpoint, err := url.Parse(raw)
	if err != nil || endpoint.Scheme != "ws" || endpoint.User != nil || endpoint.Hostname() == "" || endpoint.Port() == "" || endpoint.RawQuery != "" || endpoint.ForceQuery || endpoint.Fragment != "" || endpoint.Opaque != "" {
		return errors.New("Chrome published an invalid DevTools endpoint")
	}
	ip := net.ParseIP(endpoint.Hostname())
	if ip == nil || !ip.IsLoopback() {
		return errors.New("Chrome published a non-loopback DevTools endpoint")
	}
	port, err := strconv.ParseUint(endpoint.Port(), 10, 16)
	if err != nil || port == 0 {
		return errors.New("Chrome published an invalid DevTools endpoint port")
	}
	const browserPath = "/devtools/browser/"
	if !strings.HasPrefix(endpoint.EscapedPath(), browserPath) || len(endpoint.EscapedPath()) == len(browserPath) {
		return errors.New("Chrome published an invalid DevTools browser path")
	}
	return nil
}

func stopTimer(timer *time.Timer) {
	if timer == nil || timer.Stop() {
		return
	}
	select {
	case <-timer.C:
	default:
	}
}

type tailWriter struct {
	mu        sync.Mutex
	limit     int
	buffer    []byte
	truncated bool
}

func (writer *tailWriter) Write(data []byte) (int, error) {
	written := len(data)
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if writer.limit <= 0 {
		writer.truncated = writer.truncated || len(data) > 0
		return written, nil
	}
	if len(data) >= writer.limit {
		writer.buffer = append(writer.buffer[:0], data[len(data)-writer.limit:]...)
		writer.truncated = true
		return written, nil
	}
	if overflow := len(writer.buffer) + len(data) - writer.limit; overflow > 0 {
		copy(writer.buffer, writer.buffer[overflow:])
		writer.buffer = writer.buffer[:len(writer.buffer)-overflow]
		writer.truncated = true
	}
	writer.buffer = append(writer.buffer, data...)
	return written, nil
}

func (writer *tailWriter) String() string {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	output := string(writer.buffer)
	if writer.truncated {
		// A byte-tail may begin inside a URL or secret value. Discard the
		// incomplete first line so redaction always has the key/prefix.
		if newline := strings.IndexByte(output, '\n'); newline >= 0 {
			output = output[newline+1:]
		} else {
			output = ""
		}
		output = "[earlier output truncated]\n" + output
	}
	return output
}

var (
	ansiDiagnosticPattern    = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)
	urlDiagnosticPattern     = regexp.MustCompile(`(?i)\b(?:wss?|https?)://[^\s"'<>]+`)
	secretDiagnosticPattern  = regexp.MustCompile(`(?i)\b(token|secret|password|authorization)["']?\s*[:=]\s*(?:"[^"]*"|'[^']*'|[^\n]+)`)
	profileDiagnosticPattern = regexp.MustCompile(`(?i)(?:[A-Za-z]:)?[/\\][^\s"']*(?:chromedp-runner|gosx-chrome-startup-)[^\s"']*`)
	socketDiagnosticPattern  = regexp.MustCompile(`\b(?:[0-9]{1,3}\.){3}[0-9]{1,3}:[0-9]+\b`)
)

func redactDiagnostics(raw string) string {
	if raw == "" {
		return ""
	}
	raw = ansiDiagnosticPattern.ReplaceAllString(raw, "")
	safe := strings.Map(func(r rune) rune {
		switch r {
		case '\n', '\t':
			return r
		case '\r':
			return '\n'
		default:
			if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
				return '�'
			}
			return r
		}
	}, raw)
	safe = urlDiagnosticPattern.ReplaceAllStringFunc(safe, func(value string) string {
		if index := strings.Index(value, "://"); index >= 0 {
			return value[:index] + "://<redacted>"
		}
		return "<redacted-url>"
	})
	safe = secretDiagnosticPattern.ReplaceAllString(safe, "$1=<redacted>")
	safe = profileDiagnosticPattern.ReplaceAllString(safe, "<chrome-profile>")
	safe = socketDiagnosticPattern.ReplaceAllString(safe, "<chrome-endpoint>")
	if len(safe) > diagnosticsLimit {
		safe = safe[:diagnosticsLimit] + " [output truncated]"
	}
	return strings.TrimSpace(safe)
}

func hasControl(value string) bool {
	for _, r := range value {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}
