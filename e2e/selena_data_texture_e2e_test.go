//go:build e2e

package e2e

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image/png"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"m31labs.dev/gosx/scene"
)

// Three flat boxes sit side by side under a top-down camera. Each is covered by
// a Selena material that paints the texel of a 2x2 data texture under each
// quadrant with load(), one box per storage format (rgba8unorm, r16float,
// r32float). The engine side is the real Go encoder: frames from
// scene.DataTextureEncoder reach the GPU through handle.applyDataTexture, the
// path scene.TextureWriter uses. A second round of partial writes proves that a
// rectangle update reaches the texture.
const selenaDataTextureSource = `material Quad {
    param tex : texture2d format %s
    surface(geo) -> color {
        let c = int(clamp(floor(geo.worldPos.x - (%s) + 1.0), 0.0, 1.0))
        let r = int(clamp(floor(geo.worldPos.z + 1.0), 0.0, 1.0))
        let t = load(tex, c, r)
        return %s
    }
}`

type dataTextureCase struct {
	key    string
	format scene.TextureFormat
	centre float64
	rgb    string
	// first fills the whole texture; second rewrites one rectangle.
	first, second dataTextureWrite
	// want are the quadrant colours (c0r0, c1r0, c0r1, c1r1) after each round, as bright(1)/dark(0) or "RGB" channel triples.
	wantFirst, wantSecond [4][3]int
}

type dataTextureWrite struct {
	x, y, w, h int
	texels     []byte
}

func dataTextureCases() []dataTextureCase {
	rgba := func(r, g, b byte) []byte { return []byte{r, g, b, 255} }
	cat := func(parts ...[]byte) []byte { return bytes.Join(parts, nil) }
	half := func(v ...float32) []byte { return scene.AppendFloat16(nil, v) }
	full := func(v ...float32) []byte { return scene.AppendFloat32(nil, v) }
	return []dataTextureCase{
		{
			key: "e2e/rgba8", format: scene.TextureRGBA8Unorm, centre: -3, rgb: "rgb(t.r, t.g, t.b)",
			first:     dataTextureWrite{0, 0, 2, 2, cat(rgba(255, 0, 0), rgba(0, 255, 0), rgba(0, 0, 255), rgba(255, 255, 255))},
			second:    dataTextureWrite{1, 1, 1, 1, rgba(0, 0, 0)},
			wantFirst: [4][3]int{{255, 0, 0}, {0, 255, 0}, {0, 0, 255}, {255, 255, 255}}, wantSecond: [4][3]int{{255, 0, 0}, {0, 255, 0}, {0, 0, 255}, {0, 0, 0}},
		},
		{
			key: "e2e/r16", format: scene.TextureR16Float, centre: 0, rgb: "rgb(t.r, t.r, t.r)",
			first:     dataTextureWrite{0, 0, 2, 2, half(1, 0, 0, 1)},
			second:    dataTextureWrite{0, 0, 2, 1, half(0, 1)},
			wantFirst: [4][3]int{{255, 255, 255}, {0, 0, 0}, {0, 0, 0}, {255, 255, 255}}, wantSecond: [4][3]int{{0, 0, 0}, {255, 255, 255}, {0, 0, 0}, {255, 255, 255}},
		},
		{
			key: "e2e/r32", format: scene.TextureR32Float, centre: 3, rgb: "rgb(t.r, t.r, t.r)",
			first:     dataTextureWrite{0, 0, 2, 2, full(0, 1, 1, 0)},
			second:    dataTextureWrite{0, 0, 1, 1, full(1)},
			wantFirst: [4][3]int{{0, 0, 0}, {255, 255, 255}, {255, 255, 255}, {0, 0, 0}}, wantSecond: [4][3]int{{255, 255, 255}, {255, 255, 255}, {255, 255, 255}, {0, 0, 0}},
		},
	}
}

// Run on real GPUs with the charter runners, for example:
//
//	/home/draco/work/.runtime/tools/gpu/gpu-chrome.sh run -- go test -tags e2e -run TestSelenaDataTexturesReachShaders ./e2e/
//	/home/draco/.local/bin/mac-gpu-run <worktree> go test -tags e2e -run TestSelenaDataTexturesReachShaders ./e2e/
func TestSelenaDataTexturesReachShaders(t *testing.T) {
	chrome := e2eChromePath(t)
	root := e2eRepoRoot(t)
	bootstrap, err := os.ReadFile(filepath.Join(root, "client/js/bootstrap.js"))
	if err != nil {
		t.Fatal(err)
	}
	backends := []string{"webgl"}
	if os.Getenv("GPU_CHROME_ENDPOINT") != "" || os.Getenv("GOSX_SELENA_DATA_TEXTURE_WEBGPU") == "1" {
		backends = append(backends, "webgpu")
	}
	cases := dataTextureCases()
	var meshes []scene.Node
	for _, c := range cases {
		source := fmt.Sprintf(selenaDataTextureSource, c.format, fmt.Sprint(c.centre), c.rgb)
		material, _, err := scene.CompileSelenaMaterial([]byte(source), scene.SelenaMaterialOptions{Uniforms: map[string]any{"tex": scene.DataTextureRef(c.key)}})
		if err != nil {
			t.Fatalf("%s: %v", c.format, err)
		}
		meshes = append(meshes, scene.Mesh{ID: "quad-" + c.format.String(), Geometry: scene.BoxGeometry{Width: 2, Height: 0.1, Depth: 2}, Position: scene.Vector3{X: c.centre, Y: -0.05}, Material: material})
	}
	for _, backend := range backends {
		t.Run(backend, func(t *testing.T) {
			yes, no := true, false
			props := scene.Props{
				Width: 320, Height: 240, Background: "#000000", AutoRotate: &no,
				Camera:      scene.PerspectiveCamera{Position: scene.Vector3{Y: 6, Z: 0.001}, Rotation: scene.Euler{X: -math.Pi / 2}, FOV: 60, Near: 0.1, Far: 30},
				Environment: scene.Environment{ToneMapping: "linear"},
				Graph:       scene.NewGraph(meshes...),
			}
			if backend == "webgl" {
				props.ForceWebGL = &yes
				props.RequireWebGL = &yes
			} else {
				props.PreferWebGPU = &yes
			}
			manifest, err := json.Marshal(map[string]any{"engines": []any{map[string]any{
				"id": "selena-data", "component": "GoSXScene3D", "kind": "surface", "mountId": "selena-data-root", "props": props,
			}}})
			if err != nil {
				t.Fatal(err)
			}
			page := []byte(fmt.Sprintf(`<!doctype html><html><head><meta charset="utf-8"><style>html,body{margin:0;background:#000}#selena-data-root{width:320px;height:240px}</style></head><body><div id="selena-data-root"></div><script id="gosx-manifest" type="application/json">%s</script><script data-gosx-script="bootstrap" data-gosx-scene3d-data-texture-url="/gosx/bootstrap-feature-scene3d-data-texture.js" src="/bootstrap.js"></script></body></html>`, manifest))
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			defer server.Close()
			url := server.URL
			if e2eWindowsRelay() {
				url = e2eExposeToWindows(t, server.URL)
			}
			flags := map[string]any{"use-angle": "swiftshader", "enable-unsafe-swiftshader": true}
			if backend == "webgl" {
				flags["disable-features"] = "WebGPU"
			} else {
				flags["enable-unsafe-webgpu"] = true
			}
			browser := newBrowserPage(t, chrome, flags, 400, 320, `window.__gosx_scene3d_render_truth=true`, 150*time.Second)
			browser.navigate(t, url+"/")
			browser.waitFor(t, `!!(document.getElementById('selena-data-root')||{}).__gosxScene3DHandle`, 60*time.Second, "the Scene3D handle")
			browser.waitFor(t, `document.querySelector('[data-gosx-scene3d-mounted]')?.getAttribute('data-gosx-scene3d-backend')==='`+backend+`'`, 60*time.Second, "the "+backend+" backend (a fallback would hide a failure)")

			// write sends GSXT frames through the mount handle, as scene.TextureWriter does.
			write := func(frames [][]byte) {
				encoded := make([]string, len(frames))
				for i, f := range frames {
					encoded[i] = base64.StdEncoding.EncodeToString(f)
				}
				list, _ := json.Marshal(encoded)
				var results []map[string]any
				browser.eval(t, `(async()=>{const h=document.getElementById('selena-data-root').__gosxScene3DHandle;const out=[];for(const b of `+string(list)+`){out.push(await h.applyDataTexture(Uint8Array.from(atob(b),c=>c.charCodeAt(0))))}return out})()`, &results)
				for i, r := range results {
					if r["applied"] != true {
						t.Fatalf("frame %d was not applied: %v", i, r)
					}
				}
			}
			enc := scene.NewDataTextureEncoder()
			var round1, round2 [][]byte
			for _, c := range cases {
				create, err := enc.Create(c.key, 2, 2, c.format)
				must(t, err)
				round1 = append(round1, append([]byte(nil), create...))
				f, err := enc.WriteTexture(c.key, c.first.w, c.first.h, c.format, c.first.texels, c.first.x, c.first.y)
				must(t, err)
				round1 = append(round1, append([]byte(nil), f...))
				f, err = enc.WriteTexture(c.key, c.second.w, c.second.h, c.format, c.second.texels, c.second.x, c.second.y)
				must(t, err)
				round2 = append(round2, append([]byte(nil), f...))
			}

			// Pixel of quadrant (col,row): x = centre +- 0.5 m, z = -0.5 or +0.5 m below the screen centre; 34.6 px per metre.
			read := func() ([3][4][3]int, []byte) {
				var shot []byte
				if err := chromedp.Run(browser.ctx, chromedp.Screenshot("canvas", &shot, chromedp.ByQuery)); err != nil {
					t.Fatal(err)
				}
				img, err := png.Decode(bytes.NewReader(shot))
				must(t, err)
				b := img.Bounds()
				var got [3][4][3]int
				for i, c := range cases {
					for q := 0; q < 4; q++ {
						col, row := q%2, q/2
						px := 160 + 34.64*(c.centre+float64(col)-0.5)
						py := 120 + 34.64*(float64(row)-0.5)
						got[i][q] = pixel8(img, b.Min.X+int(px*float64(b.Dx())/320), b.Min.Y+int(py*float64(b.Dy())/240))
					}
				}
				return got, shot
			}
			near := func(a, b [3]int) bool {
				for i := range a {
					if d := a[i] - b[i]; d > 40 || d < -40 {
						return false
					}
				}
				return true
			}
			expect := func(label string, round [][]byte, want func(dataTextureCase) [4][3]int) {
				t.Helper()
				write(round)
				var got [3][4][3]int
				var shot []byte
				ok := false
				for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline) && !ok; time.Sleep(250 * time.Millisecond) {
					got, shot = read()
					ok = true
					for i, c := range cases {
						for q := 0; q < 4; q++ {
							ok = ok && near(got[i][q], want(c)[q])
						}
					}
				}
				if out := os.Getenv("GOSX_SELENA_DATA_TEXTURE_OUT"); out != "" && len(shot) > 0 {
					_ = os.MkdirAll(out, 0o755)
					_ = os.WriteFile(filepath.Join(out, backend+"-"+label+".png"), shot, 0o644)
				}
				if !ok {
					t.Fatalf("%s %s: quadrant colours %v do not match the texels written; pageErrors=%v console=%s", backend, label, got, browser.PageErrors(), browser.Console())
				}
				t.Logf("%s %s: quadrant colours %v", backend, label, got)
			}
			expect("full-write", round1, func(c dataTextureCase) [4][3]int { return c.wantFirst })
			expect("rect-write", round2, func(c dataTextureCase) [4][3]int { return c.wantSecond })
			if errors := browser.PageErrors(); len(errors) != 0 {
				t.Fatalf("page errors: %v", errors)
			}
			if console := browser.Console(); strings.Contains(console, "data texture") {
				t.Fatalf("data texture errors in the console: %s", console)
			}
		})
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
