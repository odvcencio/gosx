// Package web serves the game-stress example: the page, the generated
// figures, the Go WASM engine and the GoSX browser runtime. The browser test
// in e2e/game_stress_e2e_test.go and the example's main both use Handler.
package web

import (
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/examples/game-stress/stage"
	"m31labs.dev/gosx/examples/game-stress/stress"
	"m31labs.dev/gosx/game/audio/worklet"
	"m31labs.dev/gosx/island"
	"m31labs.dev/gosx/scene"
)

// Options configures the server. Each run's counts and modes come from the
// page URL (see Run), so one server serves every configuration.
type Options struct {
	// Root is the GoSX checkout; the browser runtime is read from Root/client/js.
	Root string
	// EngineWASM is the engine built with GOOS=js GOARCH=wasm from ../engine.
	EngineWASM string
	// WASMExec is the Go toolchain's wasm_exec.js.
	WASMExec string
	// CicadaDir holds fixture.json and kernel.wasm for the Cicada music (see
	// README.md). Empty disables the "cicada" audio mode.
	CicadaDir string
}

// Run is one configuration, parsed from the page's query string.
type Run struct {
	Players     int    // agents and skinned figures (22)
	Crowd       int    // crowd figures (40000)
	Sections    int    // crowd batches (8)
	Backend     string // webgpu or webgl
	Thread      string // main or worker
	Audio       string // off, worklet (a silent kernel) or cicada
	ReplayEvery int    // frames between replays (3)
	ReplayTicks int    // ticks per replay (6)
	Width       int    // canvas CSS pixels (1920)
	Height      int    // (1080)
	Cull        bool   // GPU frustum culling for crowds on WebGPU
	Shadows     bool   // a shadow-casting sun
}

// ParseRun reads a Run from query values, with the jam budget's defaults.
func ParseRun(q map[string][]string) Run {
	get := func(k string) string {
		if v := q[k]; len(v) > 0 {
			return v[0]
		}
		return ""
	}
	num := func(k string, def int) int {
		if n, err := strconv.Atoi(get(k)); err == nil && n >= 0 {
			return n
		}
		return def
	}
	r := Run{
		Players: num("players", 22), Crowd: num("crowd", 40000), Sections: num("sections", 8),
		Backend: get("backend"), Thread: get("thread"), Audio: get("audio"),
		ReplayEvery: num("replayEvery", 3), ReplayTicks: num("replayTicks", 6),
		Width: num("width", 1920), Height: num("height", 1080),
		Cull: get("cull") != "0", Shadows: get("shadows") == "1",
	}
	if r.Backend != "webgl" {
		r.Backend = "webgpu"
	}
	if r.Thread != "worker" {
		r.Thread = "main"
	}
	if r.Audio != "worklet" && r.Audio != "cicada" {
		r.Audio = "off"
	}
	r.Players = max(2, min(r.Players, 64))
	r.Crowd = min(r.Crowd, 60000)
	r.Sections = max(4, min(r.Sections, 16))
	return r
}

// Handler serves the example.
func Handler(opts Options) (http.Handler, error) {
	if opts.Root == "" {
		return nil, fmt.Errorf("web: Options.Root is empty")
	}
	wasmExec, err := os.ReadFile(opts.WASMExec)
	if err != nil {
		return nil, fmt.Errorf("web: read wasm_exec.js: %w", err)
	}
	wrapped := wrapWASMExec(wasmExec)
	runner, fan := RunnerGLB(), FanGLB()
	kernel := silentKernelWASM()
	mux := http.NewServeMux()
	var pageMu sync.Mutex
	pages := map[string][]byte{}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		key := r.URL.RawQuery
		pageMu.Lock()
		body, ok := pages[key]
		pageMu.Unlock()
		if !ok {
			body = []byte(Page(ParseRun(r.URL.Query()), opts.CicadaDir != ""))
			pageMu.Lock()
			pages[key] = body
			pageMu.Unlock()
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(body)
	})
	mux.HandleFunc("/gosx/", func(w http.ResponseWriter, r *http.Request) {
		name := filepath.Base(r.URL.Path)
		if name == "standard-go-wasm_exec.js" {
			w.Header().Set("Content-Type", "text/javascript")
			_, _ = w.Write(wrapped)
			return
		}
		path := filepath.Join(opts.Root, "client", "js", name)
		if _, err := os.Stat(path); err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		http.ServeFile(w, r, path)
	})
	mux.HandleFunc("/engines/stress.wasm", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/wasm")
		w.Header().Set("Cache-Control", "no-store")
		http.ServeFile(w, r, opts.EngineWASM)
	})
	glb := func(data []byte) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "model/gltf-binary")
			_, _ = w.Write(data)
		}
	}
	mux.HandleFunc("/assets/runner.glb", glb(runner))
	mux.HandleFunc("/assets/fan.glb", glb(fan))
	mux.HandleFunc("/audio/processor.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		_, _ = w.Write(worklet.Source())
	})
	mux.HandleFunc("/audio/silent.wasm", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/wasm")
		_, _ = w.Write(kernel)
	})
	if opts.CicadaDir != "" {
		mux.HandleFunc("/audio/cicada/", func(w http.ResponseWriter, r *http.Request) {
			name := filepath.Base(r.URL.Path)
			if name != "fixture.json" && name != "kernel.wasm" {
				http.NotFound(w, r)
				return
			}
			if name == "kernel.wasm" {
				w.Header().Set("Content-Type", "application/wasm")
			}
			http.ServeFile(w, r, filepath.Join(opts.CicadaDir, name))
		})
	}
	return mux, nil
}

// Scene declares the stress scene for a run.
func Scene(run Run) scene.Props {
	yes, no := true, false
	cfg := stage.EngineProps{Players: run.Players}.SimConfig()
	sim := stress.New(cfg)
	nodes := []scene.Node{
		scene.AmbientLight{ID: "sky", Color: "#c8d6ff", Intensity: 0.55},
		scene.DirectionalLight{ID: "sun", Color: "#fff4e0", Intensity: 1.4, Direction: scene.Vector3{X: -0.35, Y: -1, Z: -0.45},
			CastShadow: run.Shadows, ShadowSize: 2048},
		scene.Mesh{ID: "ground", Geometry: scene.BoxGeometry{Width: 2*cfg.HalfX + 8, Height: 0.2, Depth: 2*cfg.HalfZ + 8},
			Position: scene.Vector3{Y: -0.1}, Material: scene.StandardMaterial{Color: "#3f7a3a", Roughness: 0.95}},
		scene.Mesh{ID: "apron", Geometry: scene.BoxGeometry{Width: 2*cfg.HalfX + 30, Height: 0.1, Depth: 2*cfg.HalfZ + 30},
			Position: scene.Vector3{Y: -0.25}, Material: scene.StandardMaterial{Color: "#6b6b6b", Roughness: 1}},
	}
	for i, o := range sim.Obstacles() {
		nodes = append(nodes, scene.Mesh{ID: "obstacle-" + strconv.Itoa(i),
			Geometry: scene.BoxGeometry{Width: 2 * o.HalfX, Height: 2, Depth: 2 * o.HalfZ},
			Position: scene.Vector3{X: o.X, Y: 1, Z: o.Z}, Material: scene.StandardMaterial{Color: "#8a8f99", Roughness: 0.7}})
	}
	players := make([]scene.MeshInstance, run.Players)
	for i, a := range sim.Agents() {
		players[i] = scene.MeshInstance{ID: "p" + strconv.Itoa(i), Position: scene.Vector3{X: a.X, Z: a.Z},
			Rotation: scene.Euler{Y: a.Heading}, Scale: scene.Vector3{X: 1, Y: 1, Z: 1},
			Animation: stage.ClipIdle, AnimationLoop: true, Color: stage.TeamColors[a.Team]}
	}
	nodes = append(nodes, scene.InstancedGLBMesh{ID: stage.PlayersBatch, Src: "/assets/runner.glb",
		Material: scene.StandardMaterial{Color: "#ffffff", Roughness: 0.7}, Instances: players})
	for _, sec := range stage.Crowd(run.Crowd, run.Sections, cfg.HalfX, cfg.HalfZ) {
		nodes = append(nodes, scene.Mesh{ID: "stand-" + sec.ID, Geometry: scene.BoxGeometry{Width: sec.W, Height: sec.H, Depth: sec.D},
			Position: scene.Vector3{X: sec.CX, Y: sec.CY, Z: sec.CZ}, Rotation: scene.Euler{X: sec.Tilt, Y: sec.Yaw},
			Material: scene.StandardMaterial{Color: "#4a4f5c", Roughness: 0.9}})
		fans := make([]scene.MeshInstance, len(sec.Seats))
		for i, s := range sec.Seats {
			clip := stage.ClipSway
			if s.Cheer {
				clip = stage.ClipCheer
			}
			fans[i] = scene.MeshInstance{ID: stage.SeatID(i), Position: scene.Vector3{X: s.X, Y: s.Y, Z: s.Z},
				Rotation: scene.Euler{Y: s.Yaw}, Scale: scene.Vector3{X: 1, Y: 1, Z: 1},
				Animation: clip, AnimationTime: s.Phase, AnimationLoop: true, Color: stage.Palette[s.Tint]}
		}
		nodes = append(nodes, scene.InstancedGLBMesh{ID: sec.ID, Src: "/assets/fan.glb",
			Material: scene.StandardMaterial{Color: "#ffffff", Roughness: 0.8}, Instances: fans})
	}
	props := scene.Props{
		Width: run.Width, Height: run.Height, Responsive: &no, Background: "#0d1420", Controls: "none", AutoRotate: &no,
		MaxDevicePixelRatio: 1,
		Camera: scene.PerspectiveCamera{Position: scene.Vector3{X: 0, Y: 46, Z: 92}, Rotation: scene.Euler{X: -0.46},
			FOV: 48, Near: 0.5, Far: 600},
		Graph: scene.NewGraph(nodes...),
	}
	if run.Backend == "webgl" {
		props.ForceWebGL, props.RequireWebGL = &yes, &yes
	} else {
		props.PreferWebGPU = &yes
		if run.Cull {
			props.GPUDriven = &scene.GPUDriven{}
		}
	}
	return props
}

// Page renders the HTML for a run.
func Page(run Run, cicada bool) string {
	r := island.NewRenderer("stress")
	r.SetClientAssetPaths("/gosx/wasm_exec.js", "/gosx/patch.js", "/gosx/bootstrap.js")
	r.SetStandardGoWASMExecPath("/gosx/standard-go-wasm_exec.js")
	surface := Scene(run).EngineConfig()
	surface.MountID = stage.MountID
	if surface.MountAttrs == nil {
		surface.MountAttrs = map[string]any{}
	}
	surface.MountAttrs["style"] = fmt.Sprintf("width:%dpx;height:%dpx", run.Width, run.Height)
	props, _ := json.Marshal(stage.EngineProps{Target: stage.MountID, Players: run.Players, Crowd: run.Crowd, Sections: run.Sections,
		ReplayEvery: run.ReplayEvery, ReplayTicks: run.ReplayTicks, FrameHz: 60})
	eng := engine.Config{Name: "GameStress", Kind: engine.KindWorker, Runtime: engine.RuntimeGoWASM, MountID: "stress",
		WASMPath: "/engines/stress.wasm", Props: props, Capabilities: []engine.Capability{engine.CapKeyboard}}
	if run.Thread == "worker" {
		eng.Thread = engine.ThreadWorker
		eng.Worker = engine.WorkerRequired
	}
	audio := run.Audio
	if audio == "cicada" && !cicada {
		audio = "off"
	}
	runJSON, _ := json.Marshal(map[string]any{"run": run, "audio": audio, "crowdTriangles": fanRig().triangles(),
		"figureTriangles": runnerRig().triangles()})
	// Render the engines first: the renderer collects them into the
	// manifest that PageHead writes.
	body := gosx.RenderHTML(r.RenderEngine(surface, gosx.Node{})) + gosx.RenderHTML(r.RenderEngine(eng, gosx.Node{}))
	var b strings.Builder
	b.WriteString("<!doctype html><html><head><meta charset=\"utf-8\"><title>GoSX game stress</title>")
	b.WriteString("<style>html,body{margin:0;background:#000;overflow:hidden}</style>")
	b.WriteString("<script>window.__stressRun=" + string(runJSON) + ";</script>")
	b.WriteString("<script>" + harnessJS + "</script>")
	b.WriteString(gosx.RenderHTML(r.PageHead()))
	b.WriteString("</head><body>")
	b.WriteString(body)
	b.WriteString("<script>" + audioJS + "</script>")
	b.WriteString("<noscript>" + html.EscapeString("This page needs JavaScript and WebGPU or WebGL2.") + "</noscript>")
	b.WriteString("</body></html>")
	return b.String()
}

// wrapWASMExec applies the isolation wrapper `gosx build` puts around the
// toolchain's wasm_exec.js (cmd/gosx wrapStandardGoWASMExec).
func wrapWASMExec(src []byte) []byte {
	return []byte("(function(global){\n\"use strict\";\nvar hadGo = Object.prototype.hasOwnProperty.call(global, \"Go\");\nvar previousGo = global.Go;\nvar standardGo;\ntry {\n" +
		string(src) +
		"\nstandardGo = global.Go;\n} finally {\nif (hadGo) global.Go = previousGo; else delete global.Go;\n}\nif (typeof standardGo !== \"function\") throw new Error(\"standard-Go wasm_exec.js did not publish Go\");\nObject.defineProperty(global, \"__gosx_standard_go_wasm_ctor\", {value: standardGo, writable: false, configurable: true});\n})(globalThis);\n")
}
