//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image/png"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"m31labs.dev/gosx/dev"
	"m31labs.dev/gosx/scene"
)

const devSwapSource = `material DevSwap {
    surface(geo) -> color {
        return rgb(%s)
    }
}
`

// TestSelenaDevHotSwapChangesShaderWithoutReload runs the dev proxy with its
// file watcher in front of a page that draws a Selena material, edits the .sel
// file on disk, and checks that the canvas changes color while the page keeps
// its JavaScript state (no reload). The hook stands in for gosx dev's
// devSelenaChange and compiles the edited file the same way.
//
//	/home/draco/work/.runtime/tools/gpu/gpu-chrome.sh run -- go test -tags e2e -run TestSelenaDevHotSwap -timeout 10m ./e2e/
func TestSelenaDevHotSwapChangesShaderWithoutReload(t *testing.T) {
	chrome := e2eChromePath(t)
	root := e2eRepoRoot(t)
	bootstrap, err := os.ReadFile(filepath.Join(root, "client/js/bootstrap.js"))
	if err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	source := filepath.Join(project, "devswap.sel")
	if err := os.WriteFile(source, []byte(fmt.Sprintf(devSwapSource, "1.0, 0.0, 0.0")), 0o644); err != nil {
		t.Fatal(err)
	}
	compile := func() (scene.CustomMaterial, error) {
		src, err := os.ReadFile(source)
		if err != nil {
			return scene.CustomMaterial{}, err
		}
		material, _, err := scene.CompileSelenaMaterial(src, scene.SelenaMaterialOptions{})
		return material, err
	}
	red, err := compile()
	if err != nil {
		t.Fatal(err)
	}
	yes := true
	props := scene.Props{
		Width: 240, Height: 160, Background: "#000000", AutoRotate: &yes, ForceWebGL: &yes, RequireWebGL: &yes,
		Camera: scene.PerspectiveCamera{Position: scene.Vector3{Z: 3}, FOV: 60, Near: 0.1, Far: 20},
		Graph:  scene.NewGraph(scene.Mesh{ID: "card", Geometry: scene.BoxGeometry{Width: 4, Height: 4, Depth: 0.1}, Material: red}),
	}
	manifest, err := json.Marshal(map[string]any{"engines": []any{map[string]any{
		"id": "dev-swap", "component": "GoSXScene3D", "kind": "surface", "mountId": "dev-swap-root", "props": props,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	page := []byte(fmt.Sprintf(`<!doctype html><html><head><meta charset="utf-8"><style>html,body{margin:0;background:#000}#dev-swap-root{width:240px;height:160px}</style></head><body><div id="dev-swap-root"></div><script id="gosx-manifest" type="application/json">%s</script><script src="/bootstrap.js"></script></body></html>`, manifest))
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write(page)
		case r.URL.Path == "/bootstrap.js":
			w.Header().Set("Content-Type", "application/javascript")
			_, _ = w.Write(bootstrap)
		case strings.HasPrefix(r.URL.Path, "/gosx/bootstrap-feature-") && strings.HasSuffix(r.URL.Path, ".js"):
			http.ServeFile(w, r, filepath.Join(root, "client", "js", filepath.Base(r.URL.Path)))
		default:
			http.NotFound(w, r)
		}
	}))
	defer app.Close()

	restarts := 0
	devServer := &dev.Server{
		Dir:          project,
		ProxyTarget:  app.URL,
		PollInterval: 50 * time.Millisecond,
		OnChange:     func() error { restarts++; return nil },
		SelenaChange: func(paths []string) ([]dev.SelenaUpdate, error) {
			material, err := compile()
			if err != nil {
				return nil, err
			}
			return []dev.SelenaUpdate{{Source: "devswap.sel", Materials: []map[string]any{{
				"material": "DevSwap", "customVertex": material.VertexGLSL, "customFragment": material.FragmentGLSL,
				"customVertexWGSL": material.VertexWGSL, "customFragmentWGSL": material.FragmentWGSL, "shaderLayout": material.ShaderLayout,
			}}}}, nil
		},
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	_ = listener.Close()
	go func() { _ = devServer.ListenAndServe(addr) }()
	defer devServer.Shutdown(t.Context())
	url := "http://" + addr
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if resp, err := http.Get(url + "/gosx/dev/info"); err == nil {
			resp.Body.Close()
			break
		}
	}
	if e2eWindowsRelay() {
		url = e2eExposeToWindows(t, url)
	}

	browser := newBrowserPage(t, chrome, map[string]any{"use-angle": "swiftshader", "enable-unsafe-swiftshader": true, "disable-features": "WebGPU"}, 320, 240, "", 120*time.Second)
	browser.navigate(t, url+"/")
	center := func() [3]int {
		var shot []byte
		if err := chromedp.Run(browser.ctx, chromedp.Screenshot("canvas", &shot, chromedp.ByQuery)); err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(bytes.NewReader(shot))
		if err != nil {
			t.Fatal(err)
		}
		b := img.Bounds()
		return pixel8(img, b.Min.X+b.Dx()/2, b.Min.Y+b.Dy()/2)
	}
	waitFor := func(label string, ok func([3]int) bool) [3]int {
		var px [3]int
		for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
			var ready string
			browser.eval(t, `document.getElementById('dev-swap-root').getAttribute('data-gosx-scene3d-ready') || ''`, &ready)
			if ready != "true" {
				continue
			}
			if px = center(); ok(px) {
				return px
			}
		}
		t.Fatalf("canvas never became %s: last pixel %v; errors=%v console=%s", label, px, browser.PageErrors(), browser.Console())
		return px
	}
	waitFor("red", func(p [3]int) bool { return p[0] > 150 && p[1] < 80 && p[2] < 80 })
	var injected bool
	browser.eval(t, `typeof window.__gosx_dev_selena_swap === "function"`, &injected)
	if !injected {
		t.Fatal("the dev proxy did not inject the Selena swap script")
	}
	browser.eval(t, `window.__devSwapMarker = 7; true`, &injected)

	if err := os.WriteFile(source, []byte(fmt.Sprintf(devSwapSource, "0.0, 1.0, 0.0")), 0o644); err != nil {
		t.Fatal(err)
	}
	px := waitFor("green", func(p [3]int) bool { return p[1] > 150 && p[0] < 80 && p[2] < 80 })
	var marker int
	browser.eval(t, `window.__devSwapMarker || 0`, &marker)
	if marker != 7 {
		t.Fatalf("the page reloaded during the hot swap (marker %d)", marker)
	}
	if restarts != 0 {
		t.Fatalf("the app restarted %d times for a .sel edit", restarts)
	}
	if errs := browser.PageErrors(); len(errs) != 0 {
		t.Fatalf("page errors: %v", errs)
	}
	t.Logf("hot swap turned the card from red to %v without a reload", px)
}
