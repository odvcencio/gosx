package host

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"m31labs.dev/gosx/engine"
)

// CallOptions tunes ServeCalls and ServeCallsAsync.
type CallOptions struct {
	// Timeout is how long the page waits for a reply before it writes the
	// error "timeout" to the result signal. Zero means DefaultCallTimeout.
	// The page enforces it, so it still fires if the handler never returns.
	Timeout time.Duration
	// Recent is how many past replies Result.Recent keeps. Zero means
	// DefaultCallRecent.
	Recent int
	// RecentBytes is how many bytes of reply values Result.Recent may carry
	// besides the newest reply's own Value. The newest entries keep their value
	// while they fit; an older entry that no longer fits keeps its ID and error
	// only. Zero means DefaultCallRecentBytes; a negative number keeps no values.
	RecentBytes int
}

const (
	// DefaultCallTimeout is the page-side deadline for one call.
	DefaultCallTimeout = 10 * time.Second
	// DefaultCallRecent is the default length of Result.Recent.
	DefaultCallRecent = 8
	// DefaultCallRecentBytes is the default byte budget for the values in
	// Result.Recent.
	DefaultCallRecentBytes = 8 << 10
)

// CallTransport is what ServeCalls needs from the page. Each transport
// supplies one: the js/wasm engine context, or the desktop IPC bridge.
type CallTransport interface {
	// Subscribe also delivers the signal's current value once, so a call
	// written before the subscription is not lost.
	Subscribe(signal string, handler func(json.RawMessage)) (HandleFunc, error)
	// Write sets a shared signal to value.
	Write(signal string, value any) error
	// Declare tells the page half about the channel so its watchdog can fail a
	// call that the engine never answers. It is a no-op where the page has no
	// watchdog (the main thread).
	Declare(channel string, timeout time.Duration, recent int, zeroResult string)
}

// CallTrimmer is an optional capability of a CallTransport that reads and
// writes the page's own signal store at once (an engine on the main thread). A
// mailbox in the call signal lists the calls that have no reply yet; after the
// engine answers call n, ServeCalls asks such a transport to drop the calls at
// or below n from it. It asks only on a channel that has held a mailbox. A
// transport whose view of the page is a delayed copy (a worker, a native host)
// does not implement it: the page host drops them itself.
type CallTrimmer interface {
	TrimCalls(callSignal string, through uint64) error
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
// engine replies with an engine.Result[R] on "$channel.result". The signal may
// also hold a mailbox, an array of calls, so that several calls written
// together (for example by one island handler) all arrive. ServeCalls runs
// every call whose ID is higher than any it has handled, in ID order, and
// writes one result for each. It decodes the arguments, runs handle and writes
// the result. A call whose arguments do not decode gets an error result instead
// of silence. A call that arrives before ServeCalls subscribes is handled from
// the signal's current value, unless the result signal shows it was already
// answered (an engine that remounts does not run an old call again). Repeated
// deliveries of the same value, which a batch flush can produce, are ignored.
//
// Result.Recent lists the last CallOptions.Recent replies (ID, error and,
// within CallOptions.RecentBytes, value), so an island that sent several calls
// before the first reply still finds each one. It also lists, with the error
// "overwritten", each call that a writer of single calls skipped just before the
// call that arrived: the signal keeps its newest value only, so the engine never
// received it. Result.Seen is the highest call ID the engine has accepted or seen
// in a result, the number a writer counts its next call from. On a worker engine
// the page also fails a call with "timeout" when no reply arrives in
// CallOptions.Timeout, and with "worker-crashed" if the worker dies.
//
// handle runs on the engine's event loop, one call at a time, and must not
// wait for a browser promise or any callback that only the event loop can run:
// the engine would deadlock. Use ServeCallsAsync for a handler that blocks.
func ServeCalls[A, R any](ctx Context, channel string, opts CallOptions, handle func(name string, args A) (R, error)) (HandleFunc, error) {
	return ServeCallsOn(ctx.CallTransport(), channel, opts, handle)
}

// ServeCallsOn is ServeCalls over an explicit transport.
func ServeCallsOn[A, R any](tr CallTransport, channel string, opts CallOptions, handle func(name string, args A) (R, error)) (HandleFunc, error) {
	return serveCalls(tr, channel, opts, handle, false)
}

// ServeCallsAsync is ServeCalls for a handler that may block, for example on a
// browser promise (jsutil.AwaitPromise), storage or the network.
//
// ServeCalls runs the handler inside the JavaScript callback that delivers the
// call. Go cannot run any other callback until that one returns, so a handler
// that waits for one deadlocks the engine. ServeCallsAsync only queues the call
// in that callback and returns at once. The handler runs on a goroutine of its
// own, so the event loop stays free for the promise it waits for.
//
// Calls run one at a time, in ID order, and each result is written before the
// next call starts. The result of call n therefore tells a client that every
// call up to n has finished. A handler that never returns holds back every
// later call on the channel; use one channel for each kind of work that must
// not wait for the other.
//
// The page's watchdog counts from when the page saw a call, not from when its
// handler starts, so a call that waits in the queue behind a slow one can time
// out. Its real reply replaces the timeout when it comes. A client that keeps
// one call in flight (wasm.Caller does by default) never queues calls here.
//
// A handler runs on its own goroutine, never inside a call from JavaScript into
// Go. The engine's own writes to the page, a result and the trim of a mailbox,
// are made so that they do not enter Go again either. A write to a shared signal
// runs the engine's own subscriptions, which are Go functions, before it
// returns; made from a goroutine on the main thread, that is a nested entry
// from JavaScript into Go, and two goroutines in nested entries at once can stop
// the program. So the main-thread transport (wasm.ServeCallsAsync) has the
// browser make the write from the event loop, where no call from Go is pending,
// and waits for it. A handler that calls into JavaScript should follow the same
// rule and avoid functions that run a Go callback before they return (new
// Promise with a Go executor, dispatchEvent to a Go listener):
// jsutil.AwaitPromise follows it.
//
// Disposing the returned HandleFunc drops the queued calls and the reply of the
// call that is running; it does not stop a running handler. A transport that
// revokes the engine's context before it calls Dispose (the browser does) cannot
// take a write then, so the server writes the ID of each call to
// "<channel>.started" before it runs the handler. A server that mounts later on
// the same signals answers a call with no result whose ID is at or below that
// mark with the error "interrupted" and does not run it again: its handler
// started, and may have done its work. Calls that never started still run.
func ServeCallsAsync[A, R any](ctx Context, channel string, opts CallOptions, handle func(name string, args A) (R, error)) (HandleFunc, error) {
	return ServeCallsAsyncOn(ctx.CallTransport(), channel, opts, handle)
}

// ServeCallsAsyncOn is ServeCallsAsync over an explicit transport.
func ServeCallsAsyncOn[A, R any](tr CallTransport, channel string, opts CallOptions, handle func(name string, args A) (R, error)) (HandleFunc, error) {
	return serveCalls(tr, channel, opts, handle, true)
}

func serveCalls[A, R any](tr CallTransport, channel string, opts CallOptions, handle func(name string, args A) (R, error), async bool) (HandleFunc, error) {
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
	if opts.RecentBytes == 0 {
		opts.RecentBytes = DefaultCallRecentBytes
	}
	if opts.RecentBytes < 0 {
		opts.RecentBytes = 0
	}

	s := &callServer[A, R]{
		tr: tr, handle: handle, async: async,
		callSignal: callSignal, resultSignal: resultSignal, startedSignal: callSignal + ".started",
		log: replyLog{keep: opts.Recent, budget: opts.RecentBytes},
	}
	s.trimmer, _ = tr.(CallTrimmer)
	var zero string
	if encoded, err := json.Marshal(engine.Result[R]{}); err == nil {
		zero = string(encoded)
	}
	if encoded, err := json.Marshal(*new(R)); err == nil {
		s.zero = encoded
	} else {
		s.zero = json.RawMessage("null")
	}

	// Shared signals outlive an engine instance, so a remounted engine finds
	// the previous call and its result. Read the result first and treat every
	// call up to its ID as answered. The result subscription comes before the
	// call subscription so that on a worker, where both current values arrive
	// as messages, the result is seen first.
	offResult, err := tr.Subscribe(resultSignal, func(data json.RawMessage) {
		var answered struct{ ID, Seen uint64 }
		if json.Unmarshal(data, &answered) != nil {
			return
		}
		s.mu.Lock()
		if answered.ID > s.last {
			s.last = answered.ID
		}
		// The page writes results too (a timeout), and a result may come from an
		// engine that ran before this one. Seen never goes below what they saw.
		s.seen = max(s.seen, s.last, answered.Seen)
		s.mu.Unlock()
	})
	if err != nil {
		return nil, fmt.Errorf("serve calls on %s: %w", resultSignal, err)
	}
	// The mark comes between the two, so that a replay sees the result, then the
	// mark, then the call.
	offStarted := HandleFunc(func() {})
	if async {
		offStarted, err = tr.Subscribe(s.startedSignal, func(data json.RawMessage) {
			var n uint64
			if json.Unmarshal(data, &n) != nil {
				return
			}
			s.mu.Lock()
			s.started = max(s.started, n)
			s.mu.Unlock()
		})
		if err != nil {
			offResult.Dispose()
			return nil, fmt.Errorf("serve calls on %s: %w", s.startedSignal, err)
		}
	}
	off, err := tr.Subscribe(callSignal, s.deliver)
	if err != nil {
		offStarted.Dispose()
		offResult.Dispose()
		return nil, fmt.Errorf("serve calls on %s: %w", callSignal, err)
	}
	tr.Declare(callSignal, opts.Timeout, opts.Recent, zero)
	return func() {
		s.mu.Lock()
		s.disposed = true
		s.queue = nil
		s.mu.Unlock()
		off.Dispose()
		offStarted.Dispose()
		offResult.Dispose()
	}, nil
}

// callReply is the outcome of one call.
type callReply struct {
	id    uint64
	name  string
	err   string
	lost  []uint64        // IDs a writer of single calls skipped just below id
	value json.RawMessage // the encoded Value to publish; the encoded zero value for an error
	kept  json.RawMessage // the value for Result.Recent; nil for an error
}

// callServer is the engine side of one call channel. It keeps the accepted
// calls in a queue and drains it one call at a time: ServeCalls on the stack
// of the callback that delivered the call, ServeCallsAsync on a goroutine.
//
// mu guards the fields below it. It is never held across a call into the
// handler or the transport, because the transport may call back into Go before
// it returns (a write to a shared signal notifies this engine's own
// subscriptions in the same call stack).
type callServer[A, R any] struct {
	tr            CallTransport
	trimmer       CallTrimmer
	handle        func(name string, args A) (R, error)
	async         bool
	callSignal    string
	resultSignal  string
	startedSignal string // the mark of the call whose handler started last (async only)
	zero          json.RawMessage

	mu       sync.Mutex
	last     uint64      // the highest ID accepted or reported as answered
	seen     uint64      // the highest ID accepted, answered or seen by the page: Result.Seen
	started  uint64      // the highest ID whose handler started, here or in an engine before this one
	queue    []callEntry // accepted calls that have not started, in ID order
	busy     bool        // a stack or goroutine is draining the queue
	mailbox  bool        // the call signal has held a mailbox
	disposed bool
	log      replyLog
}

// deliver takes the value of the call signal.
func (s *callServer[A, R]) deliver(data json.RawMessage) {
	entries, mailbox := decodeCalls(data)
	s.mu.Lock()
	if s.disposed {
		s.mu.Unlock()
		return
	}
	if mailbox {
		s.mailbox = true
	}
	for _, entry := range entries {
		if entry.id > s.last {
			if !mailbox {
				// A mailbox lists every call it has, so a gap in it is no loss.
				entry.lost = lostBefore(s.seen, entry.id, s.log.keep)
			}
			s.last = entry.id
			s.seen = max(s.seen, entry.id)
			// A call at or below the mark has no result, and its handler started: an
			// engine before this one began it and went away. This engine ran no call
			// above the mark yet, so no call it accepts now can be its own.
			entry.interrupted = entry.id <= s.started
			s.queue = append(s.queue, entry)
		}
	}
	start := len(s.queue) > 0 && !s.busy
	if start {
		s.busy = true
	}
	s.mu.Unlock()
	switch {
	case !start:
	case s.async:
		go s.drain()
	default:
		s.drain()
	}
}

// drain runs the queued calls, oldest first, and writes each result before it
// starts the next call. A call that arrives while drain runs (a handler or a
// result write that reaches this engine's own subscription) joins the queue and
// runs after the calls before it, so results stay in ID order.
func (s *callServer[A, R]) drain() {
	finished := false
	defer func() {
		if !finished { // a panic is leaving: let the next delivery drain again
			s.mu.Lock()
			s.busy = false
			s.mu.Unlock()
		}
	}()
	for {
		s.mu.Lock()
		if s.disposed || len(s.queue) == 0 {
			s.busy = false
			s.mu.Unlock()
			finished = true
			return
		}
		entry := s.queue[0]
		s.queue[0] = callEntry{}
		s.queue = s.queue[1:]
		s.mu.Unlock()

		if s.async && !entry.interrupted && !s.markStarted(entry.id) {
			s.mu.Lock()
			s.busy = false
			s.mu.Unlock()
			finished = true
			return
		}
		reply := s.run(entry)

		s.mu.Lock()
		if s.disposed {
			s.busy = false
			s.mu.Unlock()
			finished = true
			return
		}
		result := engine.Result[json.RawMessage]{
			ID: reply.id, Name: reply.name, Error: reply.err, Value: reply.value,
			Recent: s.log.add(reply.id, reply.err, reply.kept, reply.lost),
			Seen:   s.seen,
		}
		trim := s.mailbox && s.trimmer != nil
		s.mu.Unlock()

		s.write(result)
		if trim {
			_ = s.trimmer.TrimCalls(s.callSignal, reply.id)
		}
	}
}

// markStarted writes the ID of a call whose handler is about to run, to the
// started signal, and reports whether the handler may run: it may not once the
// server is disposed.
//
// The browser revokes an engine's context before it calls Dispose, so a server
// cannot write anything when it is disposed, and a handler that is still running
// loses its reply. An engine that mounts on the same shared signals then finds
// the call with no result. The mark is what it can tell by: it answers such a
// call "interrupted" instead of running it again. A write that fails is not
// retried, and the call runs unmarked.
func (s *callServer[A, R]) markStarted(id uint64) (run bool) {
	s.mu.Lock()
	s.started = max(s.started, id)
	s.mu.Unlock()
	_ = s.tr.Write(s.startedSignal, id)
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.disposed
}

// run decodes one call, runs the handler and encodes the reply.
func (s *callServer[A, R]) run(entry callEntry) callReply {
	reply := callReply{id: entry.id, lost: entry.lost}
	if entry.interrupted {
		// No handler runs: it started in an engine that is gone, and may have
		// finished. The result still names the call, when its name decodes.
		var head struct{ Name string }
		_ = safeDecode(entry.raw, &head)
		reply.name, reply.err, reply.value = head.Name, "interrupted", s.zero
		return reply
	}
	var args A
	var call engine.Call[json.RawMessage]
	if err := safeDecode(entry.raw, &call); err != nil {
		reply.err = "bad-call: " + err.Error()
	} else {
		reply.name = call.Name
		if len(call.Args) > 0 && string(call.Args) != "null" {
			if err := safeDecode(call.Args, &args); err != nil {
				reply.err = "bad-args: " + err.Error()
			}
		}
	}
	if reply.err == "" {
		value, err := safeHandle(s.handle, call.Name, args)
		if err != nil {
			reply.err = err.Error()
		} else if encoded, err := safeEncode(value); err != nil {
			// A result value that cannot be encoded is reported as an error result.
			reply.err = "reply-failed: " + err.Error()
		} else {
			reply.value, reply.kept = encoded, encoded
		}
	}
	if reply.value == nil {
		reply.value = s.zero
	}
	return reply
}

// write publishes one result. A result the page cannot take is replaced by an
// error result for the same call.
func (s *callServer[A, R]) write(result engine.Result[json.RawMessage]) {
	if err := s.tr.Write(s.resultSignal, result); err != nil {
		// A result the page cannot take is answered with an error result.
		fallback := engine.Result[json.RawMessage]{
			ID: result.ID, Name: result.Name, Error: "reply-failed: " + err.Error(),
			Value: s.zero, Recent: result.Recent, Seen: result.Seen,
		}
		_ = s.tr.Write(s.resultSignal, fallback)
	}
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

// safeEncode marshals v, turning a panic in a custom MarshalJSON into an error.
func safeEncode(v any) (data []byte, err error) {
	defer func() {
		if r := recover(); r != nil {
			data, err = nil, fmt.Errorf("encoder panic: %v", r)
		}
	}()
	return json.Marshal(v)
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
