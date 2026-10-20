package server

import (
	"net/http"
	"os"
	"strings"

	"m31labs.dev/gosx/internal/httpcompress"
)

func canServeCompressedFile(r *http.Request) bool {
	return r.Method == http.MethodGet && r.Header.Get("Range") == "" &&
		!strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
}

// serveCompressedFile uses only sidecars accepted by the same public-file
// policy as the original. Old sidecars cannot replace a newer source file.
func serveCompressedFile(w http.ResponseWriter, r *http.Request, original string, sidecarPath func(string) (string, bool)) bool {
	if w.Header().Get("Content-Type") == "" {
		file, err := os.Open(original)
		if err != nil {
			return false
		}
		var prefix [512]byte
		n, _ := file.Read(prefix[:])
		_ = file.Close()
		w.Header().Set("Content-Type", http.DetectContentType(prefix[:n]))
	}
	if !httpcompress.Compressible(w.Header().Get("Content-Type")) || w.Header().Get("Content-Encoding") != "" {
		return false
	}
	addAcceptEncodingVary(w.Header())
	if !canServeCompressedFile(r) {
		return false
	}
	source, err := os.Stat(original)
	if err != nil || source.Size() < httpcompress.MinimumSize {
		return false
	}
	for _, encoding := range []string{"br", "gzip"} {
		if !requestAcceptsEncoding(r, encoding) {
			continue
		}
		ext := ".br"
		if encoding == "gzip" {
			ext = ".gz"
		}
		target, ok := sidecarPath(ext)
		if !ok {
			continue
		}
		info, err := os.Lstat(target)
		if err != nil || !info.Mode().IsRegular() || info.ModTime().Before(source.ModTime()) {
			continue
		}
		weakenETag(w.Header())
		http.ServeFile(&encodedContentWriter{ResponseWriter: w, encoding: encoding}, r, target)
		return true
	}
	return false
}

// encodedContentWriter delays the encoding header until ServeFile/ServeContent
// has evaluated preconditions. ServeContent computes the encoded body's length.
type encodedContentWriter struct {
	http.ResponseWriter
	encoding    string
	wroteHeader bool
}

func (w *encodedContentWriter) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	if status >= 100 && status < 200 {
		w.ResponseWriter.WriteHeader(status)
		return
	}
	w.wroteHeader = true
	if status == http.StatusOK || status == http.StatusPartialContent {
		w.Header().Set("Content-Encoding", w.encoding)
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *encodedContentWriter) Write(data []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(data)
}

func (w *encodedContentWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
