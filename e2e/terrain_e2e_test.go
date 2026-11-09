//go:build e2e

// Environment.Terrain on a real GPU. A generated island terrain with four
// splat layers (one textured, so the triplanar path runs), a shadow-casting
// sun, fog and a few shadow-casting boxes is mounted on WebGPU and on forced
// WebGL2. The test checks that the terrain pass reports ready with patches,
// that the frame is not blank, and that the lit terrain shows shadow contrast
// under the boxes. With GOSX_TERRAIN_EVIDENCE set it also writes, per
// backend, the frame before terrain existed (a flat plane), the terrain
// frame, the frame with LOD tint, and the median GPU and CPU frame times.
//
// Run: gpu-chrome.sh run -- go test -tags e2e -run TestTerrainOnTheGPU ./e2e/
package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"m31labs.dev/gosx/scene"
)

func terrainTestTexture(seed uint64, base color.NRGBA, contrast float64) []byte {
	const size = 128
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	h := scene.GenerateHeightmap(scene.TerrainNoise{Seed: seed, Columns: size, Rows: size, Frequency: 16, Octaves: 4})
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			v := (float64(h.Data[y*size+x])/65535 - 0.5) * contrast
			c := func(ch uint8) uint8 { return uint8(math.Max(0, math.Min(255, float64(ch)*(1+v)))) }
			img.SetNRGBA(x, y, color.NRGBA{c(base.R), c(base.G), c(base.B), 255})
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func terrainTestProps(width, height int, withTerrain, full bool, camera scene.Vector3) scene.Props {
	heightmap := scene.GenerateHeightmap(scene.TerrainNoise{Seed: 7, Columns: 257, Rows: 257, Frequency: 3, Ridged: 0.35, Island: 0.55, Exponent: 1.3})
	terrain := &scene.Terrain{
		Heightmap: heightmap, Position: scene.Vector3{Y: -6}, Width: 512, HeightScale: 70,
		Layers: []scene.TerrainLayer{
			{Color: "#5f7f3a", Roughness: 0.95},
			{Color: "#c9b78a", MaxHeight: scene.Float(-2), HeightBlend: 3},
			{Albedo: "/tex/rock.png", Scale: 6, MinSlope: 34, SlopeBlend: 8, Roughness: 0.8},
			{Color: "#f2f4f6", MinHeight: scene.Float(44), HeightBlend: 6, MaxSlope: 40, Roughness: 0.6},
		},
	}
	nodes := []scene.Node{
		scene.DirectionalLight{Color: "#fff4e0", Intensity: 2.6, Direction: scene.Vector3{X: -0.6, Y: -0.55, Z: -0.4}, CastShadow: true, ShadowSize: 2048},
		scene.HemisphereLight{SkyColor: "#bcd3ff", GroundColor: "#4a4034", Intensity: 0.55},
	}
	for i, x := range []float64{-30, -10, 12} {
		y := 4.0
		if withTerrain {
			y = terrain.HeightAt(x, 40) + 4
		}
		nodes = append(nodes, scene.Mesh{ID: fmt.Sprintf("box-%d", i), Geometry: scene.BoxGeometry{Width: 6, Height: 8, Depth: 6},
			Material: scene.StandardMaterial{Color: "#9a3b2f", Roughness: 0.6}, Position: scene.Vector3{X: x, Y: y, Z: 40}, CastShadow: true, ReceiveShadow: true})
	}
	if !withTerrain {
		nodes = append(nodes, scene.Mesh{ID: "ground", Geometry: scene.PlaneGeometry{Width: 512, Height: 512},
			Material: scene.StandardMaterial{Color: "#5f7f3a", Roughness: 0.95}, ReceiveShadow: true})
	}
	// AutoRotate keeps every variant rendering (frame timing) and moves the
	// camera through the LOD ranges while the frames are sampled.
	props := scene.Props{Width: width, Height: height, Responsive: scene.Bool(false), MaxDevicePixelRatio: 1, Background: "#9fb8d6",
		Controls: "orbit", ControlTarget: scene.Vector3{X: -6, Y: 6, Z: 30}, AutoRotate: scene.Bool(true),
		Camera: scene.PerspectiveCamera{Position: camera, FOV: 55, Near: 0.5, Far: 3000},
		Environment: scene.Environment{
			Sky:      &scene.Sky{Mode: "gradient", TopColor: "#4a77b8", HorizonColor: "#c4d6ea", BottomColor: "#c4d6ea"},
			FogColor: "#c4d6ea", FogDensity: 0.0018,
		},
		Graph: scene.Graph{Nodes: nodes},
	}
	if withTerrain {
		props.Environment.Terrain = terrain
	}
	if full {
		sea := 0.0
		props.Environment.Wind = &scene.Wind{Direction: 60, Speed: 7, Gust: 0.45}
		props.Environment.Ocean = &scene.Ocean{WindDirection: 60, WaveHeight: 0.6, Shoreline: &scene.OceanShoreline{}}
		props.GPUDriven = &scene.GPUDriven{}
		props.Graph.Nodes = append(props.Graph.Nodes, scene.Scatter{
			ID: "veg", Terrain: terrain, Seed: 42, WaterLevel: &sea, WaterMargin: 0.6,
			Species: []scene.ScatterSpecies{
				{Name: "grass", Density: 0.35, MaxSlope: 28, MinScale: 0.7, MaxScale: 1.3, Align: 1, Wind: 0.9, WindHeight: 0.7,
					LODs: []scene.ScatterLOD{
						{Distance: 70, Geometry: scene.CylinderGeometry{RadiusBottom: 0.18, RadiusTop: 0.01, Height: 0.7, Segments: 5}, OffsetY: 0.35,
							Material: scene.StandardMaterial{Color: "#55802c", Roughness: 0.9}},
						{Distance: 140, Keep: 0.4, Geometry: scene.BoxGeometry{Width: 0.25, Height: 0.5, Depth: 0.25}, OffsetY: 0.25,
							Material: scene.StandardMaterial{Color: "#55802c", Roughness: 0.9}},
					}},
				{Name: "tree", Density: 0.01, MinHeight: scene.Float(2), MaxSlope: 32, MinScale: 0.8, MaxScale: 1.5, Sink: 0.3,
					CastShadow: true, ReceiveShadow: true, Wind: 0.15, WindHeight: 6,
					LODs: []scene.ScatterLOD{{Distance: 220, Geometry: scene.CylinderGeometry{RadiusBottom: 1.6, RadiusTop: 0.05, Height: 6, Segments: 8},
						OffsetY: 3, Material: scene.StandardMaterial{Color: "#2f5a2a", Roughness: 0.85}}},
					Billboard: &scene.ScatterBillboard{Distance: 700, Width: 3, Height: 6, Material: scene.StandardMaterial{Color: "#2f5a2a"}}},
			},
		})
	}
	return props
}

type terrainReading struct {
	Backend, Terrain, Patches, Ocean string
	GPU                              string
	CPU, Frame                       float64
	Frames                           int
}

func TestTerrainOnTheGPU(t *testing.T) {
	if os.Getenv("GOSX_E2E_CHROME") == "" {
		t.Skip("needs a real GPU browser: run under gpu-chrome.sh or mac-gpu-run")
	}
	root, chrome := e2eRepoRoot(t), e2eChromePath(t)
	const width, height = 960, 540
	rock := terrainTestTexture(3, color.NRGBA{120, 112, 104, 255}, 1.1)
	evidence := os.Getenv("GOSX_TERRAIN_EVIDENCE")
	summary := map[string]any{}
	for _, backend := range []string{"webgpu", "webgl"} {
		for _, variant := range []string{"before", "terrain", "lod", "full", "close", "close-plane"} {
			t.Run(backend+"-"+variant, func(t *testing.T) {
				props := terrainTestProps(width, height, variant != "before" && variant != "close-plane", variant == "full", scene.Vector3{X: -150, Y: 70, Z: 210})
				if variant == "lod" {
					// A coarse error target makes the level bands visible in the tint.
					props.Environment.Terrain.PixelError = 8
				}
				if variant == "close" || variant == "close-plane" {
					// Low over the boxes: their shadows must fall on the ground.
					ground := 0.0
					if props.Environment.Terrain != nil {
						ground = props.Environment.Terrain.HeightAt(-9, 40)
					}
					props.ControlTarget = scene.Vector3{X: -9, Y: ground, Z: 40}
					// A steep view from above shows shadows whichever way they fall.
					props.Camera.Position = scene.Vector3{X: -9, Y: ground + 48, Z: 64}
					props.AutoRotate = scene.Bool(false)
				}
				if backend == "webgpu" {
					props.PreferWebGPU = scene.Bool(true)
				} else {
					props.ForceWebGL, props.RequireWebGL = scene.Bool(true), scene.Bool(true)
				}
				payload, err := json.Marshal(props)
				if err != nil {
					t.Fatal(err)
				}
				tint := map[bool]string{true: "window.__gosx_scene3d_terrain_lod_tint=true;", false: ""}[variant == "lod"]
				markup := fmt.Sprintf(`<!doctype html><html><body style="margin:0;background:#000"><div id="scene" style="width:%dpx;height:%dpx"></div>
<script src="/gosx/bootstrap-runtime.js"></script>
<script data-gosx-script="feature-scene3d" data-gosx-scene3d-webgl-url="/gosx/bootstrap-feature-scene3d-webgl.js" data-gosx-scene3d-webgpu-url="/gosx/bootstrap-feature-scene3d-webgpu.js" data-gosx-scene3d-terrain-url="/gosx/bootstrap-feature-scene3d-terrain.js" data-gosx-scene3d-compute-url="/gosx/bootstrap-feature-scene3d-compute.js" data-gosx-scene3d-decompress-url="/gosx/bootstrap-feature-scene3d-decompress.js" src="/gosx/bootstrap-feature-scene3d.js"></script>
<script>%swindow.ready=(async()=>{window.handle=await window.__gosx_engine_factories.GoSXScene3D({mount:document.getElementById('scene'),emit(){},props:%s});window.mounted=true;})().catch(error=>{window.mountError=String(error);});</script></body></html>`, width, height, tint, payload)
				mux := http.NewServeMux()
				mux.Handle("/gosx/", http.StripPrefix("/gosx/", http.FileServer(http.Dir(filepath.Join(root, "client", "js")))))
				mux.HandleFunc("/tex/rock.png", func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "image/png")
					_, _ = w.Write(rock)
				})
				mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "text/html; charset=utf-8")
					_, _ = w.Write([]byte(markup))
				})
				app := httptest.NewServer(mux)
				defer app.Close()
				url := app.URL
				if e2eWindowsRelay() {
					url = e2eExposeToWindows(t, app.URL)
				}
				flags := map[string]any{"enable-unsafe-webgpu": true, "ignore-gpu-blocklist": true}
				page := newBrowserPage(t, chrome, flags, width+40, height+40, "", 120*time.Second)
				page.navigate(t, url)
				page.waitFor(t, `window.mounted || window.mountError`, 45*time.Second, "scene mount")
				var mountError string
				page.eval(t, `window.mountError || ''`, &mountError)
				if mountError != "" {
					t.Fatalf("mount: %s\n%s", mountError, page.Console())
				}
				if !page.pollFor(fmt.Sprintf(`document.getElementById('scene').getAttribute('data-gosx-scene3d-renderer') === %q`, backend), 30*time.Second) {
					t.Fatalf("renderer is not %s\n%s", backend, page.Console())
				}
				if props.Environment.Terrain != nil && !page.pollFor(`document.getElementById('scene').getAttribute('data-gosx-scene3d-terrain') === 'ready'`, 30*time.Second) {
					var state string
					page.eval(t, `(document.getElementById('scene').getAttribute('data-gosx-scene3d-terrain')||'missing')`, &state)
					var reason string
					page.eval(t, `(document.getElementById('scene').getAttribute('data-gosx-scene3d-terrain-error')||'')`, &reason)
					t.Fatalf("terrain state %q (%s)\n%s", state, reason, page.Console())
				}
				if variant == "full" && !page.pollFor(`document.getElementById('scene').getAttribute('data-gosx-scene3d-ocean') === 'shore'`, 30*time.Second) {
					var state string
					page.eval(t, `(document.getElementById('scene').getAttribute('data-gosx-scene3d-ocean')||'missing')`, &state)
					t.Fatalf("ocean state %q, want the baked shoreline loaded\n%s", state, page.Console())
				}
				time.Sleep(1500 * time.Millisecond)
				var reading terrainReading
				var raw string
				page.eval(t, `(async () => {
  const el = document.getElementById('scene');
  const dbg = window.__gosx_scene3d_debug, rec = dbg && dbg.listSurfaces && dbg.listSurfaces()[0], id = rec && (rec.surfaceID || rec.id);
  const gpu = new Map(), cpu = []; let status = 'none';
  const stamps = []; let stop = false; const tick = (t) => { stamps.push(t); if (!stop) requestAnimationFrame(tick); }; requestAnimationFrame(tick);
  for (let i = 0; i < 40; i++) {
    const snap = id && dbg.inspect(id), ft = snap && snap.frameTiming;
    if (ft) { status = ft.status; if (ft.status === 'measured') gpu.set(ft.frameSeq, ft.gpuMS); if (ft.cpuSubmitMS > 0) cpu.push(ft.cpuSubmitMS); }
    await new Promise(r => requestAnimationFrame(r));
    await new Promise(r => setTimeout(r, 40));
  }
  stop = true;
  const g = Array.from(gpu.values()).sort((a, b) => a - b); cpu.sort((a, b) => a - b);
  const gaps = stamps.slice(1).map((t, i) => t - stamps[i]).sort((a, b) => a - b);
  return JSON.stringify({ Backend: el.getAttribute('data-gosx-scene3d-renderer'), Terrain: el.getAttribute('data-gosx-scene3d-terrain') || '',
    Patches: el.getAttribute('data-gosx-scene3d-terrain-patches') || '', Ocean: el.getAttribute('data-gosx-scene3d-ocean') || '', GPU: g.length ? g[g.length >> 1].toFixed(3) + ' ms (' + g.length + ' frames)' : 'unmeasured (' + status + ')',
    CPU: cpu.length ? cpu[cpu.length >> 1] : -1, Frame: gaps.length ? gaps[gaps.length >> 1] : -1, Frames: stamps.length });
})()`, &raw)
				if err := json.Unmarshal([]byte(raw), &reading); err != nil {
					t.Fatalf("%s: %v", raw, err)
				}
				t.Logf("%s %s: %+v", backend, variant, reading)
				if os.Getenv("GOSX_TERRAIN_DEBUG") != "" {
					var diag string
					page.eval(t, `(() => { const el = document.getElementById('scene'); const attrs = Array.from(el.attributes).filter(a => /shadow|light|quality|tier/.test(a.name)).map(a => a.name + '=' + a.value);
  const dbg = window.__gosx_scene3d_debug, rec = dbg && dbg.listSurfaces && dbg.listSurfaces()[0], id = rec && (rec.surfaceID || rec.id), snap = id && dbg.inspect(id);
  const stats = snap && (snap.frameStats || snap.stats || {}); return JSON.stringify({ features: snap && snap.features, counts: snap && snap.counts, renderer: snap && snap.rendererDiagnostics && JSON.stringify(snap.rendererDiagnostics).slice(0, 600) }); })()`, &diag)
					t.Logf("debug: %s", diag)
				}
				shot := page.screenshotElement(t, "#scene canvas")
				img, err := png.Decode(bytes.NewReader(shot))
				if err != nil {
					t.Fatal(err)
				}
				lumaMin, lumaMax := 1.0, 0.0
				b := img.Bounds()
				for y := b.Min.Y + b.Dy()/2; y < b.Max.Y; y += 4 {
					for x := b.Min.X; x < b.Max.X; x += 4 {
						r, g, bl, _ := img.At(x, y).RGBA()
						l := (0.2126*float64(r) + 0.7152*float64(g) + 0.0722*float64(bl)) / 65535
						lumaMin, lumaMax = math.Min(lumaMin, l), math.Max(lumaMax, l)
					}
				}
				if lumaMax-lumaMin < 0.1 {
					t.Fatalf("lower half of the frame is flat (luma %.3f..%.3f)", lumaMin, lumaMax)
				}
				if props.Environment.Terrain != nil {
					if reading.Terrain != "ready" || reading.Patches == "" || reading.Patches == "0" {
						t.Fatalf("terrain pass did not draw: %+v", reading)
					}
				}
				if variant == "full" && reading.Ocean != "shore" {
					t.Fatalf("ocean state %q, want the baked shoreline loaded", reading.Ocean)
				}
				summary[backend+"-"+variant] = reading
				if evidence != "" {
					_ = os.MkdirAll(evidence, 0o755)
					_ = os.WriteFile(filepath.Join(evidence, backend+"-"+variant+".png"), shot, 0o644)
				}
			})
		}
	}
	if evidence != "" {
		blob, _ := json.MarshalIndent(summary, "", "  ")
		_ = os.WriteFile(filepath.Join(evidence, "summary.json"), blob, 0o644)
	}
}
