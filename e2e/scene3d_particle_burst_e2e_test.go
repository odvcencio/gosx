//go:build e2e

package e2e

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

const sceneParticleBurstHTML = `<!doctype html><html><body style="margin:0">
<div id="scene" style="width:400px;height:300px"></div>
<script src="/gosx/bootstrap-runtime.js"></script>
<script data-gosx-script="feature-scene3d"
 data-gosx-scene3d-webgl-url="/gosx/bootstrap-feature-scene3d-webgl.js"
 data-gosx-scene3d-command-url="/gosx/bootstrap-feature-scene3d-command.js"
 data-gosx-scene3d-compute-url="/gosx/bootstrap-feature-scene3d-compute.js?v=particles1"
 data-gosx-scene3d-particle-burst-url="/activity/runtime/burst.js?v=event1"
 src="/gosx/bootstrap-feature-scene3d.js"></script>
<script>
window.ready = (async () => {
 window.handle = await window.__gosx_engine_factories.GoSXScene3D({
  mount: document.getElementById('scene'), emit() {},
  props: {width:400,height:300,responsive:false,maxDevicePixelRatio:1,particleBursts:true,
   requireWebGL:true,forceWebGL:true,shaded:true,controls:'none',background:'#08151f',
   scene:{camera:{x:0,y:0,z:6,fov:60},objects:[{
    id:'piece',kind:'box',width:1,height:1,depth:1,x:-1,y:0,z:0,color:'#ffbb33'
   }]}}
 });
 window.mounted = true;
})().catch(error => { window.mountError = String(error); });
window.burst = {version:1,id:'impact',delay:0.05,duration:1.73,particles:{
 id:'gosx-burst/impact',count:32,emitter:{kind:'point',x:1,y:0.5,z:0,lifetime:1,once:true},
 material:{color:'#ffee88',size:8,sizeEnd:0,opacity:1,opacityEnd:0,minPixelSize:3,style:'glow'}
}};
</script></body></html>`

func TestScene3DParticleBurstLazyRenderingAndCleanup(t *testing.T) {
	root, chrome := e2eRepoRoot(t), e2eChromePath(t)
	var requests atomic.Int32
	mux := http.NewServeMux()
	mux.Handle("/gosx/", http.StripPrefix("/gosx/", http.FileServer(http.Dir(filepath.Join(root, "client", "js")))))
	mux.HandleFunc("/activity/runtime/burst.js", func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Query().Get("v") != "event1" {
			t.Error("burst lost its advertised version")
		}
		http.ServeFile(w, r, filepath.Join(root, "client", "js", "bootstrap-feature-scene3d-particle-burst.js"))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(sceneParticleBurstHTML))
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
	if requests.Load() != 0 {
		t.Fatal("ordinary startup fetched the burst chunk")
	}
	before := page.screenshotElement(t, "#scene canvas")
	page.eval(t, `(async () => { window.effect = await window.__gosx.scene3d.burstParticles(handle, burst); return true; })()`, nil)
	page.waitFor(t, `Number(document.getElementById('scene').getAttribute('data-gosx-scene3d-webgl-compute-particle-draw-instances')) > 0`, 5*time.Second, "rendered particles")
	after := page.screenshotElement(t, "#scene canvas")
	if bytes.Equal(before, after) {
		t.Fatal("burst did not change rendered pixels")
	}
	if dir := os.Getenv("GOSX_PARTICLE_BURST_EVIDENCE_DIR"); dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "particle-burst.png"), after, 0644); err != nil {
			t.Fatal(err)
		}
	}
	var finished bool
	page.eval(t, `effect.finished.then(result => result.finished && !result.suppressed)`, &finished)
	if !finished || requests.Load() != 1 {
		t.Fatal("burst did not finish after one lazy request")
	}
	page.waitFor(t, `document.getElementById('scene').__gosxScene3DState.computeParticles.length === 0 && document.getElementById('scene').getAttribute('data-gosx-scene3d-render-loop') === 'stopped'`, 5*time.Second, "burst cleanup and idle render loop")
}
