// Package perffixture renders production pages for the registered type and
// compatibility coverage. It emits real runtime declarations, not byte totals.
package perffixture

import (
	"encoding/json"
	"strings"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/buildmanifest"
	runtimehost "m31labs.dev/gosx/client/runtime/host"
	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/island"
	"m31labs.dev/gosx/scene"
)

// Assets supplies actual app-owned program/media URLs from a production build.
// These private options are not a public performance record.
type Assets struct {
	EngineJSURL, EngineSharedURL, GoWASMURL, VideoURL string `json:"-"`
}

func fixtureError(pointer string) error {
	return &buildmanifest.PerfAssetError{Code: "invalid-input", Pointer: "/fixture" + pointer}
}

func assetURL(value string) bool {
	if !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") || strings.ContainsAny(value, "\\?#\x00\r\n") {
		return false
	}
	for _, part := range strings.Split(value[1:], "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

// Page creates a complete document for one type. The caller supplies a fresh
// renderer so mounts, IDs and manifests cannot leak between visits.
func Page(r *island.Renderer, shape string, assets Assets) ([]byte, error) {
	if r == nil {
		return nil, fixtureError("/renderer")
	}
	body := gosx.Text("Performance fixture")
	navigation := shape != "static"
	addIsland := func() gosx.Node {
		return r.RenderIsland("Counter", map[string]any{"Initial": 0}, gosx.Text("Count: 0"))
	}
	addScene := func(shared bool) (gosx.Node, error) {
		props := scene.Props{Width: 360, Height: 240, Background: "#18212b", Camera: scene.PerspectiveCamera{Position: scene.Vec3(0, 1, 4), FOV: 55, Near: .1, Far: 100}, Graph: scene.NewGraph(scene.Mesh{ID: "box", Geometry: scene.BoxGeometry{Width: 1, Height: 1, Depth: 1}, Material: scene.FlatMaterial{Color: "#8de1ff"}})}
		if shared {
			if !assetURL(assets.EngineSharedURL) {
				return gosx.Node{}, fixtureError("/assets/engineShared")
			}
			props.ProgramRef = assets.EngineSharedURL
		}
		return r.RenderEngine(props.EngineConfig(), gosx.Text("Scene loading")), nil
	}
	var err error
	switch shape {
	case "static", "enhanced":
	case "enhanced-lite", "lite-missing":
		r.EnableBootstrap()
	case "island", "selective-missing", "full-unconfigured":
		body = addIsland()
	case "compute-island":
		if _, err := r.RegisterComputeIsland(island.ComputeIslandConfig{Name: "Counter"}); err != nil {
			return nil, fixtureError("/compute")
		}
	case "engine-js", "engine-shared", "go-wasm":
		config := engine.Config{Name: "BudgetEngine", Kind: engine.KindSurface}
		switch shape {
		case "engine-js":
			if !assetURL(assets.EngineJSURL) {
				return nil, fixtureError("/assets/engineJS")
			}
		case "engine-shared":
			config.Runtime, config.WASMPath = engine.RuntimeShared, assets.EngineSharedURL
		case "go-wasm":
			config.Runtime, config.WASMPath = engine.RuntimeGoWASM, assets.GoWASMURL
		}
		if shape != "engine-js" && !assetURL(config.WASMPath) {
			return nil, fixtureError("/assets/engine")
		}
		body = r.RenderEngine(config, gosx.Text("Engine loading"))
	case "scene-js", "game-js":
		body, err = addScene(false)
	case "scene-shared", "game-shared":
		body, err = addScene(true)
	case "mixed":
		var mounted gosx.Node
		mounted, err = addScene(false)
		body = gosx.Fragment(addIsland(), mounted)
	case "video":
		if !assetURL(assets.VideoURL) {
			return nil, fixtureError("/assets/video")
		}
		props, _ := json.Marshal(struct {
			Src      string  `json:"src"`
			Muted    bool    `json:"muted"`
			Volume   float64 `json:"volume"`
			Autoplay bool    `json:"autoplay"`
		}{Src: assets.VideoURL, Muted: true})
		body = r.RenderEngine(engine.Config{Name: "GoSXVideo", Kind: engine.KindVideo, Props: props, Capabilities: []engine.Capability{engine.CapVideo}}, gosx.Text("Video ready"))
	case "preview":
		if r.Summary().BootstrapMode != "preview" {
			return nil, fixtureError("/preview")
		}
	default:
		return nil, fixtureError("/shape")
	}
	if err != nil {
		return nil, err
	}
	head := []gosx.Node{gosx.El("meta", gosx.Attrs(gosx.Attr("name", "viewport"), gosx.Attr("content", "width=device-width, initial-scale=1"))), r.PageHead()}
	if navigation {
		head = append(head, gosx.El("script", gosx.Attrs(gosx.Attr("src", runtimehost.NavigationRuntimePath), gosx.BoolAttr("defer"))))
	}
	if shape == "engine-js" {
		head = append(head, gosx.El("script", gosx.Attrs(gosx.Attr("src", assets.EngineJSURL), gosx.BoolAttr("defer"))))
	}
	document := gosx.El("html", gosx.Attrs(gosx.Attr("lang", "en")), gosx.El("head", gosx.Fragment(head...)), gosx.El("body", gosx.El("main", body)))
	return []byte("<!doctype html>" + gosx.RenderHTML(document)), nil
}
