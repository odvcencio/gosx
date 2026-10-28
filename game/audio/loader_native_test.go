//go:build !js || !wasm

package audio

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

// serveStub starts a native HTTP server whose answer to each request comes
// from respond, and returns its base URL and a counter of the requests it has
// served. The server closes when the test ends.
func serveStub(t *testing.T, respond func(path string) stubResponse) (string, func() int) {
	t.Helper()
	var count atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		reply := respond(r.URL.Path)
		if reply.drop {
			// Close the connection without an HTTP response.
			panic(http.ErrAbortHandler)
		}
		if reply.cutBody {
			// Declare one byte more than the handler sends, flush the status
			// line and headers, then drop the connection. The client gets the
			// status, and its read of the body fails with an unexpected EOF.
			w.Header().Set("Content-Length", strconv.Itoa(len(reply.body)+1))
			if reply.status != 0 {
				w.WriteHeader(reply.status)
			}
			_, _ = w.Write(reply.body)
			w.(http.Flusher).Flush()
			panic(http.ErrAbortHandler)
		}
		if reply.status != 0 {
			w.WriteHeader(reply.status)
		}
		// A 204 cannot carry a body; the write fails and the failure is the
		// stub's own business.
		_, _ = w.Write(reply.body)
	}))
	t.Cleanup(server.Close)
	return server.URL, func() int { return int(count.Load()) }
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// The Client field exists only in native builds. A configured client carries
// the request; the nil client in the shared tests uses http.DefaultClient.
func TestHTTPLoaderUsesConfiguredClient(t *testing.T) {
	var calls atomic.Int64
	var method, url string
	loader := HTTPLoader{Client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		method, url = r.Method, r.URL.String()
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader("via-client")),
			Request:    r,
		}, nil
	})}}

	got, err := loader.Load("http://clips.invalid/boom.wav")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if string(got) != "via-client" {
		t.Fatalf("Load = %q, want the configured client's body", got)
	}
	if calls.Load() != 1 || method != http.MethodGet || url != "http://clips.invalid/boom.wav" {
		t.Fatalf("client saw %d calls, %s %s; want 1 GET of the clip URI", calls.Load(), method, url)
	}
}
