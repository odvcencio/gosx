//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/scene"
)

func TestScene3DOverlayFollowsTargetAndAcceptsPointerInput(t *testing.T) {
	root, chrome := e2eRepoRoot(t), e2eChromePath(t)
	overlay := scene.Overlay{ID: "bubble", Target: "seat", Position: scene.Vec3(0, .6, 0), Width: 2, Height: .5, Interactive: true,
		Content: gosx.El("button", gosx.Attrs(gosx.Attr("id", "bubble-action"), gosx.Attr("type", "button")), gosx.Text("Your turn"))}
	props := scene.Props{Width: 400, Height: 300, Responsive: scene.Bool(false), MaxDevicePixelRatio: 1, RequireWebGL: scene.Bool(true), ForceWebGL: scene.Bool(true), Controls: "none", Background: "#08151f", Camera: scene.PerspectiveCamera{Position: scene.Vec3(0, 0, 6), FOV: 60}, Graph: scene.NewGraph(scene.Mesh{ID: "seat", Geometry: scene.BoxGeometry{Width: .5, Height: .2, Depth: .5}}, overlay)}
	payload, err := json.Marshal(props)
	if err != nil {
		t.Fatal(err)
	}
	commands, err := json.Marshal([]scene.Command{{Kind: scene.CommandSetTransform, ObjectID: "seat", Data: map[string]any{"x": 1.5, "rotationZ": math.Pi / 2}}})
	if err != nil {
		t.Fatal(err)
	}
	markup := fmt.Sprintf(`<!doctype html><html><body style="margin:0"><div id="scene" style="position:relative;width:400px;height:300px"></div>
<script src="/gosx/bootstrap-runtime.js"></script>
<script data-gosx-script="feature-scene3d" data-gosx-scene3d-webgl-url="/gosx/bootstrap-feature-scene3d-webgl.js" data-gosx-scene3d-command-url="/gosx/bootstrap-feature-scene3d-command.js" src="/gosx/bootstrap-feature-scene3d.js"></script>
<script>window.update=%s;window.ready=(async()=>{window.handle=await window.__gosx_engine_factories.GoSXScene3D({mount:document.getElementById('scene'),emit(){},props:%s});window.mounted=true;})().catch(error=>{window.mountError=String(error);});</script></body></html>`, commands, payload)
	mux := http.NewServeMux()
	mux.Handle("/gosx/", http.StripPrefix("/gosx/", http.FileServer(http.Dir(filepath.Join(root, "client", "js")))))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(markup))
	})
	app := httptest.NewServer(mux)
	defer app.Close()
	page := newBrowserPage(t, chrome, map[string]any{"use-gl": "angle", "use-angle": "swiftshader", "enable-unsafe-swiftshader": true, "enable-webgl": true}, 500, 400, "", 60*time.Second)
	page.navigate(t, app.URL)
	page.waitFor(t, `window.mounted || window.mountError`, 20*time.Second, "scene mount")
	var mountError string
	page.eval(t, `window.mountError || ''`, &mountError)
	if mountError != "" {
		t.Fatalf("mount: %s\n%s", mountError, page.Console())
	}
	page.waitFor(t, `document.getElementById('bubble-action') !== null`, 10*time.Second, "Go-authored button")
	var accessible bool
	page.eval(t, `document.getElementById('bubble-action').closest('[aria-hidden="true"]') === null`, &accessible)
	if !accessible {
		t.Fatal("overlay ancestor hides interactive content from accessibility")
	}
	page.eval(t, `window.before=document.getElementById('bubble-action').getBoundingClientRect().toJSON();window.clicks=0;document.getElementById('bubble-action').addEventListener('click',()=>window.clicks++);`, nil)
	if err := chromedp.Run(page.ctx, chromedp.Click("#bubble-action", chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	var clicks int
	page.eval(t, `window.clicks`, &clicks)
	if clicks != 1 {
		t.Fatal("overlay did not accept pointer input")
	}
	page.eval(t, `window.__gosx.scene3d.dispatchCommands(handle,window.update).then(()=>true)`, nil)
	page.waitFor(t, `document.getElementById('bubble-action').getBoundingClientRect().x > window.before.x + 20 && document.getElementById('bubble-action').getBoundingClientRect().y > window.before.y + 10`, 10*time.Second, "translated and rotated target")
	page.eval(t, `window.__gosx.scene3d.dispatchCommands(handle,[{kind:1,objectId:'seat'}]).then(()=>true)`, nil)
	page.waitFor(t, `document.getElementById('bubble-action') === null`, 10*time.Second, "missing target cleanup")
}
