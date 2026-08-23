package route

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/action"
	"m31labs.dev/gosx/server"
	"m31labs.dev/gosx/session"
)

func TestManagedRouterFreezesAndDispatchesAllMethods(t *testing.T) {
	router := NewRouter()
	if err := router.RegisterManagedPOST("save", action.Config{}, func(*action.Context) (action.Result, error) {
		return action.Result{OK: true}, nil
	}); err != nil {
		t.Fatal(err)
	}
	handler, err := router.BuildChecked()
	if err != nil {
		t.Fatal(err)
	}
	if err := router.RegisterManagedPOST("late", action.Config{}, func(*action.Context) (action.Result, error) {
		return action.Result{OK: true}, nil
	}); err == nil {
		t.Fatal("late managed registration succeeded after Build")
	}
	manager, token, cookie := routeManagedSession(t)
	post := httptest.NewRequest(http.MethodPost, "/gosx/action/save", strings.NewReader("csrf_token="+token))
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	post.Header.Set("Cookie", cookie)
	postResult := httptest.NewRecorder()
	manager.Middleware(manager.Protect(handler)).ServeHTTP(postResult, post)
	if postResult.Code != http.StatusOK {
		t.Fatalf("managed POST status=%d body=%s", postResult.Code, postResult.Body.String())
	}
	get := httptest.NewRequest(http.MethodGet, "/gosx/action/save", nil)
	get.Header.Set("Cookie", cookie)
	getResult := httptest.NewRecorder()
	manager.Middleware(manager.Protect(handler)).ServeHTTP(getResult, get)
	if getResult.Code != http.StatusMethodNotAllowed || getResult.Header().Get("Allow") != http.MethodPost {
		t.Fatalf("managed GET status=%d allow=%q body=%s", getResult.Code, getResult.Header().Get("Allow"), getResult.Body.String())
	}

	// Build is repeatable from the frozen registration snapshot.
	if _, err := router.BuildChecked(); err != nil {
		t.Fatalf("repeat BuildChecked failed: %v", err)
	}
}

func TestManagedNamespaceRejectsRawRoute(t *testing.T) {
	router := NewRouter()
	router.Handle("/gosx/action/raw", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	if _, err := router.BuildChecked(); err == nil {
		t.Fatal("raw handler under managed namespace was accepted")
	}
}

func TestManagedNamespaceRejectsComposedServeMuxPatterns(t *testing.T) {
	cases := []struct {
		name    string
		pattern string
		nested  bool
	}{
		{name: "direct page", pattern: "/gosx/action/page"},
		{name: "method qualified", pattern: "POST /gosx/action/method"},
		{name: "host qualified", pattern: "managed.example/gosx/action/host"},
		{name: "method and host qualified", pattern: "POST managed.example/gosx/action/both"},
		{name: "nested page", pattern: "/action/nested", nested: true},
		{name: "nested method and host", pattern: "/action/nested-qualified", nested: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router := NewRouter()
			if tc.nested {
				parentPattern := "/gosx"
				if strings.Contains(tc.name, "qualified") {
					parentPattern = "POST managed.example/gosx"
				}
				router.Add(Route{Pattern: parentPattern, Children: []Route{{Pattern: tc.pattern, Handler: func(*RouteContext) gosx.Node {
					return gosx.Text("unreachable")
				}}}})
			} else {
				router.Handle(tc.pattern, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
			}
			if _, err := router.BuildChecked(); err == nil {
				t.Fatalf("reserved pattern %q was accepted", tc.pattern)
			}
		})
	}
}

func TestManagedActionNameContractIsExactAcrossRegistrationAndRenderers(t *testing.T) {
	valid64 := "a" + strings.Repeat("x", 63)
	cases := []struct {
		name  string
		valid bool
	}{
		{name: "save", valid: true},
		{name: valid64, valid: true},
		{name: "", valid: false},
		{name: " save", valid: false},
		{name: "save ", valid: false},
		{name: "naïve", valid: false},
		{name: "save/name", valid: false},
		{name: "save%2Fname", valid: false},
		{name: "1starts", valid: false},
		{name: strings.Repeat("x", 65), valid: false},
	}
	for _, tc := range cases {
		t.Run(strings.ReplaceAll(tc.name, "/", "_"), func(t *testing.T) {
			path := action.ActionPath(tc.name)
			if tc.valid {
				want := "/gosx/action/" + tc.name
				if path != want || fileRenderManagedActionURL(tc.name) != want {
					t.Fatalf("name=%q path=%q filePath=%q, want %q", tc.name, path, fileRenderManagedActionURL(tc.name), want)
				}
			} else if path != "" || fileRenderManagedActionURL(tc.name) != "" {
				t.Fatalf("invalid name=%q rendered path=%q filePath=%q", tc.name, path, fileRenderManagedActionURL(tc.name))
			}

			router := action.NewRouter()
			err := router.RegisterManagedPOST(tc.name, action.Config{}, func(*action.Context) (action.Result, error) {
				return action.Result{OK: true}, nil
			})
			if (err == nil) != tc.valid {
				t.Fatalf("registration error=%v, valid=%v", err, tc.valid)
			}

			html := gosx.RenderHTML((&RouteContext{}).ActionForm(tc.name))
			if tc.valid {
				if !strings.Contains(html, `action="`+path+`"`) {
					t.Fatalf("GSX ActionForm html=%q, missing exact action path %q", html, path)
				}
			} else if strings.Contains(html, `/gosx/action/`) {
				t.Fatalf("invalid name=%q emitted managed endpoint: %q", tc.name, html)
			}
		})
	}
}

func TestManagedCapabilitySurvivesRealServerAppMount(t *testing.T) {
	router := NewRouter()
	if err := router.RegisterManagedPOST("mounted", action.Config{}, func(*action.Context) (action.Result, error) {
		return action.Result{OK: true}, nil
	}); err != nil {
		t.Fatal(err)
	}
	app := server.New()
	app.Mount("/", router.Build())
	manager, token, cookie := routeManagedSession(t)
	req := httptest.NewRequest(http.MethodPost, "/gosx/action/mounted", strings.NewReader("csrf_token="+token))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cookie", cookie)
	result := httptest.NewRecorder()
	manager.Middleware(manager.Protect(app.Build())).ServeHTTP(result, req)
	if result.Code != http.StatusOK {
		t.Fatalf("App-mounted managed action was re-parsed by Protect: status=%d body=%s", result.Code, result.Body.String())
	}
}

func TestDirectMountedManagedRouterFreezesAtAppBuild(t *testing.T) {
	var invoked int
	router := action.NewRouter()
	if err := router.RegisterManagedPOST("direct", action.Config{}, func(*action.Context) (action.Result, error) {
		invoked++
		return action.Result{OK: true}, nil
	}); err != nil {
		t.Fatal(err)
	}
	app := server.New()
	app.Mount("POST managed.example/", router)
	handler := app.Build()
	if err := router.RegisterManagedPOST("late", action.Config{}, func(*action.Context) (action.Result, error) {
		return action.Result{OK: true}, nil
	}); err == nil {
		t.Fatal("direct mounted router accepted a registration after App.Build")
	}
	manager, token, cookie := routeManagedSession(t)

	matched := httptest.NewRequest(http.MethodPost, "http://managed.example/gosx/action/direct", strings.NewReader("csrf_token="+token))
	matched.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	matched.Header.Set("Cookie", cookie)
	matchedResult := httptest.NewRecorder()
	manager.Middleware(manager.Protect(handler)).ServeHTTP(matchedResult, matched)
	if matchedResult.Code != http.StatusOK || invoked != 1 {
		t.Fatalf("direct mount status=%d invoked=%d body=%s", matchedResult.Code, invoked, matchedResult.Body.String())
	}

	wrongHost := httptest.NewRequest(http.MethodPost, "http://other.example/gosx/action/direct", strings.NewReader("csrf_token=wrong"))
	wrongHost.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	wrongHost.Header.Set("Cookie", cookie)
	wrongHostResult := httptest.NewRecorder()
	manager.Middleware(manager.Protect(handler)).ServeHTTP(wrongHostResult, wrongHost)
	if wrongHostResult.Code != http.StatusForbidden || invoked != 1 {
		t.Fatalf("direct mount wrong-host status=%d invoked=%d body=%s", wrongHostResult.Code, invoked, wrongHostResult.Body.String())
	}
}

func TestManagedCapabilitySurvivesNestedAppMount(t *testing.T) {
	var invoked int
	router := NewRouter()
	if err := router.RegisterManagedPOST("nested", action.Config{}, func(*action.Context) (action.Result, error) {
		invoked++
		return action.Result{OK: true}, nil
	}); err != nil {
		t.Fatal(err)
	}
	child := server.New()
	child.Mount("/", router.Build())
	parent := server.New()
	parent.MountApp("/nested", child)
	handler := parent.Build()
	manager, token, cookie := routeManagedSession(t)
	req := httptest.NewRequest(http.MethodPost, "/nested/gosx/action/nested", strings.NewReader("csrf_token="+token))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cookie", cookie)
	result := httptest.NewRecorder()
	manager.Middleware(manager.Protect(handler)).ServeHTTP(result, req)
	if result.Code != http.StatusOK || invoked != 1 {
		t.Fatalf("nested managed mount status=%d invoked=%d body=%s", result.Code, invoked, result.Body.String())
	}
}

func TestManagedCapabilityUsesSelectedHostAndMethodMount(t *testing.T) {
	var invoked int
	router := NewRouter()
	if err := router.RegisterManagedPOST("selected", action.Config{}, func(*action.Context) (action.Result, error) {
		invoked++
		return action.Result{OK: true}, nil
	}); err != nil {
		t.Fatal(err)
	}
	managed, err := router.BuildChecked()
	if err != nil {
		t.Fatal(err)
	}
	app := server.New()
	app.Mount("POST managed.example/", managed)
	handler := app.Build()
	manager, token, cookie := routeManagedSession(t)

	matched := httptest.NewRequest(http.MethodPost, "http://managed.example/gosx/action/selected", strings.NewReader("csrf_token="+token))
	matched.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	matched.Header.Set("Cookie", cookie)
	matchedResult := httptest.NewRecorder()
	manager.Middleware(manager.Protect(handler)).ServeHTTP(matchedResult, matched)
	if matchedResult.Code != http.StatusOK || invoked != 1 {
		t.Fatalf("selected host/method mount status=%d invoked=%d body=%s", matchedResult.Code, invoked, matchedResult.Body.String())
	}

	wrongHost := httptest.NewRequest(http.MethodPost, "http://other.example/gosx/action/selected", strings.NewReader("csrf_token=wrong"))
	wrongHost.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	wrongHost.Header.Set("Cookie", cookie)
	wrongHostResult := httptest.NewRecorder()
	manager.Middleware(manager.Protect(handler)).ServeHTTP(wrongHostResult, wrongHost)
	if wrongHostResult.Code != http.StatusForbidden || invoked != 1 {
		t.Fatalf("non-selected host status=%d invoked=%d body=%s", wrongHostResult.Code, invoked, wrongHostResult.Body.String())
	}

	get := httptest.NewRequest(http.MethodGet, "http://managed.example/gosx/action/selected", nil)
	get.Header.Set("Cookie", cookie)
	getResult := httptest.NewRecorder()
	manager.Middleware(manager.Protect(handler)).ServeHTTP(getResult, get)
	if getResult.Code != http.StatusNotFound || invoked != 1 {
		t.Fatalf("non-selected method status=%d invoked=%d body=%s", getResult.Code, invoked, getResult.Body.String())
	}
}

func TestManagedCapabilityUsesSelectedOverlappingMount(t *testing.T) {
	var managedInvoked, rawInvoked int
	router := NewRouter()
	if err := router.RegisterManagedPOST("overlap", action.Config{}, func(*action.Context) (action.Result, error) {
		managedInvoked++
		return action.Result{OK: true}, nil
	}); err != nil {
		t.Fatal(err)
	}
	managed, err := router.BuildChecked()
	if err != nil {
		t.Fatal(err)
	}
	app := server.New()
	app.Mount("/gosx/action/", http.HandlerFunc(func(http.ResponseWriter, *http.Request) { rawInvoked++ }))
	app.Mount("/", managed)
	handler := app.Build()
	manager, _, cookie := routeManagedSession(t)

	valid := httptest.NewRequest(http.MethodPost, "/gosx/action/overlap", strings.NewReader("csrf_token=wrong"))
	valid.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	valid.Header.Set("Cookie", cookie)
	validResult := httptest.NewRecorder()
	manager.Middleware(manager.Protect(handler)).ServeHTTP(validResult, valid)
	if validResult.Code != http.StatusForbidden || managedInvoked != 0 || rawInvoked != 0 {
		t.Fatalf("specific raw overlap status=%d managed=%d raw=%d body=%s", validResult.Code, managedInvoked, rawInvoked, validResult.Body.String())
	}
}

func TestManagedCapabilityIsFrozenAgainstPostBuildMountMutation(t *testing.T) {
	var managedInvoked, lateInvoked int
	router := NewRouter()
	if err := router.RegisterManagedPOST("frozen", action.Config{}, func(*action.Context) (action.Result, error) {
		managedInvoked++
		return action.Result{OK: true}, nil
	}); err != nil {
		t.Fatal(err)
	}
	managed, err := router.BuildChecked()
	if err != nil {
		t.Fatal(err)
	}
	app := server.New()
	app.Mount("/", managed)
	handler := app.Build()
	// This route is deliberately added after Build. The returned handler must
	// continue using the immutable mount mux selected at Build time.
	app.Mount("POST /gosx/action/{name}", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		lateInvoked++
		w.WriteHeader(http.StatusTeapot)
	}))
	manager, token, cookie := routeManagedSession(t)
	req := httptest.NewRequest(http.MethodPost, "/gosx/action/frozen", strings.NewReader("csrf_token="+token))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cookie", cookie)
	result := httptest.NewRecorder()
	manager.Middleware(manager.Protect(handler)).ServeHTTP(result, req)
	if result.Code != http.StatusOK || managedInvoked != 1 || lateInvoked != 0 {
		t.Fatalf("post-build mount mutation status=%d managed=%d late=%d body=%s", result.Code, managedInvoked, lateInvoked, result.Body.String())
	}
}

func TestManagedCapabilityDoesNotCrossOpaqueShortCircuitMiddleware(t *testing.T) {
	var actionInvoked, middlewareInvoked int
	router := NewRouter()
	if err := router.RegisterManagedPOST("opaque", action.Config{}, func(*action.Context) (action.Result, error) {
		actionInvoked++
		return action.Result{OK: true}, nil
	}); err != nil {
		t.Fatal(err)
	}
	managed, err := router.BuildChecked()
	if err != nil {
		t.Fatal(err)
	}
	app := server.New()
	app.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			middlewareInvoked++
			http.Error(w, "opaque", http.StatusTeapot)
		})
	})
	app.Mount("/", managed)
	handler := app.Build()
	manager, _, cookie := routeManagedSession(t)
	req := httptest.NewRequest(http.MethodPost, "/gosx/action/opaque", strings.NewReader("csrf_token=wrong"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cookie", cookie)
	result := httptest.NewRecorder()
	manager.Middleware(manager.Protect(handler)).ServeHTTP(result, req)
	if result.Code != http.StatusForbidden || actionInvoked != 0 || middlewareInvoked != 0 {
		t.Fatalf("opaque middleware status=%d action=%d middleware=%d body=%s", result.Code, actionInvoked, middlewareInvoked, result.Body.String())
	}
}

func routeManagedSession(t *testing.T) (*session.Manager, string, string) {
	t.Helper()
	manager, err := session.New("route-managed-action-test-secret", session.Options{CookieName: "route_managed", AllowInsecure: true})
	if err != nil {
		t.Fatal(err)
	}
	var token string
	bootstrap := httptest.NewRecorder()
	manager.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
		token = session.Token(req)
	})).ServeHTTP(bootstrap, httptest.NewRequest(http.MethodGet, "/", nil))
	cookie := bootstrap.Header().Get("Set-Cookie")
	if semi := strings.IndexByte(cookie, ';'); semi >= 0 {
		cookie = cookie[:semi]
	}
	return manager, token, cookie
}
