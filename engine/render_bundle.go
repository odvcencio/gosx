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
	Position    RenderPoint `json:"position"`
	Depth       float64     `json:"depth,omitempty"`
	MaxWidth    float64     `json:"maxWidth,omitempty"`
	Font        string      `json:"font,omitempty"`
	LineHeight  float64     `json:"lineHeight,omitempty"`
	Color       string      `json:"color,omitempty"`
	Background  string      `json:"background,omitempty"`
	BorderColor string      `json:"borderColor,omitempty"`
	OffsetX     float64     `json:"offsetX,omitempty"`
	OffsetY     float64     `json:"offsetY,omitempty"`
	AnchorX     float64     `json:"anchorX,omitempty"`
	AnchorY     float64     `json:"anchorY,omitempty"`
	WhiteSpace  string      `json:"whiteSpace,omitempty"`
	TextAlign   string      `json:"textAlign,omitempty"`
}

// RenderCamera describes the camera used for world-space rendering.
type RenderCamera struct {
	X    float64 `json:"x,omitempty"`
	Y    float64 `json:"y,omitempty"`
	Z    float64 `json:"z,omitempty"`
	FOV  float64 `json:"fov,omitempty"`
	Near float64 `json:"near,omitempty"`
	Far  float64 `json:"far,omitempty"`
}

// RenderMaterial is a resolved material profile for a draw bundle.
type RenderMaterial struct {
	Key        string    `json:"key,omitempty"`
	Kind       string    `json:"kind,omitempty"`
	Color      string    `json:"color,omitempty"`
	Opacity    float64   `json:"opacity,omitempty"`
	Wireframe  bool      `json:"wireframe,omitempty"`
	BlendMode  string    `json:"blendMode,omitempty"`
	RenderPass string    `json:"renderPass,omitempty"`
	ShaderData []float64 `json:"shaderData,omitempty"`
	Emissive   float64   `json:"emissive,omitempty"`
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
}

// RenderBundle is the renderer-facing scene payload emitted by the shared
// engine runtime for a single frame.
type RenderBundle struct {
	Background       string             `json:"background,omitempty"`
	Camera           RenderCamera       `json:"camera,omitempty"`
	Materials        []RenderMaterial   `json:"materials,omitempty"`
	Objects          []RenderObject     `json:"objects,omitempty"`
	Passes           []RenderPassBundle `json:"passes,omitempty"`
	Lines            []RenderLine       `json:"lines,omitempty"`
	Labels           []RenderLabel      `json:"labels,omitempty"`
	Positions        []float64          `json:"positions,omitempty"`
	Colors           []float64          `json:"colors,omitempty"`
	VertexCount      int                `json:"vertexCount,omitempty"`
	WorldPositions   []float64          `json:"worldPositions,omitempty"`
	WorldColors      []float64          `json:"worldColors,omitempty"`
	WorldVertexCount int                `json:"worldVertexCount,omitempty"`
	ObjectCount      int                `json:"objectCount,omitempty"`
}
