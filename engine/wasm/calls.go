package wasm

import (
	"encoding/json"
	"time"

	"m31labs.dev/gosx/engine/host"
)

// CallOptions tunes ServeCalls. See host.CallOptions.
type CallOptions = host.CallOptions

const (
	// DefaultCallTimeout is the page-side deadline for one call.
	DefaultCallTimeout = host.DefaultCallTimeout
	// DefaultCallRecent is the default length of Result.Recent.
	DefaultCallRecent = host.DefaultCallRecent
	// DefaultCallRecentBytes is the default byte budget for the values in
	// Result.Recent.
	DefaultCallRecentBytes = host.DefaultCallRecentBytes
)

// callTransport is what ServeCalls needs from the browser. Context implements
// it on js/wasm; tests use a fake.
type callTransport interface {
	// subscribe also delivers the signal's current value once, so a call
	// written before the subscription is not lost.
	subscribe(signal string, handler func(json.RawMessage)) (HandleFunc, error)
	// write sets a shared signal. It reaches the writer's own subscriptions to
	// the signal, as it reaches any other subscriber's: ServeCallsAsync starts a
	// handler only after the page has told it so about the mark of the call. An
	// error means the page took no part of the write.
	write(signal string, value any) error
	// declare tells the page half about the channel so its watchdog can fail a
	// call that the worker never answers. It is a no-op on the main thread.
	declare(channel string, timeout time.Duration, recent int, zeroResult string)
}

// ServeCalls answers typed requests from islands and other page code.
//
// The page writes an engine.Call[A] to the shared signal "$channel" and the
// engine replies with an engine.Result[R] on "$channel.result". The request,
// mailbox, reply, timeout and crash-error rules are those of host.ServeCalls,
// which this wraps for the engine context. On a worker engine the page also
// fails a call with "timeout" when no reply arrives in CallOptions.Timeout, and
// with "worker-crashed" if the worker dies. handle runs on the engine's event
// loop, one call at a time, and must not wait for a browser promise: use
// ServeCallsAsync for a handler that blocks.
func ServeCalls[A, R any](ctx Context, channel string, opts CallOptions, handle func(name string, args A) (R, error)) (HandleFunc, error) {
	return serveCalls(contextTransport{ctx: ctx}, channel, opts, handle)
}

// ServeCallsAsync is ServeCalls for a handler that may block, for example on a
// browser promise (jsutil.AwaitPromise), storage or the network. The handler
// runs on a goroutine, not inside the JavaScript callback that delivers the
// call, so the event loop stays free for what it waits on. Calls run one at a
// time and their results are written in ID order. On the main thread the engine
// has the browser write each result and each mark, and trim the mailbox, from the
// event loop, so that the goroutine never starts a nested entry from JavaScript
// into Go. A handler that publishes state should write it with
// Context.SetSignalsFromEventLoop, for the same reason.
// See host.ServeCallsAsync for the timeout, disposal and re-entry rules.
func ServeCallsAsync[A, R any](ctx Context, channel string, opts CallOptions, handle func(name string, args A) (R, error)) (HandleFunc, error) {
	return serveCallsAsync(contextTransport{ctx: ctx, async: true}, channel, opts, handle)
}

func serveCalls[A, R any](tr callTransport, channel string, opts CallOptions, handle func(name string, args A) (R, error)) (HandleFunc, error) {
	return host.ServeCallsOn(callAdapter{tr}, channel, opts, handle)
}

func serveCallsAsync[A, R any](tr callTransport, channel string, opts CallOptions, handle func(name string, args A) (R, error)) (HandleFunc, error) {
	return host.ServeCallsAsyncOn(callAdapter{tr}, channel, opts, handle)
}

// callAdapter presents a callTransport as a host.CallTransport.
type callAdapter struct{ t callTransport }

func (a callAdapter) Subscribe(signal string, handler func(json.RawMessage)) (HandleFunc, error) {
	return a.t.subscribe(signal, handler)
}
func (a callAdapter) Write(signal string, value any) error { return a.t.write(signal, value) }
func (a callAdapter) Declare(channel string, timeout time.Duration, recent int, zero string) {
	a.t.declare(channel, timeout, recent, zero)
}

// TrimCalls implements host.CallTrimmer for a transport that can trim.
func (a callAdapter) TrimCalls(signal string, through uint64) error {
	if t, ok := a.t.(callTrimmer); ok {
		return t.trimCalls(signal, through)
	}
	return nil
}

// callTrimmer is the optional trim half of a callTransport.
type callTrimmer interface {
	trimCalls(signal string, through uint64) error
}
