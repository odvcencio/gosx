package wasm

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"m31labs.dev/gosx/engine"
)

// CallOptions tunes ServeCalls.
type CallOptions struct {
	// Timeout is how long the page waits for a reply before it writes the
	// error "timeout" to the result signal. Zero means DefaultCallTimeout.
	// The page enforces it, so it still fires if the handler never returns.
	Timeout time.Duration
	// Recent is how many past replies Result.Recent keeps. Zero means
	// DefaultCallRecent.
	Recent int
}

const (
	// DefaultCallTimeout is the page-side deadline for one call.
	DefaultCallTimeout = 10 * time.Second
	// DefaultCallRecent is the default length of Result.Recent.
	DefaultCallRecent = 8
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

// callSignals returns the wire names of the call and result signals. A leading
// "$" on channel is optional.
func callSignals(channel string) (call, result string, err error) {
	name := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(channel), "$"))
	if name == "" {
		return "", "", errors.New("call channel is required")
	}
	return "$" + name, "$" + name + ".result", nil
}

// ServeCalls answers typed requests from islands and other page code.
//
// The page writes an engine.Call[A] to the shared signal "$channel" and the
// engine replies with an engine.Result[R] on "$channel.result". For each call
// with an ID higher than any it has handled, ServeCalls decodes the arguments,
// runs handle, and writes one result. A call whose arguments do not decode gets
// an error result instead of silence. A call that arrives before ServeCalls
// subscribes is handled from the signal's current value, unless the result
// signal shows it was already answered (an engine that remounts does not run an
// old call again). Repeated deliveries of
// the same value, which a batch flush can produce, are ignored.
//
// Result.Recent lists the last CallOptions.Recent replies (ID and error), so an
// island that sent two calls before the first reply still finds both. On a
// worker engine the page also fails a call with "timeout" when no reply arrives
// in CallOptions.Timeout, and with "worker-crashed" if the worker dies.
//
// handle runs on the engine's event loop, one call at a time.
func ServeCalls[A, R any](ctx Context, channel string, opts CallOptions, handle func(name string, args A) (R, error)) (HandleFunc, error) {
	return serveCalls(contextTransport{ctx}, channel, opts, handle)
}

func serveCalls[A, R any](tr callTransport, channel string, opts CallOptions, handle func(name string, args A) (R, error)) (HandleFunc, error) {
	if handle == nil {
		return nil, errors.New("call handler is required")
	}
	callSignal, resultSignal, err := callSignals(channel)
	if err != nil {
		return nil, err
	}
	if opts.Timeout <= 0 {
		opts.Timeout = DefaultCallTimeout
	}
	if opts.Recent <= 0 {
		opts.Recent = DefaultCallRecent
	}

	var (
		mu       sync.Mutex
		last     uint64
		recent   []engine.ResultRef
		disposed bool
	)
	process := func(data json.RawMessage) {
		// The ID decodes on its own, so a call with a wrongly typed Name or
		// Args still gets an answer.
		var head struct{ ID uint64 }
		if json.Unmarshal(data, &head) != nil {
			return // no ID to answer
		}
		mu.Lock()
		if disposed || head.ID == 0 || head.ID <= last {
			mu.Unlock()
			return
		}
		last = head.ID
		mu.Unlock()

		result := engine.Result[R]{ID: head.ID}
		var args A
		var call engine.Call[json.RawMessage]
		if err := safeDecode(data, &call); err != nil {
			result.Error = "bad-call: " + err.Error()
		} else {
			result.Name = call.Name
			if len(call.Args) > 0 && string(call.Args) != "null" {
				if err := safeDecode(call.Args, &args); err != nil {
					result.Error = "bad-args: " + err.Error()
				}
			}
		}
		if result.Error == "" {
			value, err := safeHandle(handle, call.Name, args)
			if err != nil {
				result.Error = err.Error()
			} else {
				result.Value = value
			}
		}

		mu.Lock()
		recent = append(recent, engine.ResultRef{ID: result.ID, Error: result.Error})
		if len(recent) > opts.Recent {
			recent = append([]engine.ResultRef(nil), recent[len(recent)-opts.Recent:]...)
		}
		result.Recent = append([]engine.ResultRef(nil), recent...)
		gone := disposed
		mu.Unlock()
		if gone {
			return
		}
		// A result value that cannot be encoded is reported as an error result.
		if err := tr.write(resultSignal, result); err != nil {
			fallback := engine.Result[R]{ID: result.ID, Name: result.Name, Error: "reply-failed: " + err.Error(), Recent: result.Recent}
			_ = tr.write(resultSignal, fallback)
		}
	}

	var zero string
	if encoded, err := json.Marshal(engine.Result[R]{}); err == nil {
		zero = string(encoded)
	}
	// Shared signals outlive an engine instance, so a remounted engine finds
	// the previous call and its result. Read the result first and treat every
	// call up to its ID as answered. The result subscription comes before the
	// call subscription so that on a worker, where both current values arrive
	// as messages, the result is seen first.
	offResult, err := tr.subscribe(resultSignal, func(data json.RawMessage) {
		var answered struct{ ID uint64 }
		if json.Unmarshal(data, &answered) != nil {
			return
		}
		mu.Lock()
		if answered.ID > last {
			last = answered.ID
		}
		mu.Unlock()
	})
	if err != nil {
		return nil, fmt.Errorf("serve calls on %s: %w", resultSignal, err)
	}
	off, err := tr.subscribe(callSignal, process)
	if err != nil {
		offResult.Dispose()
		return nil, fmt.Errorf("serve calls on %s: %w", callSignal, err)
	}
	tr.declare(callSignal, opts.Timeout, opts.Recent, zero)
	return func() {
		mu.Lock()
		disposed = true
		mu.Unlock()
		off.Dispose()
		offResult.Dispose()
	}, nil
}

// safeDecode unmarshals data into v, turning a panic in a custom UnmarshalJSON
// into an error so a bad call cannot end the engine.
func safeDecode(data []byte, v any) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("decoder panic: %v", r)
		}
	}()
	return json.Unmarshal(data, v)
}

func safeHandle[A, R any](handle func(string, A) (R, error), name string, args A) (value R, err error) {
	defer func() {
		if r := recover(); r != nil {
			var zero R
			value, err = zero, fmt.Errorf("handler panic: %v", r)
		}
	}()
	return handle(name, args)
}
