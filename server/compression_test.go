package server

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/andybalholm/brotli"
	"m31labs.dev/gosx"
)

func decodeCompressedResponse(t *testing.T, encoding string, data []byte) []byte {
	t.Helper()
	var reader io.Reader = bytes.NewReader(data)
	switch encoding {
	case "br":
		reader = brotli.NewReader(reader)
	case "gzip":
		gz, err := gzip.NewReader(reader)
		if err != nil {
			t.Fatal(err)
		}
		defer gz.Close()
		reader = gz
	}
	body, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestCompressionNegotiation(t *testing.T) {
	raw := bytes.Repeat([]byte("<p>response text</p>"), 128)
	for _, tc := range []struct{ accept, want string }{
		{"br, gzip", "br"}, {"gzip", "gzip"}, {"br", "br"},
		{"br;q=0, gzip", "gzip"}, {"br;q=0, gzip;q=0", ""},
		{"identity", ""}, {"", ""}, {"xgzip, zebra", ""},
		{"BR; Q=0.5, gzip;q=1", "br"}, {"*", "br"},
		{"*;q=1, br;q=0", "gzip"},
		{"br;q=invalid, gzip;q=0.8", "gzip"}, {"br;q=NaN, gzip;q=2", ""},
	} {
		t.Run(tc.accept, func(t *testing.T) {
			handler := CompressionMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.Header().Set("Content-Length", strconv.Itoa(len(raw)))
				_, _ = w.Write(raw[:600])
				_, _ = w.Write(raw[600:])
			}))
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.Header.Set("Accept-Encoding", tc.accept)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if got := w.Result().Header.Get("Content-Encoding"); got != tc.want {
				t.Fatalf("encoding = %q, want %q", got, tc.want)
			}
			if got := decodeCompressedResponse(t, tc.want, w.Body.Bytes()); !bytes.Equal(got, raw) {
				t.Fatal("decoded response changed")
			}
			if tc.want != "" && w.Result().Header.Get("Content-Length") != "" {
				t.Fatal("compressed response retained identity length")
			}
			if w.Result().Header.Get("Vary") != "Accept-Encoding" {
				t.Fatal("identity and compressed responses must vary by encoding")
			}
		})
	}
}

func TestCompressionSkips(t *testing.T) {
	for _, tc := range []struct {
		name, contentType, method, header, value string
		status, size                             int
	}{
		{name: "small", size: 1023}, {name: "empty", size: 0},
		{name: "head", method: "HEAD", size: 0},
		{name: "no-content", status: 204, size: 0}, {name: "not-modified", status: 304, size: 0},
		{name: "range", header: "Range", value: "bytes=0-4095", size: 4096},
		{name: "partial", status: 206, size: 4096},
		{name: "content-range", header: "Content-Range", value: "bytes 0-4095/8192", size: 4096},
		{name: "websocket", header: "Upgrade", value: "WebSocket", size: 4096},
		{name: "encoded", header: "Content-Encoding", value: "gzip", size: 4096},
		{name: "image", contentType: "image/png", size: 4096},
		{name: "font", contentType: "font/woff2", size: 4096},
		{name: "video", contentType: "video/mp4", size: 4096},
		{name: "audio", contentType: "audio/mpeg", size: 4096},
		{name: "wasm", contentType: "application/wasm", size: 4096},
		{name: "binary", contentType: "application/octet-stream", size: 4096},
		{name: "zip", contentType: "application/zip", size: 4096},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := bytes.Repeat([]byte("a"), tc.size)
			handler := CompressionMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				contentType := tc.contentType
				if contentType == "" {
					contentType = "text/plain"
				}
				w.Header().Set("Content-Type", contentType)
				w.Header().Set("Content-Length", strconv.Itoa(len(raw)))
				if tc.header == "Content-Encoding" || tc.header == "Content-Range" {
					w.Header().Set(tc.header, tc.value)
				}
				if tc.status != 0 {
					w.WriteHeader(tc.status)
				}
				if len(raw) > 0 {
					_, _ = w.Write(raw)
				}
			}))
			method := tc.method
			if method == "" {
				method = "GET"
			}
			r := httptest.NewRequest(method, "/", nil)
			r.Header.Set("Accept-Encoding", "br, gzip")
			r.Header.Set(tc.header, tc.value)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			wantEncoding := ""
			if tc.header == "Content-Encoding" {
				wantEncoding = tc.value
			}
			if got := w.Result().Header.Get("Content-Encoding"); got != wantEncoding {
				t.Fatalf("encoding = %q, want %q", got, wantEncoding)
			}
			if !bytes.Equal(w.Body.Bytes(), raw) {
				t.Fatal("skipped response body changed")
			}
		})
	}
}

func TestCompressionTypesAndThreshold(t *testing.T) {
	for _, contentType := range []string{"text/plain", "application/json", "application/problem+json", "application/xml", "application/javascript", "text/css", ""} {
		t.Run(contentType, func(t *testing.T) {
			handler := CompressionMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if contentType != "" {
					w.Header().Set("Content-Type", contentType)
				}
				_, _ = io.WriteString(w, strings.Repeat("a", 1024))
			}))
			r := httptest.NewRequest("GET", "/", nil)
			r.Header.Set("Accept-Encoding", "br")
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Result().Header.Get("Content-Encoding") != "br" {
				t.Fatal("text at the threshold was not compressed")
			}
		})
	}
}

func TestCompressionVaryETagAndHeaders(t *testing.T) {
	for _, vary := range []string{"Origin", "Origin, accept-encoding", "*"} {
		for _, accept := range []string{"br", "gzip", "identity"} {
			for _, status := range []int{200, 304} {
				t.Run(fmt.Sprintf("%s/%s/%d", vary, accept, status), func(t *testing.T) {
					h := CompressionMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
						w.Header().Set("Content-Type", "text/plain")
						w.Header().Set("Vary", vary)
						w.Header().Set("ETag", `"body"`)
						w.Header().Set("Cache-Control", "private, no-store")
						w.Header().Add("Set-Cookie", "session=example; HttpOnly")
						w.WriteHeader(status)
						w.Header().Set("ETag", `"too-late"`)
						if status == 200 {
							_, _ = io.WriteString(w, strings.Repeat("a", 2048))
						}
					}))
					r := httptest.NewRequest("GET", "/", nil)
					r.Header.Set("Accept-Encoding", accept)
					w := httptest.NewRecorder()
					h.ServeHTTP(w, r)
					headers := w.Result().Header
					wantETag := `W/"body"`
					if accept == "identity" {
						wantETag = `"body"`
					}
					if headers.Get("ETag") != wantETag {
						t.Fatalf("ETag = %q, want %q", headers.Get("ETag"), wantETag)
					}
					values := strings.Join(headers.Values("Vary"), ", ")
					if vary == "Origin" && values != "Origin, Accept-Encoding" || vary != "Origin" && values != vary {
						t.Fatalf("Vary = %q", values)
					}
					if headers.Get("Cache-Control") != "private, no-store" || len(headers.Values("Set-Cookie")) != 1 {
						t.Fatal("private cache or cookie headers changed")
					}
				})
			}
		}
	}
}

func TestCompressionStreamingFlush(t *testing.T) {
	for _, encoding := range []string{"br", "gzip"} {
		for _, contentType := range []string{"text/html", "text/event-stream"} {
			t.Run(encoding+"/"+contentType, func(t *testing.T) {
				release := make(chan struct{})
				first := strings.Repeat("data: first chunk\n\n", 128)
				srv := httptest.NewServer(CompressionMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", contentType)
					_, _ = io.WriteString(w, first)
					_ = http.NewResponseController(w).Flush()
					<-release
					_, _ = io.WriteString(w, "last chunk")
				})))
				defer srv.Close()
				defer close(release)
				request, _ := http.NewRequest("GET", srv.URL, nil)
				request.Header.Set("Accept-Encoding", encoding)
				client := &http.Client{Timeout: 3 * time.Second}
				response, err := client.Do(request)
				if err != nil {
					t.Fatal(err)
				}
				defer response.Body.Close()
				if response.Header.Get("Content-Encoding") != encoding {
					t.Fatal("stream was not compressed")
				}
				var reader io.Reader = brotli.NewReader(response.Body)
				if encoding == "gzip" {
					gz, err := gzip.NewReader(response.Body)
					if err != nil {
						t.Fatal(err)
					}
					defer gz.Close()
					reader = gz
				}
				prefix := make([]byte, len(first))
				if _, err := io.ReadFull(reader, prefix); err != nil {
					t.Fatalf("first chunk unavailable before handler completion: %v", err)
				}
				if string(prefix) != first {
					t.Fatal("flushed prefix changed")
				}
			})
		}
	}
}

func TestCompressionSmallFlush(t *testing.T) {
	h := CompressionMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: ready\n\n")
		w.(http.Flusher).Flush()
	}))
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Accept-Encoding", "br, gzip")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if !w.Flushed || w.Result().Header.Get("Content-Encoding") != "" || w.Body.String() != "data: ready\n\n" {
		t.Fatal("small streaming response must flush immediately as identity")
	}
}

func TestAppCompressionOptions(t *testing.T) {
	for _, option := range []string{"default", "disabled", "enable-gzip", "disabled-then-gzip", "gzip-then-disabled"} {
		t.Run(option, func(t *testing.T) {
			app := New()
			switch option {
			case "disabled":
				app.DisableCompression()
			case "enable-gzip":
				app.EnableGzip()
				app.EnableGzip()
			case "disabled-then-gzip":
				app.DisableCompression()
				app.EnableGzip()
			case "gzip-then-disabled":
				app.EnableGzip()
				app.DisableCompression()
			}
			raw := strings.Repeat("<p>page body</p>", 256)
			// Compression also applies to responses written by app middleware.
			app.Use(func(_ http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "text/html")
					_, _ = io.WriteString(w, raw)
				})
			})
			r := httptest.NewRequest("GET", "/", nil)
			r.Header.Set("Accept-Encoding", "br, gzip")
			w := httptest.NewRecorder()
			app.Build().ServeHTTP(w, r)
			want := "br"
			if option == "disabled" || option == "gzip-then-disabled" {
				want = ""
			} else if option == "disabled-then-gzip" {
				want = "gzip"
			}
			if got := w.Result().Header.Get("Content-Encoding"); got != want {
				t.Fatalf("encoding = %q, want %q", got, want)
			}
			if string(decodeCompressedResponse(t, want, w.Body.Bytes())) != raw {
				t.Fatal("compatibility option double-compressed or changed the body")
			}
		})
	}
}

func TestAppCompressionDeferredFragments(t *testing.T) {
	for _, encoding := range []string{"br", "gzip"} {
		t.Run(encoding, func(t *testing.T) {
			app := New()
			app.Page("GET /", func(ctx *Context) gosx.Node {
				return gosx.El("main", gosx.Text(strings.Repeat("shell ", 256)), ctx.Suspense(
					gosx.El("p", gosx.Text("pending")),
					func() (gosx.Node, error) { return gosx.El("p", gosx.Text("resolved")), nil },
				))
			})
			r := httptest.NewRequest("GET", "/", nil)
			r.Header.Set("Accept-Encoding", encoding)
			w := httptest.NewRecorder()
			app.Build().ServeHTTP(w, r)
			if !w.Flushed || w.Result().Header.Get("Content-Encoding") != encoding {
				t.Fatal("deferred HTML must flush through the compressor")
			}
			body := string(decodeCompressedResponse(t, encoding, w.Body.Bytes()))
			if !strings.Contains(body, "resolved") || !strings.Contains(body, "data-gosx-stream-template") {
				t.Fatal("compressed deferred fragment missing")
			}
		})
	}
}

func BenchmarkDynamicBrotli(b *testing.B) {
	for _, size := range []int{180 * 1024, 740 * 1024} {
		var html strings.Builder
		for i := 0; html.Len() < size; i++ {
			fmt.Fprintf(&html, `<section id="section-%d"><h2>Server rendering %d</h2><p>Render HTML on the server and load browser code only when the page needs it.</p><pre>const value = %d;</pre></section>`, i, i, i*7919)
		}
		body := []byte(html.String())
		for _, level := range []int{4, 5} {
			b.Run(fmt.Sprintf("%dKiB/level%d", size/1024, level), func(b *testing.B) {
				var compressed bytes.Buffer
				writer := brotli.NewWriterLevel(&compressed, level)
				b.SetBytes(int64(len(body)))
				b.ReportAllocs()
				for b.Loop() {
					compressed.Reset()
					writer.Reset(&compressed)
					_, _ = writer.Write(body)
					_ = writer.Close()
				}
				b.ReportMetric(float64(compressed.Len()), "compressed-B")
			})
		}
	}
}

func TestCompressionPreservesTrailersSetAfterWrite(t *testing.T) {
	for _, tc := range []struct {
		name, accept, body, wantEncoding string
	}{
		{"identity small body", "identity", "hello", ""},
		{"brotli large body", "br", strings.Repeat("hello trailer ", 200), "br"},
		{"gzip large body", "gzip", strings.Repeat("hello trailer ", 200), "gzip"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler := CompressionMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/plain; charset=utf-8")
				w.Header().Set("Trailer", "X-Checksum")
				w.Write([]byte(tc.body))
				w.Header().Set("X-Checksum", "abc")
				w.Header().Set(http.TrailerPrefix+"X-Late", "late")
			}))
			srv := httptest.NewServer(handler)
			defer srv.Close()
			req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
			req.Header.Set("Accept-Encoding", tc.accept)
			resp, err := (&http.Client{Transport: &http.Transport{DisableCompression: true}}).Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if _, err := io.ReadAll(resp.Body); err != nil {
				t.Fatal(err)
			}
			if got := resp.Header.Get("Content-Encoding"); got != tc.wantEncoding {
				t.Fatalf("Content-Encoding = %q, want %q", got, tc.wantEncoding)
			}
			if got := resp.Trailer.Get("X-Checksum"); got != "abc" {
				t.Fatalf("declared trailer = %q, want abc (trailers %v)", got, resp.Trailer)
			}
			if got := resp.Trailer.Get("X-Late"); got != "late" {
				t.Fatalf("prefixed trailer = %q, want late (trailers %v)", got, resp.Trailer)
			}
		})
	}
}

func TestCompressionRejectsUnacceptableIdentity(t *testing.T) {
	for _, tc := range []struct {
		accept, contentType, encoding string
		size, status                  int
		flush                         bool
	}{
		{"br, identity;q=0", "text/plain", "br", 100, 200, false},
		{"gzip, identity;q=0", "text/plain", "gzip", 100, 200, true},
		{"*;q=0", "text/plain", "", 2048, 406, false},
		{"br;q=0, gzip;q=0, identity;q=0", "text/plain", "", 100, 406, true},
		{"br, identity;q=0", "application/octet-stream", "", 100, 406, false},
		{"*;q=0, identity;q=1", "text/plain", "", 100, 200, false},
	} {
		t.Run(tc.accept+tc.contentType, func(t *testing.T) {
			raw := bytes.Repeat([]byte("x"), tc.size)
			h := CompressionMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tc.contentType)
				w.Header().Set("Content-Length", strconv.Itoa(len(raw)))
				_, _ = w.Write(raw)
				if tc.flush {
					w.(http.Flusher).Flush()
				}
			}))
			req := httptest.NewRequest("GET", "/", nil)
			req.Header.Set("Accept-Encoding", tc.accept)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if w.Code != tc.status || w.Header().Get("Content-Encoding") != tc.encoding {
				t.Fatalf("status=%d encoding=%q", w.Code, w.Header().Get("Content-Encoding"))
			}
			if tc.status == 200 && !bytes.Equal(decodeCompressedResponse(t, tc.encoding, w.Body.Bytes()), raw) {
				t.Fatal("body changed")
			}
			if tc.status == 406 && w.Body.Len() != 0 {
				t.Fatal("unacceptable representation was sent")
			}
		})
	}
}

func TestCompressionHijackCommitsHeaders(t *testing.T) {
	for _, accept := range []string{"", "br"} {
		t.Run(accept, func(t *testing.T) {
			errs := make(chan error, 1)
			h := CompressionMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Tunnel", "ready")
				w.WriteHeader(http.StatusOK)
				conn, rw, err := w.(http.Hijacker).Hijack()
				if err != nil {
					errs <- err
					return
				}
				defer conn.Close()
				_, err = rw.WriteString("tunnel-data")
				if err == nil {
					err = rw.Flush()
				}
				errs <- err
			}))
			srv := httptest.NewServer(h)
			defer srv.Close()
			conn, err := net.Dial("tcp", strings.TrimPrefix(srv.URL, "http://"))
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
			_, err = fmt.Fprintf(conn, "CONNECT example.test:443 HTTP/1.1\r\nHost: example.test\r\nAccept-Encoding: %s\r\n\r\n", accept)
			if err != nil {
				t.Fatal(err)
			}
			reader := bufio.NewReader(conn)
			line, err := reader.ReadString('\n')
			if err != nil {
				t.Fatal(err)
			}
			if line != "HTTP/1.1 200 OK\r\n" {
				t.Fatalf("status line=%q", line)
			}
			var headers strings.Builder
			for {
				line, err = reader.ReadString('\n')
				if err != nil {
					t.Fatal(err)
				}
				if line == "\r\n" {
					break
				}
				headers.WriteString(line)
			}
			if !strings.Contains(headers.String(), "X-Tunnel: ready") {
				t.Fatal("committed header missing")
			}
			body, err := io.ReadAll(reader)
			if err != nil {
				t.Fatal(err)
			}
			if string(body) != "tunnel-data" {
				t.Fatalf("tunnel bytes=%q", body)
			}
			if err := <-errs; err != nil {
				t.Fatal(err)
			}
		})
	}
}

type hijackRecordingWriter struct {
	*httptest.ResponseRecorder
	hijacked          bool
	writesAfterHijack int
}

func (w *hijackRecordingWriter) WriteHeader(status int) {
	if w.hijacked {
		w.writesAfterHijack++
		return
	}
	w.ResponseRecorder.WriteHeader(status)
}
func (w *hijackRecordingWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	w.hijacked = true
	return nil, nil, nil
}
func TestCompressionRawHijackSkipsFinalization(t *testing.T) {
	w := &hijackRecordingWriter{ResponseRecorder: httptest.NewRecorder()}
	handler := CompressionMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, _, err := w.(http.Hijacker).Hijack(); err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte("late")); err != http.ErrHijacked {
			t.Fatalf("late write: %v", err)
		}
	}))
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodConnect, "/", nil))
	if w.writesAfterHijack != 0 {
		t.Fatal("middleware finalized HTTP output after raw takeover")
	}
}
