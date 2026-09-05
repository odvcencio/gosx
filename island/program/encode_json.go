// JSON serialization for IslandProgram (dev mode).
package program

import (
	"encoding/json"
	"fmt"
)

// EncodeJSON serializes an IslandProgram to JSON (dev mode).
func EncodeJSON(p *Program) ([]byte, error) {
	if err := p.ValidateServerSlots(); err != nil {
		return nil, err
	}
	if p.HasServerSlots() {
		type legacy Program
		return json.MarshalIndent(struct {
			*legacy
			Requires string `json:"requires"`
		}{(*legacy)(p), ServerSlotCapability}, "", "  ")
	}
	return json.MarshalIndent(p, "", "  ")
}

// DecodeJSON deserializes an IslandProgram from JSON.
func DecodeJSON(data []byte) (*Program, error) {
	var p Program
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	var meta struct {
		Requires string `json:"requires"`
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	if meta.Requires != "" && meta.Requires != ServerSlotCapability {
		return nil, fmt.Errorf("unsupported program capability %q", meta.Requires)
	}
	if p.HasServerSlots() && meta.Requires != ServerSlotCapability {
		return nil, fmt.Errorf("ServerSlot requires explicit capability")
	}
	if err := p.ValidateServerSlots(); err != nil {
		return nil, err
	}
	return &p, nil
}
