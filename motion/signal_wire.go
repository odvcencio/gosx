package motion

import "encoding/json"

// MarshalJSON preserves an authored zero upper bound. The browser defaults a
// missing map/clamp maximum to one; zero is meaningful, not an absent option.
// Other signals retain their compact wire representation.
func (s SignalSpec) MarshalJSON() ([]byte, error) {
	type plain SignalSpec
	if (s.Kind == SignalMap || s.Kind == SignalClamp) && s.Max == 0 {
		return json.Marshal(struct {
			plain
			Max float64 `json:"max"`
		}{plain: plain(s), Max: s.Max})
	}
	return json.Marshal(plain(s))
}
