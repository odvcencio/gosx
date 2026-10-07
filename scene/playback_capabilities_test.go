package scene

import (
	"encoding/json"
	"testing"
)

func TestPlaybackCapabilitiesPreserveExplicitOptIn(t *testing.T) {
	for _, capability := range []struct {
		name string
		set  func(*Props, *bool)
	}{
		{"timelines", func(p *Props, enabled *bool) { p.Timelines = enabled }},
		{"particleBursts", func(p *Props, enabled *bool) { p.ParticleBursts = enabled }},
	} {
		for _, state := range []struct {
			name    string
			enabled *bool
		}{
			{"absent", nil}, {"false", Bool(false)}, {"true", Bool(true)},
		} {
			t.Run(capability.name+"/"+state.name, func(t *testing.T) {
				var props Props
				capability.set(&props, state.enabled)
				payload, err := json.Marshal(props)
				if err != nil {
					t.Fatal(err)
				}
				var wire map[string]any
				if err := json.Unmarshal(payload, &wire); err != nil {
					t.Fatal(err)
				}
				for name, lowered := range map[string]map[string]any{
					"JSON": wire, "legacy": props.LegacyProps(), "spread": props.GoSXSpreadProps(),
				} {
					value, exists := lowered[capability.name]
					if state.enabled == nil {
						if exists {
							t.Errorf("%s: absent capability was serialized", name)
						}
					} else if !exists || value != *state.enabled {
						t.Errorf("%s: capability = %v, want %t", name, value, *state.enabled)
					}
				}
			})
		}
	}
}
