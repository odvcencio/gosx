package hubclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Browser Dial returns before the socket opens. Close must own that pending
// socket and stop the pump without waiting for an asynchronous close event.
func TestCloseBeforeTransportOpens(t *testing.T) {
	cn := newPendingConn()
	c := New(Options{})
	c.dial = lifecycleDialer(func() (conn, error) { return cn, nil })
	c.Connect()
	awaitLifecycle(t, cn.pumping, "transport event pump")
	if err := c.Send("intent", nil); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("Send before open = %v, want ErrNotConnected", err)
	}
	if got := cn.sends.Load(); got != 0 {
		t.Fatalf("sent %d messages before open", got)
	}
	done := make(chan struct{})
	go func() { _ = c.Close(); close(done) }()
	awaitLifecycle(t, cn.closed, "pending socket closure")
	awaitLifecycle(t, done, "client disposal without a transport close event")
	if got := c.State(); got != StateClosed {
		t.Fatalf("State after Close = %v, want closed", got)
	}
	if err := c.Send("intent", nil); !errors.Is(err, ErrClosed) {
		t.Fatalf("Send after Close = %v, want ErrClosed", err)
	}
}

// Native Dial can return after Close starts. Its newly returned socket must
// be closed immediately, without publishing an open state or queued messages.
func TestCloseWhileTransportDialIsPending(t *testing.T) {
	cn := newPendingConn()
	cn.events <- frameEvent{Kind: frameOpen}
	cn.events <- frameEvent{Kind: frameMessage, Data: []byte(`{"event":"intent","data":{}}`)}
	var connected, delivered atomic.Int32
	c := New(Options{OnStateChange: func(s State) {
		if s == StateConnected {
			connected.Add(1)
		}
	}})
	c.On("intent", func(json.RawMessage) { delivered.Add(1) })
	dialing, finishDial := make(chan struct{}), make(chan struct{})
	c.dial = lifecycleDialer(func() (conn, error) {
		close(dialing)
		<-finishDial
		return cn, nil
	})
	c.Connect()
	awaitLifecycle(t, dialing, "pending dial")
	done := make(chan struct{})
	go func() { _ = c.Close(); close(done) }()
	awaitLifecycle(t, c.closeCh, "client close request")
	close(finishDial)
	awaitLifecycle(t, cn.closed, "late socket closure")
	awaitLifecycle(t, done, "client disposal after dial")
	if connected.Load() != 0 || delivered.Load() != 0 {
		t.Fatalf("disposed client published %d open states and %d messages", connected.Load(), delivered.Load())
	}
	if got := c.State(); got != StateClosed {
		t.Fatalf("State after Close = %v, want closed", got)
	}
}

func TestConcurrentConnectStartsOneLoop(t *testing.T) {
	cn := newPendingConn()
	var dials atomic.Int32
	c := New(Options{})
	c.dial = lifecycleDialer(func() (conn, error) {
		dials.Add(1)
		return cn, nil
	})
	var callers sync.WaitGroup
	for range 32 {
		callers.Go(c.Connect)
	}
	callers.Wait()
	awaitLifecycle(t, cn.pumping, "transport event pump")
	done := make(chan struct{})
	go func() { _ = c.Close(); close(done) }()
	awaitLifecycle(t, done, "client disposal")
	c.Connect()
	if got := dials.Load(); got != 1 {
		t.Fatalf("Dial called %d times, want one", got)
	}
}

func TestTransportStopUnblocksFullEventQueue(t *testing.T) {
	stream := newEventStream(1)
	stream.emit(frameEvent{Kind: frameOpen})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		if stream.emit(frameEvent{Kind: frameClosed}) {
			t.Error("closed transport delivered an event to a full queue")
		}
	}()
	stream.stop()
	stream.stop()
	awaitLifecycle(t, finished, "event producer cancellation")
}

func TestCloseFromMessageHandler(t *testing.T) {
	for _, typed := range []bool{false, true} {
		t.Run(map[bool]string{false: "raw", true: "typed"}[typed], func(t *testing.T) {
			cn := newPendingConn()
			cn.events <- frameEvent{Kind: frameOpen}
			cn.events <- frameEvent{Kind: frameMessage, Data: []byte(`{"event":"stop","data":{}}`)}
			c := New(Options{})
			c.dial = lifecycleDialer(func() (conn, error) { return cn, nil })
			returned := make(chan struct{})
			stop := func() {
				_ = c.Close()
				_ = c.Close() // Reentrant callers must also skip their own wait.
				close(returned)
			}
			if typed {
				On(c, "stop", func(struct{}) { stop() })
			} else {
				c.On("stop", func(json.RawMessage) { stop() })
			}
			c.Connect()
			awaitLifecycle(t, returned, "Close from a message handler")
			closeClient(t, c)
			awaitLifecycle(t, cn.closed, "handler's socket closure")
			if c.State() != StateClosed {
				t.Fatal("handler's client did not reach StateClosed")
			}
		})
	}
}

func TestCloseFromStateChangeCallback(t *testing.T) {
	for _, state := range []State{StateConnecting, StateConnected, StateReconnecting, StateClosed} {
		t.Run(state.String(), func(t *testing.T) {
			cn := newPendingConn()
			cn.events <- frameEvent{Kind: frameOpen}
			if state == StateReconnecting {
				close(cn.events)
			}
			returned := make(chan struct{})
			var calls atomic.Int32
			var c *Client
			c = New(Options{OnStateChange: func(s State) {
				if s == state {
					_ = c.Close()
					_ = c.Close()
					calls.Add(1)
					close(returned)
				}
			}})
			c.dial = lifecycleDialer(func() (conn, error) { return cn, nil })
			if state == StateClosed {
				// Exercise the final callback even without a preceding Connect.
				go c.Close()
			} else {
				c.Connect()
			}
			awaitLifecycle(t, returned, "Close from a state callback")
			closeClient(t, c)
			if c.State() != StateClosed || calls.Load() != 1 {
				t.Fatalf("state=%v callback calls=%d, want closed/1", c.State(), calls.Load())
			}
		})
	}
}

func TestCloseFromCallbackWithoutGoroutineID(t *testing.T) {
	for _, callback := range []string{"message", "state"} {
		t.Run(callback, func(t *testing.T) {
			cn := newPendingConn()
			cn.events <- frameEvent{Kind: frameOpen}
			cn.events <- frameEvent{Kind: frameMessage, Data: []byte(`{"event":"stop","data":{}}`)}
			returned := make(chan struct{})
			var c *Client
			stop := func() {
				c.mu.Lock()
				c.loopID = 0 // Simulate a runtime without goroutine identification.
				c.mu.Unlock()
				_ = c.Close()
				_ = c.Close()
				close(returned)
			}
			c = New(Options{OnStateChange: func(s State) {
				if callback == "state" && s == StateConnected {
					stop()
				}
			}})
			c.On("stop", func(json.RawMessage) {
				if callback == "message" {
					stop()
				}
			})
			c.dial = lifecycleDialer(func() (conn, error) { return cn, nil })
			c.Connect()
			awaitLifecycle(t, returned, "Close without goroutine identification")
			awaitLifecycle(t, c.done, "callback shutdown completion")
			closeClient(t, c)
			c.mu.Lock()
			depth := c.callbackDepth
			c.mu.Unlock()
			if depth != 0 || c.State() != StateClosed {
				t.Fatalf("callback depth=%d state=%v, want 0/closed", depth, c.State())
			}
		})
	}
}

func TestCloseConcurrentCallersWaitForLoop(t *testing.T) {
	dialing, finishDial := make(chan struct{}), make(chan struct{})
	closedCallback, finishCallback := make(chan struct{}), make(chan struct{})
	var dialOnce, callbackOnce sync.Once
	defer dialOnce.Do(func() { close(finishDial) })
	defer callbackOnce.Do(func() { close(finishCallback) })
	var closedStates atomic.Int32
	c := New(Options{OnStateChange: func(s State) {
		if s == StateClosed {
			closedStates.Add(1)
			close(closedCallback)
			<-finishCallback
		}
	}})
	c.dial = lifecycleDialer(func() (conn, error) {
		close(dialing)
		<-finishDial
		return nil, errors.New("dial stopped")
	})
	c.Connect()
	awaitLifecycle(t, dialing, "pending dial")
	first, second := make(chan struct{}), make(chan struct{})
	go func() { _ = c.Close(); close(first) }()
	awaitLifecycle(t, c.closeCh, "first close request")
	go func() { _ = c.Close(); close(second) }()
	select {
	case <-second:
		t.Fatal("concurrent Close returned while the loop was dialing")
	case <-time.After(30 * time.Millisecond):
	}
	dialOnce.Do(func() { close(finishDial) })
	awaitLifecycle(t, closedCallback, "final StateClosed callback")
	for _, done := range []<-chan struct{}{first, second} {
		select {
		case <-done:
			t.Fatal("Close returned before the final state callback finished")
		default:
		}
	}
	callbackOnce.Do(func() { close(finishCallback) })
	awaitLifecycle(t, first, "first Close completion")
	awaitLifecycle(t, second, "concurrent Close completion")
	if c.State() != StateClosed || closedStates.Load() != 1 {
		t.Fatalf("state=%v closed transitions=%d, want closed/1", c.State(), closedStates.Load())
	}
}

// Run Close off the test goroutine so a shutdown regression fails at this
// assertion instead of hanging in a deferred Close until the package timeout.
func closeClient(t *testing.T, c *Client) {
	t.Helper()
	done := make(chan struct{})
	var err error
	go func() { err = c.Close(); close(done) }()
	awaitLifecycle(t, done, "client shutdown")
	if err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func awaitLifecycle(t *testing.T, done <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

type lifecycleDialer func() (conn, error)

func (d lifecycleDialer) Dial(context.Context, string, http.Header) (conn, error) { return d() }

type pendingConn struct {
	events  chan frameEvent
	closed  chan struct{}
	pumping chan struct{}
	close   sync.Once
	pump    sync.Once
	sends   atomic.Int32
}

func newPendingConn() *pendingConn {
	return &pendingConn{events: make(chan frameEvent, 2), closed: make(chan struct{}), pumping: make(chan struct{})}
}

func (c *pendingConn) Send([]byte, bool) error { c.sends.Add(1); return nil }
func (c *pendingConn) Close() error            { c.close.Do(func() { close(c.closed) }); return nil }
func (c *pendingConn) Events() <-chan frameEvent {
	c.pump.Do(func() { close(c.pumping) })
	return c.events
}
