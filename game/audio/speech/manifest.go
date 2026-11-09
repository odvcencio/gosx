package speech

import (
	"encoding/json"
	"fmt"
	"strings"
)

// RenderManifest is the input to "gosx voice render": which voices speak,
// and which phrases to record in which pitch variants.
type RenderManifest struct {
	// Voices are keyed by a local name that phrases refer to.
	Voices map[string]Voice `json:"voices"`
	// Variants maps a variant name (start, mid, end) to the text template
	// sent to the voice, where {text} is the phrase text. Defaults:
	// start "{text},", mid "{text}", end "{text}.". The punctuation nudges a
	// text-to-speech voice toward a rising, level or falling finish; audition
	// the result, because voices differ.
	Variants map[string]string `json:"variants,omitempty"`
	Phrases  []RenderPhrase    `json:"phrases"`
}

// Voice configures one speaker.
type Voice struct {
	// Provider is "elevenlabs", "local" or "mock". Empty uses the tool default.
	Provider string `json:"provider,omitempty"`
	// VoiceID is the provider's voice identifier (an ElevenLabs voice ID, or a
	// `say` voice name for the local provider).
	VoiceID string `json:"voice_id,omitempty"`
	// Model is the provider's model name. Optional.
	Model string `json:"model,omitempty"`
	// Stability, Similarity, Style and Speed are provider voice settings.
	// Zero values mean "provider default".
	Stability  float64 `json:"stability,omitempty"`
	Similarity float64 `json:"similarity,omitempty"`
	Style      float64 `json:"style,omitempty"`
	Speed      float64 `json:"speed,omitempty"`
	// TargetLUFS overrides the tool's loudness target for this voice.
	TargetLUFS *float64 `json:"target_lufs,omitempty"`
}

// RenderPhrase is one fragment to record.
type RenderPhrase struct {
	// Key is the fragment key the runtime looks up (Compiled.Fragments).
	Key string `json:"key"`
	// Text is what the voice says.
	Text string `json:"text"`
	// Voice names an entry of RenderManifest.Voices.
	Voice string `json:"voice"`
	// Variants lists the pitch variants to render. Empty means all three.
	Variants []string `json:"variants,omitempty"`
	// Override replaces the text sent for one variant, for example to spell
	// out a name the voice mispronounces.
	Override map[string]string `json:"override,omitempty"`
}

// DefaultVariantTemplates are the text templates used when the manifest sets
// none.
var DefaultVariantTemplates = map[string]string{
	"start": "{text},",
	"mid":   "{text}",
	"end":   "{text}.",
}

// VariantNames lists the variant names in playback-position order.
var VariantNames = [...]string{"start", "mid", "end"}

// ParseVariant maps a name to a Pos.
func ParseVariant(name string) (Pos, bool) {
	switch name {
	case "start":
		return PosStart, true
	case "mid":
		return PosMid, true
	case "end":
		return PosEnd, true
	}
	return 0, false
}

// ParseRenderManifest decodes and validates a render manifest.
func ParseRenderManifest(data []byte) (*RenderManifest, error) {
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	var m RenderManifest
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("speech: render manifest: %w", err)
	}
	return &m, m.Validate()
}

// Validate checks references and names.
func (m *RenderManifest) Validate() error {
	if len(m.Voices) == 0 {
		return fmt.Errorf("speech: render manifest has no voices")
	}
	for name, tpl := range m.Variants {
		if _, ok := ParseVariant(name); !ok {
			return fmt.Errorf("speech: unknown variant %q (want start, mid or end)", name)
		}
		if !strings.Contains(tpl, "{text}") {
			return fmt.Errorf("speech: variant %q template lacks {text}", name)
		}
	}
	seen := map[string]bool{}
	for i, p := range m.Phrases {
		if p.Key == "" || strings.TrimSpace(p.Text) == "" {
			return fmt.Errorf("speech: phrase %d needs a key and text", i)
		}
		if seen[p.Key] {
			return fmt.Errorf("speech: duplicate phrase key %q", p.Key)
		}
		seen[p.Key] = true
		if _, ok := m.Voices[p.Voice]; !ok {
			return fmt.Errorf("speech: phrase %q uses unknown voice %q", p.Key, p.Voice)
		}
		for _, v := range p.Variants {
			if _, ok := ParseVariant(v); !ok {
				return fmt.Errorf("speech: phrase %q has unknown variant %q", p.Key, v)
			}
		}
		for v := range p.Override {
			if _, ok := ParseVariant(v); !ok {
				return fmt.Errorf("speech: phrase %q overrides unknown variant %q", p.Key, v)
			}
		}
	}
	return nil
}

// Request is one text-to-speech call the manifest implies.
type Request struct {
	Key     string
	Variant string
	Voice   string
	// Text is the exact text sent to the provider.
	Text string
}

// Requests expands the manifest to one Request per phrase and variant, in
// manifest order.
func (m *RenderManifest) Requests() []Request {
	var out []Request
	for _, p := range m.Phrases {
		names := p.Variants
		if len(names) == 0 {
			names = VariantNames[:]
		}
		for _, v := range names {
			text, ok := p.Override[v]
			if !ok {
				tpl := m.Variants[v]
				if tpl == "" {
					tpl = DefaultVariantTemplates[v]
				}
				text = strings.ReplaceAll(tpl, "{text}", p.Text)
			}
			out = append(out, Request{Key: p.Key, Variant: v, Voice: p.Voice, Text: text})
		}
	}
	return out
}

// RenderPhrases turns the grammar's fragments into render-manifest phrases
// for one voice. Merge the result with Voices and Variants to get a manifest.
func (c *Compiled) RenderPhrases(voice string) []RenderPhrase {
	out := make([]RenderPhrase, 0, len(c.frags))
	for _, f := range c.frags {
		out = append(out, RenderPhrase{Key: f.Key, Text: f.Text, Voice: voice})
	}
	return out
}

// ClipManifest describes the rendered clips. The render tool writes it and
// the runtime loads it.
type ClipManifest struct {
	Version int `json:"version"`
	// Rate is the sample rate of every atlas, in Hz.
	Rate int `json:"rate"`
	// Atlases are the audio files. Each holds many clips back to back.
	Atlases []Atlas     `json:"atlases"`
	Clips   []ClipEntry `json:"clips"`
	// Stats summarize the render: loudness per voice and so on.
	Voices map[string]VoiceStats `json:"voices,omitempty"`
}

// Atlas is one audio file of packed clips.
type Atlas struct {
	File  string `json:"file"`
	Voice string `json:"voice"`
	// Frames is the file length in sample frames.
	Frames int `json:"frames"`
	// SHA256 is the hex digest of the file, for cache busting and integrity.
	SHA256 string `json:"sha256,omitempty"`
}

// ClipEntry locates one recording inside an atlas.
type ClipEntry struct {
	Key     string `json:"key"`
	Variant string `json:"variant"`
	Atlas   int    `json:"atlas"`
	Offset  int    `json:"offset"`
	Frames  int    `json:"frames"`
}

// VoiceStats records what the render measured for one voice.
type VoiceStats struct {
	Clips           int     `json:"clips"`
	Characters      int     `json:"characters"`
	MeanLUFS        float64 `json:"mean_lufs"`
	MaxTruePeakDBTP float64 `json:"max_true_peak_dbtp"`
}

// ParseClipManifest decodes and validates a clip manifest.
func ParseClipManifest(data []byte) (*ClipManifest, error) {
	var m ClipManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("speech: clip manifest: %w", err)
	}
	if m.Version != 1 {
		return nil, fmt.Errorf("speech: clip manifest version %d is not supported", m.Version)
	}
	if m.Rate <= 0 {
		return nil, fmt.Errorf("speech: clip manifest has no sample rate")
	}
	for i, c := range m.Clips {
		if c.Atlas < 0 || c.Atlas >= len(m.Atlases) {
			return nil, fmt.Errorf("speech: clip %d names atlas %d of %d", i, c.Atlas, len(m.Atlases))
		}
		if _, ok := ParseVariant(c.Variant); !ok {
			return nil, fmt.Errorf("speech: clip %q has unknown variant %q", c.Key, c.Variant)
		}
		if c.Offset < 0 || c.Frames <= 0 || c.Offset+c.Frames > m.Atlases[c.Atlas].Frames {
			return nil, fmt.Errorf("speech: clip %q (%s) lies outside atlas %q", c.Key, c.Variant, m.Atlases[c.Atlas].File)
		}
	}
	return &m, nil
}

// Attach registers the manifest's clips in lib for the keys lib already
// holds (the grammar interned them) and returns how many it attached and the
// keys it ignored. It fails when the manifest's rate differs from the library's. handle builds the Clip.Handle for an entry, for example a
// region of a decoded atlas. Keys the grammar never asked for are skipped.
func (l *Library) Attach(m *ClipManifest, handle func(a *Atlas, c *ClipEntry) any) (attached int, ignored []string, err error) {
	if m.Rate != l.rate {
		return 0, nil, fmt.Errorf("speech: clip manifest rate %d Hz differs from the library's %d Hz", m.Rate, l.rate)
	}
	for i := range m.Clips {
		c := &m.Clips[i]
		id, ok := l.Lookup(c.Key)
		if !ok {
			ignored = append(ignored, c.Key)
			continue
		}
		pos, _ := ParseVariant(c.Variant)
		l.Set(id, pos, Clip{Frames: int32(c.Frames), Handle: handle(&m.Atlases[c.Atlas], c)})
		attached++
	}
	return attached, ignored, nil
}
