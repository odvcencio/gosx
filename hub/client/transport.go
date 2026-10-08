package hubclient

import (
	"context"
	"net/http"
	"sync"
)

// frameKind identifies one event delivered on a conn's event channel.
type frameKind int

const (
	frameOpen frameKind = iota
	frameMessage
	frameError
	frameClosed
)

// frameEvent is one transport-level event. Exactly one of the frameKind-
// specific fields is meaningful for a given Kind.
type frameEvent struct {
	Kind   frameKind
	Data   []byte // frameMessage
	Binary bool   // frameMessage
	Err    error  // frameError, frameClosed (may be nil on a clean close)
}

// eventStream lets transports stop producing events when Close retires their
// consumer. A full queue must not trap a read loop or a browser close callback.
type eventStream struct {
	events   chan frameEvent
	stopped  chan struct{}
	stopOnce sync.Once
}

func newEventStream(size int) *eventStream {
	return &eventStream{events: make(chan frameEvent, size), stopped: make(chan struct{})}
}

func (s *eventStream) emit(event frameEvent) bool {
	select {
	case <-s.stopped:
		return false
	case s.events <- event:
		return true
	}
}

func (s *eventStream) stop() { s.stopOnce.Do(func() { close(s.stopped) }) }

func (s *eventStream) Events() <-chan frameEvent { return s.events }

// conn is one dialed transport connection. A conn's Events channel delivers
// exactly one frameOpen (if the connection reaches an open state at all)
// before any frameMessage, and is closed by the transport after it sends a
// terminal frameClosed. Send and Close are safe to call from any goroutine;
// the channel is only ever written to by the transport's own internal
// goroutine or callback.
type conn interface {
	Send(data []byte, binary bool) error
	Close() error
	Events() <-chan frameEvent
}

// dialer starts a connection to rawURL. Header carries additional request
// headers for transports that support them; the browser WebSocket transport
// ignores it, because the browser WebSocket API does not allow custom
// request headers.
//
// Dial observes ctx cancellation. Native Dial waits for the handshake and may
// return an error; browser Dial returns a pending socket and reports handshake
// failures asynchronously through its Events channel.
type dialer interface {
	Dial(ctx context.Context, rawURL string, header http.Header) (conn, error)
}
