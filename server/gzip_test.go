package server

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/andybalholm/brotli"
)

func TestGzipMiddlewarePassesThroughPreEncodedResponses(t *testing.T) {
	raw := bytes.Repeat([]byte("framework runtime payload "), 16)
	encoded := map[string][]byte{
		"br":   encodeBrotliResponse(t, raw),
		"gzip": encodeGzipResponse(t, raw),
	}

	for encoding, payload := range encoded {
		for _, explicitHeader := range []bool{false, true} {
			name := encoding + "/implicit-write"
			wantStatus := http.StatusOK
			if explicitHeader {
				name = encoding + "/explicit-header-write-flush"
				wantStatus = http.StatusPartialContent
			}
			t.Run(name, func(t *testing.T) {
				handler := GzipMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Encoding", encoding)
					w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
					if explicitHeader {
						w.WriteHeader(http.StatusPartialContent)
					}
					if _, err := w.Write(payload); err != nil {
						t.Errorf("write pre-encoded response: %v", err)
					}
					w.(http.Flusher).Flush()
				}))

				req := httptest.NewRequest(http.MethodGet, "/asset", nil)
				req.Header.Set("Accept-Encoding", "br, gzip")
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)

				if rec.Code != wantStatus {
					t.Fatalf("status = %d, want %d", rec.Code, wantStatus)
				}
				if got := rec.Header().Get("Content-Encoding"); got != encoding {
					t.Fatalf("Content-Encoding = %q, want %q", got, encoding)
				}
				if got := rec.Header().Get("Content-Length"); got != strconv.Itoa(len(payload)) {
					t.Fatalf("Content-Length = %q, want %d", got, len(payload))
				}
				if !rec.Flushed {
					t.Fatal("underlying response was not flushed")
				}
				if !bytes.Equal(rec.Body.Bytes(), payload) {
					t.Fatalf("pre-encoded %s payload was transformed", encoding)
				}
			})
		}
	}
}

func TestGzipMiddlewareCompressesDynamicHTML(t *testing.T) {
	raw := bytes.Repeat([]byte("<p>dynamic page</p>"), 32)
	handler := GzipMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Length", strconv.Itoa(len(raw)))
		if _, err := w.Write(raw); err != nil {
			t.Errorf("write HTML response: %v", err)
		}
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}
	if got := rec.Header().Get("Content-Length"); got != "" {
		t.Fatalf("compressed Content-Length = %q, want empty", got)
	}
	if got := rec.Header().Values("Vary"); len(got) != 1 || got[0] != "Accept-Encoding" {
		t.Fatalf("Vary = %q, want Accept-Encoding", got)
	}
	if got := decodeGzipResponse(t, rec.Body.Bytes()); !bytes.Equal(got, raw) {
		t.Fatalf("decoded HTML mismatch: got %d bytes, want %d", len(got), len(raw))
	}
}

func TestGzipMiddlewareStreamsValidGzip(t *testing.T) {
	handler := GzipMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		flusher.Flush()
		_, _ = io.WriteString(w, "first chunk")
		flusher.Flush()
		_, _ = io.WriteString(w, " second chunk")
		flusher.Flush()
	}))

	req := httptest.NewRequest(http.MethodGet, "/stream", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !rec.Flushed {
		t.Fatal("underlying response was not flushed")
	}
	if got := decodeGzipResponse(t, rec.Body.Bytes()); string(got) != "first chunk second chunk" {
		t.Fatalf("decoded stream = %q", got)
	}
}

func encodeGzipResponse(t *testing.T, raw []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func encodeBrotliResponse(t *testing.T, raw []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := brotli.NewWriter(&buf)
	if _, err := w.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func decodeGzipResponse(t *testing.T, encoded []byte) []byte {
	t.Helper()
	r, err := gzip.NewReader(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("open gzip response: %v", err)
	}
	decoded, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read gzip response: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("close gzip response: %v", err)
	}
	return decoded
}
