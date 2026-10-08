//go:build !(js && wasm)

package hubclient

import (
	"context"
	"net"
	"net/http"
	"sync"
	"time"

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

func (d nativeDialer) Dial(ctx context.Context, rawURL string, header http.Header) (conn, error) {
	dialer := *websocket.DefaultDialer
	dialer.EnableCompression = d.enableCompression
	// Gorilla observes cancellation while dialing TCP/TLS, but proxy CONNECT
	// and HTTP upgrade reads only observe deadlines. Watch each socket from
	// creation so cancellation closes it during either handshake as well.
	var stopCancel func() bool
	watchDial := func(dial func(context.Context, string, string) (net.Conn, error)) func(context.Context, string, string) (net.Conn, error) {
		return func(dialCtx context.Context, network, addr string) (net.Conn, error) {
			cn, err := dial(dialCtx, network, addr)
			if err != nil {
				return nil, err
			}
			stopCancel = context.AfterFunc(ctx, func() { _ = cn.Close() })
			return cn, nil
		}
	}
	dialTCP := dialer.NetDialContext
	if dialTCP == nil {
		if dialer.NetDial != nil {
			dialTCP = func(_ context.Context, network, addr string) (net.Conn, error) {
				return dialer.NetDial(network, addr)
			}
		} else {
			dialTCP = (&net.Dialer{}).DialContext
		}
	}
	dialer.NetDialContext = watchDial(dialTCP)
	if dialer.NetDialTLSContext != nil {
		dialer.NetDialTLSContext = watchDial(dialer.NetDialTLSContext)
	}
	defer func() {
		if stopCancel != nil {
			stopCancel()
		}
	}()
	ws, _, err := dialer.DialContext(ctx, rawURL, header)
	if err != nil {
		return nil, err
	}
	nc := &nativeConn{ws: ws, eventStream: newEventStream(16)}
	// gorilla's Dial is synchronous: a successful return means the connection
	// is already open, so frameOpen is queued before the read loop starts,
	// keeping it first on the channel as conn's contract requires.
	nc.emit(frameEvent{Kind: frameOpen})
	go nc.readLoop()
	return nc, nil
}

type nativeConn struct {
	*eventStream
	ws        *websocket.Conn
	closeOnce sync.Once
	closeErr  error
}

func (c *nativeConn) Send(data []byte, binary bool) error {
	messageType := websocket.TextMessage
	if binary {
		messageType = websocket.BinaryMessage
	}
	return c.ws.WriteMessage(messageType, data)
}

func (c *nativeConn) Close() error {
	c.closeOnce.Do(func() {
		c.stop()
		// WriteControl may run concurrently with Send and the read loop. Keep
		// shutdown bounded even if a peer stops reading or Send holds the writer.
		_ = c.ws.WriteControl(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
			time.Now().Add(100*time.Millisecond))
		c.closeErr = c.ws.Close()
	})
	return c.closeErr
}

func (c *nativeConn) readLoop() {
	defer close(c.events)
	for {
		messageType, data, err := c.ws.ReadMessage()
		if err != nil {
			c.emit(frameEvent{Kind: frameClosed, Err: err})
			return
		}
		if !c.emit(frameEvent{Kind: frameMessage, Data: data, Binary: messageType == websocket.BinaryMessage}) {
			return
		}
	}
}
