package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"runtime"
	"strings"
	"testing"

	"m31labs.dev/gosx/server"
)

func TestDocsStrictFormRestoresValidationAndFlash(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	configureDocsTestSecret(t)
	app, err := buildDocsApp(server.ResolveAppRoot(thisFile), "8080")
	if err != nil {
		t.Fatal(err)
	}
	handler := app.Build()
	var cookies []*http.Cookie
	request := func(method, path string, values url.Values) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(values.Encode()))
		for _, cookie := range cookies {
			req.AddCookie(cookie)
		}
		if method == "POST" {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("Origin", "http://"+req.Host)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if next := rec.Result().Cookies(); len(next) > 0 {
			cookies = next
		}
		return rec
	}
	get := request("GET", "/docs/forms", nil)
	if get.Code != 200 || !strings.Contains(get.Body.String(), `action="/docs/forms/__actions/subscribe"`) {
		t.Fatalf("GET strict form: %d %s", get.Code, get.Body.String())
	}
	post := request("POST", "/docs/forms/__actions/subscribe", url.Values{"email": {"bad"}})
	if post.Code != 303 {
		t.Fatalf("validation POST: %d %s", post.Code, post.Body.String())
	}
	get = request("GET", "/docs/forms", nil)
	body := get.Body.String()
	if get.Code != 200 || !strings.Contains(body, `value="bad"`) || !strings.Contains(body, "A valid email address is required.") {
		t.Fatalf("restored validation: %d %s", get.Code, body)
	}
	const prefix = `name="csrf_token" value="`
	_, tokenBody, found := strings.Cut(body, prefix)
	if !found {
		t.Fatal("strict form has no token control")
	}
	token, _, _ := strings.Cut(tokenBody, `"`)
	post = request("POST", "/docs/forms/__actions/subscribe", url.Values{"email": {"reader@example.test"}, "csrf_token": {token}})
	if post.Code != 303 {
		t.Fatalf("success POST: %d %s", post.Code, post.Body.String())
	}
	get = request("GET", "/docs/forms", nil)
	if get.Code != 200 || !strings.Contains(get.Body.String(), "Subscription saved.") || !strings.Contains(get.Body.String(), "Subscribed!") {
		t.Fatalf("success flash: %d %s", get.Code, get.Body.String())
	}
	get = request("GET", "/docs/forms", nil)
	if strings.Contains(get.Body.String(), "Subscription saved.") {
		t.Fatal("success notice was not consumed after one GET")
	}
}
