//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image/png"
	"math"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/scene"
	"m31labs.dev/gosx/server"
)

func TestScene3DTypedOrderedPostUniforms(t *testing.T) {
	material, _, err := scene.CompileSelenaPost([]byte(`material TypedTint kind post {
 param red : float = 1.0
 param green : float = 1.0
 surface(post) -> color {
  let c = sceneColor(post.uv)
  return rgb(c.r * red, c.g * green, c.b, c.a)
 }
}`), scene.SelenaMaterialOptions{})
	if err != nil {
		t.Fatal(err)
	}
	addMaterial, _, err := scene.CompileSelenaPost([]byte(`material TypedAdd kind post {
 param red : float = 0.0
 param green : float = 0.0
 surface(post) -> color {
  let c = sceneColor(post.uv)
  return rgb(c.r + red * 0.2, c.g + green * 0.2, c.b, c.a)
 }
}`), scene.SelenaMaterialOptions{})
	if err != nil {
		t.Fatal(err)
	}
	app := server.New()
	app.SetRuntimeRoot(e2eRepoRoot(t))
	app.Page("/", func(ctx *server.Context) gosx.Node {
		effects := []scene.PostEffect{scene.Vignette{Intensity: .7}, scene.ColorGrade{Exposure: 1.1, Contrast: .6, Saturation: .5}}
		if ctx.Request.URL.Query().Get("reverse") == "1" {
			effects[0], effects[1] = effects[1], effects[0]
		}
		effects = append(effects, scene.CustomPost{Name: "red-effect", Material: &material, Uniforms: map[string]any{"red": 1., "green": 1.}}, scene.CustomPost{Name: "green-effect", Material: &addMaterial, Uniforms: map[string]any{"red": 0., "green": 0.}})
		props := scene.Props{Width: 240, Height: 180, ForceWebGL: scene.Bool(true), RequireWebGL: scene.Bool(true), Background: "#102030", Camera: scene.PerspectiveCamera{Position: scene.Vec3(0, 0, 5), FOV: 50}, Environment: scene.Environment{AmbientColor: "#ffffff", AmbientIntensity: .6}, PostFX: scene.PostFX{Effects: effects}, Graph: scene.NewGraph(scene.Mesh{ID: "sphere", Geometry: scene.SphereGeometry{Radius: 1.2, Segments: 16}, Material: scene.StandardMaterial{Color: "#50d0a0", Roughness: .6}})}
		return gosx.El("div", gosx.Attrs(gosx.Attr("style", "width:240px;height:180px")), ctx.Engine(props.EngineConfig(), gosx.Text("loading")))
	})
	srv := httptest.NewServer(app.Build())
	defer srv.Close()
	// Observation only: record real shader submissions/program creation without
	// changing source, return values or application state.
	page := newBrowserPage(t, e2eChromePath(t), map[string]any{"use-gl": "angle", "use-angle": "swiftshader", "enable-unsafe-swiftshader": true, "enable-webgl": true, "ignore-gpu-blocklist": true}, 640, 640, `window.__postShaders=[];window.__postPrograms=0;for(const cls of [window.WebGLRenderingContext,window.WebGL2RenderingContext]){if(!cls)continue;const p=cls.prototype,s=p.shaderSource,c=p.createProgram;p.shaderSource=function(a,b){__postShaders.push(b);return s.call(this,a,b)};p.createProgram=function(){__postPrograms++;return c.call(this)}}`, 95*time.Second)
	var lastShot []byte
	capture := func(label string) []byte {
		t.Helper()
		var shot []byte
		if err := chromedp.Run(page.ctx, chromedp.CaptureScreenshot(&shot)); err != nil {
			t.Fatal(err)
		}
		lastShot = shot
		img, err := png.Decode(bytes.NewReader(shot))
		if err != nil {
			t.Fatal(err)
		}
		var samples []byte
		var rect struct{ X, Y float64 }
		page.eval(t, `(()=>{const r=document.querySelector('canvas').getBoundingClientRect();return {X:r.x,Y:r.y}})()`, &rect)
		if rect.X < 0 || rect.Y < 0 || int(rect.X)+240 > img.Bounds().Max.X || int(rect.Y)+180 > img.Bounds().Max.Y {
			t.Fatalf("canvas sample region outside screenshot: origin=%+v bounds=%v", rect, img.Bounds())
		}
		for y := 9; y < 180; y += 18 {
			for x := 7; x < 240; x += 15 {
				r, g, b, _ := img.At(int(rect.X)+x, int(rect.Y)+y).RGBA()
				samples = append(samples, byte(r>>8), byte(g>>8), byte(b>>8))
			}
		}
		return samples
	}
	converge := func(label string, accept func([]byte) bool) []byte {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		var prev []byte
		stable := 0
		for time.Now().Before(deadline) {
			got := capture(label)
			if accept(got) && bytes.Equal(got, prev) {
				stable++
			} else {
				stable = 0
			}
			prev = got
			if stable >= 2 {
				if path := os.Getenv("GOSX_POST_SCREENSHOT"); path != "" {
					if err := os.WriteFile(path+"."+label+".png", lastShot, 0600); err != nil {
						t.Fatal(err)
					}
				}
				t.Logf("%s stable sample=%v", label, got[240:249])
				return got
			}
			if err := chromedp.Run(page.ctx, chromedp.Sleep(50*time.Millisecond)); err != nil {
				t.Fatal(err)
			}
		}
		if path := os.Getenv("GOSX_POST_SCREENSHOT"); path != "" {
			if err := os.WriteFile(path+"."+label+".failed.png", lastShot, 0600); err != nil {
				t.Fatal(err)
			}
		}
		t.Fatalf("%s pixels did not converge, last=%v", label, prev)
		return nil
	}
	visible := func(p []byte) bool {
		for _, v := range p {
			if v > 30 {
				return true
			}
		}
		return false
	}
	ready := func() {
		page.waitFor(t, `!!document.querySelector('[data-gosx-scene3d-renderer="webgl"][data-gosx-scene3d-ready="true"]')`, 15*time.Second, "typed post ready")
	}
	page.navigate(t, srv.URL+"/?reverse=1")
	ready()
	reverse := converge("reverse", visible)
	page.navigate(t, srv.URL)
	ready()
	base := converge("base", visible)
	if bytes.Equal(base, reverse) {
		t.Fatal("reversing supported neighbors did not change pixels")
	}
	diffs := []int{}
	for i := 0; i < len(base); i += 3 {
		if !bytes.Equal(base[i:i+3], reverse[i:i+3]) {
			diffs = append(diffs, i/3)
		}
	}
	t.Logf("order differs at %d/160 sample locations: %v", len(diffs), diffs)
	page.eval(t, `window.__postCanvas=document.querySelector('canvas');window.__postMount=document.querySelector('[data-gosx-scene3d-ready="true"]');window.__postHandle=__postMount.__gosxScene3DHandle;window.__postShaderSnapshot=JSON.stringify(__postShaders);window.__postProgramSnapshot=__postPrograms`, nil)
	var handlePresent bool
	page.eval(t, `!!__postHandle && typeof __postHandle.applyCommands==='function'`, &handlePresent)
	if !handlePresent {
		t.Fatal("mounted command handle absent before patches")
	}
	var shaderProof bool
	// Compare exact Go-compiled fragment payloads, not a marker supplied by the observer.
	// Match the checked-in WebGL ES precision normalization at webgl.ts:1875;
	// the resulting complete source must actually reach the GL observer.
	normalize := func(src string) string {
		src = strings.TrimSpace(src)
		if strings.HasPrefix(src, "#version 300 es") {
			return src
		}
		for _, kind := range []string{"float", "int"} {
			src = regexp.MustCompile(`\bprecision\s+(lowp|mediump|highp)\s+`+kind+`\s*;`).ReplaceAllString(src, "precision highp "+kind+";")
		}
		if !strings.Contains(src, "precision highp float;") {
			src = "precision highp float;\n" + src
		}
		if !strings.Contains(src, "precision highp int;") {
			src = strings.Replace(src, "precision highp float;", "precision highp float;\nprecision highp int;", 1)
		}
		return src
	}
	tintJSON, _ := json.Marshal(normalize(material.FragmentGLSL))
	addJSON, _ := json.Marshal(normalize(addMaterial.FragmentGLSL))
	page.eval(t, fmt.Sprintf(`__postPrograms>0&&__postShaders.includes(%s)&&__postShaders.includes(%s)`, tintJSON, addJSON), &shaderProof)
	if !shaderProof {
		t.Fatal("shader observer did not capture both compiled Go custom payloads")
	}
	patch := func(name, key string, value float64) {
		t.Helper()
		cmd, err := json.Marshal([]scene.Command{scene.SetPostUniformsCommand([]scene.PostUniformPatch{{Name: name, Uniforms: map[string]any{key: value}}})})
		if err != nil {
			t.Fatal(err)
		}
		page.eval(t, fmt.Sprintf(`window.__postDone=false;window.__gosx.scene3d.dispatchCommands(__postMount,%s).then(()=>window.__postDone=true)`, cmd), nil)
		page.waitFor(t, `window.__postDone===true`, 5*time.Second, "production command applied")
	}
	patch("missing", "red", 0)
	if !bytes.Equal(base, converge("unknown", func(p []byte) bool { return bytes.Equal(base, p) })) {
		t.Fatal("unknown named patch changed pixels")
	}
	patch("red-effect", "red", .2)
	response := func(channel int, mul, add float64) func([]byte) bool {
		return func(p []byte) bool {
			for i, v := range base {
				want := float64(v)
				if i%3 == channel {
					want = math.Min(255, want*mul+add)
				}
				if math.Abs(float64(p[i])-want) > 2 {
					return false
				}
			}
			return true
		}
	}
	red := converge("red", response(0, .2, 0))
	if bytes.Equal(base, red) {
		t.Fatal("red patch invisible")
	}
	patch("red-effect", "red", 1)
	if !bytes.Equal(base, converge("reset-red", func(p []byte) bool { return bytes.Equal(base, p) })) {
		t.Fatal("red reset failed")
	}
	patch("green-effect", "green", .5)
	green := converge("green", response(1, 1, 25.5))
	if bytes.Equal(base, green) || bytes.Equal(red, green) {
		t.Fatal("separate named green patch not sensitive")
	}
	patch("green-effect", "green", 0)
	if !bytes.Equal(base, converge("reset-green", func(p []byte) bool { return bytes.Equal(base, p) })) {
		t.Fatal("green reset failed")
	}
	var same bool
	page.eval(t, `__postCanvas===document.querySelector('canvas')&&__postHandle===__postMount.__gosxScene3DHandle&&__postShaderSnapshot===JSON.stringify(__postShaders)&&__postProgramSnapshot===__postPrograms`, &same)
	if !same {
		t.Fatal("patch remounted scene")
	}
	if errs := page.PageErrors(); len(errs) > 0 {
		t.Fatalf("page errors=%v", errs)
	}
}
