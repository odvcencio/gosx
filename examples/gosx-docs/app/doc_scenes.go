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
		Route:           "/docs/motion",
		Slug:            "motion",
		Eyebrow:         "One motion graph, two render surfaces",
		Title:           "HTML and Scene3D read the same values in one frame.",
		Summary:         "Scroll progress moves the HTML card and camera together; a spring scales one scene node, and another follows the HTML marker.",
		InteractionHint: "Pointer interaction only: drag to orbit; wheel or pinch to zoom. Scroll the page; hover or focus the button to move the mint sphere, and the amber sphere follows its HTML marker.",
		DemoHref:        "/demos/scene3d",
		DemoLabel:       "See declarative motion in Scene3D",
		Anchors: []docSceneAnchor{
			{ID: "hover-node", Position: scene.Vec3(1.5, 0.25, 0), Shape: docSceneSphere, Accent: true},
			{ID: "pinned-node", Position: scene.Vec3(-1.3, -0.35, 0), Shape: docSceneSphere},
		},
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
	controls := scene.ControlOrbit
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
			Controls:            controls,
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
