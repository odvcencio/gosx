//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"m31labs.dev/gosx/scene"
)

// nightSkyShot mounts a physical sky and an open ocean with the sun far below
// the horizon, draws one frame and returns the canvas pixels. The camera looks
// toward the horizon from 6 m up, so the moon at 22 degrees sits above centre
// and its path runs down the middle of the water.
//
// GOSX_NIGHT_SKY_BACKEND=webgpu (with GPU Chrome) draws on WebGPU; the default
// is WebGL2. GOSX_NIGHT_SKY_HOST=<WSL address> (and GOSX_NIGHT_SKY_PORT) serves on every
// address for Windows Chrome; WebGPU needs a secure origin, so start that Chrome
// with --unsafely-treat-insecure-origin-as-secure for the same address and port.
// GOSX_NIGHT_SKY_SHOTS names a folder for PNG copies.
func nightSkyShot(t *testing.T, name string, width, height int, night *scene.SkyNight, clouds *scene.SkyClouds) image.Image {
	t.Helper()
	root, chrome := e2eRepoRoot(t), e2eChromePath(t)
	webgpu := os.Getenv("GOSX_NIGHT_SKY_BACKEND") == "webgpu"
	props := scene.Props{Width: width, Height: height, Responsive: scene.Bool(false), MaxDevicePixelRatio: 1, Controls: "none", Background: "#000000",
		Camera: scene.PerspectiveCamera{Position: scene.Vec3(0, 6, 0), FOV: 60, Near: 0.1, Far: 30000},
		Environment: scene.Environment{
			Sky: &scene.Sky{Mode: "physical", SunDirection: scene.Vector3{X: 0.3, Y: -0.35, Z: -0.9}, Clouds: clouds, Night: night},
			Ocean: &scene.Ocean{WaveHeight: 0.9, WaveLength: 24, Roughness: 0.05, Speed: 0.5,
				DeepColor: "#02101a", ShallowColor: "#0a3a48", ScatterColor: "#1c6f70"},
		}}
	if webgpu {
		props.PreferWebGPU = scene.Bool(true)
	} else {
		props.ForceWebGL, props.RequireWebGL = scene.Bool(true), scene.Bool(true)
	}
	payload, err := json.Marshal(props)
	if err != nil {
		t.Fatal(err)
	}
	markup := fmt.Sprintf(`<!doctype html><html><body style="margin:0;background:#000"><div id="scene" style="width:%dpx;height:%dpx"></div>
<script src="/gosx/bootstrap-runtime.js"></script>
<script data-gosx-script="feature-scene3d" data-gosx-scene3d-webgl-url="/gosx/bootstrap-feature-scene3d-webgl.js" src="/gosx/bootstrap-feature-scene3d.js"></script>
<script>window.ready=(async()=>{window.handle=await window.__gosx_engine_factories.GoSXScene3D({mount:document.getElementById('scene'),emit(){},props:%s});window.mounted=true;})().catch(error=>{window.mountError=String(error);});</script></body></html>`, width, height, payload)
	mux := http.NewServeMux()
	mux.Handle("/gosx/", http.StripPrefix("/gosx/", http.FileServer(http.Dir(filepath.Join(root, "client", "js")))))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(markup))
	})
	app := httptest.NewUnstartedServer(mux)
	url := ""
	if host := os.Getenv("GOSX_NIGHT_SKY_HOST"); host != "" {
		// Windows Chrome cannot reach a WSL loopback server: listen on every address and use the WSL address.
		listener, err := net.Listen("tcp", "0.0.0.0:"+os.Getenv("GOSX_NIGHT_SKY_PORT"))
		if err != nil {
			t.Fatal(err)
		}
		app.Listener = listener
		app.Start()
		url = fmt.Sprintf("http://%s:%d", host, listener.Addr().(*net.TCPAddr).Port)
	} else {
		app.Start()
		url = app.URL
	}
	defer app.Close()
	flags := map[string]any{"use-gl": "angle", "use-angle": "swiftshader", "enable-unsafe-swiftshader": true, "enable-webgl": true}
	if webgpu {
		flags = map[string]any{"enable-unsafe-webgpu": true, "ignore-gpu-blocklist": true}
	}
	page := newBrowserPage(t, chrome, flags, width+40, height+40, "", 90*time.Second)
	page.navigate(t, url)
	page.waitFor(t, `window.mounted || window.mountError`, 30*time.Second, "scene mount")
	var mountError string
	page.eval(t, `window.mountError || ''`, &mountError)
	if mountError != "" {
		t.Fatalf("mount: %s\n%s", mountError, page.Console())
	}
	page.waitFor(t, `(document.getElementById('scene').getAttribute('data-gosx-scene3d-sky')||'')==='physical' && (document.getElementById('scene').getAttribute('data-gosx-scene3d-ocean')||'')!==''`, 30*time.Second, "sky and ocean state")
	want := map[bool]string{true: "webgpu", false: "webgl"}[webgpu]
	if !page.pollFor(fmt.Sprintf(`document.getElementById('scene').getAttribute('data-gosx-scene3d-renderer') === %q`, want), 20*time.Second) {
		var attrs string
		page.eval(t, `Array.from(document.getElementById('scene').attributes).filter(a => /renderer|fallback/.test(a.name)).map(a => a.name + '=' + a.value).join(' ')`, &attrs)
		t.Fatalf("renderer is not %q: %s\n%s", want, attrs, page.Console())
	}
	time.Sleep(1500 * time.Millisecond)
	if os.Getenv("GOSX_NIGHT_SKY_SHOTS") != "" {
		t.Logf("%s: median GPU frame time %s", name, nightGPUMedian(t, page))
	}
	shot := page.screenshotElement(t, "#scene canvas")
	if dir := os.Getenv("GOSX_NIGHT_SKY_SHOTS"); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name+".png"), shot, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	img, err := png.Decode(bytes.NewReader(shot))
	if err != nil {
		t.Fatal(err)
	}
	return img
}

// nightGPUMedian samples the renderer's own GPU timer (WebGPU timestamps or the
// WebGL disjoint timer query) for three seconds and returns the median.
func nightGPUMedian(t *testing.T, page *browserPage) string {
	t.Helper()
	var out string
	page.eval(t, `(async () => {
  const dbg = window.__gosx_scene3d_debug, rec = dbg && dbg.listSurfaces && dbg.listSurfaces()[0], id = rec && (rec.surfaceID || rec.id);
  const seen = new Map(); let status = 'none';
  for (let i = 0; i < 30; i++) {
    const snap = id && dbg.inspect(id), ft = snap && snap.frameTiming;
    if (ft) { status = ft.status; if (ft.status === 'measured') seen.set(ft.frameSeq, ft.gpuMS); }
    await new Promise(r => setTimeout(r, 100));
  }
  const v = Array.from(seen.values()).sort((a, b) => a - b);
  return v.length ? v[v.length >> 1].toFixed(3) + ' ms over ' + v.length + ' frames' : 'no samples (' + status + ')';
})()`, &out)
	return out
}

func nightLuma(img image.Image, x, y int) float64 {
	r, g, b, _ := img.At(x, y).RGBA()
	return (0.2126*float64(r) + 0.7152*float64(g) + 0.0722*float64(b)) / 65535
}

// nightStats measures the sky above the horizon (the top 40 percent of the
// frame) and the water below it.
func nightStats(img image.Image) (stars int, moon, water float64) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	for y := 0; y < h*40/100; y++ {
		for x := 0; x < w; x++ {
			l := nightLuma(img, x, y)
			if l > 0.45 && l < 0.97 {
				stars++
			}
			if l > moon {
				moon = l
			}
		}
	}
	for y := h * 60 / 100; y < h; y++ {
		for x := w / 3; x < w*2/3; x++ {
			water += nightLuma(img, x, y)
		}
	}
	water /= float64(h*40/100) * float64(w/3)
	return
}

func TestScene3DNightSkyDrawsStarsMoonAndMoonlitSea(t *testing.T) {
	w, h := 640, 400
	if size := os.Getenv("GOSX_NIGHT_SKY_SIZE"); size != "" {
		if _, err := fmt.Sscanf(size, "%dx%d", &w, &h); err != nil {
			t.Fatalf("GOSX_NIGHT_SKY_SIZE %q: %v", size, err)
		}
	}
	moon := scene.Vector3{X: 0, Y: 0.37, Z: -0.93}
	dark := nightSkyShot(t, "no-night", w, h, nil, nil)
	none := nightSkyShot(t, "no-moon", w, h, &scene.SkyNight{MoonDirection: moon}, nil)
	full := nightSkyShot(t, "full-moon", w, h, &scene.SkyNight{MoonDirection: moon, MoonPhase: 0.5}, nil)
	half := nightSkyShot(t, "half-moon", w, h, &scene.SkyNight{MoonDirection: moon, MoonPhase: 0.25}, nil)
	over := nightSkyShot(t, "overcast-full-moon", w, h, &scene.SkyNight{MoonDirection: moon, MoonPhase: 0.5}, &scene.SkyClouds{Coverage: 1, Opacity: 1})

	sDark, _, wDark := nightStats(dark)
	sNone, _, wNone := nightStats(none)
	sFull, mFull, wFull := nightStats(full)
	_, mHalf, wHalf := nightStats(half)
	sOver, _, wOver := nightStats(over)
	t.Logf("stars/moon/water: dark %d/-/%.4f none %d/-/%.4f full %d/%.3f/%.4f half -/%.3f/%.4f overcast %d/-/%.4f", sDark, wDark, sNone, wNone, sFull, mFull, wFull, mHalf, wHalf, sOver, wOver)

	if sDark > 5 {
		t.Errorf("a sky without Night drew %d star-like pixels", sDark)
	}
	if sNone < 40 {
		t.Errorf("new moon: %d star pixels, want a star field", sNone)
	}
	if mFull < 0.9 {
		t.Errorf("full moon peak luma %.3f, want a bright disc", mFull)
	}
	if !(wFull > wNone*2 && wFull > wHalf && wHalf > wNone) {
		t.Errorf("moon path on the water should grow with the lit fraction: none %.4f half %.4f full %.4f", wNone, wHalf, wFull)
	}
	if sOver >= sFull/2 || wOver >= wFull {
		t.Errorf("overcast should dim the stars and the path: stars %d vs %d, water %.4f vs %.4f", sOver, sFull, wOver, wFull)
	}
}
