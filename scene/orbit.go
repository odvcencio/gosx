package scene

import "encoding/json"

// Orbit is the target and limits of the built-in orbit controls
// (Props.Controls = ControlOrbit). Mount-time Props carry them as ControlTarget,
// ControlMinDistance, ControlMaxDistance and ControlPitchLimit. Put an Orbit on a
// camera (PerspectiveCamera.Orbit, IRCamera.Orbit) or send it with
// engine/wasm Scene3D.SetOrbit to change them after mount: the camera keeps its
// pose, the controls take the new target, and the player's drag orbits around it.
//
// Target is always sent, so the origin is a valid target. A zero MinDistance,
// MaxDistance or PitchLimit (radians) leaves that limit as it is.
type Orbit struct {
	Target      Vector3
	MinDistance float64
	MaxDistance float64
	PitchLimit  float64
}

type orbitWire struct {
	Target struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
		Z float64 `json:"z"`
	} `json:"target"`
	MinDistance float64 `json:"minDistance,omitempty"`
	MaxDistance float64 `json:"maxDistance,omitempty"`
	PitchLimit  float64 `json:"pitchLimit,omitempty"`
}

// MarshalJSON writes the target's three coordinates even when they are zero.
func (o Orbit) MarshalJSON() ([]byte, error) {
	wire := orbitWire{MinDistance: o.MinDistance, MaxDistance: o.MaxDistance, PitchLimit: o.PitchLimit}
	wire.Target.X, wire.Target.Y, wire.Target.Z = o.Target.X, o.Target.Y, o.Target.Z
	return json.Marshal(wire)
}

// UnmarshalJSON reads what MarshalJSON writes.
func (o *Orbit) UnmarshalJSON(data []byte) error {
	var wire orbitWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	*o = Orbit{Target: Vec3(wire.Target.X, wire.Target.Y, wire.Target.Z), MinDistance: wire.MinDistance, MaxDistance: wire.MaxDistance, PitchLimit: wire.PitchLimit}
	return nil
}

func (o *Orbit) clone() *Orbit {
	if o == nil {
		return nil
	}
	c := *o
	return &c
}
