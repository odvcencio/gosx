package scene

import (
	"fmt"
	"math"
	"strings"
)

// Optics declares a perspective instrument controlled by a GoSX shared signal.
// InputSignal holds a boolean: true raises the instrument, false puts it away.
// OutputSignal receives OpticsState. The Scene3D engine keeps camera and vessel
// ownership; magnification changes only the projection field of view.
type Optics struct {
	Magnification float64    `json:"magnification,omitempty"` // default 3; bounded 1..8
	InputSignal   string     `json:"inputSignal,omitempty"`
	OutputSignal  string     `json:"outputSignal,omitempty"`
	Landmarks     []Landmark `json:"landmarks,omitempty"`
}

// Landmark is a surveyed world position, shared by instruments and navigation.
// A bearing cue describes the survey direction, not an occlusion/visibility test.
type Landmark struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Position Vector3 `json:"position"`
	Landing  bool    `json:"landing,omitempty"`
}

// OpticsState is a read-only snapshot suitable for declarative island bindings.
type OpticsState struct {
	Ready        bool
	Enabled      bool
	Factor       float64
	Bearing      float64
	BearingLabel string
	LandmarkID   string
	LandmarkName string
	Range        float64
	RangeLabel   string
	CanNavigate  bool
}

func (o Optics) Validate() error {
	finite := func(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
	if !finite(o.Magnification) || o.Magnification != 0 && (o.Magnification < 1 || o.Magnification > 8) {
		return fmt.Errorf("scene optics magnification must be finite and within 1..8")
	}
	if len(o.Landmarks) > 256 {
		return fmt.Errorf("scene optics supports at most 256 surveyed landmarks")
	}
	seen := make(map[string]bool, len(o.Landmarks))
	for _, place := range o.Landmarks {
		if strings.TrimSpace(place.ID) == "" || strings.TrimSpace(place.Name) == "" || seen[place.ID] {
			return fmt.Errorf("scene optics landmarks need unique IDs and names")
		}
		if !finite(place.Position.X) || !finite(place.Position.Y) || !finite(place.Position.Z) {
			return fmt.Errorf("scene optics landmarks need finite world positions")
		}
		seen[place.ID] = true
	}
	return nil
}
