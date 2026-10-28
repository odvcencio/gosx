package wasm

import (
	"encoding/json"
	"time"

	"m31labs.dev/gosx/engine"
)

// contextTransport adapts Context to callTransport.
type contextTransport struct {
	ctx Context
	// async is set for ServeCallsAsync, which answers from a goroutine. On the
	// main thread that goroutine must not write the result, or trim the mailbox,
	// from inside a call into JavaScript, because the write runs this engine's
	// own subscriptions before it returns: the browser makes the write from the
	// event loop instead (see Context.fromEventLoop). ServeCalls answers inside
	// the callback that delivered the call, on the stack of one entry, and writes
	// at once.
	async bool
}

func (t contextTransport) subscribe(signal string, handler func(json.RawMessage)) (HandleFunc, error) {
	return t.ctx.subscribeSignalReplay(signal, handler)
}

func (t contextTransport) write(signal string, value any) error {
	values := map[string]any{signal: value}
	if t.async && t.ctx.Thread() != engine.ThreadWorker {
		return t.ctx.setSignalsFromEventLoop(values)
	}
	return t.ctx.SetSignals(values)
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
	if t.async {
		return t.ctx.trimCallsFromEventLoop(signal, through)
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
