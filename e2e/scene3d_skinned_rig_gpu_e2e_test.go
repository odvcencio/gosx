//go:build e2e

package e2e

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
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

const skinnedRigJoints = 128

// skinnedRigGLB builds a 128-joint rig with no mesh content in common with any
// game. Two quads sit left and right of the origin. The left quad is bound to
// joint 5 and the right quad to joint 120, past the old 64-joint cap. An
// animation lifts both joints by 2 units, so a renderer that skins both quads
// puts them at the same height and one that drops joints above 63 leaves the
// right quad on the ground.
func skinnedRigGLB(t *testing.T) []byte {
	t.Helper()
	var bin bytes.Buffer
	view := func(data any, target int) int {
		for bin.Len()%4 != 0 {
			bin.WriteByte(0)
		}
		offset := bin.Len()
		if err := binary.Write(&bin, binary.LittleEndian, data); err != nil {
			t.Fatal(err)
		}
		views = append(views, map[string]any{"buffer": 0, "byteOffset": offset, "byteLength": bin.Len() - offset, "target": target})
		return len(views) - 1
	}
	var accessors []map[string]any
	add := func(viewIndex, componentType, count int, kind string, extra map[string]any) int {
		a := map[string]any{"bufferView": viewIndex, "componentType": componentType, "count": count, "type": kind}
		for k, v := range extra {
			a[k] = v
		}
		accessors = append(accessors, a)
		return len(accessors) - 1
	}
	quad := func(x0, x1 float32, joint uint8) map[string]int {
		pos := []float32{x0, 0, 0, x1, 0, 0, x1, 1, 0, x0, 1, 0}
		nrm := []float32{0, 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1}
		jnt := []uint8{joint, 0, 0, 0, joint, 0, 0, 0, joint, 0, 0, 0, joint, 0, 0, 0}
		wgt := []float32{1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0}
		idx := []uint16{0, 1, 2, 0, 2, 3}
		return map[string]int{
			"POSITION":  add(view(pos, 34962), 5126, 4, "VEC3", map[string]any{"min": []float32{x0, 0, 0}, "max": []float32{x1, 1, 0}}),
			"NORMAL":    add(view(nrm, 34962), 5126, 4, "VEC3", nil),
			"JOINTS_0":  add(view(jnt, 34962), 5121, 4, "VEC4", nil),
			"WEIGHTS_0": add(view(wgt, 34962), 5126, 4, "VEC4", nil),
			"INDICES":   add(view(idx, 34963), 5123, 6, "SCALAR", nil),
		}
	}
	left, right := quad(-3, -2, 5), quad(2, 3, 120)
	ibm := make([]float32, skinnedRigJoints*16)
	for j := 0; j < skinnedRigJoints; j++ {
		ibm[j*16], ibm[j*16+5], ibm[j*16+10], ibm[j*16+15] = 1, 1, 1, 1
	}
	ibmAccessor := add(view(ibm, 0), 5126, skinnedRigJoints, "MAT4", nil)
	times := add(view([]float32{0, 1}, 0), 5126, 2, "SCALAR", map[string]any{"min": []float32{0}, "max": []float32{1}})
	lift := add(view([]float32{0, 2, 0, 0, 2, 0}, 0), 5126, 2, "VEC3", nil)

	prim := func(a map[string]int, material int) map[string]any {
		return map[string]any{
			"attributes": map[string]int{"POSITION": a["POSITION"], "NORMAL": a["NORMAL"], "JOINTS_0": a["JOINTS_0"], "WEIGHTS_0": a["WEIGHTS_0"]},
			"indices":    a["INDICES"], "material": material,
		}
	}
	unlit := map[string]any{"KHR_materials_unlit": map[string]any{}}
	nodes := []map[string]any{{"name": "skinned", "mesh": 0, "skin": 0}, {"name": "rig"}}
	var joints, children []int
	for j := 0; j < skinnedRigJoints; j++ {
		nodes = append(nodes, map[string]any{"name": fmt.Sprintf("joint-%d", j)})
		joints = append(joints, len(nodes)-1)
		children = append(children, len(nodes)-1)
	}
	nodes[1]["children"] = children
	doc := map[string]any{
		"asset":          map[string]any{"version": "2.0", "generator": "gosx-e2e"},
		"extensionsUsed": []string{"KHR_materials_unlit"},
		"scene":          0,
		"scenes":         []map[string]any{{"nodes": []int{0, 1}}},
		"nodes":          nodes,
		"meshes":         []map[string]any{{"primitives": []map[string]any{prim(left, 0), prim(right, 1)}}},
		"materials": []map[string]any{
			{"doubleSided": true, "pbrMetallicRoughness": map[string]any{"baseColorFactor": []float32{0, 1, 0, 1}}, "extensions": unlit},
			{"doubleSided": true, "pbrMetallicRoughness": map[string]any{"baseColorFactor": []float32{1, 0, 0, 1}}, "extensions": unlit},
		},
		"skins": []map[string]any{{"joints": joints, "inverseBindMatrices": ibmAccessor}},
		"animations": []map[string]any{{"name": "lift",
			"samplers": []map[string]any{{"input": times, "output": lift, "interpolation": "LINEAR"}},
			"channels": []map[string]any{
				{"sampler": 0, "target": map[string]any{"node": joints[5], "path": "translation"}},
				{"sampler": 0, "target": map[string]any{"node": joints[120], "path": "translation"}},
			}}},
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

// views is shared by the closures above; declared here to keep the helper flat.
var views []map[string]any

type rigReading struct {
	Backend                  string `json:"backend"`
	Fallback                 string `json:"fallback"`
	GreenPixels, RedPixels   int
	GreenCenterY, RedCenterY float64
}

func gpuRunnerAvailable() bool {
	return os.Getenv("GOSX_WATER_E2E_REQUIRE_WEBGPU") == "1" || os.Getenv("GPU_CHROME_ENDPOINT") != ""
}

// TestSkinnedRigBeyond64JointsOnTheGPU renders a 128-joint rig through WebGPU
// and through WebGL2 on a real GPU. Both quads must lift by the same amount.
// Run it with the GPU runners in the charter:
//
//	gpu-chrome.sh run -- go test -tags e2e -run SkinnedRig ./e2e
//	mac-gpu-run <worktree> go test -tags e2e -run SkinnedRig ./e2e
func TestSkinnedRigBeyond64JointsOnTheGPU(t *testing.T) {
	if !gpuRunnerAvailable() {
		t.Skip("needs a real GPU: use gpu-chrome.sh run -- or mac-gpu-run")
	}
	chrome := e2eChromePath(t)
	root := e2eRepoRoot(t)
	bootstrap, err := os.ReadFile(filepath.Join(root, "client/js/bootstrap.js"))
	if err != nil {
		t.Fatal(err)
	}
	views = nil
	glb := skinnedRigGLB(t)

	for _, backend := range []string{"webgpu", "webgl"} {
		t.Run(backend, func(t *testing.T) {
			yes, no := true, false
			props := scene.Props{
				Width: 480, Height: 320, Background: "#000000", AutoRotate: &no,
				Camera: scene.PerspectiveCamera{Position: scene.Vector3{Y: 1.5, Z: 9}, FOV: 50, Near: 0.1, Far: 40},
				Graph: scene.NewGraph(scene.Model{
					ID: "rig", Src: "/rig.glb", Animation: "lift", Loop: &yes, Position: scene.Vector3{Y: -1},
				}),
			}
			if backend == "webgl" {
				props.ForceWebGL, props.RequireWebGL = &yes, &yes
			} else {
				props.PreferWebGPU = &yes
			}
			manifest, err := json.Marshal(map[string]any{"engines": []any{map[string]any{
				"id": "rig", "component": "GoSXScene3D", "kind": "surface", "mountId": "rig-root", "props": props,
			}}})
			if err != nil {
				t.Fatal(err)
			}
			page := fmt.Sprintf(`<!doctype html><html><head><meta charset="utf-8"><style>html,body{margin:0;background:#000}#rig-root{width:480px;height:320px}</style></head><body><div id="rig-root"></div><script id="gosx-manifest" type="application/json">%s</script><script src="/bootstrap.js"></script></body></html>`, manifest)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/rig.glb":
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

			var reading rigReading
			deadline := time.Now().Add(90 * time.Second)
			for time.Now().Before(deadline) {
				browser.eval(t, `(() => {const e=document.querySelector('[data-gosx-scene3d-mounted]');return {backend:e?.getAttribute('data-gosx-scene3d-backend')||'',fallback:e?.getAttribute('data-gosx-scene3d-render-mesh-material-fallback')||''}})()`, &reading)
				if reading.Backend == backend {
					var shot []byte
					if err := chromedp.Run(browser.ctx, chromedp.Screenshot("canvas", &shot, chromedp.ByQuery)); err != nil {
						t.Fatal(err)
					}
					img, err := png.Decode(bytes.NewReader(shot))
					if err != nil {
						t.Fatal(err)
					}
					var gN, rN int
					var gY, rY float64
					b := img.Bounds()
					for y := b.Min.Y; y < b.Max.Y; y++ {
						for x := b.Min.X; x < b.Max.X; x++ {
							r, g, bl, _ := img.At(x, y).RGBA()
							switch {
							case g > 40000 && r < 12000 && bl < 12000:
								gN++
								gY += float64(y)
							case r > 40000 && g < 12000 && bl < 12000:
								rN++
								rY += float64(y)
							}
						}
					}
					reading.GreenPixels, reading.RedPixels = gN, rN
					if gN > 0 {
						reading.GreenCenterY = gY / float64(gN)
					}
					if rN > 0 {
						reading.RedCenterY = rY / float64(rN)
					}
					if gN > 150 && rN > 150 && abs(reading.GreenCenterY-reading.RedCenterY) < 4 {
						t.Logf("%s: both quads lifted together: %+v", backend, reading)
						if errs := browser.PageErrors(); len(errs) != 0 {
							t.Fatalf("page errors: %v", errs)
						}
						return
					}
				}
				time.Sleep(250 * time.Millisecond)
			}
			t.Fatalf("%s: the joint-120 quad did not follow the joint-5 quad: %+v; pageErrors=%v; console=%s", backend, reading, browser.PageErrors(), browser.Console())
		})
	}
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
