package docs

func Page() Node {
	return <div>
		<section id="scene-graph">
			<h2>Scene Graph</h2>
			<p>
				GoSX ships a ground-up PBR renderer — not a wrapper around Three.js or Babylon.js. Scenes are declared in pure Go using typed structs. The server serialises the scene graph into a compact wire format and a browser-side engine reconstructs it on WebGPU or WebGL2 at runtime. No JavaScript scene code ever touches your server.
			</p>
			<p>
				A scene starts with
				<span class="inline-code">scene.Props</span>
				, which describes the canvas dimensions, background, camera, environment, and the root
				<span class="inline-code">scene.Graph</span>
				.
			</p>
			{CodeBlock("go", `import "github.com/odvcencio/gosx/scene"
	
	props := scene.Props{
	    Width:      1280,
	    Height:     720,
	    Background: "#08151f",
	    Responsive: scene.Bool(true),
	    Controls:   "orbit",
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
	        // lights, meshes, models, particles ...
	    ),
	}`)}
			<p>
				Pass the props to the
				<span class="inline-code">Scene3D</span>
				component from your route loader:
			</p>
			{CodeBlock("go", `func Load(ctx *route.RouteContext, page route.FilePage) (any, error) {
	    return map[string]any{
	        "scene": MyScene(),
	    }, nil
	}`)}
			{CodeBlock("gosx", `<Scene3D {...data.scene} />`)}
			<p>
				The live demo above is a full PBR scene declared in Go and rendered by the engine. Drag to orbit, scroll to zoom.
			</p>
		</section>
		<section id="scene3d-demo" class="scene3d-demo-well" aria-label="PBR demo scene">
			<Scene3D {...data.demoScene} />
		</section>
		<section id="camera-controls">
			<h2>Camera &amp; Controls</h2>
			<p>
				The
				<span class="inline-code">scene.PerspectiveCamera</span>
				struct sets the initial view.
				<span class="inline-code">Position</span>
				and
				<span class="inline-code">Rotation</span>
				accept
				<span class="inline-code">scene.Vec3</span>
				and
				<span class="inline-code">scene.Euler</span>
				helpers.
			</p>
			{CodeBlock("go", `Camera: scene.PerspectiveCamera{
	    Position: scene.Vec3(0, 2, 8),
	    FOV:      65,
	    Near:     0.1,
	    Far:      1000,
	},`)}
			<p>
				The
				<span class="inline-code">Controls</span>
				field accepts
				<span class="inline-code">"orbit"</span>
				for standard drag-to-orbit behaviour. Omit it for a static camera. The engine handles pointer, touch, and wheel events natively.
			</p>
			{CodeBlock("go", `// Orbit controls with custom speed
	Controls:           "orbit",
	ControlRotateSpeed: 1.0,
	ControlZoomSpeed:   1.2,
	
	// Scroll-driven camera (for parallax hero sections)
	ScrollCameraStart: 0.0,  // camera position lerp start (0 = top of page)
	ScrollCameraEnd:   1.0,  // lerp end (1 = bottom of page)`)}
			<p>
				<span class="inline-code">DragToRotate</span>
				restricts orbit to pointer-drag only, which is useful for full-page heroes where scroll must not conflict with page scrolling.
			</p>
		</section>
		<section id="geometries">
			<h2>Geometries</h2>
			<p>
				All eight primitives implement the
				<span class="inline-code">scene.Geometry</span>
				interface. Pass any of them as the
				<span class="inline-code">Geometry</span>
				field of a
				<span class="inline-code">scene.Mesh</span>
				.
			</p>
			<div class="scene3d-geometry-grid">
				<div class="scene3d-geometry-card glass-panel">
					<span class="scene3d-geometry-name">SphereGeometry</span>
					<span class="scene3d-geometry-fields">Radius, Segments</span>
				</div>
				<div class="scene3d-geometry-card glass-panel">
					<span class="scene3d-geometry-name">BoxGeometry</span>
					<span class="scene3d-geometry-fields">Width, Height, Depth</span>
				</div>
				<div class="scene3d-geometry-card glass-panel">
					<span class="scene3d-geometry-name">CubeGeometry</span>
					<span class="scene3d-geometry-fields">Size</span>
				</div>
				<div class="scene3d-geometry-card glass-panel">
					<span class="scene3d-geometry-name">PlaneGeometry</span>
					<span class="scene3d-geometry-fields">Width, Height</span>
				</div>
				<div class="scene3d-geometry-card glass-panel">
					<span class="scene3d-geometry-name">CylinderGeometry</span>
					<span class="scene3d-geometry-fields">
						RadiusTop, RadiusBottom, Height, Segments
					</span>
				</div>
				<div class="scene3d-geometry-card glass-panel">
					<span class="scene3d-geometry-name">PyramidGeometry</span>
					<span class="scene3d-geometry-fields">Width, Height, Depth</span>
				</div>
				<div class="scene3d-geometry-card glass-panel">
					<span class="scene3d-geometry-name">TorusGeometry</span>
					<span class="scene3d-geometry-fields">
						Radius, Tube, RadialSegments, TubularSegments
					</span>
				</div>
				<div class="scene3d-geometry-card glass-panel">
					<span class="scene3d-geometry-name">LinesGeometry</span>
					<span class="scene3d-geometry-fields">Points []Vec3, Segments [][2]int</span>
				</div>
			</div>
			{CodeBlock("go", `// A torus with smooth tessellation
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
	
	// A cylinder acting as a pillar
	scene.Mesh{
	    Geometry: scene.CylinderGeometry{
	        RadiusTop: 0.3, RadiusBottom: 0.3, Height: 3, Segments: 24,
	    },
	    Material: scene.StandardMaterial{Color: "#C0C0C0", Roughness: 0.3, Metalness: 0.8},
	}`)}
		</section>
		<section id="materials">
			<h2>Materials</h2>
			<p>
				<span class="inline-code">scene.StandardMaterial</span>
				implements the physically-based roughness/metalness workflow. Every parameter maps directly to the PBR model running in the GPU shader.
			</p>
			{CodeBlock("go", `// Polished gold sphere
	scene.StandardMaterial{
	    Color:     "#D4AF37",
	    Roughness: 0.15,   // 0 = mirror, 1 = fully diffuse
	    Metalness: 0.95,   // 0 = dielectric, 1 = conductor
	}
	
	// Ceramic / painted surface
	scene.StandardMaterial{
	    Color:     "#F5F0E8",
	    Roughness: 0.7,
	    Metalness: 0.0,
	}
	
	// Brushed steel
	scene.StandardMaterial{
	    Color:        "#9BA0A8",
	    Roughness:    0.4,
	    Metalness:    0.9,
	    RoughnessMap: "/textures/brushed-roughness.png",
	    NormalMap:    "/textures/brushed-normal.png",
	}
	
	// Emissive glow
	scene.StandardMaterial{
	    Color:    "#D4AF37",
	    Emissive: 0.6,   // adds self-illumination
	    Roughness: 0.5,
	    Metalness: 0.2,
	}`)}
			<p>
				Five stylised material presets are also available for non-PBR use cases:
			</p>
			{CodeBlock("go", `// Flat (unlit, solid colour)
	scene.FlatMaterial{Color: "#E8E8E8"}
	
	// Ghost (transparent, depth-aware outline)
	scene.GhostMaterial{Color: "#D4AF37", Opacity: floatPtr(0.3)}
	
	// Glass (refraction + environment reflection)
	scene.GlassMaterial{Color: "#aaddff"}
	
	// Glow (additive bloom)
	scene.GlowMaterial{Color: "#fff1d6"}
	
	// Matte (Lambertian, no specular)
	scene.MatteMaterial{Color: "#1a1a18"}`)}
		</section>
		<section id="lights-shadows">
			<h2>Lights &amp; Shadows</h2>
			<p>
				Five light types cover the full photometric range. All lights are added as nodes to
				<span class="inline-code">scene.NewGraph()</span>
				.
			</p>
			{CodeBlock("go", `// Ambient — flat fill, no direction
	scene.AmbientLight{Color: "#ffffff", Intensity: 0.15}
	
	// Directional — parallel rays, cast from infinity
	scene.DirectionalLight{
	    Color:      "#fff1d6",
	    Intensity:  1.2,
	    Direction:  scene.Vec3(0.3, -1.0, -0.5),
	    CastShadow: true,
	    ShadowBias: -0.001,
	    ShadowSize: 2048,
	}
	
	// Point — omnidirectional, falls off with distance
	scene.PointLight{
	    Color:     "#D4AF37",
	    Intensity: 0.8,
	    Position:  scene.Vec3(-3, 4, 2),
	    Range:     20,
	    Decay:     2,
	}
	
	// Spot — cone light with penumbra
	scene.SpotLight{
	    Color:     "#ffffff",
	    Intensity: 1.5,
	    Position:  scene.Vec3(0, 6, 0),
	    Direction: scene.Vec3(0, -1, 0),
	    Angle:     0.35,     // outer cone in radians
	    Penumbra:  0.2,      // 0 = hard, 1 = soft
	    CastShadow: true,
	}
	
	// Hemisphere — sky/ground gradient ambient
	scene.HemisphereLight{
	    SkyColor:    "#87ceeb",
	    GroundColor: "#2d4a1e",
	    Intensity:   0.4,
	}`)}
			<p>
				Shadow maps are generated per
				<span class="inline-code">DirectionalLight</span>
				and
				<span class="inline-code">SpotLight</span>
				with
				<span class="inline-code">CastShadow: true</span>
				. Individual meshes opt in with
				<span class="inline-code">CastShadow</span>
				and
				<span class="inline-code">ReceiveShadow</span>
				.
			</p>
		</section>
		<section id="animation">
			<h2>Animation</h2>
			<p>
				Procedural animation is declared on meshes — no keyframe timeline required. The engine drives it in the GPU update loop.
			</p>
			{CodeBlock("go", `scene.Mesh{
	    Geometry: scene.TorusGeometry{Radius: 2.5, Tube: 0.08, RadialSegments: 64, TubularSegments: 128},
	    Material: scene.StandardMaterial{Color: "#D4AF37", Roughness: 0.2, Metalness: 0.9},
	
	    // Spin: constant angular velocity in radians/frame
	    Spin: scene.Rotate(0, 0.003, 0),   // Y axis slow spin
	
	    // Drift: sinusoidal translation
	    Drift:      scene.Vec3(0, 0.4, 0), // up/down axis
	    DriftSpeed: 1.0,                   // cycles per second
	    DriftPhase: 0.5,                   // phase offset (0–1)
	}`)}
			<p>
				glTF models can play embedded skeletal animations by name:
			</p>
			{CodeBlock("go", `scene.Model{
	    Src:       "/models/character.glb",
	    Animation: "Run",
	    Loop:      scene.Bool(true),
	}`)}
		</section>
		<section id="particles">
			<h2>Particles</h2>
			<p>
				<span class="inline-code">scene.ComputeParticles</span>
				drives a GPU compute particle system. On WebGPU the full simulation runs in WGSL compute shaders; on WebGL2 it falls back to a CPU-updated vertex buffer. Both paths produce the same visual at 100 K+ particles.
			</p>
			{CodeBlock("go", `scene.ComputeParticles{
	    Count: 5000,
	    Emitter: scene.ParticleEmitter{
	        Kind:     "sphere",   // "point", "sphere", "disc", "spiral"
	        Position: scene.Vec3(0, 2, 0),
	        Radius:   5,
	        Rate:     500,         // particles/second
	        Lifetime: 4,           // seconds
	    },
	    Forces: []scene.ParticleForce{
	        {Kind: "gravity",    Strength: 0.5, Direction: scene.Vec3(0, -1, 0)},
	        {Kind: "turbulence", Strength: 0.3},
	        {Kind: "orbit",      Strength: 0.2},
	    },
	    Material: scene.ParticleMaterial{
	        Color:       "#D4AF37",
	        ColorEnd:    "#ffffff",
	        Size:        0.05,
	        SizeEnd:     0.01,
	        Opacity:     0.9,
	        OpacityEnd:  0.0,
	        BlendMode:   scene.BlendAdditive,
	        Attenuation: true,     // size scales with distance
	    },
	    Bounds: 12,
	}`)}
		</section>
		<section id="gltf-loading">
			<h2>glTF Loading</h2>
			<p>
				<span class="inline-code">scene.Model</span>
				instances a framework-owned glTF asset. The engine loads the
				<span class="inline-code">.glb</span>
				file at runtime, applies any material override from Go, and attaches the model to the scene graph at the declared transform.
			</p>
			{CodeBlock("go", `scene.Model{
	    Src:      "/models/helmet.glb",
	    Position: scene.Vec3(0, 1, 0),
	    Scale:    scene.Vec3(1.2, 1.2, 1.2),
	    Rotation: scene.Rotate(0, math.Pi, 0),
	}
	
	// Override material from Go — useful for theming shared assets
	scene.Model{
	    Src:      "/models/base.glb",
	    Position: scene.Vec3(3, 0, 0),
	    Material: scene.StandardMaterial{
	        Color:     "#D4AF37",
	        Roughness: 0.3,
	        Metalness: 0.8,
	    },
	}
	
	// Play embedded animation
	scene.Model{
	    Src:       "/models/robot.glb",
	    Animation: "Walk",
	    Loop:      scene.Bool(true),
	    Position:  scene.Vec3(-2, 0, 0),
	}`)}
			<p>
				Model assets are served from the
				<span class="inline-code">public/models/</span>
				directory. The engine handles binary glTF, embedded textures, and Draco-compressed geometry automatically.
			</p>
		</section>
		<section id="instancing">
			<h2>Instancing</h2>
			<p>
				<span class="inline-code">scene.InstancedMesh</span>
				renders N copies of a single geometry with per-instance transforms in a single draw call. Use it for particle fields, foliage, crowds, or any repeated geometry where instancing yields order-of-magnitude GPU savings.
			</p>
			{CodeBlock("go", `positions := make([]scene.Vector3, 500)
	for i := range positions {
	    positions[i] = scene.Vec3(
	        (rand.Float64()-0.5)*20,
	        0,
	        (rand.Float64()-0.5)*20,
	    )
	}
	
	scene.InstancedMesh{
	    Count:    500,
	    Geometry: scene.CylinderGeometry{RadiusTop: 0.05, RadiusBottom: 0.05, Height: 2, Segments: 6},
	    Material: scene.StandardMaterial{Color: "#2d4a1e", Roughness: 0.9, Metalness: 0.0},
	    Positions: positions,
	    CastShadow:    true,
	    ReceiveShadow: true,
	}`)}
			<p>
				Per-instance rotations and scales are optional. If omitted, all instances share the identity rotation and a uniform scale of 1.
			</p>
		</section>
	</div>
}
