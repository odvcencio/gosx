//go:build js && wasm

package jsutil

import (
	"sync"
	"syscall/js"
)

// callbackKey is the property that holds a Callback's number on the function
// that enters Go. A deferred call records that function, and the number leads
// back to the Go function.
const callbackKey = "gosxCallback"

// Callback is a Go function that JavaScript can call, which never enters Go
// while Go is inside a Call. Hand Value to JavaScript where a js.Func would go,
// for example to addEventListener, and release it when it is no longer needed.
//
// JavaScript calls a Callback in one of two ways:
//
//   - Outside Call, for example from a click or a timer, the Callback enters Go
//     at once, as a js.Func does.
//   - Inside Call, while the JavaScript call that Call made is on the stack,
//     the Callback only records the call. Call runs it, in the order JavaScript
//     made the calls, on the goroutine that called Call, after the JavaScript
//     call has returned and before Call returns.
//
// A Callback returns no useful value to JavaScript, so it suits listeners and
// notifications, not functions whose result JavaScript uses. A deferred call
// runs after the JavaScript that made it has finished, so it cannot change
// that JavaScript, for example by calling preventDefault on an event that is
// already delivered.
//
// If the host has no Proxy, which every current browser has, Callback behaves
// as a js.Func and Call never defers.
type Callback struct {
	fn       func(this js.Value, args []js.Value)
	direct   js.Func  // enters Go
	value    js.Value // the Proxy that JavaScript calls
	id       uint32
	released bool
}

// gate is the state that every Callback of one Go program shares. It is the
// reason a Callback can be a plain function to JavaScript and still know when
// Go is inside a call.
//
// The Proxy of each Callback shares one handler, and JavaScript reads the
// handler's apply trap at every call. While Go is not inside a Call the trap is
// Reflect.apply, which runs the Go function. While Go is inside a Call the trap
// is the push function of that Call's frame, which stores (function, this,
// arguments) in the frame's queue for Call to run later. Both are native
// functions, so the gate needs no eval, and no Go runs when the trap is a push
// function.
//
// Each Call owns one frame. The JavaScript calls of different Calls form a stack
// (a call made inside another call is on top of it), so the trap points to the
// innermost frame, and a frame holds only the calls recorded during its own
// JavaScript call.
type gate struct {
	once  sync.Once
	ready bool // Proxy and Reflect.apply exist

	proxy   js.Value // the Proxy constructor
	handler js.Value // shared by every Proxy; its apply trap is forward or a frame's push
	forward js.Value // Reflect.apply
	array   js.Value // the Array constructor, for new frames

	mu     sync.Mutex
	frames []*frame // the Calls in progress, innermost last
	free   []*frame // empty frames, kept for the next Call
	nextID uint32
	byID   map[uint32]*Callback
}

// frame is the record of one Call.
type frame struct {
	queue js.Value // (function, this, arguments) triples of the deferred calls
	push  js.Value // queue.push, bound to queue
}

var theGate gate

func (g *gate) setup() {
	g.once.Do(func() {
		global := js.Global()
		proxy, reflect := global.Get("Proxy"), global.Get("Reflect")
		if proxy.Type() != js.TypeFunction || reflect.Type() != js.TypeObject || reflect.Get("apply").Type() != js.TypeFunction {
			return
		}
		g.proxy = proxy
		g.forward = reflect.Get("apply")
		g.handler = global.Get("Object").New()
		g.handler.Set("apply", g.forward)
		g.array = global.Get("Array")
		g.ready = true
	})
}

// NewCallback wraps fn. JavaScript passes the arguments of the call to fn, and
// the value of this that it called with. For an event listener, this is the
// element the listener is attached to.
func NewCallback(fn func(this js.Value, args []js.Value)) *Callback {
	if fn == nil {
		fn = func(js.Value, []js.Value) {}
	}
	g := &theGate
	g.setup()
	c := &Callback{fn: fn}
	c.direct = js.FuncOf(func(this js.Value, args []js.Value) any {
		fn(this, args)
		return nil
	})
	if !g.ready {
		c.value = c.direct.Value
		return c
	}
	g.mu.Lock()
	g.nextID++
	c.id = g.nextID
	if g.byID == nil {
		g.byID = map[uint32]*Callback{}
	}
	g.byID[c.id] = c
	g.mu.Unlock()
	c.direct.Set(callbackKey, c.id)
	c.value = g.proxy.New(c.direct, g.handler)
	return c
}

// Value returns the JavaScript function to hand to JavaScript. It is the same
// function every time, so removeEventListener can find it.
func (c *Callback) Value() js.Value { return c.value }

// Release frees the Go function. A call that JavaScript deferred and that Call
// has not yet run is dropped. Release is idempotent. Remove the function from
// whatever holds it first: JavaScript that calls a released Callback outside
// Call reaches a released js.Func, which logs "call to released function".
func (c *Callback) Release() {
	if c == nil {
		return
	}
	g := &theGate
	g.mu.Lock()
	if c.released {
		g.mu.Unlock()
		return
	}
	c.released = true
	delete(g.byID, c.id)
	g.mu.Unlock()
	c.direct.Release()
}

// Call calls target[method](args...) and returns the result, like
// js.Value.Call. While the JavaScript call runs, no Callback enters Go: each one
// that JavaScript calls only records the call. After the JavaScript call has
// returned, or has thrown, Call runs the calls it recorded, in order, and
// returns.
//
// Use Call for a call that makes JavaScript run callbacks before it returns,
// such as dispatchEvent, focus or click, or a function that notifies
// subscribers. Without Call, a Go callback runs inside the JavaScript call, and
// Go's js/wasm runtime can stop the program (see the package documentation).
// A property write can run callbacks too: a write that removes the node that has
// focus fires blur and focusout in Chrome. js.Value.Set is not a call, so make
// the write as Call(js.Global().Get("Reflect"), "set", target, name, value).
//
// A recorded callback may call Call itself. The calls that inner Call records
// run before the inner Call returns. Each Call runs only the calls recorded
// during its own JavaScript call, also when Calls from different goroutines
// overlap. Callbacks made with js.FuncOf are not deferred and still enter Go
// inside the call.
func Call(target js.Value, method string, args ...any) js.Value {
	g := &theGate
	f := g.begin()
	if f == nil {
		return target.Call(method, args...)
	}
	defer g.end(f)
	return target.Call(method, args...)
}

// begin gives the Call a frame and points the apply trap at it. It returns nil
// when no Callback is live, and the Call is then a plain call.
func (g *gate) begin() *frame {
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.byID) == 0 {
		return nil
	}
	var f *frame
	if n := len(g.free); n > 0 {
		f, g.free = g.free[n-1], g.free[:n-1]
	} else {
		queue := g.array.New()
		f = &frame{queue: queue, push: queue.Get("push").Call("bind", queue)}
	}
	g.frames = append(g.frames, f)
	g.handler.Set("apply", f.push)
	return f
}

// end takes the Call's frame off the stack, points the apply trap at the frame
// below it (or at Reflect.apply when there is none), and runs the calls the
// frame recorded. It runs when the JavaScript call panics too, so a failed call
// cannot leave the trap on a push function, where every later call from
// JavaScript would be lost.
//
// The trap goes back before the recorded calls run, not after. A recorded call
// that panics, or that waits for JavaScript (a listener that waits for a timer),
// would otherwise leave the trap on a push function while it runs or for good,
// and every call from JavaScript to a Callback in that time would be recorded
// and never run.
func (g *gate) end(f *frame) {
	g.mu.Lock()
	for i := len(g.frames) - 1; i >= 0; i-- {
		if g.frames[i] == f {
			g.frames = append(g.frames[:i], g.frames[i+1:]...)
			break
		}
	}
	if n := len(g.frames); n > 0 {
		g.handler.Set("apply", g.frames[n-1].push)
	} else {
		g.handler.Set("apply", g.forward)
	}
	g.mu.Unlock()

	// The frame is empty and unused once take returns, so it can serve the next
	// Call before the callbacks below run.
	calls := g.take(f)
	g.mu.Lock()
	g.free = append(g.free, f)
	g.mu.Unlock()

	for _, call := range calls {
		g.mu.Lock()
		c := g.byID[call.id]
		g.mu.Unlock()
		if c != nil {
			c.fn(call.this, call.args)
		}
	}
}

type deferredCall struct {
	id   uint32
	this js.Value
	args []js.Value
}

// take moves the calls out of the frame's queue and empties the queue.
func (g *gate) take(f *frame) []deferredCall {
	n := f.queue.Length()
	if n < 3 {
		return nil
	}
	calls := make([]deferredCall, 0, n/3)
	for i := 0; i+2 < n; i += 3 {
		function := f.queue.Index(i)
		arguments := f.queue.Index(i + 2)
		args := make([]js.Value, arguments.Length())
		for j := range args {
			args[j] = arguments.Index(j)
		}
		calls = append(calls, deferredCall{
			id:   uint32(function.Get(callbackKey).Int()),
			this: f.queue.Index(i + 1),
			args: args,
		})
	}
	f.queue.Set("length", 0)
	return calls
}
