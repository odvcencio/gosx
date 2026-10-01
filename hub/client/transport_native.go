//go:build !(js && wasm)

package hubclient

import (
	"net/http"

	"github.com/gorilla/websocket"
)

// newDefaultDialer returns the native transport, which dials with
// gorilla/websocket — the same library package hub's server side uses. This
// is the transport used by native tests and by any non-browser Go process
// (a bot, a headless test client, a dedicated native game client) that wants
// to speak the hub wire protocol without a browser.
//
// enableCompression offers permessage-deflate at handshake, matching a hub's
// own EnableCompression opt-in (see package hub). It is off by default,
// following gorilla/websocket.DefaultDialer: a native process that dials a
// hub without compression enabled connects uncompressed even if the hub
// itself would accept the extension, since negotiation needs both sides to
// offer it.
func newDefaultDialer(enableCompression bool) dialer {
	return nativeDialer{enableCompression: enableCompression}
}

type nativeDialer struct {
	enableCompression bool
}

func (d nativeDialer) Dial(rawURL string, header http.Header) (conn, error) {
	dialer := *websocket.DefaultDialer
	dialer.EnableCompression = d.enableCompression
	ws, _, err := dialer.Dial(rawURL, header)
	if err != nil {
		return nil, err
	}
	nc := &nativeConn{ws: ws, events: make(chan frameEvent, 16)}
	// gorilla's Dial is synchronous: a successful return means the connection
	// is already open, so frameOpen is queued before the read loop starts,
	// keeping it first on the channel as conn's contract requires.
	nc.events <- frameEvent{Kind: frameOpen}
	go nc.readLoop()
	return nc, nil
}

type nativeConn struct {
	ws     *websocket.Conn
	events chan frameEvent
}

func (c *nativeConn) Send(data []byte, binary bool) error {
	messageType := websocket.TextMessage
	if binary {
		messageType = websocket.BinaryMessage
	}
	return c.ws.WriteMessage(messageType, data)
}

func (c *nativeConn) Close() error {
	return c.ws.Close()
}

func (c *nativeConn) Events() <-chan frameEvent {
	return c.events
}

func (c *nativeConn) readLoop() {
	defer close(c.events)
	for {
		messageType, data, err := c.ws.ReadMessage()
		if err != nil {
			c.events <- frameEvent{Kind: frameClosed, Err: err}
			return
		}
		c.events <- frameEvent{Kind: frameMessage, Data: data, Binary: messageType == websocket.BinaryMessage}
	}
}
