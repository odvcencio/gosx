package wasm

import (
	"encoding/json"
	"time"

	"m31labs.dev/gosx/engine"
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

// trimCalls drops the answered calls (ID at or below through) from a mailbox in
// the call signal. Only an engine on the main thread does this: it reads and
// writes the page's store in one step, so no island write can slip in between.
// A worker's copy of the signal lags the page, so the page host trims for it.
func (t contextTransport) trimCalls(signal string, through uint64) error {
	if t.ctx.Thread() == engine.ThreadWorker {
		return nil
	}
	raw, err := t.ctx.SignalJSON(signal)
	if err != nil {
		return nil // no value yet: nothing to trim
	}
	var calls []json.RawMessage
	if json.Unmarshal(raw, &calls) != nil {
		return nil // a single call, not a mailbox
	}
	// An entry with no usable ID can never be answered, so it leaves with the
	// answered calls.
	kept := make([]json.RawMessage, 0, len(calls))
	for _, call := range calls {
		var head struct{ ID uint64 }
		if json.Unmarshal(call, &head) == nil && head.ID > through {
			kept = append(kept, call)
		}
	}
	if len(kept) == len(calls) {
		return nil
	}
	return t.ctx.SetSignal(signal, kept)
}
