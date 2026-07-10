package scene

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestAudioBusPropsDropsEmptyID(t *testing.T) {
	if got := (AudioBus{}).Props(); got != nil {
		t.Fatalf("expected nil for empty ID, got %+v", got)
	}
}

func TestAudioBusPropsFull(t *testing.T) {
	got := AudioBus{
		ID:     "sfx",
		Parent: "master",
		Volume: Float(0.75),
		Muted:  true,
	}.Props()
	want := map[string]any{
		"id":     "sfx",
		"parent": "master",
		"volume": 0.75,
		"muted":  true,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("AudioBus.Props() = %#v, want %#v", got, want)
	}
}

func TestAudioBusPropsVolumeNilOmitted(t *testing.T) {
	got := AudioBus{ID: "music"}.Props()
	want := map[string]any{"id": "music"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("AudioBus.Props() = %#v, want %#v", got, want)
	}
}

func TestAudioClipPropsDropsMissingIDOrSrc(t *testing.T) {
	if got := (AudioClip{Src: "hit.wav"}).Props(); got != nil {
		t.Fatalf("expected nil for missing ID, got %+v", got)
	}
	if got := (AudioClip{ID: "hit"}).Props(); got != nil {
		t.Fatalf("expected nil for missing Src, got %+v", got)
	}
}

func TestAudioClipPropsFull(t *testing.T) {
	got := AudioClip{
		ID:          "hit",
		Src:         "/audio/hit.mp3",
		ContentType: "audio/mpeg",
		Bus:         "sfx",
		Preload:     true,
		Loop:        false,
		Volume:      Float(0.9),
		Rate:        Float(1.1),
		Metadata:    map[string]any{"category": "impact"},
	}.Props()
	want := map[string]any{
		"id":          "hit",
		"src":         "/audio/hit.mp3",
		"contentType": "audio/mpeg",
		"bus":         "sfx",
		"preload":     true,
		"volume":      0.9,
		"rate":        1.1,
		"metadata":    map[string]any{"category": "impact"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("AudioClip.Props() = %#v, want %#v", got, want)
	}
}

func TestAudioManifestPropsEmpty(t *testing.T) {
	got := Audio{}.Props()
	if len(got) != 0 {
		t.Fatalf("expected empty manifest to lower to an empty map, got %#v", got)
	}
}

func TestAudioManifestPropsMutedIgnoredWithoutMasterVolume(t *testing.T) {
	// Documents the client-side quirk: Muted only takes effect when
	// MasterVolume is also set (gosxAudioRegisterManifest only
	// re-registers "master" when the manifest carries a masterVolume key).
	got := Audio{Muted: true}.Props()
	if _, ok := got["muted"]; ok {
		t.Fatalf("expected muted to be dropped without MasterVolume, got %#v", got)
	}
}

func TestAudioManifestPropsMasterVolumeAndMuted(t *testing.T) {
	got := Audio{MasterVolume: Float(0.5), Muted: true}.Props()
	want := map[string]any{"masterVolume": 0.5, "muted": true}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Audio.Props() = %#v, want %#v", got, want)
	}
}

func TestAudioManifestPropsBusesAndClips(t *testing.T) {
	manifest := Audio{
		Buses: []AudioBus{
			{ID: "master", Volume: Float(1)},
			{}, // dropped: empty ID
			{ID: "sfx", Volume: Float(0.6)},
		},
		Clips: []AudioClip{
			{ID: "hit", Src: "/hit.mp3"},
			{Src: "/missing-id.mp3"}, // dropped: empty ID
		},
	}
	got := manifest.Props()
	buses, _ := got["buses"].([]map[string]any)
	if len(buses) != 2 {
		t.Fatalf("expected 2 buses (empty-ID bus dropped), got %d: %#v", len(buses), buses)
	}
	clips, _ := got["clips"].([]map[string]any)
	if len(clips) != 1 {
		t.Fatalf("expected 1 clip (empty-ID clip dropped), got %d: %#v", len(clips), clips)
	}
}

func TestADSRPropsZeroValueIsNil(t *testing.T) {
	if got := (ADSR{}).Props(); got != nil {
		t.Fatalf("expected nil for zero-value ADSR, got %+v", got)
	}
}

func TestADSRPropsFull(t *testing.T) {
	got := ADSR{Attack: 0.01, Decay: 0.05, Sustain: 0.6, Release: 0.2}.Props()
	want := map[string]any{
		"attack":  0.01,
		"decay":   0.05,
		"sustain": 0.6,
		"release": 0.2,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ADSR.Props() = %#v, want %#v", got, want)
	}
}

func TestSynthPatchPropsLayers(t *testing.T) {
	patch := SynthPatch{
		Name: "power-up",
		Tones: []ToneLayer{
			{Frequency: 440, Duration: 0.05, Waveform: WaveSquare, Gain: Float(0.5)},
		},
		Sweeps: []SweepLayer{
			{StartFrequency: 200, EndFrequency: 900, Duration: 0.2, Waveform: WaveSawtooth,
				Envelope: &ADSR{Attack: 0.01, Release: 0.1}},
		},
		Noises: []NoiseLayer{
			{Duration: 0.08, FilterType: FilterHighpass, FilterFrequency: 1500},
		},
	}
	got := patch.Props()

	wantTones := []map[string]any{
		{"frequency": 440.0, "duration": 0.05, "waveform": "square", "gain": 0.5},
	}
	if !reflect.DeepEqual(got["tones"], wantTones) {
		t.Fatalf("tones = %#v, want %#v", got["tones"], wantTones)
	}

	wantSweeps := []map[string]any{
		{
			"startFrequency": 200.0,
			"endFrequency":   900.0,
			"duration":       0.2,
			"waveform":       "sawtooth",
			"envelope":       map[string]any{"attack": 0.01, "release": 0.1},
		},
	}
	if !reflect.DeepEqual(got["sweeps"], wantSweeps) {
		t.Fatalf("sweeps = %#v, want %#v", got["sweeps"], wantSweeps)
	}

	wantNoises := []map[string]any{
		{"duration": 0.08, "filterType": "highpass", "filterFrequency": 1500.0},
	}
	if !reflect.DeepEqual(got["noises"], wantNoises) {
		t.Fatalf("noises = %#v, want %#v", got["noises"], wantNoises)
	}

	if got["name"] != "power-up" {
		t.Fatalf("name = %#v, want power-up", got["name"])
	}
}

func TestAudioCuePropsBaselineFieldsAlwaysPresent(t *testing.T) {
	got := AudioCue{}.Props()
	want := map[string]any{
		"seq":       uint32(0),
		"cue":       "",
		"phaseCue":  "",
		"intensity": 0.0,
		"pan":       0.0,
		"depth":     0.0,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("AudioCue{}.Props() = %#v, want %#v", got, want)
	}
}

// TestAudioCuePropsMatchesFightDemoSchema pins the field-for-field shape
// the embedded-tick-field delivery path relies on (see AudioCue's doc):
// a fighting-game demo already hand-rolls this exact
// {seq,cue,phaseCue,intensity,pan,depth,bus} shape as its per-tick "audio"
// field, consumed unmodified by the client's hub input controller.
func TestAudioCuePropsMatchesFightDemoSchema(t *testing.T) {
	cue := AudioCue{
		Seq:       42,
		Cue:       "hit_heavy",
		PhaseCue:  "fight",
		Intensity: 0.8,
		Pan:       -0.3,
		Depth:     0.1,
		Bus:       "sfx",
	}
	got := cue.Props()
	want := map[string]any{
		"seq":       uint32(42),
		"cue":       "hit_heavy",
		"phaseCue":  "fight",
		"intensity": 0.8,
		"pan":       -0.3,
		"depth":     0.1,
		"bus":       "sfx",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("AudioCue.Props() = %#v, want %#v", got, want)
	}
}

func TestAudioCuePropsClipPathFields(t *testing.T) {
	cue := AudioCue{
		Seq:      7,
		Clip:     "victory-fanfare",
		Position: &Vector3{X: 1, Y: 2, Z: 3},
		Volume:   Float(0.4),
		Rate:     1.2,
		Loop:     true,
		Handle:   "music-1",
	}
	got := cue.Props()
	want := map[string]any{
		"seq":       uint32(7),
		"cue":       "",
		"phaseCue":  "",
		"intensity": 0.0,
		"pan":       0.0,
		"depth":     0.0,
		"clip":      "victory-fanfare",
		"position":  map[string]any{"x": 1.0, "y": 2.0, "z": 3.0},
		"volume":    0.4,
		"rate":      1.2,
		"loop":      true,
		"handle":    "music-1",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("AudioCue.Props() = %#v, want %#v", got, want)
	}
}

func TestAudioCuePropsPatchField(t *testing.T) {
	cue := AudioCue{
		Cue: "custom-stinger",
		Patch: &SynthPatch{
			Tones: []ToneLayer{{Frequency: 660, Duration: 0.03}},
		},
	}
	got := cue.Props()
	patch, ok := got["patch"].(map[string]any)
	if !ok {
		t.Fatalf("expected patch map in cue props, got %#v", got["patch"])
	}
	tones, ok := patch["tones"].([]map[string]any)
	if !ok || len(tones) != 1 {
		t.Fatalf("expected one tone layer, got %#v", patch["tones"])
	}
	if tones[0]["frequency"] != 660.0 {
		t.Fatalf("frequency = %#v, want 660", tones[0]["frequency"])
	}
}

// --- Golden tests: struct -> props map -> canonical JSON, pinned to disk. ---

func goldenJSON(t *testing.T, name string, value any) {
	t.Helper()
	got, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	var tree any
	if err := json.Unmarshal(got, &tree); err != nil {
		t.Fatalf("round-trip unmarshal failed: %v\nraw: %s", err, got)
	}
	canonical, err := json.MarshalIndent(tree, "", "  ")
	if err != nil {
		t.Fatalf("round-trip remarshal failed: %v", err)
	}
	canonical = append(canonical, '\n')

	goldenPath := filepath.Join("testdata", name)
	if _, err := os.Stat(goldenPath); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(goldenPath, canonical, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("created golden: %s (%d bytes)", goldenPath, len(canonical))
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if string(canonical) != string(want) {
		actualPath := goldenPath + ".actual"
		_ = os.WriteFile(actualPath, canonical, 0o644)
		t.Fatalf("%s drifted from golden — diff %s against %s", name, goldenPath, actualPath)
	}
}

func TestAudioManifestPropsGolden(t *testing.T) {
	manifest := Audio{
		Buses: []AudioBus{
			{ID: "master", Volume: Float(1)},
			{ID: "sfx", Parent: "master", Volume: Float(0.8)},
			{ID: "music", Parent: "master", Volume: Float(0.5), Muted: true},
		},
		Clips: []AudioClip{
			{
				ID: "hit", Src: "/audio/hit.mp3", ContentType: "audio/mpeg",
				Bus: "sfx", Preload: true, Volume: Float(0.9), Rate: Float(1),
			},
			{
				ID: "theme", Src: "/audio/theme.ogg", Bus: "music",
				Loop: true, Volume: Float(0.7),
			},
		},
		MasterVolume: Float(1),
	}
	goldenJSON(t, "audio_manifest_golden.json", manifest.Props())
}

func TestAudioCueGoldenEmbeddedTickField(t *testing.T) {
	cue := AudioCue{
		Seq:       128,
		Cue:       "launcher",
		PhaseCue:  "fight",
		Intensity: 0.72,
		Pan:       0.25,
		Depth:     -0.1,
		Bus:       "sfx",
	}
	goldenJSON(t, "audio_cue_tick_golden.json", cue.Props())
}

func TestAudioCueGoldenSynthPatch(t *testing.T) {
	cue := AudioCue{
		Seq: 9,
		Cue: "beast-roar",
		Patch: &SynthPatch{
			Name: "beast-roar",
			Tones: []ToneLayer{
				{Frequency: 90, Duration: 0.4, Waveform: WaveSawtooth, Gain: Float(0.8),
					Envelope: &ADSR{Attack: 0.02, Decay: 0.1, Sustain: 0.6, Release: 0.3}},
			},
			Sweeps: []SweepLayer{
				{StartFrequency: 220, EndFrequency: 60, Duration: 0.35, Waveform: WaveSquare, Pan: Float(-0.2)},
			},
			Noises: []NoiseLayer{
				{Duration: 0.15, FilterType: FilterLowpass, FilterFrequency: 500, DelayMS: 40},
			},
		},
		Intensity: 1.1,
		Pan:       0,
		Depth:     0.4,
	}
	goldenJSON(t, "audio_cue_patch_golden.json", cue.Props())
}
