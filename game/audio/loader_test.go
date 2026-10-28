package audio

import (
	"bytes"
	"fmt"
	"strconv"
	"testing"
)

// These tests exercise HTTPLoader{} as a caller sees it, and they build for
// every target. The native HTTPLoader (net/http) and the js/wasm HTTPLoader
// (the browser's fetch) must agree on what they return, so one test body runs
// against both. Only the network differs: serveStub, defined in
// loader_native_test.go and loader_js_test.go, starts a native HTTP server or
// installs a fake fetch.

// stubResponse is what a stub network answers for one request. The zero value
// is an empty 200.
type stubResponse struct {
	status  int // 0 means 200
	body    []byte
	drop    bool // fail the request in the transport instead of answering it
	cutBody bool // send the status, then fail while the body is being read
}

func TestHTTPLoaderReturnsBodyBytes(t *testing.T) {
	// A RIFF header, then NUL, a high bit and 0xff: bytes that a text decoder
	// would change.
	want := []byte{'R', 'I', 'F', 'F', 0x00, 0x7f, 0x80, 0xff}
	base, requests := serveStub(t, func(path string) stubResponse {
		if path != "/boom.wav" {
			return stubResponse{status: 404}
		}
		return stubResponse{body: want}
	})

	got, err := (HTTPLoader{}).Load(base + "/boom.wav")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("Load = % x, want % x", got, want)
	}
	if requests() != 1 {
		t.Fatalf("requests = %d, want 1", requests())
	}
}

func TestHTTPLoaderEmptyBodyIsNotAnError(t *testing.T) {
	base, _ := serveStub(t, func(string) stubResponse { return stubResponse{} })

	got, err := (HTTPLoader{}).Load(base + "/empty.wav")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("Load = % x, want no bytes", got)
	}
}

// 204 is a 2xx status other than 200. The native loader rejects every status
// but 200, so the browser loader must reject 204 with the same text.
func TestHTTPLoaderNonOKStatusErrors(t *testing.T) {
	for _, status := range []int{204, 404, 500} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			base, requests := serveStub(t, func(string) stubResponse {
				return stubResponse{status: status}
			})
			uri := base + "/missing.wav"

			got, err := (HTTPLoader{}).Load(uri)
			want := fmt.Sprintf("audio: GET %s: status %d", uri, status)
			if err == nil || err.Error() != want {
				t.Fatalf("Load error = %v, want %q", err, want)
			}
			if got != nil {
				t.Fatalf("Load returned % x with an error, want no bytes", got)
			}
			if requests() != 1 {
				t.Fatalf("requests = %d, want 1", requests())
			}
		})
	}
}

func TestHTTPLoaderTransportFailureErrors(t *testing.T) {
	base, _ := serveStub(t, func(string) stubResponse { return stubResponse{drop: true} })

	got, err := (HTTPLoader{}).Load(base + "/boom.wav")
	if err == nil {
		t.Fatal("Load succeeded, want a transport error")
	}
	if got != nil {
		t.Fatalf("Load returned % x with an error, want no bytes", got)
	}
}

// The native loader checks the status before it reads the body, so a 404 whose
// body then fails still reports "status 404". The browser loader has already
// tried to read the body when it checks the status, and must give the same
// answer.
func TestHTTPLoaderStatusWinsOverBodyFailure(t *testing.T) {
	base, _ := serveStub(t, func(string) stubResponse {
		return stubResponse{status: 404, cutBody: true}
	})
	uri := base + "/missing.wav"

	got, err := (HTTPLoader{}).Load(uri)
	want := fmt.Sprintf("audio: GET %s: status 404", uri)
	if err == nil || err.Error() != want {
		t.Fatalf("Load error = %v, want %q", err, want)
	}
	if got != nil {
		t.Fatalf("Load returned % x with an error, want no bytes", got)
	}
}

// A 200 whose body fails is an error. A loader that dropped it would hand the
// player a clip cut short, with no error to report.
func TestHTTPLoaderBodyFailureOnOKErrors(t *testing.T) {
	base, _ := serveStub(t, func(string) stubResponse {
		return stubResponse{body: []byte("RIFF"), cutBody: true}
	})

	if _, err := (HTTPLoader{}).Load(base + "/cut.wav"); err == nil {
		t.Fatal("Load succeeded, want an error for a body that failed after the status arrived")
	}
}
