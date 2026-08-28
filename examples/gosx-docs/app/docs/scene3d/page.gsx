package docs

import ui "../../ui"

func Page() Node {
	return <div>
		<section id="scene-graph">
			<h2>Scene Graph</h2>
			<p>
				Scene3D is a Go-first 3D layer. You declare a scene as typed Go structs. The server lowers those structs into a compact intermediate representation (IR) and ships it on the wire. A browser-side engine rebuilds the scene on WebGPU, WebGL2, or a 2D canvas. No JavaScript scene code runs on your server.
			</p>
			<p>
				Scene3D is not a wrapper around three.js or Babylon.js. It carries its own renderers, its own physically based rendering (PBR) shaders, and its own glTF loader. Read
				<a href="/docs/scene3d-vs-threejs" data-gosx-link="true">Scene3D vs three.js</a>
				for a measured feature-by-feature comparison, including where three.js wins.
			</p>
			<p>
				A scene starts with
				<span class="inline-code">scene.Props</span>
				. It carries more than 70 fields. They cover:
			</p>
			<ul>
				<li>
					canvas size, background, and responsiveness;
				</li>
				<li>camera and controls;</li>
				<li>
					environment, post effects, and shadow policy;
				</li>
				<li>physics, audio, and the quality ladder;</li>
				<li>
					signal bindings and backend preferences;
				</li>
				<li>
					the root
					<span class="inline-code">scene.Graph</span>
					.
				</li>
			</ul>
			{CodeBlock("go", `import "m31labs.dev/gosx/scene"

	props := scene.Props{
	    Width:      1280,
	    Height:     720,
	    Background: "#08151f",
	    Responsive: scene.Bool(true),
	    Controls:   scene.ControlOrbit,
	    AriaLabel:  "Product configurator",
	    Camera: scene.PerspectiveCamera{
	        Position: scene.Vec3(0, 2, 8),
	        FOV:      65,
	    },
	    Environment: scene.Environment{
	        AmbientColor:     "#ffffff",
	        AmbientIntensity: 0.15,
	        Exposure:         1.2,
	        ToneMapping:      "aces",
	    },
	    Graph: scene.NewGraph(
	        // lights, meshes, models, particles, helpers ...
	    ),
	}`)}
			<p>
				Return the props from a route loader, then spread them into the
				<span class="inline-code">Scene3D</span>
				component.
			</p>
			{CodeBlock("go", `func Load(ctx *route.RouteContext, page route.FilePage) (any, error) {
	    return map[string]any{"scene": MyScene()}, nil
	}`)}
			{CodeBlock("gosx", `<Scene3D {...data.scene} />`)}
			<p>
				Set
				<span class="inline-code">AriaLabel</span>
				on every scene. The canvas carries it as an accessible name. Set
				<span class="inline-code">UnsupportedMessage</span>
				to control the text a browser sees when no backend can draw the scene.
			</p>
			<p>
				The live demo below is the scene in
				<span class="inline-code">app/docs/scene3d/program.go</span>
				. Drag to orbit. Scroll to zoom.
			</p>
		</section>
		<section id="scene3d-demo" class="scene3d-demo-well" aria-label="PBR demo scene">
			<Scene3D {...data.demoScene} />
		</section>
		<section id="no-hierarchical-scale">
			<h2>No Hierarchical Scale</h2>
			<div class="scene3d-warning" role="note">
				<p class="scene3d-warning__title">
					Read this before you port a three.js or glTF scene.
				</p>
				<p>
					A GoSX world transform carries position and rotation only. It carries no scale. A
					<span class="inline-code">scene.Group</span>
					has no
					<span class="inline-code">Scale</span>
					field at all. Parent scale never propagates to children.
				</p>
			</div>
			<p>
				<span class="inline-code">Mesh.Scale</span>
				,
				<span class="inline-code">Model.Scale</span>
				, and
				<span class="inline-code">MeshInstance.Scale</span>
				are leaf scales. Each one scales that node's own geometry and stops there. This is deliberate: a scale-free parent transform keeps normals correct without an inverse-transpose pass, and it keeps the ray tracer able to intersect primitives analytically.
			</p>
			<p>Three consequences follow.</p>
			<ul>
				<li>
					You cannot scale a subtree with one number. Scale each leaf.
				</li>
				<li>
					A glTF file that scales a parent node loses that scale on import.
				</li>
				<li>
					A three.js port that relies on
					<span class="inline-code">group.scale.set()</span>
					needs a rewrite, not a translation.
				</li>
			</ul>
			{CodeBlock("go", `// Wrong: Group has no Scale field. This does not compile.
	// scene.Group{Scale: scene.Vec3(2, 2, 2), Children: ...}

	// Right: scale each leaf mesh.
	scene.Group{
	    ID:       "cluster",
	    Position: scene.Vec3(0, 1, 0),
	    Rotation: scene.Rotate(0, 0.4, 0),
	    Children: []scene.Node{
	        scene.Mesh{
	            Geometry: scene.CubeGeometry{Size: 1},
	            Material: scene.StandardMaterial{Color: "#d4af37"},
	            Scale:    scene.Vec3(2, 2, 2),
	        },
	        scene.Mesh{
	            Geometry: scene.SphereGeometry{Radius: 0.5, Segments: 32},
	            Material: scene.StandardMaterial{Color: "#e8e8e8"},
	            Position: scene.Vec3(2, 0, 0),
	            Scale:    scene.Vec3(2, 2, 2),
	        },
	    },
	}`)}
		</section>
		<section id="backends">
			<h2>Backends and Capability Verdicts</h2>
			<p>
				Four renderers exist. Three of them draw browser frames. One does not.
			</p>
			<ul>
				<li>
					<span class="inline-code">WebGPU (JavaScript)</span>
					— the primary browser path. It owns compute particles, GPU instance culling, and the true GPU pick.
				</li>
				<li>
					<span class="inline-code">WebGL2 (JavaScript)</span>
					— the lazily loaded GPU fallback. The server verdict can select it first when it is the faithful available backend.
				</li>
				<li>
					<span class="inline-code">canvas2d (JavaScript)</span>
					— the last resort. It draws a flat approximation.
				</li>
				<li>
					<span class="inline-code">Go WebGPU (render/bundle)</span>
					— the desktop renderer plus a headless test oracle. It renders no browser frames by design.
				</li>
			</ul>
			<h3>The WebGL chunk loads on demand</h3>
			<p>
				The WebGL backend ships as a separate feature chunk. A page that settles on WebGPU does not need to fetch it during the initial mount. Read the build manifest and current Ouroboros receipt for byte totals; the documentation does not pin a historical bundle measurement.
			</p>
			<p>
				The runtime fetches the WebGL chunk when the capability verdict, author preference, or a WebGPU failure needs it. Canvas2D remains available only when the scene verdict permits that approximation.
			</p>
			<h3>The server computes the verdict</h3>
			<p>
				<span class="inline-code">scene/capability</span>
				is the single source of truth for which backend can draw a feature faithfully. Go collects the features a scene uses, computes a
				<span class="inline-code">BackendCaps</span>
				verdict, and ships it on the wire. The client obeys the verdict. A scene a backend cannot render faithfully gets diverted, not drawn wrong.
			</p>
			<p>
				Two outcomes exist per feature and backend. A required feature that a backend lacks
				<em>excludes</em>
				that backend. An optional feature that a backend lacks
				<em>degrades</em>
				it, and the reason ships with the verdict.
			</p>
			<div class="scene3d-table-wrap">
				<table class="scene3d-matrix">
					<caption>
						Every row of the
						<span class="inline-code">scene/capability</span>
						matrix, plus the default policy. A feature absent from the matrix is supported everywhere.
					</caption>
					<thead>
						<tr>
							<th scope="col">Feature</th>
							<th scope="col">WebGPU</th>
							<th scope="col">WebGL2</th>
							<th scope="col">Canvas2D</th>
							<th scope="col">Required</th>
						</tr>
					</thead>
					<tbody>
						<tr>
							<th scope="row">skinning</th>
							<td>yes</td>
							<td>yes</td>
							<td>no</td>
							<td>yes</td>
						</tr>
						<tr>
							<th scope="row">gpu-picking</th>
							<td>yes</td>
							<td>yes</td>
							<td>no</td>
							<td>yes</td>
						</tr>
						<tr>
							<th scope="row">water-simulation</th>
							<td>yes</td>
							<td>yes</td>
							<td>no</td>
							<td>yes</td>
						</tr>
						<tr>
							<th scope="row">water-object-texture-pass</th>
							<td>yes</td>
							<td>yes</td>
							<td>no</td>
							<td>yes</td>
						</tr>
						<tr>
							<th scope="row">water-object-mesh-shadow-pass</th>
							<td>yes</td>
							<td>no</td>
							<td>no</td>
							<td>no</td>
						</tr>
						<tr>
							<th scope="row">compute-particles</th>
							<td>yes</td>
							<td>no</td>
							<td>no</td>
							<td>no</td>
						</tr>
						<tr>
							<th scope="row">gpu-cull</th>
							<td>yes</td>
							<td>no</td>
							<td>no</td>
							<td>no</td>
						</tr>
						<tr>
							<th scope="row">environment-map</th>
							<td>yes</td>
							<td>yes</td>
							<td>no</td>
							<td>no</td>
						</tr>
						<tr>
							<th scope="row">ibl</th>
							<td>yes</td>
							<td>no</td>
							<td>no</td>
							<td>no</td>
						</tr>
						<tr>
							<th scope="row">line-dashed</th>
							<td>no</td>
							<td>no</td>
							<td>yes</td>
							<td>no</td>
						</tr>
						<tr>
							<th scope="row">sky-environment</th>
							<td>no</td>
							<td>no</td>
							<td>no</td>
							<td>no</td>
						</tr>
						<tr>
							<th scope="row">rect-area-light</th>
							<td>yes</td>
							<td>no</td>
							<td>no</td>
							<td>no</td>
						</tr>
						<tr>
							<th scope="row">rect-area-specular</th>
							<td>no</td>
							<td>no</td>
							<td>no</td>
							<td>no</td>
						</tr>
						<tr>
							<th scope="row">light-probe-sh</th>
							<td>no</td>
							<td>no</td>
							<td>no</td>
							<td>no</td>
						</tr>
					</tbody>
				</table>
			</div>
			<p>
				Read the table plainly. The legacy environment-map path is present on both GPU backends. Prepared split-sum IBL is faithful on WebGPU, while the WebGL2 path is capability-gated and therefore remains a degradation in the unconditional matrix. Compute particles degrade to a CPU mirror on WebGL2. Dashed-line styling is not faithful on either GPU backend; Canvas2D is the backend that implements the dash pattern. Skinning, water simulation, and GPU picking exclude Canvas2D.
			</p>
			<h3>Gate a scene to WebGPU on purpose</h3>
			<p>
				<span class="inline-code">RequiredCapabilities</span>
				gates the scene at mount time. The WebGPU probe negotiates optional adapter features, required features, and device limits before it reports success.
			</p>
			{CodeBlock("go", `props.RequiredCapabilities = scene.RequireWebGPU(
	    engine.CapWebGPUTimestampQuery,
	    engine.CapWebGPUShaderF16,
	    engine.WebGPULimit("maxTextureDimension2D", 4096),
	    engine.WebGPUAdapterLimit("maxTextureDimension2D", 8192),
	)

	props.WebGPUAlphaMode = "opaque"
	props.WebGPUColorSpace = "display-p3"
	props.WebGPUToneMapping = "extended"
	props.WebGPUPowerPreference = "high-performance"`)}
			{CodeBlock("gosx", `<Scene3D
	  {...data.scene}
	  requiredCapabilities="webgpu webgpu:timestamp-query"
	  webgpuAlphaMode="opaque"
	  webgpuColorSpace="display-p3"
	/>`)}
			<p>
				<span class="inline-code">Capabilities</span>
				describes the surface capability set used for runtime planning;
				<span class="inline-code">RequiredCapabilities</span>
				is the fail-closed browser gate. Do not use the broader set as a substitute for requirements.
			</p>
			<p>
				<span class="inline-code">PreferWebGPU</span>
				,
				<span class="inline-code">PreferWebGL</span>
				,
				<span class="inline-code">ForceWebGL</span>
				, and
				<span class="inline-code">PreferCanvas</span>
				steer backend selection. Use them to reproduce a path in development.
				<span class="inline-code">RequireWebGL</span>
				is different: it contributes hard
				<span class="inline-code">canvas</span>
				and
				<span class="inline-code">webgl</span>
				requirements and removes Canvas2D fallback.
			</p>
		</section>
		<section id="camera-controls">
			<h2>Camera and Controls</h2>
			<p>
				<span class="inline-code">scene.PerspectiveCamera</span>
				sets the initial view.
				<span class="inline-code">Props.OrthographicCamera</span>
				replaces it for computer-aided design (CAD), editor, and flat views. Set
				<span class="inline-code">TransitionMS</span>
				above zero and the client interpolates to the new pose instead of cutting.
			</p>
			{CodeBlock("go", `Camera: scene.PerspectiveCamera{
	    Position:     scene.Vec3(0, 2, 8),
	    Rotation:     scene.Rotate(0, 0, 0),
	    FOV:          65,
	    Near:         0.1,
	    Far:          1000,
	    TransitionMS: 600,
	},

	// Orthographic replaces the perspective camera when set.
	OrthographicCamera: &scene.OrthographicCamera{
	    Position: scene.Vec3(0, 10, 0),
	    Zoom:     1.5,
	    Near:     0.1,
	    Far:      500,
	},`)}
			<p>
				Three control modes exist. Omit
				<span class="inline-code">Controls</span>
				for a fixed camera.
			</p>
			<ul>
				<li>
					<span class="inline-code">scene.ControlOrbit</span>
					— pointer-drag orbit and wheel zoom around
					<span class="inline-code">ControlTarget</span>
					.
				</li>
				<li>
					<span class="inline-code">scene.ControlFirstPerson</span>
					— horizon-locked look with WASD movement.
				</li>
				<li>
					<span class="inline-code">scene.ControlFly</span>
					— free-flight look and movement, pitch included.
				</li>
			</ul>
			{CodeBlock("go", `Controls:               scene.ControlOrbit,
	ControlTarget:          scene.Vec3(0, 1, 0),
	ControlRotateSpeed:     1.0,
	ControlZoomSpeed:       1.2,
	ControlMinDistance:     3,
	ControlMaxDistance:     40,
	ControlPitchLimit:      1.2,
	ControlRotateDirection: "grab", // "orbit" keeps the historical direction
	DragToRotate:           scene.Bool(true),

	// First-person alternative.
	// Controls:         scene.ControlFirstPerson,
	// PointerLock:      scene.Bool(true),
	// ControlLookSpeed: 0.9,
	// ControlMoveSpeed: 6,

	// Scroll-driven camera for parallax hero sections. The two values are
	// page-scroll fractions: 0 is the top of the page, 1 is the bottom.
	ScrollCameraStart: 0.0,
	ScrollCameraEnd:   1.0,`)}
			<h3>Drive the camera from a signal</h3>
			<p>
				Four signal bindings let other parts of the page read and write scene state without app JavaScript.
			</p>
			<ul>
				<li>
					<span class="inline-code">CameraInputSignal</span>
					— the engine applies the camera from this signal. Use it for follow cameras.
				</li>
				<li>
					<span class="inline-code">CameraOutputSignal</span>
					— the engine publishes the camera on change.
				</li>
				<li>
					<span class="inline-code">CursorOutputSignal</span>
					— the engine publishes the normalized pointer position over the canvas, both axes in the range 0 to 1.
				</li>
				<li>
					<span class="inline-code">SelectionInputSignal</span>
					— bidirectional. The engine outlines the named object and writes its own viewport picks back.
				</li>
			</ul>
		</section>
		<section id="geometry">
			<h2>Geometry</h2>
			<p>
				Ten types implement
				<span class="inline-code">scene.Geometry</span>
				. One constructor,
				<span class="inline-code">scene.PolygonGeometry</span>
				, builds an eleventh shape from a 2D outline.
			</p>
			<div class="scene3d-geometry-grid">
				<div class="scene3d-geometry-card glass-panel">
					<span class="scene3d-geometry-name">CubeGeometry</span>
					<span class="scene3d-geometry-fields">Size</span>
				</div>
				<div class="scene3d-geometry-card glass-panel">
					<span class="scene3d-geometry-name">BoxGeometry</span>
					<span class="scene3d-geometry-fields">Width, Height, Depth</span>
				</div>
				<div class="scene3d-geometry-card glass-panel">
					<span class="scene3d-geometry-name">PlaneGeometry</span>
					<span class="scene3d-geometry-fields">Width, Height</span>
				</div>
				<div class="scene3d-geometry-card glass-panel">
					<span class="scene3d-geometry-name">PyramidGeometry</span>
					<span class="scene3d-geometry-fields">Width, Height, Depth</span>
				</div>
				<div class="scene3d-geometry-card glass-panel">
					<span class="scene3d-geometry-name">SphereGeometry</span>
					<span class="scene3d-geometry-fields">Radius, Segments</span>
				</div>
				<div class="scene3d-geometry-card glass-panel">
					<span class="scene3d-geometry-name">CylinderGeometry</span>
					<span class="scene3d-geometry-fields">
						RadiusTop, RadiusBottom, Height, Segments
					</span>
				</div>
				<div class="scene3d-geometry-card glass-panel">
					<span class="scene3d-geometry-name">TorusGeometry</span>
					<span class="scene3d-geometry-fields">
						Radius, Tube, RadialSegments, TubularSegments
					</span>
				</div>
				<div class="scene3d-geometry-card glass-panel">
					<span class="scene3d-geometry-name">TorusKnotGeometry</span>
					<span class="scene3d-geometry-fields">
						Radius, Tube, RadialSegments, TubularSegments
					</span>
				</div>
				<div class="scene3d-geometry-card glass-panel">
					<span class="scene3d-geometry-name">LinesGeometry</span>
					<span class="scene3d-geometry-fields">Points, Segments, Width</span>
				</div>
				<div class="scene3d-geometry-card glass-panel">
					<span class="scene3d-geometry-name">BufferGeometry</span>
					<span class="scene3d-geometry-fields">Positions, Normals, UVs, Indices</span>
				</div>
				<div class="scene3d-geometry-card glass-panel">
					<span class="scene3d-geometry-name">PolygonGeometry()</span>
					<span class="scene3d-geometry-fields">polygon, holes, y</span>
				</div>
			</div>
			<p>
				There is no cone type. A cone is a
				<span class="inline-code">CylinderGeometry</span>
				with
				<span class="inline-code">RadiusTop: 0</span>
				.
			</p>
			<h3>BufferGeometry</h3>
			<p>
				<span class="inline-code">BufferGeometry</span>
				carries raw triangle data: flat position and normal triples, flat texture-coordinate pairs, and optional indices. Use it for output from constructive solid geometry (CSG), a tessellator, or your own mesh generator. Indexed data expands into a flat triangle list at lowering time.
			</p>
			<h3>PolygonGeometry</h3>
			<p>
				<span class="inline-code">
					scene.PolygonGeometry(polygon, holes, y)
				</span>
				triangulates a 2D outline with holes and returns a
				<span class="inline-code">BufferGeometry</span>
				lying flat in the XZ plane at height
				<span class="inline-code">y</span>
				. It uses
				<span class="inline-code">scene/earcut</span>
				, a pure-Go port of mapbox/earcut. Normals point straight up. Texture coordinates are omitted; derive them from the positions if you need them.
			</p>
			{CodeBlock("go", `// A building footprint with a courtyard, at ground level.
	outline := []float64{0, 0, 20, 0, 20, 14, 0, 14}
	courtyard := [][]float64{{6, 4, 14, 4, 14, 10, 6, 10}}

	scene.Mesh{
	    Geometry: scene.PolygonGeometry(outline, courtyard, 0),
	    Material: scene.StandardMaterial{Color: "#2c3a46", Roughness: 0.9},
	    ReceiveShadow: true,
	}`)}
			{CodeBlock("go", `// A torus with smooth tessellation.
	scene.Mesh{
	    Geometry: scene.TorusGeometry{
	        Radius:          2.5,
	        Tube:            0.08,
	        RadialSegments:  64,
	        TubularSegments: 128,
	    },
	    Material: scene.StandardMaterial{Color: "#D4AF37", Roughness: 0.2, Metalness: 0.9},
	    Position: scene.Vec3(0, 1.5, 0),
	}

	// A cone: a cylinder with a zero top radius.
	scene.Mesh{
	    Geometry: scene.CylinderGeometry{
	        RadiusTop: 0, RadiusBottom: 1, Height: 2, Segments: 24,
	    },
	    Material: scene.StandardMaterial{Color: "#C0C0C0", Roughness: 0.3, Metalness: 0.8},
	}`)}
		</section>
		<section id="materials">
			<h2>Materials</h2>
			<p>
				Nine material types implement
				<span class="inline-code">scene.Material</span>
				.
			</p>
			<h3>StandardMaterial</h3>
			<p>
				<span class="inline-code">scene.StandardMaterial</span>
				is the PBR material. It uses the roughness and metalness workflow, plus five extended lobes: clearcoat, sheen, transmission, iridescence, and anisotropy. Each lobe is one scalar in the range 0 to 1. Anisotropy is the exception: it runs from -1 to 1, and the sign selects the tangent or the bitangent direction.
			</p>
			{CodeBlock("go", `// Polished gold.
	scene.StandardMaterial{
	    Color:     "#D4AF37",
	    Roughness: 0.15, // 0 = mirror, 1 = fully diffuse
	    Metalness: 0.95, // 0 = dielectric, 1 = conductor
	}

	// Car paint: a clearcoat layer over a coloured base.
	scene.StandardMaterial{
	    Color:     "#8f1d2c",
	    Roughness: 0.35,
	    Metalness: 0.1,
	    Clearcoat: 0.9,
	}

	// Brushed steel with texture maps.
	scene.StandardMaterial{
	    Color:        "#9BA0A8",
	    Roughness:    0.4,
	    Metalness:    0.9,
	    RoughnessMap: "/textures/brushed-roughness.png",
	    NormalMap:    "/textures/brushed-normal.png",
	}

	// Frosted glass: transmission plus roughness.
	scene.StandardMaterial{
	    Color:        "#aaddff",
	    Roughness:    0.25,
	    Transmission: 0.85,
	    Opacity:      scene.Float(0.6),
	    BlendMode:    scene.BlendAlpha,
	}

	// Soap bubble: thin-film iridescence.
	scene.StandardMaterial{
	    Color:       "#ffffff",
	    Roughness:   0.05,
	    Iridescence: 1.0,
	}`)}
			<h3>Style presets</h3>
			<p>
				Five presets share the
				<span class="inline-code">scene.MaterialStyle</span>
				field set: Color, Texture, Opacity, Emissive, BlendMode, RenderPass, and Wireframe. Use
				<span class="inline-code">scene.Float</span>
				and
				<span class="inline-code">scene.Bool</span>
				for the pointer fields.
			</p>
			{CodeBlock("go", `scene.FlatMaterial{Color: "#E8E8E8"}                             // unlit solid
	scene.GhostMaterial{Color: "#D4AF37", Opacity: scene.Float(0.3)} // transparent
	scene.GlassMaterial{Color: "#aaddff"}                            // refractive
	scene.GlowMaterial{Color: "#fff1d6", Emissive: scene.Float(1)}   // additive
	scene.MatteMaterial{Color: "#1a1a18"}                            // Lambertian`)}
			<h3>Line materials</h3>
			<p>
				<span class="inline-code">LineBasicMaterial</span>
				and
				<span class="inline-code">LineDashedMaterial</span>
				embed
				<span class="inline-code">MaterialStyle</span>
				, so a composite literal needs the embedded field by name. The capability verdict reports that neither GPU backend faithfully renders dashed-line styling; Canvas2D owns the actual dash pattern.
			</p>
			{CodeBlock("go", `scene.LineBasicMaterial{
	    MaterialStyle: scene.MaterialStyle{Color: "#8ecfff"},
	    Width:         3,
	}

	scene.LineDashedMaterial{
	    MaterialStyle: scene.MaterialStyle{Color: "#8ecfff"},
	    Width:         2,
	    DashSize:      0.4,
	    GapSize:       0.2,
	}`)}
			<h3>Render passes and blending</h3>
			<p>
				<span class="inline-code">BlendMode</span>
				selects the blend equation:
				<span class="inline-code">BlendOpaque</span>
				,
				<span class="inline-code">BlendAlpha</span>
				, or
				<span class="inline-code">BlendAdditive</span>
				.
				<span class="inline-code">RenderPass</span>
				selects the draw bucket:
				<span class="inline-code">RenderOpaque</span>
				,
				<span class="inline-code">RenderAlpha</span>
				, or
				<span class="inline-code">RenderAdditive</span>
				. Set both when you want an additive glow that still sorts correctly.
			</p>
			<h3>Animate a material uniform</h3>
			<p>
				<span class="inline-code">Mesh.MaterialAnims</span>
				drives one material uniform per entry. Each entry either carries keyframes, or replaces them with a spring or an oscillator. Material tracks ship in their own wire program, so they route independently from transform motion.
			</p>
			{CodeBlock("go", `scene.Mesh{
	    Geometry: scene.SphereGeometry{Radius: 1, Segments: 48},
	    Material: scene.StandardMaterial{Color: "#d4af37", Emissive: 0.2},
	    MaterialAnims: []scene.MaterialUniformAnim{
	        {
	            Uniform: "emissive",
	            Arity:   1,
	            Oscillator: &scene.MaterialOscillatorAnim{
	                Base:      []float64{0.4},
	                Amplitude: []float64{0.3},
	                Freq:      []float64{0.8},
	            },
	        },
	        {
	            Uniform: "roughness",
	            Arity:   1,
	            Spring: &scene.MaterialSpringAnim{
	                From: 0.8, To: 0.15, Stiffness: 120, Damping: 14,
	            },
	        },
	    },
	}`)}
		</section>
		<section id="selena">
			<h2>Selena Shaders</h2>
			<p>
				Selena is the default shader authoring language for Scene3D. You write one
				<span class="inline-code">.sel</span>
				file. The compiler then emits three artifacts:
			</p>
			<ul>
				<li>
					WebGPU Shading Language (WGSL) for the WebGPU path;
				</li>
				<li>
					OpenGL Shading Language (GLSL) for the WebGL path;
				</li>
				<li>
					a backend-agnostic binding descriptor that wires uniforms and textures.
				</li>
			</ul>
			<p>
				Five entry points exist, one per surface kind.
			</p>
			<ul>
				<li>
					<span class="inline-code">scene.CompileSelenaMaterial</span>
					— a mesh surface. Attach the result as a
					<span class="inline-code">Mesh.Material</span>
					.
				</li>
				<li>
					<span class="inline-code">scene.CompileSelenaPoints</span>
					— a points layer. Attach it as
					<span class="inline-code">Points.Material</span>
					.
				</li>
				<li>
					<span class="inline-code">scene.CompileSelenaParticleRender</span>
					— the draw pass of a compute particle system.
				</li>
				<li>
					<span class="inline-code">scene.CompileSelenaPost</span>
					— a full-screen post pass. Attach it to a
					<span class="inline-code">scene.CustomPost</span>
					effect.
				</li>
				<li>
					<span class="inline-code">scene.CompileSelenaBundle</span>
					— many named materials from one source, parsed once.
				</li>
			</ul>
			{CodeBlock("go", `source, err := os.ReadFile("materials/glow.sel")
	if err != nil {
	    return err
	}
	material, layout, err := scene.CompileSelenaMaterial(source, scene.SelenaMaterialOptions{
	    Material: "glow",
	    Standard: scene.StandardMaterial{Color: "#d4af37", Roughness: 0.2},
	    Uniforms: map[string]any{"pulse": 0.4, "tint": []float64{1, 0.85, 0.4}},
	})
	if err != nil {
	    return err
	}
	_ = layout // the binding layout, for tooling and validation

	mesh := scene.Mesh{
	    Geometry: scene.SphereGeometry{Radius: 1, Segments: 48},
	    Material: material,
	}`)}
			<p>
				<span class="inline-code">scene.SelenaUniforms</span>
				converts a typed Go struct into the host uniform map. A shader parameter rename then fails at the call site instead of doing nothing in silence. Field names resolve in order: the
				<span class="inline-code">selena</span>
				struct tag, then the
				<span class="inline-code">json</span>
				struct tag, then a lower-camel form of the Go field name.
			</p>
			{CodeBlock("go", `type GlowUniforms struct {
	    Pulse float64   // resolves to "pulse"
	    Tint  []float64 // resolves to "tint"
	}

	uniforms, err := scene.SelenaUniforms(GlowUniforms{
	    Pulse: 0.4,
	    Tint:  []float64{1, 0.85, 0.4},
	})
	if err != nil {
	    return err
	}
	opts := scene.SelenaMaterialOptions{Material: "glow", Uniforms: uniforms}`)}
			<p>
				<span class="inline-code">scene.CustomMaterial</span>
				is the transport underneath. It embeds
				<span class="inline-code">StandardMaterial</span>
				and adds the shader payload, so you can hand-write WGSL and GLSL when you do not want Selena.
			</p>
			{CodeBlock("go", `scene.CustomMaterial{
	    StandardMaterial: scene.StandardMaterial{Color: "#8ecfff"},
	    ShaderBackend:    "selena",
	    VertexWGSL:       vertexWGSL,
	    FragmentWGSL:     fragmentWGSL,
	    VertexGLSL:       vertexGLSL,
	    FragmentGLSL:     fragmentGLSL,
	    Uniforms:         map[string]any{"pulse": 0.4},
	}`)}
			<p>
				A custom material narrows the capability verdict. The resolver reads which shader sources the material actually carries. WGSL only serves WebGPU. GLSL only serves WebGL. Ship both, or accept that one backend drops out.
			</p>
		</section>
		<section id="lights">
			<h2>Lights</h2>
			<p>
				Seven light types exist, and all seven now render on WebGPU. Until recently the WebGPU backend honoured ambient and directional only; spot, hemisphere, rect-area, and light-probe all drew as point lights. That is fixed.
			</p>
			<p>
				The old hard cap of 8 lights is also gone. The light storage buffer starts at 8 entries and doubles on demand up to 256. Past 256 the runtime reports the overflow instead of dropping lights in silence.
			</p>
			{CodeBlock("go", `// Ambient — flat fill, no direction.
	scene.AmbientLight{Color: "#ffffff", Intensity: 0.15}

	// Directional — parallel rays from infinity. Casts shadows.
	scene.DirectionalLight{
	    Color:          "#fff1d6",
	    Intensity:      1.2,
	    Direction:      scene.Vec3(0.3, -1.0, -0.5),
	    CastShadow:     true,
	    ShadowBias:     -0.001,
	    ShadowSize:     2048,
	    ShadowCascades: 3,
	    ShadowSoftness: 1.5,
	}

	// Point — omnidirectional with distance falloff.
	scene.PointLight{
	    Color:     "#D4AF37",
	    Intensity: 0.8,
	    Position:  scene.Vec3(-3, 4, 2),
	    Range:     20,
	    Decay:     2,
	}

	// Spot — a cone with a soft edge.
	scene.SpotLight{
	    Color:     "#ffffff",
	    Intensity: 1.5,
	    Position:  scene.Vec3(0, 6, 0),
	    Direction: scene.Vec3(0, -1, 0),
	    Angle:     0.35, // outer cone, radians
	    Penumbra:  0.2,  // 0 = hard edge, 1 = fully soft
	    Range:     30,
	    Decay:     2,
	}

	// Hemisphere — a sky and ground gradient.
	scene.HemisphereLight{
	    SkyColor:    "#87ceeb",
	    GroundColor: "#2d4a1e",
	    Intensity:   0.4,
	}

	// Rect-area — a rectangular emitter.
	scene.RectAreaLight{
	    Color:     "#ffffff",
	    Intensity: 3,
	    Position:  scene.Vec3(0, 3, 2),
	    Direction: scene.Vec3(0, -0.3, -1),
	    Width:     4,
	    Height:    2,
	}

	// Light probe — an ambient probe.
	scene.LightProbe{
	    Color:     "#cfe6ff",
	    Intensity: 0.5,
	}`)}
			<h3>Two honest shortfalls remain</h3>
			<p>
				Both shortfalls report themselves through the capability system, so tooling sees them.
			</p>
			<ul>
				<li>
					<strong>Rect-area specular.</strong>
					The diffuse half is exact: the WebGPU shader evaluates the analytic polygon form factor over the four world corners. The specular half substitutes a representative-point lobe for the fitted linearly transformed cosine (LTC) tables. Energy lands in roughly the right place. The highlight shape is wrong on glossy surfaces. On WebGL2 a rect-area light has no shape at all; it draws as a point light, and Width and Height stop at the IR.
				</li>
				<li>
					<strong>Light-probe spherical harmonics.</strong>
					<span class="inline-code">LightProbe.Coefficients</span>
					reaches the IR, and then no renderer reads it. Both GPU backends fold a probe into a flat ambient term built from Color and Intensity. Ambient is the right fold — a probe carries no position, so a point light would invent a falloff — but it is not a spherical-harmonic evaluation.
				</li>
			</ul>
			<p>
				One more gap carries no capability flag yet: a spot light casts no shadow on either backend. Both shadow passes skip any light whose kind is not
				<span class="inline-code">directional</span>
				, even though
				<span class="inline-code">SpotLight</span>
				accepts
				<span class="inline-code">CastShadow</span>
				,
				<span class="inline-code">ShadowBias</span>
				, and
				<span class="inline-code">ShadowSize</span>
				.
			</p>
		</section>
		<section id="shadows">
			<h2>Shadows</h2>
			<p>
				<span class="inline-code">Props.Shadows</span>
				caps how many pixels each shadow map may allocate. The zero value applies the safe default of 1024 by 1024, which is 1,048,576 pixels. A light that asks for more gets scaled down uniformly.
			</p>
			<p>
				The cap matters for memory. A light that requests 4096 with the default cap gets a 1024 map. Per-light depth memory drops from about 64 MB to about 4 MB.
			</p>
			{CodeBlock("go", `props.Shadows = scene.Shadows{MaxPixels: scene.ShadowMaxPixels1024}

	// Presets: ShadowMaxPixels512, ShadowMaxPixels1024 (default),
	// ShadowMaxPixels2048, ShadowMaxPixels4096.
	// Opt out with scene.ShadowMaxPixelsUnbounded.`)}
			<p>
				A mesh opts in per direction. Set
				<span class="inline-code">CastShadow</span>
				to write into the map and
				<span class="inline-code">ReceiveShadow</span>
				to sample it. A ground plane usually receives without casting.
			</p>
			<div class="scene3d-warning" role="note">
				<p class="scene3d-warning__title">
					Two shadow slots, directional lights only.
				</p>
				<p>
					Both GPU backends allocate at most two shadow maps per scene, and both skip any light that is not a
					<span class="inline-code">DirectionalLight</span>
					. A third shadow-casting directional light is ignored. A shadow-casting spot light is ignored.
				</p>
			</div>
		</section>
		<section id="post-processing">
			<h2>Post-processing</h2>
			<p>
				<span class="inline-code">Props.PostFX</span>
				is an ordered chain. An empty chain renders straight to the canvas with no offscreen buffer. One or more effects switch the scene to a high dynamic range (HDR) offscreen target. The chain then ping-pongs between framebuffers, and the last pass blits to the canvas.
			</p>
			<p>Eight effect types exist.</p>
			<ul>
				<li>
					<span class="inline-code">Tonemap</span>
					— Mode selects
					<span class="inline-code">TonemapACES</span>
					,
					<span class="inline-code">TonemapReinhard</span>
					, or
					<span class="inline-code">TonemapFilmic</span>
					. Exposure multiplies before the curve.
				</li>
				<li>
					<span class="inline-code">Bloom</span>
					— Threshold, Strength, Radius, and Scale. Scale is an extra internal downscale on top of the chain cap. Bloom is a low-frequency blur, so 0.25 costs a quarter of the pixels and loses nothing visible.
				</li>
				<li>
					<span class="inline-code">Vignette</span>
					— Intensity darkens the edges.
				</li>
				<li>
					<span class="inline-code">ColorGrade</span>
					— Exposure, Contrast, Saturation.
				</li>
				<li>
					<span class="inline-code">SSAO</span>
					— screen-space ambient occlusion. Radius, Intensity, Bias.
				</li>
				<li>
					<span class="inline-code">DOF</span>
					— depth of field. FocusDistance, Aperture, MaxBlur.
				</li>
				<li>
					<span class="inline-code">CustomPost</span>
					— a Selena-authored pass. Stage selects
					<span class="inline-code">beforeTonemap</span>
					(the default) or
					<span class="inline-code">afterTonemap</span>
					. A shader that fails validation becomes an identity pass instead of killing the frame.
				</li>
				<li>
					<span class="inline-code">FXAA</span>
					— fast approximate anti-aliasing. No tunable fields. Place it last.
				</li>
			</ul>
			<div class="scene3d-warning" role="note">
				<p class="scene3d-warning__title">
					Post-processing defeats hardware multisample anti-aliasing (MSAA).
				</p>
				<p>
					Once any effect is present, the scene renders into an offscreen buffer, and
					<span class="inline-code">Props.MSAASamples</span>
					no longer smooths the presented image. Add
					<span class="inline-code">FXAA</span>
					at the end of the chain to get edge smoothing back.
				</p>
			</div>
			{CodeBlock("go", `props.PostFX = scene.PostFX{
	    MaxPixels: scene.PostFXMaxPixels1080p,
	    Effects: []scene.PostEffect{
	        scene.SSAO{Radius: 4, Intensity: 0.55},
	        scene.Bloom{Threshold: 0.8, Strength: 0.5, Radius: 6, Scale: 0.25},
	        scene.Tonemap{Mode: scene.TonemapACES, Exposure: 1.1},
	        scene.ColorGrade{Contrast: 1.05, Saturation: 1.1},
	        scene.Vignette{Intensity: 0.4},
	        scene.FXAA{}, // always last: it searches tonemapped luma
	    },
	}

	// A ready-made 60 fps chain: half-resolution bloom, ACES, FXAA, 720p cap.
	props.PostFX = scene.GameplayPostFX()`)}
			<p>
				<span class="inline-code">MaxPixels</span>
				caps the offscreen pipeline by total backing pixels after the device pixel ratio. The zero value applies the 1080p default, which is 2,073,600 pixels. Presets run
				<span class="inline-code">PostFXMaxPixels540p</span>
				,
				<span class="inline-code">720p</span>
				,
				<span class="inline-code">1080p</span>
				,
				<span class="inline-code">1440p</span>
				, and
				<span class="inline-code">4K</span>
				. Use
				<span class="inline-code">PostFXMaxPixelsUnbounded</span>
				to opt out. Memory then scales with the display, so measure before you do.
			</p>
			<p>
				<span class="inline-code">DeferPostFX</span>
				and
				<span class="inline-code">DeferPostFXDelayMS</span>
				delay chain setup past first paint, which keeps the largest contentful paint (LCP) clean on a heavy page.
			</p>
		</section>
		<section id="animation">
			<h2>Animation</h2>
			<p>
				Three animation paths exist. Pick by what drives the motion.
			</p>
			<h3>Procedural motion on a node</h3>
			<p>
				<span class="inline-code">Spin</span>
				adds constant angular velocity.
				<span class="inline-code">Drift</span>
				adds sinusoidal translation along an axis, with
				<span class="inline-code">DriftSpeed</span>
				in cycles per second and
				<span class="inline-code">DriftPhase</span>
				as an offset from 0 to 1.
			</p>
			{CodeBlock("go", `scene.Mesh{
	    Geometry: scene.TorusGeometry{Radius: 2.5, Tube: 0.08, RadialSegments: 64},
	    Material: scene.StandardMaterial{Color: "#D4AF37", Roughness: 0.2, Metalness: 0.9},
	    Spin:       scene.Rotate(0, 0.003, 0),
	    Drift:      scene.Vec3(0, 0.4, 0),
	    DriftSpeed: 1.0,
	    DriftPhase: 0.5,
	}`)}
			<h3>Keyframe clips</h3>
			<p>
				<span class="inline-code">scene.AnimationClip</span>
				carries named channels. Each channel targets a node index and one property:
				<span class="inline-code">translation</span>
				,
				<span class="inline-code">rotation</span>
				, or
				<span class="inline-code">scale</span>
				. Rotation values are quaternions, so four floats per key. Interpolation is
				<span class="inline-code">LINEAR</span>
				or
				<span class="inline-code">STEP</span>
				.
			</p>
			{CodeBlock("go", `scene.AnimationClip{
	    Name:     "hover",
	    Duration: 2.0,
	    Channels: []scene.AnimationChannel{{
	        TargetNode:    0,
	        Property:      "translation",
	        Interpolation: "LINEAR",
	        Times:         []float64{0, 1, 2},
	        Values:        []float64{0, 0, 0, 0, 0.5, 0, 0, 0, 0},
	    }},
	}`)}
			<h3>Skeletal animation from glTF</h3>
			<p>
				<span class="inline-code">scene.Model</span>
				plays a clip embedded in the asset by name. Skinning runs on both GPU backends and is a required feature, so a skinned scene never falls back to canvas2d.
			</p>
			{CodeBlock("go", `scene.Model{
	    Src:                "/models/character.glb",
	    Animation:          "Run",
	    Loop:               scene.Bool(true),
	    AnimationSpeed:     scene.Float(1.2),
	    AnimationWeight:    scene.Float(1.0),
	    AnimationFadeInMS:  scene.Int(180),
	    AnimationFadeOutMS: scene.Int(180),
	}`)}
			<p>
				Two capabilities are absent, and a character pipeline usually wants both. There are no morph targets, and there is no blend tree. Cross-fade between two clips with the fade fields, or sequence clips with
				<span class="inline-code">AnimationSeq</span>
				.
			</p>
		</section>
		<section id="transitions">
			<h2>Transitions and Live Updates</h2>
			<p>
				Most node types carry a
				<span class="inline-code">Transition</span>
				, an
				<span class="inline-code">InState</span>
				, an
				<span class="inline-code">OutState</span>
				, and a
				<span class="inline-code">Live</span>
				list.
				<span class="inline-code">InState</span>
				and
				<span class="inline-code">OutState</span>
				are partial prop bags of pointer fields, so an unset field means "do not change this".
			</p>
			{CodeBlock("go", `scene.Mesh{
	    ID:       "featured",
	    Geometry: scene.CubeGeometry{Size: 1},
	    Material: scene.StandardMaterial{Color: "#d4af37"},
	    Transition: scene.Transition{
	        In:     scene.TransitionTiming{Duration: 400 * time.Millisecond, Easing: scene.EaseOut},
	        Out:    scene.TransitionTiming{Duration: 250 * time.Millisecond, Easing: scene.EaseIn},
	        Update: scene.TransitionTiming{Duration: 300 * time.Millisecond, Easing: scene.EaseInOut},
	    },
	    InState:  &scene.MeshProps{Opacity: scene.Float(0), Scale: &smallScale},
	    OutState: &scene.MeshProps{Opacity: scene.Float(0)},
	    Live:     scene.Live("inventory:update"),
	}`)}
			<p>
				<span class="inline-code">Live</span>
				names the hub events that should re-read this node. Combine it with a hub binding in the loader, and the node updates without a page navigation.
			</p>
		</section>
		<section id="css-scene-state">
			<h2>CSS-driven Scene State</h2>
			<p>
				A Scene3D field can read a CSS custom property. Write
				<span class="inline-code">var(--scene-core-roughness, 0.4)</span>
				as the value. The client planner then reads the computed style, applies the resolved number to a cloned IR, and passes ordinary data to the renderer. A class change, a media query, or a
				<span class="inline-code">prefers-color-scheme</span>
				switch drives scene state with no authored JavaScript.
			</p>
			<p>
				The planner reads a fixed key list per record type, batched in one computed-style pass per frame.
			</p>
			<ul>
				<li>
					<strong>environment</strong>
					— ambientColor, ambientIntensity, skyColor, skyIntensity, groundColor, groundIntensity, exposure, fogColor, fogDensity.
				</li>
				<li>
					<strong>materials</strong>
					— color, opacity, emissive, roughness, metalness, clearcoat, sheen, transmission, iridescence, anisotropy, and the four texture maps.
				</li>
				<li>
					<strong>lights</strong>
					— color, groundColor, intensity, position, direction, angle, penumbra, range, decay, width, height, shadowBias, shadowSize.
				</li>
				<li>
					<strong>objects</strong>
					— the material keys plus lineWidth, position, rotation, and spin.
				</li>
				<li>
					<strong>points</strong>
					— color, size, opacity, position, rotation, spin.
				</li>
				<li>
					<strong>instancedMeshes</strong>
					— color, roughness, metalness, width, height, depth, radius.
				</li>
				<li>
					<strong>labels and sprites</strong>
					— color, background, borderColor, offsets, opacity, width, height, scale.
				</li>
				<li>
					<strong>post effects</strong>
					— threshold, intensity, radius, scale, bias, saturation, contrast, exposure, focusDistance, aperture, maxBlur.
				</li>
				<li>
					<strong>compute particles</strong>
					— the emitter transform, radius, rate, lifetime, arms, wind, scatter, and the particle material colours and sizes.
				</li>
			</ul>
			<p>
				The planner also interpolates. A custom property change reaches a record that carries an
				<span class="inline-code">Update</span>
				transition. The planner then animates from the old resolved value to the new one, over that duration and easing. It does not slam the value.
			</p>
			<p>
				Typed Go fields such as
				<span class="inline-code">StandardMaterial.Roughness</span>
				are
				<span class="inline-code">float64</span>
				, so they cannot hold the string
				<span class="inline-code">var(--x)</span>
				. Author CSS-driven values through the composable Scene3D elements, whose attributes are strings.
			</p>
			{CodeBlock("gosx", `<Scene3D ariaLabel="Galaxy" background="var(--galaxy-bg)">
	  <Environment ambientColor="var(--galaxy-ambient)" fogDensity="var(--galaxy-fog-density)" />
	  <Material name="core" color="var(--galaxy-core-inner)" roughness="var(--scene-core-roughness, 0.4)" />
	  <Mesh material="core" kind="sphere" radius="1.4" />
	</Scene3D>`)}
			{CodeBlock("bash", `/* The scene follows the theme with no scene JavaScript. */
	.galaxy { --galaxy-core-inner: #5eead4; --scene-core-roughness: 0.30; }
	.galaxy.is-hot { --galaxy-core-inner: #ff8a5b; --scene-core-roughness: 0.12; }
	@media (prefers-color-scheme: light) { .galaxy { --galaxy-ambient: #ffffff; } }`)}
			<p>
				three.js has no equivalent. This is a framework-level feature, not a library feature: it needs the same runtime to own both the document styles and the scene state.
			</p>
		</section>
		<section id="instancing">
			<h2>Instancing and Level of Detail</h2>
			<p>
				<span class="inline-code">scene.InstancedMesh</span>
				draws N copies of one geometry in one draw call. WebGPU uses instance-rate vertex buffers for the transform and colour streams. WebGL2 uses the matching instanced draw path. Both share the same IR, pass ordering, and shadow flags.
			</p>
			{CodeBlock("go", `positions := make([]scene.Vector3, 500)
	for i := range positions {
	    positions[i] = scene.Vec3((rand.Float64()-0.5)*20, 0, (rand.Float64()-0.5)*20)
	}

	scene.InstancedMesh{
	    ID:            "grass",
	    Count:         500,
	    Geometry:      scene.CylinderGeometry{RadiusTop: 0.05, RadiusBottom: 0.05, Height: 2, Segments: 6},
	    Material:      scene.StandardMaterial{Color: "#2d4a1e", Roughness: 0.9},
	    Positions:     positions,
	    CastShadow:    true,
	    ReceiveShadow: true,
	}`)}
			<p>
				Rotations, Scales, and Colors are optional. Omit them and every instance takes the identity rotation, unit scale, and the material colour.
				<span class="inline-code">Attributes</span>
				carries extra named per-instance float streams for a custom shader.
			</p>
			<h3>GPU instance culling</h3>
			<p>
				Set
				<span class="inline-code">CullKernelWGSL</span>
				,
				<span class="inline-code">CullKernelEntry</span>
				, and
				<span class="inline-code">CullRadius</span>
				and the WebGPU runtime runs a compute pass to cull instances before drawing. The fields are additive: leave them empty and the payload is byte-identical to a plain instanced mesh. Culling is WebGPU only, and the verdict says so.
			</p>
			<h3>Instanced glTF</h3>
			<p>
				<span class="inline-code">scene.InstancedGLBMesh</span>
				loads one binary glTF file once, extracts its geometry, and draws every instance through the instanced path. Each
				<span class="inline-code">MeshInstance</span>
				carries its own position, rotation, and leaf scale.
			</p>
			{CodeBlock("go", `scene.InstancedGLBMesh{
	    ID:  "trees",
	    Src: "/models/pine.glb",
	    Instances: []scene.MeshInstance{
	        {ID: "pine-0", Position: scene.Vec3(0, 0, 0), Scale: scene.Vec3(1, 1, 1)},
	        {ID: "pine-1", Position: scene.Vec3(6, 0, -3), Scale: scene.Vec3(1.4, 1.6, 1.4)},
	    },
	    Static: scene.Bool(true),
	}`)}
			<h3>Discrete level of detail</h3>
			<p>
				<span class="inline-code">scene.LODGroup</span>
				swaps whole authored nodes by camera distance. Each
				<span class="inline-code">LODLevel.Distance</span>
				is the minimum distance at which that level becomes active, and the next level's distance ends it.
			</p>
			{CodeBlock("go", `scene.LODGroup{
	    ID:       "statue",
	    Position: scene.Vec3(0, 0, 0),
	    Levels: []scene.LODLevel{
	        {Distance: 0, Node: scene.Model{Src: "/models/statue-high.glb"}},
	        {Distance: 25, Node: scene.Model{Src: "/models/statue-low.glb"}},
	        {Distance: 80, Node: scene.Mesh{
	            Geometry: scene.BoxGeometry{Width: 1, Height: 3, Depth: 1},
	            Material: scene.MatteMaterial{Color: "#3a3a38"},
	        }},
	    },
	}`)}
			<p>
				<span class="inline-code">LODGroup</span>
				swaps authored geometry.
				<span class="inline-code">Compression.LOD</span>
				swaps vertex payload precision instead. The two compose.
			</p>
		</section>
		<section id="particles">
			<h2>Points and Compute Particles</h2>
			<p>
				Two particle surfaces exist. Choose by who moves the particles.
			</p>
			<h3>Points</h3>
			<p>
				<span class="inline-code">scene.Points</span>
				draws a static or server-updated point cloud as billboards. You supply the positions. Per-particle sizes and colours are optional.
				<span class="inline-code">MinPixelSize</span>
				and
				<span class="inline-code">MaxPixelSize</span>
				clamp the screen-space footprint so distant points stay visible and near points stay sane.
			</p>
			{CodeBlock("go", `scene.Points{
	    ID:           "stars",
	    Count:        20000,
	    Positions:    starPositions,
	    Color:        "#dbeafe",
	    Style:        scene.PointStyleGlow,
	    Size:         0.04,
	    MinPixelSize: 1.5,
	    MaxPixelSize: 6,
	    Opacity:      0.9,
	    BlendMode:    scene.BlendAdditive,
	    Attenuation:  true,
	}`)}
			<h3>Compute particles</h3>
			<p>
				<span class="inline-code">scene.ComputeParticles</span>
				simulates on the GPU. The emitter, the force list, and the material are declarative; a WebGPU compute kernel integrates them. This is a WebGPU-only feature. A WebGL2 page keeps the scene but reports the degradation through the verdict, so gate large counts or profile the fallback.
			</p>
			{CodeBlock("go", `scene.ComputeParticles{
	    ID:    "embers",
	    Count: 5000,
	    Emitter: scene.ParticleEmitter{
	        Kind:     "sphere", // "point", "sphere", "disc", "spiral"
	        Position: scene.Vec3(0, 2, 0),
	        Radius:   5,
	        Rate:     500, // particles per second
	        Lifetime: 4,   // seconds
	        Scatter:  0.3,
	    },
	    Forces: []scene.ParticleForce{
	        {Kind: "gravity", Strength: 0.5, Direction: scene.Vec3(0, -1, 0)},
	        {Kind: "turbulence", Strength: 0.3, Frequency: 1.2},
	        {Kind: "orbit", Strength: 0.2},
	    },
	    Material: scene.ParticleMaterial{
	        Color:       "#D4AF37",
	        ColorEnd:    "#ffffff",
	        Size:        0.05,
	        SizeEnd:     0.01,
	        Opacity:     0.9,
	        OpacityEnd:  0.0,
	        BlendMode:   scene.BlendAdditive,
	        Attenuation: true,
	    },
	    Bounds: 12,
	}`)}
			<p>
				Force kinds are
				<span class="inline-code">gravity</span>
				,
				<span class="inline-code">wind</span>
				,
				<span class="inline-code">turbulence</span>
				,
				<span class="inline-code">orbit</span>
				,
				<span class="inline-code">drag</span>
				, and
				<span class="inline-code">radial</span>
				.
			</p>
			<p>
				Replace the built-in kernel with
				<span class="inline-code">ComputeWGSL</span>
				and
				<span class="inline-code">ComputeEntry</span>
				. Your kernel must match the four-binding contract: particles as read-write storage, render data as read-write storage, params as a uniform, and forces as read-only storage. Replace the draw pass with
				<span class="inline-code">RenderMaterial</span>
				, a Selena material of kind
				<span class="inline-code">particle-render</span>
				.
			</p>
		</section>
		<section id="water">
			<h2>Water Simulation</h2>
			<p>
				<span class="inline-code">scene.WaterSystem</span>
				is a GPU heightfield water simulation with pool geometry, caustics, reflection, refraction, and floating-object displacement. It runs on WebGPU first and on WebGL2 as an honest fallback. Both cells are true in the capability matrix, and water is a required feature, so a water scene never degrades to canvas2d.
			</p>
			<p>
				Five feedback compute kernels drive the surface: seed, drop, displacement, simulation, and normal. Each one is authored in Selena and compiled to WGSL, GLSL, and OpenGL ES Shading Language.
			</p>
			{CodeBlock("go", `scene.WaterSystem{
	    ID:         "pool",
	    Resolution: 512, // simulation grid along one axis

	    // Surface topology is independent from the simulation grid, so you can
	    // match a reference mesh budget without paying for it in compute state.
	    SurfaceResolution: 256,

	    PoolShape:    "rounded",
	    PoolWidth:    12,
	    PoolHeight:   4,
	    PoolLength:   12,
	    CornerRadius: 1.5,

	    WaveSpeed:   1.0,
	    Damping:     0.995,
	    NormalScale: 1.2,

	    SeedDrops:    6,
	    DropRadius:   0.06,
	    DropStrength: 0.9,

	    ShallowColor:    "#7fd4e8",
	    DeepColor:       "#0b3a4a",
	    AboveWaterColor: scene.Vec3(1.2, 1.05, 0.95), // linear HDR, may exceed 1
	    CubeMap:         "/textures/sky-cube.ktx2",
	    TileTexture:     "/textures/pool-tile.png",

	    Caustics:           true,
	    Reflection:         true,
	    Refraction:         true,
	    CausticsResolution: 512,
	    LightDirection:     scene.Vec3(0.3, -1, -0.4),
	}`)}
			<p>
				A floating object displaces the surface. Set
				<span class="inline-code">ObjectKind</span>
				and the object transform fields, then use
				<span class="inline-code">ObjectDisplacementSpheres</span>
				to approximate a compound shape with up to a few dozen proxy spheres.
				<span class="inline-code">ObjectDisplacementEvents</span>
				records one-shot motion segments so a splash stays correct across a frame boundary.
			</p>
			<p>
				<span class="inline-code">ObjectTextureResolutionMode</span>
				and
				<span class="inline-code">ObjectTexturePixelBudget</span>
				cap the object render-to-texture passes, and
				<span class="inline-code">Paused</span>
				and
				<span class="inline-code">FollowCamera</span>
				control the simulation loop.
			</p>
		</section>
		<section id="overlays">
			<h2>Labels, Sprites, and HTML</h2>
			<p>
				Four node types project document content from world space. All four resolve their screen position from a target object or an explicit position, and all four can occlude behind geometry.
			</p>
			<h3>Label</h3>
			<p>
				<span class="inline-code">scene.Label</span>
				projects a styled text box. It runs through the GoSX text layout engine, so it honours
				<span class="inline-code">MaxWidth</span>
				,
				<span class="inline-code">MaxLines</span>
				,
				<span class="inline-code">Overflow</span>
				,
				<span class="inline-code">WhiteSpace</span>
				, and
				<span class="inline-code">TextAlign</span>
				.
				<span class="inline-code">Collision</span>
				declares what happens when two labels overlap, and
				<span class="inline-code">Priority</span>
				breaks the tie.
			</p>
			{CodeBlock("go", `scene.Label{
	    ID:        "hotspot-1",
	    Target:    "engine-block",
	    Text:      "Cast aluminium block",
	    MaxWidth:  220,
	    MaxLines:  3,
	    Overflow:  "ellipsis",
	    Collision: "hide",
	    Priority:  10,
	    Occlude:   true,
	    OffsetY:   -12,
	    AnchorX:   0.5,
	    AnchorY:   1,
	    ClassName: "hotspot-label",
	}`)}
			<h3>Sprite</h3>
			<p>
				<span class="inline-code">scene.Sprite</span>
				projects an image billboard.
				<span class="inline-code">Fit</span>
				controls how the image fills its box.
			</p>
			<h3>HTML and HTMLSurface</h3>
			<p>
				<span class="inline-code">scene.HTML</span>
				has three modes.
				<span class="inline-code">HTMLDOM</span>
				projects real document nodes over the canvas.
				<span class="inline-code">HTMLPortal</span>
				moves an existing element into the projection.
				<span class="inline-code">HTMLTexture</span>
				asks for a rasterized surface inside the 3D scene.
			</p>
			<p>
				Be honest about texture mode.
				<span class="inline-code">scene.HTMLSurface</span>
				lowers to an explicit DOM overlay fallback today. It records the mode and the fallback reason in the IR, so tooling can see the degradation. The native texture manager does not exist yet.
			</p>
			<p>
				<span class="inline-code">MaxTexturePixels</span>
				caps each surface. The default is 1024 by 1024, which keeps one RGBA8 surface near 4 MB. Presets run
				<span class="inline-code">HTMLTextureMaxPixels512</span>
				,
				<span class="inline-code">1024</span>
				, and
				<span class="inline-code">2048</span>
				, with
				<span class="inline-code">HTMLTextureMaxPixelsUnbounded</span>
				to opt out.
			</p>
			{CodeBlock("go", `scene.HTML{
	    ID:            "spec-card",
	    Target:        "engine-block",
	    Mode:          scene.HTMLDOM,
	    Markup:        "<h3>V8</h3><p>6.2 litre</p>",
	    ClassName:     "spec-card",
	    PointerEvents: "auto",
	    Occlude:       true,
	    Scale:         1,
	    Opacity:       0.95,
	}`)}
		</section>
		<section id="gltf">
			<h2>glTF Loading</h2>
			<p>
				<span class="inline-code">scene.Model</span>
				instances a glTF 2.0 asset. The loader reads binary glTF and JSON glTF, embedded textures included.
				<span class="inline-code">Fit</span>
				,
				<span class="inline-code">FitAlign</span>
				, and
				<span class="inline-code">Bounds</span>
				normalize an asset into a target box, which saves guessing the author's unit scale.
			</p>
			{CodeBlock("go", `scene.Model{
	    ID:       "helmet",
	    Src:      "/models/helmet.glb",
	    Position: scene.Vec3(0, 1, 0),
	    Rotation: scene.Rotate(0, math.Pi, 0),
	    Scale:    scene.Vec3(1.2, 1.2, 1.2),
	    Fit:      "contain",
	    Bounds:   2.5,

	    // Override the asset material from Go — useful for theming shared assets.
	    Material: scene.StandardMaterial{Color: "#D4AF37", Roughness: 0.3, Metalness: 0.8},

	    CastShadow:    true,
	    ReceiveShadow: true,
	    Static:        scene.Bool(true),
	}`)}
			<h3>Nine material extensions parse</h3>
			<ul>
				<li>
					<span class="inline-code">KHR_materials_clearcoat</span>
					— maps to Clearcoat.
				</li>
				<li>
					<span class="inline-code">KHR_materials_transmission</span>
					— maps to Transmission.
				</li>
				<li>
					<span class="inline-code">KHR_materials_iridescence</span>
					— maps to Iridescence.
				</li>
				<li>
					<span class="inline-code">KHR_materials_sheen</span>
					— maps to Sheen.
					<strong>Lossy.</strong>
					The extension carries a sheen colour and a sheen roughness. Scene3D has one scalar, so the loader takes the colour peak and drops the hue and the roughness.
				</li>
				<li>
					<span class="inline-code">KHR_materials_anisotropy</span>
					— maps to Anisotropy.
					<strong>Lossy.</strong>
					The extension carries a strength and a rotation. Scene3D has one signed scalar, so the loader projects the rotation onto the tangent and bitangent pair. A rotation between the two axes loses its exact angle.
				</li>
				<li>
					<span class="inline-code">KHR_materials_emissive_strength</span>
					— maps to Emissive.
				</li>
				<li>
					<span class="inline-code">KHR_materials_unlit</span>
					— switches to the flat shading path. Both GPU backends read it.
				</li>
				<li>
					<span class="inline-code">KHR_materials_ior</span>
					—
					<strong>parsed but inert.</strong>
					The value reaches the material. The PBR shaders derive their base reflectance from a fixed 0.04, so nothing consumes it yet.
				</li>
				<li>
					<span class="inline-code">KHR_texture_transform</span>
					— bakes into the texture-coordinate buffer at load time.
				</li>
			</ul>
			<p>
				A required extension outside that list produces one console warning naming it, because a dropped required extension changes how the asset looks.
			</p>
			<h3>Geometry compression is not supported</h3>
			<div class="scene3d-warning" role="note">
				<p class="scene3d-warning__title">
					No Draco. No meshopt. No KTX2 or Basis transcoding.
				</p>
				<p>
					An asset that uses
					<span class="inline-code">KHR_draco_mesh_compression</span>
					or
					<span class="inline-code">EXT_meshopt_compression</span>
					now raises a named error at the point of use. It used to build a garbage mesh in silence. A texture that uses
					<span class="inline-code">KHR_texture_basisu</span>
					warns and renders without that map.
				</p>
				<p>
					Re-export the asset without compression, or run gltfpack without the
					<span class="inline-code">-cc</span>
					flag, until a decoder ships.
				</p>
			</div>
			<p>
				One more asymmetry worth knowing: the Go side never loads a glTF file. Ray queries against a
				<span class="inline-code">Model</span>
				therefore use a bounds box, not triangles. See
				<a href="#raycasting">Raycasting and Picking</a>
				.
			</p>
		</section>
		<section id="compression">
			<h2>Compression and Progressive Transport</h2>
			<p>
				<span class="inline-code">Props.Compression</span>
				quantizes bulk float arrays in the IR: point positions and sizes, instanced transforms, and animation keyframe times and values.
			</p>
			<p>
				The codec is per-chunk minimum and maximum scalar quantization with bit packing. Chunks hold up to 4096 floats. The only per-chunk metadata is two floats. It is
				<strong>not</strong>
				TurboQuant-backed, and it has no delta stage. Do not expect either.
			</p>
			<p>
				One detail earns its keep. Interleaved data deinterleaves per lane before quantizing, so a 4-by-4 matrix stream splits into 16 lanes and each lane gets its own range. Translation lanes then stop crushing scale lanes, which is exactly what a shared range does to an instanced transform buffer.
			</p>
			{CodeBlock("go", `props.Compression = &scene.Compression{
	    BitWidth: 12, // 1-8 for point clouds; 12 for transforms

	    // Progressive: ship a 2-bit preview beside the full payload. The client
	    // renders the preview at once and upgrades when the full data arrives.
	    Progressive:        true,
	    PreviewBitWidth:    2,
	    ProgressiveDelayMS: 400, // spend upgrade CPU after first paint

	    // LOD: keep both payloads and pick per object by camera distance.
	    LOD:          true,
	    LODThreshold: 20,
	}`)}
			<p>
				Use 12 bits for instanced transforms. That holds the error near 0.069 world units. At 8 bits a transform that spans a wide world range drifts visibly.
			</p>
			{CodeBlock("go", `// Compress one instanced mesh on its own, for a spread into a template.
	props := scene.CompressInstancedTransforms(mesh, 12)
	if props == nil {
	    props = mesh.SpreadProps() // fall back to raw transforms
	}`)}
		</section>
		<section id="raycasting">
			<h2>Raycasting and Picking</h2>
			<p>
				Scene3D answers ray queries in Go, on the same graph the renderer draws. Use it for hitscan weapons, editor picking, interaction probes, and tests.
			</p>
			<h3>Build an accelerator for many rays</h3>
			<p>
				<span class="inline-code">scene.NewSceneAccelerator</span>
				flattens the graph and builds a bounding-volume hierarchy (BVH). Build it once, then trace per ray. It holds a snapshot, so rebuild it after the graph changes.
			</p>
			<div class="scene3d-stat-row">
				<ui.StatCard Value="58,075 → 1,257 ns" Label="1,000-node graph, per ray" />
				<ui.StatCard Value="107,661 → 560 ns" Label="10,000-instance mesh, per ray" />
				<ui.StatCard Value="1 alloc" Label="per ray, either path" />
			</div>
			{CodeBlock("go", `accel := scene.NewSceneAccelerator(props.Graph, scene.PointThreshold(0.25))

	hit, ok := accel.Raycast(scene.Ray{
	    Origin:    scene.Vec3(0, 4, 12),
	    Direction: scene.Vec3(0, -0.3, -1),
	}, scene.PickableOnly(), scene.MaxDistance(50))
	if ok {
	    log.Printf("hit %s at %.3f via %s", hit.ID, hit.Distance, hit.Method)
	}`)}
			<p>
				Three one-shot helpers skip the accelerator for a single query:
				<span class="inline-code">scene.Raycast</span>
				,
				<span class="inline-code">scene.RaycastGraph</span>
				, and
				<span class="inline-code">scene.RaycastAll</span>
				. An instanced mesh reports one hit per intersected instance, identified by
				<span class="inline-code">InstanceIndex</span>
				.
			</p>
			<h3>Every hit names its method</h3>
			<p>
				<span class="inline-code">RayHit.Method</span>
				records the routine that produced the answer, so a trace never overstates its exactness.
			</p>
			<div class="scene3d-table-wrap">
				<table class="scene3d-matrix">
					<caption>Intersection method per geometry.</caption>
					<thead>
						<tr>
							<th scope="col">Geometry</th>
							<th scope="col">Method</th>
							<th scope="col">Exactness</th>
						</tr>
					</thead>
					<tbody>
						<tr>
							<th scope="row">SphereGeometry</th>
							<td>analytic-sphere</td>
							<td>exact</td>
						</tr>
						<tr>
							<th scope="row">CubeGeometry, BoxGeometry</th>
							<td>analytic-aabb</td>
							<td>exact</td>
						</tr>
						<tr>
							<th scope="row">PlaneGeometry</th>
							<td>analytic-plane</td>
							<td>exact</td>
						</tr>
						<tr>
							<th scope="row">PyramidGeometry</th>
							<td>analytic-pyramid</td>
							<td>exact</td>
						</tr>
						<tr>
							<th scope="row">CylinderGeometry (cone included)</th>
							<td>analytic-frustum</td>
							<td>exact</td>
						</tr>
						<tr>
							<th scope="row">TorusGeometry</th>
							<td>analytic-torus</td>
							<td>exact</td>
						</tr>
						<tr>
							<th scope="row">BufferGeometry, PolygonGeometry</th>
							<td>mesh-triangle</td>
							<td>exact triangles</td>
						</tr>
						<tr>
							<th scope="row">TorusKnotGeometry</th>
							<td>tessellated-triangle</td>
							<td>exact on the tessellation</td>
						</tr>
						<tr>
							<th scope="row">LinesGeometry</th>
							<td>line-threshold</td>
							<td>pick radius</td>
						</tr>
						<tr>
							<th scope="row">Points, Sprite</th>
							<td>point-threshold</td>
							<td>pick radius</td>
						</tr>
						<tr>
							<th scope="row">Decal</th>
							<td>analytic-plane</td>
							<td>exact</td>
						</tr>
						<tr>
							<th scope="row">Model (glTF)</th>
							<td>bounds-aabb</td>
							<td>bounds only</td>
						</tr>
					</tbody>
				</table>
			</div>
			<p>
				Points, sprites, and line strokes own no world thickness, so a ray hits one when it passes within a radius.
				<span class="inline-code">scene.PointThreshold</span>
				sets that radius; the default is 0.1 world units. This mirrors the two separate three.js thresholds for points and lines, kept as one number.
			</p>
			<p>
				A
				<span class="inline-code">Model</span>
				hits a bounds box because Go never loads the glTF file. If you need triangles, tessellate the mesh yourself and ship a
				<span class="inline-code">BufferGeometry</span>
				.
			</p>
			<h3>Read the trace</h3>
			<p>
				<span class="inline-code">Trace</span>
				returns traversal telemetry with no wall-clock timings, so a snapshot stays stable across machines.
			</p>
			{CodeBlock("go", `trace := accel.Trace(ray)
	log.Printf("nodes=%d tested=%d rejected=%d instances=%d hits=%d",
	    trace.NodesVisited,
	    trace.PrimitivesTested,   // exact tests that ran
	    trace.BroadphaseRejected, // removed by a bounding-volume test first
	    trace.InstancesTested,
	    len(trace.Hits))`)}
			<h3>Browser picking</h3>
			<p>
				GPU picking now works on both GPU backends. Before that, one pickable object forced a whole scene onto WebGL2.
			</p>
			<p>
				The pick contract does not branch on backend. The same input layer produces the pointer events, the pick and drag signal namespaces, and every hit field including the world-space ray.
			</p>
			<p>
				The WebGPU renderer adds a true GPU pick on top of that shared contract. It rasterizes pickable geometry into a 32-bit unsigned integer identifier attachment, then reads back the pointer pixel. It derives every geometric field from the shared CPU helpers WebGL2 uses, so both backends report the same numbers.
			</p>
			<p>
				Opt a mesh out with
				<span class="inline-code">Pickable: scene.Bool(false)</span>
				. Route the results with
				<span class="inline-code">PickSignalNamespace</span>
				,
				<span class="inline-code">DragSignalNamespace</span>
				, and
				<span class="inline-code">EventSignalNamespace</span>
				.
			</p>
		</section>
		<section id="helpers">
			<h2>Helpers and Editor Surfaces</h2>
			<p>
				Six helper nodes lower to line geometry, and one lowers to a gizmo group. Use them for editors, debug views, and documentation figures.
			</p>
			{CodeBlock("go", `scene.NewGraph(
	    scene.AxesHelper{Size: 2, Width: 2},
	    scene.GridHelper{Size: 20, Divisions: 20, Color: "#2b3b47", Width: 1},
	    scene.BoxHelper{Width: 2, Height: 1, Depth: 3, Color: "#8ecfff", WidthPx: 2},
	    scene.BoundingBoxHelper{
	        Min:     scene.Vec3(-1, 0, -1),
	        Max:     scene.Vec3(1, 2, 1),
	        Color:   "#d4af37",
	        WidthPx: 2,
	    },
	    scene.SkeletonHelper{
	        Joints: joints,
	        Bones:  [][2]int{{0, 1}, {1, 2}, {2, 3}},
	        Color:  "#ff8a5b",
	    },
	    scene.TransformControls{
	        ID:     "gizmo",
	        Target: "selected-mesh",
	        Mode:   "translate", // "translate", "rotate", "scale"
	        Size:   1.5,
	    },
	)`)}
			<p>
				<span class="inline-code">TransformControls</span>
				draws the handles. The browser controls layer owns the pointer mutation. Wire it live with two signals.
			</p>
			<ul>
				<li>
					<span class="inline-code">GizmoInputSignal</span>
					— switches the active mode with no re-render.
				</li>
				<li>
					<span class="inline-code">GizmoOutputSignal</span>
					— publishes each drag phase: target, mode, axis, phase, and the resulting transform.
				</li>
			</ul>
			<p>
				Three mesh flags cooperate with the gizmo.
				<span class="inline-code">GizmoHelper</span>
				marks a mesh as part of the live helper group.
				<span class="inline-code">GizmoFormMode</span>
				labels which form it belongs to.
				<span class="inline-code">GizmoRing</span>
				keeps the older mode-only toggle for a rotate ring.
			</p>
			<p>
				<span class="inline-code">Props.Inspector</span>
				enables the built-in scene inspector overlay.
				<span class="inline-code">gosx dev --scene-inspector</span>
				turns it on for a whole development run.
				<span class="inline-code">Props.Stats</span>
				shows the frame statistics overlay.
			</p>
		</section>
		<section id="physics-audio">
			<h2>Physics and Audio</h2>
			<h3>Physics</h3>
			<p>
				<span class="inline-code">Props.Physics</span>
				declares a scene-wide rigid-body world. A zero value means physics is off. Attach a body to a mesh with
				<span class="inline-code">Mesh.RigidBody</span>
				; the mesh identifier becomes the body identifier, and the physics runner then drives that mesh's transform.
			</p>
			<p>
				The IR carries the config beside the declared bodies. A server can therefore rebuild an authoritative world from the IR, then run it behind a simulation runner and a hub.
			</p>
			{CodeBlock("go", `props.Physics = scene.PhysicsWorld{
	    Gravity:          scene.Vec3(0, -9.81, 0),
	    FixedTimestep:    1.0 / 60.0,
	    SolverIterations: 8,
	    BroadphaseCell:   2.0,
	    Topic:            "arena:physics",
	    Colliders: []scene.Collider3D{
	        {Shape: "plane", Normal: scene.Vec3(0, 1, 0), Distance: 0},
	    },
	}

	scene.Mesh{
	    ID:       "crate-1",
	    Geometry: scene.CubeGeometry{Size: 1},
	    Material: scene.StandardMaterial{Color: "#a97b3d", Roughness: 0.8},
	    Position: scene.Vec3(0, 6, 0),
	    RigidBody: &scene.RigidBody3D{
	        Mass:        4,
	        Restitution: 0.2,
	        Friction:    0.6,
	        Colliders:   []scene.Collider3D{{Shape: "box", Width: 1, Height: 1, Depth: 1}},
	    },
	}`)}
			<p>
				Collider shapes are
				<span class="inline-code">box</span>
				,
				<span class="inline-code">sphere</span>
				,
				<span class="inline-code">capsule</span>
				, and
				<span class="inline-code">plane</span>
				. Convex hulls and triangle meshes do not exist yet.
				<span class="inline-code">Constraints</span>
				carries joints; the first supported kind is
				<span class="inline-code">distance</span>
				.
			</p>
			<h3>Audio</h3>
			<p>
				<span class="inline-code">Props.Audio</span>
				declares a sample-player manifest: named buses with volume and mute, plus clips addressed by identifier. The client registers the manifest when the engine mounts. No client change is needed.
			</p>
			{CodeBlock("go", `props.Audio = &scene.Audio{
	    MasterVolume: scene.Float(0.8),
	    Buses: []scene.AudioBus{
	        {ID: "sfx", Volume: scene.Float(0.9)},
	        {ID: "music", Volume: scene.Float(0.4)},
	    },
	    Clips: []scene.AudioClip{
	        {ID: "splash", Src: "/audio/splash.mp3", Bus: "sfx", Preload: true},
	        {ID: "theme", Src: "/audio/theme.mp3", Bus: "music", Loop: true},
	    },
	}`)}
			<p>
				One quirk is inherited from the client:
				<span class="inline-code">Muted</span>
				applies only when
				<span class="inline-code">MasterVolume</span>
				is also set. Set both when you need a mute to take effect.
			</p>
		</section>
		<section id="quality">
			<h2>Adaptive Quality and Budgets</h2>
			<p>
				Scene3D caps GPU memory by default. Every cap has a safe zero value and an explicit opt-out.
			</p>
			<ul>
				<li>
					<span class="inline-code">PostFX.MaxPixels</span>
					— 1080p by default.
				</li>
				<li>
					<span class="inline-code">Shadows.MaxPixels</span>
					— 1024 by 1024 by default.
				</li>
				<li>
					<span class="inline-code">HTML.MaxTexturePixels</span>
					— 1024 by 1024 by default.
				</li>
				<li>
					<span class="inline-code">Props.MaxPixels</span>
					and
					<span class="inline-code">MaxDevicePixelRatio</span>
					— cap the main render target.
				</li>
				<li>
					<span class="inline-code">Props.MSAASamples</span>
					— request hardware multisampling. It has no effect once a post-effect chain exists.
				</li>
				<li>
					<span class="inline-code">MaxFrameRate</span>
					,
					<span class="inline-code">MaxFPS</span>
					, and
					<span class="inline-code">FrameIntervalMS</span>
					— throttle the loop.
				</li>
			</ul>
			<h3>Frame-time governor</h3>
			<p>
				The adaptive block reacts to delivered frame time.
			</p>
			{CodeBlock("go", `props.AdaptiveQuality = scene.Bool(true)
	props.AdaptiveTargetFrameMS = 16.7 // aim for 60 fps
	props.AdaptiveWarmupFrames = 30    // ignore the first 30 frames
	props.AdaptivePostFX = scene.Bool(true)`)}
			<h3>Quality ladder</h3>
			<p>
				<span class="inline-code">Props.QualityLadder</span>
				replaces the legacy pixel-ratio tiers with a work-based ladder. One design law shapes the schema: degradation reduces
				<em>work</em>
				, never
				<em>clarity</em>
				. A rung has no resolution field, no pixel-ratio field, and no post-effect pixel budget. You physically cannot author a blur.
			</p>
			{CodeBlock("go", `props.QualityLadder = []scene.QualityRung{
	    {Name: "raw", PostEffects: nil, LayerGroups: []string{"core"}},
	    {Name: "lit", PostEffects: []string{"toneMapping"}, LayerGroups: []string{"core", "detail"}},
	    {Name: "full", PostEffects: []string{"bloom", "toneMapping", "fxaa"},
	        LayerGroups: []string{"core", "detail", "dust"}},
	}
	props.QualityStartRung = 1

	// Tag a mesh or a points layer into a rung group.
	scene.Mesh{QualityGroup: "dust", /* ... */ }

	// Map a GLB-baked points layer by name when the layer cannot carry a tag.
	props.PointQualityGroups = map[string]string{"nebula-dust": "dust"}`)}
			<p>
				Two rung fields are pass-through today, and the docs will not pretend otherwise.
				<span class="inline-code">ComputeBudgetScale</span>
				and
				<span class="inline-code">ExpensivePassCadence</span>
				reach the rung telemetry and the mount attributes, but nothing scales a compute dispatch or skips a pass yet. Read the attribute and drive your own particle counts if you need real work reduction now.
			</p>
		</section>
		<section id="streaming">
			<h2>Streaming Scene Mutations</h2>
			<p>
				<span class="inline-code">scene.DiffCommands</span>
				compares two scene states and emits the minimum command list that turns the first into the second. Send the list over a hub and the client applies it without a re-render.
			</p>
			{CodeBlock("go", `commands := scene.DiffPropsCommands(previous, next)
	payload, err := scene.MarshalCommands(commands)
	if err != nil {
	    return err
	}
	// Publish payload on any hub channel the page already binds.`)}
			<p>
				Command builders exist for each record kind. Use one to emit a targeted patch with no diff.
			</p>
			<ul>
				<li>
					<span class="inline-code">CreateObjectCommand</span>
					and
					<span class="inline-code">RemoveObjectCommand</span>
					for one mesh.
				</li>
				<li>
					<span class="inline-code">SetCameraCommand</span>
					and
					<span class="inline-code">SetEnvironmentCommand</span>
					for scene-wide state.
				</li>
				<li>
					<span class="inline-code">SetMaterialsCommand</span>
					,
					<span class="inline-code">SetPostEffectsCommand</span>
					, and
					<span class="inline-code">SetPostUniformsCommand</span>
					for appearance.
				</li>
				<li>
					One builder per particle, instance, model, light, label, sprite, HTML, and animation collection.
				</li>
			</ul>
			<p>
				<span class="inline-code">SetPostUniformsCommand</span>
				is the non-destructive path: it patches effect uniforms without rebuilding the chain.
			</p>
		</section>
		<section id="native-preview">
			<h2>Native Preview and Certification</h2>
			<p>
				Two packages render and certify a scene with no browser, no WebGPU device, and no platform graphics driver.
			</p>
			<h3>scene/preview</h3>
			<p>
				<span class="inline-code">preview.Render</span>
				lowers typed props to the native render bundle and rasterizes one frame in pure Go. Use it for authoring previews, thumbnails, documentation images, and deterministic visual tests. It also accepts a bare IR or the serialized props JSON, which lets a command-line tool render an artifact it received rather than built.
			</p>
			{CodeBlock("go", `result, err := preview.Render(props, preview.Options{
	    Width:       1280,
	    Height:      720,
	    Time:        0,
	    MaxSegments: 24,   // cap curved tessellation for fast thumbnails
	    DisablePostFX: true,
	})
	if err != nil {
	    return err
	}

	file, err := os.Create("scene.png")
	if err != nil {
	    return err
	}
	defer file.Close()
	if err := preview.WritePNG(file, result); err != nil {
	    return err
	}

	// result.Bundle and result.Stats expose the exact renderer payload:
	// draw batches, instance counts, materials, and native fallback diagnostics.`)}
			<h3>scene/harness</h3>
			<p>
				<span class="inline-code">harness.New</span>
				starts a browser-free session. Interleave frames and ray probes to model a camera move and a pointer hover, then write one JSON report designed for both humans and agents.
			</p>
			<p>
				The report carries the evidence a golden-hash test needs:
			</p>
			<ul>
				<li>a SHA-256 hash of each frame;</li>
				<li>
					pixel coverage, changed-pixel counts, and visible bounds;
				</li>
				<li>
					unique colour counts, luminance variance, and edge energy;
				</li>
				<li>
					draw batch, instance, and material counts;
				</li>
				<li>
					Selena artifact hashes and validation state per material.
				</li>
			</ul>
			{CodeBlock("go", `session := harness.New(props, preview.Options{Width: 640, Height: 360})

	if _, err := session.Render(0); err != nil {
	    return err
	}
	if _, err := session.Render(0.5); err != nil {
	    return err
	}
	session.Trace("centre", scene.Ray{
	    Origin:    scene.Vec3(0, 0, 8),
	    Direction: scene.Vec3(0, 0, -1),
	})
	if err := session.Validate(); err != nil {
	    return err
	}
	return session.WriteJSON(os.Stdout)`)}
			<p>
				The session builds one accelerator for the graph and reuses it for every trace, so a long probe sequence stays cheap.
			</p>
		</section>
		<section id="assets">
			<h2>Asset Planning</h2>
			<div class="scene3d-warning" role="note">
				<p class="scene3d-warning__title">
					<span class="inline-code">gosx assets plan</span>
					inventories and plans. It does not execute.
				</p>
				<p>
					Every meshopt pass, Draco pass, KTX2 transcode, image-based-lighting prefilter, split-sum lookup table, and level-of-detail stack entry is reported as a
					<span class="inline-code">candidate</span>
					or
					<span class="inline-code">planned</span>
					action. No encoder exists in the module. The command tells you what work exists. You still have to do it.
				</p>
			</div>
			{CodeBlock("bash", `gosx assets plan public
	gosx assets plan --json --write report.json public/assets`)}
			<p>
				The report is still useful. It classifies every asset, probes glTF and KTX2 containers, and measures HTML texture budgets. It also lists shader entry points and names the diagnostics that would break a build. Read it as a work list, not a receipt.
			</p>
			<p>
				Asset manifests stay simple until you opt into variants. Register a base binary glTF or texture for every browser. Add capability-gated variants for compressed or high-end forms. Collapse them with
				<span class="inline-code">
					rt.Assets().ManifestFor("webgpu", "ktx2")
				</span>
				once the route knows its target tier.
			</p>
		</section>
		<section id="full-stack-3d">
			<h2>Full-Stack 3D</h2>
			<p>
				Scene3D is part of the GoSX page model. One route can do four things at once from the same loader contract:
			</p>
			<ul>
				<li>server-render the page shell;</li>
				<li>stream data through a hub;</li>
				<li>hydrate island controls;</li>
				<li>mount the 3D surface.</li>
			</ul>
			<p>
				Compute islands fill the headless controller role. They hydrate through the island virtual machine and the shared-signal bridge, and they own no document nodes. That makes them the right place for input normalization and scene state derivation.
			</p>
			{CodeBlock("go", `rt := game.New(game.Config{
	    Profile: game.Web3DProfile(),
	    Assets:  assets,
	    Scene: func(ctx *game.Context) scene.Props {
	        return ProductScene(ctx.Assets)
	    },
	})

	func Load(ctx *route.RouteContext, page route.FilePage) (any, error) {
	    ctx.Runtime().BindHub("inventory", "/ws/inventory", nil)
	    ctx.Runtime().ComputeIsland(island.ComputeIslandConfig{
	        Name:         "ProductSceneController",
	        Props:        map[string]any{"assetManifest": rt.Assets().Manifest()},
	        Capabilities: []engine.Capability{engine.CapFetch, engine.CapStorage},
	    })
	    sceneProps, ok := rt.BuildScene()
	    if !ok {
	        return nil, fmt.Errorf("scene runtime did not produce props")
	    }
	    return map[string]any{
	        "scene":  sceneProps,
	        "assets": rt.Assets().Manifest(),
	    }, nil
	}`)}
			<p>
				Use
				<span class="inline-code">game.Web3DProfile()</span>
				for configurators, maps, and simulation dashboards. Use
				<span class="inline-code">game.FightingProfile()</span>
				or a custom profile when the page is a deterministic game surface.
			</p>
		</section>
		<section id="node-index">
			<h2>Node Type Index</h2>
			<p>
				Twenty-eight types satisfy
				<span class="inline-code">scene.Node</span>
				. Every one may appear in
				<span class="inline-code">scene.NewGraph</span>
				.
			</p>
			<h3>Structure and geometry</h3>
			<div class="scene3d-geometry-grid">
				<div class="scene3d-geometry-card glass-panel">
					<span class="scene3d-geometry-name">Group</span>
					<span class="scene3d-geometry-fields">position and rotation only</span>
				</div>
				<div class="scene3d-geometry-card glass-panel">
					<span class="scene3d-geometry-name">Mesh</span>
					<span class="scene3d-geometry-fields">geometry, material, leaf scale</span>
				</div>
				<div class="scene3d-geometry-card glass-panel">
					<span class="scene3d-geometry-name">LODGroup</span>
					<span class="scene3d-geometry-fields">distance-swapped levels</span>
				</div>
				<div class="scene3d-geometry-card glass-panel">
					<span class="scene3d-geometry-name">Decal</span>
					<span class="scene3d-geometry-fields">projected planar marker</span>
				</div>
				<div class="scene3d-geometry-card glass-panel">
					<span class="scene3d-geometry-name">Model</span>
					<span class="scene3d-geometry-fields">glTF instance</span>
				</div>
				<div class="scene3d-geometry-card glass-panel">
					<span class="scene3d-geometry-name">InstancedMesh</span>
					<span class="scene3d-geometry-fields">N copies, one draw call</span>
				</div>
				<div class="scene3d-geometry-card glass-panel">
					<span class="scene3d-geometry-name">InstancedGLBMesh</span>
					<span class="scene3d-geometry-fields">N copies of one glTF</span>
				</div>
			</div>
			<h3>Particles and simulation</h3>
			<div class="scene3d-geometry-grid">
				<div class="scene3d-geometry-card glass-panel">
					<span class="scene3d-geometry-name">Points</span>
					<span class="scene3d-geometry-fields">supplied point cloud</span>
				</div>
				<div class="scene3d-geometry-card glass-panel">
					<span class="scene3d-geometry-name">ComputeParticles</span>
					<span class="scene3d-geometry-fields">GPU-simulated, WebGPU only</span>
				</div>
				<div class="scene3d-geometry-card glass-panel">
					<span class="scene3d-geometry-name">WaterSystem</span>
					<span class="scene3d-geometry-fields">GPU heightfield water</span>
				</div>
			</div>
			<h3>Overlays</h3>
			<div class="scene3d-geometry-grid">
				<div class="scene3d-geometry-card glass-panel">
					<span class="scene3d-geometry-name">Label</span>
					<span class="scene3d-geometry-fields">projected text box</span>
				</div>
				<div class="scene3d-geometry-card glass-panel">
					<span class="scene3d-geometry-name">Sprite</span>
					<span class="scene3d-geometry-fields">projected image billboard</span>
				</div>
				<div class="scene3d-geometry-card glass-panel">
					<span class="scene3d-geometry-name">HTML</span>
					<span class="scene3d-geometry-fields">dom, portal, or texture mode</span>
				</div>
				<div class="scene3d-geometry-card glass-panel">
					<span class="scene3d-geometry-name">HTMLSurface</span>
					<span class="scene3d-geometry-fields">texture mode, DOM fallback today</span>
				</div>
			</div>
			<h3>Lights</h3>
			<div class="scene3d-geometry-grid">
				<div class="scene3d-geometry-card glass-panel">
					<span class="scene3d-geometry-name">AmbientLight</span>
					<span class="scene3d-geometry-fields">flat fill</span>
				</div>
				<div class="scene3d-geometry-card glass-panel">
					<span class="scene3d-geometry-name">DirectionalLight</span>
					<span class="scene3d-geometry-fields">casts shadows</span>
				</div>
				<div class="scene3d-geometry-card glass-panel">
					<span class="scene3d-geometry-name">PointLight</span>
					<span class="scene3d-geometry-fields">range and decay</span>
				</div>
				<div class="scene3d-geometry-card glass-panel">
					<span class="scene3d-geometry-name">SpotLight</span>
					<span class="scene3d-geometry-fields">cone, no shadow yet</span>
				</div>
				<div class="scene3d-geometry-card glass-panel">
					<span class="scene3d-geometry-name">HemisphereLight</span>
					<span class="scene3d-geometry-fields">sky and ground blend</span>
				</div>
				<div class="scene3d-geometry-card glass-panel">
					<span class="scene3d-geometry-name">RectAreaLight</span>
					<span class="scene3d-geometry-fields">exact diffuse, approximate specular</span>
				</div>
				<div class="scene3d-geometry-card glass-panel">
					<span class="scene3d-geometry-name">LightProbe</span>
					<span class="scene3d-geometry-fields">folds to ambient</span>
				</div>
			</div>
			<h3>Animation and helpers</h3>
			<div class="scene3d-geometry-grid">
				<div class="scene3d-geometry-card glass-panel">
					<span class="scene3d-geometry-name">AnimationClip</span>
					<span class="scene3d-geometry-fields">keyframe channels</span>
				</div>
				<div class="scene3d-geometry-card glass-panel">
					<span class="scene3d-geometry-name">AxesHelper</span>
					<span class="scene3d-geometry-fields">coloured XYZ axes</span>
				</div>
				<div class="scene3d-geometry-card glass-panel">
					<span class="scene3d-geometry-name">GridHelper</span>
					<span class="scene3d-geometry-fields">XZ reference grid</span>
				</div>
				<div class="scene3d-geometry-card glass-panel">
					<span class="scene3d-geometry-name">BoxHelper</span>
					<span class="scene3d-geometry-fields">wire box from extents</span>
				</div>
				<div class="scene3d-geometry-card glass-panel">
					<span class="scene3d-geometry-name">BoundingBoxHelper</span>
					<span class="scene3d-geometry-fields">wire box from min and max</span>
				</div>
				<div class="scene3d-geometry-card glass-panel">
					<span class="scene3d-geometry-name">SkeletonHelper</span>
					<span class="scene3d-geometry-fields">bone graph as lines</span>
				</div>
				<div class="scene3d-geometry-card glass-panel">
					<span class="scene3d-geometry-name">TransformControls</span>
					<span class="scene3d-geometry-fields">translate, rotate, scale handles</span>
				</div>
			</div>
		</section>
	</div>
}
