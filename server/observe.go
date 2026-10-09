package server

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"time"
)

type observeContextKey string

const requestObservationContextKey observeContextKey = "gosx.observe"

// RequestEvent describes an observed framework request.
type RequestEvent struct {
	Request  *http.Request
	ID       string
	Method   string
	Path     string
	Pattern  string
	Kind     string
	Status   int
	Duration time.Duration
	// ResponseBytes counts body bytes accepted by the underlying writer,
	// including partial writes. App observers see compressed representation
	// bytes, excluding headers and bytes written on a hijacked connection.
	ResponseBytes int64
	Hijacked      bool
}

// RequestObserver records framework request events.
type RequestObserver interface {
	Observe(RequestEvent)
}

// RequestStartObserver optionally receives entry at the same wrapper that
// reports completion. ObserveRequestStart runs before application middleware;
// Observe still fires once on return. Callbacks must do bounded work and must
// not perform I/O. Implementing only RequestObserver remains supported.
type RequestStartObserver interface {
	RequestObserver
	ObserveRequestStart()
}

// RequestObserverFunc adapts a function into a request observer.
type RequestObserverFunc func(RequestEvent)

// Observe records a request event.
func (fn RequestObserverFunc) Observe(event RequestEvent) {
	if fn != nil {
		fn(event)
	}
}

type requestObservation struct {
	kind    string
	pattern string
}

// ObserveHandler wraps a handler with response capture and observer callbacks.
// Nested wrappers share route metadata. An observer explicitly installed at
// both scopes receives a notification from each scope.
func ObserveHandler(handler http.Handler, observers []RequestObserver) http.Handler {
	if handler == nil || len(observers) == 0 {
		return handler
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state, _ := r.Context().Value(requestObservationContextKey).(*requestObservation)
		if state == nil {
			state = &requestObservation{}
			r = r.WithContext(context.WithValue(r.Context(), requestObservationContextKey, state))
		}
		recorder := &observedResponseWriter{ResponseWriter: w}
		started := time.Now()
		for _, observer := range observers {
			if entry, ok := observer.(RequestStartObserver); ok {
				entry.ObserveRequestStart()
			}
		}
		handler.ServeHTTP(recorder, r)

		status := recorder.status
		if status == 0 && !recorder.hijacked {
			status = http.StatusOK
		}
		event := RequestEvent{
			Request:       r,
			ID:            recorder.Header().Get(requestIDHeader),
			Method:        r.Method,
			Path:          requestPath(r),
			Pattern:       state.pattern,
			Kind:          state.kind,
			Status:        status,
			Duration:      time.Since(started),
			ResponseBytes: recorder.bytes,
			Hijacked:      recorder.hijacked,
		}
		for _, observer := range observers {
			if observer != nil {
				observer.Observe(event)
			}
		}
	})
}

// MarkObservedRequest attaches route metadata to the current observed request.
func MarkObservedRequest(r *http.Request, kind, pattern string) {
	if r == nil {
		return
	}
	state, _ := r.Context().Value(requestObservationContextKey).(*requestObservation)
	if state == nil {
		return
	}
	state.kind = kind
	state.pattern = pattern
}

type observedResponseWriter struct {
	http.ResponseWriter
	status   int
	bytes    int64
	hijacked bool
}

func (w *observedResponseWriter) WriteHeader(status int) {
	if w.status != 0 || w.hijacked {
		return
	}
	w.ResponseWriter.WriteHeader(status)
	// 101 switches protocols and is terminal; other 1xx responses are interim.
	if status < 100 || status >= 200 || status == http.StatusSwitchingProtocols {
		w.status = status
	}
}

func (w *observedResponseWriter) Write(data []byte) (int, error) {
	if w.status == 0 && !w.hijacked {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(data)
	w.bytes += int64(n)
	return n, err
}

// ReadFrom preserves the underlying fast path after response commitment and
// accounts its accepted bytes.
func (w *observedResponseWriter) ReadFrom(r io.Reader) (int64, error) {
	if rf, ok := w.ResponseWriter.(io.ReaderFrom); ok && w.status != 0 {
		n, err := rf.ReadFrom(r)
		w.bytes += n
		return n, err
	}
	// Hide ReaderFrom so Copy routes every fallback write through Write.
	// Empty or failing reads leave the status unset until a write commits it.
	return io.Copy(struct{ io.Writer }{w}, r)
}

func (w *observedResponseWriter) Flush() { _ = w.FlushError() }

func (w *observedResponseWriter) FlushError() error {
	err := http.NewResponseController(w.ResponseWriter).Flush()
	// A supported flush commits headers before an I/O error can be returned.
	// Unsupported flushes leave the response uncommitted.
	if !errors.Is(err, http.ErrNotSupported) && w.status == 0 && !w.hijacked {
		w.status = http.StatusOK
	}
	return err
}

func (w *observedResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, rw, err := http.NewResponseController(w.ResponseWriter).Hijack()
	if err == nil {
		w.hijacked = true
	}
	return conn, rw, err
}

func (w *observedResponseWriter) Push(target string, opts *http.PushOptions) error {
	pusher, ok := w.ResponseWriter.(http.Pusher)
	if !ok {
		return http.ErrNotSupported
	}
	return pusher.Push(target, opts)
}

func (w *observedResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func requestPath(r *http.Request) string {
	if r == nil || r.URL == nil || r.URL.Path == "" {
		return "/"
	}
	return r.URL.Path
}
