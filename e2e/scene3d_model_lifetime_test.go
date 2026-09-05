//go:build e2e

package e2e

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"image/png"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

const modelLifetimeHTML = `<!doctype html><html><body style="margin:0;background:#000">
<script src="/gosx/bootstrap-runtime.js"></script>
<script src="/gosx/bootstrap-feature-engines.js"></script>
<script>
const register = window.__gosx_register_engine_factory;
window.__gosx_register_engine_factory = function(name, factory) {
 if (name === "GoSXScene3D") window.modelFactory = factory;
 return register.apply(this, arguments);
};
</script>
<script src="/gosx/bootstrap-feature-scene3d.js"></script>
<script src="/gosx/bootstrap-feature-scene3d-webgl.js"></script>
<script>
window.openModel = async function(src) {
 const mount = document.createElement("div"); mount.style="width:240px;height:240px";
 document.body.appendChild(mount);
 const handle = await window.modelFactory({mount, props:{width:240,height:240,preferWebGPU:false,forceWebGL:true,requireWebGL:true,
   background:"#000000",camera:{x:0,y:0,z:4},models:[{id:"model",src}]},emit:function(){}});
 if (!handle.dispose) throw Error("missing actual mounted handle");
 if(mount.getAttribute('data-gosx-scene3d-renderer')!=='webgl') throw Error('not WebGL: '+mount.getAttribute('data-gosx-scene3d-renderer'));
 return {mount,handle};
};
window.closeModel = function(entry) { entry.handle.dispose(); entry.mount.remove(); };
window.pendingModels = function(ids, src) {
 const manifest=document.createElement('script');manifest.id='gosx-manifest';manifest.type='application/json';
 const engines=ids.map(id=>{const mount=document.createElement('div');mount.id=id;mount.style='width:240px;height:240px';document.body.appendChild(mount);
 return {id,component:'GoSXScene3D',kind:'surface',mountId:id,props:{width:240,height:240,forceWebGL:true,requireWebGL:true,models:[{id:'model',src}]}};});
 manifest.textContent=JSON.stringify({engines});document.body.appendChild(manifest);
 return {boot:window.__gosx_bootstrap_page(),manifest};
};
window.waitRequests = async function(field, count) {
 for(let i=0;i<100;i++){const value=await (await fetch('/counts')).json();if(value[field]===count)return;await new Promise(r=>setTimeout(r,10));}
 throw Error('request count not reached '+field);
};
window.waitModel = async function(entry) {
 for(let i=0;i<200;i++) {
  if(entry.mount.__gosxScene3DState && entry.mount.__gosxScene3DState.objects.size) {
   await new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r))); return;
  }
  await new Promise(r=>setTimeout(r,10));
 }
 throw Error("model not hydrated");
};
window.runModelCycles = async function() {
 for(let i=0;i<100;i++) {
  const entry=await openModel('/model.gltf'); await waitModel(entry); closeModel(entry);
  const s=__gosx.scene3d.modelAssetResidency();
  if(s.entries||s.pending||s.owners) throw Error('retired GLTF scope retained '+JSON.stringify(s));
 }
 const slow=pendingModels(['initial-slow'],'/slow.json');await waitRequests('started',1);
 if(document.getElementById('initial-slow').__gosxScene3DHandle) throw Error('factory already resolved');
 __gosx_dispose_engine('initial-slow');await slow.boot;await waitRequests('aborted',1);
 if(document.getElementById('initial-slow').__gosxScene3DHandle) throw Error('late initial handle');
 slow.manifest.remove();document.getElementById('initial-slow').remove();
 const shared=pendingModels(['shared-a','shared-b'],'/shared.json');await waitRequests('shared',1);
 for(let i=0;i<100 && __gosx.scene3d.modelAssetResidency().owners<2;i++)await new Promise(r=>setTimeout(r,1));
 if(__gosx.scene3d.modelAssetResidency().owners!==2)throw Error('both shared owners not attached');
 __gosx_dispose_engine('shared-a');await shared.boot;
 if(__gosx.scene3d.modelAssetResidency().owners!==1)throw Error('survivor lost ownership');
 const b={mount:document.getElementById('shared-b')};await waitModel(b);
 if(b.mount.getAttribute('data-gosx-scene3d-renderer')!=='webgl') throw Error('not WebGL');
 __gosx_dispose_engine('shared-b');shared.manifest.remove();
 document.getElementById('shared-a').remove();b.mount.remove();
 window.finalModel=await openModel('/model.gltf'); await waitModel(finalModel);
 return __gosx.scene3d.modelAssetResidency();
};
</script></body></html>`

func TestScene3DModelLifetimeServedWebGL(t *testing.T) {
	var slowStarted, slowAborted, sharedRequests atomic.Int32
	root := e2eRepoRoot(t)
	mux := http.NewServeMux()
	mux.Handle("/gosx/", http.StripPrefix("/gosx/", http.FileServer(http.Dir(filepath.Join(root, "client", "js")))))
	model := func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"objects":[{"id":"visible","kind":"box","size":1.5,"material":{"kind":"flat","color":"#ff2020"}}]}`))
	}
	mux.HandleFunc("/model.json", func(w http.ResponseWriter, r *http.Request) { model(w) })
	var vertices bytes.Buffer
	if err := binary.Write(&vertices, binary.LittleEndian, []float32{-0.8, -0.8, 0, 0.8, -0.8, 0, 0, 0.8, 0}); err != nil {
		t.Fatal(err)
	}
	gltf := map[string]any{
		"asset": map[string]string{"version": "2.0"}, "scene": 0, "scenes": []any{map[string]any{"nodes": []int{0}}},
		"nodes":          []any{map[string]int{"mesh": 0}},
		"buffers":        []any{map[string]any{"uri": "data:application/octet-stream;base64," + base64.StdEncoding.EncodeToString(vertices.Bytes()), "byteLength": 36}},
		"bufferViews":    []any{map[string]int{"buffer": 0, "byteOffset": 0, "byteLength": 36}},
		"accessors":      []any{map[string]any{"bufferView": 0, "componentType": 5126, "count": 3, "type": "VEC3", "min": []float64{-.8, -.8, 0}, "max": []float64{.8, .8, 0}}},
		"meshes":         []any{map[string]any{"primitives": []any{map[string]any{"attributes": map[string]int{"POSITION": 0}, "material": 0}}}},
		"materials":      []any{map[string]any{"doubleSided": true, "extensions": map[string]any{"KHR_materials_unlit": map[string]any{}}, "pbrMetallicRoughness": map[string]any{"baseColorFactor": []float64{1, .05, .05, 1}}}},
		"extensionsUsed": []string{"KHR_materials_unlit"},
	}
	mux.HandleFunc("/model.gltf", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "model/gltf+json")
		_ = json.NewEncoder(w).Encode(gltf)
	})
	mux.HandleFunc("/counts", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]int32{"started": slowStarted.Load(), "aborted": slowAborted.Load(), "shared": sharedRequests.Load()})
	})
	mux.HandleFunc("/slow.json", func(w http.ResponseWriter, r *http.Request) {
		slowStarted.Add(1)
		select {
		case <-r.Context().Done():
			slowAborted.Add(1)
		case <-time.After(5 * time.Second):
			model(w)
		}
	})
	mux.HandleFunc("/shared.json", func(w http.ResponseWriter, r *http.Request) {
		sharedRequests.Add(1)
		select {
		case <-r.Context().Done():
			return
		case <-time.After(150 * time.Millisecond):
			model(w)
		}
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(modelLifetimeHTML)) })
	server := httptest.NewServer(mux)
	defer server.Close()
	page := newBrowserPage(t, e2eChromePath(t), map[string]any{"use-gl": "angle", "use-angle": "swiftshader", "enable-unsafe-swiftshader": true, "enable-webgl": true, "ignore-gpu-blocklist": true}, 300, 300, "", 90*time.Second)
	page.navigate(t, server.URL)
	page.waitFor(t, `!!window.modelFactory && !!window.__gosx.scene3d.modelAssetResidency`, 10*time.Second, "model runtime")
	page.eval(t, `(async()=>{window.firstModel=await openModel('/model.gltf');await waitModel(firstModel);return firstModel.mount.getAttribute('data-gosx-scene3d-renderer')})()`, new(string))
	assertModelPixels := func() {
		t.Helper()
		var shot []byte
		if err := chromedp.Run(page.ctx, chromedp.CaptureScreenshot(&shot)); err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(bytes.NewReader(shot))
		if err != nil {
			t.Fatal(err)
		}
		red := 0
		for y := 0; y < img.Bounds().Dy(); y++ {
			for x := 0; x < img.Bounds().Dx(); x++ {
				r, g, b, _ := img.At(x, y).RGBA()
				if r > 20000 && r > g*2 && r > b*2 {
					red++
				}
			}
		}
		if red < 100 {
			t.Fatalf("visible model control has only %d red pixels; console=%s", red, page.Console())
		}
		t.Logf("visible model red pixels=%d", red)
	}
	assertModelPixels()
	var backendInfo map[string]string
	page.eval(t, `(()=>{const gl=firstModel.mount.querySelector('canvas').getContext('webgl2');if(!gl)throw Error('WebGL2 identity unavailable');const ext=gl.getExtension('WEBGL_debug_renderer_info');return {backend:firstModel.mount.getAttribute('data-gosx-scene3d-renderer'),renderer:gl.getParameter(ext?ext.UNMASKED_RENDERER_WEBGL:gl.RENDERER),vendor:gl.getParameter(ext?ext.UNMASKED_VENDOR_WEBGL:gl.VENDOR),userAgent:navigator.userAgent}})()`, &backendInfo)
	t.Logf("served backend identity=%v", backendInfo)
	page.eval(t, `closeModel(firstModel)`, nil)
	var stats map[string]int
	page.eval(t, `runModelCycles()`, &stats)
	assertModelPixels()
	if slowStarted.Load() != 1 || slowAborted.Load() != 1 || sharedRequests.Load() != 1 {
		t.Fatalf("request controls started=%d aborted=%d shared=%d", slowStarted.Load(), slowAborted.Load(), sharedRequests.Load())
	}
	if stats["entries"] > 17 || stats["pending"] != 0 {
		t.Fatalf("residency=%v", stats)
	}
	page.eval(t, `closeModel(finalModel); __gosx.scene3d.modelAssetResidency()`, &stats)
	if stats["owners"] != 0 || stats["pending"] != 0 || stats["entries"] > 16 {
		t.Fatalf("final residency=%v", stats)
	}
	if errors := page.PageErrors(); len(errors) > 0 {
		t.Fatalf("page errors: %v", errors)
	}
	t.Logf("100 same-URL GLTF fresh-scope remounts, initial server abort and surviving shared request passed; final cache counters=%v; no heap/GPU-byte measurement", stats)
}
