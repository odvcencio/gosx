//go:build js && wasm

package socket

import (
	"bytes"
	"fmt"
	"syscall/js"
	"testing"
)

type fakeSocket struct {
	value      js.Value
	listeners  map[string]js.Value
	functions  []js.Func
	sent       []js.Value
	closeCalls int
}

func installSocket(t testing.TB) *fakeSocket {
	t.Helper()
	g := js.Global()
	old := g.Get("WebSocket")
	f := &fakeSocket{value: g.Get("Object").New(), listeners: map[string]js.Value{}}
	f.value.Set("readyState", 0)
	f.value.Set("bufferedAmount", 17)
	method := func(name string, fn func(js.Value, []js.Value) any) {
		cb := js.FuncOf(fn)
		f.functions = append(f.functions, cb)
		f.value.Set(name, cb)
	}
	method("addEventListener", func(_ js.Value, a []js.Value) any { f.listeners[a[0].String()] = a[1]; return nil })
	method("removeEventListener", func(_ js.Value, a []js.Value) any { delete(f.listeners, a[0].String()); return nil })
	method("send", func(_ js.Value, a []js.Value) any { f.sent = append(f.sent, a[0]); return nil })
	method("close", func(_ js.Value, a []js.Value) any { f.closeCalls++; f.value.Set("readyState", 2); return nil })
	constructor := js.FuncOf(func(_ js.Value, a []js.Value) any { return f.value })
	f.functions = append(f.functions, constructor)
	g.Set("WebSocket", constructor)
	t.Cleanup(func() {
		g.Set("WebSocket", old)
		for _, fn := range f.functions {
			fn.Release()
		}
	})
	return f
}
func (f *fakeSocket) event(name string, fields map[string]any) {
	if fn, ok := f.listeners[name]; ok {
		fn.Invoke(js.ValueOf(fields))
	}
}
func (f *fakeSocket) binary(data []byte) {
	v := js.Global().Get("Uint8Array").New(len(data))
	js.CopyBytesToJS(v, data)
	f.event("message", map[string]any{"data": v.Get("buffer")})
}

func TestLifecycleAndExactlyOnceSend(t *testing.T) {
	f := installSocket(t)
	opens, errors, closes := 0, 0, 0
	var terminal CloseEvent
	s, err := Dial("wss://example.test/crew", Callbacks{
		Open: func() { opens++ }, Error: func(error) { errors++ }, Closed: func(e CloseEvent) { closes++; terminal = e },
	})
	if err != nil {
		t.Fatal(err)
	}
	if f.value.Get("binaryType").String() != "arraybuffer" {
		t.Fatal("binary receive type")
	}
	if s.ReadyState() != Connecting || s.BufferedAmount() != 17 {
		t.Fatal("initial state")
	}
	if s.SendText("pending") != ErrNotOpen || len(f.sent) != 0 {
		t.Fatal("pending send")
	}
	f.value.Set("readyState", 1)
	f.event("open", nil)
	if err := s.SendText("hello"); err != nil {
		t.Fatal(err)
	}
	if err := s.SendBinary([]byte{0, 255, 1}); err != nil {
		t.Fatal(err)
	}
	if opens != 1 || len(f.sent) != 2 || f.sent[0].String() != "hello" {
		t.Fatal("send delivered more than once")
	}
	binary := make([]byte, 3)
	js.CopyBytesToGo(binary, f.sent[1])
	if !bytes.Equal(binary, []byte{0, 255, 1}) {
		t.Fatal(binary)
	}
	f.event("error", nil)
	if errors != 1 || len(f.listeners) != 4 {
		t.Fatal("error prematurely retired callbacks")
	}
	if err := s.Close(4000, "state timeout"); err != nil {
		t.Fatal(err)
	}
	_ = s.Close(4000, "again")
	if f.closeCalls != 1 || closes != 0 || s.ReadyState() != Closing {
		t.Fatal("graceful close")
	}
	f.value.Set("readyState", 3)
	f.event("close", map[string]any{"code": 1008, "reason": "session superseded", "wasClean": true})
	if closes != 1 || terminal.Code != 1008 || terminal.Reason != "session superseded" || !terminal.Clean || len(f.listeners) != 0 {
		t.Fatal(terminal, closes, len(f.listeners))
	}
	s.Dispose()
	s.Dispose()
	if f.closeCalls != 1 || s.ReadyState() != Closed || s.SendText("late") != ErrDisposed {
		t.Fatal("terminal disposal")
	}
}

func TestBinaryStorageReuseAndReentry(t *testing.T) {
	f := installSocket(t)
	var first *byte
	messages := 0
	s, err := Dial("ws://example.test", Callbacks{Message: func(m Message) {
		if !m.IsBinary || m.Text != "" {
			t.Fatal("binary type")
		}
		messages++
		switch messages {
		case 1:
			first = &m.Binary[0]
			f.binary([]byte{9, 8})
			if !bytes.Equal(m.Binary, []byte{1, 2, 3}) {
				t.Fatal("reentry corrupted active storage")
			}
		case 2:
			if &m.Binary[0] == first || !bytes.Equal(m.Binary, []byte{9, 8}) {
				t.Fatal("nested frame storage")
			}
		case 3:
			if &m.Binary[0] != first || !bytes.Equal(m.Binary, []byte{4, 5}) {
				t.Fatal("receive storage not reused")
			}
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Dispose()
	f.binary([]byte{1, 2, 3})
	f.binary([]byte{4, 5})
	if messages != 3 {
		t.Fatal(messages)
	}
}

func TestDisposeFromMessageAndCloseCallbacks(t *testing.T) {
	for _, event := range []string{"message", "close", "open", "error"} {
		t.Run(event, func(t *testing.T) {
			f := installSocket(t)
			calls := 0
			var s *Socket
			retire := func() { calls++; s.Dispose(); s.Dispose() }
			callbacks := Callbacks{}
			switch event {
			case "message":
				callbacks.Message = func(m Message) {
					if m.Text != "text" || m.IsBinary {
						t.Fatal(m)
					}
					retire()
				}
			case "open":
				callbacks.Open = retire
			case "error":
				callbacks.Error = func(error) { retire() }
			case "close":
				callbacks.Closed = func(CloseEvent) { retire() }
			}
			var err error
			s, err = Dial("ws://example.test", callbacks)
			if err != nil {
				t.Fatal(err)
			}
			fields := map[string]any{"data": "text", "code": 1000, "reason": "", "wasClean": true}
			f.event(event, fields)
			f.event(event, fields)
			if calls != 1 || len(f.listeners) != 0 || s.ReadyState() != Closed {
				t.Fatal(calls, len(f.listeners))
			}
			wantClose := 1
			if event == "close" {
				wantClose = 0
			}
			if f.closeCalls != wantClose {
				t.Fatal(f.closeCalls)
			}
		})
	}
}

func TestConstructorAndSendErrors(t *testing.T) {
	f := installSocket(t)
	previous := js.Global().Get("WebSocket")
	js.Global().Set("WebSocket", js.Undefined())
	if s, err := Dial("invalid", Callbacks{}); s != nil || err == nil {
		t.Fatal("missing constructor accepted")
	}
	js.Global().Set("WebSocket", previous)
	s, err := Dial("ws://example.test", Callbacks{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Dispose()
	f.value.Set("readyState", 1)
	f.value.Set("send", js.Undefined())
	if s.SendText("x") == nil || s.SendBinary([]byte{1}) == nil {
		t.Fatal("host send failure ignored")
	}
}

func TestDisposeStressReleasesPendingSockets(t *testing.T) {
	f := installSocket(t)
	for i := 0; i < 1000; i++ {
		f.value.Set("readyState", 0)
		s, err := Dial("ws://example.test/pending", Callbacks{})
		if err != nil {
			t.Fatal(err)
		}
		if len(f.listeners) != 4 {
			t.Fatal("listeners were not attached")
		}
		s.Dispose()
		if len(f.listeners) != 0 {
			t.Fatalf("iteration %d leaked listeners", i)
		}
	}
	if f.closeCalls != 1000 {
		t.Fatal("pending close requests", f.closeCalls)
	}
}

// This runs the actual compiled WASM receive callback and host-to-Go copy. The
// message and ArrayBuffer are prepared before timing to isolate transport cost.
// owned-copy represents callers that retain callback bytes across events.
func BenchmarkBinaryReceive(b *testing.B) {
	for _, size := range []int{4096, 32768} {
		for _, retain := range []bool{false, true} {
			name := "borrowed"
			if retain {
				name = "owned-copy"
			}
			b.Run(fmt.Sprintf("%d/%s", size, name), func(b *testing.B) {
				f := installSocket(b)
				var last []byte
				s, err := Dial("ws://example.test", Callbacks{Message: func(m Message) {
					if retain {
						last = append([]byte(nil), m.Binary...)
					} else {
						last = m.Binary
					}
				}})
				if err != nil {
					b.Fatal(err)
				}
				defer s.Dispose()
				view := js.Global().Get("Uint8Array").New(size)
				message := js.ValueOf(map[string]any{"data": view.Get("buffer")})
				callback := f.listeners["message"]
				callback.Invoke(message) // warm receive storage
				b.SetBytes(int64(size))
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					callback.Invoke(message)
				}
				b.StopTimer()
				if len(last) != size {
					b.Fatal("receive truncated")
				}
			})
		}
	}
}
