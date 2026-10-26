package wasm

import (
	"encoding/json"
	"time"
)

// contextTransport adapts Context to callTransport.
type contextTransport struct{ ctx Context }

func (t contextTransport) subscribe(signal string, handler func(json.RawMessage)) (HandleFunc, error) {
	return t.ctx.subscribeSignalReplay(signal, handler)
}

func (t contextTransport) write(signal string, value any) error {
	return t.ctx.SetSignals(map[string]any{signal: value})
}

func (t contextTransport) declare(channel string, timeout time.Duration, recent int, zero string) {
	t.ctx.declareCalls(channel, timeout, recent, zero)
}
