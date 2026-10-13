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
	QuickBoard    bool        `json:"quickBoard,omitempty"`    // show a button that takes the helm from anywhere; E still requires proximity
	WindDirection float64     `json:"windDirection,omitempty"` // fallback Ocean.WindDirection
	WindStrength  float64     `json:"windStrength,omitempty"`  // default 8 m/s
	SailTrim      float64     `json:"sailTrim,omitempty"`      // initial trim, default 0.55
	MaxSpeed      float64     `json:"maxSpeed,omitempty"`      // default 10
	Bounds        *WalkBounds `json:"bounds,omitempty"`        // otherwise uses Walk.Bounds unless OpenWater
	OpenWater     bool        `json:"openWater,omitempty"`     // sail beyond Walk.Bounds; terrain and collider checks still apply
	Navigation    bool        `json:"navigation,omitempty"`    // opt into course holding and scoped vessel command/state events
	LODs          []VesselLOD `json:"lods,omitempty"`
	WakeTexture   string      `json:"wakeTexture,omitempty"` // optional alpha foam image
	Wake          *bool       `json:"wake,omitempty"`        // default true
}

// VesselCommandEvent accepts a VesselCommand on the mounted Scene3D element.
// Navigation is opt-in; commands are local input, not authoritative network state.
const VesselCommandEvent = "gosx:scene3d:vessel-command"

// VesselStateEvent bubbles a VesselState at five Hz, plus explicit commands.
const VesselStateEvent = "gosx:scene3d:vessel-state"

// VesselCommand controls the configured vessel without taking ownership of its
// transforms. Actions are helm, course, release-course and trim. Course Bearing
// is a compass heading in degrees [0,360]; Trim is a fraction in [0,1].
type VesselCommand struct {
	NodeID  string  `json:"nodeId"`
	Action  string  `json:"action"`
	Bearing float64 `json:"bearing"`
	Trim    float64 `json:"trim"`
}

// VesselState is a snapshot of local navigation, not trusted multiplayer state.
// Bearing and WindFrom are compass degrees; Course is nil when steering freely.
// Position and Distance are metres, Speed and WindStrength metres per second.
type VesselState struct {
	NodeID       string   `json:"nodeId"`
	Mode         string   `json:"mode"` // moored, deck or sailing
	Position     Vector3  `json:"position"`
	Bearing      float64  `json:"bearing"`
	Speed        float64  `json:"speed"`
	Trim         float64  `json:"trim"`
	Distance     float64  `json:"distance"`
	Grounded     bool     `json:"grounded"`
	WindFrom     float64  `json:"windFrom"`
	WindStrength float64  `json:"windStrength"`
	Course       *float64 `json:"course"`
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
