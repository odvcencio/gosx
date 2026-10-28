//go:build js && wasm

package client

import (
	"encoding/json"
	"errors"
	"sync"
	"syscall/js"

	"m31labs.dev/gosx/client/jsutil"
	"m31labs.dev/gosx/netplay"
)

// jsLink drives window.__gosx.netplay (client/runtime/host/netplay.ts).
type jsLink struct {
	api   js.Value
	mu    sync.Mutex
	id    int
	out   js.Value // Uint8Array scratch for Send
	in    js.Value // Uint8Array scratch for Recv
	gobuf [netplay.MaxPacketSize]byte

	// The binding calls onState from inside open (when the connection cannot
	// start) and inside signal (when the relay reports a failure). They are
	// deferred callbacks, so Go runs them after that call into the binding has
	// returned and before Open or Signal returns.
	onSignal, onState *jsutil.Callback
}

// newBrowserLink returns a link over the page's netplay binding. It fails with
// errNoBrowser until the "netplay" runtime feature has loaded.
func newBrowserLink() (rtcLink, error) {
	host := js.Global().Get("__gosx")
	if host.IsUndefined() || host.IsNull() {
		return nil, errNoBrowser
	}
	api := host.Get("netplay")
	if api.IsUndefined() || api.IsNull() || api.Get("open").Type() != js.TypeFunction {
		return nil, errNoBrowser
	}
	if sup := api.Call("supported"); sup.Type() != js.TypeBoolean || !sup.Bool() {
		return nil, errors.New("netplay/client: this browser has no RTCPeerConnection")
	}
	u8 := js.Global().Get("Uint8Array")
	return &jsLink{api: api, out: u8.New(netplay.MaxPacketSize), in: u8.New(netplay.MaxPacketSize)}, nil
}

func (l *jsLink) Open(cfg rtcConfig, onSignal func(string, []byte), onState func(string)) error {
	servers := make([]map[string]any, 0, len(cfg.ICEServers))
	for _, s := range cfg.ICEServers {
		urls := make([]any, len(s.URLs))
		for i, u := range s.URLs {
			urls[i] = u
		}
		e := map[string]any{"urls": urls}
		if s.Username != "" {
			e["username"] = s.Username
		}
		if s.Credential != "" {
			e["credential"] = s.Credential
		}
		servers = append(servers, e)
	}
	raw, err := json.Marshal(map[string]any{"session": cfg.Session, "iceServers": servers})
	if err != nil {
		return err
	}
	obj := js.Global().Get("JSON").Call("parse", string(raw))
	l.onSignal = jsutil.NewCallback(func(_ js.Value, args []js.Value) {
		if len(args) == 2 {
			onSignal(args[0].String(), []byte(args[1].String()))
		}
	})
	l.onState = jsutil.NewCallback(func(_ js.Value, args []js.Value) {
		if len(args) == 1 {
			onState(args[0].String())
		}
	})
	obj.Set("onSignal", l.onSignal.Value())
	obj.Set("onState", l.onState.Value())
	// A refused connection reports "failed" inside open. The state callback runs
	// before Call returns, so it is delivered before the callbacks are released
	// below, and before Open reports the error.
	id := jsutil.Call(l.api, "open", obj).Int()
	if id <= 0 {
		l.release()
		return errors.New("netplay/client: the browser refused the WebRTC connection")
	}
	l.mu.Lock()
	l.id = id
	l.mu.Unlock()
	return nil
}

func (l *jsLink) conn() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.id
}

func (l *jsLink) Signal(event string, data []byte) {
	if id := l.conn(); id > 0 {
		jsutil.Call(l.api, "signal", id, event, string(data))
	}
}

func (l *jsLink) Send(p []byte) bool {
	id := l.conn()
	if id <= 0 || len(p) == 0 || len(p) > netplay.MaxPacketSize {
		return false
	}
	js.CopyBytesToJS(l.out, p)
	return l.api.Call("send", id, l.out, len(p)).Truthy()
}

func (l *jsLink) Recv(buf []byte) (int, bool) {
	id := l.conn()
	if id <= 0 {
		return 0, false
	}
	n := l.api.Call("recv", id, l.in).Int()
	if n < 0 {
		return 0, false
	}
	js.CopyBytesToGo(l.gobuf[:], l.in)
	return copy(buf, l.gobuf[:n]), true
}

func (l *jsLink) Close() {
	l.mu.Lock()
	id := l.id
	l.id = 0
	l.mu.Unlock()
	if id > 0 {
		l.api.Call("close", id)
	}
	l.release()
}

func (l *jsLink) release() {
	l.onSignal.Release()
	l.onState.Release()
}
