package app

import (
	"fmt"
	"math"
	"strings"

	"m31labs.dev/gosx/examples/turbofan/cycle"
	"m31labs.dev/gosx/scene"
)

// Options combines presentation choices with a solved operating point's visual
// state. Geometry is illustrative; mapped spool ratios come from the solver.
type Options struct {
	Exterior bool
	View     string
	Backend  string
	Visual   *cycle.RenderState
}

var (
	titanium = scene.StandardMaterial{Color: "#b9c8d1", Metalness: .68, Roughness: .34}
	steel    = scene.StandardMaterial{Color: "#637c8c", Metalness: .62, Roughness: .4}
	shell    = scene.StandardMaterial{Color: "#536979", Metalness: .58, Roughness: .38}
	ceramic  = scene.StandardMaterial{Color: "#ce9870", Metalness: .4, Roughness: .47}
	copper   = scene.StandardMaterial{Color: "#ec9156", Metalness: .48, Roughness: .32}
)

// Turbofan builds all geometry in Go. X is the shaft axis, with intake at -X.
// Only the two complete rotor assemblies spin; no per-blade spin is copied.
func Turbofan(options Options) scene.Props {
	lowSpeed, highSpeed := .48, .82
	liner := ceramic
	if options.Visual != nil {
		lowSpeed, highSpeed = options.Visual.LowSpin, options.Visual.HighSpin
		liner.Color = options.Visual.Color
	}
	low := []scene.Node{
		lathe("spinner", []scene.LathePoint{{Radius: .02, Y: -5.1}, {Radius: .22, Y: -4.95}, {Radius: .44, Y: -4.55}, {Radius: .56, Y: -4.12}, {Radius: .54, Y: -3.85}}, titanium, false),
		shaft("low-shaft", -3.9, 4.15, .16, steel),
	}
	low = append(low, bladeRow("fan", -3.95, .55, 2.14, .66, 24, .0, titanium)...)
	low = append(low, shaft("fan-disc", -4.03, -3.83, .66, steel))
	for i := range 3 {
		x := 2.55 + float64(i)*.48
		low = append(low, bladeRow(fmt.Sprintf("lp-turbine-%d", i), x, .36, .86+float64(i)*.035, .2, 26, .18, ceramic)...)
		low = append(low, shaft(fmt.Sprintf("lp-disc-%d", i), x-.07, x+.07, .43, steel))
	}

	high := []scene.Node{shaft("high-shaft", -2.65, 2.22, .28, steel)}
	for i := range 7 {
		x := -2.6 + float64(i)*.48
		tip := 1.05 - float64(i)*.052
		high = append(high, bladeRow(fmt.Sprintf("compressor-%d", i), x, .37+float64(i)*.037, tip, .21, 30, float64(i)*.06, titanium)...)
		high = append(high, shaft(fmt.Sprintf("compressor-disc-%d", i), x-.07, x+.07, .43+float64(i)*.032, steel))
	}
	high = append(high, bladeRow("hp-turbine", 1.91, .35, .82, .23, 30, 0, copper)...)
	high = append(high, shaft("hp-disc", 1.8, 2.02, .43, steel))

	nodes := []scene.Node{
		scene.DirectionalLight{ID: "key", Color: "#fff2df", Intensity: 1.4, Direction: scene.Vec3(.25, -1, -.7)},
		scene.DirectionalLight{ID: "rim", Color: "#b2d6ff", Intensity: .8, Direction: scene.Vec3(-.35, .3, 1)},
		scene.HemisphereLight{ID: "studio", SkyColor: "#d9e9fa", GroundColor: "#3b3633", Intensity: .4},
		scene.Group{ID: "low-spool", Spin: scene.Rotate(lowSpeed, 0, 0), Children: low},
		scene.Group{ID: "high-spool", Spin: scene.Rotate(highSpeed, 0, 0), Children: high},
	}
	cut := true
	// Closed profiles give the nacelle real wall thickness. In section mode the
	// near half is omitted in the generated geometry, rather than made transparent.
	nodes = append(nodes,
		lathe("nacelle", []scene.LathePoint{{Radius: 2.28, Y: -4.45}, {Radius: 2.4, Y: -4.3}, {Radius: 2.46, Y: -3.8}, {Radius: 2.4, Y: -2.6}, {Radius: 2.16, Y: -.7}, {Radius: 1.86, Y: 1.15}, {Radius: 1.76, Y: 1.3}, {Radius: 1.72, Y: 1.13}, {Radius: 2.02, Y: -.7}, {Radius: 2.26, Y: -2.6}, {Radius: 2.32, Y: -3.8}, {Radius: 2.28, Y: -4.15}, {Radius: 2.18, Y: -4.3}, {Radius: 2.28, Y: -4.45}}, shell, cut),
		lathe("intake-lip", []scene.LathePoint{{Radius: 2.18, Y: -4.3}, {Radius: 2.2, Y: -4.5}, {Radius: 2.3, Y: -4.58}, {Radius: 2.4, Y: -4.47}, {Radius: 2.4, Y: -4.3}}, titanium, cut),
		lathe("core-case", []scene.LathePoint{{Radius: 1.17, Y: -2.95}, {Radius: 1.14, Y: -2.4}, {Radius: .95, Y: .4}, {Radius: 1.02, Y: 1.4}, {Radius: 1.04, Y: 2.2}, {Radius: 1.14, Y: 3.65}, {Radius: 1.07, Y: 3.65}, {Radius: .97, Y: 2.2}, {Radius: .95, Y: 1.4}, {Radius: .88, Y: .4}, {Radius: 1.07, Y: -2.4}, {Radius: 1.1, Y: -2.95}, {Radius: 1.17, Y: -2.95}}, steel, cut),
		lathe("combustor-liner", []scene.LathePoint{{Radius: .73, Y: .58}, {Radius: .85, Y: .8}, {Radius: .86, Y: 1.35}, {Radius: .75, Y: 1.65}, {Radius: .69, Y: 1.65}, {Radius: .78, Y: 1.35}, {Radius: .77, Y: .8}, {Radius: .66, Y: .58}, {Radius: .73, Y: .58}}, liner, cut),
		lathe("exhaust-duct", []scene.LathePoint{{Radius: 1.14, Y: 3.65}, {Radius: .95, Y: 4.7}, {Radius: .88, Y: 4.7}, {Radius: 1.07, Y: 3.65}, {Radius: 1.14, Y: 3.65}}, titanium, cut),
		lathe("exhaust-cone", []scene.LathePoint{{Radius: .01, Y: 5.2}, {Radius: .21, Y: 4.8}, {Radius: .39, Y: 4.25}, {Radius: .44, Y: 3.65}, {Radius: 0, Y: 3.65}}, steel, false),
	)
	// Stators stay attached to the casing between moving stages.
	for i := range 6 {
		x := -2.36 + float64(i)*.48
		nodes = append(nodes, bladeRow(fmt.Sprintf("stator-%d", i), x, .43+float64(i)*.03, 1.03-float64(i)*.05, .13, 26, .1, steel)...)
	}
	// Outlet guide vanes visibly connect the core to the bypass duct.
	for i := range 10 {
		angle := float64(i) * 2 * math.Pi / 10
		nodes = append(nodes, scene.Mesh{ID: fmt.Sprintf("guide-%d", i), Geometry: scene.BoxGeometry{Width: .48, Height: 1.05, Depth: .055}, Material: steel, Position: scene.Vec3(-3.1, 1.61*math.Cos(angle), 1.61*math.Sin(angle)), Rotation: scene.Rotate(angle, 0, -.15)})
	}
	for i := range 12 {
		angle := float64(i) * 2 * math.Pi / 12
		nodes = append(nodes, scene.Mesh{ID: fmt.Sprintf("injector-%d", i), Geometry: scene.CylinderGeometry{RadiusTop: .055, RadiusBottom: .075, Height: .36, Segments: 8}, Material: copper, Position: scene.Vec3(.69, .59*math.Cos(angle), .59*math.Sin(angle)), Rotation: scene.Rotate(0, 0, -math.Pi/2)})
	}
	// Circumferential joints and section edges clarify the silhouette.
	for i, x := range []float64{-4.27, -3.65, -2.85, -.6, 1.17} {
		r := []float64{2.38, 2.46, 2.43, 2.16, 1.85}[i]
		nodes = append(nodes, collar(fmt.Sprintf("nacelle-joint-%d", i), x, r, .025, titanium, cut))
	}
	if cut {
		for i, edge := range []struct{ x, y, radius float64 }{{-4.4, -2.6, 2.35}, {-2.6, -.7, 2.13}, {-.7, 1.2, 1.88}, {-2.9, .4, 1.0}, {.4, 2.2, .98}, {2.2, 3.65, 1.1}} {
			// Thin orange rails mark the two cut faces without obscuring the machinery.
			for _, sign := range []float64{-1, 1} {
				nodes = append(nodes, scene.Mesh{ID: fmt.Sprintf("section-edge-%d-%g", i, sign), Geometry: scene.BoxGeometry{Width: edge.y - edge.x, Height: .025, Depth: .025}, Material: copper, Position: scene.Vec3((edge.x+edge.y)/2, sign*edge.radius, 0), Visible: scene.Bool(!options.Exterior)})
			}
		}
	}
	// Both halves are resident. Casing controls only patch visibility, preserving
	// geometry buffers, the orbit camera and the current rotor clock.
	for _, node := range nodes {
		mesh, ok := node.(scene.Mesh)
		if !ok || !isCasing(mesh.ID) {
			continue
		}
		geometry := mesh.Geometry.(scene.BufferGeometry)
		cover := geometry
		cover.Positions = append([]float64(nil), geometry.Positions...)
		cover.Normals = append([]float64(nil), geometry.Normals...)
		cover.Indices = append([]int(nil), geometry.Indices...)
		for i := 2; i < len(cover.Positions); i += 3 {
			cover.Positions[i] = -cover.Positions[i]
			cover.Normals[i] = -cover.Normals[i]
		}
		for i := 0; i < len(cover.Indices); i += 3 {
			cover.Indices[i+1], cover.Indices[i+2] = cover.Indices[i+2], cover.Indices[i+1]
		}
		mesh.ID += "-cover"
		mesh.Geometry = cover
		mesh.Visible = scene.Bool(options.Exterior)
		nodes = append(nodes, mesh)
	}
	presentation := &scene.Presentation{InputSignal: "$turbofan.presentation", OutputSignal: "$turbofan.presentationState", InitialView: options.View, InitialPeriod: "cutaway", Query: true}
	if presentation.InitialView == "" {
		presentation.InitialView = "perspective"
	}
	if options.Exterior {
		presentation.InitialPeriod = "exterior"
	}
	for _, view := range []string{"perspective", "side", "intake"} {
		presentation.Views = append(presentation.Views, scene.PresentationView{ID: view, Camera: scene.CameraIR(viewCamera(view))})
	}
	for _, period := range []string{"cutaway", "exterior"} {
		commands := []scene.Command{}
		for _, node := range nodes {
			mesh, ok := node.(scene.Mesh)
			if !ok {
				continue
			}
			if strings.HasSuffix(mesh.ID, "-cover") || strings.HasPrefix(mesh.ID, "section-edge-") {
				visible := (period == "exterior") == strings.HasSuffix(mesh.ID, "-cover")
				commands = append(commands, scene.Command{Kind: scene.CommandSetTransform, ObjectID: mesh.ID, Data: map[string]any{"visible": visible}})
			}
		}
		presentation.Periods = append(presentation.Periods, scene.PresentationPeriod{ID: period, Commands: commands})
	}
	return scene.Props{
		AriaLabel: "Procedural two-spool turbofan. Drag to orbit; scroll or pinch to zoom.",
		Width:     1280, Height: 540, Responsive: scene.Bool(true), FillHeight: scene.Bool(true), Background: "#010203",
		Controls: "orbit", ControlTarget: scene.Vec3(-.1, 0, 0), AutoRotate: scene.Bool(false), ControlZoom: scene.Bool(true), ControlMinDistance: 6, ControlMaxDistance: 25,
		Camera:       viewCamera(options.View),
		Presentation: presentation,
		Environment:  scene.Environment{AmbientColor: "#dce8f2", AmbientIntensity: .25, ToneMapping: "aces", Exposure: .95},
		MaxFPS:       30, MaxDevicePixelRatio: 1.5, MaxPixels: 1280 * 720,
		PreferWebGPU: scene.Bool(options.Backend == "webgpu"), ForceWebGL: scene.Bool(options.Backend == "webgl"),
		UnsupportedMessage: "3D rendering is unavailable in this browser. The component descriptions below explain the engine cycle.",
		Graph:              scene.NewGraph(nodes...),
	}
}

func viewCamera(view string) scene.PerspectiveCamera {
	position := scene.Vec3(-5.9, 3.6, 11.3)
	switch view {
	case "side":
		position = scene.Vec3(-.1, 1.2, 13.5)
	case "intake":
		position = scene.Vec3(-11.5, 1.1, 2.2)
	}
	return scene.PerspectiveCamera{Position: position, FOV: 32, PortraitFOV: 56, Near: .1, Far: 100}
}

func isCasing(id string) bool {
	return id == "nacelle" || id == "intake-lip" || id == "core-case" || id == "combustor-liner" || id == "exhaust-duct" || strings.HasPrefix(id, "nacelle-joint-")
}

func lathe(id string, profile []scene.LathePoint, material scene.Material, cut bool) scene.Mesh {
	start, length := 0.0, 2*math.Pi
	if cut {
		start, length = math.Pi, math.Pi // retain the far (-Z) half
	}
	geometry := scene.LatheGeometry(profile, 48, start, length)
	geometry.Immutable, geometry.Revision = true, 1
	return scene.Mesh{ID: id, Geometry: geometry, Material: material, Rotation: scene.Rotate(0, 0, -math.Pi/2)}
}

func shaft(id string, start, end, radius float64, material scene.Material) scene.Mesh {
	return scene.Mesh{ID: id, Geometry: scene.CylinderGeometry{RadiusTop: radius, RadiusBottom: radius, Height: end - start, Segments: 32}, Material: material, Position: scene.Vec3((start+end)/2, 0, 0), Rotation: scene.Rotate(0, 0, -math.Pi/2)}
}

func collar(id string, x, radius, thickness float64, material scene.Material, cut bool) scene.Mesh {
	return lathe(id, []scene.LathePoint{{Radius: radius, Y: x - thickness}, {Radius: radius + thickness, Y: x}, {Radius: radius, Y: x + thickness}}, material, cut)
}

// bladeRow batches a complete row of twisted airfoils into one indexed mesh.
// InstancedMesh currently supports named primitives, not BufferGeometry. Baking
// repeated local transforms here retains the airfoil on all backends and gives
// each row one draw call. The enclosing Group still owns the assembly's motion.
func bladeRow(id string, x, root, tip, chord float64, count int, phase float64, material scene.Material) []scene.Node {
	geometry := scene.ParametricGeometry(func(u, v float64) scene.Vector3 {
		span := tip - root
		theta := v * 2 * math.Pi
		width := chord * (1 - .43*u)
		twist := .62 - .86*u
		axial := math.Cos(theta) * width * .5
		tangent := math.Sin(theta)*width*.085 + .13*width*math.Sin(theta)*math.Sin(theta)
		return scene.Vec3(axial*math.Cos(twist)-tangent*math.Sin(twist)+chord*.28*u*u, span*u, axial*math.Sin(twist)+tangent*math.Cos(twist)+chord*.34*u*u)
	}, 5, 8)
	batch := scene.BufferGeometry{Immutable: true, Revision: 1}
	round := func(value float64) float64 { return math.Round(value*1e5) / 1e5 }
	for i := range count {
		angle := phase + float64(i)*2*math.Pi/float64(count)
		cos, sin := math.Cos(angle), math.Sin(angle)
		offset := len(batch.Positions) / 3
		for j := 0; j < len(geometry.Positions); j += 3 {
			p, n := geometry.Positions[j:j+3], geometry.Normals[j:j+3]
			batch.Positions = append(batch.Positions, round(x+p[0]), round((root+p[1])*cos-p[2]*sin), round((root+p[1])*sin+p[2]*cos))
			batch.Normals = append(batch.Normals, round(n[0]), round(n[1]*cos-n[2]*sin), round(n[1]*sin+n[2]*cos))
		}
		batch.UVs = append(batch.UVs, geometry.UVs...)
		for _, index := range geometry.Indices {
			batch.Indices = append(batch.Indices, offset+index)
		}
	}
	return []scene.Node{scene.Mesh{ID: id, Geometry: batch, Material: material}}
}
