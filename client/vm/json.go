package vm

import "encoding/json"

// ValueFromAny converts a decoded Go value into a VM value.
func ValueFromAny(value any) Value {
	return parseAnyValue(value)
}

// ValueFromJSON decodes arbitrary JSON into a VM value.
func ValueFromJSON(raw string) (Value, error) {
	var value any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return Value{}, err
	}
	return ValueFromAny(value), nil
}
