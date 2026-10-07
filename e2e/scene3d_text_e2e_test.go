//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image/png"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"m31labs.dev/gosx/scene"
)

func TestScene3DTextRendersAndUpdatesThroughWorldTexture(t *testing.T) {
	root, chrome := e2eRepoRoot(t), e2eChromePath(t)
	text := scene.Text3D{ID: "score", Text: "12", Width: 3, Height: 1, Font: "96px monospace", Color: "#fff0b8", Rotation: scene.Euler{X: -math.Pi / 2}}
	props := scene.Props{Width: 400, Height: 300, Responsive: scene.Bool(false), MaxDevicePixelRatio: 1, RequireWebGL: scene.Bool(true), ForceWebGL: scene.Bool(true), Controls: "none", Background: "#08151f", Camera: scene.PerspectiveCamera{Position: scene.Vec3(0, 0, 6), FOV: 60}, Graph: scene.NewGraph(text)}
	payload, err := json.Marshal(props)
	if err != nil {
		t.Fatal(err)
	}
	next := text
	next.Text = "13"
	commands, err := json.Marshal(scene.DiffCommands(scene.NewGraph(text).SceneIR(), scene.NewGraph(next).SceneIR()))
	if err != nil {
		t.Fatal(err)
	}
	markup := fmt.Sprintf(`<!doctype html><html><body style="margin:0"><div id="scene" style="width:400px;height:300px"></div>
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
	page.waitFor(t, `document.querySelector('#scene [data-gosx-scene-html-texture-uploaded="1"]') !== null`, 10*time.Second, "glyph texture upload")
	before := page.screenshotElement(t, "#scene canvas")
	img, err := png.Decode(bytes.NewReader(before))
	if err != nil {
		t.Fatal(err)
	}
	colors := map[uint32]bool{}
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			colors[(r>>8)<<16|(g>>8)<<8|(b>>8)] = true
		}
	}
	if len(colors) < 8 {
		t.Fatal("glyphs did not appear in the rendered scene")
	}
	page.eval(t, `window.__gosx.scene3d.dispatchCommands(handle,window.update).then(()=>true)`, nil)
	page.waitFor(t, `document.getElementById('scene').__gosxScene3DState.html.get('score').html.includes('13') && Number(document.querySelector('#scene [data-gosx-scene-html-texture-revision]')?.getAttribute('data-gosx-scene-html-texture-revision')) >= 2`, 10*time.Second, "changed glyph texture")
	page.waitFor(t, `document.querySelector('#scene [data-gosx-scene-html-texture-uploaded="1"]') !== null`, 10*time.Second, "updated glyph upload")
	after := page.screenshotElement(t, "#scene canvas")
	if bytes.Equal(before, after) {
		t.Fatal("score update did not change rendered glyphs")
	}
}
