package hubclient

import "encoding/json"

// wireMessage is the JSON envelope package hub reads and writes:
// {"event": "...", "data": ...}. It intentionally mirrors hub.Message; the
// two are not the same type because hubclient must not import package hub's
// server-only internals (Client, Hub) to stay usable from GOOS=js/wasm.
type wireMessage struct {
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data,omitempty"`
}

// State is the Client's current connection lifecycle state.
type State int

const (
	// StateDisconnected is the initial state before the first Dial call.
	StateDisconnected State = iota
	// StateConnecting means a dial attempt (initial or reconnect) is in flight.
	StateConnecting
	// StateConnected means the transport is open and messages can be sent.
	StateConnected
	// StateReconnecting means the transport dropped and a backoff-scheduled
	// retry is pending.
	StateReconnecting
	// StateClosed means Client.Close was called. The client will not
	// reconnect; a closed Client cannot be reused.
	StateClosed
)

// String returns a lowercase, hyphenated name for the state, suitable for
// logging or status UI.
func (s State) String() string {
	switch s {
	case StateDisconnected:
		return "disconnected"
	case StateConnecting:
		return "connecting"
	case StateConnected:
		return "connected"
	case StateReconnecting:
		return "reconnecting"
	case StateClosed:
		return "closed"
	default:
		return "unknown"
	}
}

// TokenStore persists a single resume token value. game/storage.Namespaced
// satisfies this interface; so does any other type with matching method
// signatures — hubclient does not import package storage to avoid coupling
// two independent, separately reusable packages.
type TokenStore interface {
	Get(key string) (string, bool)
	Set(key, value string) error
}
