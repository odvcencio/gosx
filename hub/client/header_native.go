//go:build !(js && wasm)

package hubclient

import "net/http"

// Header carries additional request headers for the WebSocket handshake. On
// native builds it is net/http's Header, so existing code compiles unchanged.
// In js/wasm builds it is a plain map[string][]string, so a browser client
// does not link net/http or crypto/tls.
type Header = http.Header
