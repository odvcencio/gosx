package game

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"m31labs.dev/gosx/game/storage"
)

// SlotFormat is the version of the envelope that wraps every saved payload.
const SlotFormat = 1

// SaveKind says how a save was made.
type SaveKind string

const (
	// SaveAuto is a save the game made by itself.
	SaveAuto SaveKind = "auto"
	// SaveManual is a save the player asked for.
	SaveManual SaveKind = "manual"
)

// Errors returned by SlotStore.
var (
	// ErrNoSave means no valid copy of the slot exists.
	ErrNoSave = errors.New("game: no save in slot")
	// ErrSaveCorrupt means a slot exists but no copy of it passes its checksum.
	ErrSaveCorrupt = errors.New("game: save is corrupt")
	// ErrSaveTooNew means the save was written by a newer build.
	ErrSaveTooNew = errors.New("game: save is from a newer version")
	// ErrSaveUnreadable means the store cannot be read at all (private
	// browsing, storage disabled); the error wraps the store's own.
	ErrSaveUnreadable = errors.New("game: save storage cannot be read")
	// ErrSaveConflict means the slot changed since the caller loaded it, for
	// example in another tab (SaveBytesIfSeq).
	ErrSaveConflict = errors.New("game: save changed since it was loaded")
)

// SaveMigration upgrades a payload from version From to From+1.
type SaveMigration struct {
	From int
	Up   func(payload json.RawMessage) (json.RawMessage, error)
}

// SlotConfig tunes a SlotStore.
type SlotConfig struct {
	// Store is the key/value store. Required.
	Store *storage.Namespaced
	// Version is the current payload schema version. Zero selects 1.
	Version int
	// Migrations upgrade older payloads one version at a time.
	Migrations []SaveMigration
	// Now supplies the save time. Default time.Now.
	Now func() time.Time
}

// SlotStore keeps named save slots in a key/value store. Every slot has two
// physical copies. A write goes to the copy that does not hold the newest
// valid save, so a write that is cut short (the tab closes, the quota fills,
// the power fails) leaves the previous save readable. A copy carries a
// sequence number and a SHA-256 checksum of its payload; Load returns the
// valid copy with the highest sequence number.
//
// Use names like "auto" and "slot1". Not safe for concurrent use.
type SlotStore struct{ cfg SlotConfig }

// NewSlotStore creates a SlotStore.
func NewSlotStore(cfg SlotConfig) *SlotStore {
	if cfg.Version <= 0 {
		cfg.Version = 1
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &SlotStore{cfg: cfg}
}

type slotEnvelope struct {
	Format  int             `json:"format"`
	Seq     uint64          `json:"seq"`
	Kind    SaveKind        `json:"kind"`
	Version int             `json:"version"`
	Label   string          `json:"label,omitempty"`
	SavedAt int64           `json:"savedAt"` // Unix milliseconds
	Sum     string          `json:"sum"`
	Payload json.RawMessage `json:"payload"`
}

// SlotInfo describes a saved slot for a load screen.
type SlotInfo struct {
	Name    string
	Kind    SaveKind
	Label   string
	Version int
	Seq     uint64
	SavedAt time.Time
}

func sumOf(payload []byte) string {
	h := sha256.Sum256(payload)
	return hex.EncodeToString(h[:])
}

func (s *SlotStore) key(name string, copy int) string {
	return "save." + name + "." + string(rune('a'+copy))
}

func validName(name string) bool {
	return name != "" && !strings.ContainsAny(name, ". /\\")
}

// read returns the envelope of one copy, or ok=false when it is absent,
// torn, or fails its checksum. err is set when the store cannot be read.
func (s *SlotStore) read(name string, copy int) (env slotEnvelope, ok, present bool, err error) {
	raw, present, err := s.cfg.Store.Read(s.key(name, copy))
	if err != nil {
		return slotEnvelope{}, false, false, fmt.Errorf("%w: %w", ErrSaveUnreadable, err)
	}
	if !present {
		return slotEnvelope{}, false, false, nil
	}
	if json.Unmarshal([]byte(raw), &env) != nil || env.Format != SlotFormat || env.Sum != sumOf(env.Payload) {
		return slotEnvelope{}, false, true, nil
	}
	return env, true, true, nil
}

// newest returns the valid envelope with the highest sequence, which copy
// holds it, and whether any copy was present at all.
func (s *SlotStore) newest(name string) (env slotEnvelope, copy int, found, present bool, err error) {
	for c := 0; c < 2; c++ {
		e, ok, pres, readErr := s.read(name, c)
		if readErr != nil {
			return slotEnvelope{}, 0, false, false, readErr
		}
		present = present || pres
		if ok && (!found || e.Seq > env.Seq) {
			env, copy, found = e, c, true
		}
	}
	return
}

// SaveBytes writes payload (which must be valid JSON) to the slot. label is
// free text for a load screen, such as a level name.
func (s *SlotStore) SaveBytes(name string, kind SaveKind, label string, payload []byte) error {
	_, err := s.save(name, kind, label, payload, nil)
	return err
}

// SaveBytesIfSeq is SaveBytes that writes only when the slot's newest save
// still has sequence number seq (the SlotInfo.Seq the caller loaded, or 0
// for a slot that had no save). Otherwise it writes nothing and returns
// ErrSaveConflict: another tab, say, has saved since. It returns the new
// save's sequence number.
func (s *SlotStore) SaveBytesIfSeq(name string, seq uint64, kind SaveKind, label string, payload []byte) (uint64, error) {
	return s.save(name, kind, label, payload, &seq)
}

func (s *SlotStore) save(name string, kind SaveKind, label string, payload []byte, expect *uint64) (uint64, error) {
	if s == nil || s.cfg.Store == nil {
		return 0, storage.ErrNoBackend
	}
	if !validName(name) {
		return 0, fmt.Errorf("game: bad save slot name %q", name)
	}
	if !json.Valid(payload) {
		return 0, errors.New("game: save payload is not valid JSON")
	}
	cur, curCopy, found, _, err := s.newest(name)
	if err != nil {
		// Never write over saves that cannot be read.
		return 0, err
	}
	target, seq := 0, uint64(1)
	if found {
		target, seq = 1-curCopy, cur.Seq+1
	}
	if expect != nil && seq-1 != *expect {
		return 0, ErrSaveConflict
	}
	env := slotEnvelope{
		Format: SlotFormat, Seq: seq, Kind: kind, Version: s.cfg.Version, Label: label,
		SavedAt: s.cfg.Now().UnixMilli(), Sum: sumOf(payload), Payload: append(json.RawMessage(nil), payload...),
	}
	data, err := json.Marshal(env)
	if err != nil {
		return 0, err
	}
	if err := s.cfg.Store.Set(s.key(name, target), string(data)); err != nil {
		return 0, err
	}
	return seq, nil
}

// Save encodes v as JSON and writes it to the slot.
func (s *SlotStore) Save(name string, kind SaveKind, label string, v any) error {
	payload, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return s.SaveBytes(name, kind, label, payload)
}

// LoadBytes returns the newest valid payload of the slot, migrated to the
// current version.
func (s *SlotStore) LoadBytes(name string) ([]byte, SlotInfo, error) {
	if s == nil || s.cfg.Store == nil {
		return nil, SlotInfo{}, ErrNoSave
	}
	env, _, found, present, err := s.newest(name)
	if err != nil {
		return nil, SlotInfo{}, err
	}
	if !found {
		if present {
			return nil, SlotInfo{}, ErrSaveCorrupt
		}
		return nil, SlotInfo{}, ErrNoSave
	}
	info := infoOf(name, env)
	if env.Version > s.cfg.Version {
		return nil, info, fmt.Errorf("%w (%d > %d)", ErrSaveTooNew, env.Version, s.cfg.Version)
	}
	payload := env.Payload
	for v := env.Version; v < s.cfg.Version; v++ {
		var step *SaveMigration
		for i := range s.cfg.Migrations {
			if s.cfg.Migrations[i].From == v {
				step = &s.cfg.Migrations[i]
			}
		}
		if step == nil {
			return nil, info, fmt.Errorf("game: no save migration from version %d", v)
		}
		next, err := step.Up(payload)
		if err != nil {
			return nil, info, fmt.Errorf("game: migrate save from version %d: %w", v, err)
		}
		payload = next
	}
	info.Version = s.cfg.Version
	return payload, info, nil
}

// LoadSave decodes the newest valid payload of the slot into T.
func LoadSave[T any](s *SlotStore, name string) (T, SlotInfo, error) {
	var v T
	raw, info, err := s.LoadBytes(name)
	if err != nil {
		return v, info, err
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return v, info, fmt.Errorf("%w: %v", ErrSaveCorrupt, err)
	}
	return v, info, nil
}

func infoOf(name string, e slotEnvelope) SlotInfo {
	return SlotInfo{Name: name, Kind: e.Kind, Label: e.Label, Version: e.Version, Seq: e.Seq, SavedAt: time.UnixMilli(e.SavedAt)}
}

// Info returns the metadata of the newest valid copy without migrating it.
func (s *SlotStore) Info(name string) (SlotInfo, bool) {
	if s == nil || s.cfg.Store == nil {
		return SlotInfo{}, false
	}
	env, _, found, _, err := s.newest(name)
	if err != nil || !found {
		return SlotInfo{}, false
	}
	return infoOf(name, env), true
}

// List returns the metadata of the named slots that hold a valid save,
// newest first.
func (s *SlotStore) List(names ...string) []SlotInfo {
	var out []SlotInfo
	for _, n := range names {
		if info, ok := s.Info(n); ok {
			out = append(out, info)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].SavedAt.After(out[j].SavedAt) })
	return out
}

// Delete removes both copies of a slot.
func (s *SlotStore) Delete(name string) error {
	if s == nil || s.cfg.Store == nil {
		return storage.ErrNoBackend
	}
	e1 := s.cfg.Store.Delete(s.key(name, 0))
	e2 := s.cfg.Store.Delete(s.key(name, 1))
	return errors.Join(e1, e2)
}

// SaveRuntime stores r.Snapshot() in the slot. The runtime needs a
// SnapshotCodec (see SaveCodec.SnapshotCodec) that produces JSON.
func (s *SlotStore) SaveRuntime(name string, kind SaveKind, label string, r *Runtime) error {
	snap := r.Snapshot()
	if len(snap) == 0 {
		return errors.New("game: runtime has no snapshot codec")
	}
	return s.SaveBytes(name, kind, label, snap)
}

// LoadRuntime restores the runtime from the slot. A failed restore changes
// neither the world nor the clock (see Runtime.RestoreSnapshot).
func (s *SlotStore) LoadRuntime(name string, r *Runtime) error {
	raw, _, err := s.LoadBytes(name)
	if err != nil {
		return err
	}
	return r.RestoreSnapshot(raw)
}

// Autosaver saves on a timer. Call Update every frame; when Interval has
// passed and the game is not Blocked it calls Capture and writes the result
// to the "auto" slot.
type Autosaver struct {
	Store    *SlotStore
	Name     string // default "auto"
	Interval time.Duration
	// Capture returns the payload and label to save. Returning ok=false skips
	// this round (for example during a cutscene).
	Capture func() (payload []byte, label string, ok bool)
	// OnError receives write errors.
	OnError func(error)

	acc time.Duration
}

// Update adds dt to the timer and saves when it is due. It reports whether a
// save was attempted.
func (a *Autosaver) Update(dt time.Duration) bool {
	if a == nil || a.Store == nil || a.Capture == nil || a.Interval <= 0 {
		return false
	}
	a.acc += dt
	if a.acc < a.Interval {
		return false
	}
	payload, label, ok := a.Capture()
	if !ok {
		return false // try again next frame
	}
	a.acc = 0
	name := a.Name
	if name == "" {
		name = "auto"
	}
	if err := a.Store.SaveBytes(name, SaveAuto, label, payload); err != nil && a.OnError != nil {
		a.OnError(err)
	}
	return true
}
