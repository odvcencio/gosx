//go:build js && wasm

package socket

import (
	"fmt"
	"syscall/js"
)

// Socket owns one browser socket and its event listeners. Methods and callbacks
// should be used on the browser event loop. Dispose releases listeners even
// when the host never delivers a close event.
type Socket struct {
	ws                                 js.Value
	callbacks                          Callbacks
	listeners                          [4]js.Func
	bound                              int
	disposed, terminal, closeRequested bool
	receiveDepth                       int
	receive                            []byte
}

// Dial returns a connecting socket; handshake results arrive through callbacks.
func Dial(endpoint string, callbacks Callbacks) (result *Socket, err error) {
	var s *Socket
	defer func() {
		if r := recover(); r != nil {
			if s != nil {
				s.Dispose()
			}
			result = nil
			err = fmt.Errorf("socket: dial: %v", r)
		}
	}()
	s = &Socket{ws: js.Global().Get("WebSocket").New(endpoint), callbacks: callbacks}
	s.ws.Set("binaryType", "arraybuffer")
	s.bind(0, "open", func([]js.Value) {
		if fn := s.callbacks.Open; fn != nil {
			fn()
		}
	})
	s.bind(1, "message", func(args []js.Value) {
		if len(args) == 0 {
			return
		}
		data := args[0].Get("data")
		fn := s.callbacks.Message
		if fn == nil {
			return
		}
		if data.Type() == js.TypeString {
			fn(Message{Text: data.String()})
			return
		}
		view := js.Global().Get("Uint8Array").New(data)
		n := view.Get("byteLength").Int()
		var buf []byte
		if s.receiveDepth == 0 {
			if cap(s.receive) < n {
				s.receive = make([]byte, n)
			}
			buf = s.receive[:n]
		} else {
			buf = make([]byte, n)
		}
		js.CopyBytesToGo(buf, view)
		s.receiveDepth++
		defer func() { s.receiveDepth-- }()
		fn(Message{Binary: buf, IsBinary: true})
	})
	s.bind(2, "error", func([]js.Value) {
		if fn := s.callbacks.Error; fn != nil {
			fn(fmt.Errorf("socket: websocket error"))
		}
	})
	s.bind(3, "close", func(args []js.Value) {
		event := CloseEvent{}
		if len(args) > 0 {
			event.Code = args[0].Get("code").Int()
			event.Reason = args[0].Get("reason").String()
			clean := args[0].Get("wasClean")
			event.Clean = clean.Type() == js.TypeBoolean && clean.Bool()
		}
		s.terminal = true
		fn := s.callbacks.Closed
		s.release()
		s.callbacks = Callbacks{}
		if fn != nil {
			fn(event)
		}
	})
	return s, nil
}

func (s *Socket) bind(index int, event string, fn func([]js.Value)) {
	s.listeners[index] = js.FuncOf(func(_ js.Value, args []js.Value) any {
		if !s.disposed && !s.terminal {
			fn(args)
		}
		return nil
	})
	// Track before attaching so construction failures can release the function.
	s.bound++
	s.ws.Call("addEventListener", event, s.listeners[index])
}

func (s *Socket) release() {
	for i, name := range [...]string{"open", "message", "error", "close"} {
		if i >= s.bound {
			break
		}
		func() {
			defer func() { _ = recover() }()
			s.ws.Call("removeEventListener", name, s.listeners[i])
		}()
		s.listeners[i].Release()
	}
	s.bound = 0
}

func (s *Socket) ReadyState() State {
	if s == nil || s.disposed || s.terminal {
		return Closed
	}
	return State(s.ws.Get("readyState").Int())
}
func (s *Socket) BufferedAmount() int {
	if s == nil || s.disposed || s.terminal {
		return 0
	}
	return s.ws.Get("bufferedAmount").Int()
}
func (s *Socket) SendText(data string) error { return s.send(data) }
func (s *Socket) SendBinary(data []byte) (err error) {
	if s == nil || s.disposed {
		return ErrDisposed
	}
	if s.ReadyState() != Open {
		return ErrNotOpen
	}
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("socket: send: %v", r)
		}
	}()
	view := js.Global().Get("Uint8Array").New(len(data))
	js.CopyBytesToJS(view, data)
	return s.send(view)
}
func (s *Socket) send(data any) (err error) {
	if s == nil || s.disposed {
		return ErrDisposed
	}
	if s.ReadyState() != Open {
		return ErrNotOpen
	}
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("socket: send: %v", r)
		}
	}()
	s.ws.Call("send", data)
	return nil
}

// Close starts graceful closure and preserves the terminal Closed callback.
// Code zero uses the host default. Repeated requests never send another close.
func (s *Socket) Close(code int, reason string) (err error) {
	if s == nil || s.disposed || s.terminal || s.closeRequested {
		return nil
	}
	defer func() {
		if r := recover(); r != nil {
			s.closeRequested = false
			err = fmt.Errorf("socket: close: %v", r)
		}
	}()
	s.closeRequested = true
	if code == 0 {
		s.ws.Call("close")
	} else {
		s.ws.Call("close", code, reason)
	}
	return nil
}

// Dispose retires the socket immediately, suppresses all further callbacks,
// releases receive storage and listeners, and requests host closure once.
func (s *Socket) Dispose() {
	if s == nil || s.disposed {
		return
	}
	s.disposed = true
	s.callbacks = Callbacks{}
	s.receive = nil
	s.release()
	if !s.terminal && !s.closeRequested {
		s.closeRequested = true
		func() { defer func() { _ = recover() }(); s.ws.Call("close") }()
	}
}
