// Package socket provides a browser WebSocket transport without application
// envelopes, reconnect policy, queues, or HTTP dependencies.
package socket

import "errors"

type State uint8

const (
	Connecting State = iota
	Open
	Closing
	Closed
)

var (
	ErrNotOpen     = errors.New("socket: not open")
	ErrDisposed    = errors.New("socket: disposed")
	ErrUnsupported = errors.New("socket: browser transport unavailable")
)

// Message contains either Text or Binary, selected by IsBinary. Binary storage
// belongs to Socket and is valid only until the Message callback returns. Copy
// it before retaining it or handing it to another goroutine. Nested callbacks
// use separate storage, so reentry does not overwrite an active message.
type Message struct {
	Text     string
	Binary   []byte
	IsBinary bool
}

type CloseEvent struct {
	Code   int
	Reason string
	Clean  bool
}

// Callbacks run synchronously on the browser event callback. They may Send,
// Close, or Dispose the socket. Error does not imply closure: Closed reports
// the terminal event exactly once, unless Dispose has suppressed callbacks.
type Callbacks struct {
	Open    func()
	Message func(Message)
	Error   func(error)
	Closed  func(CloseEvent)
}
