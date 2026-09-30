package docs

import (
	"fmt"

	beaconDemo "m31labs.dev/gosx/examples/gosx-docs/app/demos/beacon"
	checkersDemo "m31labs.dev/gosx/examples/gosx-docs/app/demos/checkers"
	htmlSurfaceDemo "m31labs.dev/gosx/examples/gosx-docs/app/demos/html-surface"
	orreryDemo "m31labs.dev/gosx/examples/gosx-docs/app/demos/orrery"
	scene3dDemo "m31labs.dev/gosx/examples/gosx-docs/app/demos/scene3d"
	scene3dBenchDemo "m31labs.dev/gosx/examples/gosx-docs/app/demos/scene3d-bench"
	"m31labs.dev/gosx/scene"
	"m31labs.dev/gosx/scene/capability"
)

var demoSummaries = map[string]string{
	"showreel":      "Orbit a compact sculpture assembled from typed Go scene data.",
	"checkers":      "Play a two-seat Chinese Checkers match with Go-checked moves and CPU turns.",
	"beacon":        "Change the light and tide as you orbit a volcanic shoreline.",
	"water":         "Draw ripples and watch the pool bend light around floating objects.",
	"playground":    "Edit a GoSX component and compile its browser preview as you type.",
	"fluid":         "Drag through a server-computed velocity field and trace its flow.",
	"livesim":       "Drop circles into a physics world shared by every open tab.",
	"collab":        "Edit one Markdown document from two tabs and watch changes converge.",
	"scene3d":       "Turn seven materials under one composed Scene3D light rig.",
	"scene3d-bench": "Compare renderer workloads with measured frame-time charts.",
	"html-surface":  "Orbit HTML panels textured onto geometry inside a 3D scene.",
	"cms":           "Build a block page, inspect its live preview, and publish a validated draft.",
	"orrery":        "Pause a 24-second star-system animation and orbit its keyframed scene.",
}

var demoGroups = map[string]string{
	"showreel":      "Scenes and materials",
	"scene3d":       "Scenes and materials",
	"html-surface":  "Scenes and materials",
	"orrery":        "Scenes and materials",
	"checkers":      "Games and interaction",
	"fluid":         "Live systems",
	"livesim":       "Live systems",
	"collab":        "Live systems",
	"playground":    "Application patterns",
	"cms":           "Application patterns",
	"beacon":        "Engine studies",
	"scene3d-bench": "Engine studies",
	"water":         "",
}

var demoSources = map[string][]string{
	"showreel": {
		"examples/gosx-docs/app/demos/showreel.go",
		"examples/gosx-docs/app/demos/showreel/page.gsx",
		"examples/gosx-docs/app/demos/showreel/page.server.go",
		"examples/gosx-docs/app/demos/showreel/page.css",
	},
	"checkers": {
		"examples/gosx-docs/app/demos/checkers/compute.go",
		"examples/gosx-docs/app/demos/checkers/generate.go",
		"examples/gosx-docs/app/demos/checkers/match.go",
		"examples/gosx-docs/app/demos/checkers/materials/sources/brushed-steel.sel",
		"examples/gosx-docs/app/demos/checkers/materials/sources/carved-wood.sel",
		"examples/gosx-docs/app/demos/checkers/materials/sources/imperial-jade.sel",
		"examples/gosx-docs/app/demos/checkers/materials/sources/midnight-lacquer.sel",
		"examples/gosx-docs/app/demos/checkers/materials/sources/moon-porcelain.sel",
		"examples/gosx-docs/app/demos/checkers/materials/materials.go",
		"examples/gosx-docs/app/demos/checkers/page.css",
		"examples/gosx-docs/app/demos/checkers/page.gsx",
		"examples/gosx-docs/app/demos/checkers/page.server.go",
		"examples/gosx-docs/app/demos/checkers/policy/policy.go",
		"examples/gosx-docs/app/demos/checkers/previewgen/main.go",
		"examples/gosx-docs/app/demos/checkers/program.go",
		"examples/gosx-docs/app/demos/checkers/policy/checkers-policy.arb",
		"examples/gosx-docs/app/demos/checkers/replay.go",
		"examples/gosx-docs/app/demos/checkers/rules.go",
		"examples/gosx-docs/app/demos/checkers/search.go",
		"examples/gosx-docs/app/demos/checkers/topology.go",
		"examples/gosx-docs/public/checkers-client.js",
	},
	"beacon": {
		"examples/gosx-docs/app/demos/beacon/beach.go",
		"examples/gosx-docs/app/demos/beacon/ibl-beach/golden-hour.json",
		"examples/gosx-docs/internal/beachgen/beachgen.go",
		"examples/gosx-docs/internal/beachgen/skyibl.go",
		"examples/gosx-docs/app/demos/beacon/page.css",
		"examples/gosx-docs/app/demos/beacon/page.gsx",
		"examples/gosx-docs/app/demos/beacon/page.server.go",
		"examples/gosx-docs/app/demos/beacon/route.config.json",
	},
	"water": {
		"examples/gosx-docs/app/demos/water/cmd/generate-selena-fixtures/main.go",
		"examples/gosx-docs/app/demos/water/diag.go",
		"examples/gosx-docs/app/demos/water/page.css",
		"examples/gosx-docs/app/demos/water/page.gsx",
		"examples/gosx-docs/app/demos/water/page.server.go",
		"examples/gosx-docs/app/demos/water/program.go",
		"examples/gosx-docs/app/demos/water/route.config.json",
		"examples/gosx-docs/app/demos/water/selena_glsl.go",
		"examples/gosx-docs/app/demos/water/shaders/jeantimex-water.selena/caustics.sel",
		"examples/gosx-docs/app/demos/water/shaders/jeantimex-water.selena/compound-shadow.sel",
		"examples/gosx-docs/app/demos/water/shaders/jeantimex-water.selena/displacement.sel",
		"examples/gosx-docs/app/demos/water/shaders/jeantimex-water.selena/drop.sel",
		"examples/gosx-docs/app/demos/water/shaders/jeantimex-water.selena/duck-material.sel",
		"examples/gosx-docs/app/demos/water/shaders/jeantimex-water.selena/normal.sel",
		"examples/gosx-docs/app/demos/water/shaders/jeantimex-water.selena/object-material.sel",
		"examples/gosx-docs/app/demos/water/shaders/jeantimex-water.selena/object-mesh-shadow.sel",
		"examples/gosx-docs/app/demos/water/shaders/jeantimex-water.selena/object-shadow.sel",
		"examples/gosx-docs/app/demos/water/shaders/jeantimex-water.selena/pool.sel",
		"examples/gosx-docs/app/demos/water/shaders/jeantimex-water.selena/seed.sel",
		"examples/gosx-docs/app/demos/water/shaders/jeantimex-water.selena/simulation.sel",
		"examples/gosx-docs/app/demos/water/shaders/jeantimex-water.selena/surface-below.sel",
		"examples/gosx-docs/app/demos/water/shaders/jeantimex-water.selena/surface.sel",
		"examples/gosx-docs/public/water-diag.js",
	},
	"playground": {
		"examples/gosx-docs/app/demos/playground/cache.go",
		"examples/gosx-docs/app/demos/playground/compile_handler.go",
		"examples/gosx-docs/app/demos/playground/page.css",
		"examples/gosx-docs/app/demos/playground/page.gsx",
		"examples/gosx-docs/app/demos/playground/page.server.go",
		"examples/gosx-docs/app/demos/playground/presets.go",
		"examples/gosx-docs/app/demos/playground/preview_policy.go",
		"examples/gosx-docs/app/demos/playground/route.config.json",
		"examples/gosx-docs/public/playground-editor.js",
	},
	"fluid": {
		"examples/gosx-docs/app/demos/fluid/page.css",
		"examples/gosx-docs/app/demos/fluid/page.gsx",
		"examples/gosx-docs/app/demos/fluid/page.server.go",
		"examples/gosx-docs/app/demos/fluid/sim.go",
		"examples/gosx-docs/app/demos/fluid/route.config.json",
		"examples/gosx-docs/public/fluid-client.js",
	},
	"livesim": {
		"examples/gosx-docs/app/demos/livesim/game.go",
		"examples/gosx-docs/app/demos/livesim/page.css",
		"examples/gosx-docs/app/demos/livesim/page.gsx",
		"examples/gosx-docs/app/demos/livesim/page.server.go",
		"examples/gosx-docs/app/demos/livesim/route.config.json",
		"examples/gosx-docs/public/livesim-client.js",
	},
	"collab": {
		"examples/gosx-docs/app/demos/collab/doc.go",
		"examples/gosx-docs/app/demos/collab/page.css",
		"examples/gosx-docs/app/demos/collab/page.gsx",
		"examples/gosx-docs/app/demos/collab/page.server.go",
		"examples/gosx-docs/app/demos/collab/presence.go",
		"examples/gosx-docs/app/demos/collab/route.config.json",
		"examples/gosx-docs/public/collab-client.js",
	},
	"scene3d": {
		"examples/gosx-docs/app/demos/scene3d/page.css",
		"examples/gosx-docs/app/demos/scene3d/page.gsx",
		"examples/gosx-docs/app/demos/scene3d/page.server.go",
		"examples/gosx-docs/app/demos/scene3d/program.go",
	},
	"scene3d-bench": {
		"examples/gosx-docs/app/demos/scene3d-bench/gpu_driven.go",
		"examples/gosx-docs/app/demos/scene3d-bench/page.css",
		"examples/gosx-docs/app/demos/scene3d-bench/page.gsx",
		"examples/gosx-docs/app/demos/scene3d-bench/page.server.go",
		"examples/gosx-docs/app/demos/scene3d-bench/program.go",
		"examples/gosx-docs/app/demos/scene3d-bench/route.config.json",
		"examples/gosx-docs/public/scene3d-bench-client.js",
	},
	"html-surface": {
		"examples/gosx-docs/app/demos/html-surface/page.css",
		"examples/gosx-docs/app/demos/html-surface/page.gsx",
		"examples/gosx-docs/app/demos/html-surface/page.server.go",
		"examples/gosx-docs/app/demos/html-surface/program.go",
	},
	"cms": {
		"examples/gosx-docs/app/demos/cms/page.css",
		"examples/gosx-docs/app/demos/cms/page.gsx",
		"examples/gosx-docs/app/demos/cms/page.server.go",
		"examples/gosx-docs/app/demos/cms/route.config.json",
		"examples/gosx-docs/public/cms-client.js",
	},
	"orrery": {
		"examples/gosx-docs/app/demos/orrery/page.css",
		"examples/gosx-docs/app/demos/orrery/page.gsx",
		"examples/gosx-docs/app/demos/orrery/page.server.go",
		"examples/gosx-docs/app/demos/orrery/program.go",
	},
}

type DemoGroup struct {
	ID          string
	Title       string
	Description string
	Demos       []DemoDefinition
}

var orderedDemoGroups = []DemoGroup{
	{ID: "scenes-and-materials", Title: "Scenes and materials", Description: "Study typed geometry, lighting, animation, and HTML textures."},
	{ID: "games-and-interaction", Title: "Games and interaction", Description: "Play a Go-checked board game with a Scene3D table."},
	{ID: "live-systems", Title: "Live systems", Description: "Follow server-owned simulations, shared documents, and presence."},
	{ID: "application-patterns", Title: "Application patterns", Description: "Compile components, edit content, and publish through server actions."},
	{ID: "engine-studies", Title: "Engine studies", Description: "Explore Blackglass Coast and inspect renderer workloads."},
}

var nonSceneBackends = map[string][]string{
	"playground": {"Server and browser"},
	"fluid":      {"Server and Canvas 2D"},
	"livesim":    {"Server and Canvas 2D"},
	"collab":     {"Server and browser DOM"},
	"cms":        {"Server and browser DOM"},
}

func GalleryDemos() ([]DemoDefinition, error) {
	demos := Demos()
	for i := range demos {
		props, isScene := scenePropsForDemo(demos[i].Slug)
		if !isScene {
			backends := nonSceneBackends[demos[i].Slug]
			if len(backends) == 0 {
				return nil, fmt.Errorf("demo %q has no backend description", demos[i].Slug)
			}
			demos[i].Backends = append([]string(nil), backends...)
			continue
		}
		verdict := props.SceneIR().BackendCaps
		if verdict == nil || len(verdict.Capable) == 0 {
			return nil, fmt.Errorf("demo %q produced no capable Scene3D backend verdict", demos[i].Slug)
		}
		for _, backend := range verdict.Capable {
			if label := backendLabel(backend); label != "" {
				demos[i].Backends = append(demos[i].Backends, label)
			}
		}
	}
	return demos, nil
}

func FeaturedGalleryDemos(demos []DemoDefinition) []DemoDefinition {
	return galleryShowcaseDemos(demos)
}

func galleryShowcaseDemos(demos []DemoDefinition) []DemoDefinition {
	// An explicitly featured, unranked demo is the new flagship. This lets a
	// focused demo land without editing the existing ranked catalog entries.
	for _, demo := range demos {
		if demo.Status == "featured" && demo.ShowcaseRank == 0 {
			return []DemoDefinition{demo}
		}
	}

	featured := make([]DemoDefinition, 0, 4)
	for rank := 1; rank <= 4; rank++ {
		for _, demo := range demos {
			if demo.ShowcaseRank == rank {
				featured = append(featured, demo)
				break
			}
		}
	}
	return featured
}

func galleryAdditionalDemos(demos []DemoDefinition) []DemoDefinition {
	featured := make(map[string]bool)
	for _, demo := range galleryShowcaseDemos(demos) {
		featured[demo.Slug] = true
	}

	additional := make([]DemoDefinition, 0, len(demos)-len(featured))
	for _, demo := range demos {
		if featured[demo.Slug] {
			continue
		}
		if demo.Status == "featured" {
			demo.Status = "live"
		}
		additional = append(additional, demo)
	}
	return additional
}

func GroupedGalleryDemos(demos []DemoDefinition) []DemoGroup {
	groups := make([]DemoGroup, len(orderedDemoGroups))
	for i, group := range orderedDemoGroups {
		groups[i] = DemoGroup{ID: group.ID, Title: group.Title, Description: group.Description}
	}
	groupIndex := make(map[string]int, len(groups))
	for i, group := range groups {
		groupIndex[group.Title] = i
	}
	featured := make(map[string]bool)
	for _, demo := range galleryShowcaseDemos(demos) {
		featured[demo.Slug] = true
	}
	for _, demo := range demos {
		if featured[demo.Slug] {
			continue
		}
		if demo.Status == "featured" {
			demo.Status = "live"
		}
		index, ok := groupIndex[demo.Group]
		if !ok {
			continue
		}
		groups[index].Demos = append(groups[index].Demos, demo)
	}
	result := make([]DemoGroup, 0, len(groups))
	for _, group := range groups {
		if len(group.Demos) > 0 {
			result = append(result, group)
		}
	}
	return result
}

func scenePropsForDemo(slug string) (scene.Props, bool) {
	switch slug {
	case "showreel":
		return DemoShowreelProgram(), true
	case "checkers":
		return checkersDemo.ShowcaseScene(), true
	case "beacon":
		return beaconDemo.BlackglassBeachProgram("shore", "golden-hour"), true
	case "water":
		// The Water page authors Scene3D with typed JSX elements. This matching
		// capability probe carries the same WaterSystem passes into SceneIR.
		return scene.Props{Graph: scene.NewGraph(scene.WaterSystem{
			ID: "water-main", Resolution: 256, SurfaceResolution: 48,
			ObjectTextureResolutionMode: "viewport", ObjectTexturePixelBudget: 786432,
			ObjectShadowResolution: 512, ActiveObject: "Sphere", ObjectKind: "sphere",
			Caustics: true, Reflection: true, Refraction: true,
		})}, true
	case "scene3d":
		return scene3dDemo.GeometryZooProgram(), true
	case "scene3d-bench":
		return scene3dBenchDemo.BenchPBRHeavyScene(), true
	case "html-surface":
		return htmlSurfaceDemo.HTMLSurfaceProgram(), true
	case "orrery":
		return orreryDemo.LodestarMeridianProgram(), true
	default:
		return scene.Props{}, false
	}
}

func backendLabel(backend capability.Backend) string {
	switch backend {
	case capability.BackendWebGPU:
		return "WebGPU"
	case capability.BackendWebGL:
		return "WebGL2"
	case capability.BackendCanvas2D:
		return "Canvas 2D"
	default:
		return ""
	}
}
