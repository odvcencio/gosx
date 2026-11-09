//go:build e2e

package e2e

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
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

// uv1Fixture is a flat quad with two UV sets. TEXCOORD_0 runs left to right;
// TEXCOORD_1 is the mirror image. The map under test is a two-colour (or
// black and white) texture split down the middle, so which set the shader reads
// decides which half of the quad shows which colour.
type uv1Fixture struct {
	kind        string // "emissive" or "occlusion"
	texCoord    int    // the set the map reads
	left, right color.RGBA
}

func uv1HalfPNG(t *testing.T, left, right color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			if x < 32 {
				img.SetRGBA(x, y, left)
			} else {
				img.SetRGBA(x, y, right)
			}
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func uv1GLB(t *testing.T, f uv1Fixture) []byte {
	t.Helper()
	var bin bytes.Buffer
	var views []map[string]any
	view := func(data []byte, target int) int {
		for bin.Len()%4 != 0 {
			bin.WriteByte(0)
		}
		offset := bin.Len()
		bin.Write(data)
		v := map[string]any{"buffer": 0, "byteOffset": offset, "byteLength": len(data)}
		if target != 0 {
			v["target"] = target
		}
		views = append(views, v)
		return len(views) - 1
	}
	raw := func(v any) []byte {
		var b bytes.Buffer
		if err := binary.Write(&b, binary.LittleEndian, v); err != nil {
			t.Fatal(err)
		}
		return b.Bytes()
	}
	pos := []float32{-2, -1.5, 0, 2, -1.5, 0, 2, 1.5, 0, -2, 1.5, 0}
	nrm := []float32{0, 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1}
	uv0 := []float32{0, 1, 1, 1, 1, 0, 0, 0}
	uv1 := []float32{1, 1, 0, 1, 0, 0, 1, 0} // mirrored in u
	idx := []uint16{0, 1, 2, 0, 2, 3}
	accessors := []map[string]any{
		{"bufferView": view(raw(pos), 34962), "componentType": 5126, "count": 4, "type": "VEC3", "min": []float32{-2, -1.5, 0}, "max": []float32{2, 1.5, 0}},
		{"bufferView": view(raw(nrm), 34962), "componentType": 5126, "count": 4, "type": "VEC3"},
		{"bufferView": view(raw(uv0), 34962), "componentType": 5126, "count": 4, "type": "VEC2"},
		{"bufferView": view(raw(uv1), 34962), "componentType": 5126, "count": 4, "type": "VEC2"},
		{"bufferView": view(raw(idx), 34963), "componentType": 5123, "count": 6, "type": "SCALAR"},
	}
	image := view(uv1HalfPNG(t, f.left, f.right), 0)
	material := map[string]any{"doubleSided": true}
	tex := map[string]any{"index": 0, "texCoord": f.texCoord}
	switch f.kind {
	case "emissive":
		material["pbrMetallicRoughness"] = map[string]any{"baseColorFactor": []float32{0, 0, 0, 1}, "metallicFactor": 0, "roughnessFactor": 1}
		material["emissiveFactor"] = []float32{1, 1, 1}
		material["emissiveTexture"] = tex
	default:
		material["pbrMetallicRoughness"] = map[string]any{"baseColorFactor": []float32{1, 1, 1, 1}, "metallicFactor": 0, "roughnessFactor": 1}
		tex["strength"] = 1
		material["occlusionTexture"] = tex
	}
	doc := map[string]any{
		"asset":  map[string]any{"version": "2.0", "generator": "gosx-e2e"},
		"scene":  0,
		"scenes": []map[string]any{{"nodes": []int{0}}},
		"nodes":  []map[string]any{{"mesh": 0}},
		"meshes": []map[string]any{{"primitives": []map[string]any{{
			"attributes": map[string]int{"POSITION": 0, "NORMAL": 1, "TEXCOORD_0": 2, "TEXCOORD_1": 3}, "indices": 4, "material": 0,
		}}}},
		"materials":   []map[string]any{material},
		"textures":    []map[string]any{{"source": 0}},
		"images":      []map[string]any{{"bufferView": image, "mimeType": "image/png"}},
		"accessors":   accessors,
		"bufferViews": views,
		"buffers":     []map[string]any{{"byteLength": bin.Len()}},
	}
	chunk, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	for len(chunk)%4 != 0 {
		chunk = append(chunk, ' ')
	}
	payload := bin.Bytes()
	for len(payload)%4 != 0 {
		payload = append(payload, 0)
	}
	var out bytes.Buffer
	out.WriteString("glTF")
	_ = binary.Write(&out, binary.LittleEndian, uint32(2))
	_ = binary.Write(&out, binary.LittleEndian, uint32(12+8+len(chunk)+8+len(payload)))
	_ = binary.Write(&out, binary.LittleEndian, uint32(len(chunk)))
	_ = binary.Write(&out, binary.LittleEndian, uint32(0x4E4F534A))
	out.Write(chunk)
	_ = binary.Write(&out, binary.LittleEndian, uint32(len(payload)))
	_ = binary.Write(&out, binary.LittleEndian, uint32(0x004E4942))
	out.Write(payload)
	return out.Bytes()
}

// halves reads a screenshot and returns, for the quad's lit pixels, the mean
// brightness of its left and right fifth and the x centroids of strongly red
// and strongly green pixels.
type uv1Reading struct {
	Backend                     string
	LeftLuma, RightLuma         float64
	RedX, GreenX                float64
	RedPixels, GreenPixels, Lit int
}

func readUV1(t *testing.T, shot []byte) uv1Reading {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(shot))
	if err != nil {
		t.Fatal(err)
	}
	b := img.Bounds()
	minX, maxX := b.Max.X, b.Min.X
	luma := func(x, y int) float64 {
		r, g, bl, _ := img.At(x, y).RGBA()
		return (0.2126*float64(r) + 0.7152*float64(g) + 0.0722*float64(bl)) / 65535
	}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if luma(x, y) > 0.04 {
				if x < minX {
					minX = x
				}
				if x > maxX {
					maxX = x
				}
			}
		}
	}
	var r uv1Reading
	if maxX <= minX {
		return r
	}
	width := maxX - minX
	var lSum, rSum float64
	var lN, rN int
	var redSum, greenSum float64
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := minX; x <= maxX; x++ {
			cr, cg, cb, _ := img.At(x, y).RGBA()
			rr, gg, bb := float64(cr)/65535, float64(cg)/65535, float64(cb)/65535
			l := luma(x, y)
			if l > 0.02 {
				r.Lit++
			}
			if x < minX+width/5 {
				lSum += l
				lN++
			}
			if x > maxX-width/5 {
				rSum += l
				rN++
			}
			switch {
			case rr > 0.3 && rr > 2*gg && rr > 2*bb:
				r.RedPixels++
				redSum += float64(x)
			case gg > 0.3 && gg > 2*rr && gg > 2*bb:
				r.GreenPixels++
				greenSum += float64(x)
			}
		}
	}
	if lN > 0 {
		r.LeftLuma = lSum / float64(lN)
	}
	if rN > 0 {
		r.RightLuma = rSum / float64(rN)
	}
	if r.RedPixels > 0 {
		r.RedX = redSum / float64(r.RedPixels)
	}
	if r.GreenPixels > 0 {
		r.GreenX = greenSum / float64(r.GreenPixels)
	}
	return r
}

// TestUV1MapsReadTheSecondUVSetOnTheGPU renders an emissive map and an
// occlusion map that read TEXCOORD_1 on a real GPU. TEXCOORD_1 mirrors
// TEXCOORD_0, so a renderer that ignores the second set puts each colour on the
// opposite half of the quad. GOSX_UV1_CONTROL=uv0 points the maps at TEXCOORD_0
// and expects the opposite result, which proves the checks can tell them apart.
//
//	gpu-chrome.sh run -- go test -tags e2e -run UV1Maps ./e2e
//	mac-gpu-run <worktree> go test -tags e2e -run UV1Maps ./e2e
func TestUV1MapsReadTheSecondUVSetOnTheGPU(t *testing.T) {
	if !gpuRunnerAvailable() {
		t.Skip("needs a real GPU: use gpu-chrome.sh run -- or mac-gpu-run")
	}
	chrome := e2eChromePath(t)
	root := e2eRepoRoot(t)
	bootstrap, err := os.ReadFile(filepath.Join(root, "client/js/bootstrap.js"))
	if err != nil {
		t.Fatal(err)
	}
	control := os.Getenv("GOSX_UV1_CONTROL") == "uv0"
	texCoord := 1
	if control {
		texCoord = 0
	}
	red, green := color.RGBA{255, 0, 0, 255}, color.RGBA{0, 255, 0, 255}
	black, white := color.RGBA{0, 0, 0, 255}, color.RGBA{255, 255, 255, 255}
	cases := []struct {
		name string
		f    uv1Fixture
		// check receives the reading. mirrored is true when the maps read TEXCOORD_1.
		check func(t *testing.T, r uv1Reading, mirrored bool) bool
	}{
		{"emissive", uv1Fixture{kind: "emissive", texCoord: texCoord, left: red, right: green},
			func(t *testing.T, r uv1Reading, mirrored bool) bool {
				if r.RedPixels < 200 || r.GreenPixels < 200 {
					return false
				}
				return (r.RedX > r.GreenX) == mirrored
			}},
		{"occlusion", uv1Fixture{kind: "occlusion", texCoord: texCoord, left: black, right: white},
			func(t *testing.T, r uv1Reading, mirrored bool) bool {
				if r.Lit < 500 {
					return false
				}
				// The black half must be clearly darker than the white half.
				if mirrored {
					return r.RightLuma < 0.6*r.LeftLuma
				}
				return r.LeftLuma < 0.6*r.RightLuma
			}},
	}
	for _, backend := range []string{"webgl", "webgpu"} {
		for _, tc := range cases {
			t.Run(backend+"/"+tc.name, func(t *testing.T) {
				yes, no := true, false
				glb := uv1GLB(t, tc.f)
				props := scene.Props{
					Width: 480, Height: 320, Background: "#000000", AutoRotate: &no,
					Camera: scene.PerspectiveCamera{Position: scene.Vector3{Z: 6}, FOV: 50, Near: 0.1, Far: 40},
					// Occlusion only dims the environment term, so light the quad with
					// that term alone.
					Environment: scene.Environment{AmbientColor: "#ffffff", AmbientIntensity: 1.5},
					Graph:       scene.NewGraph(scene.Model{ID: "quad", Src: "/quad.glb"}),
				}
				if backend == "webgl" {
					props.ForceWebGL, props.RequireWebGL = &yes, &yes
				} else {
					props.PreferWebGPU = &yes
				}
				manifest, err := json.Marshal(map[string]any{"engines": []any{map[string]any{
					"id": "uv1", "component": "GoSXScene3D", "kind": "surface", "mountId": "uv1-root", "props": props,
				}}})
				if err != nil {
					t.Fatal(err)
				}
				page := fmt.Sprintf(`<!doctype html><html><head><meta charset="utf-8"><style>html,body{margin:0;background:#000}#uv1-root{width:480px;height:320px}</style></head><body><div id="uv1-root"></div><script id="gosx-manifest" type="application/json">%s</script><script src="/bootstrap.js"></script></body></html>`, manifest)
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch {
					case r.URL.Path == "/quad.glb":
						w.Header().Set("Content-Type", "model/gltf-binary")
						_, _ = w.Write(glb)
					case r.URL.Path == "/bootstrap.js":
						w.Header().Set("Content-Type", "application/javascript")
						_, _ = w.Write(bootstrap)
					case r.URL.Path == "/":
						w.Header().Set("Content-Type", "text/html")
						_, _ = w.Write([]byte(page))
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
				browser := newBrowserPage(t, chrome, map[string]any{"enable-unsafe-webgpu": true}, 520, 360, `window.__gosx_scene3d_render_truth=true`, 150*time.Second)
				browser.navigate(t, url+"/")
				var reading uv1Reading
				deadline := time.Now().Add(60 * time.Second)
				for time.Now().Before(deadline) {
					var state struct{ Backend string }
					browser.eval(t, `(() => {const e=document.querySelector('[data-gosx-scene3d-mounted]');return {Backend:e?.getAttribute('data-gosx-scene3d-backend')||''}})()`, &state)
					if state.Backend == backend {
						var shot []byte
						if err := chromedp.Run(browser.ctx, chromedp.Screenshot("canvas", &shot, chromedp.ByQuery)); err != nil {
							t.Fatal(err)
						}
						reading = readUV1(t, shot)
						reading.Backend = state.Backend
						// The first frames can draw before the variant program links, so keep
						// reading until the expected half is lit or the deadline passes.
						if reading.Lit > 500 && tc.check(t, reading, !control) {
							if errs := browser.PageErrors(); len(errs) != 0 {
								t.Fatalf("page errors: %v", errs)
							}
							t.Logf("%s %s: %+v", backend, tc.name, reading)
							return
						}
					}
					time.Sleep(250 * time.Millisecond)
				}
				t.Fatalf("%s %s: the expected half never lit (control=%v): %+v; pageErrors=%v; console=%s", backend, tc.name, control, reading, browser.PageErrors(), browser.Console())
			})
		}
	}
}
