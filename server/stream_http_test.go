package server

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/session"
)

// Exercise the wrapper stack, not just a writer that directly exposes
// FlushError. Each wrapper must preserve transport failure and deadlines.
func streamWrappedHandler(next http.Handler) http.Handler {
	manager := session.MustNew("stream-synthetic-test-secret", session.Options{})
	inner := manager.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session.Current(r).Set("synthetic", "cookie-commit-control")
		next.ServeHTTP(newInterceptResponseWriter(&observedResponseWriter{ResponseWriter: w}), r)
	}))
	return GzipMiddleware()(inner)
}

func TestStreamWrappedFlushErrorStopsAdmission(t *testing.T) {
	for _, encoding := range []string{"identity", "gzip"} {
		t.Run(encoding, func(t *testing.T) {
			var calls atomic.Int32
			r := NewDeferredRegistry()
			_ = r.SetStreamOptions(StreamOptions{OnError: quietStreamError})
			node := r.Defer(gosx.Text("pending"), func() (gosx.Node, error) {
				calls.Add(1)
				return gosx.Text("must not resolve"), nil
			})
			w := &streamControlledWriter{ResponseRecorder: httptest.NewRecorder(), failFlushAt: 1}
			req := httptest.NewRequest(http.MethodGet, "/stream", nil)
			req.Header.Set("Accept-Encoding", encoding)
			streamWrappedHandler(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				WriteHTML(w, HTMLResponse{Node: node, Deferred: r, Request: req})
			})).ServeHTTP(w, req)
			if calls.Load() != 0 || w.flushes != 1 {
				t.Fatalf("flush error swallowed: callbacks=%d transport flushes=%d", calls.Load(), w.flushes)
			}
			if len(w.Result().Cookies()) != 1 {
				t.Fatal("stream shell did not commit exactly one session cookie")
			}
		})
	}
}

func TestStreamWrappedBufferedWriteErrorStopsAdmission(t *testing.T) {
	for _, encoding := range []string{"identity", "gzip"} {
		t.Run(encoding, func(t *testing.T) {
			var calls atomic.Int32
			r := NewDeferredRegistry()
			_ = r.SetStreamOptions(StreamOptions{OnError: quietStreamError})
			node := r.Defer(gosx.Text("pending"), func() (gosx.Node, error) { calls.Add(1); return gosx.Text("late"), nil })
			w := &streamControlledWriter{ResponseRecorder: httptest.NewRecorder(), failWriteAt: 1}
			req := httptest.NewRequest(http.MethodGet, "/stream", nil)
			req.Header.Set("Accept-Encoding", encoding)
			streamWrappedHandler(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				WriteHTML(w, HTMLResponse{Node: node, Deferred: r, Request: req})
			})).ServeHTTP(w, req)
			if calls.Load() != 0 || w.writes != 1 || w.flushes != 0 {
				t.Fatalf("buffered write failure swallowed or retried: callbacks=%d writes=%d flushes=%d", calls.Load(), w.writes, w.flushes)
			}
		})
	}
}

type streamPlainWriter struct {
	header http.Header
	body   bytes.Buffer
}

func (w *streamPlainWriter) Header() http.Header         { return w.header }
func (w *streamPlainWriter) WriteHeader(int)             {}
func (w *streamPlainWriter) Write(b []byte) (int, error) { return w.body.Write(b) }

func TestStreamWrappedLegacyWriterWithoutFlushStillCompletes(t *testing.T) {
	for _, encoding := range []string{"identity", "gzip"} {
		t.Run(encoding, func(t *testing.T) {
			r := NewDeferredRegistry()
			node := r.Defer(gosx.Text("pending"), func() (gosx.Node, error) { return gosx.Text("COMPLETE-FRAGMENT"), nil })
			w := &streamPlainWriter{header: make(http.Header)}
			req := httptest.NewRequest(http.MethodGet, "/stream", nil)
			req.Header.Set("Accept-Encoding", encoding)
			streamWrappedHandler(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				WriteHTML(w, HTMLResponse{Node: gosx.Fragment(node, gosx.RawHTML(streamTailMarker+"TAIL")), Deferred: r, Request: req})
			})).ServeHTTP(w, req)
			body := w.body.Bytes()
			if encoding == "gzip" {
				reader, err := gzip.NewReader(bytes.NewReader(body))
				if err != nil {
					t.Fatal(err)
				}
				body, err = io.ReadAll(reader)
				reader.Close()
				if err != nil {
					t.Fatal(err)
				}
			}
			if !bytes.Contains(body, []byte("COMPLETE-FRAGMENT")) || !bytes.HasSuffix(body, []byte("TAIL")) || w.header.Get("Set-Cookie") == "" {
				t.Fatalf("legacy writer lost completion/cookie: headers=%v body=%q", w.header, body)
			}
		})
	}
}

func TestStreamWrappedCancellationInterruptsBlockedWrite(t *testing.T) {
	for _, encoding := range []string{"identity", "gzip"} {
		t.Run(encoding, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			r := NewDeferredRegistry()
			_ = r.SetStreamOptions(StreamOptions{OnError: quietStreamError})
			var calls atomic.Int32
			node := r.Defer(gosx.Text("pending"), func() (gosx.Node, error) { calls.Add(1); return gosx.Text("late"), nil })
			w := &streamDeadlineWriter{header: make(http.Header), started: make(chan struct{}), stop: make(chan struct{})}
			req := httptest.NewRequest(http.MethodGet, "/stream", nil).WithContext(ctx)
			req.Header.Set("Accept-Encoding", encoding)
			returned := make(chan struct{})
			go func() {
				defer close(returned)
				streamWrappedHandler(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
					WriteHTML(w, HTMLResponse{Node: node, Deferred: r, Request: req})
				})).ServeHTTP(w, req)
			}()
			awaitStreamSignal(t, w.started, "wrapped blocked output")
			cancel()
			awaitStreamSignal(t, returned, "wrapped deadline cancellation")
			if calls.Load() != 0 {
				t.Fatal("blocked shell launched work")
			}
		})
	}
}

func TestStreamWrappedTimeoutAbortsHTTPRepresentation(t *testing.T) {
	for _, encoding := range []string{"identity", "gzip"} {
		t.Run(encoding, func(t *testing.T) {
			finished := make(chan struct{})
			handler := streamWrappedHandler(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if req.URL.Path == "/healthy" {
					_, _ = io.WriteString(w, "complete")
					// The dispatcher normally commits non-streamed buffered output.
					_ = http.NewResponseController(w).Flush()
					return
				}
				defer close(finished)
				r := NewDeferredRegistry()
				_ = r.SetStreamOptions(StreamOptions{Timeout: 50 * time.Millisecond, OnError: quietStreamError})
				node := r.DeferContext(gosx.Text("pending"), func(ctx context.Context) (gosx.Node, error) {
					<-ctx.Done()
					return gosx.Node{}, ctx.Err()
				})
				cache := NewCacheState()
				cache.SetPolicy(PublicCache(time.Minute))
				WriteHTML(w, HTMLResponse{Node: gosx.Fragment(node, gosx.RawHTML(streamTailMarker+"COMPLETE-TAIL")), Deferred: r, Request: req, Cache: cache, Headers: make(http.Header)})
			}))
			srv := httptest.NewServer(handler)
			defer srv.Close()
			transport := &http.Transport{DisableCompression: true}
			defer transport.CloseIdleConnections()
			client := &http.Client{Transport: transport, Timeout: 2 * time.Second}
			req, _ := http.NewRequest(http.MethodGet, srv.URL+"/stream", nil)
			req.Header.Set("Accept-Encoding", encoding)
			res, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			body, readErr := io.ReadAll(res.Body)
			res.Body.Close()
			awaitStreamSignal(t, finished, "native HTTP timeout return")
			if !errors.Is(readErr, io.ErrUnexpectedEOF) {
				t.Fatalf("partial representation completed successfully: error=%v body=%q", readErr, body)
			}
			if res.Header.Get("ETag") != "" || strings.Contains(string(body), "COMPLETE-TAIL") {
				t.Fatalf("incomplete stream claimed completion/validator: headers=%v body=%q", res.Header, body)
			}
			var reused bool
			req, _ = http.NewRequest(http.MethodGet, srv.URL+"/healthy", nil)
			req = req.WithContext(httptrace.WithClientTrace(req.Context(), &httptrace.ClientTrace{GotConn: func(info httptrace.GotConnInfo) { reused = info.Reused }}))
			res, err = client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			body, readErr = io.ReadAll(res.Body)
			res.Body.Close()
			if readErr != nil || string(body) != "complete" || reused {
				t.Fatalf("following request reused incomplete response connection: reused=%v body=%q error=%v", reused, body, readErr)
			}
		})
	}
}

func TestStreamCompletedHTTPResponseKeepsConnectionReusable(t *testing.T) {
	srv := httptest.NewServer(streamWrappedHandler(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r := NewDeferredRegistry()
		node := r.Defer(gosx.Text("pending"), func() (gosx.Node, error) { return gosx.Text("COMPLETE"), nil })
		WriteHTML(w, HTMLResponse{Node: gosx.Fragment(node, gosx.RawHTML(streamTailMarker+"TAIL")), Deferred: r, Request: req})
	})))
	defer srv.Close()
	transport := &http.Transport{}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 2 * time.Second}
	for i := range 2 {
		var reused bool
		req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
		req = req.WithContext(httptrace.WithClientTrace(req.Context(), &httptrace.ClientTrace{GotConn: func(info httptrace.GotConnInfo) { reused = info.Reused }}))
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(res.Body)
		res.Body.Close()
		if err != nil || !bytes.Contains(body, []byte("COMPLETE")) || !bytes.HasSuffix(body, []byte("TAIL")) || (i == 1 && !reused) {
			t.Fatalf("completed stream damaged keepalive: reused=%v error=%v body=%q", reused, err, body)
		}
	}
}
