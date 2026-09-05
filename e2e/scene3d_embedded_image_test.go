//go:build e2e

package e2e

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/scene"
	"m31labs.dev/gosx/server"
)

// embeddedImageGLB uses one BIN chunk for positions, normals, UVs and PNG.
func embeddedImageGLB(t *testing.T, mode string) []byte {
	t.Helper()
	var bin bytes.Buffer
	views := []map[string]int{}
	add := func(data []byte) int {
		for bin.Len()%4 != 0 {
			bin.WriteByte(0)
		}
		views = append(views, map[string]int{"buffer": 0, "byteOffset": bin.Len(), "byteLength": len(data)})
		bin.Write(data)
		return len(views) - 1
	}
	floats := func(values []float32) int {
		var b bytes.Buffer
		if err := binary.Write(&b, binary.LittleEndian, values); err != nil {
			t.Fatal(err)
		}
		return add(b.Bytes())
	}
	pos := floats([]float32{-1, -1, 0, 1, -1, 0, 1, 1, 0, -1, 1, 0})
	norm := floats([]float32{0, 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1})
	uvs := []float32{0, 1, 1, 1, 1, 0, 0, 0}
	if mode == "flip" {
		for i := 0; i < len(uvs); i += 2 {
			uvs[i] = 1 - uvs[i]
		}
	}
	uv := floats(uvs)
	indices := add([]byte{0, 0, 1, 0, 2, 0, 0, 0, 2, 0, 3, 0})
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	palette := []color.RGBA{{255, 0, 0, 255}, {0, 255, 0, 255}, {0, 0, 255, 255}, {255, 255, 0, 255}}
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			img.SetRGBA(x, y, palette[(y/16)*2+x/16])
		}
	}
	var pngBytes bytes.Buffer
	if err := png.Encode(&pngBytes, img); err != nil {
		t.Fatal(err)
	}
	picture := add(pngBytes.Bytes())
	doc := map[string]any{
		"asset": map[string]string{"version": "2.0"}, "buffers": []any{map[string]int{"byteLength": bin.Len()}}, "bufferViews": views,
		"accessors": []any{map[string]any{"bufferView": pos, "componentType": 5126, "count": 4, "type": "VEC3", "min": []int{-1, -1, 0}, "max": []int{1, 1, 0}}, map[string]any{"bufferView": norm, "componentType": 5126, "count": 4, "type": "VEC3"}, map[string]any{"bufferView": uv, "componentType": 5126, "count": 4, "type": "VEC2"}, map[string]any{"bufferView": indices, "componentType": 5123, "count": 6, "type": "SCALAR"}},
		"images":    []any{map[string]any{"bufferView": picture, "mimeType": "image/png"}}, "textures": []any{map[string]int{"source": 0}},
		"materials": []any{map[string]any{"pbrMetallicRoughness": map[string]any{"baseColorTexture": map[string]int{"index": 0}, "metallicFactor": 0, "roughnessFactor": 1}}},
		"meshes":    []any{map[string]any{"primitives": []any{map[string]any{"attributes": map[string]int{"POSITION": 0, "NORMAL": 1, "TEXCOORD_0": 2}, "indices": 3, "material": 0}}}},
		"nodes":     []any{map[string]int{"mesh": 0}}, "scenes": []any{map[string]any{"nodes": []int{0}}}, "scene": 0,
	}
	if mode == "plain" {
		delete(doc["materials"].([]any)[0].(map[string]any)["pbrMetallicRoughness"].(map[string]any), "baseColorTexture")
	}
	js, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	for len(js)%4 != 0 {
		js = append(js, ' ')
	}
	for bin.Len()%4 != 0 {
		bin.WriteByte(0)
	}
	var out bytes.Buffer
	for _, v := range []uint32{0x46546c67, 2, uint32(12 + 8 + len(js) + 8 + bin.Len()), uint32(len(js)), 0x4e4f534a} {
		if err := binary.Write(&out, binary.LittleEndian, v); err != nil {
			t.Fatal(err)
		}
	}
	out.Write(js)
	for _, v := range []uint32{uint32(bin.Len()), 0x004e4942} {
		if err := binary.Write(&out, binary.LittleEndian, v); err != nil {
			t.Fatal(err)
		}
	}
	out.Write(bin.Bytes())
	return out.Bytes()
}

func TestScene3DEmbeddedImageTypedServedWebGL(t *testing.T) {
	root := e2eRepoRoot(t)
	app := server.New()
	app.SetRuntimeRoot(root)
	app.EnableSecurityPolicy(server.SecurityPolicy{ContentSecurityPolicy: "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' blob:; connect-src 'self'; worker-src 'self' blob:"})
	app.Page("/", func(ctx *server.Context) gosx.Node {
		mode := ctx.Request.URL.Query().Get("mode")
		modelMode := mode
		if mode == "shared" {
			modelMode = ""
		}
		props := scene.Props{Width: 240, Height: 240, Background: "#000000", ForceWebGL: scene.Bool(true), RequireWebGL: scene.Bool(true), Camera: scene.PerspectiveCamera{Position: scene.Vec3(0, 0, 4)}, Graph: scene.NewGraph(scene.AmbientLight{Intensity: 1, Color: "#ffffff"}, scene.DirectionalLight{Intensity: 2, Color: "#ffffff", Direction: scene.Vec3(0, 0, -1)}, scene.Model{ID: "textured", Src: "/model.glb?mode=" + modelMode})}
		mount := func() gosx.Node {
			return gosx.El("div", gosx.Attrs(gosx.Attr("style", "width:240px;height:240px")), ctx.Engine(props.EngineConfig(), gosx.Text("loading model")))
		}
		if mode == "shared" {
			return gosx.El("main", mount(), mount())
		}
		return mount()
	})
	glbs := map[string][]byte{"": embeddedImageGLB(t, ""), "flip": embeddedImageGLB(t, "flip"), "plain": embeddedImageGLB(t, "plain")}
	mux := http.NewServeMux()
	mux.HandleFunc("/model.glb", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := glbs[r.URL.Query().Get("mode")]; !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "model/gltf-binary")
		_, _ = w.Write(glbs[r.URL.Query().Get("mode")])
	})
	mux.Handle("/", app.Build())
	srv := httptest.NewServer(mux)
	defer srv.Close()
	page := newBrowserPage(t, e2eChromePath(t), map[string]any{"use-gl": "angle", "use-angle": "swiftshader", "enable-unsafe-swiftshader": true, "enable-webgl": true, "ignore-gpu-blocklist": true}, 640, 640, `window.__imageProbes=[];window.__liveURLs=new Set();window.__urlCounts={created:0,revoked:0};const create=URL.createObjectURL.bind(URL),revoke=URL.revokeObjectURL.bind(URL);URL.createObjectURL=function(blob){const url=create(blob);__liveURLs.add(url);__urlCounts.created++;return url};URL.revokeObjectURL=function(url){if(!__liveURLs.delete(url))throw Error('revoking unowned URL');__urlCounts.revoked++;revoke(url)};const NativeImage=window.Image;window.Image=function(){const img=new NativeImage(...arguments);window.__imageProbes.push(img);return img;};`, 90*time.Second)
	page.navigate(t, srv.URL)
	page.waitFor(t, `!!document.querySelector('[data-gosx-scene3d-renderer="webgl"] canvas')`, 15*time.Second, "typed model WebGL mount")
	page.waitFor(t, `(()=>{const m=document.querySelector('[data-gosx-scene3d-renderer="webgl"]');return m&&m.__gosxScene3DState&&m.__gosxScene3DState.objects.size>0})()`, 10*time.Second, "typed model hydration")
	page.waitFor(t, `document.querySelector('[data-gosx-scene3d-ready="true"]')!==null`, 10*time.Second, "typed scene reveal")
	var diagnostics string
	page.eval(t, `(()=>{const m=document.querySelector('[data-gosx-scene3d-renderer="webgl"]');const c=m.querySelector('canvas');const gl=c.getContext('webgl2');const ext=gl.getExtension('WEBGL_debug_renderer_info');return JSON.stringify({bounds:c.getBoundingClientRect(),size:[c.width,c.height],images:__imageProbes.map(i=>({complete:i.complete,w:i.naturalWidth,h:i.naturalHeight})),renderer:gl.getParameter(ext?ext.UNMASKED_RENDERER_WEBGL:gl.RENDERER),vendor:gl.getParameter(ext?ext.UNMASKED_VENDOR_WEBGL:gl.VENDOR),userAgent:navigator.userAgent,error:gl.getError()})})()`, &diagnostics)
	t.Logf("typed diagnostics=%s", diagnostics)
	assertPixels := func(stage string, want [4]string) {
		t.Helper()
		deadline := time.Now().Add(6 * time.Second)
		var got [4]string
		for time.Now().Before(deadline) {
			var shot []byte
			if err := chromedp.Run(page.ctx, chromedp.CaptureScreenshot(&shot)); err != nil {
				t.Fatal(err)
			}
			img, err := png.Decode(bytes.NewReader(shot))
			if err != nil {
				t.Fatal(err)
			}
			for i, p := range [4]image.Point{{108, 108}, {148, 108}, {108, 148}, {148, 148}} {
				r, g, b, _ := img.At(p.X, p.Y).RGBA()
				got[i] = "dark"
				switch {
				case r > 10000 && r > g*2 && r > b*2:
					got[i] = "red"
				case g > 10000 && g > r*2 && g > b*2:
					got[i] = "green"
				case b > 10000 && b > r*2 && b > g*2:
					got[i] = "blue"
				case r > 10000 && g > 10000 && r > b*2 && g > b*2:
					got[i] = "yellow"
				case r > 10000 && g > 10000 && b > 10000:
					got[i] = "white"
				}
			}
			if got == want {
				if target := os.Getenv("GOSX_EMBEDDED_IMAGE_SCREENSHOT"); target != "" {
					if err := os.WriteFile(target+"."+stage+".png", shot, 0600); err != nil {
						t.Fatal(err)
					}
				}
				t.Logf("%s converged texture quadrants=%v", stage, got)
				return
			}
			if err := chromedp.Run(page.ctx, chromedp.Sleep(50*time.Millisecond)); err != nil {
				t.Fatal(err)
			}
		}
		t.Fatalf("%s texture quadrants=%v want=%v console=%s", stage, got, want, page.Console())
	}
	positive := [4]string{"red", "green", "blue", "yellow"}
	assertPixels("first", positive)
	var stats map[string]int
	page.eval(t, `(async()=>{for(let i=0;i<100;i++){const before=__urlCounts.created;__gosx_dispose_engine('gosx-engine-0');await __gosx_bootstrap_page();for(let n=0;n<300 && (__urlCounts.created===before||__liveURLs.size);n++)await new Promise(r=>setTimeout(r,5));if(__urlCounts.created===before||__liveURLs.size)throw Error('decode did not settle cycle'+i);await new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r)));}return {...__urlCounts,live:__liveURLs.size}})()`, &stats)
	assertPixels("final", positive)
	if stats["created"] < 101 || stats["created"] != stats["revoked"] || stats["live"] != 0 {
		t.Fatalf("100cycle owned registry counts=%v", stats)
	}
	page.eval(t, `(async()=>{const c=document.querySelector('[data-gosx-scene3d-renderer="webgl"] canvas');const gl=c.getContext('webgl2');const ext=gl.getExtension('WEBGL_lose_context');if(!ext)throw Error('missing context loss extension');const before=__urlCounts.created;await new Promise((resolve,reject)=>{const timer=setTimeout(()=>reject(Error('context restore timeout')),5000);c.addEventListener('webglcontextrestored',()=>{clearTimeout(timer);resolve()},{once:true});c.addEventListener('webglcontextlost',()=>setTimeout(()=>ext.restoreContext(),50),{once:true});ext.loseContext()});for(let n=0;n<300&&(__urlCounts.created===before||__liveURLs.size);n++)await new Promise(r=>setTimeout(r,5));if(__urlCounts.created===before||__liveURLs.size)throw Error('restored decoder did not settle');return {...__urlCounts,live:__liveURLs.size}})()`, &stats)
	assertPixels("restored", positive)
	page.eval(t, `__gosx_dispose_engine('gosx-engine-0')`, nil)
	t.Logf("100 fresh generic engine scopes settled owned registry=%v; no heap/GPU-byte claim", stats)
	for _, control := range []struct {
		mode string
		want [4]string
	}{{"plain", [4]string{"white", "white", "white", "white"}}, {"flip", [4]string{"green", "red", "yellow", "blue"}}} {
		page.navigate(t, srv.URL+"/?mode="+control.mode)
		assertPixels(control.mode, control.want)
	}
	page.navigate(t, srv.URL+"/?mode=shared")
	page.waitFor(t, `__gosx.scene3d.modelAssetResidency().owners===2&&__urlCounts.created>=2&&__liveURLs.size===0`, 10*time.Second, "both textured consumers")
	page.eval(t, `(()=>{const m=document.getElementById('gosx-engine-mount-0');__gosx_dispose_engine('gosx-engine-0');m.parentElement.remove();if(__gosx.scene3d.modelAssetResidency().owners!==1)throw Error('shared survivor lost ownership')})()`, nil)
	assertPixels("survivor", positive)
	page.eval(t, `__gosx_dispose_engine('gosx-engine-1')`, nil)
	page.waitFor(t, `__liveURLs.size===0&&__gosx.scene3d.modelAssetResidency().owners===0`, 5*time.Second, "final shared disposal releases all ownership")
	page.navigate(t, srv.URL+"/?mode=missing")
	page.waitFor(t, `document.querySelector('[data-gosx-scene3d-model-status="error"]')!==null`, 10*time.Second, "missing GLB reports failure")
	page.eval(t, `(()=>{const m=document.querySelector('[data-gosx-scene3d-model-status="error"]');if(!m.getAttribute('data-gosx-scene3d-model-error').includes('404'))throw Error('missing GLB did not report HTTP404');if(m.__gosxScene3DState.objects.size!==0||__urlCounts.created!==0||__liveURLs.size!==0||__gosx.scene3d.modelAssetResidency().owners!==0)throw Error('missing GLB retained model/image ownership')})()`, nil)
	assertPixels("missing", [4]string{"dark", "dark", "dark", "dark"})
	if errors := page.PageErrors(); len(errors) > 0 {
		t.Fatalf("page errors=%v", errors)
	}
}
