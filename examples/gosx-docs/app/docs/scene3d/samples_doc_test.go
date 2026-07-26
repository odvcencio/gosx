// Compile guard for the Scene3D documentation samples.
//
// Every type, field, constant, and function named in a page.gsx code sample
// appears here in valid Go. A rename in package scene therefore breaks this
// file, and the docs stop drifting from the API in silence.
//
// The test never calls these functions. It only takes their values, so the
// guard costs one compile and no run time.
package docs

import (
	"math"
	"os"
	"testing"
	"time"

	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/scene"
	"m31labs.dev/gosx/scene/harness"
	"m31labs.dev/gosx/scene/preview"
)

var sampleSmallScale = scene.Vec3(0.2, 0.2, 0.2)

func sampleSceneGraph() scene.Props {
	return scene.Props{
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
		Graph: scene.NewGraph(),
	}
}

func sampleNoHierarchicalScale() scene.Node {
	return scene.Group{
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
	}
}

func sampleBackends(props *scene.Props) {
	props.RequiredCapabilities = scene.RequireWebGPU(
		engine.CapWebGPUTimestampQuery,
		engine.CapWebGPUShaderF16,
		engine.WebGPULimit("maxTextureDimension2D", 4096),
		engine.WebGPUAdapterLimit("maxTextureDimension2D", 8192),
	)
	props.WebGPUAlphaMode = "opaque"
	props.WebGPUColorSpace = "display-p3"
	props.WebGPUToneMapping = "extended"
	props.WebGPUPowerPreference = "high-performance"
	props.PreferWebGPU = scene.Bool(true)
	props.PreferWebGL = scene.Bool(false)
	props.ForceWebGL = scene.Bool(false)
	props.RequireWebGL = scene.Bool(false)
	props.PreferCanvas = scene.Bool(false)
	props.UnsupportedMessage = "This view needs WebGPU."
}

func sampleCameraControls() scene.Props {
	return scene.Props{
		Camera: scene.PerspectiveCamera{
			Position:     scene.Vec3(0, 2, 8),
			Rotation:     scene.Rotate(0, 0, 0),
			FOV:          65,
			Near:         0.1,
			Far:          1000,
			TransitionMS: 600,
		},
		OrthographicCamera: &scene.OrthographicCamera{
			Position: scene.Vec3(0, 10, 0),
			Zoom:     1.5,
			Near:     0.1,
			Far:      500,
		},
		Controls:               scene.ControlOrbit,
		ControlTarget:          scene.Vec3(0, 1, 0),
		ControlRotateSpeed:     1.0,
		ControlZoomSpeed:       1.2,
		ControlMinDistance:     3,
		ControlMaxDistance:     40,
		ControlPitchLimit:      1.2,
		ControlRotateDirection: "grab",
		DragToRotate:           scene.Bool(true),
		PointerLock:            scene.Bool(true),
		ControlLookSpeed:       0.9,
		ControlMoveSpeed:       6,
		ScrollCameraStart:      0.0,
		ScrollCameraEnd:        1.0,
		CameraInputSignal:      "camera.in",
		CameraOutputSignal:     "camera.out",
		CursorOutputSignal:     "cursor",
		SelectionInputSignal:   "pick.selectedID",
		PickSignalNamespace:    "pick",
		DragSignalNamespace:    "drag",
		EventSignalNamespace:   "event",
		GizmoInputSignal:       "gizmo.mode",
		GizmoOutputSignal:      "gizmo.drag",
	}
}

func sampleGeometry() []scene.Node {
	outline := []float64{0, 0, 20, 0, 20, 14, 0, 14}
	courtyard := [][]float64{{6, 4, 14, 4, 14, 10, 6, 10}}
	return []scene.Node{
		scene.Mesh{
			Geometry:      scene.PolygonGeometry(outline, courtyard, 0),
			Material:      scene.StandardMaterial{Color: "#2c3a46", Roughness: 0.9},
			ReceiveShadow: true,
		},
		scene.Mesh{
			Geometry: scene.TorusGeometry{
				Radius:          2.5,
				Tube:            0.08,
				RadialSegments:  64,
				TubularSegments: 128,
			},
			Material: scene.StandardMaterial{Color: "#D4AF37", Roughness: 0.2, Metalness: 0.9},
			Position: scene.Vec3(0, 1.5, 0),
		},
		scene.Mesh{
			Geometry: scene.CylinderGeometry{
				RadiusTop: 0, RadiusBottom: 1, Height: 2, Segments: 24,
			},
			Material: scene.StandardMaterial{Color: "#C0C0C0", Roughness: 0.3, Metalness: 0.8},
		},
		scene.Mesh{Geometry: scene.BoxGeometry{Width: 1, Height: 1, Depth: 1}},
		scene.Mesh{Geometry: scene.PlaneGeometry{Width: 8, Height: 8}},
		scene.Mesh{Geometry: scene.PyramidGeometry{Width: 1, Height: 1, Depth: 1}},
		scene.Mesh{Geometry: scene.TorusKnotGeometry{Radius: 1, Tube: 0.2, RadialSegments: 16, TubularSegments: 96}},
		scene.Mesh{Geometry: scene.LinesGeometry{Points: []scene.Vector3{scene.Vec3(0, 0, 0)}, Segments: [][2]int{{0, 0}}, Width: 3}},
		scene.Mesh{Geometry: scene.BufferGeometry{Positions: []float64{0, 0, 0}, Normals: []float64{0, 1, 0}, UVs: []float64{0, 0}, Indices: []int{0}}},
	}
}

func sampleMaterials() []scene.Material {
	return []scene.Material{
		scene.StandardMaterial{Color: "#D4AF37", Roughness: 0.15, Metalness: 0.95},
		scene.StandardMaterial{Color: "#8f1d2c", Roughness: 0.35, Metalness: 0.1, Clearcoat: 0.9},
		scene.StandardMaterial{
			Color:        "#9BA0A8",
			Roughness:    0.4,
			Metalness:    0.9,
			RoughnessMap: "/textures/brushed-roughness.png",
			NormalMap:    "/textures/brushed-normal.png",
			MetalnessMap: "/textures/brushed-metal.png",
			EmissiveMap:  "/textures/brushed-emissive.png",
			Texture:      "/textures/brushed.png",
			Wireframe:    scene.Bool(false),
		},
		scene.StandardMaterial{
			Color:        "#aaddff",
			Roughness:    0.25,
			Transmission: 0.85,
			Opacity:      scene.Float(0.6),
			BlendMode:    scene.BlendAlpha,
		},
		scene.StandardMaterial{Color: "#ffffff", Roughness: 0.05, Iridescence: 1.0, Sheen: 0.5, Anisotropy: -0.4, Emissive: 0.2},
		scene.FlatMaterial{Color: "#E8E8E8"},
		scene.GhostMaterial{Color: "#D4AF37", Opacity: scene.Float(0.3)},
		scene.GlassMaterial{Color: "#aaddff"},
		scene.GlowMaterial{Color: "#fff1d6", Emissive: scene.Float(1)},
		scene.MatteMaterial{Color: "#1a1a18"},
		scene.LineBasicMaterial{
			MaterialStyle: scene.MaterialStyle{Color: "#8ecfff"},
			Width:         3,
		},
		scene.LineDashedMaterial{
			MaterialStyle: scene.MaterialStyle{Color: "#8ecfff"},
			Width:         2,
			DashSize:      0.4,
			GapSize:       0.2,
		},
		scene.FlatMaterial{Color: "#d6ebff", BlendMode: scene.BlendAdditive, RenderPass: scene.RenderAdditive},
		scene.FlatMaterial{Color: "#88d4ff", BlendMode: scene.BlendOpaque, RenderPass: scene.RenderOpaque},
		scene.FlatMaterial{Color: "#88d4ff", RenderPass: scene.RenderAlpha, Texture: "/t.png"},
	}
}

func sampleMaterialAnims() scene.Node {
	return scene.Mesh{
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
					Phase:     []float64{0},
				},
			},
			{
				Uniform: "roughness",
				Arity:   1,
				Spring: &scene.MaterialSpringAnim{
					From: 0.8, To: 0.15, Stiffness: 120, Damping: 14,
				},
			},
			{
				Uniform: "color",
				Arity:   3,
				Times:   []float64{0, 1},
				Values:  []float64{1, 0, 0, 0, 1, 0},
				Interp:  "LINEAR",
				Loop:    true,
			},
		},
	}
}

func sampleSelena() error {
	source, err := os.ReadFile("sampleMaterials/glow.sel")
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
	_ = layout
	_ = scene.Mesh{
		Geometry: scene.SphereGeometry{Radius: 1, Segments: 48},
		Material: material,
	}

	type GlowUniforms struct {
		Pulse float64
		Tint  []float64
	}
	uniforms, err := scene.SelenaUniforms(GlowUniforms{Pulse: 0.4, Tint: []float64{1, 0.85, 0.4}})
	if err != nil {
		return err
	}
	_ = scene.SelenaMaterialOptions{Material: "glow", Uniforms: uniforms}

	if _, err := scene.CompileSelenaBundle(source, scene.SelenaMaterialOptions{Material: "glow"}); err != nil {
		return err
	}
	if _, _, err := scene.CompileSelenaPoints(source, scene.SelenaMaterialOptions{}); err != nil {
		return err
	}
	if _, _, err := scene.CompileSelenaParticleRender(source, scene.SelenaMaterialOptions{}); err != nil {
		return err
	}
	if _, _, err := scene.CompileSelenaPost(source, scene.SelenaMaterialOptions{}); err != nil {
		return err
	}

	_ = scene.CustomMaterial{
		StandardMaterial: scene.StandardMaterial{Color: "#8ecfff"},
		ShaderBackend:    "sampleSelena",
		VertexWGSL:       "",
		FragmentWGSL:     "",
		VertexGLSL:       "",
		FragmentGLSL:     "",
		Uniforms:         map[string]any{"pulse": 0.4},
	}
	return nil
}

func sampleLights() []scene.Node {
	return []scene.Node{
		scene.AmbientLight{Color: "#ffffff", Intensity: 0.15},
		scene.DirectionalLight{
			Color:          "#fff1d6",
			Intensity:      1.2,
			Direction:      scene.Vec3(0.3, -1.0, -0.5),
			CastShadow:     true,
			ShadowBias:     -0.001,
			ShadowSize:     2048,
			ShadowCascades: 3,
			ShadowSoftness: 1.5,
		},
		scene.PointLight{Color: "#D4AF37", Intensity: 0.8, Position: scene.Vec3(-3, 4, 2), Range: 20, Decay: 2},
		scene.SpotLight{
			Color:     "#ffffff",
			Intensity: 1.5,
			Position:  scene.Vec3(0, 6, 0),
			Direction: scene.Vec3(0, -1, 0),
			Angle:     0.35,
			Penumbra:  0.2,
			Range:     30,
			Decay:     2,
		},
		scene.HemisphereLight{SkyColor: "#87ceeb", GroundColor: "#2d4a1e", Intensity: 0.4},
		scene.RectAreaLight{
			Color:     "#ffffff",
			Intensity: 3,
			Position:  scene.Vec3(0, 3, 2),
			Direction: scene.Vec3(0, -0.3, -1),
			Width:     4,
			Height:    2,
		},
		scene.LightProbe{Color: "#cfe6ff", Intensity: 0.5, Coefficients: []scene.Vector3{scene.Vec3(0, 0, 0)}},
	}
}

func sampleShadowsAndPostFX(props *scene.Props) {
	props.Shadows = scene.Shadows{MaxPixels: scene.ShadowMaxPixels1024}
	_ = scene.ShadowMaxPixels512
	_ = scene.ShadowMaxPixels2048
	_ = scene.ShadowMaxPixels4096
	_ = scene.ShadowMaxPixelsUnbounded

	props.PostFX = scene.PostFX{
		MaxPixels: scene.PostFXMaxPixels1080p,
		Effects: []scene.PostEffect{
			scene.SSAO{Radius: 4, Intensity: 0.55, Bias: 0.01},
			scene.Bloom{Threshold: 0.8, Strength: 0.5, Radius: 6, Scale: 0.25},
			scene.Tonemap{Mode: scene.TonemapACES, Exposure: 1.1},
			scene.ColorGrade{Contrast: 1.05, Saturation: 1.1, Exposure: 1},
			scene.Vignette{Intensity: 0.4},
			scene.DOF{FocusDistance: 8, Aperture: 0.04, MaxBlur: 8},
			scene.CustomPost{Name: "scanlines", Stage: scene.CustomPostAfterTonemap},
			scene.FXAA{},
		},
	}
	props.PostFX = scene.GameplayPostFX()
	_ = scene.PostFXMaxPixels540p
	_ = scene.PostFXMaxPixels720p
	_ = scene.PostFXMaxPixels1440p
	_ = scene.PostFXMaxPixels4K
	_ = scene.PostFXMaxPixelsUnbounded
	_ = scene.TonemapReinhard
	_ = scene.TonemapFilmic
	_ = scene.CustomPostBeforeTonemap
	props.DeferPostFX = scene.Bool(true)
	props.DeferPostFXDelayMS = 500
	props.MSAASamples = 4
}

func sampleAnimation() []scene.Node {
	return []scene.Node{
		scene.Mesh{
			Geometry:   scene.TorusGeometry{Radius: 2.5, Tube: 0.08, RadialSegments: 64},
			Material:   scene.StandardMaterial{Color: "#D4AF37", Roughness: 0.2, Metalness: 0.9},
			Spin:       scene.Rotate(0, 0.003, 0),
			Drift:      scene.Vec3(0, 0.4, 0),
			DriftSpeed: 1.0,
			DriftPhase: 0.5,
		},
		scene.AnimationClip{
			Name:     "hover",
			Duration: 2.0,
			Channels: []scene.AnimationChannel{{
				TargetNode:    0,
				Property:      "translation",
				Interpolation: "LINEAR",
				Times:         []float64{0, 1, 2},
				Values:        []float64{0, 0, 0, 0, 0.5, 0, 0, 0, 0},
			}},
		},
		scene.Model{
			Src:                "/models/character.glb",
			Animation:          "Run",
			AnimationSeq:       "Run,Idle",
			Loop:               scene.Bool(true),
			AnimationSpeed:     scene.Float(1.2),
			AnimationWeight:    scene.Float(1.0),
			AnimationFadeInMS:  scene.Int(180),
			AnimationFadeOutMS: scene.Int(180),
		},
	}
}

func sampleTransitions() scene.Node {
	return scene.Mesh{
		ID:       "featured",
		Geometry: scene.CubeGeometry{Size: 1},
		Material: scene.StandardMaterial{Color: "#d4af37"},
		Transition: scene.Transition{
			In:     scene.TransitionTiming{Duration: 400 * time.Millisecond, Easing: scene.EaseOut},
			Out:    scene.TransitionTiming{Duration: 250 * time.Millisecond, Easing: scene.EaseIn},
			Update: scene.TransitionTiming{Duration: 300 * time.Millisecond, Easing: scene.EaseInOut},
		},
		InState:  &scene.MeshProps{Opacity: scene.Float(0), Scale: &sampleSmallScale},
		OutState: &scene.MeshProps{Opacity: scene.Float(0)},
		Live:     scene.Live("inventory:update"),
	}
}

func sampleInstancing() []scene.Node {
	positions := make([]scene.Vector3, 500)
	for i := range positions {
		positions[i] = scene.Vec3(0, 0, 0)
	}
	mesh := scene.InstancedMesh{
		ID:            "grass",
		Count:         500,
		Geometry:      scene.CylinderGeometry{RadiusTop: 0.05, RadiusBottom: 0.05, Height: 2, Segments: 6},
		Material:      scene.StandardMaterial{Color: "#2d4a1e", Roughness: 0.9},
		Positions:     positions,
		Rotations:     []scene.Euler{scene.Rotate(0, 0, 0)},
		Scales:        []scene.Vector3{scene.Vec3(1, 1, 1)},
		Colors:        []string{"#2d4a1e"},
		Attributes:    map[string][]float64{"sway": {0.1}},
		CastShadow:    true,
		ReceiveShadow: true,
		Pickable:      scene.Bool(true),

		CullKernelWGSL:  "",
		CullKernelEntry: "cull",
		CullRadius:      1.5,
		CullBackend:     "elio",
	}
	_ = mesh.SpreadProps()

	return []scene.Node{
		mesh,
		scene.InstancedGLBMesh{
			ID:  "trees",
			Src: "/models/pine.glb",
			Instances: []scene.MeshInstance{
				{ID: "pine-0", Position: scene.Vec3(0, 0, 0), Scale: scene.Vec3(1, 1, 1)},
				{ID: "pine-1", Position: scene.Vec3(6, 0, -3), Scale: scene.Vec3(1.4, 1.6, 1.4), Rotation: scene.Rotate(0, 1, 0)},
			},
			Static:   scene.Bool(true),
			Visible:  scene.Bool(true),
			Pickable: scene.Bool(false),
		},
		scene.LODGroup{
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
		},
	}
}

func sampleParticles() []scene.Node {
	return []scene.Node{
		scene.Points{
			ID:           "stars",
			Count:        20000,
			Positions:    []scene.Vector3{scene.Vec3(0, 0, 0)},
			Sizes:        []float64{1},
			Colors:       []string{"#fff"},
			Color:        "#dbeafe",
			Style:        scene.PointStyleGlow,
			Size:         0.04,
			MinPixelSize: 1.5,
			MaxPixelSize: 6,
			Opacity:      0.9,
			BlendMode:    scene.BlendAdditive,
			DepthWrite:   false,
			Attenuation:  true,
			QualityGroup: "dust",
		},
		scene.ComputeParticles{
			ID:    "embers",
			Count: 5000,
			Emitter: scene.ParticleEmitter{
				Kind:     "sphere",
				Position: scene.Vec3(0, 2, 0),
				Radius:   5,
				Rate:     500,
				Lifetime: 4,
				Scatter:  0.3,
				Arms:     3,
				Wind:     0.2,
				Once:     false,
				Spin:     scene.Rotate(0, 0.1, 0),
			},
			Forces: []scene.ParticleForce{
				{Kind: "gravity", Strength: 0.5, Direction: scene.Vec3(0, -1, 0)},
				{Kind: "turbulence", Strength: 0.3, Frequency: 1.2},
				{Kind: "orbit", Strength: 0.2},
				{Kind: "wind", Strength: 0.1},
				{Kind: "drag", Strength: 0.1},
				{Kind: "radial", Strength: 0.1},
			},
			Material: scene.ParticleMaterial{
				Color:       "#D4AF37",
				ColorEnd:    "#ffffff",
				Style:       scene.PointStyleFocus,
				Size:        0.05,
				SizeEnd:     0.01,
				Opacity:     0.9,
				OpacityEnd:  0.0,
				BlendMode:   scene.BlendAdditive,
				Attenuation: true,
			},
			Bounds:         12,
			ComputeWGSL:    "",
			ComputeEntry:   "simulate",
			ComputeBackend: "elio",
		},
		scene.Points{Style: scene.PointStyleSquare},
	}
}

func sampleWater() scene.Node {
	return scene.WaterSystem{
		ID:                    "pool",
		Resolution:            512,
		SurfaceResolution:     256,
		SurfaceMeshResolution: 256,
		PoolShape:             "rounded",
		PoolWidth:             12,
		PoolHeight:            4,
		PoolLength:            12,
		CornerRadius:          1.5,
		WaveSpeed:             1.0,
		Damping:               0.995,
		NormalScale:           1.2,
		SeedDrops:             6,
		DropRadius:            0.06,
		DropStrength:          0.9,
		ShallowColor:          "#7fd4e8",
		DeepColor:             "#0b3a4a",
		AboveWaterColor:       scene.Vec3(1.2, 1.05, 0.95),
		CubeMap:               "/textures/sky-cube.ktx2",
		TileTexture:           "/textures/pool-tile.png",
		Caustics:              true,
		Reflection:            true,
		Refraction:            true,
		CausticsResolution:    512,
		LightDirection:        scene.Vec3(0.3, -1, -0.4),
		Paused:                false,
		FollowCamera:          false,
		ObjectKind:            "sphere",

		ObjectTextureResolutionMode: "budget",
		ObjectTexturePixelBudget:    1 << 18,
		ObjectDisplacementSpheres: []scene.WaterDisplacementSphere{
			{Offset: scene.Vec3(0, 0, 0), Radius: 0.5},
		},
		ObjectDisplacementEvents: []scene.WaterObjectDisplacementEvent{
			{ID: 1, ObjectKind: "sphere", ObjectRadius: 0.5},
		},
	}
}

func sampleOverlays() []scene.Node {
	return []scene.Node{
		scene.Label{
			ID:         "hotspot-1",
			Target:     "engine-block",
			Text:       "Cast aluminium block",
			MaxWidth:   220,
			MaxLines:   3,
			Overflow:   "ellipsis",
			Collision:  "hide",
			Priority:   10,
			Occlude:    true,
			OffsetY:    -12,
			AnchorX:    0.5,
			AnchorY:    1,
			ClassName:  "hotspot-label",
			WhiteSpace: "normal",
			TextAlign:  "center",
		},
		scene.Sprite{ID: "pin", Target: "engine-block", Src: "/pin.png", Fit: "contain"},
		scene.HTML{
			ID:            "spec-card",
			Target:        "engine-block",
			Mode:          scene.HTMLDOM,
			Markup:        "<h3>V8</h3>",
			ClassName:     "spec-card",
			PointerEvents: "auto",
			Occlude:       true,
			Scale:         1,
			Opacity:       0.95,
		},
		scene.HTML{Mode: scene.HTMLPortal},
		scene.HTMLSurface{
			ID:               "panel",
			Markup:           "<p>hi</p>",
			MaxTexturePixels: scene.HTMLTextureMaxPixels1024,
			TextureWidth:     1024,
			TextureHeight:    1024,
			SurfaceWidth:     2,
			SurfaceHeight:    1,
			FallbackReason:   "no texture manager",
		},
	}
}

func sampleGLTF() scene.Node {
	_ = scene.HTMLTextureMaxPixels512
	_ = scene.HTMLTextureMaxPixels2048
	_ = scene.HTMLTextureMaxPixelsUnbounded
	_ = scene.HTMLTexture
	return scene.Model{
		ID:            "helmet",
		Src:           "/models/helmet.glb",
		Position:      scene.Vec3(0, 1, 0),
		Rotation:      scene.Rotate(0, math.Pi, 0),
		Scale:         scene.Vec3(1.2, 1.2, 1.2),
		Fit:           "contain",
		FitAlign:      "center",
		Bounds:        2.5,
		Material:      scene.StandardMaterial{Color: "#D4AF37", Roughness: 0.3, Metalness: 0.8},
		CastShadow:    true,
		ReceiveShadow: true,
		Static:        scene.Bool(true),
	}
}

func sampleCompression(props *scene.Props, mesh scene.InstancedMesh) {
	props.Compression = &scene.Compression{
		BitWidth:           12,
		Progressive:        true,
		PreviewBitWidth:    2,
		ProgressiveDelayMS: 400,
		LOD:                true,
		LODThreshold:       20,
	}
	out := scene.CompressInstancedTransforms(mesh, 12)
	if out == nil {
		out = mesh.SpreadProps()
	}
	_ = out
}

func sampleRaycasting(props scene.Props) {
	accel := scene.NewSceneAccelerator(props.Graph, scene.PointThreshold(0.25))
	ray := scene.Ray{Origin: scene.Vec3(0, 4, 12), Direction: scene.Vec3(0, -0.3, -1)}
	hit, ok := accel.Raycast(ray, scene.PickableOnly(), scene.MaxDistance(50))
	if ok {
		_ = hit.ID
		_ = hit.Distance
		_ = hit.Method
		_ = hit.Kind
		_ = hit.Point
		_ = hit.Normal
		_ = hit.Pickable
		_ = hit.InstanceIndex
	}
	trace := accel.Trace(ray)
	_ = trace.NodesVisited
	_ = trace.PrimitivesTested
	_ = trace.BroadphaseRejected
	_ = trace.InstancesTested
	_ = trace.Hits
	_ = trace.Closest
	_ = trace.FilteredPrimitives
	_ = accel.PrimitiveCount()
	_, _ = scene.Raycast(props, ray)
	_, _ = scene.RaycastGraph(props.Graph, ray)
	_ = scene.RaycastAll(props.Graph, ray)
	_ = scene.TraceGraph(props.Graph, ray)
	_ = scene.DefaultPointThreshold
}

func sampleHelpers() []scene.Node {
	joints := []scene.Vector3{scene.Vec3(0, 0, 0), scene.Vec3(0, 1, 0), scene.Vec3(0, 2, 0), scene.Vec3(0, 3, 0)}
	return []scene.Node{
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
			Mode:   "translate",
			Size:   1.5,
		},
		scene.Mesh{GizmoHelper: true, GizmoFormMode: "rotate", GizmoRing: true, Selected: true, OutlineColor: "#fff", OutlineWidth: 2, DepthWrite: scene.Bool(false), Visible: scene.Bool(true)},
		scene.Decal{ID: "scorch", Src: "/decals/scorch.png", Width: 1, Height: 1, Opacity: 0.8, Color: "#000", DepthWrite: scene.Bool(false), Pickable: scene.Bool(false)},
	}
}

func samplePhysicsAudio(props *scene.Props) {
	props.Inspector = scene.Bool(true)
	props.Stats = scene.Bool(true)
	props.Physics = scene.PhysicsWorld{
		Gravity:          scene.Vec3(0, -9.81, 0),
		FixedTimestep:    1.0 / 60.0,
		SolverIterations: 8,
		BroadphaseCell:   2.0,
		Topic:            "arena:physics",
		Colliders: []scene.Collider3D{
			{Shape: "plane", Normal: scene.Vec3(0, 1, 0), Distance: 0},
		},
		Constraints: []scene.Constraint3D{
			{Kind: "distance", BodyA: "crate-1", BodyB: "crate-2", Distance: 2},
		},
	}
	_ = scene.Mesh{
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
	}
	props.Audio = &scene.Audio{
		MasterVolume: scene.Float(0.8),
		Buses: []scene.AudioBus{
			{ID: "sfx", Volume: scene.Float(0.9)},
			{ID: "music", Volume: scene.Float(0.4)},
		},
		Clips: []scene.AudioClip{
			{ID: "splash", Src: "/audio/splash.mp3", Bus: "sfx", Preload: true},
			{ID: "theme", Src: "/audio/theme.mp3", Bus: "music", Loop: true},
		},
	}
}

func sampleQuality(props *scene.Props) {
	props.AdaptiveQuality = scene.Bool(true)
	props.AdaptiveTargetFrameMS = 16.7
	props.AdaptiveWarmupFrames = 30
	props.AdaptivePostFX = scene.Bool(true)
	props.MaxPixels = 1920 * 1080
	props.MaxDevicePixelRatio = 2
	props.MinDevicePixelRatio = 1
	props.MaxFrameRate = 60
	props.MaxFPS = 60
	props.FrameIntervalMS = 16.7
	props.QualityLadder = []scene.QualityRung{
		{Name: "raw", PostEffects: nil, LayerGroups: []string{"core"}},
		{Name: "lit", PostEffects: []string{"toneMapping"}, LayerGroups: []string{"core", "detail"}},
		{Name: "full", PostEffects: []string{"bloom", "toneMapping", "fxaa"},
			LayerGroups: []string{"core", "detail", "dust"}, ComputeBudgetScale: 1, ExpensivePassCadence: 1},
	}
	props.QualityStartRung = 1
	_ = scene.Mesh{QualityGroup: "dust"}
	props.PointQualityGroups = map[string]string{"nebula-dust": "dust"}
	props.CapabilityTier = "full"
}

func sampleStreaming(previous, next scene.Props) error {
	commands := scene.DiffPropsCommands(previous, next)
	payload, err := scene.MarshalCommands(commands)
	if err != nil {
		return err
	}
	_ = payload
	_ = scene.DiffCommands(previous.SceneIR(), next.SceneIR())
	_ = scene.RemoveObjectCommand("id")
	_ = scene.SetCameraCommand(previous.Camera)
	_ = scene.SetEnvironmentCommand(previous.Environment)
	_ = scene.SetPostUniformsCommand(nil)
	return nil
}

func sampleNativePreview(props scene.Props) error {
	result, err := preview.Render(props, preview.Options{
		Width:         1280,
		Height:        720,
		Time:          0,
		MaxSegments:   24,
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
	_ = result.Bundle
	_ = result.Stats

	session := harness.New(props, preview.Options{Width: 640, Height: 360})
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
	return session.WriteJSON(os.Stdout)
}

func TestScene3DDocSamplesCompile(t *testing.T) {
	// Taking the function values compiles every sample without running it.
	guarded := []any{
		sampleSceneGraph,
		sampleBackends,
		sampleCameraControls,
		sampleNoHierarchicalScale,
		sampleGeometry,
		sampleMaterials,
		sampleMaterialAnims,
		sampleSelena,
		sampleLights,
		sampleShadowsAndPostFX,
		sampleAnimation,
		sampleTransitions,
		sampleInstancing,
		sampleParticles,
		sampleWater,
		sampleOverlays,
		sampleGLTF,
		sampleCompression,
		sampleRaycasting,
		sampleHelpers,
		samplePhysicsAudio,
		sampleQuality,
		sampleStreaming,
		sampleNativePreview,
	}
	if len(guarded) == 0 {
		t.Fatal("no documentation samples guarded")
	}
	_ = sampleSmallScale
}
