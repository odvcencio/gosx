// Package hubclient is a Go/WASM client for a gosx hub connection.
//
// It is the browser-side counterpart to package hub: a game running as a
// Go/WASM client dials a hub endpoint, registers typed handlers that mirror
// the event names a server registers with Hub.On, and sends events back with
// Client.Send. The client reconnects automatically with exponential backoff,
// carries an application-assigned resume token across reconnects, and
// reports its connection state so a game can drive UI (for example
// "reconnecting") off it.
//
// The wire format is the same JSON envelope package hub already writes and
// reads: {"event": "...", "data": ...}. hubclient does not change or extend
// that protocol; resume support is a convention, not a hub feature: a
// non-empty ResumeToken is attached to the dial URL as a query parameter
// (Options.ResumeParam, default "resume"), the same way an application HTTP
// handler in front of Hub.ServeHTTPWithMetadata would read it to build
// hub.ConnectionMetadata before accepting the upgrade.
//
// The transport is platform-specific: under GOOS=js/GOARCH=wasm, Client
// drives the browser's WebSocket object directly via syscall/js. Everywhere
// else (including native tests) it dials with gorilla/websocket, the same
// library package hub's server side uses, so integration tests exercise the
// real wire protocol without a browser.
package hubclient
