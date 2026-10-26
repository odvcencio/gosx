//go:build js && wasm

package wasm

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sync"
	"syscall/js"

	"m31labs.dev/gosx/engine"
)

// Thread reports where this engine instance runs. It is engine.ThreadWorker
// only when the manifest asked for a worker and the browser started one; after
// a fallback it is engine.ThreadMain and ThreadFallbackReason says why.
func (c Context) Thread() engine.Thread {
	if stringProperty(c.value, "threadMode") == string(engine.ThreadWorker) {
		return engine.ThreadWorker
	}
	return engine.ThreadMain
}

// ThreadFallbackReason returns the reason code (for example
// "worker-create-failed") when a worker-thread engine fell back to the main
// thread, and the empty string otherwise.
func (c Context) ThreadFallbackReason() string {
	return stringProperty(c.value, "threadFallbackReason")
}

// Port is the engine's message port to the page. It exists only in a worker.
// Messages are plain data: JSON values and byte slices. Byte slices are
// copied into a fresh buffer and transferred, so the page receives them
// without a second copy.
type Port struct{ value js.Value }

// Message is one page-to-engine application message.
type Message struct {
	Name  string
	JSON  json.RawMessage // nil when the page sent no JSON
	Bytes []byte          // nil when the page sent no bytes
}

// ErrNoPort reports use of Port outside a worker-thread engine.
var ErrNoPort = errors.New("gosx engine/wasm: the engine port exists only in a worker-thread engine")

// Port returns the page port. ok is false on the main thread, including after
// a fallback, so code that supports both modes can branch once at startup.
func (c Context) Port() (port Port, ok bool) {
	if c.Thread() != engine.ThreadWorker {
		return Port{}, false
	}
	bridge := js.Global().Get("__gosx").Get("worker")
	if !bridge.Truthy() {
		return Port{}, false
	}
	return Port{value: bridge}, true
}

// Post sends name and the JSON encoding of data to the page. The page
// receives it through __gosx.engineWorker.get(id).onMessage(name, fn) and as a
// gosx:engine:message document event.
func (p Port) Post(name string, data any) error {
	encoded, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("encode engine message: %w", err)
	}
	p.value.Call("post", name, string(encoded))
	return nil
}

// PostBytes sends name and a copy of data to the page, transferring the copy.
func (p Port) PostBytes(name string, data []byte) error {
	buf := js.Global().Get("Uint8Array").New(len(data))
	js.CopyBytesToJS(buf, data)
	p.value.Call("postBytes", name, buf)
	return nil
}

// OnMessage calls handler for each message the page posts to this engine.
// Dispose the result to stop and to release the Go callback.
func (p Port) OnMessage(handler func(Message)) HandleFunc {
	if handler == nil {
		return func() {}
	}
	callback := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		msg := Message{Name: args[0].String()}
		if len(args) > 1 && args[1].Type() == js.TypeString {
			msg.JSON = json.RawMessage(args[1].String())
		}
		if len(args) > 2 && args[2].Truthy() {
			bytes := make([]byte, args[2].Length())
			js.CopyBytesToGo(bytes, args[2])
			msg.Bytes = bytes
		}
		handler(msg)
		return nil
	})
	unsubscribe := p.value.Call("onMessage", callback)
	return releaseOnce(callback, unsubscribe)
}

// Hidden reports whether the page is hidden. The page forwards its visibility,
// so this is correct inside a worker, where there is no document.
func (p Port) Hidden() bool { return p.value.Get("hidden").Truthy() }

// OnVisibility calls handler when the page is hidden or shown again.
func (p Port) OnVisibility(handler func(hidden bool)) HandleFunc {
	if handler == nil {
		return func() {}
	}
	callback := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 {
			handler(args[0].Truthy())
		}
		return nil
	})
	unsubscribe := p.value.Call("onVisibility", callback)
	return releaseOnce(callback, unsubscribe)
}

// SetFrameRate sets the worker's fixed frame rate in hertz (1 to 1000). The
// default is 60 or the engine prop "workerFrameHz".
func (p Port) SetFrameRate(hz float64) error {
	if math.IsNaN(hz) || hz < 1 || hz > 1000 {
		return fmt.Errorf("frame rate %v is outside 1..1000 Hz", hz)
	}
	if !p.value.Call("setFrameRate", hz).Truthy() {
		return fmt.Errorf("frame rate %v was refused", hz)
	}
	return nil
}

// FrameStats reports the worker clock's measured cadence.
type FrameStats struct {
	Hz           float64 `json:"hz"`
	Frames       int     `json:"frames"`
	LateMaxMS    float64 `json:"lateMaxMs"`
	GapMaxMS     float64 `json:"gapMaxMs"`
	GapAvgMS     float64 `json:"gapAvgMs"`
	SkippedSlots int     `json:"skippedSlots"`
}

// FrameStats returns the worker clock's cadence so far.
func (p Port) FrameStats() (stats FrameStats) {
	raw := js.Global().Get("JSON").Call("stringify", p.value.Call("frameStats"))
	if raw.Type() == js.TypeString {
		_ = json.Unmarshal([]byte(raw.String()), &stats)
	}
	return stats
}

// Now returns the worker's performance.now() in milliseconds.
func (p Port) Now() float64 { return p.value.Call("now").Float() }

func releaseOnce(callback js.Func, unsubscribe js.Value) HandleFunc {
	var once sync.Once
	return func() {
		once.Do(func() {
			if unsubscribe.Type() == js.TypeFunction {
				unsubscribe.Invoke()
			}
			callback.Release()
		})
	}
}
