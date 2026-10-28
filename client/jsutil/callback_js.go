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
// is queue.push, which stores (function, this, arguments) for Call to run
// later. Both are native functions, so the gate needs no eval, and no Go runs
// when the trap is queue.push.
type gate struct {
	once  sync.Once
	ready bool // Proxy and Reflect.apply exist

	proxy   js.Value // the Proxy constructor
	handler js.Value // shared by every Proxy; its apply trap is forward or push
	forward js.Value // Reflect.apply
	push    js.Value // queue.push, bound to queue
	queue   js.Value // (function, this, arguments) triples of the deferred calls

	mu     sync.Mutex
	active int // Calls whose JavaScript call has started and whose trap restore has not run
	nextID uint32
	byID   map[uint32]*Callback
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
		g.queue = global.Get("Array").New()
		g.push = g.queue.Get("push").Call("bind", g.queue)
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
//
// A recorded callback may call Call itself. The calls that inner Call records
// run before the inner Call returns. Callbacks made with js.FuncOf are not
// deferred and still enter Go inside the call.
func Call(target js.Value, method string, args ...any) js.Value {
	g := &theGate
	start, ok := g.begin()
	if !ok {
		return target.Call(method, args...)
	}
	defer g.end(start)
	return target.Call(method, args...)
}

// begin switches the apply trap to queue.push, unless no Callback is live. It
// returns the queue length at which this Call's recorded calls start. Calls
// that overlap or nest, which happens only when a function made with js.FuncOf
// enters Go inside a Call, record after the calls of the Call they are inside,
// so each Call runs only the calls from its own start on.
func (g *gate) begin() (start int, ok bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.byID) == 0 {
		return 0, false
	}
	g.active++
	if g.active == 1 {
		g.handler.Set("apply", g.push)
	} else {
		start = g.queue.Length()
	}
	return start, true
}

// end restores the trap when the last Call ends, then runs this Call's recorded
// calls. It runs when the JavaScript call panics too, so a failed call cannot
// leave the trap on queue.push, where every later call from JavaScript would be
// lost.
func (g *gate) end(start int) {
	g.mu.Lock()
	g.active--
	if g.active == 0 {
		g.handler.Set("apply", g.forward)
	}
	g.mu.Unlock()
	g.drain(start)
}

type deferredCall struct {
	id   uint32
	this js.Value
	args []js.Value
}

// drain runs the calls recorded from start on, until none is left. A callback
// that makes another Call has that Call's calls run first, and calls that arrive
// while a batch runs form the next batch.
func (g *gate) drain(start int) {
	for {
		n := g.queue.Length()
		if n-start < 3 {
			return
		}
		for _, call := range g.take(start, n) {
			g.mu.Lock()
			c := g.byID[call.id]
			g.mu.Unlock()
			if c != nil {
				c.fn(call.this, call.args)
			}
		}
	}
}

// take moves the recorded values from start up to n out of the queue.
func (g *gate) take(start, n int) []deferredCall {
	calls := make([]deferredCall, 0, (n-start)/3)
	for i := start; i+2 < n; i += 3 {
		function := g.queue.Index(i)
		arguments := g.queue.Index(i + 2)
		args := make([]js.Value, arguments.Length())
		for j := range args {
			args[j] = arguments.Index(j)
		}
		calls = append(calls, deferredCall{
			id:   uint32(function.Get(callbackKey).Int()),
			this: g.queue.Index(i + 1),
			args: args,
		})
	}
	g.queue.Set("length", start)
	return calls
}
