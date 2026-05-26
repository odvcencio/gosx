package program

import "encoding/json"

// DecodeProgramJSON deserializes an island program from JSON, injecting
// Surface=SurfaceDOM into the in-memory model. The wire format has no
// "surface" field per ADR 0001; the surface kind is recovered from the
// caller's choice of decoder.
//
// This is the surface-aware sibling of DecodeJSON. Prefer DecodeProgramJSON
// for any code path that hands the program to a reconciler or VM that cares
// about surface kind.
func DecodeProgramJSON(data []byte) (*Program, error) {
	var p Program
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	p.Surface = SurfaceDOM
	return &p, nil
}
