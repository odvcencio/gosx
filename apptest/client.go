package apptest

import (
	"io"
	"net/http"
	"net/url"
	"testing"

	"github.com/odvcencio/gosx/route"
	"github.com/odvcencio/gosx/server"
)

// Client maintains cookies across multiple in-memory requests, which makes it
// suitable for session-backed app and router tests.
type Client struct {
	handler http.Handler
	cookies map[string]*http.Cookie
}

// NewClient creates a stateful test client for any HTTP handler.
func NewClient(handler http.Handler) *Client {
	if handler == nil {
		return &Client{}
	}
	return &Client{
		handler: handler,
		cookies: make(map[string]*http.Cookie),
	}
}

// NewAppClient creates a stateful client from a server.App.
func NewAppClient(app *server.App) *Client {
	if app == nil {
		return &Client{}
	}
	return NewClient(app.Build())
}

// NewRouterClient creates a stateful client from a route.Router.
func NewRouterClient(router *route.Router) *Client {
	if router == nil {
		return &Client{}
	}
	return NewClient(router.Build())
}

// Do executes a request while automatically applying and persisting cookies.
func (c *Client) Do(t testing.TB, req *http.Request) *Response {
	t.Helper()
	if c == nil || c.handler == nil {
		t.Fatal("apptest.Client requires a non-nil handler")
	}
	if req == nil {
		t.Fatal("apptest.Client.Do requires a non-nil request")
	}
	for _, cookie := range c.cookies {
		if cookie != nil {
			req.AddCookie(cookie)
		}
	}
	res := Do(t, c.handler, req)
	c.storeCookies(res)
	return res
}

// Request builds and executes a request through the stateful client.
func (c *Client) Request(t testing.TB, method, target string, body io.Reader, opts ...RequestOption) *Response {
	t.Helper()
	return c.Do(t, Request(method, target, body, opts...))
}

// Get performs a GET request.
func (c *Client) Get(t testing.TB, target string, opts ...RequestOption) *Response {
	t.Helper()
	return c.Do(t, Request(http.MethodGet, target, nil, opts...))
}

// JSON performs a JSON request and persists any response cookies.
func (c *Client) JSON(t testing.TB, method, target string, payload any, opts ...RequestOption) *Response {
	t.Helper()
	return c.Do(t, JSONRequest(t, method, target, payload, opts...))
}

// Form performs a form-encoded request and persists any response cookies.
func (c *Client) Form(t testing.TB, method, target string, values url.Values, opts ...RequestOption) *Response {
	t.Helper()
	return c.Do(t, FormRequest(method, target, values, opts...))
}

// FollowRedirect executes a request and follows redirect responses, preserving
// cookies and switching to GET for See Other redirects like a browser would.
func (c *Client) FollowRedirect(t testing.TB, req *http.Request, max int) *Response {
	t.Helper()
	if max <= 0 {
		max = 10
	}
	currentReq := req
	for i := 0; i < max; i++ {
		res := c.Do(t, currentReq)
		location := res.Header("Location")
		if location == "" || !isRedirectStatus(res.StatusCode()) {
			return res
		}

		method := currentReq.Method
		var body io.Reader = nil
		if res.StatusCode() == http.StatusSeeOther || res.StatusCode() == http.StatusFound || res.StatusCode() == http.StatusMovedPermanently {
			method = http.MethodGet
		}
		nextURL := location
		if currentReq.URL != nil {
			nextURL = currentReq.URL.ResolveReference(mustParseURL(t, location)).String()
		}
		currentReq = Request(method, nextURL, body)
	}
	t.Fatalf("apptest.Client.FollowRedirect exceeded %d redirects", max)
	return nil
}

// Cookies returns the currently persisted cookie jar.
func (c *Client) Cookies() []*http.Cookie {
	if c == nil || len(c.cookies) == 0 {
		return nil
	}
	out := make([]*http.Cookie, 0, len(c.cookies))
	for _, cookie := range c.cookies {
		if cookie != nil {
			out = append(out, cookie)
		}
	}
	return out
}

func (c *Client) storeCookies(res *Response) {
	if c == nil || res == nil || res.Result() == nil {
		return
	}
	if c.cookies == nil {
		c.cookies = make(map[string]*http.Cookie)
	}
	for _, cookie := range res.Result().Cookies() {
		if cookie == nil {
			continue
		}
		if cookie.MaxAge < 0 || cookie.Value == "" {
			delete(c.cookies, cookie.Name)
			continue
		}
		copy := *cookie
		c.cookies[cookie.Name] = &copy
	}
}

func isRedirectStatus(status int) bool {
	switch status {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		return true
	default:
		return false
	}
}

func mustParseURL(t testing.TB, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse redirect url %q: %v", raw, err)
	}
	return parsed
}
