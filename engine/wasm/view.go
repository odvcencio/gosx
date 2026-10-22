package wasm

import (
	"bytes"
	"encoding/json"
	"errors"
	"sync"
	"time"
)

// ViewOptions tunes NewView.
type ViewOptions struct {
	// MinInterval is the shortest time between two batches. Zero means
	// DefaultViewInterval (10 Hz).
	MinInterval time.Duration
	// AckTimeout is how long the view waits for the page to confirm a batch
	// before it sends the next one anyway. It keeps a lost acknowledgement from
	// freezing the view. Zero means DefaultViewAckTimeout.
	AckTimeout time.Duration
}

const (
	// DefaultViewInterval is the default MinInterval.
	DefaultViewInterval = 100 * time.Millisecond
	// DefaultViewAckTimeout is the default AckTimeout.
	DefaultViewAckTimeout = 2 * time.Second
)

// viewSink delivers one batch to the page. Context implements it on js/wasm.
type viewSink interface {
	// send delivers an encoded {"$name": value, ...} batch. It calls done once
	// the page has handled it: on a worker after the page's acknowledgement, on
	// the main thread at once. done(false) means the page rejected the batch.
	send(batch []byte, done func(ok bool)) error
	hidden() bool
	// onVisibility calls fn when the page is hidden or shown.
	onVisibility(fn func()) HandleFunc
	release()
}

// View sends the engine's state to the page's shared signals at a pace the
// page can follow. It keeps only the newest value for each signal and sends a
// batch when two things hold: at least MinInterval has passed since the last
// batch, and the page has applied the previous one. A simulation can call Set
// every tick without flooding the page. While the page is hidden nothing is
// sent and nothing blocks; the next batch after the page returns carries the
// newest state.
//
// View sends only signals whose JSON changed since the last batch. Use it for
// state the engine owns. For one-off events use Context.SetSignals.
//
// Keep each signal small and separate (one per island panel). The page re-renders
// every reader of a signal that changes.
type View struct {
	sink viewSink
	opts ViewOptions

	mu       sync.Mutex
	pending  map[string]json.RawMessage
	sent     map[string]json.RawMessage
	lastSend time.Time
	inFlight uint64                     // token of the unacknowledged batch; 0 when none
	batch    map[string]json.RawMessage // the unacknowledged batch
	token    uint64
	timer    *time.Timer
	ackTimer *time.Timer
	visOff   HandleFunc
	disposed bool
}

// NewView creates a View for the engine instance.
func NewView(ctx Context, opts ViewOptions) *View {
	return newView(ctx.viewSink(), opts)
}

func newView(sink viewSink, opts ViewOptions) *View {
	if opts.MinInterval <= 0 {
		opts.MinInterval = DefaultViewInterval
	}
	if opts.AckTimeout <= 0 {
		opts.AckTimeout = DefaultViewAckTimeout
	}
	v := &View{sink: sink, opts: opts, pending: map[string]json.RawMessage{}, sent: map[string]json.RawMessage{}}
	v.visOff = sink.onVisibility(v.kick)
	return v
}

// Set records the newest value of each named signal. Names are shared signal
// names with their "$" prefix, as in Context.SetSignals. Values are encoded at
// once, so the caller may reuse them. A value that cannot be encoded is
// skipped and reported in the returned error; the others are kept.
func (v *View) Set(signals map[string]any) error {
	var failed error
	encoded := make(map[string]json.RawMessage, len(signals))
	for name, value := range signals {
		data, err := json.Marshal(value)
		if err != nil {
			failed = errors.Join(failed, err)
			continue
		}
		encoded[name] = data
	}
	v.mu.Lock()
	if v.disposed {
		v.mu.Unlock()
		return nil
	}
	for name, data := range encoded {
		v.pending[name] = data
	}
	v.mu.Unlock()
	v.kick()
	return failed
}

// Dispose stops the view and drops unsent state.
func (v *View) Dispose() {
	v.mu.Lock()
	if v.disposed {
		v.mu.Unlock()
		return
	}
	v.disposed = true
	v.pending = nil
	stopTimer(&v.timer)
	stopTimer(&v.ackTimer)
	off := v.visOff
	v.mu.Unlock()
	off.Dispose()
	v.sink.release()
}

func stopTimer(t **time.Timer) {
	if *t != nil {
		(*t).Stop()
		*t = nil
	}
}

// kick sends the pending batch if pacing, credit and visibility allow.
func (v *View) kick() {
	v.mu.Lock()
	if v.disposed || len(v.pending) == 0 || v.inFlight != 0 || v.sink.hidden() {
		v.mu.Unlock()
		return
	}
	if wait := v.opts.MinInterval - time.Since(v.lastSend); wait > 0 {
		if v.timer == nil {
			v.timer = time.AfterFunc(wait, func() {
				v.mu.Lock()
				v.timer = nil
				v.mu.Unlock()
				v.kick()
			})
		}
		v.mu.Unlock()
		return
	}
	stopTimer(&v.timer)
	batch := map[string]json.RawMessage{}
	for name, data := range v.pending {
		if prev, ok := v.sent[name]; ok && bytes.Equal(prev, data) {
			continue
		}
		batch[name] = data
	}
	v.pending = map[string]json.RawMessage{}
	if len(batch) == 0 {
		v.mu.Unlock()
		return
	}
	encoded, err := json.Marshal(batch)
	if err != nil {
		v.mu.Unlock()
		return
	}
	for name, data := range batch {
		v.sent[name] = data
	}
	v.token++
	token := v.token
	v.inFlight = token
	v.batch = batch
	v.lastSend = time.Now()
	v.ackTimer = time.AfterFunc(v.opts.AckTimeout, func() { v.acknowledge(token, true) })
	v.mu.Unlock()

	if err := v.sink.send(encoded, func(ok bool) { v.acknowledge(token, ok) }); err != nil {
		v.acknowledge(token, false)
	}
}

// acknowledge returns the credit for the batch with token and sends the next.
// A rejected batch goes back into the pending state, except for names that
// have a newer value, and is retried after MinInterval.
func (v *View) acknowledge(token uint64, ok bool) {
	v.mu.Lock()
	if v.inFlight != token {
		v.mu.Unlock()
		return
	}
	v.inFlight = 0
	stopTimer(&v.ackTimer)
	if !ok && !v.disposed {
		for name, data := range v.batch {
			delete(v.sent, name)
			if _, newer := v.pending[name]; !newer {
				v.pending[name] = data
			}
		}
	}
	v.batch = nil
	v.mu.Unlock()
	v.kick()
}
