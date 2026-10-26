package wasm

import (
	"m31labs.dev/gosx/engine/host"
)

// ViewOptions tunes NewView. See host.ViewOptions.
type ViewOptions = host.ViewOptions

const (
	// DefaultViewInterval is the default MinInterval.
	DefaultViewInterval = host.DefaultViewInterval
	// DefaultViewAckTimeout is the default AckTimeout.
	DefaultViewAckTimeout = host.DefaultViewAckTimeout
)

// View sends the engine's state to the page's shared signals at a pace the
// page can follow. It is host.View behind the engine context; the pacing,
// acknowledgement and change-only rules are documented there.
type View = host.View

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

// NewView creates a View for the engine instance.
func NewView(ctx Context, opts ViewOptions) *View {
	return newView(ctx.viewSink(), opts)
}

func newView(sink viewSink, opts ViewOptions) *View {
	return host.NewViewOn(viewSinkAdapter{sink}, opts)
}

// viewSinkAdapter presents a viewSink as a host.ViewSink.
type viewSinkAdapter struct{ s viewSink }

func (a viewSinkAdapter) Send(batch []byte, done func(bool)) error { return a.s.send(batch, done) }
func (a viewSinkAdapter) Hidden() bool                             { return a.s.hidden() }
func (a viewSinkAdapter) OnVisibility(fn func()) HandleFunc        { return a.s.onVisibility(fn) }
func (a viewSinkAdapter) Release()                                 { a.s.release() }
