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
