package main

import (
	"errors"
	"sync"
	"time"

	"m31labs.dev/gosx/engine/host"
)

// Names of the engine's wire contract: the component the page's engine.Config
// names, the view signal it publishes and the call channel it serves. The
// island in panel.gsx reads and writes the same names.
const (
	component   = "CounterEngine"
	mountID     = "counter"
	viewSignal  = "$counter.view"
	callChannel = "counter.cmd"
)

// View is what the engine publishes. Its field names are the signal's keys.
type View struct {
	Count  int    // the total of every increment so far
	Ticks  int    // the engine's own clock, advancing 20 times a second
	Source string // where the engine runs
}

// IncArgs are the arguments of the "increment" call.
type IncArgs struct{ By int }

// Total is the reply to a call.
type Total struct{ Total int }

// Counter is a small native engine. It knows nothing about the transport: it
// publishes through a host.Publisher and its calls are plain methods, so the
// same type can sit behind a WASM worker as well as behind desktop IPC.
type Counter struct {
	mu    sync.Mutex
	count int
	ticks int
	pub   host.Publisher

	stop chan struct{}
	done chan struct{}
}

// NewCounter starts the engine's clock. It publishes the first view at once and
// a new one every tick; the view's pacing keeps the page from being flooded.
func NewCounter(pub host.Publisher, tick time.Duration) *Counter {
	c := &Counter{pub: pub, stop: make(chan struct{}), done: make(chan struct{})}
	c.publish()
	go c.run(tick)
	return c
}

func (c *Counter) run(tick time.Duration) {
	defer close(c.done)
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		select {
		case <-c.stop:
			return
		case <-t.C:
			c.mu.Lock()
			c.ticks++
			c.mu.Unlock()
			c.publish()
		}
	}
}

func (c *Counter) publish() {
	c.mu.Lock()
	v := View{Count: c.count, Ticks: c.ticks, Source: "native"}
	c.mu.Unlock()
	_ = c.pub.Set(map[string]any{viewSignal: v})
}

// Call serves the page's calls.
func (c *Counter) Call(name string, args IncArgs) (Total, error) {
	switch name {
	case "increment":
		by := args.By
		if by == 0 {
			by = 1
		}
		c.mu.Lock()
		c.count += by
		total := c.count
		c.mu.Unlock()
		c.publish()
		return Total{Total: total}, nil
	case "refuse":
		return Total{}, errors.New("refused")
	}
	return Total{}, errors.New("unknown call " + name)
}

// Dispose stops the clock and waits for it.
func (c *Counter) Dispose() {
	select {
	case <-c.stop:
	default:
		close(c.stop)
	}
	<-c.done
}

// Mount builds the engine for one page. It takes a host.Context, so it works
// behind any transport that provides one; desktop IPC does here.
func Mount(in host.Context, tick time.Duration) (host.Handle, error) {
	view := host.NewView(in, host.ViewOptions{MinInterval: 100 * time.Millisecond})
	c := NewCounter(view, tick)
	off, err := host.ServeCalls(in, callChannel, host.CallOptions{Timeout: 5 * time.Second}, c.Call)
	if err != nil {
		c.Dispose()
		view.Dispose()
		return nil, err
	}
	return host.HandleFunc(func() {
		off.Dispose()
		c.Dispose()
		view.Dispose()
	}), nil
}
