//go:build e2e

package e2e

import (
	"bytes"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

const sceneTimelineHTML = `<!doctype html><html><body style="margin:0">
<div id="scene" style="width:400px;height:300px"></div>
<script src="/gosx/bootstrap-runtime.js"></script>
<script data-gosx-script="feature-scene3d"
 data-gosx-scene3d-webgl-url="/gosx/bootstrap-feature-scene3d-webgl.js"
 data-gosx-scene3d-command-url="/gosx/bootstrap-feature-scene3d-command.js"
 data-gosx-scene3d-timeline-url="/activity/runtime/timeline.js?v=plan1"
 src="/gosx/bootstrap-feature-scene3d.js"></script>
<script>
window.ready = (async () => {
 window.handle = await window.__gosx_engine_factories.GoSXScene3D({
  mount: document.getElementById('scene'), emit() {},
  props: { width:400,height:300,responsive:false,maxDevicePixelRatio:1,timelines:true,
   requireWebGL:true,forceWebGL:true,controls:'orbit',background:'#08151f',
   scene: {camera:{x:0,y:0,z:6,fov:60},objects:[{
    id:'piece',kind:'box',width:1,height:1,depth:1,x:-1,y:0,z:0,color:'#ffbb33'
   }]}}
 });
 window.mounted = true;
})().catch(error => { window.mountError = String(error); });
window.plan = {version:1,id:'place',tweens:[
 {node:'piece',property:'x',from:-1,to:1,at:0,duration:0.4,ease:{kind:2}},
 {camera:true,property:'y',from:0,to:0.5,at:0,duration:0.4,ease:{kind:0}}
]};
</script></body></html>`

func TestScene3DTimelineLazyPlaybackInBrowser(t *testing.T) {
	root, chrome := e2eRepoRoot(t), e2eChromePath(t)
	var timelineRequests atomic.Int32
	mux := http.NewServeMux()
	mux.Handle("/gosx/", http.StripPrefix("/gosx/", http.FileServer(http.Dir(filepath.Join(root, "client", "js")))))
	mux.HandleFunc("/activity/runtime/timeline.js", func(w http.ResponseWriter, r *http.Request) {
		timelineRequests.Add(1)
		if r.URL.Query().Get("v") != "plan1" {
			t.Error("timeline lost its advertised version")
		}
		http.ServeFile(w, r, filepath.Join(root, "client", "js", "bootstrap-feature-scene3d-timeline.js"))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(sceneTimelineHTML))
	})
	app := httptest.NewServer(mux)
	defer app.Close()
	page := newBrowserPage(t, chrome, map[string]any{
		"use-gl": "angle", "use-angle": "swiftshader", "enable-unsafe-swiftshader": true, "enable-webgl": true,
	}, 500, 400, "", 60*time.Second)
	page.navigate(t, app.URL)
	page.waitFor(t, `window.mounted || window.mountError`, 20*time.Second, "scene mount")
	var mountError string
	page.eval(t, `window.mountError || ''`, &mountError)
	if mountError != "" {
		t.Fatalf("mount: %s\n%s", mountError, page.Console())
	}
	if timelineRequests.Load() != 0 {
		t.Fatal("ordinary scene startup fetched the timeline")
	}
	var backend string
	page.eval(t, `document.getElementById("scene").getAttribute("data-gosx-scene3d-backend")`, &backend)
	if backend != "webgl" {
		t.Fatalf("expected real WebGL rendering, got %s", backend)
	}
	before := page.screenshotElement(t, "#scene canvas")
	page.eval(t, `(async () => { window.playback = await window.__gosx.scene3d.playTimeline(handle, window.plan); playback.pause(); playback.seek(0.1); return true; })()`, nil)
	page.waitFor(t, `document.getElementById('scene').__gosxScene3DState.objects.get('piece').x > -1`, 5*time.Second, "tween sample")
	page.eval(t, `playback.finish(); true`, nil)
	var finished bool
	page.eval(t, `playback.finished.then(result => result.finished)`, &finished)
	if !finished || timelineRequests.Load() != 1 {
		t.Fatal("timeline did not finish after exactly one lazy fetch")
	}
	page.waitFor(t, `Math.abs(handle.getCamera().y - 0.5) < 1e-6`, 5*time.Second, "settled camera controls")
	page.waitFor(t, `document.getElementById('scene').__gosxScene3DState.objects.get('piece').x === 1`, 5*time.Second, "settled piece")
	after := page.screenshotElement(t, "#scene canvas")
	if bytes.Equal(before, after) {
		t.Fatal("timeline did not change rendered pixels")
	}
	img, err := png.Decode(bytes.NewReader(after))
	if err != nil {
		t.Fatal(err)
	}
	colors := map[uint32]bool{}
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			colors[r<<16|g<<8|b] = true
		}
	}
	if len(colors) < 2 {
		t.Fatal("timeline screenshot is blank")
	}
	if dir := os.Getenv("GOSX_TIMELINE_EVIDENCE_DIR"); dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "timeline.png"), after, 0644); err != nil {
			t.Fatal(err)
		}
	}
}
