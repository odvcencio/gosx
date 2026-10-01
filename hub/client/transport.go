package hubclient

import "net/http"

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
// Dial itself only fails for input the transport can reject synchronously
// (a malformed URL, for example). A rejected or dropped connection attempt
// is reported asynchronously as a frameError/frameClosed pair on the
// returned conn's Events channel, so Client's reconnect loop has one failure
// path regardless of platform.
type dialer interface {
	Dial(rawURL string, header http.Header) (conn, error)
}
