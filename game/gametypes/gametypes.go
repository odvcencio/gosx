package gametypes

// AssetID is a stable asset handle.
type AssetID string

// Vec3 is a small vector component for common simulation state.
type Vec3 struct {
	X float64 `json:"x,omitempty"`
	Y float64 `json:"y,omitempty"`
	Z float64 `json:"z,omitempty"`
}

// AudioBus is a named mixer bus for music, SFX, voice, UI, or game-specific
// stems.
type AudioBus struct {
	ID     string  `json:"id"`
	Parent string  `json:"parent,omitempty"`
	Volume float64 `json:"volume,omitempty"`
	Muted  bool    `json:"muted,omitempty"`
}

// AudioClip is the runtime-facing audio asset contract.
type AudioClip struct {
	ID          AssetID           `json:"id"`
	URI         string            `json:"uri"`
	ContentType string            `json:"contentType,omitempty"`
	Bus         string            `json:"bus,omitempty"`
	Preload     bool              `json:"preload,omitempty"`
	Loop        bool              `json:"loop,omitempty"`
	Volume      float64           `json:"volume,omitempty"`
	Rate        float64           `json:"rate,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// AudioManifest describes the clips and buses a client should register.
type AudioManifest struct {
	MasterVolume float64     `json:"masterVolume,omitempty"`
	Buses        []AudioBus  `json:"buses,omitempty"`
	Clips        []AudioClip `json:"clips,omitempty"`
}

// AudioPlayback configures one play request.
type AudioPlayback struct {
	Clip          AssetID `json:"clip"`
	Bus           string  `json:"bus,omitempty"`
	Handle        string  `json:"handle,omitempty"`
	Volume        float64 `json:"volume,omitempty"`
	Rate          float64 `json:"rate,omitempty"`
	Pan           float64 `json:"pan,omitempty"`
	Loop          bool    `json:"loop,omitempty"`
	Position      *Vec3   `json:"position,omitempty"`
	Velocity      *Vec3   `json:"velocity,omitempty"`
	RefDistance   float64 `json:"refDistance,omitempty"`
	MaxDistance   float64 `json:"maxDistance,omitempty"`
	RolloffFactor float64 `json:"rolloffFactor,omitempty"`
}
