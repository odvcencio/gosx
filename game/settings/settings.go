// Package settings holds the player's options: audio bus volumes, motion and
// camera-shake preferences, render scale, frame cap, quality tier, key
// bindings and language. The data is typed and versioned, validated on every
// change, saved through game/storage, and pushed to the game by apply hooks.
//
// A Manager clamps every value to a safe range, so a hand-edited or corrupt
// save cannot produce a zero render scale or a negative volume. A saved
// document with a newer version than the build understands is never
// overwritten: Load reports ErrTooNew and the defaults stay in memory only.
package settings

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"

	"m31labs.dev/gosx/game/actions"
	"m31labs.dev/gosx/game/storage"
)

// Audio bus names. They match the buses of the game/audio mixer.
const (
	BusMaster = "master"
	BusMusic  = "music"
	BusSFX    = "sfx"
	BusVoice  = "voice"
	BusUI     = "ui"
)

// Buses lists the bus names in display order.
var Buses = []string{BusMaster, BusMusic, BusSFX, BusVoice, BusUI}

// Volumes holds a gain in [0,1] for each bus.
type Volumes struct {
	Master float64 `json:"master"`
	Music  float64 `json:"music"`
	SFX    float64 `json:"sfx"`
	Voice  float64 `json:"voice"`
	UI     float64 `json:"ui"`
}

// Get returns the volume of a bus by name, or 1 for an unknown bus.
func (v Volumes) Get(bus string) float64 {
	switch bus {
	case BusMaster:
		return v.Master
	case BusMusic:
		return v.Music
	case BusSFX:
		return v.SFX
	case BusVoice:
		return v.Voice
	case BusUI:
		return v.UI
	}
	return 1
}

// With returns v with one bus changed. Unknown buses are ignored.
func (v Volumes) With(bus string, value float64) Volumes {
	switch bus {
	case BusMaster:
		v.Master = value
	case BusMusic:
		v.Music = value
	case BusSFX:
		v.SFX = value
	case BusVoice:
		v.Voice = value
	case BusUI:
		v.UI = value
	}
	return v
}

// Motion says whether to reduce motion.
type Motion string

const (
	// MotionSystem follows the operating system preference.
	MotionSystem Motion = "system"
	// MotionReduce always reduces motion.
	MotionReduce Motion = "reduce"
	// MotionFull never reduces motion.
	MotionFull Motion = "full"
)

// Quality is a render quality tier.
type Quality string

const (
	QualityAuto   Quality = "auto"
	QualityLow    Quality = "low"
	QualityMedium Quality = "medium"
	QualityHigh   Quality = "high"
	QualityUltra  Quality = "ultra"
)

// Settings is the whole options document.
type Settings struct {
	Version     int     `json:"version"`
	Volume      Volumes `json:"volume"`
	Motion      Motion  `json:"motion"`
	CameraShake float64 `json:"cameraShake"` // 0..1 multiplier on camera shake
	RenderScale float64 `json:"renderScale"` // 0.25..2, 1 is native
	FrameCap    int     `json:"frameCap"`    // frames per second; 0 means uncapped
	Quality     Quality `json:"quality"`
	Language    string  `json:"language"`
	// Bindings holds the player's changed key bindings (see actions.Map.Export).
	Bindings actions.SavedBindings `json:"bindings"`
	// Extra holds game-specific options as raw JSON, kept through a load and save.
	Extra map[string]json.RawMessage `json:"extra,omitempty"`
}

// Defaults returns the default settings at the given document version.
func Defaults(version int) Settings {
	return Settings{
		Version:     version,
		Volume:      Volumes{Master: 1, Music: 0.8, SFX: 1, Voice: 1, UI: 0.8},
		Motion:      MotionSystem,
		CameraShake: 1,
		RenderScale: 1,
		FrameCap:    0,
		Quality:     QualityAuto,
		Language:    "en",
		Bindings:    actions.SavedBindings{Version: actions.BindingsVersion},
	}
}

func clamp(v, lo, hi, fallback float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return fallback
	}
	return math.Max(lo, math.Min(hi, v))
}

// Valid returns s with every field forced into its allowed range. Unknown
// enum values become the default.
func (s Settings) Valid() Settings {
	d := Defaults(s.Version)
	s.Volume = Volumes{
		Master: clamp(s.Volume.Master, 0, 1, d.Volume.Master),
		Music:  clamp(s.Volume.Music, 0, 1, d.Volume.Music),
		SFX:    clamp(s.Volume.SFX, 0, 1, d.Volume.SFX),
		Voice:  clamp(s.Volume.Voice, 0, 1, d.Volume.Voice),
		UI:     clamp(s.Volume.UI, 0, 1, d.Volume.UI),
	}
	switch s.Motion {
	case MotionSystem, MotionReduce, MotionFull:
	default:
		s.Motion = d.Motion
	}
	switch s.Quality {
	case QualityAuto, QualityLow, QualityMedium, QualityHigh, QualityUltra:
	default:
		s.Quality = d.Quality
	}
	s.CameraShake = clamp(s.CameraShake, 0, 1, d.CameraShake)
	s.RenderScale = clamp(s.RenderScale, 0.25, 2, d.RenderScale)
	if s.FrameCap != 0 {
		s.FrameCap = int(clamp(float64(s.FrameCap), 15, 360, 0))
	}
	s.Language = strings.TrimSpace(s.Language)
	if s.Language == "" || len(s.Language) > 35 {
		s.Language = d.Language
	}
	if s.Bindings.Version == 0 {
		s.Bindings.Version = actions.BindingsVersion
	}
	return s
}

// Field is a bit set naming the settings that changed.
type Field uint16

// Settings fields, for apply hooks.
const (
	FieldVolume Field = 1 << iota
	FieldMotion
	FieldCameraShake
	FieldRenderScale
	FieldFrameCap
	FieldQuality
	FieldLanguage
	FieldBindings
	FieldExtra
	FieldAll Field = 1<<iota - 1
)

func diff(a, b Settings) Field {
	var f Field
	if a.Volume != b.Volume {
		f |= FieldVolume
	}
	if a.Motion != b.Motion {
		f |= FieldMotion
	}
	if a.CameraShake != b.CameraShake {
		f |= FieldCameraShake
	}
	if a.RenderScale != b.RenderScale {
		f |= FieldRenderScale
	}
	if a.FrameCap != b.FrameCap {
		f |= FieldFrameCap
	}
	if a.Quality != b.Quality {
		f |= FieldQuality
	}
	if a.Language != b.Language {
		f |= FieldLanguage
	}
	if !jsonEqual(a.Bindings, b.Bindings) {
		f |= FieldBindings
	}
	if !jsonEqual(a.Extra, b.Extra) {
		f |= FieldExtra
	}
	return f
}

func jsonEqual(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}

// Migration upgrades a stored document from version From to From+1. Doc maps
// top-level field names to raw JSON; edit it in place.
type Migration struct {
	From int
	Up   func(doc map[string]json.RawMessage) error
}

// Config tunes a Manager.
type Config struct {
	// Version is the current document version. Zero selects 1.
	Version int
	// Migrations upgrade older documents, one step per version.
	Migrations []Migration
	// Store and Key say where to save. A nil Store keeps settings in memory.
	Store *storage.Namespaced
	Key   string // default "settings"
	// NoAutoSave stops Set and Update from saving; call Save yourself.
	NoAutoSave bool
	// Languages, if set, lists the allowed language codes. Other values fall
	// back to the first entry.
	Languages []string
}

// Errors from Load.
var (
	// ErrTooNew means the stored document is from a newer build.
	ErrTooNew = errors.New("settings: stored version is newer than this build")
	// ErrCorrupt means the stored document could not be decoded.
	ErrCorrupt = errors.New("settings: stored document is corrupt")
)

type hook struct {
	mask Field
	fn   func(Settings, Field)
}

// Manager owns the current Settings. It is not safe for concurrent use.
type Manager struct {
	cfg          Config
	cur          Settings
	hooks        []hook
	systemMotion bool
	readOnly     bool // a newer document is in storage; never overwrite it
}

// NewManager creates a Manager holding the defaults.
func NewManager(cfg Config) *Manager {
	if cfg.Version <= 0 {
		cfg.Version = 1
	}
	if cfg.Key == "" {
		cfg.Key = "settings"
	}
	m := &Manager{cfg: cfg}
	m.cur = m.valid(Defaults(cfg.Version))
	return m
}

func (m *Manager) valid(s Settings) Settings {
	s.Version = m.cfg.Version
	s = s.Valid()
	if len(m.cfg.Languages) > 0 {
		ok := false
		for _, l := range m.cfg.Languages {
			if l == s.Language {
				ok = true
			}
		}
		if !ok {
			s.Language = m.cfg.Languages[0]
		}
	}
	return s
}

// Get returns a copy of the current settings.
func (m *Manager) Get() Settings {
	s := m.cur
	s.Extra = cloneExtra(s.Extra)
	return s
}

func cloneExtra(in map[string]json.RawMessage) map[string]json.RawMessage {
	if in == nil {
		return nil
	}
	out := make(map[string]json.RawMessage, len(in))
	for k, v := range in {
		out[k] = append(json.RawMessage(nil), v...)
	}
	return out
}

// OnChange registers fn to run after a change that touches any field in mask,
// with the new settings and the fields that changed. Hooks run in
// registration order. Load and Reset run them too.
func (m *Manager) OnChange(mask Field, fn func(Settings, Field)) {
	m.hooks = append(m.hooks, hook{mask, fn})
}

// Apply runs every hook once with all fields marked changed. Call it once
// after Load so the game starts with the stored options.
func (m *Manager) Apply() { m.fire(FieldAll) }

func (m *Manager) fire(changed Field) {
	if changed == 0 {
		return
	}
	cur := m.Get()
	for _, h := range m.hooks {
		if h.mask&changed != 0 {
			h.fn(cur, changed&h.mask)
		}
	}
}

// Update changes the settings with fn, validates the result, saves it (unless
// NoAutoSave) and runs the hooks for the fields that changed. It returns the
// changed fields and any save error. The settings change even if saving fails.
func (m *Manager) Update(fn func(*Settings)) (Field, error) {
	next := m.Get()
	fn(&next)
	next = m.valid(next)
	changed := diff(m.cur, next)
	if changed == 0 {
		return 0, nil
	}
	m.cur = next
	var err error
	if !m.cfg.NoAutoSave {
		err = m.Save()
	}
	m.fire(changed)
	return changed, err
}

// SetVolume changes one bus volume.
func (m *Manager) SetVolume(bus string, v float64) (Field, error) {
	return m.Update(func(s *Settings) { s.Volume = s.Volume.With(bus, v) })
}

// SetBindings stores the exported bindings of an actions.Map.
func (m *Manager) SetBindings(b actions.SavedBindings) (Field, error) {
	return m.Update(func(s *Settings) { s.Bindings = b })
}

// Reset restores the defaults (the language is kept) and saves.
func (m *Manager) Reset() (Field, error) {
	lang := m.cur.Language
	return m.Update(func(s *Settings) {
		*s = Defaults(m.cfg.Version)
		s.Language = lang
	})
}

// SetSystemReducedMotion records the operating system preference
// (prefers-reduced-motion). It affects ReducedMotion only when Motion is
// MotionSystem.
func (m *Manager) SetSystemReducedMotion(reduce bool) {
	if m.systemMotion == reduce {
		return
	}
	m.systemMotion = reduce
	if m.cur.Motion == MotionSystem {
		m.fire(FieldMotion)
	}
}

// ReducedMotion reports whether motion should be reduced.
func (m *Manager) ReducedMotion() bool {
	switch m.cur.Motion {
	case MotionReduce:
		return true
	case MotionFull:
		return false
	}
	return m.systemMotion
}

// ShakeScale returns the multiplier for camera shake: 0 when motion is
// reduced, else the CameraShake setting.
func (m *Manager) ShakeScale() float64 {
	if m.ReducedMotion() {
		return 0
	}
	return m.cur.CameraShake
}

// Save writes the settings. It refuses (and returns ErrTooNew) when the
// stored document came from a newer build.
func (m *Manager) Save() error {
	if m.cfg.Store == nil {
		return nil
	}
	if m.readOnly {
		return ErrTooNew
	}
	return storage.SetJSON(m.cfg.Store, m.cfg.Key, m.cur)
}

// Load reads the stored document, migrates and validates it, and runs the
// hooks for the fields that differ from what the Manager held. It returns
// whether a document was found. A missing document is not an error. On a
// corrupt document the settings stay at their current values and a later Save
// replaces it. On a newer document Load returns ErrTooNew and Save is disabled.
func (m *Manager) Load() (bool, error) {
	if m.cfg.Store == nil {
		return false, nil
	}
	raw, ok := m.cfg.Store.Get(m.cfg.Key)
	if !ok {
		return false, nil
	}
	loaded, err := m.decode([]byte(raw))
	if errors.Is(err, ErrTooNew) {
		m.readOnly = true
		return true, err
	}
	if err != nil {
		return true, err
	}
	m.readOnly = false
	changed := diff(m.cur, loaded)
	m.cur = loaded
	m.fire(changed)
	return true, nil
}

func (m *Manager) decode(raw []byte) (Settings, error) {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil || doc == nil {
		return Settings{}, fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	version := 0
	if v, ok := doc["version"]; ok {
		if err := json.Unmarshal(v, &version); err != nil {
			return Settings{}, fmt.Errorf("%w: version: %v", ErrCorrupt, err)
		}
	}
	if version > m.cfg.Version {
		return Settings{}, fmt.Errorf("%w (%d > %d)", ErrTooNew, version, m.cfg.Version)
	}
	if version < 1 {
		version = 1 // an unversioned document is treated as the first version
	}
	for version < m.cfg.Version {
		var step *Migration
		for i := range m.cfg.Migrations {
			if m.cfg.Migrations[i].From == version {
				step = &m.cfg.Migrations[i]
			}
		}
		if step == nil {
			return Settings{}, fmt.Errorf("%w: no migration from version %d", ErrCorrupt, version)
		}
		if err := step.Up(doc); err != nil {
			return Settings{}, fmt.Errorf("settings: migrate from %d: %w", version, err)
		}
		version++
		doc["version"], _ = json.Marshal(version)
	}
	merged, _ := json.Marshal(doc)
	// Start from the defaults so fields missing in an older document keep
	// sensible values instead of zero.
	s := Defaults(m.cfg.Version)
	dec := json.NewDecoder(bytes.NewReader(merged))
	if err := dec.Decode(&s); err != nil {
		return Settings{}, fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	return m.valid(s), nil
}
