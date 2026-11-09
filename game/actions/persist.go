package actions

import (
	"encoding/json"
	"fmt"

	"m31labs.dev/gosx/game/storage"
)

// BindingsVersion is the format version of saved bindings.
const BindingsVersion = 1

// SavedBinding is the stored form of one Binding.
type SavedBinding struct {
	Source string  `json:"src"`
	Scale  float64 `json:"scale,omitempty"`
}

// SavedBindings holds the bindings that differ from the defaults, keyed by
// context and then action. It is plain data so it can sit inside a larger
// settings document.
type SavedBindings struct {
	Version  int                                  `json:"version"`
	Contexts map[string]map[string][]SavedBinding `json:"contexts,omitempty"`
}

func equalBindings(a, b []Binding) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Source != b[i].Source || a[i].scale() != b[i].scale() {
			return false
		}
	}
	return true
}

// Export returns the bindings that differ from the defaults. The output is
// the same for the same state: maps encode with sorted keys.
func (m *Map) Export() SavedBindings {
	out := SavedBindings{Version: BindingsVersion}
	for _, name := range m.order {
		for _, d := range m.contexts[name].defs {
			if equalBindings(d.Bindings, d.defaults) {
				continue
			}
			if out.Contexts == nil {
				out.Contexts = map[string]map[string][]SavedBinding{}
			}
			if out.Contexts[name] == nil {
				out.Contexts[name] = map[string][]SavedBinding{}
			}
			list := make([]SavedBinding, len(d.Bindings))
			for i, b := range d.Bindings {
				list[i] = SavedBinding{Source: b.Source.String(), Scale: b.scale()}
			}
			out.Contexts[name][d.Name] = list
		}
	}
	return out
}

// Import applies saved bindings over the current ones. Entries for contexts
// or actions that no longer exist are skipped, so a save from an older build
// loads. An entry that does not parse, or that has a different slot count
// from the action it names, is skipped too. The count of applied actions is
// returned. A version newer than BindingsVersion is an error.
func (m *Map) Import(saved SavedBindings) (int, error) {
	if saved.Version > BindingsVersion {
		return 0, fmt.Errorf("actions: bindings version %d is newer than %d", saved.Version, BindingsVersion)
	}
	applied := 0
	for ctx, acts := range saved.Contexts {
		for action, list := range acts {
			d := m.def(ctx, action)
			if d == nil || len(list) != len(d.defaults) {
				continue
			}
			next := make([]Binding, len(list))
			ok := true
			for i, sb := range list {
				src, err := ParseSource(sb.Source)
				if err != nil {
					ok = false
					break
				}
				if !src.Zero() && !d.defaults[i].Source.Zero() && src.Class() != d.defaults[i].Source.Class() {
					ok = false
					break
				}
				next[i] = Binding{Source: src, Scale: Binding{Scale: sb.Scale}.scale()}
			}
			if ok {
				d.Bindings = next
				applied++
			}
		}
	}
	return applied, nil
}

// Save writes the exported bindings as JSON under key.
func (m *Map) Save(store *storage.Namespaced, key string) error {
	return storage.SetJSON(store, key, m.Export())
}

// Load reads bindings saved under key and imports them. A missing key is not
// an error and returns (false, nil). Stored data that does not decode is
// reported as an error and leaves the bindings unchanged.
func (m *Map) Load(store *storage.Namespaced, key string) (bool, error) {
	raw, ok := store.Get(key)
	if !ok {
		return false, nil
	}
	var saved SavedBindings
	if err := json.Unmarshal([]byte(raw), &saved); err != nil {
		return false, fmt.Errorf("actions: decode bindings: %w", err)
	}
	_, err := m.Import(saved)
	return err == nil, err
}
