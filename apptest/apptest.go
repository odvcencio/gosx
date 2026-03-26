package apptest

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/odvcencio/gosx/route"
	"github.com/odvcencio/gosx/server"
)

// RequestOption mutates a test request before execution.
type RequestOption func(*http.Request)

// WithHeader adds or overwrites a request header.
func WithHeader(name, value string) RequestOption {
	return func(req *http.Request) {
		if req == nil || strings.TrimSpace(name) == "" {
			return
		}
		req.Header.Set(name, value)
	}
}

// WithCookie attaches a cookie to the request.
func WithCookie(cookie *http.Cookie) RequestOption {
	return func(req *http.Request) {
		if req == nil || cookie == nil {
			return
		}
		req.AddCookie(cookie)
	}
}

// WithAcceptJSON asks the handler for a JSON response.
func WithAcceptJSON() RequestOption {
	return WithHeader("Accept", "application/json")
}

// Request builds a test HTTP request.
func Request(method, target string, body io.Reader, opts ...RequestOption) *http.Request {
	if strings.TrimSpace(method) == "" {
		method = http.MethodGet
	}
	req := httptest.NewRequest(method, target, body)
	for _, opt := range opts {
		if opt != nil {
			opt(req)
		}
	}
	return req
}

// JSONRequest builds a request with a JSON-encoded body.
func JSONRequest(t testing.TB, method, target string, payload any, opts ...RequestOption) *http.Request {
	t.Helper()
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(payload); err != nil {
		t.Fatalf("encode json request: %v", err)
	}
	opts = append([]RequestOption{WithHeader("Content-Type", "application/json")}, opts...)
	return Request(method, target, &body, opts...)
}

// FormRequest builds a request with a URL-encoded form body.
func FormRequest(method, target string, values url.Values, opts ...RequestOption) *http.Request {
	body := strings.NewReader(values.Encode())
	opts = append([]RequestOption{WithHeader("Content-Type", "application/x-www-form-urlencoded")}, opts...)
	return Request(method, target, body, opts...)
}

// Response wraps a response recorder with assertion helpers.
type Response struct {
	Recorder *httptest.ResponseRecorder
}

// StatusCode returns the recorded response code.
func (r *Response) StatusCode() int {
	if r == nil || r.Recorder == nil {
		return 0
	}
	return r.Recorder.Code
}

// BodyString returns the recorded response body as a string.
func (r *Response) BodyString() string {
	if r == nil || r.Recorder == nil {
		return ""
	}
	return r.Recorder.Body.String()
}

// Header returns a named response header.
func (r *Response) Header(name string) string {
	if r == nil || r.Recorder == nil {
		return ""
	}
	return r.Recorder.Header().Get(name)
}

// Result returns the recorded http.Response.
func (r *Response) Result() *http.Response {
	if r == nil || r.Recorder == nil {
		return nil
	}
	return r.Recorder.Result()
}

// Cookie finds a named cookie in the response.
func (r *Response) Cookie(name string) (*http.Cookie, bool) {
	if r == nil || r.Recorder == nil {
		return nil, false
	}
	for _, cookie := range r.Recorder.Result().Cookies() {
		if cookie.Name == name {
			return cookie, true
		}
	}
	return nil, false
}

// DecodeJSON decodes the recorded JSON response body.
func (r *Response) DecodeJSON(t testing.TB, dst any) {
	t.Helper()
	if err := json.Unmarshal(r.Recorder.Body.Bytes(), dst); err != nil {
		t.Fatalf("decode json response: %v", err)
	}
}

// AssertStatus fails the test when the response status does not match.
func (r *Response) AssertStatus(t testing.TB, want int) {
	t.Helper()
	if got := r.StatusCode(); got != want {
		t.Fatalf("expected status %d, got %d: %s", want, got, r.BodyString())
	}
}

// AssertContains fails the test when any snippet is missing from the response body.
func (r *Response) AssertContains(t testing.TB, snippets ...string) {
	t.Helper()
	body := r.BodyString()
	for _, snippet := range snippets {
		if !strings.Contains(body, snippet) {
			t.Fatalf("expected %q in %q", snippet, body)
		}
	}
}

// Do executes a request against any HTTP handler.
func Do(t testing.TB, handler http.Handler, req *http.Request) *Response {
	t.Helper()
	if handler == nil {
		t.Fatal("apptest.Do requires a non-nil handler")
	}
	if req == nil {
		t.Fatal("apptest.Do requires a non-nil request")
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return &Response{Recorder: recorder}
}

// App executes a request against a server.App.
func App(t testing.TB, app *server.App, req *http.Request) *Response {
	t.Helper()
	if app == nil {
		t.Fatal("apptest.App requires a non-nil app")
	}
	return Do(t, app.Build(), req)
}

// Router executes a request against a route.Router.
func Router(t testing.TB, router *route.Router, req *http.Request) *Response {
	t.Helper()
	if router == nil {
		t.Fatal("apptest.Router requires a non-nil router")
	}
	return Do(t, router.Build(), req)
}
