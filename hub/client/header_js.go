//go:build js && wasm

package hubclient

// Header carries additional request headers. The browser transport ignores
// it (the browser WebSocket API sets no custom headers). It is a plain map
// here so a browser client does not link net/http or crypto/tls; on native
// builds it is net/http's Header.
type Header = map[string][]string
