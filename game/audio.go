package game

import (
	"strconv"
	"strings"
)

const (
	// EventAudioPlay asks the client audio runtime to start a clip.
	EventAudioPlay = "audio:play"
	// EventAudioStop asks the client audio runtime to stop a playback handle
	// or all playbacks for a clip.
	EventAudioStop = "audio:stop"
)

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
	Clip   AssetID `json:"clip"`
	Bus    string  `json:"bus,omitempty"`
	Handle string  `json:"handle,omitempty"`
	Volume float64 `json:"volume,omitempty"`
	Rate   float64 `json:"rate,omitempty"`
	Pan    float64 `json:"pan,omitempty"`
	Loop   bool    `json:"loop,omitempty"`
}

// AudioManifestFromAssets converts registered audio assets into the client
// audio manifest shape.
func AudioManifestFromAssets(assets *Assets, buses ...AudioBus) AudioManifest {
	manifest := AudioManifest{
		MasterVolume: 1,
		Buses:        normalizeAudioBuses(buses),
	}
	for _, ref := range assets.ByKind(AssetAudio) {
		manifest.Clips = append(manifest.Clips, AudioClipFromAsset(ref))
	}
	return manifest
}

// AudioManifest returns the runtime's current audio manifest.
func (r *Runtime) AudioManifest(buses ...AudioBus) AudioManifest {
	if r == nil {
		return AudioManifest{MasterVolume: 1, Buses: normalizeAudioBuses(buses)}
	}
	return AudioManifestFromAssets(r.assets, buses...)
}

// AudioClipFromAsset converts an AssetAudio reference into a clip. Metadata
// keys "bus", "loop", "volume", and "rate" are honored when present.
func AudioClipFromAsset(ref AssetRef) AudioClip {
	clip := AudioClip{
		ID:          ref.ID,
		URI:         ref.URI,
		ContentType: ref.ContentType,
		Preload:     ref.Preload,
		Metadata:    cloneStringMap(ref.Metadata),
	}
	if clip.Metadata != nil {
		clip.Bus = strings.TrimSpace(clip.Metadata["bus"])
		clip.Loop = parseAudioBool(clip.Metadata["loop"])
		clip.Volume = parseAudioFloat(clip.Metadata["volume"])
		clip.Rate = parseAudioFloat(clip.Metadata["rate"])
	}
	return clip
}

// PlayAudio emits an audio play event from a system context.
func (ctx *Context) PlayAudio(clip AssetID, playback AudioPlayback) {
	if ctx == nil {
		return
	}
	playback.Clip = clip
	ctx.Emit(Event{Type: EventAudioPlay, Target: string(clip), Data: playback})
}

// StopAudio emits an audio stop event from a system context.
func (ctx *Context) StopAudio(target string) {
	if ctx == nil {
		return
	}
	target = strings.TrimSpace(target)
	ctx.Emit(Event{Type: EventAudioStop, Target: target, Data: map[string]string{"target": target}})
}

func normalizeAudioBuses(buses []AudioBus) []AudioBus {
	out := make([]AudioBus, 0, len(buses))
	seen := map[string]struct{}{}
	for _, bus := range buses {
		bus.ID = strings.TrimSpace(bus.ID)
		bus.Parent = strings.TrimSpace(bus.Parent)
		if bus.ID == "" {
			continue
		}
		if _, ok := seen[bus.ID]; ok {
			continue
		}
		seen[bus.ID] = struct{}{}
		out = append(out, bus)
	}
	return out
}

func parseAudioBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on", "loop":
		return true
	default:
		return false
	}
}

func parseAudioFloat(value string) float64 {
	parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil || parsed == 0 {
		return 0
	}
	return parsed
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
