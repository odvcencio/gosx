package route

import (
	"net/http"

	"github.com/odvcencio/gosx"
	"github.com/odvcencio/gosx/scene"
)

// benchStaticRouter returns a router with a few static routes for measuring
// the dispatch overhead on a no-loader, no-params handler.
func benchStaticRouter() http.Handler {
	r := NewRouter()
	r.SetLayout(func(ctx *RouteContext, body gosx.Node) gosx.Node {
		return gosx.El("html", gosx.El("body", body))
	})
	r.Add(
		Route{Pattern: "/", Handler: func(ctx *RouteContext) gosx.Node { return gosx.Text("home") }},
		Route{Pattern: "/about", Handler: func(ctx *RouteContext) gosx.Node { return gosx.Text("about") }},
		Route{Pattern: "/contact", Handler: func(ctx *RouteContext) gosx.Node { return gosx.Text("contact") }},
	)
	return r.Build()
}

// benchParamRouter exercises the param-extraction hot path with two
// path-value parameters per request.
func benchParamRouter() http.Handler {
	r := NewRouter()
	r.SetLayout(func(ctx *RouteContext, body gosx.Node) gosx.Node {
		return gosx.El("html", gosx.El("body", body))
	})
	r.Add(Route{
		Pattern: "/users/{userID}/posts/{slug}",
		Handler: func(ctx *RouteContext) gosx.Node {
			return gosx.Text("user " + ctx.Param("userID") + " post " + ctx.Param("slug"))
		},
	})
	return r.Build()
}

// benchNestedRouter exercises a small layout chain (root layout wraps
// dashboard layout wraps page) so we can see the layout-composition cost
// per request.
func benchNestedRouter() http.Handler {
	r := NewRouter()
	r.SetLayout(func(ctx *RouteContext, body gosx.Node) gosx.Node {
		return gosx.El("html", gosx.El("body",
			gosx.Attrs(gosx.Attr("class", "root")), body))
	})
	r.Add(Route{
		Pattern: "/dashboard",
		Layout: func(ctx *RouteContext, body gosx.Node) gosx.Node {
			return gosx.El("section",
				gosx.Attrs(gosx.Attr("class", "dashboard")),
				gosx.El("nav", gosx.Text("dashboard nav")),
				body,
			)
		},
		Children: []Route{
			{
				Pattern: "/settings",
				Handler: func(ctx *RouteContext) gosx.Node {
					return gosx.El("main", gosx.Text("settings page body"))
				},
			},
		},
	})
	return r.Build()
}

// benchScene3DProps returns a moderate Scene3D fixture used by the
// engine-props benches. Matches scene.benchMixedScene's shape —
// 20 meshes, a directional + point light, ambient light, a wireframe
// overlay, a grounding plane, shadow + tonemap + bloom post-fx.
// Inlined here instead of exported from the scene package because
// it's only used by cross-package benchmarks and the scene test
// fixtures stay _test.go-scoped.
func benchScene3DProps() scene.Props {
	const boxCount = 20
	nodes := make([]scene.Node, 0, 32)
	nodes = append(nodes,
		scene.DirectionalLight{
			Color:      "#fff1d6",
			Intensity:  1.1,
			Direction:  scene.Vec3(0.3, -1, -0.5),
			CastShadow: true,
			ShadowSize: 2048,
		},
		scene.PointLight{
			Color:     "#5fa3ff",
			Intensity: 0.8,
			Position:  scene.Vec3(0, 4, 0),
			Range:     15,
		},
		scene.AmbientLight{Color: "#ffffff", Intensity: 0.2},
	)
	for range boxCount {
		nodes = append(nodes, scene.Mesh{
			Geometry: scene.SphereGeometry{Segments: 24},
			Material: scene.StandardMaterial{
				Color:     "#d4af37",
				Roughness: 0.3,
				Metalness: 0.9,
			},
			Position:      scene.Vec3(0, 0.5, 0),
			CastShadow:    true,
			ReceiveShadow: true,
		})
	}
	return scene.Props{
		Width:      1024,
		Height:     600,
		Background: "#05080f",
		Responsive: scene.Bool(true),
		Controls:   "orbit",
		Camera: scene.PerspectiveCamera{
			Position: scene.Vec3(0, 4, 10),
			FOV:      55,
		},
		Environment: scene.Environment{
			AmbientColor:     "#ffffff",
			AmbientIntensity: 0.2,
		},
		Shadows: scene.Shadows{MaxPixels: scene.ShadowMaxPixels1024},
		PostFX: scene.PostFX{
			MaxPixels: scene.PostFXMaxPixels1080p,
			Effects: []scene.PostEffect{
				scene.Bloom{Threshold: 0.8, Strength: 0.4, Radius: 6, Scale: 0.25},
				scene.Tonemap{Mode: scene.TonemapACES, Exposure: 1.1},
			},
		},
		Graph: scene.NewGraph(nodes...),
	}
}

// benchGalaxyScene3DProps returns an 80-mesh galaxy-scale fixture
// approximating m31labs.dev's homepage scene — stresses the lowerer
// and the marshal at realistic production node counts.
func benchGalaxyScene3DProps() scene.Props {
	const starCount = 80
	nodes := make([]scene.Node, 0, starCount+4)
	nodes = append(nodes,
		scene.DirectionalLight{
			Color:      "#fff1d6",
			Intensity:  1.1,
			Direction:  scene.Vec3(0.3, -1, -0.5),
			CastShadow: true,
			ShadowSize: 2048,
		},
		scene.AmbientLight{Color: "#101325", Intensity: 0.12},
	)
	for i := 0; i < starCount; i++ {
		angle := float64(i) / float64(starCount)
		nodes = append(nodes, scene.Mesh{
			Geometry: scene.SphereGeometry{Segments: 12},
			Material: scene.StandardMaterial{
				Color:     "#c8a8ff",
				Roughness: 0.2,
				Metalness: 0.4,
				Emissive:  0.35,
			},
			Position:   scene.Vec3(6*angle, 0.5, 3*angle),
			CastShadow: false,
		})
	}
	nodes = append(nodes,
		scene.Mesh{
			Geometry: scene.PlaneGeometry{Width: 40, Height: 40},
			Material: scene.StandardMaterial{Color: "#05080f", Roughness: 0.9, Metalness: 0.05},
			Rotation: scene.Rotate(-1.5708, 0, 0),
		},
	)
	return scene.Props{
		Width:      1280,
		Height:     720,
		Background: "#03070d",
		Responsive: scene.Bool(true),
		Controls:   "orbit",
		Camera: scene.PerspectiveCamera{
			Position: scene.Vec3(0, 6, 14),
			FOV:      55,
		},
		Environment: scene.Environment{
			AmbientColor:     "#ffffff",
			AmbientIntensity: 0.22,
		},
		Shadows: scene.Shadows{MaxPixels: scene.ShadowMaxPixels1024},
		PostFX: scene.PostFX{
			MaxPixels: scene.PostFXMaxPixels1080p,
			Effects: []scene.PostEffect{
				scene.Bloom{Threshold: 0.7, Strength: 0.55, Radius: 8, Scale: 0.25},
				scene.Tonemap{Mode: scene.TonemapACES, Exposure: 1.15},
			},
		},
		Graph: scene.NewGraph(nodes...),
	}
}
