package scene

// Vessel opts one model into local arcade sailing. Its model meshes must use
// local coordinates with the bow toward -Z and deck above Y=0. NodeID identifies
// the model; LODs identify alternative models in the same graph. Distances are
// metres, speeds metres/second, wind direction degrees toward which wind travels.
// The runtime owns these models' transforms while this contract is mounted.
type Vessel struct {
	NodeID        string      `json:"nodeId"`
	Position      Vector3     `json:"position"`
	Heading       float64     `json:"heading,omitempty"`       // yaw radians; 0 faces -Z
	Length        float64     `json:"length,omitempty"`        // default 22
	Beam          float64     `json:"beam,omitempty"`          // default 5
	Draft         float64     `json:"draft,omitempty"`         // default 1.4
	DeckHeight    float64     `json:"deckHeight,omitempty"`    // default 2.5
	Helm          Vector3     `json:"helm"`                    // local eye position; default (0.65,4.2,8)
	BoardRadius   float64     `json:"boardRadius,omitempty"`   // default 4
	WindDirection float64     `json:"windDirection,omitempty"` // fallback Ocean.WindDirection
	WindStrength  float64     `json:"windStrength,omitempty"`  // default 8 m/s
	SailTrim      float64     `json:"sailTrim,omitempty"`      // initial trim, default 0.55
	MaxSpeed      float64     `json:"maxSpeed,omitempty"`      // default 10
	Bounds        *WalkBounds `json:"bounds,omitempty"`        // otherwise uses Walk.Bounds
	LODs          []VesselLOD `json:"lods,omitempty"`
	WakeTexture   string      `json:"wakeTexture,omitempty"` // optional alpha foam image
	Wake          *bool       `json:"wake,omitempty"`        // default true
}

// VesselLOD switches visibility at Distance from the ship (not another fetch).
// Author ascending distances, excluding the full model in NodeID.
type VesselLOD struct {
	NodeID   string  `json:"nodeId"`
	Distance float64 `json:"distance"`
}

// WalkSurface adds a rectangular deck or ramp without modifying the seabed.
// Y is the centre height; slopes are rise/run along the rectangle's local axes.
// Highest overlapping surface wins. Collision footprints still use feet height.
type WalkSurface struct {
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	Z         float64 `json:"z"`
	SizeX     float64 `json:"sizeX"`
	SizeZ     float64 `json:"sizeZ"`
	RotationY float64 `json:"rotationY,omitempty"`
	SlopeX    float64 `json:"slopeX,omitempty"`
	SlopeZ    float64 `json:"slopeZ,omitempty"`
}
