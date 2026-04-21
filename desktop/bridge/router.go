package bridge

import (
	"encoding/json"
	"fmt"
	"runtime/debug"
	"sync"
	"time"
)

// Sender transports an encoded envelope to the page. For the Windows
// backend this is wired to coreWebView2.postWebMessageAsString.
type Sender func(raw string) error

// MethodFunc runs a single registered method. Implementations read the
// request payload via Context.Decode, then call exactly one of Respond,
// End, or Error — or stream with repeated Stream + End. Returning an
// error is equivalent to calling Error with CodeInternal.
type MethodFunc func(ctx *Context) error

// Context is the per-request handle passed to a method handler.
type Context struct {
	// Method is the registered name the page called.
	Method string
	// ID is the correlation ID of this exchange. Use it in logs; the
	// Context methods reference it implicitly.
	ID string

	rawPayload json.RawMessage
	send       Sender

	// mu guards the terminal-state flags. Every method that would
	// finalize or extend the exchange reads + updates these atomically
	// so handlers can't accidentally double-respond or mix Respond
	// with Stream.
	mu        sync.Mutex
	responded bool // Respond, End, or Error delivered
	streaming bool // at least one Stream frame sent
}

// Decode unmarshals the request payload into dst. A nil payload leaves
// dst unchanged and returns nil.
func (c *Context) Decode(dst any) error {
	if len(c.rawPayload) == 0 {
		return nil
	}
	if err := json.Unmarshal(c.rawPayload, dst); err != nil {
		return fmt.Errorf("bridge: decode payload: %w", err)
	}
	return nil
}

// RawPayload returns the request payload as received, so handlers that
// want to re-forward it (proxy-style) can skip a round-trip through
// typed structs.
func (c *Context) RawPayload() json.RawMessage {
	return c.rawPayload
}

// Respond delivers a single success result and terminates the exchange.
// A second terminal call (Respond, End, Error) is a silent no-op so
// handlers can't accidentally double-respond.
func (c *Context) Respond(payload any) error {
	c.mu.Lock()
	if c.responded {
		c.mu.Unlock()
		return nil
	}
	c.responded = true
	c.mu.Unlock()

	env, err := newResponseEnvelope(c.ID, payload)
	if err != nil {
		return c.sendEnvelope(newErrorEnvelope(c.ID, &Error{
			Code:    CodeInternal,
			Message: "response payload could not be encoded",
			Detail:  err.Error(),
		}))
	}
	return c.sendEnvelope(env)
}

// Stream emits one partial-result frame under the current correlation
// ID. Handlers call Stream one or more times, then End. Stream after
// Respond or End is a silent no-op.
func (c *Context) Stream(frame any) error {
	c.mu.Lock()
	if c.responded {
		c.mu.Unlock()
		return nil
	}
	c.streaming = true
	c.mu.Unlock()

	env, err := newStreamFrameEnvelope(c.ID, frame)
	if err != nil {
		c.forceError(&Error{
			Code:    CodeInternal,
			Message: "stream frame could not be encoded",
			Detail:  err.Error(),
		})
		return err
	}
	return c.sendEnvelope(env)
}

// End terminates a streaming exchange. No-op if no Stream frame was
// sent (the exchange terminates via Respond in that case) or if the
// exchange already closed.
func (c *Context) End() error {
	c.mu.Lock()
	if c.responded || !c.streaming {
		c.responded = true
		c.mu.Unlock()
		return nil
	}
	c.responded = true
	c.mu.Unlock()
	return c.sendEnvelope(newStreamEndEnvelope(c.ID))
}

// Error terminates the exchange with a typed bridge error. First call
// wins; subsequent calls are no-ops.
func (c *Context) Error(code, message string) error {
	return c.ErrorWithDetail(code, message, "")
}

// ErrorWithDetail is Error plus a free-form context string for the
// page-side handler to log.
func (c *Context) ErrorWithDetail(code, message, detail string) error {
	c.mu.Lock()
	if c.responded {
		c.mu.Unlock()
		return nil
	}
	c.responded = true
	c.mu.Unlock()
	return c.sendEnvelope(newErrorEnvelope(c.ID, &Error{
		Code:    code,
		Message: message,
		Detail:  detail,
	}))
}

// forceError bypasses the responded-once check for bridge-internal
// encoding failures where we have no choice but to emit an error frame
// after already marking the exchange live.
func (c *Context) forceError(bridgeErr *Error) {
	c.mu.Lock()
	c.responded = true
	c.mu.Unlock()
	_ = c.sendEnvelope(newErrorEnvelope(c.ID, bridgeErr))
}

func (c *Context) sendEnvelope(env Envelope) error {
	raw, err := EncodeEnvelope(env)
	if err != nil {
		return err
	}
	return c.send(raw)
}

func (c *Context) isResolved() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.responded
}

// Router dispatches inbound envelopes to registered methods.
//
// A Router is safe for concurrent use; registrations and Dispatch calls
// can happen from any goroutine. The caller (webview integration) is
// responsible for providing a Sender that is safe to call from the
// WebView2 dispatcher thread — all Router traffic ultimately funnels
// through that single Sender call.
type Router struct {
	limit Limit

	mu       sync.RWMutex
	methods  map[string]MethodFunc
	limiter  *limiter
	sender   Sender
	handlers uint64
}

// NewRouter builds a Router bound to a Sender. The Limit controls the
// inbound rate + payload cap; pass a zero Limit for DefaultLimit.
func NewRouter(send Sender, limit Limit) *Router {
	r := &Router{
		methods: make(map[string]MethodFunc),
		sender:  send,
		limit:   limit.withDefaults(),
	}
	r.limiter = newLimiter(r.limit)
	return r
}

// Register binds fn to a method name. Calling Register twice for the
// same name replaces the prior binding. Panics if name is empty.
func (r *Router) Register(name string, fn MethodFunc) {
	if name == "" {
		panic("bridge: Register name must be non-empty")
	}
	r.mu.Lock()
	r.methods[name] = fn
	r.mu.Unlock()
}

// Unregister removes a previously registered method. No-op if not
// registered.
func (r *Router) Unregister(name string) {
	r.mu.Lock()
	delete(r.methods, name)
	r.mu.Unlock()
}

// Emit sends a host→page event with the given method name and payload.
// No correlation ID; fire-and-forget.
func (r *Router) Emit(method string, payload any) error {
	env, err := newEventEnvelope(method, payload)
	if err != nil {
		return err
	}
	raw, err := EncodeEnvelope(env)
	if err != nil {
		return err
	}
	return r.sender(raw)
}

// Dispatch processes one inbound raw message. The message may be any
// envelope op but typically is a request — responses / errors travel
// host→page, not the reverse. Dispatch is safe to call from any
// goroutine; handlers execute synchronously on the caller's goroutine.
//
// Returns nil on successful dispatch (including handler-level errors
// that were surfaced as OpError to the page). Returns a non-nil error
// only when the bridge itself failed — e.g. the Sender returned an
// error while trying to deliver a fault frame.
func (r *Router) Dispatch(raw string) error {
	if !r.limiter.allow() {
		return r.sendBridgeFault("", &Error{
			Code:    CodeRateLimited,
			Message: "inbound message rate exceeded",
		})
	}
	if r.limit.MaxBytes > 0 && len(raw) > r.limit.MaxBytes {
		return r.sendBridgeFault("", &Error{
			Code:    CodeTooLarge,
			Message: fmt.Sprintf("inbound message exceeds %d bytes", r.limit.MaxBytes),
		})
	}

	env, err := ParseEnvelope(raw)
	if err != nil {
		var be *Error
		if ok := asBridgeError(err, &be); ok {
			return r.sendBridgeFault("", be)
		}
		return r.sendBridgeFault("", &Error{
			Code:    CodeDecode,
			Message: err.Error(),
		})
	}

	switch env.Op {
	case OpRequest:
		return r.dispatchRequest(env)
	case OpEvent:
		// Page-originated events are accepted but unhandled for now;
		// a future API can let apps subscribe to event-method names.
		return nil
	case OpResponse, OpError, OpStreamFrame, OpStreamEnd:
		// These ops are host→page only. Silently ignore — a bad actor
		// could forge them but they have nowhere to land on the host.
		return nil
	default:
		return r.sendBridgeFault(env.ID, &Error{
			Code:    CodeInvalidOp,
			Message: fmt.Sprintf("op %q is not accepted from the page", env.Op),
		})
	}
}

func (r *Router) dispatchRequest(env Envelope) error {
	if env.ID == "" {
		return r.sendBridgeFault("", &Error{
			Code:    CodeDecode,
			Message: "request missing id",
		})
	}
	if env.Method == "" {
		return r.sendBridgeFault(env.ID, &Error{
			Code:    CodeDecode,
			Message: "request missing method",
		})
	}

	r.mu.RLock()
	fn, ok := r.methods[env.Method]
	r.mu.RUnlock()
	if !ok {
		return r.sendBridgeFault(env.ID, &Error{
			Code:    CodeNotFound,
			Message: fmt.Sprintf("method %q is not registered", env.Method),
		})
	}

	ctx := &Context{
		Method:     env.Method,
		ID:         env.ID,
		rawPayload: env.Payload,
		send:       r.sender,
	}

	err := runHandler(fn, ctx)
	if err != nil {
		return ctx.ErrorWithDetail(CodeInternal, "handler returned error", err.Error())
	}
	// Handler returned without resolving — treat as void success.
	if !ctx.isResolved() {
		return ctx.Respond(nil)
	}
	return nil
}

// runHandler wraps the method call in a panic recovery so a buggy
// handler surfaces as an OpError instead of crashing the dispatcher
// thread.
func runHandler(fn MethodFunc, ctx *Context) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("panic: %v\n%s", rec, debug.Stack())
		}
	}()
	return fn(ctx)
}

func (r *Router) sendBridgeFault(id string, bridgeErr *Error) error {
	env := newErrorEnvelope(id, bridgeErr)
	raw, err := EncodeEnvelope(env)
	if err != nil {
		return err
	}
	return r.sender(raw)
}

// asBridgeError mirrors errors.As for *Error without importing errors
// into the already heavy envelope file.
func asBridgeError(err error, target **Error) bool {
	if err == nil {
		return false
	}
	type iunwrap interface{ Unwrap() error }
	for cur := err; cur != nil; {
		if be, ok := cur.(*Error); ok {
			*target = be
			return true
		}
		u, ok := cur.(iunwrap)
		if !ok {
			break
		}
		cur = u.Unwrap()
	}
	return false
}

// limiter is a simple token-bucket rate limiter. Zero Rate disables it.
type limiter struct {
	mu     sync.Mutex
	rate   float64 // tokens per second
	burst  float64
	tokens float64
	last   time.Time
}

func newLimiter(l Limit) *limiter {
	return &limiter{
		rate:   l.Rate,
		burst:  float64(l.Burst),
		tokens: float64(l.Burst),
		last:   time.Now(),
	}
}

// allow attempts to take one token. Returns false when the bucket is
// empty; true (always) when rate limiting is disabled.
func (l *limiter) allow() bool {
	if l.rate <= 0 {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	elapsed := now.Sub(l.last).Seconds()
	l.last = now
	l.tokens += elapsed * l.rate
	if l.tokens > l.burst {
		l.tokens = l.burst
	}
	if l.tokens < 1 {
		return false
	}
	l.tokens--
	return true
}
