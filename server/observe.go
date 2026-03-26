package server

import (
	"context"
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
}

// RequestObserver records framework request events.
type RequestObserver interface {
	Observe(RequestEvent)
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

// ObserveHandler wraps a handler with status capture and observer callbacks.
func ObserveHandler(handler http.Handler, observers []RequestObserver) http.Handler {
	if handler == nil || len(observers) == 0 {
		return handler
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state := &requestObservation{}
		r = r.WithContext(context.WithValue(r.Context(), requestObservationContextKey, state))
		recorder := &observedResponseWriter{ResponseWriter: w}
		started := time.Now()
		handler.ServeHTTP(recorder, r)

		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}
		event := RequestEvent{
			Request:  r,
			ID:       recorder.Header().Get(requestIDHeader),
			Method:   r.Method,
			Path:     requestPath(r),
			Pattern:  state.pattern,
			Kind:     state.kind,
			Status:   status,
			Duration: time.Since(started),
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
	status int
}

func (w *observedResponseWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *observedResponseWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(data)
}

func requestPath(r *http.Request) string {
	if r == nil || r.URL == nil || r.URL.Path == "" {
		return "/"
	}
	return r.URL.Path
}
