package server

import (
	"bufio"
	"compress/gzip"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/andybalholm/brotli"
	"m31labs.dev/gosx/internal/httpcompress"
)

const dynamicBrotliLevel = 4

var brotliWriterPool = sync.Pool{
	New: func() any { return brotli.NewWriterLevel(io.Discard, dynamicBrotliLevel) },
}

type responseCompressor interface {
	io.WriteCloser
	Flush() error
}

// CompressionMiddleware negotiates Brotli, then gzip, for text bodies of at
// least 1 KiB. Flush commits the decision using the bytes buffered so far.
// Small flushed prefixes use identity so streaming never waits for more data.
func CompressionMiddleware() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodHead {
				addAcceptEncodingVary(w.Header())
				next.ServeHTTP(w, r)
				return
			}
			if r.Header.Get("Range") != "" ||
				strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
				next.ServeHTTP(w, r)
				return
			}
			encoding := ""
			if requestAcceptsBrotli(r) {
				encoding = "br"
			} else if requestAcceptsGzip(r) {
				encoding = "gzip"
			}
			cw := &compressionWriter{ResponseWriter: w, encoding: encoding, identityRejected: !httpcompress.Accepts(r.Header.Get("Accept-Encoding"), "identity")}
			var writer http.ResponseWriter = cw
			_, flush := w.(http.Flusher)
			_, flushError := w.(interface{ FlushError() error })
			if flush || flushError {
				writer = &compressionFlushWriter{cw}
			}
			defer cw.finish()
			next.ServeHTTP(writer, r)
		})
	}
}

type compressionWriter struct {
	http.ResponseWriter
	encoding         string
	status           int
	headers          http.Header
	buffer           []byte
	started          bool
	compressor       responseCompressor
	identityRejected bool
	rejected         bool
	hijacked         bool
}

func (w *compressionWriter) WriteHeader(status int) {
	if w.status != 0 || w.started || w.hijacked {
		return
	}
	if status >= 100 && status < 200 && status != http.StatusSwitchingProtocols {
		w.ResponseWriter.WriteHeader(status)
		return
	}
	w.status = status
	w.headers = w.Header().Clone()
	if status == http.StatusNotModified {
		addAcceptEncodingVary(w.headers)
		if w.encoding != "" {
			weakenETag(w.headers)
		}
	}
	// These responses cannot acquire a compressed body later.
	if status == http.StatusSwitchingProtocols || status == http.StatusNoContent ||
		status == http.StatusNotModified || status == http.StatusPartialContent ||
		w.headers.Get("Content-Encoding") != "" {
		_ = w.start(false)
	}
}

func (w *compressionWriter) Write(data []byte) (int, error) {
	if w.hijacked {
		return 0, http.ErrHijacked
	}
	if w.rejected {
		return len(data), nil
	}
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	if w.started {
		return w.bodyWriter().Write(data)
	}
	n := min(len(data), httpcompress.MinimumSize-len(w.buffer))
	w.buffer = append(w.buffer, data[:n]...)
	if len(w.buffer) < httpcompress.MinimumSize {
		return n, nil
	}
	if err := w.start(true); err != nil {
		return 0, err
	}
	if w.rejected {
		return len(data), nil
	}
	written, err := w.bodyWriter().Write(data[n:])
	return n + written, err
}

func (w *compressionWriter) start(compress bool) error {
	if w.started {
		return nil
	}
	if w.status == 0 {
		w.status = http.StatusOK
		w.headers = w.Header().Clone()
	}
	h := w.headers
	if h.Get("Content-Type") == "" && len(w.buffer) > 0 {
		h.Set("Content-Type", http.DetectContentType(w.buffer))
	}
	eligible := w.status != http.StatusSwitchingProtocols && w.status != http.StatusNoContent &&
		w.status != http.StatusNotModified && w.status != http.StatusPartialContent &&
		h.Get("Content-Encoding") == "" && h.Get("Content-Range") == "" &&
		httpcompress.Compressible(h.Get("Content-Type"))
	if eligible {
		addAcceptEncodingVary(h)
	}
	if w.identityRejected && h.Get("Content-Encoding") == "" &&
		w.status != http.StatusSwitchingProtocols && w.status != http.StatusNoContent && w.status != http.StatusNotModified &&
		(len(w.buffer) > 0 || h.Get("Content-Type") != "") {
		if eligible && w.encoding != "" {
			compress = true
		} else {
			w.status = http.StatusNotAcceptable
			w.buffer = nil
			w.rejected = true
			h.Del("Content-Length")
			addAcceptEncodingVary(h)
		}
	}
	if compress && eligible && w.encoding != "" && !w.rejected {
		h.Del("Content-Length")
		h.Set("Content-Encoding", w.encoding)
		weakenETag(h)
		if w.encoding == "br" {
			br := brotliWriterPool.Get().(*brotli.Writer)
			br.Reset(w.ResponseWriter)
			w.compressor = br
		} else {
			gz := gzipWriterPool.Get().(*gzip.Writer)
			gz.Reset(w.ResponseWriter)
			w.compressor = gz
		}
	}
	// WriteHeader takes a snapshot even though we may delay sending it until
	// the size is known. Later handler mutations must not change that snapshot,
	// except trailers: net/http reads trailer values from the header map after
	// the handler returns, so values set after the first Write must survive.
	trailers := declaredTrailers(h)
	for key := range w.Header() {
		if isTrailerKey(key, trailers) {
			continue
		}
		delete(w.Header(), key)
	}
	for key, values := range h {
		if isTrailerKey(key, trailers) {
			if _, set := w.Header()[key]; set {
				continue
			}
		}
		w.Header()[key] = append([]string(nil), values...)
	}
	w.started = true
	w.ResponseWriter.WriteHeader(w.status)
	if len(w.buffer) > 0 {
		_, err := w.bodyWriter().Write(w.buffer)
		w.buffer = nil
		return err
	}
	return nil
}

func (w *compressionWriter) bodyWriter() io.Writer {
	if w.rejected {
		return io.Discard
	}
	if w.compressor != nil {
		return w.compressor
	}
	return w.ResponseWriter
}

func (w *compressionWriter) finish() {
	if w.hijacked {
		return
	}
	_ = w.start(len(w.buffer) >= httpcompress.MinimumSize)
	if w.compressor == nil {
		return
	}
	_ = w.compressor.Close()
	switch compressor := w.compressor.(type) {
	case *brotli.Writer:
		compressor.Reset(io.Discard)
		brotliWriterPool.Put(compressor)
	case *gzip.Writer:
		compressor.Reset(io.Discard)
		gzipWriterPool.Put(compressor)
	}
	w.compressor = nil
}

func (w *compressionWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *compressionWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	// A raw takeover without prior HTTP output must remain raw. Once the
	// handler has committed a response, send that output before handing over.
	if w.status != 0 || w.started {
		if err := w.start(false); err != nil {
			return nil, nil, err
		}
		w.finish()
		if err := http.NewResponseController(w.ResponseWriter).Flush(); err != nil && err != http.ErrNotSupported {
			return nil, nil, err
		}
	}
	conn, rw, err := http.NewResponseController(w.ResponseWriter).Hijack()
	if err == nil {
		w.hijacked = true
	}
	return conn, rw, err
}

type compressionFlushWriter struct{ *compressionWriter }

func (w *compressionFlushWriter) Flush() { _ = w.FlushError() }

func (w *compressionFlushWriter) FlushError() error {
	if err := w.start(len(w.buffer) >= httpcompress.MinimumSize); err != nil {
		return err
	}
	if w.compressor != nil {
		if err := w.compressor.Flush(); err != nil {
			return err
		}
	}
	return http.NewResponseController(w.ResponseWriter).Flush()
}

// declaredTrailers returns the canonical names listed in the Trailer header.
func declaredTrailers(h http.Header) map[string]bool {
	out := map[string]bool{}
	for _, value := range h.Values("Trailer") {
		for _, name := range strings.Split(value, ",") {
			if name = strings.TrimSpace(name); name != "" {
				out[http.CanonicalHeaderKey(name)] = true
			}
		}
	}
	return out
}

func isTrailerKey(key string, declared map[string]bool) bool {
	return strings.HasPrefix(key, http.TrailerPrefix) || declared[http.CanonicalHeaderKey(key)]
}

func addAcceptEncodingVary(h http.Header) {
	for _, value := range h.Values("Vary") {
		for _, token := range strings.Split(value, ",") {
			if token = strings.TrimSpace(token); token == "*" || strings.EqualFold(token, "Accept-Encoding") {
				return
			}
		}
	}
	h.Add("Vary", "Accept-Encoding")
}

func weakenETag(h http.Header) {
	if etag := h.Get("ETag"); etag != "" && !strings.HasPrefix(etag, "W/") {
		h.Set("ETag", "W/"+etag)
	}
}
