// Package enginehost runs a GoSX engine natively in a desktop host process and
// connects it to the page over desktop IPC (desktop/bridge).
//
// It is the desktop adapter of engine/host. A page that today gets its state
// and call results from a WASM engine worker (wasm.NewView, wasm.ServeCalls)
// gets the same shared signals and the same call results from a native engine
// registered here, with no application JavaScript: the GoSX page runtime
// (engine-worker feature) attaches to the host when the page runs inside a
// desktop window that registered one, and otherwise starts the worker as before.
//
// Register an engine by the component name the page's engine.Config uses:
//
//	h := enginehost.New(app.Bridge(), enginehost.Options{})
//	h.Register("Sim", func(in *enginehost.Instance) (host.Handle, error) {
//	    view := host.NewView(in, host.ViewOptions{})
//	    calls, err := host.ServeCalls(in, "sim.cmd", host.CallOptions{}, handle)
//	    ...
//	})
//
// The factory runs when the page mounts the engine and its Handle is disposed
// when the page disposes it, replaces it (a reload attaches again) or the host
// closes. All messages for one instance run one at a time, in order, on the
// instance's own goroutine, never on the window thread, so a slow call handler
// does not freeze the window.
package enginehost

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"m31labs.dev/gosx/desktop/bridge"
	"m31labs.dev/gosx/engine/host"
)

// Protocol is the version of the wire protocol below. The page checks it.
const Protocol = 1

// Bridge methods and events. The page runtime speaks these; an app does not.
const (
	// MethodAttach is a page request {id, component, props, hidden}. The host
	// answers {v} when a factory is registered for component, and a bridge
	// error with CodeNotRegistered when not (the page then starts its worker).
	MethodAttach = "gosx.engine.attach"
	// MethodPost is a page request {id, msg}: one message in the engine worker
	// protocol (docs/engine-worker.md) addressed to the attached instance.
	MethodPost = "gosx.engine.post"
	// EventMessage is a host event {id, msg}: one worker-protocol message for
	// the page's engine record.
	EventMessage = "gosx.engine.msg"
)

// CodeNativeDisabled reports an attach rejected by a host's disable policy.
const CodeNativeDisabled = "engine.native_disabled"

// CodeNotRegistered is the bridge error code for an attach to a component
// with no registered factory.
const CodeNotRegistered = "engine.not_registered"

// Bridge is the part of bridge.Router that the host uses.
type Bridge interface {
	Register(name string, fn bridge.MethodFunc) error
	Unregister(name string)
	Emit(method string, payload any) error
}

// Factory creates one engine instance when the page mounts it.
type Factory func(in *Instance) (host.Handle, error)

// Options tunes a Host.
type Options struct {
	// DisableNative forces the WASM worker for these exact, case-sensitive
	// component names. "*" disables every component. Entries are trimmed;
	// empty entries are ignored. New copies the slice and fixes the policy
	// for the lifetime of the host.
	DisableNative []string
	// Log receives diagnostics (rejected messages, failed sends). Nil discards.
	Log func(format string, args ...any)
}

// Host serves native engines to the page behind a bridge.
type Host struct {
	b             Bridge
	disableNative map[string]struct{}
	opt           Options

	mu        sync.Mutex
	factories map[string]Factory
	sessions  map[string]*session
	closed    bool
}

// New registers the engine protocol on b and returns the host. It panics if b
// refuses the registration, which happens only for an empty method name.
func New(b Bridge, opt Options) *Host {
	opt.DisableNative = append([]string(nil), opt.DisableNative...)
	disabled := make(map[string]struct{}, len(opt.DisableNative))
	for _, component := range opt.DisableNative {
		if component = strings.TrimSpace(component); component != "" {
			disabled[component] = struct{}{}
		}
	}
	h := &Host{b: b, opt: opt, disableNative: disabled, factories: map[string]Factory{}, sessions: map[string]*session{}}
	if err := b.Register(MethodAttach, h.attach); err != nil {
		panic(err)
	}
	if err := b.Register(MethodPost, h.post); err != nil {
		panic(err)
	}
	return h
}

// Register binds factory to a component name, the engine.Config Name of the
// page's engine. A second registration for a name replaces the first for later
// attaches.
func (h *Host) Register(component string, factory Factory) error {
	component = strings.TrimSpace(component)
	if component == "" {
		return errors.New("enginehost: component name is required")
	}
	if factory == nil {
		return errors.New("enginehost: factory is required")
	}
	h.mu.Lock()
	h.factories[component] = factory
	h.mu.Unlock()
	return nil
}

// Close disposes every instance and stops answering the page. It waits for
// each Handle's Dispose to return.
func (h *Host) Close() {
	h.mu.Lock()
	h.closed = true
	live := make([]*session, 0, len(h.sessions))
	for _, s := range h.sessions {
		live = append(live, s)
	}
	h.sessions = map[string]*session{}
	h.mu.Unlock()
	h.b.Unregister(MethodAttach)
	h.b.Unregister(MethodPost)
	for _, s := range live {
		s.shutdown(false)
	}
	for _, s := range live {
		<-s.done
	}
}

func (h *Host) logf(format string, args ...any) {
	if h.opt.Log != nil {
		h.opt.Log(format, args...)
	}
}

type attachRequest struct {
	ID        string          `json:"id"`
	Component string          `json:"component"`
	Props     json.RawMessage `json:"props"`
	Hidden    bool            `json:"hidden"`
}

func (h *Host) attach(ctx *bridge.Context) error {
	var req attachRequest
	if err := ctx.Decode(&req); err != nil {
		return ctx.ErrorWithDetail(bridge.CodeDecode, "attach request is malformed", err.Error())
	}
	if req.ID == "" {
		return ctx.Error(bridge.CodeDecode, "attach request has no engine id")
	}
	_, allDisabled := h.disableNative["*"]
	_, componentDisabled := h.disableNative[req.Component]
	if allDisabled || componentDisabled {
		return ctx.Error(CodeNativeDisabled, fmt.Sprintf("native engine is disabled for %q", req.Component))
	}
	h.mu.Lock()
	factory := h.factories[req.Component]
	if h.closed || factory == nil {
		h.mu.Unlock()
		return ctx.Error(CodeNotRegistered, fmt.Sprintf("no native engine is registered for %q", req.Component))
	}
	old := h.sessions[req.ID]
	s := newSession(h, req, factory, old)
	h.sessions[req.ID] = s
	h.mu.Unlock()
	if old != nil {
		old.shutdown(false)
	}
	return ctx.Respond(map[string]any{"v": Protocol})
}

type postRequest struct {
	ID  string          `json:"id"`
	Msg json.RawMessage `json:"msg"`
}

func (h *Host) post(ctx *bridge.Context) error {
	var req postRequest
	if err := ctx.Decode(&req); err != nil {
		return ctx.ErrorWithDetail(bridge.CodeDecode, "post request is malformed", err.Error())
	}
	h.mu.Lock()
	s := h.sessions[req.ID]
	h.mu.Unlock()
	if s == nil {
		return ctx.Error(CodeNotRegistered, fmt.Sprintf("engine %q is not attached", req.ID))
	}
	var head struct {
		T string `json:"t"`
	}
	if json.Unmarshal(req.Msg, &head) != nil || head.T == "" {
		return ctx.Error(bridge.CodeDecode, "message has no type")
	}
	msg := append(json.RawMessage(nil), req.Msg...)
	s.enqueue(func() { s.handle(head.T, msg) })
	return nil
}

// drop forgets s if it is still the current session for its id.
func (h *Host) drop(s *session) {
	h.mu.Lock()
	if h.sessions[s.id] == s {
		delete(h.sessions, s.id)
	}
	h.mu.Unlock()
}

// ---------------------------------------------------------------------------

// Instance is one mounted engine. It implements host.Context, so
// host.NewView(in, ...) and host.ServeCalls(in, ...) work on it.
type Instance struct{ s *session }

// ID is the page's engine id (for example "gosx-engine-bv").
func (in *Instance) ID() string { return in.s.id }

// Component is the registered component name.
func (in *Instance) Component() string { return in.s.component }

// DecodeProps decodes the engine props the page's engine.Config carries.
func (in *Instance) DecodeProps(v any) error {
	if len(in.s.props) == 0 || string(in.s.props) == "null" {
		return nil
	}
	return json.Unmarshal(in.s.props, v)
}

// ViewSink implements host.Context.
func (in *Instance) ViewSink() host.ViewSink { return viewSink{in.s} }

// CallTransport implements host.Context.
func (in *Instance) CallTransport() host.CallTransport { return callTransport{in.s} }

// Fail reports that the engine has crashed. The page sets the engine's status
// to "crashed" and fails every pending and later call with "worker-crashed",
// as it does for a worker that dies. The instance stays attached until the
// page disposes it.
func (in *Instance) Fail(err error) {
	in.s.emit(map[string]any{"t": "fatal", "code": "engine-crashed", "message": errString(err)})
}

// Log writes a line to the page's console, as the worker does.
func (in *Instance) Log(level, text string) {
	in.s.emit(map[string]any{"t": "log", "level": level, "text": text})
}

func errString(err error) string {
	if err == nil {
		return "engine failed"
	}
	return err.Error()
}

// ---------------------------------------------------------------------------

type session struct {
	h         *Host
	id        string
	component string
	props     json.RawMessage
	factory   Factory
	after     *session // the instance this one replaces; mount waits for it

	// The mailbox runs one message at a time, in order.
	qmu    sync.Mutex
	qcond  *sync.Cond
	queue  []func()
	closed bool
	done   chan struct{}

	// Owned by the mailbox goroutine except where noted.
	engine  host.Handle
	mounted bool

	mu       sync.Mutex // guards the fields below; held briefly, never across a callback
	hidden   bool
	visFns   map[int]func()
	nextVis  int
	acks     map[uint64]func(bool)
	nextSeq  uint64
	subs     map[uint64]func(json.RawMessage)
	nextSid  uint64
	dead     bool
	shutOnce sync.Once
}

func newSession(h *Host, req attachRequest, f Factory, after *session) *session {
	s := &session{
		h: h, id: req.ID, component: req.Component, props: req.Props, factory: f, after: after,
		done: make(chan struct{}), hidden: req.Hidden,
		visFns: map[int]func(){}, acks: map[uint64]func(bool){}, subs: map[uint64]func(json.RawMessage){},
	}
	s.qcond = sync.NewCond(&s.qmu)
	go s.run()
	return s
}

func (s *session) enqueue(fn func()) {
	s.qmu.Lock()
	if s.closed {
		s.qmu.Unlock()
		return
	}
	s.queue = append(s.queue, fn)
	s.qmu.Unlock()
	s.qcond.Signal()
}

func (s *session) run() {
	defer close(s.done)
	for {
		s.qmu.Lock()
		for len(s.queue) == 0 && !s.closed {
			s.qcond.Wait()
		}
		if len(s.queue) == 0 {
			s.qmu.Unlock()
			return
		}
		fn := s.queue[0]
		s.queue[0] = nil
		s.queue = s.queue[1:]
		s.qmu.Unlock()
		fn()
	}
}

// shutdown disposes the engine through the mailbox, so it runs after every
// message already queued, then stops the mailbox. notify says whether to tell
// the page ("disposed"); a replaced or closed instance does not.
func (s *session) shutdown(notify bool) {
	s.shutOnce.Do(func() {
		s.enqueue(func() {
			s.disposeHandle()
			s.mu.Lock()
			s.dead = true
			s.acks = map[uint64]func(bool){}
			s.subs = map[uint64]func(json.RawMessage){}
			s.mu.Unlock()
			s.h.drop(s)
			if notify {
				s.rawEmit(map[string]any{"t": "disposed"})
			}
			s.qmu.Lock()
			s.closed = true
			s.qmu.Unlock()
			s.qcond.Signal()
		})
	})
}

func (s *session) disposeHandle() {
	if s.engine == nil {
		return
	}
	h := s.engine
	s.engine = nil
	func() {
		defer func() {
			if r := recover(); r != nil {
				s.h.logf("enginehost: %s: dispose panic: %v", s.id, r)
			}
		}()
		h.Dispose()
	}()
}

// handle runs one page message.
func (s *session) handle(t string, raw json.RawMessage) {
	switch t {
	case "mount":
		s.mount()
	case "dispose":
		s.shutdown(true)
	case "visibility":
		var m struct {
			Hidden bool `json:"hidden"`
		}
		if json.Unmarshal(raw, &m) != nil {
			return
		}
		s.mu.Lock()
		changed := s.hidden != m.Hidden
		s.hidden = m.Hidden
		fns := make([]func(), 0, len(s.visFns))
		for _, id := range slices.Sorted(maps.Keys(s.visFns)) {
			fns = append(fns, s.visFns[id])
		}
		s.mu.Unlock()
		if changed {
			for _, fn := range fns {
				fn()
			}
		}
	case "ack":
		var m struct {
			Seq uint64 `json:"seq"`
			OK  *bool  `json:"ok"`
		}
		if json.Unmarshal(raw, &m) != nil {
			return
		}
		s.mu.Lock()
		done := s.acks[m.Seq]
		delete(s.acks, m.Seq)
		s.mu.Unlock()
		if done != nil {
			done(m.OK == nil || *m.OK)
		}
	case "signal":
		var m struct {
			Sid  uint64          `json:"sid"`
			JSON json.RawMessage `json:"json"`
		}
		if json.Unmarshal(raw, &m) != nil {
			return
		}
		// The page sends the value as a JSON string (the worker protocol) .
		var text string
		data := m.JSON
		if json.Unmarshal(m.JSON, &text) == nil {
			data = json.RawMessage(text)
		}
		s.mu.Lock()
		fn := s.subs[m.Sid]
		s.mu.Unlock()
		if fn != nil {
			fn(data)
		}
	case "fault":
		var m struct{ Where, Message string }
		_ = json.Unmarshal(raw, &m)
		s.h.logf("enginehost: %s: the page rejected %s: %s", s.id, m.Where, m.Message)
	default:
		// Input, app messages and the like are not part of the native engine
		// channel yet.
	}
}

func (s *session) mount() {
	if s.mounted {
		return
	}
	s.mounted = true
	if s.after != nil {
		<-s.after.done // the replaced instance finishes disposing first
		s.after = nil
	}
	var (
		handle host.Handle
		err    error
	)
	func() {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("engine factory panic: %v", r)
			}
		}()
		handle, err = s.factory(&Instance{s})
	}()
	if err != nil {
		s.rawEmit(map[string]any{"t": "fatal", "code": "engine-mount-failed", "message": err.Error()})
		return
	}
	s.engine = handle
	s.rawEmit(map[string]any{"t": "mounted"})
}

// emit sends a worker-protocol message to the page unless the instance has ended.
func (s *session) emit(msg map[string]any) { s.rawEmit(msg) }

func (s *session) rawEmit(msg map[string]any) {
	if err := s.send(msg); err != nil {
		s.h.logf("enginehost: %s: send %v: %v", s.id, msg["t"], err)
	}
}

func (s *session) send(msg map[string]any) error {
	return s.h.b.Emit(EventMessage, map[string]any{"id": s.id, "msg": msg})
}

// ---------------------------------------------------------------------------

type viewSink struct{ s *session }

func (v viewSink) Send(batch []byte, done func(bool)) error {
	s := v.s
	s.mu.Lock()
	if s.dead {
		s.mu.Unlock()
		return errors.New("engine instance has ended")
	}
	s.nextSeq++
	seq := s.nextSeq
	s.acks[seq] = done
	s.mu.Unlock()
	err := s.send(map[string]any{"t": "signals", "changedOnly": true, "text": string(batch), "seq": seq})
	if err != nil {
		s.mu.Lock()
		delete(s.acks, seq)
		s.mu.Unlock()
	}
	return err
}

func (v viewSink) Hidden() bool {
	v.s.mu.Lock()
	defer v.s.mu.Unlock()
	return v.s.hidden
}

func (v viewSink) OnVisibility(fn func()) host.HandleFunc {
	s := v.s
	s.mu.Lock()
	id := s.nextVis
	s.nextVis++
	s.visFns[id] = fn
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		delete(s.visFns, id)
		s.mu.Unlock()
	}
}

func (v viewSink) Release() {}

type callTransport struct{ s *session }

func (c callTransport) Subscribe(signal string, handler func(json.RawMessage)) (host.HandleFunc, error) {
	s := c.s
	s.mu.Lock()
	if s.dead {
		s.mu.Unlock()
		return nil, errors.New("engine instance has ended")
	}
	s.nextSid++
	sid := s.nextSid
	s.subs[sid] = handler
	s.mu.Unlock()
	if err := s.send(map[string]any{"t": "subscribe", "sid": sid, "name": signal}); err != nil {
		s.mu.Lock()
		delete(s.subs, sid)
		s.mu.Unlock()
		return nil, err
	}
	return func() {
		s.mu.Lock()
		_, live := s.subs[sid]
		delete(s.subs, sid)
		s.mu.Unlock()
		if live {
			_ = s.send(map[string]any{"t": "unsubscribe", "sid": sid})
		}
	}, nil
}

func (c callTransport) Write(signal string, value any) error {
	data, err := json.Marshal(map[string]any{signal: value})
	if err != nil {
		return err
	}
	return c.s.send(map[string]any{"t": "signals", "changedOnly": false, "text": string(data)})
}

func (c callTransport) Declare(channel string, timeout time.Duration, recent int, zero string) {
	_ = c.s.send(map[string]any{
		"t": "calls", "channel": channel, "timeout": int64((timeout + time.Millisecond - 1) / time.Millisecond),
		"recent": recent, "zero": zero,
	})
}
