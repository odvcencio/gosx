// Package host is the transport-neutral half of an engine host: the part of
// an engine that publishes views to a page and answers its calls, with no
// knowledge of how the page is reached.
//
// A page reads an engine through shared signals. The signal names are the wire
// contract (see docs/engine-worker.md): an island writes "$channel" to call the
// engine, reads "$channel.result" for the reply, and reads the signals a View
// sets. This package implements the engine side of that contract once.
// Transports plug in below it:
//
//   - engine/wasm adapts a Go WASM engine running in a browser worker or on the
//     main thread (wasm.NewView, wasm.ServeCalls);
//   - desktop/enginehost adapts an engine running natively in a GoSX desktop
//     host process, over desktop IPC.
//
// An engine written against this package (a Publisher for state, ServeCalls for
// commands) runs behind either, and the page's islands do not change.
package host

import "errors"

// Handle owns one running engine instance. The transport calls Dispose
// exactly once when the instance is replaced or the page goes away.
type Handle interface {
	Dispose()
}

// HandleFunc adapts a function to Handle.
type HandleFunc func()

// Dispose calls f when it is non-nil.
func (f HandleFunc) Dispose() {
	if f != nil {
		f()
	}
}

// Context is what a transport gives an engine instance: the two channels to its
// page.
type Context interface {
	// ViewSink is where this instance's views are delivered.
	ViewSink() ViewSink
	// CallTransport carries this instance's calls and results.
	CallTransport() CallTransport
}

// Publisher receives the newest value of each named signal. *View implements
// it, so an engine can take a Publisher and not care which transport is below.
type Publisher interface {
	Set(signals map[string]any) error
}

var _ Publisher = (*View)(nil)

// ErrUnsupported reports a transport that is not available in this build.
var ErrUnsupported = errors.New("gosx engine/host transport is not available")
