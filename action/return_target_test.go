package action

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"m31labs.dev/gosx/session"
)

func TestReturnTargetURLFormIsPrivateAndPreservesFragment(t *testing.T) {
	values := url.Values{
		ReturnTargetField: {"/board?pos=WR&page=2#board-pool"},
		"name":            {"Ada"},
	}
	req := httptest.NewRequest(http.MethodPost, "https://league.example/gosx/action/save", strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	var seen *http.Request
	var formData map[string]string
	w := httptest.NewRecorder()
	ServeHandler(w, req, func(ctx *Context) error {
		seen = ctx.Request
		formData = ctx.FormData
		if _, ok := ctx.FormData[ReturnTargetField]; ok {
			t.Fatal("reserved return target leaked into Context.FormData")
		}
		return nil
	})

	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", w.Code)
	}
	if got := w.Header().Get("Location"); got != "/board?pos=WR&page=2#board-pool" {
		t.Fatalf("Location = %q", got)
	}
	if seen == nil || seen.Form.Has(ReturnTargetField) || seen.PostForm.Has(ReturnTargetField) {
		t.Fatalf("reserved field leaked through parsed request: %#v", seen)
	}
	if seen == nil || seen.Context().Value(returnTargetContextKey{}) == nil {
		t.Fatal("validated return target was not privately carried")
	}
	if formData["name"] != "Ada" {
		t.Fatalf("form data = %#v", formData)
	}
}

func TestReturnTargetMultipartScalarIsPrivate(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField(ReturnTargetField, "/team#roster"); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteField("name", "Ada"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "https://league.example/gosx/action/save", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()
	ServeHandler(w, req, func(ctx *Context) error {
		if _, ok := ctx.FormData[ReturnTargetField]; ok {
			t.Fatal("reserved multipart field leaked into Context.FormData")
		}
		if _, ok := ctx.Request.MultipartForm.Value[ReturnTargetField]; ok {
			t.Fatal("reserved multipart field leaked into MultipartForm.Value")
		}
		return nil
	})
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/team#roster" {
		t.Fatalf("status/location = %d %q", w.Code, w.Header().Get("Location"))
	}
}

func TestReturnTargetInvalidValuesFallThrough(t *testing.T) {
	for _, raw := range []string{
		"//evil.example/path",
		"https://evil.example/path",
		"javascript:alert(1)",
		"team/roster",
		"?next=/admin",
		"#roster",
		"/team\\roster",
		"/team%5Croster",
		"/team%0Aroster",
		"/team%ZZ",
		"\t/team",
		"/team\n",
		" /team",
		"/team ",
	} {
		if got := normalizedReturnTarget(raw); got != "" {
			t.Errorf("normalizedReturnTarget(%q) = %q, want empty", raw, got)
		}
	}
	if got := normalizedReturnTarget("/team?tab=board#roster"); got != "/team?tab=board#roster" {
		t.Fatalf("valid target = %q", got)
	}
}

func TestReturnTargetReadsFirstBodyValueAndIgnoresQuery(t *testing.T) {
	values := "" + url.QueryEscape(ReturnTargetField) + "=" + url.QueryEscape("//evil.example") + "&" + url.QueryEscape(ReturnTargetField) + "=" + url.QueryEscape("/good")
	req := httptest.NewRequest(http.MethodPost, "https://league.example/gosx/action/save?"+ReturnTargetField+"=%2Fquery", strings.NewReader(values))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	ServeHandler(w, req, func(ctx *Context) error { return nil })
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 because the first body value is invalid and query cannot steer", w.Code)
	}
	if got := w.Header().Get("Location"); got != "" {
		t.Fatalf("unexpected Location %q", got)
	}
}

func TestRedirectTargetPrecedenceAndRefererHardening(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "https://league.example/gosx/action/save", nil)
	req.Header.Set("Referer", "https://EVIL.example/team?tab=board")
	if got := redirectTarget(req, Result{}); got != "" {
		t.Fatalf("cross-site referer = %q", got)
	}

	req = httptest.NewRequest(http.MethodPost, "https://league.example/gosx/action/save", nil)
	req.Header.Set("Referer", "https://LEAGUE.example/team?tab=board#roster")
	if got := redirectTarget(req, Result{}); got != "/team?tab=board" {
		t.Fatalf("same-host referer = %q", got)
	}

	req = requestWithReturnTarget(req, "/draft#clock")
	if got := redirectTarget(req, Result{}); got != "/draft#clock" {
		t.Fatalf("reserved target = %q", got)
	}
	if got := redirectTarget(req, Result{Redirect: "/explicit"}); got != "/explicit" {
		t.Fatalf("explicit redirect = %q", got)
	}
}

func TestReturnTargetIsOmittedFromManagedJSONValues(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "https://league.example/gosx/action/save", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()
	ServeHandler(w, req, func(ctx *Context) error {
		ctx.SetResult(Result{
			OK: true,
			Values: map[string]string{
				ReturnTargetField: "/secret",
				"name":            "Ada",
			},
		})
		return nil
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var result Result
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if _, ok := result.Values[ReturnTargetField]; ok {
		t.Fatalf("reserved field leaked into JSON result: %#v", result.Values)
	}
	if result.Values["name"] != "Ada" {
		t.Fatalf("result values = %#v", result.Values)
	}
}

func TestReturnTargetNativeValidationFlashOmitsReservedValue(t *testing.T) {
	sessions := session.MustNew("return-target-session-secret", session.Options{})
	handler := sessions.Middleware(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodPost {
			ServeHandler(w, req, func(ctx *Context) error {
				return Validation("name is required", map[string]string{
					"name": "required",
				}, ctx.FormData)
			})
			return
		}

		view, ok := State(req, "save")
		if !ok {
			t.Fatal("expected flashed validation state")
		}
		if _, ok := view.Result.Values[ReturnTargetField]; ok {
			t.Fatalf("reserved field leaked into flash values: %#v", view.Result.Values)
		}
		if view.Value("name") != "Ada" || view.Error("name") != "required" {
			t.Fatalf("unexpected flashed view: %#v", view)
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	values := url.Values{
		ReturnTargetField: {"/team?tab=board#roster"},
		"name":            {"Ada"},
	}
	postReq := httptest.NewRequest(http.MethodPost, "/account/__actions/save", strings.NewReader(values.Encode()))
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postRes := httptest.NewRecorder()
	handler.ServeHTTP(postRes, postReq)
	if postRes.Code != http.StatusSeeOther {
		t.Fatalf("POST status = %d, want 303", postRes.Code)
	}
	if got := postRes.Header().Get("Location"); got != "/team?tab=board#roster" {
		t.Fatalf("POST Location = %q", got)
	}
	cookie := postRes.Result().Cookies()[0]
	getReq := httptest.NewRequest(http.MethodGet, "/team?tab=board", nil)
	getReq.AddCookie(cookie)
	getRes := httptest.NewRecorder()
	handler.ServeHTTP(getRes, getReq)
	if getRes.Code != http.StatusNoContent {
		t.Fatalf("GET status = %d, want 204", getRes.Code)
	}
}

func TestRedirectBackWithMessageNativePRGUsesSubmittedTargetAndFlashesMessage(t *testing.T) {
	sessions := session.MustNew("redirect-back-native-secret", session.Options{})
	handler := sessions.Middleware(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodPost {
			ServeHandler(w, req, func(ctx *Context) error {
				if _, ok := ctx.FormData[ReturnTargetField]; ok {
					t.Fatal("reserved return target leaked into FormData")
				}
				ctx.RedirectBackWithMessage("/fallback", "  Saved.  ")
				return nil
			})
			return
		}

		view, ok := State(req, "save")
		if !ok {
			t.Fatal("expected flashed redirect-back result")
		}
		if view.Message() != "Saved." {
			t.Fatalf("flashed message = %q, want Saved.", view.Message())
		}
		if view.Redirect() != "/board?tab=all#roster" {
			t.Fatalf("flashed redirect = %q", view.Redirect())
		}
		if _, ok := view.Result.Values[ReturnTargetField]; ok {
			t.Fatalf("reserved return target leaked into flash values: %#v", view.Result.Values)
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	values := url.Values{
		ReturnTargetField: {"/board?tab=all#roster"},
		"name":            {"Ada"},
	}
	postReq := httptest.NewRequest(http.MethodPost, "/account/__actions/save", strings.NewReader(values.Encode()))
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postRes := httptest.NewRecorder()
	handler.ServeHTTP(postRes, postReq)
	if postRes.Code != http.StatusSeeOther {
		t.Fatalf("POST status = %d, want 303", postRes.Code)
	}
	if got := postRes.Header().Get("Location"); got != "/board?tab=all#roster" {
		t.Fatalf("POST Location = %q", got)
	}
	cookies := postRes.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("POST did not set a session cookie")
	}

	getReq := httptest.NewRequest(http.MethodGet, "/board?tab=all", nil)
	getReq.AddCookie(cookies[0])
	getRes := httptest.NewRecorder()
	handler.ServeHTTP(getRes, getReq)
	if getRes.Code != http.StatusNoContent {
		t.Fatalf("GET status = %d, want 204", getRes.Code)
	}
}

func TestRedirectBackWithMessageManagedResultUsesSubmittedTarget(t *testing.T) {
	registry := NewRegistry()
	registry.Register("save", func(ctx *Context) error {
		if _, ok := ctx.FormData[ReturnTargetField]; ok {
			t.Fatal("reserved return target leaked into FormData")
		}
		ctx.RedirectBackWithMessage("/fallback", "  Saved.  ")
		return nil
	})

	values := url.Values{
		ReturnTargetField: {"/board?tab=all#roster"},
		"name":            {"Ada"},
	}
	req := httptest.NewRequest(http.MethodPost, "/gosx/action/save", strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.SetPathValue("name", "save")
	w := httptest.NewRecorder()
	registry.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("managed status = %d, want 303", w.Code)
	}

	var got Result
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.OK || got.Message != "Saved." || got.Redirect != "/board?tab=all#roster" {
		t.Fatalf("managed result = %+v", got)
	}
	if _, ok := got.Values[ReturnTargetField]; ok {
		t.Fatalf("reserved return target leaked into managed values: %#v", got.Values)
	}
	if got.Values["name"] != "Ada" {
		t.Fatalf("managed values = %#v", got.Values)
	}
}

func TestRedirectBackWithMessageFallbackAndRootSafety(t *testing.T) {
	tests := []struct {
		name      string
		submitted string
		fallback  string
		want      string
	}{
		{
			name:     "missing submitted target uses fallback",
			fallback: "/fallback?tab=one#top",
			want:     "/fallback?tab=one#top",
		},
		{
			name:      "empty submitted target uses fallback",
			submitted: "",
			fallback:  "/fallback#top",
			want:      "/fallback#top",
		},
		{
			name:      "protocol relative submitted target uses fallback",
			submitted: "//evil.example/path",
			fallback:  "/fallback#top",
			want:      "/fallback#top",
		},
		{
			name:      "absolute submitted target uses fallback",
			submitted: "https://evil.example/path",
			fallback:  "/fallback?ok=1#top",
			want:      "/fallback?ok=1#top",
		},
		{
			name:      "malformed submitted target uses fallback",
			submitted: "/board%ZZ",
			fallback:  "/fallback",
			want:      "/fallback",
		},
		{
			name:     "invalid fallback resolves to root",
			fallback: "https://evil.example/path",
			want:     "/",
		},
		{
			name:     "protocol relative fallback resolves to root",
			fallback: "//evil.example/path",
			want:     "/",
		},
		{
			name:     "malformed fallback resolves to root",
			fallback: "/fallback%ZZ",
			want:     "/",
		},
		{
			name:     "empty fallback resolves to root",
			fallback: "",
			want:     "/",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registry := NewRegistry()
			registry.Register("save", func(ctx *Context) error {
				ctx.RedirectBackWithMessage(test.fallback, "saved")
				return nil
			})

			values := url.Values{"name": {"Ada"}}
			if test.submitted != "" {
				values.Set(ReturnTargetField, test.submitted)
			}
			req := httptest.NewRequest(http.MethodPost, "/gosx/action/save", strings.NewReader(values.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.SetPathValue("name", "save")
			w := httptest.NewRecorder()
			registry.ServeHTTP(w, req)
			if w.Code != http.StatusSeeOther {
				t.Fatalf("status = %d, want 303", w.Code)
			}
			if got := w.Header().Get("Location"); got != test.want {
				t.Fatalf("Location = %q, want %q", got, test.want)
			}
		})
	}
}

func TestRedirectBackWithMessageKeepsExplicitRedirectAuthoritative(t *testing.T) {
	registry := NewRegistry()
	registry.Register("save", func(ctx *Context) error {
		ctx.RedirectWithMessage("/explicit?tab=done#notice", "saved")
		return nil
	})
	values := url.Values{ReturnTargetField: {"/submitted#return"}}
	req := httptest.NewRequest(http.MethodPost, "/gosx/action/save", strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("name", "save")
	w := httptest.NewRecorder()
	registry.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", w.Code)
	}
	if got := w.Header().Get("Location"); got != "/explicit?tab=done#notice" {
		t.Fatalf("explicit Location = %q", got)
	}
}

func TestExplicitRedirectSanitizationIsSharedByNativeAndManagedResponses(t *testing.T) {
	for _, raw := range []string{
		"//evil.example/path",
		"https://evil.example/path",
		"javascript:alert(1)",
		"/bad%ZZ",
		"/bad\\path",
		"/bad%5Cpath",
		"/bad%0Apath",
	} {
		t.Run(raw, func(t *testing.T) {
			registry := NewRegistry()
			registry.Register("save", func(ctx *Context) error {
				ctx.SetResult(Result{OK: true, Message: "saved", Redirect: raw})
				return nil
			})

			nativeReq := httptest.NewRequest(http.MethodPost, "/gosx/action/save", strings.NewReader("name=Ada"))
			nativeReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			nativeReq.SetPathValue("name", "save")
			nativeRes := httptest.NewRecorder()
			registry.ServeHTTP(nativeRes, nativeReq)
			if nativeRes.Code != http.StatusSeeOther {
				t.Fatalf("native status = %d, want 303", nativeRes.Code)
			}
			if got := nativeRes.Header().Get("Location"); got != "/" {
				t.Fatalf("native unsafe Location = %q, want /", got)
			}

			managedReq := httptest.NewRequest(http.MethodPost, "/gosx/action/save", strings.NewReader("name=Ada"))
			managedReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			managedReq.Header.Set("Accept", "application/json")
			managedReq.SetPathValue("name", "save")
			managedRes := httptest.NewRecorder()
			registry.ServeHTTP(managedRes, managedReq)
			if managedRes.Code != http.StatusSeeOther {
				t.Fatalf("managed status = %d, want 303", managedRes.Code)
			}
			var got Result
			if err := json.Unmarshal(managedRes.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got.Redirect != "/" || got.Message != "saved" {
				t.Fatalf("managed sanitized result = %+v", got)
			}
		})
	}
}

func TestExplicitValidRedirectPreservesQueryAndFragment(t *testing.T) {
	registry := NewRegistry()
	registry.Register("save", func(ctx *Context) error {
		ctx.RedirectWithMessage("/explicit?tab=done#notice", "saved")
		return nil
	})

	req := httptest.NewRequest(http.MethodPost, "/gosx/action/save", strings.NewReader("name=Ada"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.SetPathValue("name", "save")
	w := httptest.NewRecorder()
	registry.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", w.Code)
	}
	var got Result
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Redirect != "/explicit?tab=done#notice" {
		t.Fatalf("redirect = %q", got.Redirect)
	}
}
