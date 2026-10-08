package server

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"m31labs.dev/gosx/internal/telemetryerr"
	"m31labs.dev/gosx/scheduled"
)

// ShutdownHooks belongs to an application's existing resource owner. Signal
// must only stop admission or cancel work: it must not wait or perform I/O.
// Drain and Flush must cooperate with their context. HTTP and the scheduler
// drain first, then source Drains run in order and Flushes in reverse order.
type ShutdownHooks struct {
	Signal func(context.Context)
	Drain  func(context.Context) error
	Flush  func(context.Context) error
}

// ShutdownSource keeps resource ownership with its implementation. SignalShutdown
// stops admission without waiting or performing I/O; Drain waits cooperatively
// for that same owner. Mounting a handler does not register a shutdown source.
type ShutdownSource interface {
	SignalShutdown(context.Context)
	Drain(context.Context) error
}

// UseShutdownSource registers an existing owner before Build. The server needs
// only this structural contract, so ordinary HTTP apps need no socket package.
func (a *App) UseShutdownSource(name string, source ShutdownSource) (func(), error) {
	if source == nil {
		return nil, shutdownConfigError(&telemetryerr.ConfigError{Field: "shutdown_source", Code: "required"})
	}
	return a.UseShutdownHook(name, ShutdownHooks{Signal: source.SignalShutdown, Drain: source.Drain})
}

type shutdownHook struct {
	name   string
	hooks  ShutdownHooks
	active bool
	signal sync.Once
}

type appShutdown struct {
	mu        sync.Mutex // registration, resource publication, and completion
	hooks     []*shutdownHook
	done      chan struct{}
	errors    []error
	result    error
	panicOnce sync.Once
	// Tests may shorten the no-deadline cooperative window before Shutdown.
	// Zero preserves the production 30-second window.
	noDeadlineGrace time.Duration
}

// ConfigurationOpen reports whether lifecycle extensions can be attached.
// Configure the App on one goroutine before its first Build.
func (a *App) ConfigurationOpen() bool {
	return a != nil && !a.configurationClosed.Load() && !a.draining.Load()
}

// UseShutdownHook reserves a unique name before Build. Nil callbacks are
// accepted. Removal is idempotent and only takes effect before Build.
func (a *App) UseShutdownHook(name string, hooks ShutdownHooks) (func(), error) {
	if a == nil {
		return nil, shutdownConfigError(&telemetryerr.ConfigError{Field: "app", Code: "required"})
	}
	if strings.TrimSpace(name) == "" {
		return nil, shutdownConfigError(&telemetryerr.ConfigError{Field: "shutdown_hook", Code: "name_required"})
	}
	a.shutdown.mu.Lock()
	defer a.shutdown.mu.Unlock()
	if !a.ConfigurationOpen() {
		return nil, shutdownConfigError(telemetryerr.ErrAfterBuild)
	}
	for _, hook := range a.shutdown.hooks {
		if hook.active && hook.name == name {
			return nil, shutdownConfigError(&telemetryerr.ConfigError{Field: "shutdown_hook", Code: "duplicate_name"})
		}
	}
	hook := &shutdownHook{name: name, hooks: hooks, active: true}
	a.shutdown.hooks = append(a.shutdown.hooks, hook)
	return func() {
		a.shutdown.mu.Lock()
		defer a.shutdown.mu.Unlock()
		if a.ConfigurationOpen() {
			hook.active = false
			hook.hooks = ShutdownHooks{}
		}
	}, nil
}

// Shutdown starts one pipeline with the first caller's context. Each caller
// waits subject to its own deadline. An unfinished callback retains the
// pipeline and its resources; an expired caller never reports completion.
// External HTTP hosts must drain their server before calling this method.
// Hijacked WebSockets must be closed by an explicit owner Drain hook.
// Shutdown is terminal: this App cannot subsequently ListenAndServe again.
// Scheduled work drains for at most 30 seconds (or ShutdownGrace when a
// deadline is present). A deadline reserves max(5 seconds, 25% of its remaining
// time) for hooks. After cancellation, deadline shutdowns wait only until the
// deadline minus half that reserve; hooks can therefore overlap a cancelled
// run still unwinding. Without a deadline, cancelled runs are joined without
// limit. Drain and Flush callbacks are always attempted afterward.
func (a *App) Shutdown(ctx context.Context) error {
	if a == nil {
		return nil
	}
	a.shutdown.mu.Lock()
	if a.shutdown.done == nil {
		a.draining.Store(true)
		a.configurationClosed.Store(true)
		a.shutdown.done = make(chan struct{})
		hooks := make([]*shutdownHook, 0, len(a.shutdown.hooks))
		for _, hook := range a.shutdown.hooks {
			if hook.active {
				hooks = append(hooks, hook)
			}
		}
		go a.runShutdown(ctx, a.srv, a.scheduler, hooks)
	}
	done := a.shutdown.done
	a.shutdown.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		if err := ctx.Err(); err != nil {
			return err
		}
		return a.shutdown.result // published by closing done
	}
}

func (a *App) runShutdown(ctx context.Context, srv *http.Server, scheduler *scheduled.Scheduler, hooks []*shutdownHook) {
	// One cancellation callback covers this pipeline, even while a Drain is
	// blocked. There is no waiter goroutine per hook or Shutdown caller.
	signalAll := func() {
		for _, hook := range hooks {
			a.signalShutdownHook(ctx, hook)
		}
	}
	stopSignal := context.AfterFunc(ctx, signalAll)
	if srv != nil {
		a.addShutdownError("http_drain_failed", srv.Shutdown(ctx))
	}
	if scheduler != nil {
		grace := scheduledDrainWindow(ctx, scheduler.ShutdownGrace())
		deadline, hasDeadline := ctx.Deadline()
		var joinDeadline time.Time
		if hasDeadline {
			remaining := max(0, time.Until(deadline))
			// A reserve cannot exceed the owner's remaining time. For a short
			// deadline, spend at most half that time joining cancelled work.
			reserve := min(remaining, max(5*time.Second, remaining/4))
			joinDeadline = deadline.Add(-reserve / 2)
		} else if a.shutdown.noDeadlineGrace > 0 {
			grace = a.shutdown.noDeadlineGrace
		}
		// Cancellation retains the legacy context.Canceled task cause, even
		// when the cooperative window or the owner's deadline expires.
		drainCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
		stopCancel := context.AfterFunc(ctx, cancel)
		timer := time.AfterFunc(grace, cancel)
		err := scheduler.StopContext(drainCtx)
		timer.Stop()
		stopCancel()
		cancel()
		if err != nil && ctx.Err() == nil {
			joinCtx := ctx
			joinCancel := func() {}
			if hasDeadline {
				joinCtx, joinCancel = context.WithDeadline(ctx, joinDeadline)
			}
			err = scheduler.StopContext(joinCtx)
			joinCancel()
		}
		a.addShutdownError("scheduler_stop_failed", err)
	}
	for _, hook := range hooks {
		if hook.hooks.Drain != nil {
			a.signalShutdownHook(ctx, hook)
		}
	}
	for _, hook := range hooks {
		if hook.hooks.Drain != nil {
			a.callShutdownHook("hook_drain_failed", ctx, hook.hooks.Drain)
		}
	}
	for i := len(hooks) - 1; i >= 0; i-- {
		if hooks[i].hooks.Flush != nil {
			a.callShutdownHook("hook_flush_failed", ctx, hooks[i].hooks.Flush)
		}
	}
	if !stopSignal() || ctx.Err() != nil {
		signalAll() // joins any already admitted, non-blocking Signals
	}
	a.addShutdownError("deadline", ctx.Err())
	a.shutdown.mu.Lock()
	a.shutdown.result = errors.Join(a.shutdown.errors...)
	a.shutdown.hooks = nil
	close(a.shutdown.done)
	a.shutdown.mu.Unlock()
}

// A context without a deadline preserves the legacy 30-second cancellation
// boundary. With a deadline, leave a reserve for source drains and final flush.
func scheduledDrainWindow(ctx context.Context, configured time.Duration) time.Duration {
	deadline, ok := ctx.Deadline()
	if !ok {
		return 30 * time.Second
	}
	remaining := time.Until(deadline)
	reserve := max(5*time.Second, remaining/4)
	return max(0, min(configured, remaining-reserve))
}

// Keep lifecycle errors in the server namespace while preserving the shared
// configuration identities for errors.Is and errors.As.
type shutdownConfigurationError struct{ cause error }

func shutdownConfigError(cause error) error { return &shutdownConfigurationError{cause} }
func (e *shutdownConfigurationError) Error() string {
	if c, ok := e.cause.(*telemetryerr.ConfigError); ok {
		return "server: invalid " + c.Field + " (" + c.Code + ")"
	}
	return "server: configuration closed after build"
}
func (e *shutdownConfigurationError) Unwrap() error { return e.cause }

func (a *App) signalShutdownHook(ctx context.Context, hook *shutdownHook) {
	hook.signal.Do(func() {
		if hook.hooks.Signal != nil {
			a.callShutdownHook("hook_signal_failed", ctx, func(ctx context.Context) error {
				hook.hooks.Signal(ctx)
				return nil
			})
		}
	})
}

func (a *App) callShutdownHook(code string, ctx context.Context, fn func(context.Context) error) {
	defer func() {
		if recover() != nil {
			a.addShutdownError(code+"_panic", shutdownPanic{})
			a.shutdown.panicOnce.Do(func() { log.Print("[gosx] shutdown callback panic") })
		}
	}()
	a.addShutdownError(code, fn(ctx))
}

type shutdownPanic struct{}

func (shutdownPanic) Error() string { return "server: shutdown callback panic" }

// The original error remains inspectable in process; Error exports only a
// fixed phase class and never application names or arbitrary callback text.
type shutdownError struct {
	code  string
	cause error
}

func (e *shutdownError) Error() string { return "server: shutdown " + e.code }
func (e *shutdownError) Unwrap() error { return e.cause }

func (a *App) addShutdownError(code string, err error) {
	if err == nil {
		return
	}
	a.shutdown.mu.Lock()
	a.shutdown.errors = append(a.shutdown.errors, &shutdownError{code: code, cause: err})
	a.shutdown.mu.Unlock()
}
