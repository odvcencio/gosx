package scene

// VesselCutaway shows a vessel as a dollhouse: everything above one deck is
// hidden (or ghosted), and the hull can be sliced open with a plane. It needs
// Vessel.Decks to name decks, or Height to name a level directly.
//
// The cut follows the ship's roll and pitch, because its planes are fixed to the
// ship, not to the world. Model parts of the vessel are clipped. A crowd whose
// ParentNode is the vessel loses every agent on the cut side, so people on a
// hidden deck are not drawn and do not cast shadows. The clipped model parts stop
// casting shadows while the cutaway is on.
//
// Only the WebGL renderer applies it today. On WebGPU the vessel stays whole and
// the mount reports data-gosx-scene3d-cutaway="unsupported".
type VesselCutaway struct {
	// Enabled switches the cutaway on. A zero VesselCutaway shows the whole ship.
	Enabled bool `json:"enabled"`
	// Deck is the index into Vessel.Decks of the highest deck to show. Everything
	// above that deck's ceiling is cut.
	Deck int `json:"deck"`
	// Height overrides Deck with a ship-local height in metres (Y up from the
	// waterline origin). Nil uses Deck.
	Height *float64 `json:"height,omitempty"`
	// Above is "hide" (the default) or "fade". Fade ghosts the model parts above
	// the cut with a screen-door pattern that keeps FadeAlpha of the pixels.
	// Crowd agents above the cut are culled either way.
	Above     string  `json:"above,omitempty"`
	FadeAlpha float64 `json:"fadeAlpha,omitempty"` // 0..1, default 0.15
	// Section slices the hull open along a plane. Nil leaves the hull whole.
	Section *VesselSection `json:"section,omitempty"`
}

// VesselSection removes one side of a ship-local plane. Axis "x" cuts across the
// beam (positive X is starboard); axis "z" cuts along the length (positive Z is
// aft). Position is metres along that axis. Side +1 removes the part with
// larger coordinates, side -1 the part with smaller ones. Section {x, 0, +1}
// removes the starboard half and shows the interior from the starboard side.
type VesselSection struct {
	Axis     string  `json:"axis"`
	Position float64 `json:"position"`
	Side     int     `json:"side"`
}
