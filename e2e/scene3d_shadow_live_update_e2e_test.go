//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
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

// The two Selena materials a staging tool puts in such a scene: a self-lit
// clay for the figures, and a transparent sheet that draws marks on the floor.
const shadowStageShaders = `material StageClay {
    param baseColor : color = rgb(0.69, 0.33, 0.18)

    context {
        cameraPos : vec3
        sunDir : vec3
    }

    surface(geo) -> color {
        let n = normalize(geo.worldNormal)
        let wrap = clamp((dot(n, sunDir) + 0.4) / 1.4, 0.0, 1.0)
        return baseColor * (0.45 + 0.55 * wrap)
    }
}

material StageMarks {
    param lineColor : color = rgb(0.43, 0.41, 0.36)

    context {
        cameraPos : vec3
    }

    surface(geo) -> vec4 {
        let p = geo.worldPos.xz
        let cell = abs(fract(p - vec2f(0.5, 0.5)) - vec2f(0.5, 0.5))
        let grid = 1.0 - step(0.02, min(cell.x, cell.y))
        return vec4f(lineColor, grid * 0.4)
    }
}`

// stageFeatures turns on the things a real staging scene has and the plain
// stage lacks. Tried one at a time, only the overlay broke the shadows: it is
// the one blended draw, and a blended draw ends the frame with depth writes
// off.
type stageFeatures struct {
	selena        bool // the casters use a Selena material
	overlay       bool // a transparent Selena sheet lies over the floor
	parts         bool // each caster is a figure of rotated parts
	hemisphere    bool // a hemisphere fill light and a sky environment
	soft          bool // a 4096 map with soft edges
	ring          bool // a flat emissive ring under each caster
	cameraCommand bool // each live update also sets the camera
}

// stagePost is one caster: where it stands and how far its arm is raised.
type stagePost struct{ X, Z, Arm float64 }

// shadowStage is a floor seen from straight above, casters that shadow it, and
// one directional light with one shadow map. It is the smallest scene in which
// a shadow can be compared before and after a live update.
func shadowStage(t *testing.T, posts []stagePost, light scene.Vector3, features stageFeatures) scene.Props {
	t.Helper()
	yes, no := true, false
	key := scene.DirectionalLight{ID: "key", Color: "#ffffff", Intensity: 3, Direction: light, CastShadow: true, ShadowSize: 2048, ShadowBias: 0.0012}
	if features.soft {
		key.ShadowSize, key.ShadowSoftness = 4096, 0.05
	}
	nodes := []scene.Node{
		key,
		scene.Mesh{ID: "floor", Geometry: scene.PlaneGeometry{Width: 40, Height: 40}, Material: scene.StandardMaterial{Color: "#d8d4c8", Roughness: 1}, ReceiveShadow: true},
	}
	environment := scene.Environment{AmbientColor: "#ffffff", AmbientIntensity: 0.35, ToneMapping: "linear"}
	if features.hemisphere {
		nodes = append(nodes, scene.HemisphereLight{ID: "fill", SkyColor: "#f6f3ec", GroundColor: "#b3ab9b", Intensity: 0.22})
		environment = scene.Environment{AmbientColor: "#ffffff", AmbientIntensity: 0.1, SkyColor: "#f6f3ec", SkyIntensity: 0.08, GroundColor: "#b3ab9b", GroundIntensity: 0.04, ToneMapping: "linear"}
	}
	var body scene.Material = scene.StandardMaterial{Color: "#b0552f", Roughness: 0.8}
	if features.selena {
		clay, _, err := scene.CompileSelenaMaterial([]byte(shadowStageShaders), scene.SelenaMaterialOptions{Material: "StageClay", Standard: scene.StandardMaterial{Color: "#b0552f", Roughness: 1}})
		if err != nil {
			t.Fatal(err)
		}
		body = clay
	}
	if features.overlay {
		hidden := 0.0
		marks, _, err := scene.CompileSelenaMaterial([]byte(shadowStageShaders), scene.SelenaMaterialOptions{Material: "StageMarks", Standard: scene.StandardMaterial{Color: "#000000", Opacity: &hidden, BlendMode: scene.BlendAlpha}})
		if err != nil {
			t.Fatal(err)
		}
		nodes = append(nodes, scene.Mesh{ID: "marks", Geometry: scene.PlaneGeometry{Width: 40, Height: 40}, Material: marks, Position: scene.Vec3(0, 0.004, 0)})
	}
	for i, post := range posts {
		id := fmt.Sprintf("post-%d", i)
		nodes = append(nodes, scene.Mesh{
			ID: id, Geometry: scene.CylinderGeometry{RadiusTop: 0.2, RadiusBottom: 0.2, Height: 1.6, Segments: 12},
			Position: scene.Vec3(post.X, 0.8, post.Z), Material: body, CastShadow: true,
		})
		if features.parts {
			// An arm out to the side, raised by post.Arm radians, and a head.
			nodes = append(nodes,
				scene.Mesh{ID: id + "-arm", Geometry: scene.CylinderGeometry{RadiusTop: 0.07, RadiusBottom: 0.07, Height: 0.9, Segments: 12},
					Position: scene.Vec3(post.X+0.45*math.Cos(post.Arm), 1.3+0.45*math.Sin(post.Arm), post.Z), Rotation: scene.Euler{Z: post.Arm - math.Pi/2},
					Material: body, CastShadow: true},
				scene.Mesh{ID: id + "-head", Geometry: scene.SphereGeometry{Radius: 0.22, Segments: 16}, Position: scene.Vec3(post.X, 1.8, post.Z), Material: body, CastShadow: true})
		}
		if features.ring {
			nodes = append(nodes, scene.Mesh{ID: id + "-ring", Position: scene.Vec3(post.X, 0.008, post.Z),
				Geometry: scene.RingGeometry(0.36, 0.4, 40, 1, 0, 2*math.Pi), Material: scene.StandardMaterial{Color: "#b0552f", Roughness: 1, Emissive: 0.5}})
		}
	}
	// Z is 0.001, not 0: the camera normalizer treats z = 0 as unset and moves
	// the camera to z = 6.
	return scene.Props{
		Width: 480, Height: 300, Background: "#f0ede6", AutoRotate: &no, Responsive: &no, DragToRotate: &no, AdaptiveQuality: &no,
		MaxDevicePixelRatio: 1, ForceWebGL: &yes, RequireWebGL: &yes,
		Environment:        environment,
		Camera:             scene.PerspectiveCamera{Position: scene.Vec3(0, 15, 0.001), Rotation: scene.Euler{X: -math.Pi / 2}, FOV: 50, Near: 0.1, Far: 100},
		OrthographicCamera: &scene.OrthographicCamera{Position: scene.Vec3(0, 15, 0.001), Rotation: scene.Euler{X: -math.Pi / 2}, Left: -8, Right: 8, Top: 5, Bottom: -5, Near: 0.1, Far: 100},
		Graph:              scene.NewGraph(nodes...),
	}
}

// shadowStagePage mounts props on the production lazy-chunk WebGL page and
// returns the page once the scene has drawn.
func shadowStagePage(t *testing.T, props scene.Props) *browserPage {
	t.Helper()
	root := e2eRepoRoot(t)
	payload, err := json.Marshal(props)
	if err != nil {
		t.Fatal(err)
	}
	page := []byte(fmt.Sprintf(`<!doctype html><html><head><meta charset="utf-8"><style>html,body{margin:0;background:#000}#stage{width:%dpx;height:%dpx}</style></head><body><div id="stage"></div>
<script src="/gosx/bootstrap-runtime.js"></script>
<script data-gosx-script="feature-scene3d" data-gosx-scene3d-webgl-url="/gosx/bootstrap-feature-scene3d-webgl.js" src="/gosx/bootstrap-feature-scene3d.js"></script>
<script>window.__gosx_engine_factories.GoSXScene3D({mount:document.getElementById('stage'),emit(){},props:%s}).then(handle=>{window.stageHandle=handle;}).catch(error=>{window.mountError=String(error);});</script></body></html>`, int(props.Width), int(props.Height), payload))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write(page)
		case strings.HasPrefix(r.URL.Path, "/gosx/bootstrap-") && strings.HasSuffix(r.URL.Path, ".js"):
			http.ServeFile(w, r, filepath.Join(root, "client", "js", filepath.Base(r.URL.Path)))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	url := server.URL
	if e2eWindowsRelay() {
		url = e2eExposeToWindows(t, server.URL)
	}
	browser := newBrowserPage(t, e2eChromePath(t), map[string]any{"use-angle": "swiftshader", "enable-unsafe-swiftshader": true, "disable-features": "WebGPU"},
		int(props.Width)+80, int(props.Height)+80, `window.__gosx_scene3d_render_truth=true`, 150*time.Second)
	browser.navigate(t, url+"/")
	browser.waitFor(t, `!!window.mountError || document.getElementById('stage')?.getAttribute('data-gosx-scene3d-render-mesh-drawn') > 0`, 60*time.Second, "the stage to draw")
	var mountError string
	browser.eval(t, `window.mountError||''`, &mountError)
	if mountError != "" {
		t.Fatalf("mount failed: %s\n%s", mountError, browser.Console())
	}
	return browser
}

// stableStageShot returns the canvas once three screenshots in a row match.
func stableStageShot(t *testing.T, browser *browserPage) image.Image {
	t.Helper()
	var last []byte
	same := 0
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		var shot []byte
		if err := chromedp.Run(browser.ctx, chromedp.Screenshot("#stage canvas", &shot, chromedp.ByQuery)); err != nil {
			t.Fatal(err)
		}
		if last != nil && bytes.Equal(last, shot) {
			same++
		} else {
			same = 0
		}
		last = shot
		if same >= 2 {
			img, err := png.Decode(bytes.NewReader(shot))
			if err != nil {
				t.Fatal(err)
			}
			return img
		}
	}
	t.Fatalf("the stage never settled; console=%s", browser.Console())
	return nil
}

// applyStageDiff sends the commands that turn one stage into the next through
// the mount's command event, the way a page updates a live scene.
func applyStageDiff(t *testing.T, browser *browserPage, revision int, previous, next scene.Props, options scene.DiffOptions, cameraCommand bool) {
	t.Helper()
	list := scene.DiffScene(previous.SceneIR(), next.SceneIR(), options).Commands
	if cameraCommand {
		plan := next.OrthographicCamera
		list = append(list, scene.Command{Kind: scene.CommandSetCamera, Data: map[string]any{
			"kind": "orthographic", "x": plan.Position.X, "y": plan.Position.Y, "z": plan.Position.Z,
			"rotationX": plan.Rotation.X, "rotationY": plan.Rotation.Y, "rotationZ": plan.Rotation.Z,
			"left": plan.Left, "right": plan.Right, "top": plan.Top, "bottom": plan.Bottom, "zoom": 1, "near": plan.Near, "far": plan.Far}})
	}
	commands, err := json.Marshal(list)
	if err != nil {
		t.Fatal(err)
	}
	browser.eval(t, fmt.Sprintf(`(() => {const m=document.getElementById('stage');window.stageApplied=0;m.addEventListener('gosx:scene3d:commands-applied',e=>{window.stageApplied=e.detail.revision;});m.dispatchEvent(new CustomEvent('gosx:scene3d:commands',{detail:{revision:%d,commands:%s}}));return true})()`, revision, commands), nil)
	browser.waitFor(t, fmt.Sprintf(`window.stageApplied === %d`, revision), 20*time.Second, "the commands to apply")
}

// differingPixels counts the pixels of two same-sized images whose largest
// channel difference is above tolerance (0..255).
func differingPixels(a, b image.Image, tolerance int) int {
	bounds := a.Bounds()
	if bounds.Dx() != b.Bounds().Dx() || bounds.Dy() != b.Bounds().Dy() {
		return bounds.Dx() * bounds.Dy()
	}
	count := 0
	for y := 0; y < bounds.Dy(); y++ {
		for x := 0; x < bounds.Dx(); x++ {
			p, q := pixel8(a, bounds.Min.X+x, bounds.Min.Y+y), pixel8(b, b.Bounds().Min.X+x, b.Bounds().Min.Y+y)
			for c := 0; c < 3; c++ {
				if d := p[c] - q[c]; d > tolerance || d < -tolerance {
					count++
					break
				}
			}
		}
	}
	return count
}

func saveStageShot(t *testing.T, name string, img image.Image) {
	t.Helper()
	out := os.Getenv("GOSX_SHADOW_LIVE_OUT")
	if out == "" {
		return
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		t.Fatal(err)
	}
	_ = os.MkdirAll(out, 0o755)
	if err := os.WriteFile(filepath.Join(out, name+".png"), buffer.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestScene3DShadowsMatchAfterLiveUpdate draws a scene two ways and expects
// one picture: mounted fresh, and reached from an earlier scene through live
// commands. A shadow map that is not redrawn, not cleared, or redrawn for a
// different fit than the one the floor samples, shows as a difference.
//
// Stage B moves both casters, raises their arms and adds a third. Stage C
// keeps B's casters and turns the light to the mirrored side.
//
// The overlay and staging cases failed until the shadow pass restored the
// depth write mask before its clear: the map kept the casters of every earlier
// frame, so the floor showed shadows where the casters used to stand (1,583
// wrong pixels on stage B and 2,790 on stage C in the overlay case).
func TestScene3DShadowsMatchAfterLiveUpdate(t *testing.T) {
	postsA := []stagePost{{X: -1, Z: 0}, {X: 1, Z: -0.5}}
	postsB := []stagePost{{X: -2, Z: 1.5, Arm: 0.9}, {X: 0, Z: 1, Arm: -0.6}, {X: 1, Z: -0.5}}
	replace, patch := scene.DiffOptions{}, scene.DiffOptions{PatchTransforms: true}
	for _, c := range []struct {
		name     string
		options  scene.DiffOptions
		features stageFeatures
	}{
		{"plain-replace", replace, stageFeatures{}},
		{"plain-patch", patch, stageFeatures{}},
		{"overlay", patch, stageFeatures{overlay: true}},
		{"staging", patch, stageFeatures{selena: true, overlay: true, parts: true, hemisphere: true, soft: true, ring: true, cameraCommand: true}},
	} {
		t.Run(c.name, func(t *testing.T) {
			stageA := shadowStage(t, postsA, scene.Vec3(0.6, -1, -0.5), c.features)
			stageB := shadowStage(t, postsB, scene.Vec3(0.6, -1, -0.5), c.features)
			stageC := shadowStage(t, postsB, scene.Vec3(-0.6, -1, -0.5), c.features)

			live := shadowStagePage(t, stageA)
			saveStageShot(t, c.name+"-a-fresh", stableStageShot(t, live))
			applyStageDiff(t, live, 1, stageA, stageB, c.options, c.features.cameraCommand)
			liveB := stableStageShot(t, live)
			applyStageDiff(t, live, 2, stageB, stageC, c.options, c.features.cameraCommand)
			liveC := stableStageShot(t, live)

			freshB := stableStageShot(t, shadowStagePage(t, stageB))
			freshC := stableStageShot(t, shadowStagePage(t, stageC))
			saveStageShot(t, c.name+"-b-live", liveB)
			saveStageShot(t, c.name+"-b-fresh", freshB)
			saveStageShot(t, c.name+"-c-live", liveC)
			saveStageShot(t, c.name+"-c-fresh", freshC)

			// A changed pixel count this small is anti-aliasing noise on a
			// 480 by 300 canvas; a displaced or missing shadow is hundreds.
			const allowed = 40
			if n := differingPixels(liveB, freshB, 24); n > allowed {
				t.Errorf("stage B after a live update differs from a fresh mount in %d pixels (allowed %d)", n, allowed)
			}
			if n := differingPixels(liveC, freshC, 24); n > allowed {
				t.Errorf("stage C after a live light change differs from a fresh mount in %d pixels (allowed %d)", n, allowed)
			}
			if errors := live.PageErrors(); len(errors) != 0 {
				t.Errorf("page errors: %v", errors)
			}
		})
	}
}
