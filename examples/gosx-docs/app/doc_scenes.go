package docs

import (
	"fmt"
	"strings"

	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/scene"
)

const (
	docSceneBindingKey = "docScene"
	docSceneMaxNodes   = 12
	docSceneCanvas     = "#000000"
	docSceneText       = "#f5f5ef"
	docSceneSecondary  = "#c7c7c2"
	docSceneAccent     = "#d4af37"
	docSceneAccentLine = "#8b7425"
)

// DocSceneFeature is the typed teaching contract shared by selected docs
// chapters. The route owns the literal Scene3D mount so capability analysis
// remains route-local; this value supplies the deterministic scene and its
// concise explanatory overlay.
type DocSceneFeature struct {
	Route           string
	Slug            string
	SurfaceID       string
	HeadingID       string
	Eyebrow         string
	Title           string
	Summary         string
	BackendTruth    string
	InteractionHint string
	DemoHref        string
	DemoLabel       string
	Scene           scene.Props
}

type docSceneShape uint8

const (
	docSceneBox docSceneShape = iota
	docSceneSphere
	docScenePyramid
	docSceneTorus
	docSceneCylinder
)

type docSceneAnchor struct {
	ID       string
	Position scene.Vector3
	Shape    docSceneShape
	Accent   bool
	Spin     scene.Euler
}

type docSceneSpec struct {
	Route           string
	Slug            string
	Eyebrow         string
	Title           string
	Summary         string
	InteractionHint string
	DemoHref        string
	DemoLabel       string
	Anchors         []docSceneAnchor
	Links           [][2]int
}

var docSceneSpecs = []docSceneSpec{
	{
		Route:           "/docs/getting-started",
		Slug:            "getting-started",
		Eyebrow:         "Request to first render",
		Title:           "A GoSX project becomes a running page in four explicit steps.",
		Summary:         "The diagram moves from source, through compile and route discovery, to server-rendered HTML.",
		InteractionHint: "Pointer interaction only: drag to orbit the build path; wheel or pinch to zoom.",
		DemoHref:        "/demos/playground",
		DemoLabel:       "Open the compiler playground",
		Anchors: []docSceneAnchor{
			{ID: "source", Position: scene.Vec3(-3, 0.8, 0), Shape: docSceneBox, Accent: true},
			{ID: "compile", Position: scene.Vec3(-1, -0.4, 0.5), Shape: docSceneCylinder},
			{ID: "route", Position: scene.Vec3(1, 0.5, -0.2), Shape: docScenePyramid},
			{ID: "html", Position: scene.Vec3(3, -0.3, 0.4), Shape: docSceneSphere, Accent: true},
		},
		Links: [][2]int{{0, 1}, {1, 2}, {2, 3}},
	},
	{
		Route:           "/docs/compiler",
		Slug:            "compiler",
		Eyebrow:         "GSX lowering pipeline",
		Title:           "One typed pipeline keeps source, IR, validation, and output coherent.",
		Summary:         "Each node is a compiler boundary; the connecting spine is the data contract passed to the next phase.",
		InteractionHint: "Pointer interaction only: drag to orbit the compiler stages; wheel or pinch to zoom.",
		DemoHref:        "/demos/playground",
		DemoLabel:       "Compile GSX interactively",
		Anchors: []docSceneAnchor{
			{ID: "gsx", Position: scene.Vec3(-3, 0, 0), Shape: docSceneBox, Accent: true},
			{ID: "cst", Position: scene.Vec3(-1, 0.8, 0.5), Shape: docSceneTorus},
			{ID: "ir", Position: scene.Vec3(1, -0.5, -0.2), Shape: docScenePyramid, Accent: true},
			{ID: "validate", Position: scene.Vec3(3, 0.3, 0.4), Shape: docSceneSphere},
		},
		Links: [][2]int{{0, 1}, {1, 2}, {2, 3}},
	},
	{
		Route:           "/docs/routing",
		Slug:            "routing",
		Eyebrow:         "File tree to route tree",
		Title:           "Layouts branch around pages while loaders feed the selected leaf.",
		Summary:         "The central route fans into static, dynamic, and nested leaves without hiding their shared layout.",
		InteractionHint: "Pointer interaction only: drag to separate the route branches in depth; wheel or pinch to zoom.",
		DemoHref:        "/demos/playground",
		DemoLabel:       "Explore route-aware compilation",
		Anchors: []docSceneAnchor{
			{ID: "root", Position: scene.Vec3(-2.8, 0, 0), Shape: docSceneBox, Accent: true},
			{ID: "layout", Position: scene.Vec3(-0.8, 0, 0), Shape: docSceneTorus},
			{ID: "static", Position: scene.Vec3(1.5, 1.2, 0.5), Shape: docSceneSphere},
			{ID: "dynamic", Position: scene.Vec3(1.5, 0, -0.5), Shape: docScenePyramid, Accent: true},
			{ID: "nested", Position: scene.Vec3(1.5, -1.2, 0.4), Shape: docSceneCylinder},
		},
		Links: [][2]int{{0, 1}, {1, 2}, {1, 3}, {1, 4}},
	},
	{
		Route:           "/docs/runtime",
		Slug:            "runtime",
		Eyebrow:         "Explicit runtime upgrades",
		Title:           "Server HTML stays central while opt-in runtimes attach around it.",
		Summary:         "Navigation, islands, hubs, and engines are separate upgrades rather than one mandatory client bundle.",
		InteractionHint: "Pointer interaction only: drag around the server node; wheel or pinch to zoom.",
		DemoHref:        "/demos/scene3d-bench",
		DemoLabel:       "Inspect live renderer telemetry",
		Anchors: []docSceneAnchor{
			{ID: "server", Position: scene.Vec3(0, 0, 0), Shape: docSceneSphere, Accent: true},
			{ID: "navigation", Position: scene.Vec3(-2.5, 1.2, 0.4), Shape: docSceneBox},
			{ID: "island", Position: scene.Vec3(2.5, 1.2, -0.4), Shape: docSceneTorus},
			{ID: "hub", Position: scene.Vec3(-2.5, -1.2, -0.4), Shape: docScenePyramid},
			{ID: "engine", Position: scene.Vec3(2.5, -1.2, 0.4), Shape: docSceneCylinder},
		},
		Links: [][2]int{{0, 1}, {0, 2}, {0, 3}, {0, 4}},
	},
	{
		Route:           "/docs/streaming",
		Slug:            "streaming",
		Eyebrow:         "Progressive response",
		Title:           "The shell arrives first; deferred regions resolve behind it.",
		Summary:         "A bounded sequence shows the initial flush followed by independently completed content regions.",
		InteractionHint: "Pointer interaction only: drag around the response path; wheel or pinch to zoom.",
		DemoHref:        "/demos/playground",
		DemoLabel:       "Open the server-rendering playground",
		Anchors: []docSceneAnchor{
			{ID: "request", Position: scene.Vec3(-3, 0, 0), Shape: docSceneSphere},
			{ID: "shell", Position: scene.Vec3(-1.5, 0.8, 0.4), Shape: docSceneBox, Accent: true},
			{ID: "defer-a", Position: scene.Vec3(0, -0.8, -0.4), Shape: docScenePyramid},
			{ID: "defer-b", Position: scene.Vec3(1.5, 0.8, 0.4), Shape: docSceneCylinder},
			{ID: "complete", Position: scene.Vec3(3, 0, 0), Shape: docSceneTorus, Accent: true},
		},
		Links: [][2]int{{0, 1}, {1, 2}, {2, 3}, {3, 4}},
	},
	{
		Route:           "/docs/islands",
		Slug:            "islands",
		Eyebrow:         "Selective hydration",
		Title:           "Static HTML surrounds small, explicit interactive islands.",
		Summary:         "The outer server surface remains whole while three isolated regions opt into the shared expression VM.",
		InteractionHint: "Pointer interaction only: drag around the shell; wheel or pinch to zoom.",
		DemoHref:        "/demos/playground",
		DemoLabel:       "Try an interactive GSX component",
		Anchors: []docSceneAnchor{
			{ID: "document", Position: scene.Vec3(0, 0, -0.8), Shape: docSceneBox},
			{ID: "island-a", Position: scene.Vec3(-2.2, 1, 0.5), Shape: docSceneSphere, Accent: true},
			{ID: "island-b", Position: scene.Vec3(0, -1.2, 0.7), Shape: docSceneTorus, Accent: true},
			{ID: "island-c", Position: scene.Vec3(2.2, 1, 0.5), Shape: docScenePyramid, Accent: true},
			{ID: "vm", Position: scene.Vec3(0, 1.5, -0.2), Shape: docSceneCylinder},
		},
		Links: [][2]int{{0, 1}, {0, 2}, {0, 3}, {4, 1}},
	},
	{
		Route:           "/docs/signals",
		Slug:            "signals",
		Eyebrow:         "Dependency graph",
		Title:           "One source signal updates only the values that depend on it.",
		Summary:         "The graph distinguishes direct subscribers, a computed value, and the effect reached through that computation.",
		InteractionHint: "Pointer interaction only: drag around the dependency graph; wheel or pinch to zoom.",
		DemoHref:        "/demos/checkers",
		DemoLabel:       "See signals drive a live game surface",
		Anchors: []docSceneAnchor{
			{ID: "source", Position: scene.Vec3(-2.8, 0, 0), Shape: docSceneSphere, Accent: true},
			{ID: "subscriber-a", Position: scene.Vec3(-0.5, 1.2, 0.5), Shape: docSceneBox},
			{ID: "subscriber-b", Position: scene.Vec3(-0.5, -1.2, -0.5), Shape: docSceneBox},
			{ID: "computed", Position: scene.Vec3(1.5, 0.7, -0.2), Shape: docSceneTorus, Accent: true},
			{ID: "effect", Position: scene.Vec3(3, -0.5, 0.4), Shape: docScenePyramid},
		},
		Links: [][2]int{{0, 1}, {0, 2}, {0, 3}, {1, 3}, {3, 4}},
	},
	{
		Route:           "/docs/hubs",
		Slug:            "hubs",
		Eyebrow:         "Realtime room",
		Title:           "A hub coordinates fanout while clients retain local state.",
		Summary:         "Two clients publish through one room coordinator, which fans a coherent revision to every participant.",
		InteractionHint: "Pointer interaction only: drag around the room; wheel or pinch to zoom.",
		DemoHref:        "/demos/collab",
		DemoLabel:       "Open the realtime collaboration demo",
		Anchors: []docSceneAnchor{
			{ID: "client-a", Position: scene.Vec3(-2.8, 1, 0.4), Shape: docSceneBox},
			{ID: "client-b", Position: scene.Vec3(-2.8, -1, -0.4), Shape: docSceneBox},
			{ID: "room", Position: scene.Vec3(0, 0, 0), Shape: docSceneTorus, Accent: true},
			{ID: "revision", Position: scene.Vec3(2, 0.8, 0.4), Shape: docSceneSphere},
			{ID: "fanout", Position: scene.Vec3(3, -0.8, -0.4), Shape: docScenePyramid, Accent: true},
		},
		Links: [][2]int{{0, 2}, {1, 2}, {2, 3}, {2, 4}},
	},
	{
		Route:           "/docs/engines",
		Slug:            "engines",
		Eyebrow:         "Managed compute surfaces",
		Title:           "One engine contract selects the browser capability it needs.",
		Summary:         "A typed engine descriptor branches into canvas, GPU, and worker surfaces without giving them DOM ownership.",
		InteractionHint: "Pointer interaction only: drag around the capability fanout; wheel or pinch to zoom.",
		DemoHref:        "/demos/scene3d",
		DemoLabel:       "Open the Scene3D engine showcase",
		Anchors: []docSceneAnchor{
			{ID: "descriptor", Position: scene.Vec3(-2.5, 0, 0), Shape: docSceneCylinder, Accent: true},
			{ID: "canvas", Position: scene.Vec3(1, 1.3, 0.5), Shape: docSceneBox},
			{ID: "gpu", Position: scene.Vec3(1, 0, -0.5), Shape: docSceneTorus, Accent: true},
			{ID: "worker", Position: scene.Vec3(1, -1.3, 0.5), Shape: docScenePyramid},
		},
		Links: [][2]int{{0, 1}, {0, 2}, {0, 3}},
	},
	{
		Route:           "/docs/motion",
		Slug:            "motion",
		Eyebrow:         "Motion with an off switch",
		Title:           "Authored motion explains change and yields to reduced-motion preference.",
		Summary:         "The keyframe path advances through a short sequence; the final node rotates only when motion is allowed.",
		InteractionHint: "Pointer interaction only: drag to inspect the path; wheel or pinch to zoom. Reduced motion freezes authored animation.",
		DemoHref:        "/demos/scene3d",
		DemoLabel:       "See declarative motion in Scene3D",
		Anchors: []docSceneAnchor{
			{ID: "start", Position: scene.Vec3(-3, -0.8, 0), Shape: docSceneSphere},
			{ID: "key-a", Position: scene.Vec3(-1.5, 0.7, 0.4), Shape: docSceneBox},
			{ID: "key-b", Position: scene.Vec3(0, -0.2, -0.4), Shape: docScenePyramid},
			{ID: "key-c", Position: scene.Vec3(1.5, 1, 0.4), Shape: docSceneCylinder},
			{ID: "finish", Position: scene.Vec3(3, 0, 0), Shape: docSceneTorus, Accent: true, Spin: scene.Rotate(0, 0.003, 0)},
		},
		Links: [][2]int{{0, 1}, {1, 2}, {2, 3}, {3, 4}},
	},
	{
		Route:           "/docs/deployment",
		Slug:            "deployment",
		Eyebrow:         "One source, three targets",
		Title:           "The same route tree can ship as SSR, static output, or an edge bundle.",
		Summary:         "Build selection happens after the application graph, so deployment does not fork the product architecture.",
		InteractionHint: "Pointer interaction only: drag around the build fanout; wheel or pinch to zoom.",
		DemoHref:        "/demos",
		DemoLabel:       "Explore the deployed demo gallery",
		Anchors: []docSceneAnchor{
			{ID: "source", Position: scene.Vec3(-2.8, 0, 0), Shape: docSceneBox, Accent: true},
			{ID: "build", Position: scene.Vec3(-0.8, 0, 0), Shape: docSceneCylinder},
			{ID: "ssr", Position: scene.Vec3(1.7, 1.3, 0.5), Shape: docSceneSphere},
			{ID: "static", Position: scene.Vec3(1.7, 0, -0.5), Shape: docScenePyramid},
			{ID: "edge", Position: scene.Vec3(1.7, -1.3, 0.5), Shape: docSceneTorus, Accent: true},
		},
		Links: [][2]int{{0, 1}, {1, 2}, {1, 3}, {1, 4}},
	},
}

// DocSceneFeatureForRoute returns a fresh deterministic feature for a selected
// conceptual docs route. Other routes deliberately return false so they stay
// free of Scene3D capabilities and runtime payload.
func DocSceneFeatureForRoute(routePath string) (DocSceneFeature, bool) {
	routePath = normalizeDocSceneRoute(routePath)
	for _, spec := range docSceneSpecs {
		if spec.Route == routePath {
			return buildDocSceneFeature(spec), true
		}
	}
	return DocSceneFeature{}, false
}

func normalizeDocSceneRoute(routePath string) string {
	routePath = strings.TrimSpace(routePath)
	if index := strings.IndexAny(routePath, "?#"); index >= 0 {
		routePath = routePath[:index]
	}
	if routePath != "/" {
		routePath = strings.TrimRight(routePath, "/")
	}
	return routePath
}

func buildDocSceneFeature(spec docSceneSpec) DocSceneFeature {
	prefix := "doc-" + spec.Slug
	nodes := make([]scene.Node, 0, 2+len(spec.Anchors)+len(spec.Links))
	nodes = append(nodes,
		scene.AmbientLight{
			ID:        prefix + "-light-ambient",
			Color:     docSceneText,
			Intensity: 0.7,
		},
		scene.DirectionalLight{
			ID:        prefix + "-light-key",
			Color:     docSceneAccent,
			Intensity: 1.1,
			Direction: scene.Vec3(-0.4, -0.8, -0.6),
		},
	)
	for index, link := range spec.Links {
		if link[0] < 0 || link[0] >= len(spec.Anchors) || link[1] < 0 || link[1] >= len(spec.Anchors) {
			continue
		}
		from := spec.Anchors[link[0]].Position
		to := spec.Anchors[link[1]].Position
		nodes = append(nodes, scene.Mesh{
			ID: prefix + "-link-" + fmt.Sprintf("%02d", index+1),
			Geometry: scene.LinesGeometry{
				Points:   []scene.Vector3{from, to},
				Segments: [][2]int{{0, 1}},
				Width:    1.5,
			},
			Material: scene.LineBasicMaterial{
				MaterialStyle: scene.MaterialStyle{
					Color: docSceneAccentLine,
				},
				Width: 1.5,
			},
		})
	}
	for _, anchor := range spec.Anchors {
		nodes = append(nodes, scene.Mesh{
			ID:       prefix + "-node-" + anchor.ID,
			Geometry: docSceneGeometry(anchor.Shape),
			Material: docSceneMaterial(anchor.Accent),
			Position: anchor.Position,
			Spin:     anchor.Spin,
		})
	}

	return DocSceneFeature{
		Route:           spec.Route,
		Slug:            spec.Slug,
		SurfaceID:       prefix + "-surface",
		HeadingID:       prefix + "-heading",
		Eyebrow:         spec.Eyebrow,
		Title:           spec.Title,
		Summary:         spec.Summary,
		BackendTruth:    "WebGPU preferred; WebGL2 and Canvas2D remain explicit fallbacks.",
		InteractionHint: spec.InteractionHint,
		DemoHref:        spec.DemoHref,
		DemoLabel:       spec.DemoLabel,
		Scene: scene.Props{
			Width:               960,
			Height:              540,
			Label:               spec.Title,
			AriaLabel:           spec.Title,
			Background:          docSceneCanvas,
			Controls:            scene.ControlOrbit,
			AutoRotate:          scene.Bool(false),
			Responsive:          scene.Bool(true),
			FillHeight:          scene.Bool(true),
			PreferWebGPU:        scene.Bool(true),
			DragToRotate:        scene.Bool(true),
			UnsupportedMessage:  "Interactive 3D is unavailable; the adjacent teaching summary preserves the complete lesson.",
			ControlTarget:       scene.Vec3(0, 0, 0),
			ControlRotateSpeed:  0.55,
			ControlZoomSpeed:    0.7,
			ControlMinDistance:  5.5,
			ControlMaxDistance:  11,
			MaxFrameRate:        30,
			MaxDevicePixelRatio: 1.5,
			MaxPixels:           384000,
			AdaptiveQuality:     scene.Bool(true),
			Camera: scene.PerspectiveCamera{
				Position: scene.Vec3(0, 0.35, 8.4),
				FOV:      46,
				Near:     0.1,
				Far:      40,
			},
			Environment: scene.Environment{
				AmbientColor:     docSceneText,
				AmbientIntensity: 0.35,
				Exposure:         1,
				ToneMapping:      "aces",
			},
			Graph: scene.NewGraph(nodes...),
		},
	}
}

func docSceneGeometry(shape docSceneShape) scene.Geometry {
	switch shape {
	case docSceneSphere:
		return scene.SphereGeometry{Radius: 0.42, Segments: 16}
	case docScenePyramid:
		return scene.PyramidGeometry{Width: 0.85, Height: 0.9, Depth: 0.85}
	case docSceneTorus:
		return scene.TorusGeometry{Radius: 0.43, Tube: 0.12, RadialSegments: 12, TubularSegments: 24}
	case docSceneCylinder:
		return scene.CylinderGeometry{RadiusTop: 0.34, RadiusBottom: 0.34, Height: 0.85, Segments: 16}
	default:
		return scene.BoxGeometry{Width: 0.9, Height: 0.62, Depth: 0.42}
	}
}

func docSceneMaterial(accent bool) scene.Material {
	color := docSceneSecondary
	metalness := 0.35
	if accent {
		color = docSceneAccent
		metalness = 0.7
	}
	return scene.StandardMaterial{
		Color:     color,
		Roughness: 0.38,
		Metalness: metalness,
	}
}

func withDocSceneFeature(opts route.FileModuleOptions) route.FileModuleOptions {
	bindings := opts.Bindings
	opts.Bindings = func(ctx *route.RouteContext, page route.FilePage, data any) route.FileTemplateBindings {
		var bound route.FileTemplateBindings
		if bindings != nil {
			bound = bindings(ctx, page, data)
		}
		feature, ok := DocSceneFeatureForRoute(page.RoutePath)
		if !ok {
			return bound
		}
		return mergeDocsBindings(bound, route.FileTemplateBindings{
			Values: map[string]any{
				docSceneBindingKey: feature,
			},
		})
	}
	return opts
}
