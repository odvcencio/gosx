package engine

// RenderPoint is a 2D point in screen space.
type RenderPoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// RenderLine is a single screen-space line segment.
type RenderLine struct {
	From      RenderPoint `json:"from"`
	To        RenderPoint `json:"to"`
	Color     string      `json:"color,omitempty"`
	LineWidth float64     `json:"lineWidth,omitempty"`
}

// RenderLabel is a screen-space text overlay anchored to a scene position.
type RenderLabel struct {
	ID          string      `json:"id,omitempty"`
	Text        string      `json:"text,omitempty"`
	ClassName   string      `json:"className,omitempty"`
	Position    RenderPoint `json:"position"`
	Depth       float64     `json:"depth,omitempty"`
	Priority    float64     `json:"priority,omitempty"`
	MaxWidth    float64     `json:"maxWidth,omitempty"`
	MaxLines    int         `json:"maxLines,omitempty"`
	Overflow    string      `json:"overflow,omitempty"`
	Font        string      `json:"font,omitempty"`
	LineHeight  float64     `json:"lineHeight,omitempty"`
	Color       string      `json:"color,omitempty"`
	Background  string      `json:"background,omitempty"`
	BorderColor string      `json:"borderColor,omitempty"`
	OffsetX     float64     `json:"offsetX,omitempty"`
	OffsetY     float64     `json:"offsetY,omitempty"`
	AnchorX     float64     `json:"anchorX,omitempty"`
	AnchorY     float64     `json:"anchorY,omitempty"`
	Collision   string      `json:"collision,omitempty"`
	Occlude     bool        `json:"occlude,omitempty"`
	WhiteSpace  string      `json:"whiteSpace,omitempty"`
	TextAlign   string      `json:"textAlign,omitempty"`
}

// RenderSprite is a projected image overlay anchored to a scene position.
type RenderSprite struct {
	ID        string      `json:"id,omitempty"`
	Src       string      `json:"src,omitempty"`
	ClassName string      `json:"className,omitempty"`
	Position  RenderPoint `json:"position"`
	Depth     float64     `json:"depth,omitempty"`
	Priority  float64     `json:"priority,omitempty"`
	Width     float64     `json:"width,omitempty"`
	Height    float64     `json:"height,omitempty"`
	Opacity   float64     `json:"opacity,omitempty"`
	OffsetX   float64     `json:"offsetX,omitempty"`
	OffsetY   float64     `json:"offsetY,omitempty"`
	AnchorX   float64     `json:"anchorX,omitempty"`
	AnchorY   float64     `json:"anchorY,omitempty"`
	Occlude   bool        `json:"occlude,omitempty"`
	Fit       string      `json:"fit,omitempty"`
}

// RenderHTML is a dom-mode HTML surface placed in world space on the canvas
// board. X/Y are world units (the client projects them via the ortho camera);
// Width/Height are page pixels. Markup is trusted host-authored HTML.
type RenderHTML struct {
	ID            string  `json:"id"`
	Markup        string  `json:"markup"`
	X             float64 `json:"x"`
	Y             float64 `json:"y"`
	Width         float64 `json:"width"`
	Height        float64 `json:"height"`
	PointerEvents string  `json:"pointerEvents,omitempty"`
}

// RenderCamera describes the camera used for world-space rendering.
//
// Mode selects the projection pipeline. The empty string is the default
// (perspective 3D, Scene3D's path). The constant bundle.OrthoCamera2DMode
// switches to the 2D pipeline (orthographic projection, depth + lighting
// + post-FX disabled) per ADR 0004. For ortho2d cameras, X/Y are reused
// as pan offsets and Z is the zoom factor (1 = 1 world unit per pixel).
type RenderCamera struct {
	Mode      string  `json:"mode,omitempty"`
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

// RenderLight is a resolved scene light record.
type RenderLight struct {
	ID          string  `json:"id,omitempty"`
	Kind        string  `json:"kind,omitempty"`
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

// RenderEnvironment describes scene-wide lighting state.
type RenderEnvironment struct {
	AmbientColor     string  `json:"ambientColor,omitempty"`
	AmbientIntensity float64 `json:"ambientIntensity,omitempty"`
	SkyColor         string  `json:"skyColor,omitempty"`
	SkyIntensity     float64 `json:"skyIntensity,omitempty"`
	GroundColor      string  `json:"groundColor,omitempty"`
	GroundIntensity  float64 `json:"groundIntensity,omitempty"`
	Exposure         float64 `json:"exposure,omitempty"`
	ToneMapping      string  `json:"toneMapping,omitempty"`
	EnvMap           string  `json:"envMap,omitempty"`
	EnvIntensity     float64 `json:"envIntensity,omitempty"`
	EnvRotation      float64 `json:"envRotation,omitempty"`
	FogColor         string  `json:"fogColor,omitempty"`
	FogDensity       float64 `json:"fogDensity,omitempty"`
}

// RenderPoints is a GPU-ready particle system entry for the render bundle.
type RenderPoints struct {
	ID          string    `json:"id,omitempty"`
	Count       int       `json:"count"`
	Positions   []float64 `json:"positions,omitempty"`
	Sizes       []float64 `json:"sizes,omitempty"`
	Colors      []float64 `json:"colors,omitempty"`
	Color       string    `json:"color,omitempty"`
	Size        float64   `json:"size,omitempty"`
	Opacity     float64   `json:"opacity,omitempty"`
	BlendMode   string    `json:"blendMode,omitempty"`
	DepthWrite  *bool     `json:"depthWrite,omitempty"`
	Attenuation bool      `json:"attenuation,omitempty"`
}

// RenderMaterial is a resolved material profile for a draw bundle.
type RenderMaterial struct {
	Key                string         `json:"key,omitempty"`
	Kind               string         `json:"kind,omitempty"`
	Color              string         `json:"color,omitempty"`
	Texture            string         `json:"texture,omitempty"`
	Opacity            float64        `json:"opacity,omitempty"`
	Wireframe          bool           `json:"wireframe,omitempty"`
	BlendMode          string         `json:"blendMode,omitempty"`
	RenderPass         string         `json:"renderPass,omitempty"`
	ShaderData         []float64      `json:"shaderData,omitempty"`
	Emissive           float64        `json:"emissive,omitempty"`
	Roughness          float64        `json:"roughness,omitempty"`
	Metalness          float64        `json:"metalness,omitempty"`
	Clearcoat          float64        `json:"clearcoat,omitempty"`
	Sheen              float64        `json:"sheen,omitempty"`
	Transmission       float64        `json:"transmission,omitempty"`
	Iridescence        float64        `json:"iridescence,omitempty"`
	Anisotropy         float64        `json:"anisotropy,omitempty"`
	NormalMap          string         `json:"normalMap,omitempty"`
	RoughnessMap       string         `json:"roughnessMap,omitempty"`
	MetalnessMap       string         `json:"metalnessMap,omitempty"`
	EmissiveMap        string         `json:"emissiveMap,omitempty"`
	CustomVertex       string         `json:"customVertex,omitempty"`
	CustomFragment     string         `json:"customFragment,omitempty"`
	CustomVertexWGSL   string         `json:"customVertexWGSL,omitempty"`
	CustomFragmentWGSL string         `json:"customFragmentWGSL,omitempty"`
	CustomUniforms     map[string]any `json:"customUniforms,omitempty"`
	ShaderBackend      string         `json:"shaderBackend,omitempty"`
	ShaderLayout       map[string]any `json:"shaderLayout,omitempty"`
	Unlit              bool           `json:"unlit,omitempty"`
}

// RenderSurface is a textured world-space quad emitted alongside line geometry.
type RenderSurface struct {
	ID              string       `json:"id,omitempty"`
	Kind            string       `json:"kind,omitempty"`
	SourceKind      string       `json:"sourceKind,omitempty"`
	SourceID        string       `json:"sourceID,omitempty"`
	TextureKey      string       `json:"textureKey,omitempty"`
	TextureWidth    int          `json:"textureWidth,omitempty"`
	TextureHeight   int          `json:"textureHeight,omitempty"`
	TextureBytes    int          `json:"textureBytes,omitempty"`
	TextureMaxBytes int          `json:"textureMaxBytes,omitempty"`
	TextureReady    bool         `json:"textureReady,omitempty"`
	Fallback        string       `json:"fallback,omitempty"`
	FallbackReason  string       `json:"fallbackReason,omitempty"`
	MaterialIndex   int          `json:"materialIndex,omitempty"`
	RenderPass      string       `json:"renderPass,omitempty"`
	Static          bool         `json:"static,omitempty"`
	Positions       []float64    `json:"positions,omitempty"`
	UV              []float64    `json:"uv,omitempty"`
	VertexCount     int          `json:"vertexCount,omitempty"`
	Bounds          RenderBounds `json:"bounds,omitempty"`
	DepthNear       float64      `json:"depthNear,omitempty"`
	DepthFar        float64      `json:"depthFar,omitempty"`
	DepthCenter     float64      `json:"depthCenter,omitempty"`
	ViewCulled      bool         `json:"viewCulled,omitempty"`
}

// RenderBounds is a world-space axis-aligned bounds record for a render object.
type RenderBounds struct {
	MinX float64 `json:"minX,omitempty"`
	MinY float64 `json:"minY,omitempty"`
	MinZ float64 `json:"minZ,omitempty"`
	MaxX float64 `json:"maxX,omitempty"`
	MaxY float64 `json:"maxY,omitempty"`
	MaxZ float64 `json:"maxZ,omitempty"`
}

// RenderObject maps a resolved scene object onto a slice of the world-space
// vertex buffers.
type RenderObject struct {
	ID            string       `json:"id,omitempty"`
	Kind          string       `json:"kind,omitempty"`
	Pickable      *bool        `json:"pickable,omitempty"`
	CastShadow    bool         `json:"castShadow,omitempty"`
	ReceiveShadow bool         `json:"receiveShadow,omitempty"`
	DepthWrite    *bool        `json:"depthWrite,omitempty"`
	MaterialIndex int          `json:"materialIndex,omitempty"`
	RenderPass    string       `json:"renderPass,omitempty"`
	VertexOffset  int          `json:"vertexOffset,omitempty"`
	VertexCount   int          `json:"vertexCount,omitempty"`
	Static        bool         `json:"static,omitempty"`
	Bounds        RenderBounds `json:"bounds,omitempty"`
	DepthNear     float64      `json:"depthNear,omitempty"`
	DepthFar      float64      `json:"depthFar,omitempty"`
	DepthCenter   float64      `json:"depthCenter,omitempty"`
	ViewCulled    bool         `json:"viewCulled,omitempty"`
}

// RenderPassBundle is a prebatched GPU upload payload for a single render pass.
type RenderPassBundle struct {
	Name        string    `json:"name,omitempty"`
	Blend       string    `json:"blend,omitempty"`
	Depth       string    `json:"depth,omitempty"`
	Static      bool      `json:"static,omitempty"`
	CacheKey    string    `json:"cacheKey,omitempty"`
	Positions   []float64 `json:"positions,omitempty"`
	Colors      []float64 `json:"colors,omitempty"`
	Materials   []float64 `json:"materials,omitempty"`
	VertexCount int       `json:"vertexCount,omitempty"`
	Normals     []float64 `json:"normals,omitempty"`
	UVs         []float64 `json:"uvs,omitempty"`
	Tangents    []float64 `json:"tangents,omitempty"`
}

// RenderAnimation describes a resolved animation clip for the render bundle.
type RenderAnimation struct {
	Name     string                   `json:"name"`
	Channels []RenderAnimationChannel `json:"channels"`
	Duration float64                  `json:"duration"`
}

// RenderAnimationChannel is a single property track within an animation clip.
type RenderAnimationChannel struct {
	TargetID      string    `json:"targetID"`
	Property      string    `json:"property"`
	Times         []float64 `json:"times"`
	Values        []float64 `json:"values"`
	Interpolation string    `json:"interpolation,omitempty"`
}

// RenderPostEffect describes a post-processing effect applied after scene rendering.
type RenderPostEffect struct {
	Kind      string             `json:"kind"`
	Mode      string             `json:"mode,omitempty"`
	Intensity float64            `json:"intensity,omitempty"`
	Threshold float64            `json:"threshold,omitempty"`
	Radius    float64            `json:"radius,omitempty"`
	Scale     float64            `json:"scale,omitempty"`
	Params    map[string]float64 `json:"params,omitempty"`
}

// RenderDiagnostic describes an explicit renderer/backend decision surfaced to
// development tools and headless tests.
type RenderDiagnostic struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Message  string `json:"message"`
	Backend  string `json:"backend,omitempty"`
	Target   string `json:"target,omitempty"`
}

// RenderInstancedMesh is a GPU-ready instanced mesh entry for the render bundle.
type RenderInstancedMesh struct {
	ID              string               `json:"id,omitempty"`
	Kind            string               `json:"kind"`
	Size            float64              `json:"size,omitempty"`
	Width           float64              `json:"width,omitempty"`
	Height          float64              `json:"height,omitempty"`
	Depth           float64              `json:"depth,omitempty"`
	Radius          float64              `json:"radius,omitempty"`
	RadiusTop       float64              `json:"radiusTop,omitempty"`
	RadiusBottom    float64              `json:"radiusBottom,omitempty"`
	Tube            float64              `json:"tube,omitempty"`
	Segments        int                  `json:"segments,omitempty"`
	RadialSegments  int                  `json:"radialSegments,omitempty"`
	TubularSegments int                  `json:"tubularSegments,omitempty"`
	MaterialIndex   int                  `json:"materialIndex"`
	VertexCount     int                  `json:"vertexCount"`
	InstanceCount   int                  `json:"instanceCount"`
	Transforms      []float64            `json:"transforms"`
	Colors          []float64            `json:"colors,omitempty"`
	Attributes      map[string][]float64 `json:"attributes,omitempty"`
	SkinID          string               `json:"skinID,omitempty"`
	JointIndices    []uint32             `json:"jointIndices,omitempty"`
	Weights         []float64            `json:"weights,omitempty"`
	BindPose        []float64            `json:"bindPose,omitempty"`
	CastShadow      bool                 `json:"castShadow,omitempty"`
	ReceiveShadow   bool                 `json:"receiveShadow,omitempty"`
}

// RenderParticleEmitter describes an emitter for the render bundle particle system.
type RenderParticleEmitter struct {
	Kind     string  `json:"kind"`
	X        float64 `json:"x,omitempty"`
	Y        float64 `json:"y,omitempty"`
	Z        float64 `json:"z,omitempty"`
	Radius   float64 `json:"radius,omitempty"`
	Rate     float64 `json:"rate,omitempty"`
	Lifetime float64 `json:"lifetime,omitempty"`
	Arms     int     `json:"arms,omitempty"`
	Wind     float64 `json:"wind,omitempty"`
	Scatter  float64 `json:"scatter,omitempty"`
}

// RenderParticleForce describes a force acting on render bundle particles.
type RenderParticleForce struct {
	Kind      string  `json:"kind"`
	Strength  float64 `json:"strength,omitempty"`
	X         float64 `json:"x,omitempty"`
	Y         float64 `json:"y,omitempty"`
	Z         float64 `json:"z,omitempty"`
	Frequency float64 `json:"frequency,omitempty"`
}

// RenderParticleMaterial describes the material for render bundle particles.
type RenderParticleMaterial struct {
	Color       string  `json:"color,omitempty"`
	ColorEnd    string  `json:"colorEnd,omitempty"`
	Size        float64 `json:"size,omitempty"`
	SizeEnd     float64 `json:"sizeEnd,omitempty"`
	Opacity     float64 `json:"opacity,omitempty"`
	OpacityEnd  float64 `json:"opacityEnd,omitempty"`
	BlendMode   string  `json:"blendMode,omitempty"`
	Attenuation bool    `json:"attenuation,omitempty"`
}

// RenderComputeParticles is a GPU-ready compute particle system for the render bundle.
type RenderComputeParticles struct {
	ID       string                 `json:"id,omitempty"`
	Count    int                    `json:"count"`
	Emitter  RenderParticleEmitter  `json:"emitter"`
	Forces   []RenderParticleForce  `json:"forces,omitempty"`
	Material RenderParticleMaterial `json:"material"`
	Bounds   float64                `json:"bounds,omitempty"`
}

// RenderBundle is the renderer-facing scene payload emitted by the shared
// engine runtime for a single frame.
type RenderBundle struct {
	Background       string                   `json:"background,omitempty"`
	Camera           RenderCamera             `json:"camera,omitempty"`
	Lights           []RenderLight            `json:"lights,omitempty"`
	Environment      RenderEnvironment        `json:"environment,omitempty"`
	Materials        []RenderMaterial         `json:"materials,omitempty"`
	Objects          []RenderObject           `json:"objects,omitempty"`
	Points           []RenderPoints           `json:"points,omitempty"`
	InstancedMeshes  []RenderInstancedMesh    `json:"instancedMeshes,omitempty"`
	ComputeParticles []RenderComputeParticles `json:"computeParticles,omitempty"`
	Surfaces         []RenderSurface          `json:"surfaces,omitempty"`
	Passes           []RenderPassBundle       `json:"passes,omitempty"`
	Lines            []RenderLine             `json:"lines,omitempty"`
	Labels           []RenderLabel            `json:"labels,omitempty"`
	Sprites          []RenderSprite           `json:"sprites,omitempty"`
	HTML             []RenderHTML             `json:"html,omitempty"`
	Positions        []float64                `json:"positions,omitempty"`
	Colors           []float64                `json:"colors,omitempty"`
	VertexCount      int                      `json:"vertexCount,omitempty"`
	WorldPositions   []float64                `json:"worldPositions,omitempty"`
	WorldColors      []float64                `json:"worldColors,omitempty"`
	WorldVertexCount int                      `json:"worldVertexCount,omitempty"`
	WorldNormals     []float64                `json:"worldNormals,omitempty"`
	WorldUVs         []float64                `json:"worldUVs,omitempty"`
	WorldTangents    []float64                `json:"worldTangents,omitempty"`
	ObjectCount      int                      `json:"objectCount,omitempty"`
	Animations       []RenderAnimation        `json:"animations,omitempty"`
	PostEffects      []RenderPostEffect       `json:"postEffects,omitempty"`
	PostFXMaxPixels  int                      `json:"postFXMaxPixels,omitempty"`
	Diagnostics      []RenderDiagnostic       `json:"diagnostics,omitempty"`
}
