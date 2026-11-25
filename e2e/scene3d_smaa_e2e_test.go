//go:build e2e

// SMAA on a real GPU, against the same scene drawn at 4 x 4 the size and box-filtered
// in linear light. The edge scene is procedural: a star of thin bars at 48 angles,
// thin stripes receding on a tilted ground (yard lines in a broadcast), and thin
// bars of several colours over a red field (facemask bars over a jersey). Every
// mode runs the same chain, Tonemap then the anti-aliasing under test, so colours
// match; MSAA 4x is WebGPU only, because WebGL2 turns the context MSAA off under a
// post chain. Three checks:
//
//   - TestSMAAEdgesOnTheGPU: mean display error on the pixels an edge crosses, for
//     none, FXAA, SMAA (and MSAA 4x, MSAA 4x + SMAA on WebGPU); SMAA must beat no
//     AA by 30% and beat FXAA, and leave flat interiors alone.
//   - TestSMAAMovingLinesOnTheGPU: the camera steps sideways by a third of a pixel;
//     the frame-to-frame change on edge pixels is compared with the reference's own
//     change (the temporal residual): SMAA must not shimmer more than no AA.
//   - TestSMAAFrameCost: whole-frame GPU p50/p95 at 1920 x 1080, none, FXAA, SMAA.
//
// GOSX_SMAA_EVIDENCE=<dir> keeps every still as PNG. Run on a GPU runner:
//
//	mac-gpu-run <checkout> env GOSX_WATER_E2E_REQUIRE_WEBGPU=1 go test -tags e2e -run TestSMAA -timeout 1200s ./e2e/
package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"m31labs.dev/gosx/scene"
)

const smaaShotW, smaaShotH = 480, 270

// smaaEdgeScene builds the edge scene with the given chain end ("none", "fxaa",
// "smaa") and camera x offset.
func smaaEdgeScene(t *testing.T, aa string, camX float64) json.RawMessage {
	t.Helper()
	flat := func(c string) scene.FlatMaterial { return scene.FlatMaterial{Color: c, Wireframe: scene.Bool(false)} }
	nodes := []scene.Node{
		scene.Mesh{ID: "field", Geometry: scene.PlaneGeometry{Width: 3.2, Height: 2.2}, Material: flat("#b0242c"), Position: scene.Vec3(2.2, 0.6, -0.2)},
	}
	for i := 0; i < 48; i++ {
		a := float64(i) * math.Pi / 48
		nodes = append(nodes, scene.Mesh{ID: fmt.Sprintf("star%d", i), Geometry: scene.BoxGeometry{Width: 1.6, Height: 0.045, Depth: 0.01},
			Material: flat("#f4f4f0"), Position: scene.Vec3(-2.2+0.8*math.Cos(a), 0.7+0.8*math.Sin(a), 0), Rotation: scene.Euler{Z: a}})
	}
	for i := 0; i < 14; i++ {
		nodes = append(nodes, scene.Mesh{ID: fmt.Sprintf("yard%d", i), Geometry: scene.BoxGeometry{Width: 7, Height: 0.004, Depth: 0.14},
			Material: flat("#ffffff"), Position: scene.Vec3(0, -1.2, -0.4*float64(i)), Rotation: scene.Euler{Z: 0.035}})
	}
	nodes = append(nodes, scene.Mesh{ID: "turf", Geometry: scene.BoxGeometry{Width: 8, Height: 0.002, Depth: 8}, Material: flat("#2f6b34"), Position: scene.Vec3(0, -1.21, -3)})
	for i, c := range []string{"#e8e8e8", "#1a1a1a", "#f2c230", "#3060c0", "#e8e8e8"} {
		nodes = append(nodes, scene.Mesh{ID: fmt.Sprintf("bar%d", i), Geometry: scene.BoxGeometry{Width: 2.6, Height: 0.06, Depth: 0.02},
			Material: flat(c), Position: scene.Vec3(2.2, 0.15+0.22*float64(i), 0), Rotation: scene.Euler{Z: 0.05 + 0.11*float64(i)}})
	}
	effects := []scene.PostEffect{scene.Tonemap{Mode: scene.TonemapACES, Exposure: 1}}
	switch aa {
	case "fxaa":
		effects = append(effects, scene.FXAA{})
	case "smaa":
		effects = append(effects, scene.SMAA{})
	}
	props := scene.Props{
		Camera: scene.PerspectiveCamera{Position: scene.Vec3(camX, 0.2, 4), FOV: 45, Near: 0.1, Far: 60},
		PostFX: scene.PostFX{Effects: effects, MaxPixels: 4096 * 4096},
		Graph:  scene.NewGraph(append(nodes, scene.AmbientLight{ID: "amb", Color: "#ffffff", Intensity: 1})...),
	}
	data, err := json.Marshal(props.SceneIR())
	if err != nil {
		t.Fatal(err)
	}
	return data
}

const smaaPage = `<!doctype html><html><head><meta charset="utf-8"><style>html,body{margin:0;background:#000;overflow:hidden}#s{width:%dpx;height:%dpx}</style></head><body>
<div id="s"></div>
<script src="/gosx/bootstrap-runtime.js"></script>
<script data-gosx-script="feature-scene3d"
 data-gosx-scene3d-webgl-url="/gosx/bootstrap-feature-scene3d-webgl.js"
 data-gosx-scene3d-webgpu-url="/gosx/bootstrap-feature-scene3d-webgpu.js"
 data-gosx-scene3d-command-url="/gosx/bootstrap-feature-scene3d-command.js"
 data-gosx-scene3d-hierarchy-url="/gosx/bootstrap-feature-scene3d-hierarchy.js"
 %s src="/gosx/bootstrap-feature-scene3d.js"></script>
<script>
window.__gosx_scene3d_perf = true; window.gpuFrames = [];
document.addEventListener('gosx:scene3d:perf', e => { if (e.detail && e.detail.frameMS > 0) window.gpuFrames.push(e.detail.frameMS); });
window.ready = (async () => {
 window.handle = await window.__gosx_engine_factories.GoSXScene3D({ mount: document.getElementById('s'), emit() {}, props: %s });
 window.mounted = true;
})().catch(error => { window.mountError = String(error && error.stack || error); });
</script></body></html>`

// smaaServer serves one edge-scene page at w x h CSS pixels.
func smaaServer(t *testing.T, aa string, msaa, w, h int, camX float64) (string, func()) {
	t.Helper()
	smaaURL := ""
	if aa == "smaa" {
		smaaURL = `data-gosx-scene3d-smaa-url="/gosx/bootstrap-feature-scene3d-smaa.js"`
	}
	markup := fmt.Sprintf(smaaPage, w, h, smaaURL, mustJSON(t, map[string]any{
		"width": w, "height": h, "responsive": false, "maxDevicePixelRatio": 1, "controls": "none", "msaaSamples": msaa, "antialias": msaa > 1,
		"shaded": true, "background": "#0b0d12", "scene": smaaEdgeScene(t, aa, camX),
	}))
	mux := http.NewServeMux()
	mux.Handle("/gosx/", http.StripPrefix("/gosx/", http.FileServer(http.Dir(filepath.Join(e2eRepoRoot(t), "client", "js")))))
	mux.HandleFunc("/", func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = rw.Write([]byte(markup))
	})
	server := httptest.NewUnstartedServer(mux)
	listener, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	server.Listener = listener
	server.Start()
	return fmt.Sprintf("http://localhost:%d", listener.Addr().(*net.TCPAddr).Port), server.Close
}

// smaaShot mounts the page at scale x the shot size and returns one still, after
// checking that an SMAA page drew SMAA frames.
func smaaShot(t *testing.T, backend, aa string, msaa, scale int, camX float64) image.Image {
	t.Helper()
	w, h := smaaShotW*scale, smaaShotH*scale
	ctx, closeBrowser := decalBrowser(t, backend)
	defer closeBrowser()
	url, stop := smaaServer(t, aa, msaa, w, h, camX)
	defer stop()
	if err := chromedp.Run(ctx, emulation.SetDeviceMetricsOverride(int64(w), int64(h), 1, false)); err != nil {
		t.Fatal(err)
	}
	decalNavigate(t, ctx, url, backend)
	time.Sleep(1500 * time.Millisecond)
	if camX != 0 {
		// The mount reads the scene's camera once; later camera moves go through the handle.
		if err := chromedp.Run(ctx, chromedp.Evaluate(fmt.Sprintf(`window.handle.setCamera({ x: %v, y: 0.2, z: 4, fov: 45, near: 0.1, far: 60 })`, camX), nil)); err != nil {
			t.Fatal(err)
		}
		time.Sleep(500 * time.Millisecond)
	}
	if aa == "smaa" {
		var frames float64
		if err := chromedp.Run(ctx, chromedp.Evaluate(`(window.__gosx_scene3d_smaa_api && window.__gosx_scene3d_smaa_api.stats.frames) || 0`, &frames)); err != nil || frames < 1 {
			t.Fatalf("%s: the SMAA chunk drew %v frames (%v)", backend, frames, err)
		}
	}
	var data []byte
	if err := chromedp.Run(ctx, chromedp.CaptureScreenshot(&data)); err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if dir := os.Getenv("GOSX_SMAA_EVIDENCE"); dir != "" {
		_ = os.MkdirAll(dir, 0o755)
		_ = os.WriteFile(filepath.Join(dir, fmt.Sprintf("edges-%s-%s-msaa%d-x%d-cam%.4f.png", backend, aa, msaa, scale, camX)), data, 0o644)
	}
	return img
}

// linear8 converts one 8-bit display code to linear light (sRGB transfer).
func linear8(c uint32) float64 {
	v := float64(c>>8) / 255
	if v <= 0.04045 {
		return v / 12.92
	}
	return math.Pow((v+0.055)/1.055, 2.4)
}

func display(v float64) float64 {
	if v <= 0.0031308 {
		return 255 * 12.92 * v
	}
	return 255 * (1.055*math.Pow(v, 1/2.4) - 0.055)
}

// smaaReference box-filters a 4x still in linear light. edge marks the pixels whose
// 4 x 4 block is not uniform, which are the pixels an edge crosses.
type smaaReference struct {
	rgb  [][3]float64
	edge []bool
}

func newSMAAReference(big image.Image) smaaReference {
	ref := smaaReference{rgb: make([][3]float64, smaaShotW*smaaShotH), edge: make([]bool, smaaShotW*smaaShotH)}
	for y := 0; y < smaaShotH; y++ {
		for x := 0; x < smaaShotW; x++ {
			var sum [3]float64
			lo, hi := [3]uint32{65535, 65535, 65535}, [3]uint32{}
			for j := 0; j < 4; j++ {
				for i := 0; i < 4; i++ {
					r, g, b, _ := big.At(x*4+i, y*4+j).RGBA()
					for c, v := range [3]uint32{r, g, b} {
						sum[c] += linear8(v) / 16
						lo[c], hi[c] = min(lo[c], v), max(hi[c], v)
					}
				}
			}
			for c := range sum {
				ref.rgb[y*smaaShotW+x][c] = display(sum[c])
				if hi[c]-lo[c] > 6*257 {
					ref.edge[y*smaaShotW+x] = true
				}
			}
		}
	}
	return ref
}

// errors returns the mean absolute display error on edge pixels and on flat pixels
// (no edge in the 5 x 5 neighbourhood), in 8-bit codes.
func (ref smaaReference) errors(img image.Image) (edge, flat float64) {
	var ne, nf float64
	for y := 2; y < smaaShotH-2; y++ {
		for x := 2; x < smaaShotW-2; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			want := ref.rgb[y*smaaShotW+x]
			d := (math.Abs(float64(r>>8)-want[0]) + math.Abs(float64(g>>8)-want[1]) + math.Abs(float64(b>>8)-want[2])) / 3
			if ref.edge[y*smaaShotW+x] {
				edge += d
				ne++
				continue
			}
			near := false
			for j := -2; j <= 2 && !near; j++ {
				for i := -2; i <= 2 && !near; i++ {
					near = ref.edge[(y+j)*smaaShotW+x+i]
				}
			}
			if !near {
				flat += d
				nf++
			}
		}
	}
	return edge / math.Max(ne, 1), flat / math.Max(nf, 1)
}

type smaaMode struct {
	aa   string
	msaa int
}

func smaaModes(backend string) []smaaMode {
	modes := []smaaMode{{"none", 1}, {"fxaa", 1}, {"smaa", 1}}
	if backend == "webgpu" {
		modes = append(modes, smaaMode{"none", 4}, smaaMode{"smaa", 4})
	}
	return modes
}

func TestSMAAEdgesOnTheGPU(t *testing.T) {
	for _, backend := range decalBackends(t) {
		t.Run(backend, func(t *testing.T) {
			ref := newSMAAReference(smaaShot(t, backend, "none", 1, 4, 0))
			got := map[smaaMode][2]float64{}
			for _, mode := range smaaModes(backend) {
				edge, flat := ref.errors(smaaShot(t, backend, mode.aa, mode.msaa, 1, 0))
				got[mode] = [2]float64{edge, flat}
				t.Logf("%s %-4s msaa %d: edge error %.2f codes, flat error %.2f codes", backend, mode.aa, mode.msaa, edge, flat)
			}
			none, fxaa, smaa := got[smaaMode{"none", 1}], got[smaaMode{"fxaa", 1}], got[smaaMode{"smaa", 1}]
			if smaa[0] > none[0]*0.7 {
				t.Errorf("SMAA edge error %.2f against %.2f with no AA: want at least 30%% lower", smaa[0], none[0])
			}
			if smaa[0] >= fxaa[0] {
				t.Errorf("SMAA edge error %.2f against FXAA %.2f: want lower", smaa[0], fxaa[0])
			}
			if smaa[1] > none[1]+0.5 {
				t.Errorf("SMAA changed flat interiors: %.2f codes against %.2f", smaa[1], none[1])
			}
		})
	}
}

func TestSMAAMovingLinesOnTheGPU(t *testing.T) {
	const steps = 5
	step := 4.0 / float64(smaaShotH) / 3 // about a third of a pixel at the scene's depth
	for _, backend := range decalBackends(t) {
		t.Run(backend, func(t *testing.T) {
			refs := make([]smaaReference, steps)
			for i := range refs {
				refs[i] = newSMAAReference(smaaShot(t, backend, "none", 1, 4, float64(i)*step))
			}
			got := map[string]float64{}
			for _, aa := range []string{"none", "fxaa", "smaa"} {
				var residual, n float64
				prev := smaaShot(t, backend, aa, 1, 1, 0)
				for i := 1; i < steps; i++ {
					cur := smaaShot(t, backend, aa, 1, 1, float64(i)*step)
					for y := 2; y < smaaShotH-2; y++ {
						for x := 2; x < smaaShotW-2; x++ {
							k := y*smaaShotW + x
							if !refs[i].edge[k] && !refs[i-1].edge[k] {
								continue
							}
							r0, g0, b0, _ := prev.At(x, y).RGBA()
							r1, g1, b1, _ := cur.At(x, y).RGBA()
							for c, d := range [3]float64{float64(r1>>8) - float64(r0>>8), float64(g1>>8) - float64(g0>>8), float64(b1>>8) - float64(b0>>8)} {
								residual += math.Abs(d - (refs[i].rgb[k][c] - refs[i-1].rgb[k][c]))
								n++
							}
						}
					}
					prev = cur
				}
				got[aa] = residual / math.Max(n, 1)
				t.Logf("%s %-4s: temporal residual %.2f codes per edge pixel and step", backend, aa, got[aa])
			}
			if got["smaa"] > got["none"] {
				t.Errorf("SMAA shimmers more than no AA on moving edges: %.2f against %.2f codes", got["smaa"], got["none"])
			}
		})
	}
}

func TestSMAAFrameCost(t *testing.T) {
	const width, height = 1920, 1080
	for _, backend := range decalBackends(t) {
		t.Run(backend, func(t *testing.T) {
			for _, mode := range []smaaMode{{"none", 1}, {"fxaa", 1}, {"smaa", 1}} {
				ctx, closeBrowser := decalBrowser(t, backend)
				url, stop := smaaServer(t, mode.aa, mode.msaa, width, height, 0)
				if err := chromedp.Run(ctx, emulation.SetDeviceMetricsOverride(width, height, 1, false)); err != nil {
					t.Fatal(err)
				}
				decalNavigate(t, ctx, url, backend)
				time.Sleep(1500 * time.Millisecond)
				var raw string
				err := chromedp.Run(ctx, chromedp.Evaluate(`(async () => {
 window.gpuFrames = []; const frames = []; let last = performance.now();
 await new Promise(resolve => { function tick(now) { frames.push(now - last); last = now; window.handle.setCamera({ x: 0.002 * frames.length % 0.2, y: 0.2, z: 4, fov: 45, near: 0.1, far: 60 }); if (frames.length < 600) requestAnimationFrame(tick); else resolve(); } requestAnimationFrame(tick); });
 await new Promise(resolve => setTimeout(resolve, 300));
 return JSON.stringify({ frames, gpu: window.gpuFrames });
})()`, &raw, func(p *runtime.EvaluateParams) *runtime.EvaluateParams { return p.WithAwaitPromise(true) }))
				stop()
				closeBrowser()
				if err != nil {
					t.Fatal(err)
				}
				var r struct{ Frames, GPU []float64 }
				if err := json.Unmarshal([]byte(raw), &r); err != nil {
					t.Fatal(err)
				}
				pct := func(v []float64, p float64) float64 {
					if len(v) == 0 {
						return 0
					}
					s := append([]float64(nil), v...)
					sort.Float64s(s)
					return s[min(len(s)-1, int(p*float64(len(s))))]
				}
				t.Logf("%s @%dx%d %-4s: frame interval p50 %.2f ms; whole-frame GPU p50 %.3f p95 %.3f ms over %d samples", backend, width, height, mode.aa, pct(r.Frames, 0.5), pct(r.GPU, 0.5), pct(r.GPU, 0.95), len(r.GPU))
			}
		})
	}
}
