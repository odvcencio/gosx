//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"golang.org/x/net/html"
)

// TestPrefixedProductionBuild checks the real production export and runtime,
// then repeats URL validation after cold and stale ISR regeneration.
func TestPrefixedProductionBuild(t *testing.T) {
	const prefix = "/.proxy/game"
	chrome := e2eChromePath(t)
	root := e2eRepoRoot(t)
	fixture := filepath.Join(t.TempDir(), "prefixed-app")
	copyFixtureTree(t, filepath.Join(root, "e2e", "testdata", "prefixed-app"), fixture)
	module := fmt.Sprintf("module example.com/prefixed-app\n\ngo 1.26\n\nrequire m31labs.dev/gosx v0.0.0\n\nreplace m31labs.dev/gosx => %s\n", filepath.ToSlash(root))
	if err := os.WriteFile(filepath.Join(fixture, "go.mod"), []byte(module), 0644); err != nil {
		t.Fatal(err)
	}
	build := exec.Command("go", "run", "./cmd/gosx", "build", "--prod", fixture)
	build.Dir = root
	build.Env = append(os.Environ(), "GOWORK=off")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("production build: %v\n%s", err, output)
	}
	dist := filepath.Join(fixture, "dist")
	var exported struct {
		BasePath string   `json:"basePath"`
		Pages    []string `json:"pages"`
		Routes   []struct {
			Path       string `json:"path"`
			File       string `json:"file"`
			Revalidate int    `json:"revalidateSeconds"`
		} `json:"routes"`
	}
	data, err := os.ReadFile(filepath.Join(dist, "export.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &exported); err != nil {
		t.Fatal(err)
	}
	if exported.BasePath != prefix || len(exported.Pages) != 3 || len(exported.Routes) != 3 {
		t.Fatalf("export: %+v", exported)
	}
	port := freeE2EPort(t)
	origin := fmt.Sprintf("http://127.0.0.1:%d", port)
	logs := startBuiltFixture(t, dist, port)
	if err := waitForHealthy(origin+prefix+"/readyz", 45*time.Second); err != nil {
		t.Fatalf("%v\n%s", err, logs.String())
	}
	client := &http.Client{Timeout: 10 * time.Second}
	get := func(target string) (string, string) {
		t.Helper()
		req, err := http.NewRequest("GET", target, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Accept", "text/html")
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		body, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != 200 {
			t.Fatalf("GET %s: %d\n%s\n%s", target, res.StatusCode, body, logs.String())
		}
		return string(body), res.Header.Get("X-GoSX-ISR")
	}
	for _, entry := range exported.Routes {
		if !strings.HasPrefix(entry.Path, prefix+"/") || !strings.HasPrefix(entry.File, ".proxy/game/") || entry.Revalidate != 60 {
			t.Fatalf("public export route: %+v", entry)
		}
		body, err := os.ReadFile(filepath.Join(dist, "static", filepath.FromSlash(entry.File)))
		if err != nil {
			t.Fatal(err)
		}
		publicURL := origin + strings.TrimRight(entry.Path, "/") + "/"
		for _, emitted := range prefixedPageURLs(t, string(body)) {
			resolved := resolvePrefixedURL(t, publicURL, emitted.value, prefix)
			if !emitted.websocket {
				get(resolved)
			}
		}
		if !strings.Contains(string(body), `name="gosx-base-path" content="/.proxy/game"`) {
			t.Fatal("export rewrote base path metadata")
		}
	}
	// A same-origin parent exercises framing without changing the secure default.
	upstream, _ := url.Parse(origin)
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	var escaped atomic.Int32
	var modelRequests, syncRequests atomic.Int32
	proxy.ModifyResponse = func(res *http.Response) error {
		if res.StatusCode == http.StatusSwitchingProtocols && res.Request.URL.Path == prefix+"/video-sync" {
			syncRequests.Add(1)
		}
		return nil
	}
	parent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<!doctype html><link rel="icon" href="%s/assets/wood.png"><iframe src="%s/" title="App"></iframe>`, prefix, prefix)
			return
		}
		if !strings.HasPrefix(r.URL.Path, prefix+"/") {
			escaped.Add(1)
		}
		if r.URL.Path == prefix+"/models/city.gltf" {
			modelRequests.Add(1)
		}
		proxy.ServeHTTP(w, r)
	}))
	defer parent.Close()
	page := newBrowserPage(t, chrome, map[string]any{"use-angle": "swiftshader", "enable-webgl": true}, 900, 600, "", 60*time.Second)
	page.navigate(t, parent.URL)
	frame := `document.querySelector("iframe").contentWindow`
	poll := func(expression string) {
		t.Helper()
		if err := chromedp.Run(page.ctx, chromedp.Poll(expression, nil, chromedp.WithPollingTimeout(15*time.Second))); err != nil {
			var diagnostic string
			page.eval(t, `JSON.stringify({href:document.querySelector("iframe").contentWindow.location.href, body:document.querySelector("iframe").contentDocument.body.innerHTML, scripts:[...document.querySelector("iframe").contentDocument.scripts].map(s=>({src:s.src,attrs:[...s.attributes].map(a=>[a.name,a.value])}))})`, &diagnostic)
			t.Fatalf("iframe condition %s: %v\n%s\n%s\nrequests: %v", expression, err, diagnostic, page.Console(), page.Requests())
		}
	}
	poll(frame + `.document.querySelector("#reactive-link")?.href.endsWith("/.proxy/game/news")`)
	poll(frame + `.__gosx?.islands?.size > 0`)
	poll(frame + `.document.querySelector("#video video[data-gosx-video=true]")?.getAttribute("src") === "/.proxy/game/media/movie.webm"`)
	poll(frame + `.document.querySelector("#video video")?.readyState >= 1`)
	poll(frame + `.document.querySelector("#video video")?.getAttribute("poster") === "/.proxy/game/assets/wood.png"`)
	poll(frame + `.document.querySelector("#scene")?.getAttribute("data-gosx-scene3d-model-hydration-committed") === "true"`)
	poll(frame + `.document.querySelector("#scene canvas") != null`)
	syncDeadline := time.Now().Add(5 * time.Second)
	for syncRequests.Load() == 0 && time.Now().Before(syncDeadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if modelRequests.Load() == 0 || syncRequests.Load() == 0 {
		t.Fatalf("built-in engine requests missing: models=%d sync=%d", modelRequests.Load(), syncRequests.Load())
	}
	page.eval(t, frame+`.document.querySelector("#update-link").click()`, nil)
	poll(frame + `.document.querySelector("#reactive-link")?.getAttribute("href") === "/.proxy/game/done"`)
	poll(frame + `.document.querySelector("#region-refreshed")?.textContent === "refreshed"`)
	// The mounted Scene3D engine loaded the shipped glTF chunk. Use its loader
	// with the actual engine props to check a compressed texture variant too.
	// Explicit renderer evidence makes the selection independent of test hardware.
	var texture string
	page.eval(t, `(async () => {
  const w = document.querySelector("iframe").contentWindow;
  const d = w.document;
  const engine = JSON.parse(d.querySelector("#gosx-manifest").textContent).engines.find(e => e.component === "GoSXScene3D");
  const loaded = await w.__gosx_scene3d_gltf_api.sceneLoadGLTFModel(engine.props.scene.models[0].src,{backend:"webgl",uploadReady:true,tokens:["device-feature:texture-compression-bc"]});
  const selected = loaded.objects[0].material.texture;
  if (!(await w.fetch(selected)).ok) throw new Error("selected variant is missing");
  return selected;
 })()`, &texture)
	if texture != parent.URL+prefix+"/assets/wood.bc7.ktx2" {
		t.Fatal("prefixed texture lookup failed", texture)
	}
	page.eval(t, frame+`.__gosx.telemetry.emit("info", "prefix-test", "URL check")`, nil)
	page.eval(t, frame+`.__gosx.telemetry.flush()`, nil)
	poll(frame + `.__gosx.telemetry.snapshot().serverAcceptedEvents > 0`)
	if escaped.Load() != 0 {
		t.Fatalf("%d browser requests escaped the prefix", escaped.Load())
	}
	artifact := filepath.Join(dist, "static", ".proxy", "game", "index.html")
	if err := os.Remove(artifact); err != nil {
		t.Fatal(err)
	}
	body, mode := get(origin + prefix + "/")
	if mode != "MISS" {
		t.Fatal("cold ISR did not regenerate", mode)
	}
	check := func(body string) {
		t.Helper()
		for _, emitted := range prefixedPageURLs(t, body) {
			resolved := resolvePrefixedURL(t, origin+prefix+"/", emitted.value, prefix)
			if !emitted.websocket {
				get(resolved)
			}
		}
		if !strings.Contains(body, `data-gosx-region-url="/.proxy/game/fragment"`) || !strings.Contains(body, `href="/.proxy/game/news"`) {
			t.Fatal("ISR lost URL prefix", body)
		}
	}
	check(body)
	res, err := client.Post(origin+prefix+"/invalidate", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 204 {
		t.Fatal(res.StatusCode)
	}
	_, mode = get(origin + prefix + "/")
	if mode != "STALE" {
		t.Fatal("stale ISR did not refresh", mode)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		body, mode = get(origin + prefix + "/")
		if mode == "HIT" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if mode != "HIT" {
		t.Fatal("stale ISR refresh did not finish")
	}
	check(body)
}

func resolvePrefixedURL(t *testing.T, base, value, prefix string) string {
	t.Helper()
	origin, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := url.Parse(value)
	if err != nil {
		t.Fatal(err)
	}
	resolved := origin.ResolveReference(ref)
	if resolved.Host == origin.Host && !strings.HasPrefix(resolved.Path, prefix+"/") {
		t.Fatalf("URL escaped base path: %s resolved from %s", value, base)
	}
	return resolved.String()
}

type prefixedPageURL struct {
	value     string
	websocket bool
}

// Collect URL attributes and URL-bearing runtime/document contract fields,
// including built-in engine props. Custom props and texture keys stay opaque.
func prefixedPageURLs(t *testing.T, body string) []prefixedPageURL {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var urls []prefixedPageURL
	var jsonURLs func(any, bool)
	jsonURLs = func(value any, engineProps bool) {
		switch value := value.(type) {
		case map[string]any:
			for key, child := range value {
				if key == "props" {
					if value["kind"] == "video" || value["component"] == "GoSXScene3D" {
						jsonURLs(child, true)
					}
					continue
				}
				known := key == "path" || key == "programRef" || key == "uri" || key == "url" || strings.HasSuffix(key, "Path") || strings.HasSuffix(key, "URL")
				if engineProps {
					switch key {
					case "src", "poster", "sync", "previewSrc", "fullSrc", "envMap", "texture", "normalMap", "roughnessMap", "metalnessMap", "occlusionMap", "emissiveMap", "endpoint", "refreshEndpoint":
						known = true
					}
				}
				if text, ok := child.(string); ok && known && text != "" {
					urls = append(urls, prefixedPageURL{value: text, websocket: engineProps && key == "sync"})
				} else {
					jsonURLs(child, engineProps)
				}
			}
		case []any:
			for _, child := range value {
				jsonURLs(child, engineProps)
			}
		}
	}
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		id := ""
		for _, attr := range node.Attr {
			if strings.HasPrefix(attr.Key, "data-gosx-scene3d-") && strings.HasSuffix(attr.Key, "-url") {
				urls = append(urls, prefixedPageURL{value: attr.Val})
			}
			switch attr.Key {
			case "id":
				id = attr.Val
			case "href", "src", "action", "formaction", "poster", "data-gosx-region-url", "data-gosx-engine-bytecode":
				if attr.Val != "" {
					urls = append(urls, prefixedPageURL{value: attr.Val})
				}
			case "srcset":
				for _, candidate := range strings.Split(attr.Val, ",") {
					if fields := strings.Fields(candidate); len(fields) > 0 {
						urls = append(urls, prefixedPageURL{value: fields[0]})
					}
				}
			}
		}
		if node.Data == "script" && (id == "gosx-manifest" || id == "gosx-document") && node.FirstChild != nil {
			var payload map[string]any
			if err := json.Unmarshal([]byte(node.FirstChild.Data), &payload); err != nil {
				t.Fatal(err)
			}
			if id == "gosx-document" {
				jsonURLs(payload["assets"], false)
			} else {
				jsonURLs(payload, false)
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	if len(urls) == 0 {
		t.Fatal("page emitted no URLs")
	}
	return urls
}
