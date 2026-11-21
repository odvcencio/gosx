//go:build js && wasm && gosx_padtest

package host

import (
	"fmt"
	"syscall/js"
)

// padTestAttribute is set on the document element: "connecting", "ready",
// "failed" or "lost". A page check reads it to tell a missing driver from a
// working one.
const padTestAttribute = "data-padtest"

// padTestTimeoutMS is how long the page waits for ready before it gives up.
const padTestTimeoutMS = 10000

// padTestMaxQueued bounds the messages held until the server answers hello.
const padTestMaxQueued = 256

// padTestPage is the connection state. Everything on it runs on the browser's
// one thread.
type padTestPage struct {
	ws      js.Value
	src     *PadTestSource
	session *PadTestSession
	queued  []PadMessage
	timer   js.Value
	ready   bool
	done    bool
	funcs   []js.Func
}

var padTestActive *padTestPage

// connectPadTest reads the page URL. Without the query field it does nothing.
// With it, it installs the test source in the GamepadReader first (from then
// on the reader never reads the navigator) and connects to the loopback server
// named there. A bad value, an unreachable server, a bad handshake or an engine
// worker (which has no page to mark and does not receive the query) ends in
// teardown: the document is marked "failed" where there is one, the error is
// logged without the URL's token, and the source stays empty.
func connectPadTest() {
	location := js.Global().Get("location")
	if !location.Truthy() {
		return
	}
	search := location.Get("search")
	if search.Type() != js.TypeString || search.String() == "" {
		return
	}
	params := js.Global().Get("URLSearchParams")
	if !params.Truthy() {
		return
	}
	value := params.New(search.String()).Call("get", PadTestQueryField)
	if value.IsNull() || value.IsUndefined() {
		return
	}
	src := NewPadTestSource()
	p := &padTestPage{src: src}
	padHook.Store(&padHooks{source: src, begin: func() { src.BeginFrame() }, end: p.endFrame})
	padTestActive = p
	if workerClock().Truthy() || !js.Global().Get("document").Truthy() {
		p.teardown("failed", "the pad test adapter runs on the main thread only; it refuses an engine worker")
		return
	}
	port, token, err := ParsePadTestEndpoint(value.String())
	if err != nil {
		p.teardown("failed", err.Error())
		return
	}
	p.session = NewPadTestSession(src, token)
	p.setStatus("connecting")
	p.open(PadTestWebSocketURL(port))
}

func (p *padTestPage) setStatus(status string) {
	doc := js.Global().Get("document")
	if doc.Truthy() && doc.Get("documentElement").Truthy() {
		doc.Get("documentElement").Call("setAttribute", padTestAttribute, status)
	}
}

// teardown is the one way out, and it is idempotent. It drops the source's
// pads and queued changes (the reader then returns no pads and never real
// ones), marks the page, logs, cancels the timer, detaches the socket's
// handlers, closes the socket, releases every js.Func, and clears the session
// and queued replies.
func (p *padTestPage) teardown(status, reason string) {
	if p.done {
		return
	}
	p.done = true
	p.src.Lose()
	p.setStatus(status)
	js.Global().Get("console").Call("error", "gosx-padtest: "+reason+"; no pads are exposed")
	if p.timer.Truthy() {
		js.Global().Call("clearTimeout", p.timer)
	}
	p.timer = js.Undefined()
	if p.ws.Truthy() {
		func() {
			defer func() { _ = recover() }()
			for _, h := range []string{"onopen", "onmessage", "onerror", "onclose"} {
				p.ws.Set(h, js.Null())
			}
			p.ws.Call("close")
		}()
	}
	p.ws = js.Undefined()
	p.session = nil
	p.queued = nil
	funcs := p.funcs
	p.funcs = nil
	// A handler may be the caller; releasing it before it returns is allowed.
	for _, f := range funcs {
		f.Release()
	}
}

func (p *padTestPage) fn(f func(js.Value, []js.Value) any) js.Func {
	fn := js.FuncOf(f)
	p.funcs = append(p.funcs, fn)
	return fn
}

// fail ends a connection with the status that fits how far it got.
func (p *padTestPage) fail(reason string) {
	if p.ready {
		p.teardown("lost", reason)
	} else {
		p.teardown("failed", reason)
	}
}

func (p *padTestPage) open(url string) {
	var ws js.Value
	func() {
		defer func() {
			if r := recover(); r != nil {
				p.teardown("failed", fmt.Sprint("cannot open WebSocket: ", r))
			}
		}()
		ws = js.Global().Get("WebSocket").New(url)
	}()
	if p.done {
		return
	}
	p.ws = ws
	ws.Set("onopen", p.fn(func(js.Value, []js.Value) any {
		p.send(p.session.Hello())
		return nil
	}))
	ws.Set("onmessage", p.fn(func(_ js.Value, args []js.Value) any { p.message(args); return nil }))
	ws.Set("onerror", p.fn(func(js.Value, []js.Value) any {
		p.fail("WebSocket error (is the pad test server running?)")
		return nil
	}))
	ws.Set("onclose", p.fn(func(js.Value, []js.Value) any {
		p.fail("WebSocket closed")
		return nil
	}))
	p.timer = js.Global().Call("setTimeout", p.fn(func(js.Value, []js.Value) any {
		if !p.ready {
			p.teardown("failed", "server did not answer hello")
		}
		return nil
	}), padTestTimeoutMS)
}

func (p *padTestPage) message(args []js.Value) {
	if p.done {
		return
	}
	if len(args) == 0 || args[0].Get("data").Type() != js.TypeString {
		p.fail("server sent a message that is not text")
		return
	}
	m, err := DecodePadMessage([]byte(args[0].Get("data").String()))
	if err != nil {
		p.fail(err.Error())
		return
	}
	replies, err := p.session.Receive(m)
	if err != nil {
		p.fail(err.Error())
		return
	}
	if m.Type == PadMsgReady {
		p.ready = true
		p.setStatus("ready")
		held := p.queued
		p.queued = nil
		for _, q := range held {
			p.send(q)
		}
	}
	for _, r := range replies {
		p.send(r)
	}
}

// send writes m, or holds it until the server has answered hello.
func (p *padTestPage) send(m PadMessage) {
	if p.done {
		return
	}
	if !p.ready && m.Type != PadMsgHello {
		if len(p.queued) < padTestMaxQueued {
			p.queued = append(p.queued, m)
		}
		return
	}
	data, err := EncodePadMessage(m)
	if err != nil {
		p.fail(err.Error())
		return
	}
	func() {
		defer func() {
			if r := recover(); r != nil {
				p.fail(fmt.Sprint("send failed: ", r))
			}
		}()
		p.ws.Call("send", string(data))
	}()
}

// endFrame is the frame bracket's end: it acknowledges the frame.
func (p *padTestPage) endFrame() {
	if p.session == nil {
		p.src.EndFrame()
		return
	}
	for _, m := range p.session.EndFrame() {
		p.send(m)
	}
}
