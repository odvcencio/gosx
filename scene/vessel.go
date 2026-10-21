package scene

// Vessel opts one model into local arcade sailing. Its model meshes must use
// local coordinates with the bow toward -Z and deck above Y=0. NodeID identifies
// the model; LODs identify alternative models in the same graph. Distances are
// metres, speeds metres/second, wind direction degrees toward which wind travels.
// The runtime owns these models' transforms while this contract is mounted.
type Vessel struct {
	ControlsAnchorID string `json:"controlsAnchorId,omitempty"` // reserve space above this authored HUD element

	// Shared signals connect GoSX islands to the local vessel authority.
	CommandSignal     string          `json:"commandSignal,omitempty"`
	StateSignal       string          `json:"stateSignal,omitempty"`
	DestinationSignal string          `json:"destinationSignal,omitempty"`
	Routes            []VesselRoute   `json:"routes,omitempty"`
	NodeID            string          `json:"nodeId"`
	Position          Vector3         `json:"position"`
	Heading           float64         `json:"heading,omitempty"`       // yaw radians; 0 faces -Z
	Length            float64         `json:"length,omitempty"`        // default 22
	Beam              float64         `json:"beam,omitempty"`          // default 5
	Draft             float64         `json:"draft,omitempty"`         // default 1.4
	DeckHeight        float64         `json:"deckHeight,omitempty"`    // default 2.5
	Helm              Vector3         `json:"helm"`                    // local eye position; default (0.65,4.2,8)
	BoardRadius       float64         `json:"boardRadius,omitempty"`   // default 4
	QuickBoard        bool            `json:"quickBoard,omitempty"`    // show a button that takes the helm from anywhere; E still requires proximity
	WindDirection     float64         `json:"windDirection,omitempty"` // fallback Ocean.WindDirection
	WindStrength      float64         `json:"windStrength,omitempty"`  // default 8 m/s
	SailTrim          float64         `json:"sailTrim,omitempty"`      // initial trim, default 0.55
	MaxSpeed          float64         `json:"maxSpeed,omitempty"`      // default 10
	Bounds            *WalkBounds     `json:"bounds,omitempty"`        // otherwise uses Walk.Bounds unless OpenWater
	OpenWater         bool            `json:"openWater,omitempty"`     // sail beyond Walk.Bounds; terrain and collider checks still apply
	Navigation        bool            `json:"navigation,omitempty"`    // opt into course holding and scoped vessel command/state events
	Landings          []VesselLanding `json:"landings,omitempty"`      // safe foot positions at reachable piers
	LODs              []VesselLOD     `json:"lods,omitempty"`
	WakeTexture       string          `json:"wakeTexture,omitempty"` // optional alpha foam image
	Wake              *bool           `json:"wake,omitempty"`        // default true

	// ExternalPose hands the pose to Go. The runtime then runs no sailing
	// physics for this vessel: it shows the pose that DispatchVesselPose sends
	// (for example from package vessel), blended between ticks, and still
	// carries the walker, the model parts, parented crowds and the wake.
	// Wind, sail trim, helm, navigation and landings are ignored. Position and
	// Heading give the pose until the first frame arrives.
	ExternalPose bool `json:"externalPose,omitempty"`

	// Decks replaces the single walk rectangle with these walkable levels in
	// ship-local coordinates. The walker stands on the highest deck at or just
	// above their feet, so a rolling ship keeps people on the deck they chose.
	// Leave it empty for the rectangle.
	Decks []VesselDeck `json:"decks,omitempty"`
}

// VesselCommandEvent accepts a VesselCommand on the mounted Scene3D element.
// Navigation is opt-in; commands are local input, not authoritative network state.
const VesselCommandEvent = "gosx:scene3d:vessel-command"

// VesselStateEvent bubbles a VesselState at five Hz, plus explicit commands.
const VesselStateEvent = "gosx:scene3d:vessel-state"

// VesselCommand controls the configured vessel without taking ownership of its
// transforms. Actions are helm, course, release-course, center-view, trim and land. Land
// stops the ship and steps ashore only near a configured landing at low speed.
// Course Bearing
// is a compass heading in degrees [0,360]; Trim is a fraction in [0,1].
type VesselCommand struct {
	BearingInput string `json:"bearingInput,omitempty"` // optional numeric form input, validated within 0..360

	Revision int     `json:"revision,omitempty"`
	NodeID   string  `json:"nodeId"`
	Action   string  `json:"action"`
	Bearing  float64 `json:"bearing"`
	Trim     float64 `json:"trim"`
}

// VesselRoute describes a surveyed landing and safe approach. Departure is
// used only while within DepartureRadius of the initial mooring. Approach is
// omitted once the vessel has crossed ApproachBeforeZ toward open water.
type VesselRoute struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Position        Vector3   `json:"position"`
	Departure       []Vector3 `json:"departure,omitempty"`
	DepartureRadius float64   `json:"departureRadius,omitempty"`
	Approach        []Vector3 `json:"approach,omitempty"`
	ApproachBeforeZ float64   `json:"approachBeforeZ,omitempty"`
}

// NavigationState is the vessel snapshot projected for declarative chart UI.
// Heading values remain available in VesselState for simulations and tools.
type NavigationState struct {
	Ready, Sailing, Following, Held, CanLand                           bool
	BearingLabel, SpeedLabel, WindLabel, DistanceLabel                 string
	HelmLabel, SailLabel, HoldLabel, LandingLabel, Status, Destination string
	MapTransform, MapViewBox                                           string
	Trim                                                               float64
}

// VesselState is a snapshot of local navigation, not trusted multiplayer state.
// Bearing and WindFrom are compass degrees; Course is nil when steering freely.
// Position and Distance are metres, Speed and WindStrength metres per second.
type VesselState struct {
	NodeID       string   `json:"nodeId"`
	Mode         string   `json:"mode"`      // moored, deck or sailing
	Following    bool     `json:"following"` // camera follows the helm; false while viewing or walking elsewhere
	Position     Vector3  `json:"position"`
	Bearing      float64  `json:"bearing"`
	Speed        float64  `json:"speed"`
	Trim         float64  `json:"trim"`
	Distance     float64  `json:"distance"`
	Grounded     bool     `json:"grounded"`
	WindFrom     float64  `json:"windFrom"`
	WindStrength float64  `json:"windStrength"`
	Course       *float64 `json:"course"`
	Landing      *string  `json:"landing,omitempty"` // available landing ID; absent while distant or moving quickly
}

// VesselLanding supplies a safe eye position on a pier. Radius defaults to
// 28 metres and MaxSpeed to 1.5 m/s. It does not teleport or reset the vessel.
type VesselLanding struct {
	ID       string  `json:"id"`
	Position Vector3 `json:"position"`
	Radius   float64 `json:"radius,omitempty"`
	MaxSpeed float64 `json:"maxSpeed,omitempty"`
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
