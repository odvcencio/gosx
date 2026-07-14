// Package preview renders typed GoSX Scene3D scenes without a browser, WebGPU,
// or a platform graphics driver. It is intended for authoring previews,
// thumbnails, documentation images, and deterministic visual tests.
package preview

import (
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"math"
	"strconv"
	"strings"

	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/render/bundle"
	"m31labs.dev/gosx/render/gpu/headless"
	"m31labs.dev/gosx/scene"
)

const (
	defaultWidth  = 1280
	defaultHeight = 720
)

// Options controls a single deterministic preview frame.
type Options struct {
	Width      int
	Height     int
	Time       float64
	Background string
	Camera     *scene.PerspectiveCamera
	// DisableShadows skips CPU shadow-map rasterization. This is useful for
	// quick editor thumbnails; leave false for fuller visual-regression parity.
	DisableShadows bool
	// DisablePostFX skips the authored post-processing chain for fast, robust
	// thumbnails while keeping the scene's lighting and materials intact.
	DisablePostFX bool
	// MaxSegments caps curved primitive tessellation for fast thumbnails. Zero
	// preserves authored geometry. Values below 3 are promoted to 3.
	MaxSegments int
}

// Result contains both the pixels and the exact renderer-facing payload used
// to produce them, making authoring tools able to inspect draw counts and
// native fallback diagnostics alongside the image.
type Result struct {
	Image  *image.RGBA
	Bundle engine.RenderBundle
	Stats  bundle.FrameStats
}

type wireDocument struct {
	Schema             string                     `json:"schema,omitempty"`
	Objects            []scene.ObjectIR           `json:"objects,omitempty"`
	Models             []scene.ModelIR            `json:"models,omitempty"`
	Points             []scene.PointsIR           `json:"points,omitempty"`
	InstancedMeshes    []scene.InstancedMeshIR    `json:"instancedMeshes,omitempty"`
	InstancedGLBMeshes []scene.InstancedGLBMeshIR `json:"instancedGLBMeshes,omitempty"`
	ComputeParticles   []scene.ComputeParticlesIR `json:"computeParticles,omitempty"`
	WaterSystems       []scene.WaterSystemIR      `json:"waterSystems,omitempty"`
	Animations         []scene.AnimationClipIR    `json:"animations,omitempty"`
	Labels             []scene.LabelIR            `json:"labels,omitempty"`
	Sprites            []scene.SpriteIR           `json:"sprites,omitempty"`
	HTML               []scene.HTMLIR             `json:"html,omitempty"`
	Lights             []scene.LightIR            `json:"lights,omitempty"`
	Environment        scene.EnvironmentIR        `json:"environment,omitempty"`
	PostEffects        []json.RawMessage          `json:"postEffects,omitempty"`
	PostFXMaxPixels    int                        `json:"postFXMaxPixels,omitempty"`
	ShadowMaxPixels    int                        `json:"shadowMaxPixels,omitempty"`
	ShaderLib          map[string]string          `json:"shaderLib,omitempty"`
}

// Render lowers typed scene props to the native RenderBundle contract and
// rasterizes one frame entirely in Go.
func Render(props scene.Props, opts Options) (*Result, error) {
	opts = normalizeSize(opts)
	frame := Bundle(props, opts)
	return renderBundle(frame, opts)
}

// RenderIR renders a bare SceneIR document. Since SceneIR intentionally does
// not contain presentation-level camera/background props, Options supplies
// those values (with useful defaults when omitted).
func RenderIR(ir scene.SceneIR, opts Options) (*Result, error) {
	opts = normalizeOptions(opts)
	frame := BundleIR(ir, opts)
	return renderBundle(frame, opts)
}

// RenderJSON renders either a bare SceneIR document or the runtime props JSON
// emitted by scene.Props (where the SceneIR is nested under "scene"). This is
// the bridge used by CLI/editor tooling that receives serialized authoring
// artifacts instead of a live Go value.
func RenderJSON(data []byte, opts Options) (*Result, error) {
	var envelope struct {
		Scene      json.RawMessage `json:"scene"`
		Background string          `json:"background"`
		Camera     struct {
			X, Y, Z                         float64
			RotationX, RotationY, RotationZ float64
			FOV, Near, Far                  float64
		} `json:"camera"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("scene preview: decode JSON: %w", err)
	}
	payload := data
	if len(envelope.Scene) > 0 && string(envelope.Scene) != "null" {
		payload = envelope.Scene
		if opts.Background == "" {
			opts.Background = envelope.Background
		}
		if opts.Camera == nil && (envelope.Camera.X != 0 || envelope.Camera.Y != 0 || envelope.Camera.Z != 0 ||
			envelope.Camera.RotationX != 0 || envelope.Camera.RotationY != 0 || envelope.Camera.RotationZ != 0 ||
			envelope.Camera.FOV != 0 || envelope.Camera.Near != 0 || envelope.Camera.Far != 0) {
			opts.Camera = &scene.PerspectiveCamera{
				Position: scene.Vec3(envelope.Camera.X, envelope.Camera.Y, envelope.Camera.Z),
				Rotation: scene.Euler{X: envelope.Camera.RotationX, Y: envelope.Camera.RotationY, Z: envelope.Camera.RotationZ},
				FOV:      envelope.Camera.FOV, Near: envelope.Camera.Near, Far: envelope.Camera.Far,
			}
		}
	}
	var doc wireDocument
	if err := json.Unmarshal(payload, &doc); err != nil {
		return nil, fmt.Errorf("scene preview: decode SceneIR: %w", err)
	}
	opts = normalizeOptions(opts)
	ir := scene.SceneIR{
		Schema: doc.Schema, Objects: doc.Objects, Models: doc.Models, Points: doc.Points,
		InstancedMeshes: doc.InstancedMeshes, InstancedGLBMeshes: doc.InstancedGLBMeshes,
		ComputeParticles: doc.ComputeParticles, WaterSystems: doc.WaterSystems,
		Animations: doc.Animations, Labels: doc.Labels, Sprites: doc.Sprites, HTML: doc.HTML,
		Lights: doc.Lights, Environment: doc.Environment, PostFXMaxPixels: doc.PostFXMaxPixels,
		ShadowMaxPixels: doc.ShadowMaxPixels, ShaderLib: doc.ShaderLib,
	}
	frame := BundleIR(ir, opts)
	for _, raw := range doc.PostEffects {
		if effect, ok := renderPostEffectJSON(raw); ok {
			frame.PostEffects = append(frame.PostEffects, effect)
		}
	}
	return renderBundle(frame, opts)
}

// WritePNG encodes a preview result without exposing the headless device.
func WritePNG(w io.Writer, result *Result) error {
	if result == nil || result.Image == nil {
		return fmt.Errorf("scene preview: nil image")
	}
	return png.Encode(w, result.Image)
}

func renderBundle(frame engine.RenderBundle, opts Options) (*Result, error) {
	if opts.DisableShadows {
		for index := range frame.InstancedMeshes {
			frame.InstancedMeshes[index].CastShadow = false
		}
	}
	if opts.DisablePostFX {
		frame.PostEffects = nil
	}
	if opts.MaxSegments != 0 {
		limit := max(opts.MaxSegments, 3)
		for index := range frame.InstancedMeshes {
			mesh := &frame.InstancedMeshes[index]
			if mesh.Segments == 0 || mesh.Segments > limit {
				mesh.Segments = limit
			}
			if mesh.RadialSegments > limit {
				mesh.RadialSegments = limit
			}
			if mesh.TubularSegments > limit {
				mesh.TubularSegments = limit
			}
		}
	}
	device, surface := headless.New(opts.Width, opts.Height)
	renderer, err := bundle.New(bundle.Config{Device: device, Surface: surface})
	if err != nil {
		return nil, fmt.Errorf("scene preview: create renderer: %w", err)
	}
	defer renderer.Destroy()
	if err := renderer.Frame(frame, opts.Width, opts.Height, opts.Time); err != nil {
		return nil, fmt.Errorf("scene preview: render frame: %w", err)
	}
	return &Result{Image: cloneRGBA(device.Framebuffer()), Bundle: frame, Stats: renderer.Stats()}, nil
}

// Bundle lowers typed authoring props to the renderer-facing native contract.
func Bundle(props scene.Props, opts Options) engine.RenderBundle {
	opts = normalizeSize(opts)
	if opts.Background == "" {
		opts.Background = props.Background
	}
	if opts.Camera == nil {
		camera := props.Camera
		if camera.Rotation == (scene.Euler{}) && strings.EqualFold(props.Controls, scene.ControlOrbit) {
			camera.Rotation = lookAtRotation(camera.Position, props.ControlTarget)
		}
		opts.Camera = &camera
	}
	return BundleIR(props.SceneIR(), opts)
}

func lookAtRotation(eye, target scene.Vector3) scene.Euler {
	dx, dy, dz := eye.X-target.X, eye.Y-target.Y, eye.Z-target.Z
	horizontal := math.Hypot(dx, dz)
	return scene.Euler{X: math.Atan2(dy, horizontal), Y: -math.Atan2(dx, dz)}
}

// BundleIR maps canonical SceneIR records onto the shared native renderer
// contract. Built-in objects become single-instance meshes, so ordinary and
// explicitly-instanced primitives exercise exactly the same geometry, PBR,
// shadow, culling, post-FX, and CPU-raster paths.
func BundleIR(ir scene.SceneIR, opts Options) engine.RenderBundle {
	opts = normalizeOptions(opts)
	frame := engine.RenderBundle{
		Background:      opts.Background,
		Camera:          renderCamera(*opts.Camera),
		Environment:     renderEnvironment(ir.Environment),
		PostFXMaxPixels: ir.PostFXMaxPixels,
		ShaderLib:       ir.ShaderLib,
	}
	if frame.Background == "" {
		frame.Background = "#0b0b0d"
	}
	for _, light := range ir.Lights {
		frame.Lights = append(frame.Lights, engine.RenderLight{
			ID: light.ID, Kind: light.Kind, Color: light.Color,
			GroundColor: light.GroundColor, Intensity: light.Intensity,
			X: light.X, Y: light.Y, Z: light.Z,
			DirectionX: light.DirectionX, DirectionY: light.DirectionY, DirectionZ: light.DirectionZ,
			Angle: light.Angle, Penumbra: light.Penumbra, Range: light.Range, Decay: light.Decay,
			CastShadow: light.CastShadow, ShadowBias: light.ShadowBias, ShadowSize: light.ShadowSize,
		})
	}

	materialIndexes := make(map[string]int)
	for _, object := range ir.Objects {
		material := materialFromObject(object)
		materialIndex := ensureMaterial(&frame, materialIndexes, material)
		frame.InstancedMeshes = append(frame.InstancedMeshes, engine.RenderInstancedMesh{
			ID: object.ID, Kind: object.Kind, Size: object.Size,
			Width: object.Width, Height: object.Height, Depth: object.Depth,
			Radius: object.Radius, RadiusTop: object.RadiusTop, RadiusBottom: object.RadiusBottom,
			Tube: object.Tube, Segments: object.Segments,
			RadialSegments: object.RadialSegments, TubularSegments: object.TubularSegments,
			MaterialIndex: materialIndex, InstanceCount: 1,
			Transforms: objectMatrix(object), CastShadow: object.CastShadow, ReceiveShadow: object.ReceiveShadow,
		})
	}
	for _, mesh := range ir.InstancedMeshes {
		material := materialFromInstance(mesh)
		materialIndex := ensureMaterial(&frame, materialIndexes, material)
		count := mesh.Count
		if matrixCount := len(mesh.Transforms) / 16; count <= 0 || count > matrixCount {
			count = matrixCount
		}
		frame.InstancedMeshes = append(frame.InstancedMeshes, engine.RenderInstancedMesh{
			ID: mesh.ID, Kind: mesh.Kind, Size: mesh.Size,
			Width: mesh.Width, Height: mesh.Height, Depth: mesh.Depth,
			Radius: mesh.Radius, RadiusTop: mesh.RadiusTop, RadiusBottom: mesh.RadiusBottom,
			Tube: mesh.Tube, Segments: mesh.Segments,
			RadialSegments: mesh.RadialSegments, TubularSegments: mesh.TubularSegments,
			MaterialIndex: materialIndex, InstanceCount: count,
			Transforms: append([]float64(nil), mesh.Transforms...), Colors: colorsToFloats(mesh.Colors),
			Attributes: cloneAttributes(mesh.Attributes), CastShadow: mesh.CastShadow, ReceiveShadow: mesh.ReceiveShadow,
		})
	}
	for _, points := range ir.Points {
		frame.Points = append(frame.Points, engine.RenderPoints{
			ID: points.ID, Count: points.Count, Positions: transformPointPositions(points),
			Sizes: append([]float64(nil), points.Sizes...), Colors: colorsToFloats(points.Colors),
			Color: points.Color, Size: points.Size, Opacity: defaultOpacity(points.Opacity),
			BlendMode: points.BlendMode, DepthWrite: points.DepthWrite, Attenuation: points.Attenuation,
			CustomVertex: points.CustomVertex, CustomFragment: points.CustomFragment,
			CustomVertexWGSL:   resolveShader(points.CustomVertexWGSL, points.CustomVertexWGSLRef, ir.ShaderLib),
			CustomFragmentWGSL: resolveShader(points.CustomFragmentWGSL, points.CustomFragmentWGSLRef, ir.ShaderLib),
			CustomUniforms:     points.CustomUniforms, ShaderBackend: points.ShaderBackend, ShaderLayout: points.ShaderLayout,
		})
	}
	for _, particles := range ir.ComputeParticles {
		forces := make([]engine.RenderParticleForce, len(particles.Forces))
		for index, force := range particles.Forces {
			forces[index] = engine.RenderParticleForce{Kind: force.Kind, Strength: force.Strength,
				X: force.X, Y: force.Y, Z: force.Z, Frequency: force.Frequency}
		}
		frame.ComputeParticles = append(frame.ComputeParticles, engine.RenderComputeParticles{
			ID: particles.ID, Count: particles.Count,
			Emitter: engine.RenderParticleEmitter{Kind: particles.Emitter.Kind, X: particles.Emitter.X,
				Y: particles.Emitter.Y, Z: particles.Emitter.Z, Radius: particles.Emitter.Radius,
				Rate: particles.Emitter.Rate, Lifetime: particles.Emitter.Lifetime,
				Arms: particles.Emitter.Arms, Wind: particles.Emitter.Wind, Scatter: particles.Emitter.Scatter},
			Forces: forces,
			Material: engine.RenderParticleMaterial{Color: particles.Material.Color, ColorEnd: particles.Material.ColorEnd,
				Size: particles.Material.Size, SizeEnd: particles.Material.SizeEnd,
				Opacity: defaultOpacity(particles.Material.Opacity), OpacityEnd: defaultOpacity(particles.Material.OpacityEnd),
				BlendMode: particles.Material.BlendMode, Attenuation: particles.Material.Attenuation},
			Bounds:       particles.Bounds,
			ComputeWGSL:  resolveShader(particles.ComputeWGSL, particles.ComputeWGSLRef, ir.ShaderLib),
			ComputeEntry: particles.ComputeEntry, ComputeBackend: particles.ComputeBackend,
			RenderVertex: particles.RenderVertex, RenderFragment: particles.RenderFragment,
			RenderVertexWGSL:   resolveShader(particles.RenderVertexWGSL, particles.RenderVertexWGSLRef, ir.ShaderLib),
			RenderFragmentWGSL: resolveShader(particles.RenderFragmentWGSL, particles.RenderFragmentWGSLRef, ir.ShaderLib),
			RenderUniforms:     particles.RenderUniforms, RenderShaderBackend: particles.RenderShaderBackend,
			RenderShaderLayout: particles.RenderShaderLayout,
		})
	}
	for _, water := range ir.WaterSystems {
		appendNativeWaterPreview(&frame, materialIndexes, water, opts.Time)
	}
	for _, clip := range ir.Animations {
		converted := engine.RenderAnimation{Name: clip.Name, Duration: clip.Duration}
		for _, channel := range clip.Channels {
			converted.Channels = append(converted.Channels, engine.RenderAnimationChannel{
				TargetID: strconv.Itoa(channel.TargetNode), Property: channel.Property,
				Times: append([]float64(nil), channel.Times...), Values: append([]float64(nil), channel.Values...),
				Interpolation: channel.Interpolation,
			})
		}
		frame.Animations = append(frame.Animations, converted)
	}
	for _, effect := range ir.PostEffects {
		if converted, ok := renderPostEffect(effect); ok {
			frame.PostEffects = append(frame.PostEffects, converted)
		}
	}
	for _, model := range ir.Models {
		frame.Diagnostics = append(frame.Diagnostics, unsupported("model", model.ID, "native preview does not load glTF/GLB assets yet"))
	}
	for _, model := range ir.InstancedGLBMeshes {
		frame.Diagnostics = append(frame.Diagnostics, unsupported("instanced-model", model.ID, "native preview does not load glTF/GLB assets yet"))
	}
	for _, label := range ir.Labels {
		frame.Diagnostics = append(frame.Diagnostics, unsupported("label", label.ID, "native PNG previews do not rasterize text overlays yet"))
	}
	for _, sprite := range ir.Sprites {
		frame.Diagnostics = append(frame.Diagnostics, unsupported("sprite", sprite.ID, "native PNG previews do not load sprite assets yet"))
	}
	for _, html := range ir.HTML {
		frame.Diagnostics = append(frame.Diagnostics, unsupported("html", html.ID, "native PNG previews do not rasterize HTML surfaces yet"))
	}
	frame.ObjectCount = len(ir.Objects) + len(ir.InstancedMeshes)
	return frame
}

func normalizeOptions(opts Options) Options {
	opts = normalizeSize(opts)
	if opts.Camera == nil {
		opts.Camera = &scene.PerspectiveCamera{Position: scene.Vec3(0, 2.5, 7), FOV: 50, Near: 0.1, Far: 100}
	}
	return opts
}

func normalizeSize(opts Options) Options {
	if opts.Width <= 0 {
		opts.Width = defaultWidth
	}
	if opts.Height <= 0 {
		opts.Height = defaultHeight
	}
	return opts
}

func renderCamera(camera scene.PerspectiveCamera) engine.RenderCamera {
	fov := camera.FOV
	if fov == 0 {
		fov = 50
	}
	// Public Scene3D FOV is authored in degrees; RenderBundle uses radians.
	if math.Abs(fov) > math.Pi*2 {
		fov *= math.Pi / 180
	}
	near, far := camera.Near, camera.Far
	if near <= 0 {
		near = 0.1
	}
	if far <= near {
		far = 100
	}
	return engine.RenderCamera{X: camera.Position.X, Y: camera.Position.Y, Z: camera.Position.Z,
		RotationX: camera.Rotation.X, RotationY: camera.Rotation.Y, RotationZ: camera.Rotation.Z,
		FOV: fov, Near: near, Far: far}
}

func renderEnvironment(env scene.EnvironmentIR) engine.RenderEnvironment {
	return engine.RenderEnvironment{AmbientColor: env.AmbientColor, AmbientIntensity: env.AmbientIntensity,
		SkyColor: env.SkyColor, SkyIntensity: env.SkyIntensity, GroundColor: env.GroundColor,
		GroundIntensity: env.GroundIntensity, Exposure: env.Exposure, ToneMapping: env.ToneMapping,
		EnvMap: env.EnvMap, EnvIntensity: env.EnvIntensity, EnvRotation: env.EnvRotation,
		FogColor: env.FogColor, FogDensity: env.FogDensity}
}

func materialFromObject(o scene.ObjectIR) engine.RenderMaterial {
	return newMaterial(o.MaterialKind, o.Color, o.Texture, o.Opacity, o.Emissive, o.BlendMode, o.RenderPass,
		boolValue(o.Wireframe), o.Roughness, o.Metalness, o.Clearcoat, o.Sheen, o.Transmission,
		o.Iridescence, o.Anisotropy, o.NormalMap, o.RoughnessMap, o.MetalnessMap, o.EmissiveMap,
		o.CustomVertex, o.CustomFragment, o.CustomVertexWGSL, o.CustomFragmentWGSL, o.CustomUniforms, o.ShaderBackend, o.ShaderLayout)
}

func materialFromInstance(m scene.InstancedMeshIR) engine.RenderMaterial {
	return newMaterial(m.MaterialKind, m.Color, m.Texture, m.Opacity, m.Emissive, m.BlendMode, m.RenderPass,
		boolValue(m.Wireframe), m.Roughness, m.Metalness, m.Clearcoat, m.Sheen, m.Transmission,
		m.Iridescence, m.Anisotropy, m.NormalMap, m.RoughnessMap, m.MetalnessMap, m.EmissiveMap,
		m.CustomVertex, m.CustomFragment, m.CustomVertexWGSL, m.CustomFragmentWGSL,
		m.CustomUniforms, m.ShaderBackend, m.ShaderLayout)
}

func newMaterial(kind, color, texture string, opacity, emissive *float64, blend, pass string, wire bool,
	roughness, metalness, clearcoat, sheen, transmission, iridescence, anisotropy float64,
	normalMap, roughnessMap, metalnessMap, emissiveMap, vertex, fragment, vertexWGSL, fragmentWGSL string,
	uniforms map[string]any, backend string, layout map[string]any) engine.RenderMaterial {
	if kind == "" {
		kind = "standard"
	}
	if color == "" {
		color = "#ffffff"
	}
	alpha := 1.0
	if opacity != nil {
		alpha = *opacity
	}
	emit := 0.0
	if emissive != nil {
		emit = *emissive
	}
	m := engine.RenderMaterial{Kind: kind, Color: color, Texture: texture, Opacity: alpha, Wireframe: wire,
		BlendMode: blend, RenderPass: pass, Emissive: emit, Roughness: roughness, Metalness: metalness,
		Clearcoat: clearcoat, Sheen: sheen, Transmission: transmission, Iridescence: iridescence,
		Anisotropy: anisotropy, NormalMap: normalMap, RoughnessMap: roughnessMap,
		MetalnessMap: metalnessMap, EmissiveMap: emissiveMap, CustomVertex: vertex,
		CustomFragment: fragment, CustomVertexWGSL: vertexWGSL, CustomFragmentWGSL: fragmentWGSL,
		CustomUniforms: uniforms, ShaderBackend: backend, ShaderLayout: layout}
	keyBytes, _ := json.Marshal(m)
	m.Key = string(keyBytes)
	return m
}

func ensureMaterial(frame *engine.RenderBundle, indexes map[string]int, material engine.RenderMaterial) int {
	if index, ok := indexes[material.Key]; ok {
		return index
	}
	index := len(frame.Materials)
	frame.Materials = append(frame.Materials, material)
	indexes[material.Key] = index
	return index
}

func objectMatrix(o scene.ObjectIR) []float64 {
	return trsMatrix(o.X, o.Y, o.Z, o.RotationX, o.RotationY, o.RotationZ, 1, 1, 1)
}

func trsMatrix(x, y, z, rx, ry, rz, sx, sy, sz float64) []float64 {
	cx, sxn := math.Cos(rx), math.Sin(rx)
	cy, syn := math.Cos(ry), math.Sin(ry)
	cz, szn := math.Cos(rz), math.Sin(rz)
	// Column-major Rz*Ry*Rx with scale applied to each basis column.
	return []float64{
		(cz * cy) * sx, (szn * cy) * sx, (-syn) * sx, 0,
		(cz*syn*sxn - szn*cx) * sy, (szn*syn*sxn + cz*cx) * sy, (cy * sxn) * sy, 0,
		(cz*syn*cx + szn*sxn) * sz, (szn*syn*cx - cz*sxn) * sz, (cy * cx) * sz, 0,
		x, y, z, 1,
	}
}

func transformPointPositions(points scene.PointsIR) []float64 {
	if len(points.Positions) == 0 {
		return nil
	}
	matrix := trsMatrix(points.X, points.Y, points.Z, points.RotationX, points.RotationY, points.RotationZ, 1, 1, 1)
	out := make([]float64, len(points.Positions))
	copy(out, points.Positions)
	for index := 0; index+2 < len(out); index += 3 {
		x, y, z := out[index], out[index+1], out[index+2]
		out[index] = matrix[0]*x + matrix[4]*y + matrix[8]*z + matrix[12]
		out[index+1] = matrix[1]*x + matrix[5]*y + matrix[9]*z + matrix[13]
		out[index+2] = matrix[2]*x + matrix[6]*y + matrix[10]*z + matrix[14]
	}
	return out
}

func colorsToFloats(colors []string) []float64 {
	if len(colors) == 0 {
		return nil
	}
	out := make([]float64, 0, len(colors)*4)
	for _, value := range colors {
		r, g, b, a := parseColor(value)
		out = append(out, r, g, b, a)
	}
	return out
}

func parseColor(value string) (float64, float64, float64, float64) {
	s := strings.TrimPrefix(strings.TrimSpace(value), "#")
	if len(s) == 3 || len(s) == 4 {
		var expanded strings.Builder
		for _, ch := range s {
			expanded.WriteRune(ch)
			expanded.WriteRune(ch)
		}
		s = expanded.String()
	}
	if len(s) != 6 && len(s) != 8 {
		return 1, 1, 1, 1
	}
	parse := func(part string) float64 {
		v, err := strconv.ParseUint(part, 16, 8)
		if err != nil {
			return 255
		}
		return float64(v) / 255
	}
	a := 1.0
	if len(s) == 8 {
		a = parse(s[6:8])
	}
	return parse(s[0:2]), parse(s[2:4]), parse(s[4:6]), a
}

func renderPostEffect(effect scene.PostEffectIR) (engine.RenderPostEffect, bool) {
	data, err := json.Marshal(effect)
	if err != nil {
		return engine.RenderPostEffect{}, false
	}
	return renderPostEffectJSON(data)
}

func renderPostEffectJSON(data []byte) (engine.RenderPostEffect, bool) {
	var out engine.RenderPostEffect
	if err := json.Unmarshal(data, &out); err != nil || out.Kind == "" {
		return engine.RenderPostEffect{}, false
	}
	// Preserve effect-specific numeric fields in Params as the native post-FX
	// implementations read those for DOF, vignette, SSAO, and color grading.
	var raw map[string]any
	if json.Unmarshal(data, &raw) == nil {
		out.Params = make(map[string]float64)
		for key, value := range raw {
			if n, ok := value.(float64); ok {
				out.Params[key] = n
			}
		}
		if len(out.Params) == 0 {
			out.Params = nil
		}
	}
	return out, true
}

func unsupported(feature, id, message string) engine.RenderDiagnostic {
	return engine.RenderDiagnostic{Severity: "warning", Code: "scene.preview.unsupported_" + feature,
		Message: message, Backend: "headless", Target: id}
}

func resolveShader(inline, ref string, library map[string]string) string {
	if inline != "" {
		return inline
	}
	return library[ref]
}

func cloneAttributes(in map[string][]float64) map[string][]float64 {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string][]float64, len(in))
	for key, value := range in {
		out[key] = append([]float64(nil), value...)
	}
	return out
}

func boolValue(value *bool) bool { return value != nil && *value }

func defaultOpacity(value float64) float64 {
	if value == 0 {
		return 1
	}
	return value
}

func cloneRGBA(src *image.RGBA) *image.RGBA {
	if src == nil {
		return nil
	}
	out := image.NewRGBA(src.Rect)
	copy(out.Pix, src.Pix)
	return out
}
