// JSON serialization for IslandProgram (dev mode).
package program

import "encoding/json"

// EncodeJSON serializes an IslandProgram to JSON (dev mode).
func EncodeJSON(p *Program) ([]byte, error) {
	return json.MarshalIndent(p, "", "  ")
}

// DecodeJSON deserializes an IslandProgram from JSON.
func DecodeJSON(data []byte) (*Program, error) {
	var p Program
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	return &p, nil
}
