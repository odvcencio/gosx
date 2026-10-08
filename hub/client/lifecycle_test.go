package hubclient

import (
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

func awaitLifecycle(t *testing.T, done <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

type lifecycleDialer func() (conn, error)

func (d lifecycleDialer) Dial(string, http.Header) (conn, error) { return d() }

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
