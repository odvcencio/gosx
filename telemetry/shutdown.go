package telemetry

import (
	"context"
	"log/slog"
	"time"
)

func (t *Telemetry) run() {
	defer func() {
		t.releaseLoops()
		t.releaseHubs()
		if err := stopTicker(t.ticker); err != nil {
			t.clockFailed(err)
		}
		t.mu.Lock()
		var cancel context.CancelFunc
		if t.closeContext != nil {
			if t.closeResult == nil {
				t.closeResult = t.closeContextError()
			}
			cancel = t.closeCancel
		}
		t.active.Store(false)
		close(t.done)
		t.mu.Unlock()
		if cancel != nil {
			cancel()
		}
	}()
	for {
		select {
		case stamp, ok := <-t.ticks:
			if !ok {
				t.clockFailed(invalid("clock", "ticker_closed"))
				return
			}
			if t.Enabled() {
				scheduled, err := stampTicker(t.ticker, stamp)
				if err != nil {
					t.clockFailed(err)
					return
				}
				// Stamp is the scheduled deadline. Collection and expiry use
				// the current coordinates even after a delayed or missed tick.
				now, err := readClock(t.opts.Clock)
				if err != nil {
					t.clockFailed(err)
					return
				}
				if scheduled.Monotonic > now.Monotonic {
					t.clockFailed(invalid("clock", "future_tick"))
					return
				}
				t.updateCore(now)
			}
		case <-t.wake:
			t.mu.Lock()
			ctx := t.closeContext
			t.mu.Unlock()
			if ctx != nil {
				t.mu.Lock()
				if t.closeResult == nil {
					t.closeResult = t.closeContextError()
				}
				t.mu.Unlock()
				return
			}
		}
	}
}

// A parent's Done can close before cancellation reaches this child. Capture
// the source error and an elapsed deadline while holding the owner lock.
func (t *Telemetry) closeContextError() error {
	ctx := t.closeContext
	if err := ctx.Err(); err != nil {
		return err
	}
	if deadline, ok := ctx.Deadline(); ok && !time.Now().Before(deadline) {
		return context.DeadlineExceeded
	}
	return nil
}

func (t *Telemetry) clockFailed(err error) {
	t.mu.Lock()
	if t.closeResult == nil {
		t.closeResult = err
	}
	t.active.Store(false)
	t.mu.Unlock()
	t.faultOnce.Do(func() {
		t.core.dropped["clock"].Add(1)
		logger := t.opts.Logger
		if logger == nil {
			logger = slog.Default()
		}
		func() {
			defer func() { _ = recover() }()
			logger.Error("telemetry clock failed", "class", "callback_failed")
		}()
	})
}

func stopTicker(t Ticker) (err error) {
	defer func() {
		if recover() != nil {
			err = invalid("clock", "callback_panic")
		}
	}()
	t.Stop()
	return nil
}

func readClock(c Clock) (now Instant, err error) {
	defer func() {
		if recover() != nil {
			err = invalid("clock", "callback_panic")
		}
	}()
	now = c.Now()
	if now.Monotonic < 0 {
		return now, invalid("clock", "negative_elapsed")
	}
	now.Wall = now.Wall.UTC()
	return now, nil
}

func stampTicker(t Ticker, stamp time.Time) (now Instant, err error) {
	defer func() {
		if recover() != nil {
			err = invalid("clock", "callback_panic")
		}
	}()
	now = t.Stamp(stamp)
	if now.Monotonic < 0 {
		return now, invalid("clock", "negative_elapsed")
	}
	now.Wall = now.Wall.UTC()
	return now, nil
}

// Signal closes admission. Existing hub callbacks stay subscribed through
// source drain. Only Flush/Close starts the shared deadline and source cleanup;
// a collector wake during source drain must not close the worker.
func (t *Telemetry) prepareShutdown(_ context.Context) {
	if t == nil || t.done == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	select {
	case <-t.done:
		return
	default:
	}
	t.active.Store(false)
}

func (t *Telemetry) signal(ctx context.Context) {
	t.prepareShutdown(ctx)
	if t == nil || t.done == nil {
		return
	}
	t.mu.Lock()
	select {
	case <-t.done:
		t.mu.Unlock()
		return
	default:
	}
	if t.closeContext == nil {
		if ctx == nil {
			ctx = context.Background()
		}
		deadline := time.Now().Add(20 * time.Second)
		if callerDeadline, ok := ctx.Deadline(); ok && callerDeadline.Before(deadline) {
			deadline = callerDeadline
		}
		t.closeContext, t.closeCancel = context.WithDeadline(context.WithoutCancel(ctx), deadline)
	}
	t.mu.Unlock()
	select {
	case t.wake <- struct{}{}:
	default:
	}
}

// Close shares the one worker completion. Every caller keeps its own deadline;
// the first caller bounds shared work to its deadline or 20 seconds. Cancelling
// that caller stops only its wait, not the shared work or another caller's wait.
func (t *Telemetry) Close(ctx context.Context) error {
	if t == nil || t.done == nil {
		return nil
	}
	if ctx == nil {
		return invalid("context", "required")
	}
	t.signal(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	t.mu.Lock()
	shared := t.closeContext
	t.mu.Unlock()
	var sharedDone <-chan struct{}
	if shared != nil {
		sharedDone = shared.Done()
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.done:
		if err := ctx.Err(); err != nil {
			return err
		}
		t.mu.Lock()
		defer t.mu.Unlock()
		return t.closeResult
	case <-sharedDone:
		if err := ctx.Err(); err != nil {
			return err
		}
		// Completion is published before canceling the shared timer, so a
		// successful close never becomes an artificial cancellation error.
		select {
		case <-t.done:
			t.mu.Lock()
			defer t.mu.Unlock()
			return t.closeResult
		default:
			return shared.Err()
		}
	}
}

// Flush is a no-op barrier for aggregate-only state. The record worker adds
// ordered persistence barriers when records and sinks become available.
func (t *Telemetry) Flush(ctx context.Context) error {
	if t == nil || t.done == nil {
		return nil
	}
	if ctx == nil {
		return invalid("context", "required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !t.Enabled() {
		return ErrClosed
	}
	return nil
}
