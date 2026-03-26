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

// RenderCamera describes the camera used for world-space rendering.
type RenderCamera struct {
	X   float64 `json:"x,omitempty"`
	Y   float64 `json:"y,omitempty"`
	Z   float64 `json:"z,omitempty"`
	FOV float64 `json:"fov,omitempty"`
}

// RenderBundle is the renderer-facing scene payload emitted by the shared
// engine runtime for a single frame.
type RenderBundle struct {
	Background       string       `json:"background,omitempty"`
	Camera           RenderCamera `json:"camera,omitempty"`
	Lines            []RenderLine `json:"lines,omitempty"`
	Positions        []float64    `json:"positions,omitempty"`
	Colors           []float64    `json:"colors,omitempty"`
	VertexCount      int          `json:"vertexCount,omitempty"`
	WorldPositions   []float64    `json:"worldPositions,omitempty"`
	WorldColors      []float64    `json:"worldColors,omitempty"`
	WorldVertexCount int          `json:"worldVertexCount,omitempty"`
	ObjectCount      int          `json:"objectCount,omitempty"`
}
