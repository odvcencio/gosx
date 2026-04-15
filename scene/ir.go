package scene

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// IRVersion is the current canonical Scene3D IR schema version.
const IRVersion = 1

// IR is the canonical scene description consumed by Scene3D planners and
// backends. The existing SceneIR type remains the compatibility payload for
// legacy prop-based consumers; IR is the compiler-first contract.
type IR struct {
	Version     int            `json:"version"`
	Camera      IRCamera       `json:"camera"`
	Environment IREnvironment  `json:"environment"`
	Materials   []IRMaterial   `json:"materials,omitempty"`
	Lights      []IRLight      `json:"lights,omitempty"`
	Nodes       []IRNode       `json:"nodes,omitempty"`
	PostFX      []IRPostEffect `json:"postFX,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// IRCamera describes the camera used to prepare a scene.
type IRCamera struct {
	Kind      string  `json:"kind,omitempty"`
	X         float64 `json:"x,omitempty"`
	Y         float64 `json:"y,omitempty"`
	Z         float64 `json:"z,omitempty"`
	RotationX float64 `json:"rotationX,omitempty"`
	RotationY float64 `json:"rotationY,omitempty"`
	RotationZ float64 `json:"rotationZ,omitempty"`
	FOV       float64 `json:"fov,omitempty"`
	Near      float64 `json:"near,omitempty"`
	Far       float64 `json:"far,omitempty"`
}

// IREnvironment describes scene-wide lighting, atmosphere, and exposure.
type IREnvironment struct {
	AmbientColor     string  `json:"ambientColor,omitempty"`
	AmbientIntensity float64 `json:"ambientIntensity,omitempty"`
	SkyColor         string  `json:"skyColor,omitempty"`
	SkyIntensity     float64 `json:"skyIntensity,omitempty"`
	GroundColor      string  `json:"groundColor,omitempty"`
	GroundIntensity  float64 `json:"groundIntensity,omitempty"`
	Background       string  `json:"background,omitempty"`
	Exposure         float64 `json:"exposure,omitempty"`
	ToneMapping      string  `json:"toneMapping,omitempty"`
	FogColor         string  `json:"fogColor,omitempty"`
	FogDensity       float64 `json:"fogDensity,omitempty"`
}

// IRMaterial is a reusable material profile referenced by node materialIndex.
type IRMaterial struct {
	Name         string    `json:"name,omitempty"`
	Kind         string    `json:"kind,omitempty"`
	Color        string    `json:"color,omitempty"`
	Albedo       []float64 `json:"albedo,omitempty"`
	Opacity      float64   `json:"opacity,omitempty"`
	Emissive     float64   `json:"emissive,omitempty"`
	Roughness    float64   `json:"roughness,omitempty"`
	Metalness    float64   `json:"metalness,omitempty"`
	Texture      string    `json:"texture,omitempty"`
	NormalMap    string    `json:"normalMap,omitempty"`
	RoughnessMap string    `json:"roughnessMap,omitempty"`
	MetalnessMap string    `json:"metalnessMap,omitempty"`
	EmissiveMap  string    `json:"emissiveMap,omitempty"`
	BlendMode    string    `json:"blendMode,omitempty"`
	RenderPass   string    `json:"renderPass,omitempty"`
	Wireframe    *bool     `json:"wireframe,omitempty"`
	DepthWrite   *bool     `json:"depthWrite,omitempty"`
}

// IRNode is a discriminated union over Kind. Exactly one payload should be set
// for the corresponding kind.
type IRNode struct {
	Kind          string           `json:"kind"`
	ID            string           `json:"id,omitempty"`
	MaterialIndex int              `json:"materialIndex,omitempty"`
	Transform     IRTransform      `json:"transform"`
	Mesh          *IRMeshNode      `json:"mesh,omitempty"`
	Points        *IRPointsNode    `json:"points,omitempty"`
	InstancedMesh *IRInstancedMesh `json:"instancedMesh,omitempty"`
	Compute       *IRComputeNode   `json:"compute,omitempty"`
	Sprite        *IRSpriteNode    `json:"sprite,omitempty"`
	Label         *IRLabelNode     `json:"label,omitempty"`
	Capabilities  []string         `json:"capabilities,omitempty"`
	Metadata      map[string]any   `json:"metadata,omitempty"`
}

// IRTransform is the shared transform block used by every renderable node.
type IRTransform struct {
	X         float64 `json:"x,omitempty"`
	Y         float64 `json:"y,omitempty"`
	Z         float64 `json:"z,omitempty"`
	RotationX float64 `json:"rotationX,omitempty"`
	RotationY float64 `json:"rotationY,omitempty"`
	RotationZ float64 `json:"rotationZ,omitempty"`
	SpinX     float64 `json:"spinX,omitempty"`
	SpinY     float64 `json:"spinY,omitempty"`
	SpinZ     float64 `json:"spinZ,omitempty"`
	ScaleX    float64 `json:"scaleX,omitempty"`
	ScaleY    float64 `json:"scaleY,omitempty"`
	ScaleZ    float64 `json:"scaleZ,omitempty"`
}

// IRMeshNode describes a single mesh primitive or model instance.
type IRMeshNode struct {
	Kind            string      `json:"kind,omitempty"`
	Src             string      `json:"src,omitempty"`
	Size            float64     `json:"size,omitempty"`
	Width           float64     `json:"width,omitempty"`
	Height          float64     `json:"height,omitempty"`
	Depth           float64     `json:"depth,omitempty"`
	Radius          float64     `json:"radius,omitempty"`
	Segments        int         `json:"segments,omitempty"`
	Points          []IRVector3 `json:"points,omitempty"`
	LineSegments    [][2]int    `json:"lineSegments,omitempty"`
	LineWidth       float64     `json:"lineWidth,omitempty"`
	RadiusTop       float64     `json:"radiusTop,omitempty"`
	RadiusBottom    float64     `json:"radiusBottom,omitempty"`
	Tube            float64     `json:"tube,omitempty"`
	RadialSegments  int         `json:"radialSegments,omitempty"`
	TubularSegments int         `json:"tubularSegments,omitempty"`
	CastShadow      bool        `json:"castShadow,omitempty"`
	ReceiveShadow   bool        `json:"receiveShadow,omitempty"`
	Pickable        *bool       `json:"pickable,omitempty"`
	Static          *bool       `json:"static,omitempty"`
	Animation       string      `json:"animation,omitempty"`
	Loop            *bool       `json:"loop,omitempty"`
}

// IRPointsNode describes a static point cloud.
type IRPointsNode struct {
	Count          int       `json:"count"`
	Positions      []float64 `json:"positions,omitempty"`
	Sizes          []float64 `json:"sizes,omitempty"`
	Colors         []string  `json:"colors,omitempty"`
	Color          string    `json:"color,omitempty"`
	Style          string    `json:"style,omitempty"`
	Size           float64   `json:"size,omitempty"`
	Opacity        float64   `json:"opacity,omitempty"`
	BlendMode      string    `json:"blendMode,omitempty"`
	DepthWrite     *bool     `json:"depthWrite,omitempty"`
	Attenuation    bool      `json:"attenuation,omitempty"`
	PositionStride int       `json:"positionStride,omitempty"`
}

// IRInstancedMesh describes one instanced primitive batch.
type IRInstancedMesh struct {
	Count         int       `json:"count"`
	Kind          string    `json:"kind"`
	Width         float64   `json:"width,omitempty"`
	Height        float64   `json:"height,omitempty"`
	Depth         float64   `json:"depth,omitempty"`
	Radius        float64   `json:"radius,omitempty"`
	Segments      int       `json:"segments,omitempty"`
	Transforms    []float64 `json:"transforms"`
	CastShadow    bool      `json:"castShadow,omitempty"`
	ReceiveShadow bool      `json:"receiveShadow,omitempty"`
}

// IRComputeNode describes a GPU particle system.
type IRComputeNode struct {
	Count    int                `json:"count"`
	Emitter  IRParticleEmitter  `json:"emitter"`
	Forces   []IRParticleForce  `json:"forces,omitempty"`
	Material IRParticleMaterial `json:"material"`
	Bounds   float64            `json:"bounds,omitempty"`
}

type IRParticleEmitter struct {
	Kind      string  `json:"kind"`
	X         float64 `json:"x,omitempty"`
	Y         float64 `json:"y,omitempty"`
	Z         float64 `json:"z,omitempty"`
	RotationX float64 `json:"rotationX,omitempty"`
	RotationY float64 `json:"rotationY,omitempty"`
	RotationZ float64 `json:"rotationZ,omitempty"`
	SpinX     float64 `json:"spinX,omitempty"`
	SpinY     float64 `json:"spinY,omitempty"`
	SpinZ     float64 `json:"spinZ,omitempty"`
	Radius    float64 `json:"radius,omitempty"`
	Rate      float64 `json:"rate,omitempty"`
	Lifetime  float64 `json:"lifetime,omitempty"`
	Arms      int     `json:"arms,omitempty"`
	Wind      float64 `json:"wind,omitempty"`
	Scatter   float64 `json:"scatter,omitempty"`
}

type IRParticleForce struct {
	Kind      string  `json:"kind"`
	Strength  float64 `json:"strength,omitempty"`
	X         float64 `json:"x,omitempty"`
	Y         float64 `json:"y,omitempty"`
	Z         float64 `json:"z,omitempty"`
	Frequency float64 `json:"frequency,omitempty"`
}

type IRParticleMaterial struct {
	Color       string  `json:"color,omitempty"`
	ColorEnd    string  `json:"colorEnd,omitempty"`
	Style       string  `json:"style,omitempty"`
	Size        float64 `json:"size,omitempty"`
	SizeEnd     float64 `json:"sizeEnd,omitempty"`
	Opacity     float64 `json:"opacity,omitempty"`
	OpacityEnd  float64 `json:"opacityEnd,omitempty"`
	BlendMode   string  `json:"blendMode,omitempty"`
	Attenuation bool    `json:"attenuation,omitempty"`
}

type IRSpriteNode struct {
	Src       string  `json:"src"`
	ClassName string  `json:"className,omitempty"`
	Width     float64 `json:"width,omitempty"`
	Height    float64 `json:"height,omitempty"`
	Scale     float64 `json:"scale,omitempty"`
	Opacity   float64 `json:"opacity,omitempty"`
	Priority  float64 `json:"priority,omitempty"`
	OffsetX   float64 `json:"offsetX,omitempty"`
	OffsetY   float64 `json:"offsetY,omitempty"`
	AnchorX   float64 `json:"anchorX,omitempty"`
	AnchorY   float64 `json:"anchorY,omitempty"`
	Occlude   bool    `json:"occlude,omitempty"`
	Fit       string  `json:"fit,omitempty"`
}

type IRLabelNode struct {
	Text        string  `json:"text"`
	ClassName   string  `json:"className,omitempty"`
	Priority    float64 `json:"priority,omitempty"`
	MaxWidth    float64 `json:"maxWidth,omitempty"`
	MaxLines    int     `json:"maxLines,omitempty"`
	Overflow    string  `json:"overflow,omitempty"`
	Font        string  `json:"font,omitempty"`
	LineHeight  float64 `json:"lineHeight,omitempty"`
	Color       string  `json:"color,omitempty"`
	Background  string  `json:"background,omitempty"`
	BorderColor string  `json:"borderColor,omitempty"`
	OffsetX     float64 `json:"offsetX,omitempty"`
	OffsetY     float64 `json:"offsetY,omitempty"`
	AnchorX     float64 `json:"anchorX,omitempty"`
	AnchorY     float64 `json:"anchorY,omitempty"`
	Collision   string  `json:"collision,omitempty"`
	Occlude     bool    `json:"occlude,omitempty"`
	WhiteSpace  string  `json:"whiteSpace,omitempty"`
	TextAlign   string  `json:"textAlign,omitempty"`
}

type IRVector3 struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
}

// IRLight describes one scene light.
type IRLight struct {
	ID          string  `json:"id,omitempty"`
	Kind        string  `json:"kind"`
	Color       string  `json:"color,omitempty"`
	GroundColor string  `json:"groundColor,omitempty"`
	Intensity   float64 `json:"intensity,omitempty"`
	X           float64 `json:"x,omitempty"`
	Y           float64 `json:"y,omitempty"`
	Z           float64 `json:"z,omitempty"`
	DirectionX  float64 `json:"directionX,omitempty"`
	DirectionY  float64 `json:"directionY,omitempty"`
	DirectionZ  float64 `json:"directionZ,omitempty"`
	Angle       float64 `json:"angle,omitempty"`
	Penumbra    float64 `json:"penumbra,omitempty"`
	Range       float64 `json:"range,omitempty"`
	Decay       float64 `json:"decay,omitempty"`
	CastShadow  bool    `json:"castShadow,omitempty"`
	ShadowBias  float64 `json:"shadowBias,omitempty"`
	ShadowSize  int     `json:"shadowSize,omitempty"`
}

// IRPostEffect describes one post-processing pass.
type IRPostEffect struct {
	Kind       string             `json:"kind"`
	Threshold  float64            `json:"threshold,omitempty"`
	Intensity  float64            `json:"intensity,omitempty"`
	Radius     float64            `json:"radius,omitempty"`
	Scale      float64            `json:"scale,omitempty"`
	Saturation float64            `json:"saturation,omitempty"`
	Contrast   float64            `json:"contrast,omitempty"`
	Exposure   float64            `json:"exposure,omitempty"`
	Mode       string             `json:"mode,omitempty"`
	Props      map[string]float64 `json:"props,omitempty"`
}

// Validate checks the schema invariants that do not require a GPU backend.
func (ir *IR) Validate() error {
	if ir == nil {
		return errors.New("scene IR is nil")
	}
	var problems []string
	if ir.Version != IRVersion {
		problems = append(problems, fmt.Sprintf("version must be %d", IRVersion))
	}
	if ir.Camera.Near < 0 {
		problems = append(problems, "camera.near must be non-negative")
	}
	if ir.Camera.Far != 0 && ir.Camera.Near != 0 && ir.Camera.Far <= ir.Camera.Near {
		problems = append(problems, "camera.far must be greater than camera.near")
	}
	for i, node := range ir.Nodes {
		problems = append(problems, validateIRNode(i, node, len(ir.Materials))...)
	}
	for i, light := range ir.Lights {
		if strings.TrimSpace(light.Kind) == "" {
			problems = append(problems, fmt.Sprintf("lights[%d].kind is required", i))
		}
	}
	for i, effect := range ir.PostFX {
		if strings.TrimSpace(effect.Kind) == "" {
			problems = append(problems, fmt.Sprintf("postFX[%d].kind is required", i))
		}
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

func validateIRNode(index int, node IRNode, materialCount int) []string {
	var problems []string
	kind := strings.TrimSpace(node.Kind)
	if kind == "" {
		return []string{fmt.Sprintf("nodes[%d].kind is required", index)}
	}
	if node.MaterialIndex < 0 {
		problems = append(problems, fmt.Sprintf("nodes[%d].materialIndex must be non-negative", index))
	}
	if materialCount > 0 && node.MaterialIndex >= materialCount {
		problems = append(problems, fmt.Sprintf("nodes[%d].materialIndex out of range", index))
	}

	payloads := 0
	if node.Mesh != nil {
		payloads++
	}
	if node.Points != nil {
		payloads++
	}
	if node.InstancedMesh != nil {
		payloads++
	}
	if node.Compute != nil {
		payloads++
	}
	if node.Sprite != nil {
		payloads++
	}
	if node.Label != nil {
		payloads++
	}
	if payloads != 1 {
		problems = append(problems, fmt.Sprintf("nodes[%d] must set exactly one payload", index))
	}

	switch kind {
	case "mesh":
		if node.Mesh == nil {
			problems = append(problems, fmt.Sprintf("nodes[%d].mesh is required", index))
		}
	case "points":
		if node.Points == nil {
			problems = append(problems, fmt.Sprintf("nodes[%d].points is required", index))
		} else if node.Points.Count < 0 {
			problems = append(problems, fmt.Sprintf("nodes[%d].points.count must be non-negative", index))
		}
	case "instanced-mesh":
		if node.InstancedMesh == nil {
			problems = append(problems, fmt.Sprintf("nodes[%d].instancedMesh is required", index))
		} else if node.InstancedMesh.Count < 0 {
			problems = append(problems, fmt.Sprintf("nodes[%d].instancedMesh.count must be non-negative", index))
		}
	case "compute-particles":
		if node.Compute == nil {
			problems = append(problems, fmt.Sprintf("nodes[%d].compute is required", index))
		} else if node.Compute.Count < 0 {
			problems = append(problems, fmt.Sprintf("nodes[%d].compute.count must be non-negative", index))
		}
	case "sprite":
		if node.Sprite == nil {
			problems = append(problems, fmt.Sprintf("nodes[%d].sprite is required", index))
		}
	case "label":
		if node.Label == nil {
			problems = append(problems, fmt.Sprintf("nodes[%d].label is required", index))
		}
	default:
		problems = append(problems, fmt.Sprintf("nodes[%d].kind %q is unknown", index, kind))
	}
	return problems
}

// CanonicalIR lowers existing typed Scene3D props into the canonical IR. It is
// intentionally additive: old prop-based consumers still use Props.MarshalJSON.
func (p Props) CanonicalIR() IR {
	legacy := p.SceneIR()
	out := IR{
		Version:     IRVersion,
		Camera:      cameraToIR(p.Camera),
		Environment: environmentToIR(p.Background, legacy.Environment),
		Lights:      lightsToIR(legacy.Lights),
		Nodes:       make([]IRNode, 0, len(legacy.Objects)+len(legacy.Models)+len(legacy.Points)+len(legacy.InstancedMeshes)+len(legacy.ComputeParticles)+len(legacy.Sprites)+len(legacy.Labels)),
	}
	materialIndexes := map[string]int{}
	for _, object := range legacy.Objects {
		materialIndex := appendIRMaterial(&out.Materials, materialIndexes, materialFromObjectIR(object))
		out.Nodes = append(out.Nodes, objectToIRNode(object, materialIndex))
	}
	for _, model := range legacy.Models {
		materialIndex := appendIRMaterial(&out.Materials, materialIndexes, materialFromObjectIR(model.ObjectIR))
		out.Nodes = append(out.Nodes, modelToIRNode(model, materialIndex))
	}
	for _, points := range legacy.Points {
		out.Nodes = append(out.Nodes, pointsToIRNode(points))
	}
	for _, instanced := range legacy.InstancedMeshes {
		materialIndex := appendIRMaterial(&out.Materials, materialIndexes, materialFromInstancedIR(instanced))
		out.Nodes = append(out.Nodes, instancedToIRNode(instanced, materialIndex))
	}
	for _, compute := range legacy.ComputeParticles {
		out.Nodes = append(out.Nodes, computeToIRNode(compute))
	}
	for _, sprite := range legacy.Sprites {
		out.Nodes = append(out.Nodes, spriteToIRNode(sprite))
	}
	for _, label := range legacy.Labels {
		out.Nodes = append(out.Nodes, labelToIRNode(label))
	}
	return out
}

func (ir IR) MarshalJSON() ([]byte, error) {
	type alias IR
	if ir.Version == 0 {
		ir.Version = IRVersion
	}
	return json.Marshal(alias(ir))
}

func cameraToIR(camera PerspectiveCamera) IRCamera {
	return IRCamera{
		Kind:      "perspective",
		X:         camera.Position.X,
		Y:         camera.Position.Y,
		Z:         camera.Position.Z,
		RotationX: camera.Rotation.X,
		RotationY: camera.Rotation.Y,
		RotationZ: camera.Rotation.Z,
		FOV:       camera.FOV,
		Near:      camera.Near,
		Far:       camera.Far,
	}
}

func environmentToIR(background string, environment EnvironmentIR) IREnvironment {
	return IREnvironment{
		AmbientColor:     environment.AmbientColor,
		AmbientIntensity: environment.AmbientIntensity,
		SkyColor:         environment.SkyColor,
		SkyIntensity:     environment.SkyIntensity,
		GroundColor:      environment.GroundColor,
		GroundIntensity:  environment.GroundIntensity,
		Background:       strings.TrimSpace(background),
		Exposure:         environment.Exposure,
		ToneMapping:      environment.ToneMapping,
		FogColor:         environment.FogColor,
		FogDensity:       environment.FogDensity,
	}
}

func lightsToIR(items []LightIR) []IRLight {
	if len(items) == 0 {
		return nil
	}
	out := make([]IRLight, 0, len(items))
	for _, item := range items {
		out = append(out, IRLight{
			ID:          item.ID,
			Kind:        item.Kind,
			Color:       item.Color,
			GroundColor: item.GroundColor,
			Intensity:   item.Intensity,
			X:           item.X,
			Y:           item.Y,
			Z:           item.Z,
			DirectionX:  item.DirectionX,
			DirectionY:  item.DirectionY,
			DirectionZ:  item.DirectionZ,
			Angle:       item.Angle,
			Penumbra:    item.Penumbra,
			Range:       item.Range,
			Decay:       item.Decay,
			CastShadow:  item.CastShadow,
			ShadowBias:  item.ShadowBias,
			ShadowSize:  item.ShadowSize,
		})
	}
	return out
}

func appendIRMaterial(materials *[]IRMaterial, indexes map[string]int, material IRMaterial) int {
	keyBytes, _ := json.Marshal(material)
	key := string(keyBytes)
	if idx, ok := indexes[key]; ok {
		return idx
	}
	idx := len(*materials)
	indexes[key] = idx
	*materials = append(*materials, material)
	return idx
}

func materialFromObjectIR(object ObjectIR) IRMaterial {
	return IRMaterial{
		Kind:         firstNonEmptySceneString(object.MaterialKind, "standard"),
		Color:        object.Color,
		Texture:      object.Texture,
		Opacity:      derefFloat64(object.Opacity),
		Emissive:     derefFloat64(object.Emissive),
		Roughness:    object.Roughness,
		Metalness:    object.Metalness,
		NormalMap:    object.NormalMap,
		RoughnessMap: object.RoughnessMap,
		MetalnessMap: object.MetalnessMap,
		EmissiveMap:  object.EmissiveMap,
		BlendMode:    object.BlendMode,
		RenderPass:   object.RenderPass,
		Wireframe:    object.Wireframe,
		DepthWrite:   object.DepthWrite,
	}
}

func materialFromInstancedIR(mesh InstancedMeshIR) IRMaterial {
	return IRMaterial{
		Kind:      firstNonEmptySceneString(mesh.MaterialKind, "standard"),
		Color:     mesh.Color,
		Roughness: mesh.Roughness,
		Metalness: mesh.Metalness,
	}
}

func objectToIRNode(object ObjectIR, materialIndex int) IRNode {
	return IRNode{
		Kind:          "mesh",
		ID:            object.ID,
		MaterialIndex: materialIndex,
		Transform:     transformFromObjectIR(object),
		Mesh: &IRMeshNode{
			Kind:            object.Kind,
			Size:            object.Size,
			Width:           object.Width,
			Height:          object.Height,
			Depth:           object.Depth,
			Radius:          object.Radius,
			Segments:        object.Segments,
			Points:          vector3ListToIR(object.Points),
			LineSegments:    object.LineSegments,
			LineWidth:       object.LineWidth,
			RadiusTop:       object.RadiusTop,
			RadiusBottom:    object.RadiusBottom,
			Tube:            object.Tube,
			RadialSegments:  object.RadialSegments,
			TubularSegments: object.TubularSegments,
			CastShadow:      object.CastShadow,
			ReceiveShadow:   object.ReceiveShadow,
			Pickable:        object.Pickable,
		},
	}
}

func modelToIRNode(model ModelIR, materialIndex int) IRNode {
	node := objectToIRNode(model.ObjectIR, materialIndex)
	node.ID = model.ID
	node.Transform.ScaleX = model.ScaleX
	node.Transform.ScaleY = model.ScaleY
	node.Transform.ScaleZ = model.ScaleZ
	node.Mesh.Src = model.Src
	node.Mesh.Static = model.Static
	node.Mesh.Animation = model.Animation
	node.Mesh.Loop = model.Loop
	return node
}

func pointsToIRNode(points PointsIR) IRNode {
	return IRNode{
		Kind:      "points",
		ID:        points.ID,
		Transform: transformFromPointsIR(points),
		Points: &IRPointsNode{
			Count:          points.Count,
			Positions:      append([]float64(nil), points.Positions...),
			Sizes:          append([]float64(nil), points.Sizes...),
			Colors:         append([]string(nil), points.Colors...),
			Color:          points.Color,
			Style:          points.Style,
			Size:           points.Size,
			Opacity:        points.Opacity,
			BlendMode:      points.BlendMode,
			DepthWrite:     points.DepthWrite,
			Attenuation:    points.Attenuation,
			PositionStride: points.PositionStride,
		},
	}
}

func instancedToIRNode(mesh InstancedMeshIR, materialIndex int) IRNode {
	return IRNode{
		Kind:          "instanced-mesh",
		ID:            mesh.ID,
		MaterialIndex: materialIndex,
		InstancedMesh: &IRInstancedMesh{
			Count:         mesh.Count,
			Kind:          mesh.Kind,
			Width:         mesh.Width,
			Height:        mesh.Height,
			Depth:         mesh.Depth,
			Radius:        mesh.Radius,
			Segments:      mesh.Segments,
			Transforms:    append([]float64(nil), mesh.Transforms...),
			CastShadow:    mesh.CastShadow,
			ReceiveShadow: mesh.ReceiveShadow,
		},
	}
}

func computeToIRNode(compute ComputeParticlesIR) IRNode {
	return IRNode{
		Kind:         "compute-particles",
		ID:           compute.ID,
		Capabilities: []string{"compute"},
		Compute: &IRComputeNode{
			Count:    compute.Count,
			Emitter:  emitterToIR(compute.Emitter),
			Forces:   forcesToIR(compute.Forces),
			Material: particleMaterialToIR(compute.Material),
			Bounds:   compute.Bounds,
		},
	}
}

func spriteToIRNode(sprite SpriteIR) IRNode {
	return IRNode{
		Kind:      "sprite",
		ID:        sprite.ID,
		Transform: transformFromSpriteIR(sprite),
		Sprite: &IRSpriteNode{
			Src:       sprite.Src,
			ClassName: sprite.ClassName,
			Width:     sprite.Width,
			Height:    sprite.Height,
			Scale:     sprite.Scale,
			Opacity:   sprite.Opacity,
			Priority:  sprite.Priority,
			OffsetX:   sprite.OffsetX,
			OffsetY:   sprite.OffsetY,
			AnchorX:   sprite.AnchorX,
			AnchorY:   sprite.AnchorY,
			Occlude:   sprite.Occlude,
			Fit:       sprite.Fit,
		},
	}
}

func labelToIRNode(label LabelIR) IRNode {
	return IRNode{
		Kind:      "label",
		ID:        label.ID,
		Transform: transformFromLabelIR(label),
		Label: &IRLabelNode{
			Text:        label.Text,
			ClassName:   label.ClassName,
			Priority:    label.Priority,
			MaxWidth:    label.MaxWidth,
			MaxLines:    label.MaxLines,
			Overflow:    label.Overflow,
			Font:        label.Font,
			LineHeight:  label.LineHeight,
			Color:       label.Color,
			Background:  label.Background,
			BorderColor: label.BorderColor,
			OffsetX:     label.OffsetX,
			OffsetY:     label.OffsetY,
			AnchorX:     label.AnchorX,
			AnchorY:     label.AnchorY,
			Collision:   label.Collision,
			Occlude:     label.Occlude,
			WhiteSpace:  label.WhiteSpace,
			TextAlign:   label.TextAlign,
		},
	}
}

func transformFromObjectIR(object ObjectIR) IRTransform {
	return IRTransform{
		X:         object.X,
		Y:         object.Y,
		Z:         object.Z,
		RotationX: object.RotationX,
		RotationY: object.RotationY,
		RotationZ: object.RotationZ,
		SpinX:     object.SpinX,
		SpinY:     object.SpinY,
		SpinZ:     object.SpinZ,
		ScaleX:    1,
		ScaleY:    1,
		ScaleZ:    1,
	}
}

func transformFromPointsIR(points PointsIR) IRTransform {
	return IRTransform{
		X:         points.X,
		Y:         points.Y,
		Z:         points.Z,
		RotationX: points.RotationX,
		RotationY: points.RotationY,
		RotationZ: points.RotationZ,
		SpinX:     points.SpinX,
		SpinY:     points.SpinY,
		SpinZ:     points.SpinZ,
		ScaleX:    1,
		ScaleY:    1,
		ScaleZ:    1,
	}
}

func transformFromSpriteIR(sprite SpriteIR) IRTransform {
	return IRTransform{
		X:      sprite.X,
		Y:      sprite.Y,
		Z:      sprite.Z,
		ScaleX: 1,
		ScaleY: 1,
		ScaleZ: 1,
	}
}

func transformFromLabelIR(label LabelIR) IRTransform {
	return IRTransform{
		X:      label.X,
		Y:      label.Y,
		Z:      label.Z,
		ScaleX: 1,
		ScaleY: 1,
		ScaleZ: 1,
	}
}

func vector3ListToIR(points []Vector3) []IRVector3 {
	if len(points) == 0 {
		return nil
	}
	out := make([]IRVector3, 0, len(points))
	for _, point := range points {
		out = append(out, IRVector3{X: point.X, Y: point.Y, Z: point.Z})
	}
	return out
}

func emitterToIR(emitter ParticleEmitterIR) IRParticleEmitter {
	return IRParticleEmitter{
		Kind:      emitter.Kind,
		X:         emitter.X,
		Y:         emitter.Y,
		Z:         emitter.Z,
		RotationX: emitter.RotationX,
		RotationY: emitter.RotationY,
		RotationZ: emitter.RotationZ,
		SpinX:     emitter.SpinX,
		SpinY:     emitter.SpinY,
		SpinZ:     emitter.SpinZ,
		Radius:    emitter.Radius,
		Rate:      emitter.Rate,
		Lifetime:  emitter.Lifetime,
		Arms:      emitter.Arms,
		Wind:      emitter.Wind,
		Scatter:   emitter.Scatter,
	}
}

func forcesToIR(forces []ParticleForceIR) []IRParticleForce {
	if len(forces) == 0 {
		return nil
	}
	out := make([]IRParticleForce, 0, len(forces))
	for _, force := range forces {
		out = append(out, IRParticleForce{
			Kind:      force.Kind,
			Strength:  force.Strength,
			X:         force.X,
			Y:         force.Y,
			Z:         force.Z,
			Frequency: force.Frequency,
		})
	}
	return out
}

func particleMaterialToIR(material ParticleMaterialIR) IRParticleMaterial {
	return IRParticleMaterial{
		Color:       material.Color,
		ColorEnd:    material.ColorEnd,
		Style:       material.Style,
		Size:        material.Size,
		SizeEnd:     material.SizeEnd,
		Opacity:     material.Opacity,
		OpacityEnd:  material.OpacityEnd,
		BlendMode:   material.BlendMode,
		Attenuation: material.Attenuation,
	}
}

func derefFloat64(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
}

func firstNonEmptySceneString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
