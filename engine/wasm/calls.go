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
)

// callTransport is what ServeCalls needs from the browser. Context implements
// it on js/wasm; tests use a fake.
type callTransport interface {
	// subscribe also delivers the signal's current value once, so a call
	// written before the subscription is not lost.
	subscribe(signal string, handler func(json.RawMessage)) (HandleFunc, error)
	write(signal string, value any) error
	// declare tells the page half about the channel so its watchdog can fail a
	// call that the worker never answers. It is a no-op on the main thread.
	declare(channel string, timeout time.Duration, recent int, zeroResult string)
}

// ServeCalls answers typed requests from islands and other page code.
//
// The page writes an engine.Call[A] to the shared signal "$channel" and the
// engine replies with an engine.Result[R] on "$channel.result". The request,
// reply, timeout and crash-error rules are those of host.ServeCalls, which
// this wraps for the engine context. On a worker engine the page also fails a
// call with "timeout" when no reply arrives in CallOptions.Timeout, and with
// "worker-crashed" if the worker dies. handle runs on the engine's event loop,
// one call at a time.
func ServeCalls[A, R any](ctx Context, channel string, opts CallOptions, handle func(name string, args A) (R, error)) (HandleFunc, error) {
	return serveCalls(contextTransport{ctx}, channel, opts, handle)
}

func serveCalls[A, R any](tr callTransport, channel string, opts CallOptions, handle func(name string, args A) (R, error)) (HandleFunc, error) {
	return host.ServeCallsOn(callAdapter{tr}, channel, opts, handle)
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
