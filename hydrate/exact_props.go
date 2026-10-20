package hydrate

import (
	"bytes"
	"encoding/json"
)

// exactPropsJSON preserves the original wire text only when a JSON integer is
// outside JavaScript's safe integer range. The regular props object remains
// available to browser hosts; WASM must receive the original tokens rather than
// JSON.stringify's rounded version. Ordinary props carry no duplicate snapshot.
func exactPropsJSON(props json.RawMessage) string {
	decoder := json.NewDecoder(bytes.NewReader(props))
	decoder.UseNumber()
	for {
		token, err := decoder.Token()
		if err != nil {
			return ""
		}
		number, ok := token.(json.Number)
		if !ok {
			continue
		}
		n, err := number.Int64()
		if err == nil && (n < -(1<<53-1) || n > 1<<53-1) {
			return string(props)
		}
	}
}

// MarshalJSON adds a lossless WASM handoff for large integer props. Deriving it
// from Props at serialization also covers manually constructed entries and
// avoids a second mutable source of truth.
func (entry IslandEntry) MarshalJSON() ([]byte, error) {
	type plain IslandEntry
	return json.Marshal(struct {
		plain
		PropsJSON string `json:"propsJSON,omitempty"`
	}{plain(entry), exactPropsJSON(entry.Props)})
}

// MarshalJSON preserves the same numeric contract for headless islands.
func (entry ComputeIslandEntry) MarshalJSON() ([]byte, error) {
	type plain ComputeIslandEntry
	return json.Marshal(struct {
		plain
		PropsJSON string `json:"propsJSON,omitempty"`
	}{plain(entry), exactPropsJSON(entry.Props)})
}
