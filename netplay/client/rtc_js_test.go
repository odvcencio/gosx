//go:build js && wasm

package client

import (
	"encoding/json"
	"syscall/js"
	"testing"

	"m31labs.dev/gosx/netplay/signal"
)

// fakeBinding installs a window.__gosx_netplay written in Go and records calls.
type fakeBinding struct {
	cfg       js.Value
	sent      [][]byte
	signals   []string
	incoming  [][]byte
	closedIDs []int
	funcs     []js.Func
}

func (f *fakeBinding) install(t *testing.T, supported bool) {
	g := js.Global()
	prev := g.Get("__gosx_netplay")
	t.Cleanup(func() {
		g.Set("__gosx_netplay", prev)
		for _, fn := range f.funcs {
			fn.Release()
		}
	})
	fn := func(h func(args []js.Value) any) js.Func {
		x := js.FuncOf(func(_ js.Value, args []js.Value) any { return h(args) })
		f.funcs = append(f.funcs, x)
		return x
	}
	api := g.Get("Object").New()
	api.Set("supported", fn(func([]js.Value) any { return supported }))
	api.Set("open", fn(func(a []js.Value) any { f.cfg = a[0]; return 7 }))
	api.Set("signal", fn(func(a []js.Value) any {
		f.signals = append(f.signals, a[1].String()+"|"+a[2].String())
		return nil
	}))
	api.Set("send", fn(func(a []js.Value) any {
		n := a[2].Int()
		b := make([]byte, n)
		js.CopyBytesToGo(b, a[1])
		f.sent = append(f.sent, b)
		return true
	}))
	api.Set("recv", fn(func(a []js.Value) any {
		if len(f.incoming) == 0 {
			return -1
		}
		p := f.incoming[0]
		f.incoming = f.incoming[1:]
		js.CopyBytesToJS(a[1], p)
		return len(p)
	}))
	api.Set("close", fn(func(a []js.Value) any { f.closedIDs = append(f.closedIDs, a[0].Int()); return nil }))
	g.Set("__gosx_netplay", api)
}

func TestJSLinkRoundTrip(t *testing.T) {
	f := &fakeBinding{}
	f.install(t, true)
	l, err := newBrowserLink()
	if err != nil {
		t.Fatal(err)
	}
	var events []string
	var states []string
	err = l.Open(rtcConfig{Session: "s1", ICEServers: []signal.ICEServer{
		{URLs: []string{"stun:a:3478"}}, {URLs: []string{"turn:b:3478"}, Username: "u", Credential: "c"},
	}}, func(ev string, data []byte) { events = append(events, ev+"|"+string(data)) }, func(s string) { states = append(states, s) })
	if err != nil {
		t.Fatal(err)
	}
	cfgJSON := js.Global().Get("JSON").Call("stringify", f.cfg.Get("iceServers")).String()
	var servers []signal.ICEServer
	if err := json.Unmarshal([]byte(cfgJSON), &servers); err != nil || len(servers) != 2 || servers[1].Credential != "c" || servers[0].Username != "" {
		t.Fatalf("ice servers %s (%v)", cfgJSON, err)
	}
	if f.cfg.Get("session").String() != "s1" {
		t.Fatal("session not passed")
	}

	// Callbacks the binding would invoke reach the Go closures.
	f.cfg.Get("onSignal").Invoke("netplay.offer", `{"session":"s1"}`)
	f.cfg.Get("onState").Invoke("open")
	if len(events) != 1 || events[0] != `netplay.offer|{"session":"s1"}` || len(states) != 1 || states[0] != "open" {
		t.Fatalf("events %v states %v", events, states)
	}

	l.Signal("netplay.answer", []byte(`{"x":1}`))
	if len(f.signals) != 1 || f.signals[0] != `netplay.answer|{"x":1}` {
		t.Fatalf("signals %v", f.signals)
	}

	if !l.Send([]byte{1, 2, 3}) || string(f.sent[0]) != "\x01\x02\x03" {
		t.Fatalf("sent %v", f.sent)
	}
	if l.Send(nil) || l.Send(make([]byte, 1201)) {
		t.Fatal("bad datagram sizes were sent")
	}

	buf := make([]byte, 64)
	if _, ok := l.Recv(buf); ok {
		t.Fatal("recv on empty returned data")
	}
	f.incoming = [][]byte{{9, 8}, {7}}
	if n, ok := l.Recv(buf); !ok || n != 2 || buf[0] != 9 || buf[1] != 8 {
		t.Fatalf("recv %d %v %v", n, ok, buf[:n])
	}
	if n, ok := l.Recv(buf[:0]); !ok || n != 0 {
		t.Fatalf("recv into empty buffer: %d %v", n, ok)
	}

	l.Close()
	if len(f.closedIDs) != 1 || f.closedIDs[0] != 7 {
		t.Fatalf("closed %v", f.closedIDs)
	}
	if l.Send([]byte{1}) {
		t.Fatal("send after close succeeded")
	}
	l.Close() // idempotent
	if len(f.closedIDs) != 1 {
		t.Fatal("second close reached the binding")
	}
}

func TestJSLinkMissingOrUnsupported(t *testing.T) {
	g := js.Global()
	prev := g.Get("__gosx_netplay")
	g.Set("__gosx_netplay", js.Undefined())
	if _, err := newBrowserLink(); err != errNoBrowser {
		t.Fatalf("missing binding: %v", err)
	}
	g.Set("__gosx_netplay", prev)
	f := &fakeBinding{}
	f.install(t, false)
	if _, err := newBrowserLink(); err == nil {
		t.Fatal("unsupported browser accepted")
	}
}
