package enginehost

import (
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"m31labs.dev/gosx/desktop/bridge"
	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/engine/host"
)

// fakePage is the page end of the bridge: it records what the host sends and
// posts requests through a real bridge.Router.
type fakePage struct {
	t        *testing.T
	router   *bridge.Router
	mu       sync.Mutex
	envs     []bridge.Envelope
	seen     int
	nextID   int
	sessions map[string]string
}

func newFakePage(t *testing.T) *fakePage {
	p := &fakePage{t: t, sessions: map[string]string{}}
	p.router = bridge.NewRouter(func(raw string) error {
		env, err := bridge.ParseEnvelope(raw)
		if err != nil {
			t.Errorf("host sent a bad envelope: %v", err)
			return err
		}
		p.mu.Lock()
		p.envs = append(p.envs, env)
		p.mu.Unlock()
		return nil
	}, bridge.Limit{})
	return p
}

// request sends a page request and returns the terminal envelope.
func (p *fakePage) request(method string, payload any) bridge.Envelope {
	p.t.Helper()
	p.nextID++
	id := "r" + string(rune('0'+p.nextID%10)) + time.Now().Format("150405.000000000")
	raw, _ := json.Marshal(bridge.Envelope{Op: bridge.OpRequest, ID: id, Method: method, Payload: mustJSON(payload)})
	if err := p.router.Dispatch(string(raw)); err != nil {
		p.t.Fatalf("dispatch: %v", err)
	}
	var out bridge.Envelope
	waitFor(p.t, "response to "+method, func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		for _, e := range p.envs {
			if e.ID == id && (e.Op == bridge.OpResponse || e.Op == bridge.OpError) {
				out = e
				return true
			}
		}
		return false
	})
	return out
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func (p *fakePage) attach(id, component string, hidden bool) bridge.Envelope {
	e := p.request(MethodAttach, map[string]any{"id": id, "component": component, "props": map[string]any{"seed": 7}, "hidden": hidden})
	if e.Op == bridge.OpResponse {
		var answer struct{ Session string }
		if err := json.Unmarshal(e.Payload, &answer); err != nil {
			p.t.Fatal(err)
		}
		p.mu.Lock()
		p.sessions[id] = answer.Session
		p.mu.Unlock()
	}
	return e
}

func (p *fakePage) post(id string, msg map[string]any) {
	p.t.Helper()
	p.mu.Lock()
	session := p.sessions[id]
	p.mu.Unlock()
	if e := p.request(MethodPost, map[string]any{"id": id, "session": session, "msg": msg}); e.Op != bridge.OpResponse {
		p.t.Fatalf("post %v failed: %+v", msg["t"], e.Error)
	}
}

type hostMsg map[string]json.RawMessage

func (m hostMsg) t() string {
	var s string
	_ = json.Unmarshal(m["t"], &s)
	return s
}
func (m hostMsg) str(k string) string { var s string; _ = json.Unmarshal(m[k], &s); return s }
func (m hostMsg) num(k string) float64 {
	var n float64
	_ = json.Unmarshal(m[k], &n)
	return n
}

// messages returns the worker-protocol messages for id sent so far.
func (p *fakePage) messages(id string) []hostMsg {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []hostMsg
	for _, e := range p.envs {
		if e.Op != bridge.OpEvent || e.Method != EventMessage {
			continue
		}
		var ev struct {
			ID  string
			Msg hostMsg
		}
		if json.Unmarshal(e.Payload, &ev) == nil && ev.ID == id {
			out = append(out, ev.Msg)
		}
	}
	return out
}

func (p *fakePage) waitMsg(id, typ string, n int) []hostMsg {
	p.t.Helper()
	var got []hostMsg
	waitFor(p.t, typ+" message", func() bool {
		got = nil
		for _, m := range p.messages(id) {
			if m.t() == typ {
				got = append(got, m)
			}
		}
		return len(got) >= n
	})
	return got
}

func (p *fakePage) count(id, typ string) int {
	n := 0
	for _, m := range p.messages(id) {
		if m.t() == typ {
			n++
		}
	}
	return n
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func mountEngine(t *testing.T, p *fakePage, id string) {
	t.Helper()
	p.post(id, map[string]any{"t": "mount"})
	p.waitMsg(id, "mounted", 1)
}

type addArgs struct{ A, B int }
type sum struct{ Total int }

func newHost(t *testing.T, p *fakePage) *Host {
	h := New(p.router, Options{Log: func(f string, a ...any) { t.Logf(f, a...) }})
	t.Cleanup(h.Close)
	return h
}

func TestAttachToAnUnregisteredComponentIsRefused(t *testing.T) {
	p := newFakePage(t)
	newHost(t, p)
	env := p.attach("e1", "Nope", false)
	if env.Op != bridge.OpError || env.Error.Code != CodeNotRegistered {
		t.Fatalf("attach = %+v, want %s", env, CodeNotRegistered)
	}
}

func TestAttachAnswersProtocolAndMountRunsTheFactoryOnce(t *testing.T) {
	p := newFakePage(t)
	h := newHost(t, p)
	var mounts int
	var props struct{ Seed int }
	_ = h.Register("Sim", func(in *Instance) (host.Handle, error) {
		mounts++
		if in.ID() != "e1" || in.Component() != "Sim" {
			t.Errorf("instance = %q %q", in.ID(), in.Component())
		}
		if err := in.DecodeProps(&props); err != nil {
			t.Error(err)
		}
		return host.HandleFunc(func() {}), nil
	})
	env := p.attach("e1", "Sim", false)
	var answer struct {
		V       int
		Session string
	}
	if env.Op != bridge.OpResponse || json.Unmarshal(env.Payload, &answer) != nil || answer.V != Protocol || answer.Session == "" {
		t.Fatalf("attach = %+v %s", env, env.Payload)
	}
	mountEngine(t, p, "e1")
	p.post("e1", map[string]any{"t": "mount"}) // a second mount is ignored
	p.post("e1", map[string]any{"t": "visibility", "hidden": false})
	if mounts != 1 || props.Seed != 7 {
		t.Fatalf("mounts = %d, props = %+v", mounts, props)
	}
}

func TestFactoryFailureAndPanicBecomeFatalMessages(t *testing.T) {
	for name, factory := range map[string]Factory{
		"error": func(*Instance) (host.Handle, error) { return nil, errors.New("no save file") },
		"panic": func(*Instance) (host.Handle, error) { panic("boom") },
	} {
		t.Run(name, func(t *testing.T) {
			p := newFakePage(t)
			h := newHost(t, p)
			_ = h.Register("Sim", factory)
			p.attach("e1", "Sim", false)
			p.post("e1", map[string]any{"t": "mount"})
			fatal := p.waitMsg("e1", "fatal", 1)[0]
			if fatal.str("code") != "engine-mount-failed" || fatal.str("message") == "" {
				t.Fatalf("fatal = %v", fatal)
			}
			if p.count("e1", "mounted") != 0 {
				t.Fatal("a failed mount must not report mounted")
			}
		})
	}
}

func TestViewPacesBatchesAndWaitsForTheAcknowledgement(t *testing.T) {
	p := newFakePage(t)
	h := newHost(t, p)
	views := make(chan *host.View, 1)
	_ = h.Register("Sim", func(in *Instance) (host.Handle, error) {
		v := host.NewView(in, host.ViewOptions{MinInterval: time.Millisecond, AckTimeout: time.Minute})
		views <- v
		return v, nil
	})
	p.attach("e1", "Sim", false)
	mountEngine(t, p, "e1")
	v := <-views
	_ = v.Set(map[string]any{"$n": 1})
	first := p.waitMsg("e1", "signals", 1)[0]
	if first.str("text") != `{"$n":1}` || first.num("seq") != 1 || string(first["changedOnly"]) != "true" {
		t.Fatalf("first batch = %v", first)
	}
	_ = v.Set(map[string]any{"$n": 2})
	time.Sleep(30 * time.Millisecond)
	if got := p.count("e1", "signals"); got != 1 {
		t.Fatalf("a batch went out before the acknowledgement: %d", got)
	}
	p.post("e1", map[string]any{"t": "ack", "seq": 1, "ok": true})
	second := p.waitMsg("e1", "signals", 2)[1]
	if second.str("text") != `{"$n":2}` || second.num("seq") != 2 {
		t.Fatalf("second batch = %v", second)
	}
	// A rejected batch is sent again.
	p.post("e1", map[string]any{"t": "ack", "seq": 2, "ok": false})
	third := p.waitMsg("e1", "signals", 3)[2]
	if third.str("text") != `{"$n":2}` {
		t.Fatalf("retry = %v", third)
	}
}

func TestViewHoldsWhileHiddenAndSendsTheNewestWhenShown(t *testing.T) {
	p := newFakePage(t)
	h := newHost(t, p)
	views := make(chan *host.View, 1)
	_ = h.Register("Sim", func(in *Instance) (host.Handle, error) {
		v := host.NewView(in, host.ViewOptions{MinInterval: time.Millisecond})
		views <- v
		return v, nil
	})
	p.attach("e1", "Sim", true) // attached while hidden
	mountEngine(t, p, "e1")
	v := <-views
	_ = v.Set(map[string]any{"$n": 1})
	_ = v.Set(map[string]any{"$n": 2})
	time.Sleep(30 * time.Millisecond)
	if got := p.count("e1", "signals"); got != 0 {
		t.Fatalf("a hidden page got %d batches", got)
	}
	p.post("e1", map[string]any{"t": "visibility", "hidden": false})
	got := p.waitMsg("e1", "signals", 1)[0]
	if got.str("text") != `{"$n":2}` {
		t.Fatalf("batch after show = %v", got)
	}
}

// serve mounts an adder on channel "sim.cmd" and returns the sids the host
// subscribed with, so the test can play the page.
func serveAdder(t *testing.T, p *fakePage, h *Host, opts host.CallOptions, fn func(string, addArgs) (sum, error)) (call, result uint64) {
	t.Helper()
	_ = h.Register("Sim", func(in *Instance) (host.Handle, error) {
		return host.ServeCalls(in, "sim.cmd", opts, fn)
	})
	p.attach("e1", "Sim", false)
	mountEngine(t, p, "e1")
	subs := p.waitMsg("e1", "subscribe", 2)
	// The result subscription comes first, so a remount sees the old answer first.
	if subs[0].str("name") != "$sim.cmd.result" || subs[1].str("name") != "$sim.cmd" {
		t.Fatalf("subscriptions = %v, %v", subs[0], subs[1])
	}
	return uint64(subs[1].num("sid")), uint64(subs[0].num("sid"))
}

func (p *fakePage) writeCall(call uint64, c engine.Call[addArgs]) {
	p.post("e1", map[string]any{"t": "signal", "sid": call, "name": "$sim.cmd", "json": string(mustJSON(c))})
}

func (p *fakePage) results(n int) []engine.Result[sum] {
	p.t.Helper()
	var out []engine.Result[sum]
	waitFor(p.t, "call results", func() bool {
		out = nil
		for _, m := range p.messages("e1") {
			if m.t() != "signals" || string(m["changedOnly"]) != "false" {
				continue
			}
			var batch map[string]engine.Result[sum]
			if json.Unmarshal([]byte(m.str("text")), &batch) == nil {
				if r, ok := batch["$sim.cmd.result"]; ok {
					out = append(out, r)
				}
			}
		}
		return len(out) >= n
	})
	return out
}

func TestServeCallsDeclaresTheChannelAndAnswersOverTheBridge(t *testing.T) {
	p := newFakePage(t)
	h := newHost(t, p)
	call, _ := serveAdder(t, p, h, host.CallOptions{Timeout: 3 * time.Second, Recent: 3}, func(name string, a addArgs) (sum, error) {
		if name != "add" {
			return sum{}, errors.New("unknown call " + name)
		}
		return sum{a.A + a.B}, nil
	})
	declared := p.waitMsg("e1", "calls", 1)[0]
	if declared.str("channel") != "$sim.cmd" || declared.num("timeout") != 3000 || declared.num("recent") != 3 {
		t.Fatalf("declared = %v", declared)
	}
	var zero engine.Result[sum]
	if err := json.Unmarshal([]byte(declared.str("zero")), &zero); err != nil {
		t.Fatalf("zero result is not a result: %v", err)
	}

	p.writeCall(call, engine.Call[addArgs]{ID: 1, Name: "add", Args: addArgs{2, 3}})
	r := p.results(1)[0]
	if r.ID != 1 || r.Name != "add" || r.Value.Total != 5 || r.Error != "" || len(r.Recent) != 1 {
		t.Fatalf("result = %+v", r)
	}
	// A repeated delivery of the same call runs nothing.
	p.writeCall(call, engine.Call[addArgs]{ID: 1, Name: "add", Args: addArgs{9, 9}})
	// A handler error comes back as the result's Error.
	p.writeCall(call, engine.Call[addArgs]{ID: 2, Name: "nope"})
	rs := p.results(2)
	if len(rs) != 2 || rs[1].ID != 2 || rs[1].Error != "unknown call nope" {
		t.Fatalf("results = %+v", rs)
	}
	// Bad arguments are an error result, not silence.
	p.post("e1", map[string]any{"t": "signal", "sid": call, "name": "$sim.cmd", "json": `{"ID":3,"Name":"add","Args":"x"}`})
	rs = p.results(3)
	if rs[2].ID != 3 || len(rs[2].Error) < 8 || rs[2].Error[:8] != "bad-args" {
		t.Fatalf("bad args = %+v", rs[2])
	}
}

func TestServeCallsSkipsACallTheResultSignalAlreadyAnswered(t *testing.T) {
	p := newFakePage(t)
	h := newHost(t, p)
	var runs int
	_, result := serveAdder(t, p, h, host.CallOptions{}, func(string, addArgs) (sum, error) { runs++; return sum{}, nil })
	subs := p.waitMsg("e1", "subscribe", 2)
	call := uint64(subs[1].num("sid"))
	// A remount: the page replays the old answer, then the old call.
	p.post("e1", map[string]any{"t": "signal", "sid": result, "name": "$sim.cmd.result", "json": `{"ID":4}`, "initial": true})
	p.post("e1", map[string]any{"t": "signal", "sid": call, "name": "$sim.cmd", "json": `{"ID":4,"Name":"add"}`, "initial": true})
	p.writeCall(call, engine.Call[addArgs]{ID: 5, Name: "add"})
	p.results(1)
	if runs != 1 {
		t.Fatalf("handler ran %d times; the already-answered call must not run again", runs)
	}
}

func TestACallRunsOffTheDispatchGoroutine(t *testing.T) {
	p := newFakePage(t)
	h := newHost(t, p)
	release := make(chan struct{})
	entered := make(chan struct{})
	call, _ := serveAdder(t, p, h, host.CallOptions{}, func(string, addArgs) (sum, error) {
		close(entered)
		<-release
		return sum{1}, nil
	})
	p.writeCall(call, engine.Call[addArgs]{ID: 1, Name: "slow"}) // returns while the handler blocks
	<-entered
	// The window thread is free: another request still gets its response.
	p.post("e1", map[string]any{"t": "visibility", "hidden": false})
	close(release)
	if r := p.results(1)[0]; r.Value.Total != 1 {
		t.Fatalf("result = %+v", r)
	}
}

// A mailbox in the call signal delivers every call, and ServeCallsAsync runs
// the handlers off the session goroutine, one at a time in ID order, writing
// each result before the next call starts.
func TestServeCallsAsyncAnswersAMailboxOverTheBridgeInOrder(t *testing.T) {
	p := newFakePage(t)
	h := newHost(t, p)
	gate := make(chan struct{})
	entered := make(chan struct{})
	var once sync.Once
	var mu sync.Mutex
	var started []string
	var resultsWhenSecondStarted int
	_ = h.Register("Sim", func(in *Instance) (host.Handle, error) {
		return host.ServeCallsAsync(in, "sim.cmd", host.CallOptions{}, func(name string, a addArgs) (sum, error) {
			mu.Lock()
			started = append(started, name)
			if name == "add" {
				resultsWhenSecondStarted = p.count("e1", "signals")
			}
			mu.Unlock()
			if name == "slow" {
				once.Do(func() { close(entered) })
				<-gate
			}
			return sum{a.A + a.B}, nil
		})
	})
	p.attach("e1", "Sim", false)
	mountEngine(t, p, "e1")
	subs := p.waitMsg("e1", "subscribe", 2)
	call := uint64(subs[1].num("sid"))
	p.post("e1", map[string]any{"t": "signal", "sid": call, "name": "$sim.cmd",
		"json": `[{"ID":2,"Name":"add","Args":{"A":2,"B":3}},{"ID":1,"Name":"slow","Args":{"A":1,"B":1}}]`})
	<-entered
	time.Sleep(20 * time.Millisecond)
	mu.Lock()
	if len(started) != 1 {
		mu.Unlock()
		t.Fatalf("handlers started %v while call 1 still ran; want only slow", started)
	}
	mu.Unlock()
	if got := p.count("e1", "signals"); got != 0 {
		t.Fatalf("call 2 was answered while call 1 still ran: %d result batches", got)
	}
	close(gate)
	rs := p.results(2)
	if rs[0].ID != 1 || rs[0].Value.Total != 2 || rs[1].ID != 2 || rs[1].Value.Total != 5 {
		t.Fatalf("results = %+v", rs)
	}
	if len(rs[1].Recent) != 2 || rs[1].Recent[0].ID != 1 || rs[1].Recent[1].ID != 2 {
		t.Fatalf("Recent = %+v", rs[1].Recent)
	}
	mu.Lock()
	defer mu.Unlock()
	if !reflect.DeepEqual(started, []string{"slow", "add"}) || resultsWhenSecondStarted != 1 {
		t.Fatalf("started %v; results written when call 2 started: %d, want [slow add] and 1", started, resultsWhenSecondStarted)
	}
}

func TestFailReportsACrashAndDisposeTearsDown(t *testing.T) {
	p := newFakePage(t)
	h := newHost(t, p)
	var inst *Instance
	disposed := make(chan struct{})
	_ = h.Register("Sim", func(in *Instance) (host.Handle, error) {
		inst = in
		return host.HandleFunc(func() { close(disposed) }), nil
	})
	p.attach("e1", "Sim", false)
	mountEngine(t, p, "e1")
	inst.Fail(errors.New("the world is inconsistent"))
	fatal := p.waitMsg("e1", "fatal", 1)[0]
	if fatal.str("code") != "engine-crashed" || fatal.str("message") != "the world is inconsistent" {
		t.Fatalf("fatal = %v", fatal)
	}
	inst.Log("warn", "careful")
	if l := p.waitMsg("e1", "log", 1)[0]; l.str("level") != "warn" || l.str("text") != "careful" {
		t.Fatalf("log = %v", l)
	}
	p.post("e1", map[string]any{"t": "dispose"})
	<-disposed
	p.waitMsg("e1", "disposed", 1)
	// The instance is gone: later posts are refused, and its view cannot send.
	waitFor(t, "session removal", func() bool {
		return p.request(MethodPost, map[string]any{"id": "e1", "msg": map[string]any{"t": "ack"}}).Op == bridge.OpError
	})
	if err := inst.ViewSink().Send([]byte(`{}`), func(bool) {}); err == nil {
		t.Fatal("a disposed instance sent a batch")
	}
}

func TestAttachAgainReplacesTheInstanceAfterTheOldOneIsDisposed(t *testing.T) {
	p := newFakePage(t)
	h := newHost(t, p)
	var mu sync.Mutex
	var order []string
	n := 0
	_ = h.Register("Sim", func(in *Instance) (host.Handle, error) {
		mu.Lock()
		n++
		me := n
		order = append(order, "mount"+string(rune('0'+me)))
		mu.Unlock()
		return host.HandleFunc(func() {
			time.Sleep(20 * time.Millisecond) // a slow save
			mu.Lock()
			order = append(order, "dispose"+string(rune('0'+me)))
			mu.Unlock()
		}), nil
	})
	p.attach("e1", "Sim", false)
	mountEngine(t, p, "e1")
	p.attach("e1", "Sim", false) // the page reloaded
	p.post("e1", map[string]any{"t": "mount"})
	p.waitMsg("e1", "mounted", 2)
	mu.Lock()
	defer mu.Unlock()
	want := []string{"mount1", "dispose1", "mount2"}
	if len(order) != 3 || order[0] != want[0] || order[1] != want[1] || order[2] != want[2] {
		t.Fatalf("order = %v, want %v", order, want)
	}
}

func TestCloseDisposesEveryInstanceAndRefusesNewAttaches(t *testing.T) {
	p := newFakePage(t)
	h := New(p.router, Options{})
	var disposed int
	var mu sync.Mutex
	_ = h.Register("Sim", func(*Instance) (host.Handle, error) {
		return host.HandleFunc(func() { mu.Lock(); disposed++; mu.Unlock() }), nil
	})
	for _, id := range []string{"a", "b"} {
		p.attach(id, "Sim", false)
		mountEngine(t, p, id)
	}
	h.Close()
	mu.Lock()
	if disposed != 2 {
		t.Fatalf("disposed = %d", disposed)
	}
	mu.Unlock()
	if env := p.attach("c", "Sim", false); env.Op != bridge.OpError {
		t.Fatalf("attach after close = %+v", env)
	}
}

func TestRegisterValidatesItsArguments(t *testing.T) {
	h := New(newFakePage(t).router, Options{})
	if h.Register(" ", func(*Instance) (host.Handle, error) { return nil, nil }) == nil {
		t.Error("an empty component name was accepted")
	}
	if h.Register("Sim", nil) == nil {
		t.Error("a nil factory was accepted")
	}
}

func TestReplacementRejectsStaleSessionTraffic(t *testing.T) {
	p := newFakePage(t)
	h := newHost(t, p)
	instances := make(chan *Instance, 2)
	_ = h.Register("Sim", func(in *Instance) (host.Handle, error) { instances <- in; return host.HandleFunc(func() {}), nil })
	p.attach("e1", "Sim", false)
	mountEngine(t, p, "e1")
	old := <-instances
	oldToken := old.s.token
	p.attach("e1", "Sim", false)
	p.post("e1", map[string]any{"t": "mount"})
	p.waitMsg("e1", "mounted", 2)
	current := <-instances
	if oldToken == "" || current.s.token == oldToken {
		t.Fatal("attachments must have distinct session tokens")
	}
	for _, token := range []string{"", oldToken} {
		e := p.request(MethodPost, map[string]any{"id": "e1", "session": token, "msg": map[string]any{"t": "dispose"}})
		if e.Op != bridge.OpError || e.Error == nil || e.Error.Code != CodeStaleSession {
			t.Fatalf("stale post accepted: %+v", e)
		}
	}
	if err := old.CallTransport().Write("$late", 1); err == nil {
		t.Fatal("replaced instance wrote into its replacement")
	}
	before := len(p.messages("e1"))
	old.Fail(errors.New("late failure"))
	old.Log("warn", "late log")
	if len(p.messages("e1")) != before {
		t.Fatal("replaced instance emitted into its replacement")
	}
	p.post("e1", map[string]any{"t": "visibility", "hidden": true})
	waitFor(t, "replacement remains alive", func() bool { return current.ViewSink().Hidden() })
}
