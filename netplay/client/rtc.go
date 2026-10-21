package client

import (
	"errors"

	"m31labs.dev/gosx/netplay/signal"
)

// errNoBrowser is returned where the WebRTC binding does not exist: outside a
// browser (GOOS=js, GOARCH=wasm), or when the page did not load the "netplay"
// runtime feature.
var errNoBrowser = errors.New("netplay/client: no browser WebRTC binding")

// Link states reported through rtcLink.Open's onState callback.
const (
	linkOpen   = "open"
	linkFailed = "failed"
	linkClosed = "closed"
)

// rtcConfig is what the browser binding needs to start a connection.
type rtcConfig struct {
	Session    string
	ICEServers []signal.ICEServer
}

// rtcLink is one browser data channel. The browser implementation wraps the
// GoSX runtime's window.__gosx.netplay binding; tests supply a fake.
type rtcLink interface {
	// Open starts the connection. onSignal receives each signaling message the
	// browser produces (offer, candidates); onState reports linkOpen,
	// linkFailed and linkClosed at most once each. Both may be called from
	// another goroutine.
	Open(cfg rtcConfig, onSignal func(event string, data []byte), onState func(state string)) error
	// Signal delivers a relay message (answer, candidate or failure).
	Signal(event string, data []byte)
	// Send writes one datagram; it reports false when the datagram was dropped.
	Send(p []byte) bool
	// Recv copies the next received datagram into buf without blocking.
	Recv(buf []byte) (n int, ok bool)
	Close()
}
