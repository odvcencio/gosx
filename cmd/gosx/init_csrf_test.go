package main

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/html"
	"m31labs.dev/gosx/internal/localapp"
)

func TestRunInitStarterFormPrerenderAndCSRF(t *testing.T) {
	if raceDetectorEnabled {
		t.Skip("builds a scaffold server in a subprocess")
	}
	dir := filepath.Join(t.TempDir(), "starter-csrf")
	if err := RunInit(dir, "example.com/starter-csrf", ""); err != nil {
		t.Fatal(err)
	}
	addLocalGoSXReplace(t, dir)
	mustWriteFile(t, filepath.Join(dir, "app", "dynamic", "route.config.json"), `{"prerender":false}`)
	mustWriteFile(t, filepath.Join(dir, "app", "dynamic", "page.gsx"), `package dynamic

func Page() Node {
	return <main><input name="csrf_token" value={csrf.token}></input></main>
}
`)
	mustWriteFile(t, filepath.Join(dir, "app", "private", "page.gsx"), `package private

func Page() Node {
	return <main><input name="csrf_token" value={csrf.token}></input></main>
}
`)
	mustWriteFile(t, filepath.Join(dir, "app", "private", "page.server.go"), `package private

import (
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/session"
)

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			session.Current(ctx.Request).Set("form", "active")
			return nil, nil
		},
	}); err != nil { panic(err) }
}
`)
	tidyModule(t, dir)
	if err := RunExport(dir); err != nil {
		t.Fatal(err)
	}
	index := readFile(t, filepath.Join(dir, "dist", "static", "index.html"))
	if got := starterCSRFToken(t, index); got != "" {
		t.Fatal("prerendered home contains a build-time token")
	}
	if _, err := os.Stat(filepath.Join(dir, "dist", "static", "private", "index.html")); !os.IsNotExist(err) {
		t.Fatal("prerendering retained a session-creating page")
	}
	manifest := readFile(t, filepath.Join(dir, "dist", "export.json"))
	if strings.Contains(manifest, `"/private"`) || strings.Contains(manifest, `"/dynamic"`) {
		t.Fatal("dynamic pages entered the ISR manifest")
	}
	port, err := pickFreePort()
	if err != nil {
		t.Fatal(err)
	}
	base := "http://127.0.0.1:" + port
	binary := filepath.Join(t.TempDir(), "server")
	if built, err := buildServerBinaryIfPresent(dir, binary); err != nil || !built {
		t.Fatalf("build scaffold server: built=%t err=%v", built, err)
	}
	cmd := exec.Command(binary)
	cmd.Dir = dir
	localEnv, err := localapp.Environment(os.Environ(), port)
	if err != nil {
		t.Fatal(err)
	}
	cmd.Env = append(localEnv, "PUBLIC_URL="+base, "GOSX_APP_ROOT="+cmd.Dir, "GOWORK=off", "GOSX_STATIC_EXPORT=")
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	if err := waitForAppReady(base, 20*time.Second); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	request := func(method, path string, cookie *http.Cookie, token, site, origin string) (*http.Response, string) {
		t.Helper()
		var body io.Reader
		if method == http.MethodPost {
			body = strings.NewReader(url.Values{"email": {"reader@example.com"}, "csrf_token": {token}}.Encode())
		}
		r, err := http.NewRequest(method, base+path, body)
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Accept", "text/html")
		r.Header.Set("Sec-Fetch-Site", site)
		r.Header.Set("Origin", origin)
		if method == http.MethodPost {
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		if cookie != nil {
			r.AddCookie(cookie)
		}
		res, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(res.Body)
		res.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		return res, string(data)
	}
	res, body := request(http.MethodGet, "/", nil, "", "", "")
	if res.StatusCode != http.StatusOK || res.Header.Get("X-GoSX-ISR") != "HIT" || len(res.Cookies()) != 0 || starterCSRFToken(t, body) != "" {
		t.Fatalf("anonymous ISR response: status=%d headers=%v", res.StatusCode, res.Header)
	}
	if !strings.Contains(res.Header.Get("Cache-Control"), "public") || !strings.Contains(strings.Join(res.Header.Values("Vary"), ","), "Cookie") {
		t.Fatalf("anonymous ISR cache headers = %v", res.Header)
	}
	res, _ = request(http.MethodPost, "/__actions/subscribe", nil, "", "same-origin", base)
	if res.StatusCode != http.StatusSeeOther || len(res.Cookies()) != 1 {
		t.Fatalf("fresh form POST: status=%d headers=%v", res.StatusCode, res.Header)
	}
	cookie := res.Cookies()[0]
	res, body = request(http.MethodGet, "/", cookie, "", "", "")
	token := starterCSRFToken(t, body)
	if res.StatusCode != http.StatusOK || res.Header.Get("X-GoSX-ISR") != "" || res.Header.Get("Cache-Control") != "private, no-store" || token == "" {
		t.Fatalf("session home response: status=%d headers=%v", res.StatusCode, res.Header)
	}
	if !strings.Contains(body, "Form submission completed") || !strings.Contains(body, "session-backed flashes") {
		t.Fatal("session home did not render form state and flashes")
	}
	for _, tc := range []struct {
		name, token, site, origin string
		cookie                    *http.Cookie
		want                      int
	}{
		{"existing session", token, "same-origin", base, cookie, 303},
		{"origin only anonymous", "", "", base, nil, 303},
		{"metadata only anonymous", "", "same-origin", "", nil, 303},
		{"cross site anonymous", "", "cross-site", "", nil, 403},
		{"cross site with token", token, "cross-site", "", cookie, 403},
		{"foreign origin", token, "", "https://foreign.example", cookie, 403},
		{"contradictory origin", token, "same-origin", "https://foreign.example", cookie, 403},
		{"session missing token", "", "same-origin", base, cookie, 403},
		{"session invalid token", "invalid", "same-origin", base, cookie, 403},
		{"legacy missing token", "", "", "", nil, 403},
		{"legacy session token", token, "", "", cookie, 303},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, _ := request(http.MethodPost, "/__actions/subscribe", tc.cookie, tc.token, tc.site, tc.origin)
			if res.StatusCode != tc.want {
				t.Fatalf("POST status = %d, want %d", res.StatusCode, tc.want)
			}
		})
	}
	res, body = request(http.MethodGet, "/dynamic", nil, "", "", "")
	if res.StatusCode != http.StatusOK || res.Header.Get("X-GoSX-ISR") != "" || len(res.Cookies()) != 0 || starterCSRFToken(t, body) != "" || !strings.Contains(res.Header.Get("Cache-Control"), "public") {
		t.Fatalf("anonymous dynamic response: status=%d headers=%v", res.StatusCode, res.Header)
	}
	res, body = request(http.MethodGet, "/private", nil, "", "", "")
	if res.StatusCode != http.StatusOK || res.Header.Get("X-GoSX-ISR") != "" || res.Header.Get("Cache-Control") != "private, no-store" || starterCSRFToken(t, body) == "" {
		t.Fatalf("session-creating response: status=%d headers=%v", res.StatusCode, res.Header)
	}
}

func starterCSRFToken(t *testing.T, body string) string {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var find func(*html.Node) (string, bool)
	find = func(n *html.Node) (string, bool) {
		if n.Type == html.ElementNode && n.Data == "input" {
			name, value := "", ""
			for _, attr := range n.Attr {
				if attr.Key == "name" {
					name = attr.Val
				} else if attr.Key == "value" {
					value = attr.Val
				}
			}
			if name == "csrf_token" {
				return value, true
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			if value, ok := find(child); ok {
				return value, true
			}
		}
		return "", false
	}
	value, ok := find(doc)
	if !ok {
		t.Fatal("HTML has no csrf_token field")
	}
	return value
}

func TestFetchExportPageRejectsPrivateHTML(t *testing.T) {
	for _, tc := range []struct {
		name, cache, cookie string
		private             bool
	}{
		{"cookie", "public, max-age=60", "session=example", true},
		{"private", "Private, max-age=60", "", true},
		{"no store", "no-store", "", true},
		{"no cache", "no-cache", "", true},
		{"public", "public, max-age=0, must-revalidate", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Cache-Control", tc.cache)
				if tc.cookie != "" {
					w.Header().Set("Set-Cookie", tc.cookie)
				}
				_, _ = io.WriteString(w, "example HTML")
			}))
			defer s.Close()
			body, err := fetchExportPage(s.Client(), s.URL)
			if errors.Is(err, errPrivateExportPage) != tc.private || (tc.private && body != "") {
				t.Fatalf("export returned body=%q error=%v", body, err)
			}
		})
	}
}
