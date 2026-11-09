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
	// Packs declares the voice packs clips are bundled into. A phrase names its
	// pack; phrases that name none go to the pack "default", which is created
	// on demand as a bundled pack with no budget.
	Packs   map[string]PackSpec `json:"packs,omitempty"`
	Phrases []RenderPhrase      `json:"phrases"`
}

// Pack modes.
const (
	// PackBundled packs ship inside the game and load at boot (for example a
	// small PA pack).
	PackBundled = "bundled"
	// PackStream packs load on demand (a booth voice pack, or a name pack for
	// one matchup).
	PackStream = "stream"
)

// PackSpec describes one voice pack.
type PackSpec struct {
	// Mode is "bundled" (default) or "stream".
	Mode string `json:"mode,omitempty"`
	// BudgetBytes caps the pack's audio files. Render fails when a pack is
	// over its budget. Zero means no cap.
	BudgetBytes int64 `json:"budget_bytes,omitempty"`
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
	// Pack names the voice pack this phrase ships in. Default "default".
	Pack string `json:"pack,omitempty"`
}

// PackName returns the phrase's pack, defaulting to "default".
func (p RenderPhrase) PackName() string {
	if p.Pack == "" {
		return "default"
	}
	return p.Pack
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
	for name, p := range m.Packs {
		if name == "" || strings.ContainsAny(name, "/\\.") {
			return fmt.Errorf("speech: pack name %q must be a plain name", name)
		}
		if p.Mode != "" && p.Mode != PackBundled && p.Mode != PackStream {
			return fmt.Errorf("speech: pack %q has unknown mode %q", name, p.Mode)
		}
		if p.BudgetBytes < 0 {
			return fmt.Errorf("speech: pack %q has a negative budget", name)
		}
	}
	seen := map[string]bool{}
	stems := map[string]string{}
	for i, p := range m.Phrases {
		if pk := p.PackName(); strings.ContainsAny(pk, "/\\.") {
			return fmt.Errorf("speech: phrase %q has pack name %q; use a plain name", p.Key, pk)
		} else if _, ok := m.Packs[pk]; !ok && p.Pack != "" && len(m.Packs) > 0 {
			return fmt.Errorf("speech: phrase %q uses undeclared pack %q", p.Key, pk)
		}
		if p.Key == "" || strings.TrimSpace(p.Text) == "" {
			return fmt.Errorf("speech: phrase %d needs a key and text", i)
		}
		if seen[p.Key] {
			return fmt.Errorf("speech: duplicate phrase key %q", p.Key)
		}
		seen[p.Key] = true
		stem := Stem(p.Key)
		if stem == "" {
			return fmt.Errorf("speech: phrase key %q has no letters or digits", p.Key)
		}
		if other, dup := stems[stem]; dup {
			return fmt.Errorf("speech: phrase keys %q and %q give the same file stem %q", other, p.Key, stem)
		}
		stems[stem] = p.Key
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

// Stem turns a fragment key into a file-name stem: lower case, every run of
// other characters becomes one underscore. "lit/takes the lead" becomes
// "lit_takes_the_lead". Recorded takes and imported files are named by stem.
func Stem(key string) string {
	var b strings.Builder
	under := true
	for _, r := range strings.ToLower(key) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			under = false
		} else if !under {
			b.WriteByte('_')
			under = true
		}
	}
	return strings.TrimRight(b.String(), "_")
}

// Request is one recording the manifest implies: a phrase in one pitch
// variant, as text for a provider or as a line for a speaker to read.
type Request struct {
	Key     string
	Variant string
	Voice   string
	Pack    string
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
			out = append(out, Request{Key: p.Key, Variant: v, Voice: p.Voice, Pack: p.PackName(), Text: text})
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

// PackIndex lists the packs a render produced. The runtime reads it to decide
// what to load at boot (bundled) and what to fetch later (stream).
type PackIndex struct {
	Version int         `json:"version"`
	Packs   []PackEntry `json:"packs"`
}

// PackEntry is one pack in the index.
type PackEntry struct {
	Name string `json:"name"`
	Mode string `json:"mode"`
	// Manifest is the pack's clip manifest path, relative to the index.
	Manifest string `json:"manifest"`
	// Bytes is the total size of the pack's audio files.
	Bytes       int64 `json:"bytes"`
	BudgetBytes int64 `json:"budget_bytes,omitempty"`
	Clips       int   `json:"clips"`
}

// ParsePackIndex decodes the pack index a render wrote.
func ParsePackIndex(data []byte) (*PackIndex, error) {
	var idx PackIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("speech: pack index: %w", err)
	}
	if idx.Version != 1 {
		return nil, fmt.Errorf("speech: pack index version %d is not supported", idx.Version)
	}
	return &idx, nil
}

// Bundled returns the packs that ship with the game and load at boot.
func (p *PackIndex) Bundled() []PackEntry {
	var out []PackEntry
	for _, e := range p.Packs {
		if e.Mode != PackStream {
			out = append(out, e)
		}
	}
	return out
}

// Find returns the pack called name.
func (p *PackIndex) Find(name string) (PackEntry, bool) {
	for _, e := range p.Packs {
		if e.Name == name {
			return e, true
		}
	}
	return PackEntry{}, false
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
