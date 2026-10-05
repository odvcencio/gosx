package route

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/action"
	"m31labs.dev/gosx/internal/strictcomponent"
	"m31labs.dev/gosx/ir"
	"m31labs.dev/gosx/session"
)

func TestStrictStringMapLookupZeroValueAndBoundary(t *testing.T) {
	for _, values := range []map[string]string{nil, {"profile.email": "saved"}} {
		env := fileRenderEnv{propsFrame: propsFrameStrict, values: map[string]any{"props": map[string]any{"Values": values}}}
		if got := evalFileExpr(`props.Values["missing"]`, env); got != "" {
			t.Fatalf("absent map key = %#v, want Go string zero value", got)
		}
		if got := evalFileExpr(`props.Values["profile.email"]`, env); got != values["profile.email"] {
			t.Fatalf("literal map key = %#v", got)
		}
	}
	path := "Values." + strictcomponent.MapKeySegment("email")
	schema := ir.SlicePropSchema{Elem: "Row", Reads: map[string]string{path: "string"}}
	type rowData struct{ Values map[string]string }
	if _, err := requireStrictSliceValue([]rowData(nil), schema); err != nil {
		t.Fatal(err)
	}
	type wrongRow struct{ Values map[string]any }
	if _, err := requireStrictSliceValue([]wrongRow(nil), schema); err == nil {
		t.Fatal("accepted an untyped map in an empty slice")
	}
	if _, err := requireStrictSpreadStructField(wrongRow{}, "Row", schema.Reads); err == nil {
		t.Fatal("accepted an untyped map in nested props")
	}
}

func TestFormStateWithoutSession(t *testing.T) {
	var nilContext *RouteContext
	if got := nilContext.FormState("save"); got.ActionURL != "" || got.CSRFToken != "" {
		t.Fatalf("nil context: %#v", got)
	}
	ctx := &RouteContext{Request: httptest.NewRequest("GET", "/notes/", nil)}
	if got := ctx.FormState("save"); got.ActionURL != "/notes/__actions/save" || got.CSRFToken != "" || got.Status != 0 {
		t.Fatalf("without session: %#v", got)
	}
}

func TestStrictFormStateRoundTrip(t *testing.T) {
	root := t.TempDir()
	writeRouteFile(t, root, "page.gsx", `package app
import forms "m31labs.dev/gosx/route"
type PageProps struct { Form forms.FormState }
component Page(props: PageProps) {
	return <form method="post" action={props.Form.ActionURL}>
		<input type="hidden" name="csrf_token" value={props.Form.CSRFToken} />
		<input name="title" value={props.Form.Values["title"]} />
		<p>{props.Form.FieldErrors["title"]}</p><p>{props.Form.Message}</p>
		<p>{props.Form.Flash["notice"]}</p><p>{props.Form.Flash["count"]}</p>
		<b>{"empty" + props.Form.Values["missing.key"]}</b>
	</form>
}
`)
	type pageData struct{ Form FormState }
	var loaded FormState
	modules := NewFileModuleRegistry()
	err := modules.Register(FileModuleFor("page.gsx", FileModuleOptions{
		Load: func(ctx *RouteContext, page FilePage) (any, error) {
			loaded = ctx.FormState("save")
			again := ctx.FormState("save")
			if !reflect.DeepEqual(loaded, again) {
				t.Fatal("reading form state consumed flashed values")
			}
			if loaded.Values != nil {
				loaded.Values["probe"] = "copy"
				if again.Values["probe"] != "" {
					t.Fatal("form state shares the action's mutable values")
				}
			}
			return pageData{Form: loaded}, nil
		},
		Actions: FileActions{
			"save": func(ctx *action.Context) error {
				if ctx.FormData["title"] != "A note" {
					ctx.ValidationFailure("Try again.", map[string]string{"title": "Title is required."})
					return nil
				}
				session.AddFlash(ctx.Request, "notice", "Saved.")
				session.AddFlash(ctx.Request, "notice", "Second notice.")
				session.AddFlash(ctx.Request, "count", 2)
				return ctx.Success("Done.", nil)
			},
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouter()
	router.SetLayout(func(ctx *RouteContext, body gosx.Node) gosx.Node { return body })
	if err := router.AddDir(root, FileRoutesOptions{Modules: modules}); err != nil {
		t.Fatal(err)
	}
	sessions, err := session.New("01234567890123456789012345678901", session.Options{AllowInsecure: true})
	if err != nil {
		t.Fatal(err)
	}
	handler := sessions.Middleware(sessions.Protect(router.Build()))
	var cookies []*http.Cookie
	request := func(method, path string, form url.Values) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, "http://example.test"+path, strings.NewReader(form.Encode()))
		for _, cookie := range cookies {
			req.AddCookie(cookie)
		}
		if method == "POST" {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("Origin", "http://example.test")
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if next := rec.Result().Cookies(); len(next) > 0 {
			cookies = next
		}
		return rec
	}
	get := request("GET", "/", nil)
	if get.Code != 200 || len(cookies) != 0 || loaded.CSRFToken != "" || !strings.Contains(get.Body.String(), "<b>empty</b>") {
		t.Fatalf("initial GET: %d %s", get.Code, get.Body.String())
	}
	post := request("POST", "/__actions/save", url.Values{"title": {"<bad>"}})
	if post.Code != 303 {
		t.Fatalf("invalid POST: %d %s", post.Code, post.Body.String())
	}
	get = request("GET", "/", nil)
	if get.Code != 200 || loaded.OK || loaded.Status != 422 || loaded.Values["title"] != "<bad>" || loaded.CSRFToken == "" {
		t.Fatalf("validation state: %#v; %d %s", loaded, get.Code, get.Body.String())
	}
	if !strings.Contains(get.Body.String(), `value="&lt;bad&gt;"`) || !strings.Contains(get.Body.String(), "Title is required.") {
		t.Fatalf("validation HTML: %s", get.Body.String())
	}
	post = request("POST", "/__actions/save", url.Values{"title": {"A note"}, "csrf_token": {"wrong"}})
	if post.Code != 403 {
		t.Fatalf("invalid token: %d", post.Code)
	}
	post = request("POST", "/__actions/save", url.Values{"title": {"A note"}, "csrf_token": {loaded.CSRFToken}})
	if post.Code != 303 {
		t.Fatalf("valid POST: %d %s", post.Code, post.Body.String())
	}
	get = request("GET", "/", nil)
	if get.Code != 200 || !loaded.OK || loaded.Message != "Done." || loaded.Flash["notice"] != "Saved." || loaded.Flash["count"] != "2" {
		t.Fatalf("success state: %#v; %d %s", loaded, get.Code, get.Body.String())
	}
	if _, leaked := loaded.Flash["__gosx_action_state"]; leaked {
		t.Fatal("internal action flash exposed as a notice")
	}
	request("GET", "/", nil)
	if loaded.Status != 0 || len(loaded.Flash) != 0 {
		t.Fatalf("flashes survived another GET: %#v", loaded)
	}
}
