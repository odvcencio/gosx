package server

import (
	"bufio"
	"compress/gzip"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
)

var gzipWriterPool = sync.Pool{
	New: func() any {
		w, _ := gzip.NewWriterLevel(io.Discard, gzip.DefaultCompression)
		return w
	},
}

// GzipMiddleware returns middleware that compresses responses with gzip when
// the client advertises support. It skips WebSocket upgrades and responses
// that already have Content-Encoding set. The wrapped writer implements
// http.Hijacker and http.Flusher so WebSocket upgrades and streaming work.
func GzipMiddleware() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") ||
				strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
				next.ServeHTTP(w, r)
				return
			}

			gz := gzipWriterPool.Get().(*gzip.Writer)
			gz.Reset(w)

			gw := &gzipWriter{
				Writer:         gz,
				ResponseWriter: w,
			}
			defer func() {
				// Only close if we actually wrote gzipped content.
				if gw.started && gw.writeErr == nil {
					gz.Close()
				}
				gzipWriterPool.Put(gz)
			}()

			next.ServeHTTP(gw, r)
		})
	}
}

type gzipWriter struct {
	io.Writer
	http.ResponseWriter
	started     bool
	headersSent bool
	passthrough bool
	writeErr    error
}

func (w *gzipWriter) WriteHeader(code int) {
	if w.headersSent {
		return
	}
	// Don't compress if upstream already set encoding (e.g., pre-compressed
	// static files). Record the decision so Write and Flush route bytes past
	// the gzip.Writer; otherwise the body is re-compressed under the upstream
	// Content-Encoding header and the client cannot decode it.
	if w.Header().Get("Content-Encoding") != "" {
		w.passthrough = true
		w.headersSent = true
		w.ResponseWriter.WriteHeader(code)
		return
	}
	w.Header().Del("Content-Length")
	w.Header().Set("Content-Encoding", "gzip")
	w.Header().Add("Vary", "Accept-Encoding")
	w.headersSent = true
	w.ResponseWriter.WriteHeader(code)
}

func (w *gzipWriter) Write(b []byte) (int, error) {
	if w.writeErr != nil {
		return 0, w.writeErr
	}
	if !w.headersSent {
		w.WriteHeader(http.StatusOK)
	}
	if w.passthrough {
		return w.ResponseWriter.Write(b)
	}
	w.started = true
	n, err := w.Writer.Write(b)
	w.writeErr = err
	return n, err
}

// Hijack supports WebSocket upgrades. If the underlying writer doesn't
// support hijacking, this returns an error and the caller falls back.
func (w *gzipWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := w.ResponseWriter.(http.Hijacker); ok {
		return hj.Hijack()
	}
	return nil, nil, http.ErrNotSupported
}

// Flush supports streaming responses (SSE, chunked transfer).
func (w *gzipWriter) Flush() {
	_ = w.FlushError()
}

func (w *gzipWriter) FlushError() error {
	if w.writeErr != nil {
		return w.writeErr
	}
	if !w.headersSent {
		w.WriteHeader(http.StatusOK)
	}
	if !w.passthrough {
		w.started = true
		if err := w.Writer.(*gzip.Writer).Flush(); err != nil {
			w.writeErr = err
			return err
		}
	}
	err := http.NewResponseController(w.ResponseWriter).Flush()
	if err != nil && !errors.Is(err, http.ErrNotSupported) {
		w.writeErr = err
	}
	return err
}

// Unwrap lets http.ResponseController find the underlying writer.
func (w *gzipWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
